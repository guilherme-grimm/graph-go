package docker

import (
	"bufio"
	"context"
	"io"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strconv"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// selfContext describes where graph-go itself runs, so each container can be
// reached at an address that works from there. NewDockerDiscovery detects it
// once at startup.
type selfContext struct {
	// InContainer is true when graph-go runs inside a container.
	InContainer bool
	// ContainerID is graph-go's own container, when it could be identified.
	// It is skipped during discovery.
	ContainerID string
	// Networks maps each network graph-go's container is attached to onto
	// that network's gateway (the host, as seen from the container).
	Networks map[string]netip.Addr
}

// address is where graph-go connects to reach a container's service.
type address struct {
	Host string
	Port uint16
	// Via says how the address was chosen, for logs and tests.
	Via string
}

const (
	viaPinnedNetwork = "pinned-network"
	viaSharedNetwork = "shared-network"
	viaPublishedPort = "published-port"
	viaContainerIP   = "container-ip"
	viaServiceName   = "service-name"
	viaFallback      = "fallback"
)

// resolveAddress picks the host:port graph-go uses to reach a container's
// service port, depending on where graph-go runs:
//
//  1. the docker.network from config, if the container is on it;
//  2. in a container: the container's IP on a network both share;
//  3. a published host port: on the host via its loopback (or the bound IP),
//     from a container via the gateway, when the port is bound on all
//     interfaces;
//  4. on a Linux host: the container IP, since bridge networks are routable
//     from the host there (not on Docker Desktop);
//  5. otherwise the Compose service name, which resolves only from inside
//     the project's network.
//
// The Compose service name used to come first, which only worked when
// graph-go ran inside the same Compose project. A shared network's IP also
// avoids ambiguous DNS when two projects on one network both have a "db".
func resolveAddress(inspect container.InspectResponse, port uint16, pinned string, self selfContext, goos string) address {
	nets := containerNetworks(inspect)

	if pinned != "" {
		if ip, ok := nets[pinned]; ok {
			return address{Host: ip.String(), Port: port, Via: viaPinnedNetwork}
		}
	}

	if self.InContainer {
		for _, name := range sortedKeys(nets) {
			if _, shared := self.Networks[name]; shared {
				return address{Host: nets[name].String(), Port: port, Via: viaSharedNetwork}
			}
		}
	}

	if a, ok := publishedAddress(inspect, port, self); ok {
		return a
	}

	if !self.InContainer && goos == "linux" {
		if ip, ok := firstNetworkIP(nets); ok {
			return address{Host: ip.String(), Port: port, Via: viaContainerIP}
		}
	}

	if inspect.Config != nil {
		if svc := inspect.Config.Labels[labelComposeService]; svc != "" {
			return address{Host: svc, Port: port, Via: viaServiceName}
		}
	}
	if ip, ok := firstNetworkIP(nets); ok {
		return address{Host: ip.String(), Port: port, Via: viaFallback}
	}
	return address{Host: "localhost", Port: port, Via: viaFallback}
}

// firstNetworkIP is the container's IP on its first network by name, so the
// choice doesn't depend on map order.
func firstNetworkIP(nets map[string]netip.Addr) (netip.Addr, bool) {
	keys := sortedKeys(nets)
	if len(keys) == 0 {
		return netip.Addr{}, false
	}
	return nets[keys[0]], true
}

// publishedAddress finds a host binding for port/tcp that graph-go can reach.
// Only IPv4 bindings are used: the address ends up inside DSNs and URLs
// built with plain host:port formatting.
func publishedAddress(inspect container.InspectResponse, port uint16, self selfContext) (address, bool) {
	if inspect.NetworkSettings == nil {
		return address{}, false
	}
	p, ok := network.PortFrom(port, network.TCP)
	if !ok {
		return address{}, false
	}
	for _, b := range inspect.NetworkSettings.Ports[p] {
		hostPort, err := strconv.ParseUint(b.HostPort, 10, 16)
		if err != nil || hostPort == 0 {
			continue
		}
		wildcard := !b.HostIP.IsValid() || b.HostIP.IsUnspecified()
		switch {
		case !self.InContainer && wildcard:
			return address{Host: "127.0.0.1", Port: uint16(hostPort), Via: viaPublishedPort}, true
		case !self.InContainer && b.HostIP.Is4():
			return address{Host: b.HostIP.String(), Port: uint16(hostPort), Via: viaPublishedPort}, true
		case self.InContainer && wildcard:
			// A port bound to the host's loopback isn't reachable from a
			// container; one bound everywhere is, through the gateway.
			if gw, ok := selfGateway(self); ok {
				return address{Host: gw.String(), Port: uint16(hostPort), Via: viaPublishedPort}, true
			}
		}
	}
	return address{}, false
}

// containerNetworks maps the container's networks to its IPv4 address on each.
func containerNetworks(inspect container.InspectResponse) map[string]netip.Addr {
	out := map[string]netip.Addr{}
	if inspect.NetworkSettings == nil {
		return out
	}
	for name, ep := range inspect.NetworkSettings.Networks {
		if ep != nil && ep.IPAddress.Is4() {
			out[name] = ep.IPAddress
		}
	}
	return out
}

// selfGateway is the gateway of the first (by name) network graph-go's
// container is on.
func selfGateway(self selfContext) (netip.Addr, bool) {
	for _, name := range sortedKeys(self.Networks) {
		if gw := self.Networks[name]; gw.Is4() {
			return gw, true
		}
	}
	return netip.Addr{}, false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// detectSelf works out whether graph-go runs in a container and, if so,
// which one and on which networks. Failing to identify the container is
// not an error: resolution then behaves as before (Compose service names).
func detectSelf(ctx context.Context, cli *client.Client) selfContext {
	if !inContainer() {
		return selfContext{}
	}
	self := selfContext{InContainer: true}

	var candidates []string
	if id := containerIDFromMountinfo(); id != "" {
		candidates = append(candidates, id)
	}
	// Docker sets the hostname to the short container ID unless --hostname
	// overrides it.
	if h, err := os.Hostname(); err == nil && h != "" {
		candidates = append(candidates, h)
	}

	for _, id := range candidates {
		res, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			continue
		}
		self.ContainerID = res.Container.ID
		self.Networks = map[string]netip.Addr{}
		if ns := res.Container.NetworkSettings; ns != nil {
			for name, ep := range ns.Networks {
				if ep != nil {
					self.Networks[name] = ep.Gateway
				}
			}
		}
		break
	}
	return self
}

// inContainer reports whether the process runs in a Docker or Podman
// container.
func inContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

var mountinfoContainerID = regexp.MustCompile(`/containers/([0-9a-f]{64})/`)

// containerIDFromMountinfo reads graph-go's container ID from the bind
// mounts Docker sets up (/etc/hostname and friends live under
// /var/lib/docker/containers/<id>/). Works on cgroup v2, where
// /proc/self/cgroup no longer carries the ID.
func containerIDFromMountinfo() string {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	return parseMountinfoContainerID(f)
}

func parseMountinfoContainerID(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if m := mountinfoContainerID.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	return ""
}

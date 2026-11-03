package docker

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
)

type binding struct {
	ip   string
	port string
}

// target builds the inspect data resolveAddress reads: Compose service label,
// networks with IPs, and the host bindings of 5432/tcp.
func target(service string, nets map[string]string, published ...binding) container.InspectResponse {
	eps := map[string]*network.EndpointSettings{}
	for name, ip := range nets {
		eps[name] = &network.EndpointSettings{IPAddress: netip.MustParseAddr(ip)}
	}
	var bindings []network.PortBinding
	for _, b := range published {
		pb := network.PortBinding{HostPort: b.port}
		if b.ip != "" {
			pb.HostIP = netip.MustParseAddr(b.ip)
		}
		bindings = append(bindings, pb)
	}
	ports := network.PortMap{}
	if len(bindings) > 0 {
		ports[network.MustParsePort("5432/tcp")] = bindings
	}
	labels := map[string]string{}
	if service != "" {
		labels[labelComposeService] = service
	}
	return container.InspectResponse{
		Config:          &container.Config{Labels: labels},
		NetworkSettings: &container.NetworkSettings{Networks: eps, Ports: ports},
	}
}

func TestResolveAddress(t *testing.T) {
	host := selfContext{}
	inNet := func(nets ...string) selfContext {
		s := selfContext{InContainer: true, ContainerID: "self", Networks: map[string]netip.Addr{}}
		for i, n := range nets {
			s.Networks[n] = netip.MustParseAddr(fmt.Sprintf("172.30.%d.1", i))
		}
		return s
	}

	tests := []struct {
		name   string
		in     container.InspectResponse
		pinned string
		self   selfContext
		goos   string
		want   address
	}{
		{
			// The bug: on the host the Compose service name doesn't resolve.
			name: "host, linux, unpublished: container IP, not the service name",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}),
			self: host, goos: "linux",
			want: address{Host: "172.20.0.3", Port: 5432, Via: viaContainerIP},
		},
		{
			name: "host, published on all interfaces: loopback and the host port",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}, binding{"0.0.0.0", "55432"}),
			self: host, goos: "linux",
			want: address{Host: "127.0.0.1", Port: 55432, Via: viaPublishedPort},
		},
		{
			name: "host, published on a specific IPv4: that IP",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}, binding{"127.0.0.1", "55432"}),
			self: host, goos: "darwin",
			want: address{Host: "127.0.0.1", Port: 55432, Via: viaPublishedPort},
		},
		{
			name: "host, published on IPv6 loopback only: binding skipped",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}, binding{"::1", "55432"}),
			self: host, goos: "linux",
			want: address{Host: "172.20.0.3", Port: 5432, Via: viaContainerIP},
		},
		{
			// Docker Desktop: bridge IPs aren't routable from macOS.
			name: "host, darwin, unpublished: service name as the last resort",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}),
			self: host, goos: "darwin",
			want: address{Host: "db", Port: 5432, Via: viaServiceName},
		},
		{
			name: "in container, shared network: IP on it",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3", "other": "172.21.0.3"}),
			self: inNet("trader_default"), goos: "linux",
			want: address{Host: "172.20.0.3", Port: 5432, Via: viaSharedNetwork},
		},
		{
			// The README docker run: graph-go on the default bridge.
			name: "in container, no shared network, published everywhere: via the gateway",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}, binding{"0.0.0.0", "55432"}),
			self: inNet("bridge"), goos: "linux",
			want: address{Host: "172.30.0.1", Port: 55432, Via: viaPublishedPort},
		},
		{
			name: "in container, published on host loopback only: unreachable, service name",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}, binding{"127.0.0.1", "55432"}),
			self: inNet("bridge"), goos: "linux",
			want: address{Host: "db", Port: 5432, Via: viaServiceName},
		},
		{
			name: "in container, self not identified: service name, as before",
			in:   target("db", map[string]string{"trader_default": "172.20.0.3"}, binding{"0.0.0.0", "55432"}),
			self: selfContext{InContainer: true}, goos: "linux",
			want: address{Host: "db", Port: 5432, Via: viaServiceName},
		},
		{
			name:   "pinned network wins",
			in:     target("db", map[string]string{"trader_default": "172.20.0.3", "infra": "10.9.0.4"}, binding{"0.0.0.0", "55432"}),
			pinned: "infra", self: host, goos: "linux",
			want: address{Host: "10.9.0.4", Port: 5432, Via: viaPinnedNetwork},
		},
		{
			name: "plain container, no networks: localhost",
			in:   container.InspectResponse{},
			self: host, goos: "linux",
			want: address{Host: "localhost", Port: 5432, Via: viaFallback},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveAddress(tt.in, 5432, tt.pinned, tt.self, tt.goos))
		})
	}
}

func TestResolveAddress_MultipleNetworksIsDeterministic(t *testing.T) {
	in := target("", map[string]string{"zeta": "172.22.0.2", "alpha": "172.21.0.2", "mid": "172.23.0.2"})
	for range 20 {
		assert.Equal(t, "172.21.0.2", resolveAddress(in, 5432, "", selfContext{}, "linux").Host)
	}
}

func TestParseMountinfoContainerID(t *testing.T) {
	id := strings.Repeat("ab12", 16)
	mountinfo := strings.Join([]string{
		"812 790 0:47 / / rw,relatime master:1 - overlay overlay rw",
		"830 812 259:2 /var/lib/docker/containers/" + id + "/hostname /etc/hostname rw,relatime - ext4 /dev/nvme0n1p2 rw",
	}, "\n")

	assert.Equal(t, id, parseMountinfoContainerID(strings.NewReader(mountinfo)))
	assert.Empty(t, parseMountinfoContainerID(strings.NewReader("812 790 0:47 / / rw - overlay overlay rw")))
}

func TestServicePort(t *testing.T) {
	assert.Equal(t, uint16(5432), servicePort(TypePostgres, nil))
	assert.Equal(t, uint16(8080), servicePort(TypeHTTP, map[string]string{}))
	assert.Equal(t, uint16(3000), servicePort(TypeHTTP, map[string]string{"PORT": "3000"}))
	assert.Equal(t, uint16(8080), servicePort(TypeHTTP, map[string]string{"PORT": "nope"}))
}

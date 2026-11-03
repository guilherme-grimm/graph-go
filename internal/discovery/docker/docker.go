package docker

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/discovery"
)

// SourceName is the Source tag applied to ServiceInfo produced by this discoverer.
const SourceName = "docker"

// DiscoveredService represents a service found via Docker daemon inspection.
type DiscoveredService struct {
	Name        string
	Type        ServiceType
	Config      adapters.ConnectionConfig
	IPAddress   string
	ContainerID string

	// Project and Service are the Compose project and service labels (empty
	// for containers not started by Compose). ContainerName is the Docker
	// container name without the leading slash. disambiguateNames uses them
	// to rename services whose Name collides with another container's.
	Project       string
	Service       string
	ContainerName string

	// ConfigNameFromLabel is true when a graphgo.name label set Config["name"].
	// disambiguateNames never rewrites a label-supplied name, even one that
	// happens to equal the derived service name.
	ConfigNameFromLabel bool
}

const (
	labelComposeProject = "com.docker.compose.project"
	labelComposeService = "com.docker.compose.service"
)

// DockerDiscoveryConfig holds settings for Docker-based discovery.
type DockerDiscoveryConfig struct {
	Socket       string
	Network      string
	IgnoreImages []string
	SelfImage    string
}

// DockerDiscovery connects to the Docker daemon and discovers running services.
// It implements discovery.Discoverer.
type DockerDiscovery struct {
	client *client.Client
	cfg    DockerDiscoveryConfig
	logger *zap.SugaredLogger
}

// Compile-time check that DockerDiscovery satisfies discovery.Discoverer.
var _ discovery.Discoverer = (*DockerDiscovery)(nil)

// Name returns the discoverer identifier used for logging.
func (d *DockerDiscovery) Name() string { return SourceName }

// NewDockerDiscovery creates a new DockerDiscovery instance and verifies
// connectivity to the daemon via Ping.
func NewDockerDiscovery(cfg DockerDiscoveryConfig, logger *zap.SugaredLogger) (*DockerDiscovery, error) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	// API version negotiation is the default in github.com/moby/moby/client.
	var opts []client.Opt
	if cfg.Socket == "" {
		// Respect DOCKER_HOST/DOCKER_TLS_VERIFY/DOCKER_CERT_PATH and otherwise
		// fall back to the Docker SDK's platform default (Unix socket on Unix,
		// named pipe on Windows).
		opts = append(opts, client.FromEnv)
	} else {
		opts = append(opts, client.WithHost(dockerHostFromSocket(cfg.Socket, runtime.GOOS)))
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("discovery: failed to create Docker client: %w", err)
	}

	// Verify connectivity
	if _, err := cli.Ping(context.Background(), client.PingOptions{}); err != nil {
		cli.Close()
		if permErr := socketPermissionError(cli.DaemonHost()); permErr != nil {
			return nil, fmt.Errorf("discovery: failed to ping Docker daemon: %w (%w)", err, permErr)
		}
		return nil, fmt.Errorf("discovery: failed to ping Docker daemon: %w", err)
	}

	return &DockerDiscovery{client: cli, cfg: cfg, logger: logger}, nil
}

// Close releases the Docker client resources.
func (d *DockerDiscovery) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// Discover lists running containers and inspects each to build a list of
// discovered services. Each service is returned as a discovery.ServiceInfo
// with Source="docker"; Docker-specific fields (IP, container ID) are
// preserved in Metadata.
func (d *DockerDiscovery) Discover(ctx context.Context) ([]discovery.ServiceInfo, error) {
	listOpts := client.ContainerListOptions{
		Filters: make(client.Filters).Add("status", "running"),
	}

	res, err := d.client.ContainerList(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("discovery: failed to list containers: %w", err)
	}

	found := make([]DiscoveredService, 0, len(res.Items))
	for _, ctr := range res.Items {
		svc, ok := d.processContainer(ctx, ctr)
		if !ok {
			continue
		}
		found = append(found, svc)
	}
	disambiguateNames(found)

	services := make([]discovery.ServiceInfo, 0, len(found))
	for _, svc := range found {
		services = append(services, discovery.ServiceInfo{
			Name:   svc.Name,
			Type:   string(svc.Type),
			Source: SourceName,
			Config: svc.Config,
			Metadata: map[string]any{
				"ip_address":   svc.IPAddress,
				"container_id": svc.ContainerID,
			},
		})
	}

	return services, nil
}

// Watch subscribes to Docker container start/stop/die events and invokes
// onChange for each one, debounced to collapse bursts (e.g. docker-compose up
// starting many containers at once). It blocks until ctx is cancelled.
func (d *DockerDiscovery) Watch(ctx context.Context, onChange func()) error {
	deb := discovery.NewDebouncer(onChange, 2*time.Second)
	defer deb.Stop()
	return watchEvents(ctx, d.client, deb.Trigger, d.logger)
}

// processContainer inspects one container and turns it into a
// DiscoveredService: it applies the ignore label and image list, classifies
// the image, resolves host and IP, and extracts credentials plus label
// overrides. The bool is false when the container is skipped or can't be
// inspected.
func (d *DockerDiscovery) processContainer(ctx context.Context, ctr container.Summary) (DiscoveredService, bool) {
	// Inspect for full details (env vars, network settings)
	res, err := d.client.ContainerInspect(ctx, ctr.ID, client.ContainerInspectOptions{})
	if err != nil {
		d.logger.Warnw("docker: failed to inspect container", "container", truncateID(ctr.ID), "err", err)
		return DiscoveredService{}, false
	}
	inspect := res.Container

	labels := ctr.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	// 1. Check ignore label
	if ShouldIgnore(labels) {
		return DiscoveredService{}, false
	}

	// 2. Derive name
	name := deriveContainerName(labels, ctr.Names)

	// 3. Check ignore images list
	if isIgnoredImage(ctr.Image, d.cfg.IgnoreImages) {
		return DiscoveredService{}, false
	}

	// 4. Classify image
	svcType := ClassifyImage(ctr.Image)

	// 5. Resolve container hostname
	host := resolveContainerHost(inspect, d.cfg.Network)

	// 6. Extract credentials
	var envVars []string
	if inspect.Config != nil {
		envVars = inspect.Config.Env
	}
	connCfg := ExtractCredentials(svcType, envVars, host, name)

	// 7. Apply label overrides
	svcType, connCfg = ApplyLabelOverrides(labels, svcType, connCfg)

	// Resolve IP address for metadata
	ip := resolveContainerIP(inspect, d.cfg.Network)

	return DiscoveredService{
		Name:          name,
		Type:          svcType,
		Config:        connCfg,
		IPAddress:     ip,
		ContainerID:   ctr.ID,
		Project:       labels[labelComposeProject],
		Service:       labels[labelComposeService],
		ContainerName: containerName(ctr.Names),

		ConfigNameFromLabel: nameFromLabel(labels),
	}, true
}

// disambiguateNames gives every service a unique Name, since the registry
// keys adapters by name and a duplicate silently replaces the first one.
// Unique names are left alone, so a host running a single Compose project
// keeps its bare service names (and its YAML overrides keep matching).
// A Compose service whose name collides is qualified with its project as
// "<project>.<service>"; Compose project names can't contain a dot, so the
// result can't be mistaken for another project's. Anything still colliding
// (replicas of a scaled service) falls back to the container name, which
// the daemon keeps unique. A fallback can itself land on another service's
// name, so that step repeats until nothing changes; each service moves to
// its container name at most once, so it terminates. The result depends
// only on the set of containers, not on list order, so refresh and prune
// stay stable.
func disambiguateNames(svcs []DiscoveredService) {
	counts := countNames(svcs)
	for i := range svcs {
		if counts[svcs[i].Name] > 1 && svcs[i].Project != "" && svcs[i].Service != "" {
			rename(&svcs[i], svcs[i].Project+"."+svcs[i].Service)
		}
	}

	for changed := true; changed; {
		changed = false
		counts = countNames(svcs)
		for i := range svcs {
			if counts[svcs[i].Name] > 1 && svcs[i].ContainerName != "" && svcs[i].Name != svcs[i].ContainerName {
				rename(&svcs[i], svcs[i].ContainerName)
				changed = true
			}
		}
	}
}

// rename sets the service's Name and keeps a generated config "name" (the
// HTTP adapter's node ID) in step with it. A graphgo.name label override is
// left as is.
func rename(svc *DiscoveredService, name string) {
	if _, ok := svc.Config["name"].(string); ok && !svc.ConfigNameFromLabel {
		svc.Config["name"] = name
	}
	svc.Name = name
}

func countNames(svcs []DiscoveredService) map[string]int {
	counts := make(map[string]int, len(svcs))
	for _, s := range svcs {
		counts[s.Name]++
	}
	return counts
}

// deriveContainerName prefers the Docker Compose service label, then
// falls back to the container name (stripped of leading /).
func deriveContainerName(labels map[string]string, names []string) string {
	if svc, ok := labels[labelComposeService]; ok && svc != "" {
		return svc
	}

	if name := containerName(names); name != "" {
		return name
	}

	return "unknown"
}

// containerName returns the container's primary name without the leading /.
func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

// resolveContainerHost returns the hostname for connecting to the container.
// Prefers the Compose service name, then IP address on the specified network,
// then the first available network IP.
func resolveContainerHost(inspect container.InspectResponse, network string) string {
	// Prefer Compose service name as hostname (Docker DNS)
	if inspect.Config != nil && inspect.Config.Labels != nil {
		if svc, ok := inspect.Config.Labels[labelComposeService]; ok && svc != "" {
			return svc
		}
	}

	return resolveContainerIP(inspect, network)
}

// resolveContainerIP returns the container's IP address, preferring the
// specified network if set.
func resolveContainerIP(inspect container.InspectResponse, network string) string {
	if inspect.NetworkSettings == nil || inspect.NetworkSettings.Networks == nil {
		return "localhost"
	}

	// If a specific network is requested, look for it
	if network != "" {
		for netName, netSettings := range inspect.NetworkSettings.Networks {
			if netName == network && netSettings.IPAddress.IsValid() {
				return netSettings.IPAddress.String()
			}
		}
	}

	// Fall back to first available network
	for _, netSettings := range inspect.NetworkSettings.Networks {
		if netSettings.IPAddress.IsValid() {
			return netSettings.IPAddress.String()
		}
	}

	return "localhost"
}

// truncateID safely truncates a container ID to 12 characters.
func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// isIgnoredImage checks if the image matches any of the ignore patterns.
func isIgnoredImage(image string, ignoreList []string) bool {
	normalized := strings.ToLower(image)
	for _, pattern := range ignoreList {
		if strings.Contains(normalized, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// dockerHostFromSocket turns a configured socket path into a DOCKER_HOST
// URL: values that already carry a scheme pass through, Windows named pipes
// become npipe://, and anything else is treated as a unix socket path.
func dockerHostFromSocket(socket, goos string) string {
	if strings.Contains(socket, "://") {
		return socket
	}
	if goos == "windows" && strings.HasPrefix(socket, `\\`) {
		return "npipe://" + strings.ReplaceAll(socket, `\`, "/")
	}
	return "unix://" + socket
}

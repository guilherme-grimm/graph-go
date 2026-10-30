package server

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/config"
	"github.com/guilherme-grimm/graph-go/internal/discovery"
	"github.com/guilherme-grimm/graph-go/internal/discovery/docker"
	"github.com/guilherme-grimm/graph-go/internal/discovery/kubernetes"
)

// BuildRegistry constructs the adapter registry and the list of active
// discoverers based on cfg. It runs one Discover() sweep, applies YAML +
// auto-discovered services to the registry, and returns a cleanup func
// that closes discoverers and the registry in the correct order.
//
// cat supplies the adapters this binary supports; build it at the composition
// root (see internal/adapters/catalog). An empty Catalog is legal and yields a
// registry with no adapters — every service is logged and skipped.
//
// It does NOT start Watch() loops — that's NewServer's job.
func BuildRegistry(cfg *config.Config, logger *zap.SugaredLogger, cat adapters.Catalog) (adapters.Registry, []discovery.Discoverer, func()) {
	reg, discoverers, _, _, cleanup := buildRegistry(cfg, logger, cat)
	return reg, discoverers, cleanup
}

// buildRegistry is BuildRegistry plus the serviceSync that later discoverer
// refreshes must reuse, so they can tell unchanged services from removed ones,
// and the first sync's result for the startup report.
func buildRegistry(cfg *config.Config, logger *zap.SugaredLogger, cat adapters.Catalog) (adapters.Registry, []discovery.Discoverer, *serviceSync, syncResult, func()) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	reg := adapters.NewRegistry(logger)

	var discoverers []discovery.Discoverer
	if dd := buildDockerDiscovery(cfg, logger); dd != nil {
		discoverers = append(discoverers, dd)
	}
	if kd := buildKubernetesDiscovery(cfg, logger); kd != nil {
		discoverers = append(discoverers, kd)
	}

	services := discoverAll(discoverers, logger)

	yamlEntries := make([]discovery.YAMLEntry, len(cfg.Connections))
	for i, entry := range cfg.Connections {
		yamlEntries[i] = discovery.YAMLEntry{
			Name:   entry.Name,
			Type:   entry.Type,
			Config: entry.ToConnectionConfig(),
		}
	}
	svcSync := newServiceSync(reg, cat, yamlEntries, logger)
	initial := svcSync.apply(services, "")

	cleanup := func() {
		for _, d := range discoverers {
			if err := d.Close(); err != nil {
				logger.Warnw("error closing discoverer", "discoverer", d.Name(), "err", err)
			}
		}
		if err := reg.CloseAll(); err != nil {
			logger.Warnw("error closing adapters", "err", err)
		}
	}

	return reg, discoverers, svcSync, initial, cleanup
}

// discoverAll runs Discover() on every discoverer in parallel and returns
// the concatenated ServiceInfo list. A failure from one discoverer is
// logged but does not block the others.
func discoverAll(discoverers []discovery.Discoverer, logger *zap.SugaredLogger) []discovery.ServiceInfo {
	if len(discoverers) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := make([][]discovery.ServiceInfo, len(discoverers))
	var wg sync.WaitGroup
	for i, d := range discoverers {
		i, d := i, d
		wg.Add(1)
		go func() {
			defer wg.Done()
			discovered, err := d.Discover(ctx)
			if err != nil {
				logger.Warnw("discovery failed", "discoverer", d.Name(), "err", err)
				return
			}
			logger.Infow("discovery complete", "discoverer", d.Name(), "services", len(discovered))
			results[i] = discovered
		}()
	}
	wg.Wait()

	var all []discovery.ServiceInfo
	for _, r := range results {
		all = append(all, r...)
	}
	return all
}

// dockerDiscoveryConfig maps config onto the discoverer. An unset socket stays
// empty so the Docker client honours DOCKER_HOST (e.g. a read-only socket
// proxy) before falling back to the platform default socket.
func dockerDiscoveryConfig(cfg *config.Config) docker.DockerDiscoveryConfig {
	return docker.DockerDiscoveryConfig{
		Socket:       cfg.Docker.Socket,
		Network:      cfg.Docker.Network,
		IgnoreImages: cfg.Docker.IgnoreImages,
	}
}

func buildDockerDiscovery(cfg *config.Config, logger *zap.SugaredLogger) *docker.DockerDiscovery {
	if !shouldEnableDocker(cfg) {
		return nil
	}

	dd, err := docker.NewDockerDiscovery(dockerDiscoveryConfig(cfg), logger)
	if err != nil {
		// Only used to word the permission hint; with no socket configured the
		// client connects via DOCKER_HOST or the platform default socket.
		socket := cfg.Docker.Socket
		if socket == "" {
			socket = "/var/run/docker.sock"
		}
		if hint := docker.SocketAccessHint(socket, err); hint != "" {
			logger.Warnw("docker discovery unavailable: permission denied on Docker socket, falling back to YAML-only", "socket", socket, "fix", hint)
			return nil
		}
		logger.Warnw("docker discovery unavailable, falling back to YAML-only", "err", err)
		return nil
	}
	logger.Infow("docker discovery enabled")
	return dd
}

func buildKubernetesDiscovery(cfg *config.Config, logger *zap.SugaredLogger) *kubernetes.Discovery {
	if cfg.Kubernetes.Enabled != nil && !*cfg.Kubernetes.Enabled {
		return nil
	}

	k8sCfg := kubernetes.Config{
		KubeconfigPath: cfg.Kubernetes.Kubeconfig,
		Context:        cfg.Kubernetes.Context,
		Namespaces:     cfg.Kubernetes.Namespaces,
	}

	kd, err := kubernetes.New(k8sCfg)
	if err != nil {
		logger.Warnw("kubernetes discovery unavailable", "err", err)
		return nil
	}
	if kd == nil {
		return nil
	}
	logger.Infow("kubernetes discovery enabled")
	return kd
}

// shouldEnableDocker determines if Docker discovery should be attempted.
// If cfg.Docker.Enabled is explicitly set, use that. Otherwise auto-detect
// by checking if the Docker socket exists.
func shouldEnableDocker(cfg *config.Config) bool {
	return shouldEnableDockerForOS(cfg, runtime.GOOS, os.Stat, os.Getenv)
}

func shouldEnableDockerForOS(cfg *config.Config, goos string, stat func(string) (os.FileInfo, error), getenv func(string) string) bool {
	if cfg.Docker.Enabled != nil {
		return *cfg.Docker.Enabled
	}

	// If DOCKER_HOST is set, let the Docker SDK try that host. This keeps
	// auto-detection aligned with Docker CLI/SDK behavior.
	if getenv("DOCKER_HOST") != "" {
		return true
	}

	// Windows Docker Desktop uses a named pipe rather than a Unix socket. Named
	// pipes are not reliably discoverable with os.Stat, so attempt discovery and
	// let the existing Docker client ping path warn-and-skip if Docker is absent.
	if goos == "windows" {
		return true
	}

	socket := "/var/run/docker.sock"
	if cfg.Docker.Socket != "" {
		socket = cfg.Docker.Socket
	}

	_, err := stat(socket)
	return err == nil
}

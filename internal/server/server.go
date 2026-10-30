package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/config"
	"github.com/guilherme-grimm/graph-go/internal/discovery"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

type Server struct {
	port           int
	allowedOrigins []string
	registry       adapters.Registry
	logger         *zap.SugaredLogger
}

// NewServer returns the HTTP server and a cleanup function that should
// be called during graceful shutdown to close adapter connections.
//
// cat supplies the adapters this binary supports; build it at the composition
// root (see internal/adapters/catalog).
//
// When report is non-nil, a per-adapter ✓/✗ summary is written to it once the
// first full discovery finishes. That runs in the background so a slow
// adapter does not delay the listener; it also warms the graph cache.
func NewServer(cfg *config.Config, logger *zap.SugaredLogger, cat adapters.Catalog, report io.Writer) (*http.Server, func()) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	allowedOrigins := cfg.Server.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://localhost:3000", "http://localhost:5173"}
	}

	reg, discoverers, svcSync, initial, regCleanup := buildRegistry(cfg, logger, cat)

	if report != nil {
		go func() {
			g, err := reg.DiscoverAll()
			if err != nil {
				logger.Warnw("startup report skipped: discovery failed", "err", err)
				return
			}
			if err := writeStartupReport(report, startupReport(initial, g)); err != nil {
				logger.Warnw("startup report write failed", "err", err)
			}
		}()
	}

	watchCancel := startWatchers(discoverers, svcSync, logger)

	s := &Server{
		port:           port,
		allowedOrigins: allowedOrigins,
		registry:       reg,
		logger:         logger,
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	cleanup := func() {
		watchCancel()
		regCleanup()
	}

	return server, cleanup
}

// StartRegistry builds the registry like BuildRegistry and also starts the
// discoverers' Watch loops, so the registry stays current for as long as the
// process runs. It is what a long-lived consumer other than the HTTP server
// (the MCP server) uses. The returned func stops the watchers and closes
// everything.
func StartRegistry(cfg *config.Config, logger *zap.SugaredLogger, cat adapters.Catalog) (adapters.Registry, func()) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	reg, discoverers, svcSync, _, regCleanup := buildRegistry(cfg, logger, cat)
	watchCancel := startWatchers(discoverers, svcSync, logger)
	return reg, func() {
		watchCancel()
		regCleanup()
	}
}

// startWatchers starts a Watch() goroutine for each discoverer and returns
// the func that stops them. Topology-oriented discoverers refresh their
// topology on each callback; adapter-oriented ones re-sync their services.
func startWatchers(discoverers []discovery.Discoverer, svcSync *serviceSync, logger *zap.SugaredLogger) context.CancelFunc {
	watchCtx, watchCancel := context.WithCancel(context.Background())
	for _, d := range discoverers {
		d := d
		onChange := buildOnChange(watchCtx, d, svcSync, logger)
		go func() {
			if err := d.Watch(watchCtx, onChange); err != nil {
				logger.Warnw("discoverer watch stopped", "discoverer", d.Name(), "err", err)
			}
		}()
		logger.Infow("discoverer watch started", "discoverer", d.Name())
	}
	return watchCancel
}

// buildOnChange returns the callback invoked on discoverer events. For
// discoverers that produce topology ServiceInfo, it re-reads their snapshot
// and replaces the registry topology before invalidating caches. For adapter
// discoverers, it syncs the fresh service list: new or changed services are
// (re)registered and ones that disappeared are pruned.
func buildOnChange(ctx context.Context, d discovery.Discoverer, svcSync *serviceSync, logger *zap.SugaredLogger) func() {
	return func() {
		fresh, err := d.Discover(ctx)
		if err != nil {
			logger.Warnw("discoverer refresh failed", "discoverer", d.Name(), "err", err)
			svcSync.reg.InvalidateCache()
			return
		}
		var hasTopology bool
		var topoNodes []nodes.Node
		var topoEdges []edges.Edge
		for _, svc := range fresh {
			if len(svc.Nodes) > 0 || len(svc.Edges) > 0 {
				hasTopology = true
				topoNodes = append(topoNodes, svc.Nodes...)
				topoEdges = append(topoEdges, svc.Edges...)
			}
		}
		if hasTopology {
			svcSync.reg.SetTopology(d.Name(), topoNodes, topoEdges)
			return
		}
		svcSync.apply(fresh, d.Name())
	}
}

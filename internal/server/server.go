package server

import (
	"context"
	"fmt"
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
func NewServer(cfg *config.Config, logger *zap.SugaredLogger, cat adapters.Catalog) (*http.Server, func()) {
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

	reg, discoverers, regCleanup := BuildRegistry(cfg, logger, cat)

	// Start a Watch() goroutine for each discoverer. Topology-oriented
	// discoverers refresh their topology on each callback; adapter-oriented
	// ones just invalidate the cache so next DiscoverAll re-queries adapters.
	watchCtx, watchCancel := context.WithCancel(context.Background())
	for _, d := range discoverers {
		d := d
		onChange := buildOnChange(watchCtx, d, reg, cat, logger)
		go func() {
			if err := d.Watch(watchCtx, onChange); err != nil {
				logger.Warnw("discoverer watch stopped", "discoverer", d.Name(), "err", err)
			}
		}()
		logger.Infow("discoverer watch started", "discoverer", d.Name())
	}

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

// buildOnChange returns the callback invoked on discoverer events. For
// discoverers that produce topology ServiceInfo, it re-reads their snapshot
// and replaces the registry topology before invalidating caches. For pure
// adapter discoverers, it just invalidates the cache.
func buildOnChange(ctx context.Context, d discovery.Discoverer, reg adapters.Registry, cat adapters.Catalog, logger *zap.SugaredLogger) func() {
	return func() {
		fresh, err := d.Discover(ctx)
		if err != nil {
			logger.Warnw("discoverer refresh failed", "discoverer", d.Name(), "err", err)
			reg.InvalidateCache()
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
			reg.SetTopology(d.Name(), topoNodes, topoEdges)
			return
		}
		applyServices(reg, cat, fresh, logger)
	}
}

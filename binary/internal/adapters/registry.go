package adapters

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"binary/internal/graph"
	"binary/internal/graph/edges"
	"binary/internal/graph/health"
	"binary/internal/graph/nodes"
)

const defaultCacheTTL = 30 * time.Second

type Registry interface {
	Register(name string, connType string, adapter Adapter, config ConnectionConfig) error
	Get(name string) (Adapter, bool)
	Names() []string
	DiscoverAll() (*graph.Graph, error)
	HealthAll() []health.HealthMetrics
	CloseAll() error
}

type registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	config   map[string]ConnectionConfig
	types    map[string]string

	cacheMu     sync.RWMutex
	cachedGraph *graph.Graph
	cacheTime   time.Time
	cacheTTL    time.Duration

	sf singleflight.Group
}

func NewRegistry() Registry {
	return &registry{
		adapters: make(map[string]Adapter),
		config:   make(map[string]ConnectionConfig),
		types:    make(map[string]string),
		cacheTTL: defaultCacheTTL,
	}
}

func (r *registry) Register(name string, connType string, adapter Adapter, config ConnectionConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := adapter.Connect(config); err != nil {
		return fmt.Errorf("registry: failed to connect adapter %q: %w", name, err)
	}

	r.adapters[name] = adapter
	r.config[name] = config
	r.types[name] = connType

	r.invalidateCache()
	return nil
}

func (r *registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.adapters[name]
	return a, ok
}

func (r *registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

func (r *registry) DiscoverAll() (*graph.Graph, error) {
	// Check cache first
	r.cacheMu.RLock()
	if r.cachedGraph != nil && time.Since(r.cacheTime) < r.cacheTTL {
		g := r.cachedGraph
		r.cacheMu.RUnlock()
		return g, nil
	}
	r.cacheMu.RUnlock()

	// Use singleflight to prevent cache stampede
	v, err, _ := r.sf.Do("discover", func() (any, error) {
		// Double-check cache inside singleflight callback
		r.cacheMu.RLock()
		if r.cachedGraph != nil && time.Since(r.cacheTime) < r.cacheTTL {
			g := r.cachedGraph
			r.cacheMu.RUnlock()
			return g, nil
		}
		r.cacheMu.RUnlock()

		r.mu.RLock()
		defer r.mu.RUnlock()

		allNodes := make([]nodes.Node, 0)
		allEdges := make([]edges.Edge, 0)

		for name, adapter := range r.adapters {
			connType := r.types[name]

			n, e, err := adapter.Discover()
			if err != nil {
				return nil, fmt.Errorf("registry: discover failed for %q: %w", name, err)
			}

			if connType == "http" {
				// HTTP adapters manage their own top-level node directly
				allNodes = append(allNodes, n...)
				allEdges = append(allEdges, e...)
				continue
			}

			// Create a service-level parent node from config
			serviceID := fmt.Sprintf("service-%s", name)
			allNodes = append(allNodes, nodes.Node{
				Id:       serviceID,
				Type:     connType,
				Name:     name,
				Metadata: map[string]any{"adapter": name},
				Health:   "healthy",
			})

			// Re-parent top-level nodes under the service node
			for i := range n {
				if n[i].Parent == "" {
					n[i].Parent = serviceID
					allEdges = append(allEdges, edges.Edge{
						Id:     fmt.Sprintf("service-contains-%s-%s", name, n[i].Id),
						Source: serviceID,
						Target: n[i].Id,
						Type:   "contains",
						Label:  "contains",
					})
				}
			}

			allNodes = append(allNodes, n...)
			allEdges = append(allEdges, e...)
		}

		g := &graph.Graph{Nodes: allNodes, Edges: allEdges}

		// Store in cache
		r.cacheMu.Lock()
		r.cachedGraph = g
		r.cacheTime = time.Now()
		r.cacheMu.Unlock()

		return g, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*graph.Graph), nil
}

func (r *registry) HealthAll() []health.HealthMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := make([]health.HealthMetrics, 0, len(r.adapters))
	for name, adapter := range r.adapters {
		m, err := adapter.Health()
		status := health.Healthy
		if err != nil {
			status = health.Unhealthy
		} else if s, ok := m["status"].(string); ok && s != "healthy" {
			status = health.Status(s)
		}

		metrics = append(metrics, health.HealthMetrics{
			NodeID:    name,
			Status:    status,
			Metrics:   m,
			Timestamp: time.Now(),
		})
	}
	return metrics
}

func (r *registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for name, adapter := range r.adapters {
		if err := adapter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("registry: close errors: %v", errs)
	}
	return nil
}

func (r *registry) invalidateCache() {
	r.cacheMu.Lock()
	r.cachedGraph = nil
	r.cacheTime = time.Time{}
	r.cacheMu.Unlock()
}

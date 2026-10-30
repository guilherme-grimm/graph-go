// Package mcp serves graph-go's topology to agents over the Model Context
// Protocol. See docs/adr/0003-mcp-server.md for the design: a warm,
// long-lived registry behind three read-only tools, with summaries instead
// of full dumps on large systems.
package mcp

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/health"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/server"
)

// healthInterval matches the WebSocket health tick.
const healthInterval = 5 * time.Second

// Starter builds the registry the tools read from and returns the func that
// shuts it down. server.StartRegistry is the production Starter; tests pass
// one that returns a registry holding fixture topology.
type Starter func() (adapters.Registry, func())

// Backend lazily starts the registry on the first tool call, so a client that
// launches the server but never uses it pays nothing, and keeps health fresh
// with a background sweep.
type Backend struct {
	start  Starter
	logger *zap.SugaredLogger

	once     sync.Once
	reg      adapters.Registry
	stopReg  func()
	stopLoop chan struct{}

	mu         sync.RWMutex
	metrics    []health.HealthMetrics
	observedAt time.Time
}

// NewBackend returns a Backend that calls start on first use.
func NewBackend(start Starter, logger *zap.SugaredLogger) *Backend {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &Backend{start: start, logger: logger, stopLoop: make(chan struct{})}
}

func (b *Backend) init() {
	b.once.Do(func() {
		b.reg, b.stopReg = b.start()
		b.sweep()
		go b.loop()
	})
}

func (b *Backend) sweep() {
	m := b.reg.HealthAll()
	b.mu.Lock()
	b.metrics, b.observedAt = m, time.Now().UTC()
	b.mu.Unlock()
}

func (b *Backend) loop() {
	t := time.NewTicker(healthInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			b.sweep()
		case <-b.stopLoop:
			return
		}
	}
}

// Snapshot returns the current graph with every node's health resolved, and
// the time of the health sweep it reflects. Node metadata is already
// redacted by the registry.
//
// A cancelled ctx stops the request from starting shared work. It does not
// interrupt a registry start or a discovery already running: other requests
// share those, and the registry and adapters take no context.
func (b *Backend) Snapshot(ctx context.Context) (*graph.Graph, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	b.init()
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	g, err := b.reg.DiscoverAll()
	if err != nil {
		return nil, time.Time{}, err
	}

	b.mu.RLock()
	metrics, observedAt := b.metrics, b.observedAt
	b.mu.RUnlock()

	// DiscoverAll hands out a copy of the node slice; give it its own backing
	// array anyway before writing health into it.
	resolved := server.NodeHealths(g, metrics)
	ns := make([]nodes.Node, len(g.Nodes))
	copy(ns, g.Nodes)
	for i := range ns {
		if h, ok := resolved[ns[i].Id]; ok {
			ns[i].Health = h
		}
	}
	return &graph.Graph{Nodes: ns, Edges: g.Edges}, observedAt, nil
}

// Close stops the health loop and the registry, if they were started.
func (b *Backend) Close() {
	// Waits for an in-flight init, and stops a later one from starting the
	// registry after shutdown.
	b.once.Do(func() {})
	if b.reg == nil {
		return
	}
	close(b.stopLoop)
	b.stopReg()
}

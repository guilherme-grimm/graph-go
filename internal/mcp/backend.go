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

// Backend owns the registry the tools read from. StartBackend builds it in the
// background, so the stdio handshake never waits on discovery, and keeps
// health fresh with a periodic sweep.
type Backend struct {
	logger *zap.SugaredLogger

	// ready is closed once the registry is built and has one health sweep;
	// reg and stopReg are set before that and never change after.
	ready   chan struct{}
	reg     adapters.Registry
	stopReg func()

	stopLoop chan struct{} // closed by Close
	loopDone chan struct{} // closed when the background goroutine returns

	mu         sync.RWMutex
	metrics    []health.HealthMetrics
	observedAt time.Time
}

// StartBackend starts building the registry with start and returns at once.
// Tools wait for it (or for their request to be cancelled) in Snapshot.
func StartBackend(start Starter, logger *zap.SugaredLogger) *Backend {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	b := &Backend{
		logger:   logger,
		ready:    make(chan struct{}),
		stopLoop: make(chan struct{}),
		loopDone: make(chan struct{}),
	}
	go b.run(start)
	return b
}

func (b *Backend) run(start Starter) {
	defer close(b.loopDone)
	b.reg, b.stopReg = start()
	b.sweep()
	close(b.ready)

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

func (b *Backend) sweep() {
	m := b.reg.HealthAll()
	b.mu.Lock()
	b.metrics, b.observedAt = m, time.Now().UTC()
	b.mu.Unlock()
}

// Snapshot returns the current graph with every node's health resolved, and
// the time of the health sweep it reflects. Node metadata is already
// redacted by the registry.
//
// It waits for the registry to be built unless ctx ends first. A cancelled
// ctx stops this request; it does not interrupt the registry start or a
// discovery already running, which other requests share and which take no
// context.
func (b *Backend) Snapshot(ctx context.Context) (*graph.Graph, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	select {
	case <-b.ready:
	case <-ctx.Done():
		return nil, time.Time{}, ctx.Err()
	}
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

// Close stops the health sweep and shuts the registry down. If the registry
// is still being built it waits for that to finish first, so nothing is
// left running.
func (b *Backend) Close() {
	close(b.stopLoop)
	<-b.loopDone
	b.stopReg()
}

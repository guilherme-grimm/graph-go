// Package adapters defines the Adapter interface, the Catalog of adapter
// constructors, and the Registry that holds live adapter instances.
//
// When adding a new adapter, you MUST also add an integration test using the
// adaptertest.RunContractTests suite. See adaptertest package for instructions.
package adapters

import (
	"fmt"
	"maps"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

type ConnectionConfig map[string]any
type HealthMetrics map[string]any

// Adapter defines the contract that all service adapters must satisfy.
type Adapter interface {
	// Connect establishes a connection using the provided configuration.
	Connect(config ConnectionConfig) error

	// Discover performs recursive discovery (BFS) returning nodes and edges.
	Discover() ([]nodes.Node, []edges.Edge, error)

	// Health returns health metrics. By convention, adapters include a "status"
	// key ("healthy", "degraded", "unhealthy") in the returned map and return
	// nil error. The error return is reserved for cases where health cannot be
	// determined at all (e.g., adapter not initialized).
	Health() (HealthMetrics, error)

	// Close releases resources upon shutting down the service.
	Close() error
}

// AdapterConstructor is a factory function that creates a new Adapter instance.
// The logger is always non-nil; adapters may ignore it or name subloggers off it.
type AdapterConstructor func(logger *zap.SugaredLogger) Adapter

// Catalog is the set of adapter constructors a binary knows how to build,
// keyed by connection type. It is immutable: build one with NewCatalog at the
// composition root and pass it down. Because it is never mutated after
// construction it needs no locking.
//
// Catalog is deliberately not user-facing — it is decided at compile time, not
// from config. See ADR-0002.
type Catalog struct {
	ctors map[string]AdapterConstructor
}

// NewCatalog returns a Catalog over a copy of ctors. A nil map yields an empty
// Catalog, for which every New call reports an unknown adapter type.
func NewCatalog(ctors map[string]AdapterConstructor) Catalog {
	owned := make(map[string]AdapterConstructor, len(ctors))
	maps.Copy(owned, ctors)
	return Catalog{ctors: owned}
}

// New creates a new adapter instance for the given connection type.
func (c Catalog) New(connType string, logger *zap.SugaredLogger) (Adapter, error) {
	ctor, ok := c.ctors[connType]
	if !ok {
		return nil, fmt.Errorf("unknown adapter type %q", connType)
	}
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return ctor(logger), nil
}

package adapters_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// leakyAdapter puts credentials everywhere an adapter can: node metadata,
// health metrics and (when told to fail) the Discover error.
type leakyAdapter struct {
	discoverErr error
}

func (a *leakyAdapter) Connect(adapters.ConnectionConfig) error { return nil }
func (a *leakyAdapter) Discover() ([]nodes.Node, []edges.Edge, error) {
	if a.discoverErr != nil {
		return nil, nil, a.discoverErr
	}
	return []nodes.Node{{
		Id:       "api",
		Type:     "api",
		Name:     "api",
		Metadata: map[string]any{"endpoint": "http://admin:hunter2@api:8080", "password": "hunter2"},
	}}, nil, nil
}
func (a *leakyAdapter) Health() (adapters.HealthMetrics, error) {
	return adapters.HealthMetrics{"status": "healthy", "dsn": "postgres://app:hunter2@db/orders"}, nil
}
func (a *leakyAdapter) Close() error { return nil }

// Nothing that leaves the registry may contain the password, whichever
// path it took.
func TestRegistryRedactsEveryOutput(t *testing.T) {
	reg := adapters.NewRegistry(zap.NewNop().Sugar())
	require.NoError(t, reg.Register("api", "http", &leakyAdapter{}, nil))
	require.NoError(t, reg.Register("orders", "postgres", &leakyAdapter{
		discoverErr: errors.New("connect postgres://app:hunter2@db:5432/orders: refused"),
	}, nil))
	reg.SetTopology("kubernetes", []nodes.Node{{
		Id:       "k8s:pod:shop/api",
		Type:     "pod",
		Name:     "api",
		Metadata: map[string]any{"labels": map[string]string{"token": "hunter2"}, "env": "DB_PASSWORD=hunter2"},
	}}, nil)

	g, err := reg.DiscoverAll()
	require.NoError(t, err)
	graphJSON, err := json.Marshal(g)
	require.NoError(t, err)
	healthJSON, err := json.Marshal(reg.HealthAll())
	require.NoError(t, err)

	for name, out := range map[string]string{"graph": string(graphJSON), "health": string(healthJSON)} {
		assert.NotContains(t, out, "hunter2", "%s output leaks the password:\n%s", name, out)
	}
	// Masking keeps what is useful: hosts and users stay readable.
	assert.Contains(t, string(graphJSON), "http://admin:***@api:8080")
	assert.Contains(t, string(graphJSON), "postgres://app:***@db:5432/orders")
	assert.Contains(t, string(healthJSON), "postgres://app:***@db/orders")
}

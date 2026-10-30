package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/mcp"
)

// ordersDB is a registered adapter whose health the backend must merge onto
// its nodes, and whose metadata carries a credential the registry must mask.
type ordersDB struct{}

func (ordersDB) Connect(adapters.ConnectionConfig) error { return nil }
func (ordersDB) Discover() ([]nodes.Node, []edges.Edge, error) {
	return []nodes.Node{{
		Id: "orders-db", Type: "database", Name: "orders",
		Metadata: map[string]any{"endpoint": "postgres://app:hunter2@db:5432/orders"},
	}}, nil, nil
}
func (ordersDB) Health() (adapters.HealthMetrics, error) {
	return adapters.HealthMetrics{"status": "degraded"}, nil
}
func (ordersDB) Close() error { return nil }

// shop is a small Kubernetes namespace: a service routing to a pod that
// depends on the orders database.
func shop(reg adapters.Registry) {
	ns := func(id, typ, name, health string) nodes.Node {
		return nodes.Node{Id: id, Type: typ, Name: name, Health: health, Metadata: map[string]any{"namespace": "shop"}}
	}
	reg.SetTopology("kubernetes", []nodes.Node{
		ns("k8s:service:shop/api", "k8s_service", "api", "healthy"),
		ns("k8s:pod:shop/api-1", "pod", "api-1", "unhealthy"),
		ns("k8s:pod:shop/web-1", "pod", "web-1", "healthy"),
	}, []edges.Edge{
		{Id: "e1", Source: "k8s:service:shop/api", Target: "k8s:pod:shop/api-1", Type: edges.TypeRoutesTo},
		{Id: "e2", Source: "k8s:pod:shop/api-1", Target: "service-orders", Type: "depends_on"},
	})
}

func smallStarter(t *testing.T, calls *atomic.Int32) mcp.Starter {
	return func() (adapters.Registry, func()) {
		calls.Add(1)
		reg := adapters.NewRegistry(zap.NewNop().Sugar())
		require.NoError(t, reg.Register("orders", "postgres", ordersDB{}, nil))
		shop(reg)
		return reg, func() {}
	}
}

// bigStarter builds a cluster above the summary threshold: n healthy pods
// spread over three namespaces, plus one unhealthy pod.
func bigStarter(n int) mcp.Starter {
	return func() (adapters.Registry, func()) {
		reg := adapters.NewRegistry(zap.NewNop().Sugar())
		var ns []nodes.Node
		for i := range n {
			space := []string{"alpha", "beta", "gamma"}[i%3]
			ns = append(ns, nodes.Node{
				Id: fmt.Sprintf("k8s:pod:%s/p-%04d", space, i), Type: "pod", Name: fmt.Sprintf("p-%04d", i),
				Health: "healthy", Metadata: map[string]any{"namespace": space, "padding": strings.Repeat("x", 200)},
			})
		}
		ns = append(ns, nodes.Node{Id: "k8s:pod:beta/broken", Type: "pod", Name: "broken", Health: "unhealthy",
			Metadata: map[string]any{"namespace": "beta"}})
		reg.SetTopology("kubernetes", ns, nil)
		return reg, func() {}
	}
}

func connect(t *testing.T, start mcp.Starter, opts mcp.Options) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	b := mcp.NewBackend(start, nil)
	t.Cleanup(b.Close)

	ct, st := sdk.NewInMemoryTransports()
	ss, err := mcp.NewServer(b, opts).Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured output, returning the raw
// JSON too so tests can check what the client actually receives.
func call[T any](t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (T, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", raw)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out, string(raw)
}

func TestToolsAreExactlyTheReadOnlySet(t *testing.T) {
	var calls atomic.Int32
	cs := connect(t, smallStarter(t, &calls), mcp.Options{})

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		require.NotNil(t, tool.Annotations, tool.Name)
		assert.True(t, tool.Annotations.ReadOnlyHint, "%s must be marked read-only", tool.Name)
		assert.NotEmpty(t, tool.Description, tool.Name)
	}
	assert.ElementsMatch(t, mcp.ToolNames, names, "adding a tool is a deliberate change: update ToolNames and ADR-0003")
	assert.Zero(t, calls.Load(), "listing tools must not start discovery")
}

func TestGetTopologySmallGraphIsFull(t *testing.T) {
	var calls atomic.Int32
	cs := connect(t, smallStarter(t, &calls), mcp.Options{})

	out, raw := call[mcp.TopologyOutput](t, cs, "get_topology", nil)
	assert.Equal(t, int32(1), calls.Load(), "first call starts discovery")
	assert.False(t, out.Truncated)
	assert.Nil(t, out.Summary)
	assert.Equal(t, out.Total.Nodes, len(out.Nodes))
	assert.False(t, out.ObservedAt.IsZero())

	health := map[string]string{}
	for _, n := range out.Nodes {
		health[n.Id] = n.Health
	}
	assert.Equal(t, "degraded", health["service-orders"], "adapter health is merged onto its nodes")
	assert.Equal(t, "unhealthy", health["k8s:pod:shop/api-1"], "topology nodes keep their own health")

	assert.NotContains(t, raw, "hunter2", "credentials are masked before they reach the agent")
	assert.Contains(t, raw, "postgres://app:***@db:5432/orders")

	_, _ = call[mcp.TopologyOutput](t, cs, "get_topology", nil)
	assert.Equal(t, int32(1), calls.Load(), "the registry stays warm between calls")
}

func TestGetTopologyLargeGraphSummarizes(t *testing.T) {
	cs := connect(t, bigStarter(600), mcp.Options{})

	out, _ := call[mcp.TopologyOutput](t, cs, "get_topology", nil)
	assert.True(t, out.Truncated)
	require.NotNil(t, out.Summary)
	assert.Equal(t, 601, out.Matched)
	assert.Equal(t, 201, out.Summary.ByNamespace["beta"]["pod"])
	assert.Equal(t, 1, out.Summary.ByHealth["unhealthy"])
	require.Len(t, out.Nodes, 1, "a summary lists only unhealthy and degraded nodes")
	assert.Equal(t, "k8s:pod:beta/broken", out.Nodes[0].Id)
	assert.Contains(t, out.Hint, "beta (201)")

	t.Run("a narrow filter gets full detail", func(t *testing.T) {
		out, _ := call[mcp.TopologyOutput](t, cs, "get_topology", map[string]any{"namespaces": []string{"alpha"}, "name": "p-00"})
		assert.False(t, out.Truncated)
		assert.Nil(t, out.Summary)
		assert.NotEmpty(t, out.Nodes)
		for _, n := range out.Nodes {
			assert.Equal(t, "alpha", n.Metadata["namespace"])
		}
	})

	t.Run("health filter", func(t *testing.T) {
		out, _ := call[mcp.TopologyOutput](t, cs, "get_topology", map[string]any{"health": []string{"unhealthy"}})
		require.Len(t, out.Nodes, 1)
		assert.Equal(t, "broken", out.Nodes[0].Name)
	})

	t.Run("no match is an answer with a hint", func(t *testing.T) {
		out, _ := call[mcp.TopologyOutput](t, cs, "get_topology", map[string]any{"namespaces": []string{"nope"}})
		assert.Empty(t, out.Nodes)
		assert.NotEmpty(t, out.Hint)
	})
}

func TestByteCapKeepsTheMostImportantNodes(t *testing.T) {
	const capBytes = 8 << 10
	cs := connect(t, bigStarter(600), mcp.Options{MaxResponseBytes: capBytes})

	out, raw := call[mcp.TopologyOutput](t, cs, "get_topology", map[string]any{"detail": "full"})
	assert.True(t, out.Truncated)
	assert.LessOrEqual(t, len(raw), capBytes)
	require.NotEmpty(t, out.Nodes)
	assert.Equal(t, "k8s:pod:beta/broken", out.Nodes[0].Id, "unhealthy nodes survive the cap first")
	assert.Less(t, len(out.Nodes), 601)
	assert.Contains(t, out.Hint, "size cap")
}

func TestGetNode(t *testing.T) {
	var calls atomic.Int32
	cs := connect(t, smallStarter(t, &calls), mcp.Options{})

	out, _ := call[mcp.NodeOutput](t, cs, "get_node", map[string]any{"id": "k8s:pod:shop/api-1"})
	require.NotNil(t, out.Node)
	assert.Equal(t, "api-1", out.Node.Name)
	assert.Len(t, out.Edges, 2, "one edge in from the service, one out to the database")
	var ids []string
	for _, n := range out.Neighbors {
		ids = append(ids, n.Id)
	}
	assert.ElementsMatch(t, []string{"k8s:service:shop/api", "service-orders"}, ids)

	t.Run("unknown id is a hint, not an error", func(t *testing.T) {
		out, _ := call[mcp.NodeOutput](t, cs, "get_node", map[string]any{"id": "nope"})
		assert.Nil(t, out.Node)
		assert.Contains(t, out.Hint, "find_nodes")
	})
}

func TestFindNodes(t *testing.T) {
	cs := connect(t, bigStarter(600), mcp.Options{})

	out, _ := call[mcp.FindOutput](t, cs, "find_nodes", map[string]any{"query": "broken"})
	require.Len(t, out.Matches, 1)
	assert.Equal(t, mcp.Match{ID: "k8s:pod:beta/broken", Name: "broken", Type: "pod", Namespace: "beta", Health: "unhealthy"}, out.Matches[0])

	out, _ = call[mcp.FindOutput](t, cs, "find_nodes", map[string]any{"query": "p-01", "limit": 5})
	assert.Len(t, out.Matches, 5)
	assert.Equal(t, 100, out.Total, "p-0100..p-0199")
	assert.Equal(t, "p-0100", out.Matches[0].Name, "name prefix matches rank first, then by name")

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "find_nodes", Arguments: map[string]any{"query": " "}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "an empty query is a tool error")
}

func TestEmptyDiscoveryIsAnAnswer(t *testing.T) {
	cs := connect(t, func() (adapters.Registry, func()) {
		return adapters.NewRegistry(zap.NewNop().Sugar()), func() {}
	}, mcp.Options{})

	out, _ := call[mcp.TopologyOutput](t, cs, "get_topology", nil)
	assert.Empty(t, out.Nodes)
	assert.Contains(t, out.Hint, "Docker")
	assert.Contains(t, out.Hint, "stderr")
}

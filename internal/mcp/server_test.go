package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	b := mcp.StartBackend(start, nil)
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
	out, raw, _ := callSized[T](t, cs, name, args)
	return out, raw
}

// callSized also returns the size of the whole result as received: the
// structured content plus the text content the SDK fills from it.
func callSized[T any](t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (T, string, int) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", raw)
	whole, err := json.Marshal(res)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out, string(raw), len(whole)
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
}

func TestGetTopologySmallGraphIsFull(t *testing.T) {
	var calls atomic.Int32
	cs := connect(t, smallStarter(t, &calls), mcp.Options{})

	out, raw := call[mcp.TopologyOutput](t, cs, "get_topology", nil)
	assert.Equal(t, int32(1), calls.Load(), "the registry is built once, at start")
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

	out, _, whole := callSized[mcp.TopologyOutput](t, cs, "get_topology", map[string]any{"detail": "full"})
	assert.True(t, out.Truncated)
	assert.LessOrEqual(t, whole, capBytes, "the cap bounds the whole result, text content included")
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

func TestFindNodesRespectsTheByteCap(t *testing.T) {
	const capBytes = 4 << 10
	long := strings.Repeat("n", 400)
	cs := connect(t, func() (adapters.Registry, func()) {
		reg := adapters.NewRegistry(zap.NewNop().Sugar())
		var ns []nodes.Node
		for i := range 50 {
			ns = append(ns, nodes.Node{Id: fmt.Sprintf("svc-%02d-%s", i, long), Type: "service", Name: fmt.Sprintf("svc-%02d-%s", i, long)})
		}
		reg.SetTopology("kubernetes", ns, nil)
		return reg, func() {}
	}, mcp.Options{MaxResponseBytes: capBytes})

	out, _, whole := callSized[mcp.FindOutput](t, cs, "find_nodes", map[string]any{"query": "svc", "limit": 50})
	assert.LessOrEqual(t, whole, capBytes)
	assert.Equal(t, 50, out.Total)
	assert.NotEmpty(t, out.Matches)
	assert.Less(t, len(out.Matches), 50)
	assert.Equal(t, "svc-00-"+long, out.Matches[0].Name, "the best matches are kept")
	assert.Contains(t, out.Hint, "size cap")
}

func TestSummaryHintFollowsDetail(t *testing.T) {
	var calls atomic.Int32
	cs := connect(t, smallStarter(t, &calls), mcp.Options{})

	out, _ := call[mcp.TopologyOutput](t, cs, "get_topology", map[string]any{"detail": "summary", "namespaces": []string{"shop"}})
	require.NotNil(t, out.Summary)
	assert.Contains(t, out.Hint, "detail auto or full", "narrowing never yields detail when a summary was asked for")
	assert.NotContains(t, out.Hint, "Add filters")
}

func TestUnknownIDEchoIsBounded(t *testing.T) {
	var calls atomic.Int32
	cs := connect(t, smallStarter(t, &calls), mcp.Options{MaxResponseBytes: mcp.MinMaxResponseBytes})

	huge := strings.Repeat("x", 100<<10)
	out, _, whole := callSized[mcp.NodeOutput](t, cs, "get_node", map[string]any{"id": huge})
	assert.Nil(t, out.Node)
	assert.LessOrEqual(t, whole, mcp.MinMaxResponseBytes)
	assert.Contains(t, out.Hint, "find_nodes")
}

func TestCapTooSmallIsAnErrorNotAnOversizedAnswer(t *testing.T) {
	cs := connect(t, bigStarter(10), mcp.Options{MaxResponseBytes: 100})

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_topology", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	raw, _ := json.Marshal(res.Content)
	assert.Contains(t, string(raw), "max-response-bytes")
}

func TestRequestStopsWaitingWhenCancelled(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	b := mcp.StartBackend(func() (adapters.Registry, func()) {
		<-release // a slow start, e.g. an unreachable cluster probe
		return smallStarter(t, &calls)()
	}, nil)
	t.Cleanup(b.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := b.Snapshot(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the request gives up instead of waiting on the start")
	assert.Less(t, time.Since(start), 2*time.Second)

	close(release)
	g, _, err := b.Snapshot(context.Background())
	require.NoError(t, err, "a later request gets the registry once it is built")
	assert.NotEmpty(t, g.Nodes)
	assert.Equal(t, int32(1), calls.Load())
}

func TestStartBackendDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	start := time.Now()
	b := mcp.StartBackend(func() (adapters.Registry, func()) {
		<-release
		return adapters.NewRegistry(zap.NewNop().Sugar()), func() {}
	}, nil)
	assert.Less(t, time.Since(start), time.Second, "the handshake must not wait on discovery")
	close(release)
	b.Close()
}

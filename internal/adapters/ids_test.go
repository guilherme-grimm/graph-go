package adapters_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// shopDB is what the Postgres adapter emits for a server with a `shop`
// database holding `orders` and `customers`; extra adds tables only this
// server has.
func shopDB(extra ...string) *stubAdapter {
	ns := []nodes.Node{
		{Id: "pg-shop", Type: "database", Name: "shop"},
		{Id: "pg-shop-orders", Type: "table", Name: "orders", Parent: "pg-shop"},
		{Id: "pg-shop-customers", Type: "table", Name: "customers", Parent: "pg-shop"},
	}
	es := []edges.Edge{
		{Id: "pg-fk-orders-customers", Source: "pg-shop-orders", Target: "pg-shop-customers", Type: "foreign_key"},
	}
	for _, t := range extra {
		ns = append(ns, nodes.Node{Id: "pg-shop-" + t, Type: "table", Name: t, Parent: "pg-shop"})
	}
	return &stubAdapter{discoverN: ns, discoverE: es}
}

func discoverAll(t *testing.T, regs map[string]*stubAdapter) *graph.Graph {
	t.Helper()
	reg := adapters.NewRegistry(zap.NewNop().Sugar())
	for name, a := range regs {
		require.NoError(t, reg.Register(name, "postgres", a, adapters.ConnectionConfig{}))
	}
	g, err := reg.DiscoverAll()
	require.NoError(t, err)
	return g
}

// requireConsistent checks what the UI and MCP rely on: unique node and edge
// IDs, and every parent and edge endpoint naming a node that exists.
func requireConsistent(t *testing.T, g *graph.Graph) map[string]nodes.Node {
	t.Helper()
	byID := map[string]nodes.Node{}
	for _, n := range g.Nodes {
		_, dup := byID[n.Id]
		require.False(t, dup, "duplicate node id %q", n.Id)
		byID[n.Id] = n
	}
	edgeIDs := map[string]bool{}
	for _, e := range g.Edges {
		require.False(t, edgeIDs[e.Id], "duplicate edge id %q", e.Id)
		edgeIDs[e.Id] = true
		assert.Contains(t, byID, e.Source, "edge %s source", e.Id)
		assert.Contains(t, byID, e.Target, "edge %s target", e.Id)
	}
	for _, n := range g.Nodes {
		if n.Parent != "" {
			assert.Contains(t, byID, n.Parent, "node %s parent", n.Id)
		}
	}
	return byID
}

func TestDiscoverAll_SameDatabaseOnTwoServersGetsUniqueIDs(t *testing.T) {
	g := discoverAll(t, map[string]*stubAdapter{
		"trader.db": shopDB("invoices"),
		"basics.db": shopDB(),
	})
	byID := requireConsistent(t, g)

	// Colliding IDs are qualified with their adapter, and stay under it.
	assert.Equal(t, "service-trader.db", byID["pg-shop@trader.db"].Parent)
	assert.Equal(t, "service-basics.db", byID["pg-shop@basics.db"].Parent)
	assert.Equal(t, "pg-shop@trader.db", byID["pg-shop-orders@trader.db"].Parent)
	assert.Equal(t, "pg-shop@basics.db", byID["pg-shop-orders@basics.db"].Parent)

	// An ID only one server emits stays bare.
	require.Contains(t, byID, "pg-shop-invoices")
	assert.Equal(t, "pg-shop@trader.db", byID["pg-shop-invoices"].Parent)
	assert.NotContains(t, byID, "pg-shop")

	// The FK edge follows its adapter's nodes.
	var fks []edges.Edge
	for _, e := range g.Edges {
		if e.Type == "foreign_key" {
			fks = append(fks, e)
		}
	}
	assert.ElementsMatch(t, []edges.Edge{
		{Id: "pg-fk-orders-customers@trader.db", Source: "pg-shop-orders@trader.db", Target: "pg-shop-customers@trader.db", Type: "foreign_key"},
		{Id: "pg-fk-orders-customers@basics.db", Source: "pg-shop-orders@basics.db", Target: "pg-shop-customers@basics.db", Type: "foreign_key"},
	}, fks)
}

func TestDiscoverAll_SingleServerKeepsBareIDs(t *testing.T) {
	byID := requireConsistent(t, discoverAll(t, map[string]*stubAdapter{"db": shopDB()}))

	assert.Contains(t, byID, "pg-shop")
	assert.Contains(t, byID, "pg-shop-orders")
	for id := range byID {
		assert.NotContains(t, id, "@", "no collision, so no qualified ids")
	}
}

func TestDiscoverAll_CollidingWithTopologyQualifiesTheAdapter(t *testing.T) {
	reg := adapters.NewRegistry(zap.NewNop().Sugar())
	reg.SetTopology("kubernetes", []nodes.Node{{Id: "pg-shop", Type: "pod", Name: "pg-shop"}}, nil)
	require.NoError(t, reg.Register("db", "postgres", shopDB(), adapters.ConnectionConfig{}))

	g, err := reg.DiscoverAll()
	require.NoError(t, err)
	byID := requireConsistent(t, g)

	assert.Equal(t, "pod", byID["pg-shop"].Type, "topology ids are never rewritten")
	assert.Equal(t, "database", byID["pg-shop@db"].Type)
}

// withDB is what the Postgres adapter emits for a lone database named db,
// which may contain `@`.
func withDB(db string) *stubAdapter {
	return &stubAdapter{discoverN: []nodes.Node{{Id: "pg-" + db, Type: "database", Name: db}}}
}

// forged is shopDB plus a table and an FK edge whose IDs equal what
// trader.db's colliding IDs would be qualified to.
func forged() *stubAdapter {
	a := shopDB("orders@trader.db")
	a.discoverE = append(a.discoverE, edges.Edge{
		Id: "pg-fk-orders-customers@trader.db", Source: "pg-shop-orders@trader.db", Target: "pg-shop-customers", Type: "foreign_key",
	})
	return a
}

func TestDiscoverAll_QualifiedIDsNeverReuseABareID(t *testing.T) {
	tests := []struct {
		name      string
		regs      map[string]*stubAdapter
		wantNodes map[string]string    // id → parent
		wantEdges map[string][2]string // id → source, target
	}{
		{
			name: "database named like a qualified id",
			regs: map[string]*stubAdapter{"a": withDB("shop@b"), "b": withDB("shop"), "c": withDB("shop")},
			wantNodes: map[string]string{
				"pg-shop@b":   "service-a",
				"pg-shop@b#2": "service-b",
				"pg-shop@c":   "service-c",
			},
		},
		{
			name: "table and fk named like a qualified id",
			regs: map[string]*stubAdapter{"trader.db": shopDB(), "hostile.db": forged()},
			wantNodes: map[string]string{
				"pg-shop-orders@trader.db":   "pg-shop@hostile.db",
				"pg-shop-orders@trader.db#2": "pg-shop@trader.db",
				"pg-shop-orders@hostile.db":  "pg-shop@hostile.db",
			},
			wantEdges: map[string][2]string{
				"pg-fk-orders-customers@trader.db":   {"pg-shop-orders@trader.db", "pg-shop-customers@hostile.db"},
				"pg-fk-orders-customers@trader.db#2": {"pg-shop-orders@trader.db#2", "pg-shop-customers@trader.db"},
				"pg-fk-orders-customers@hostile.db":  {"pg-shop-orders@hostile.db", "pg-shop-customers@hostile.db"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := discoverAll(t, tt.regs)
			byID := requireConsistent(t, g)
			for id, parent := range tt.wantNodes {
				require.Contains(t, byID, id)
				assert.Equal(t, parent, byID[id].Parent, "node %s parent", id)
			}
			edgeByID := map[string]edges.Edge{}
			for _, e := range g.Edges {
				edgeByID[e.Id] = e
			}
			for id, ends := range tt.wantEdges {
				require.Contains(t, edgeByID, id)
				assert.Equal(t, ends, [2]string{edgeByID[id].Source, edgeByID[id].Target}, "edge %s", id)
			}
		})
	}
}

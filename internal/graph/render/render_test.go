package render_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/graph/render"
)

// testGraph is a small K8s namespace with a workload, a pod and a service,
// plus a Postgres database outside the cluster.
func testGraph() *graph.Graph {
	return &graph.Graph{
		Nodes: []nodes.Node{
			{Id: "k8s:namespace:shop", Type: "namespace", Name: "shop"},
			{Id: "k8s:deployment:shop/api", Type: "deployment", Name: "api", Parent: "k8s:namespace:shop", Health: "healthy"},
			{Id: "k8s:pod:shop/api-1", Type: "pod", Name: "api-1", Parent: "k8s:namespace:shop", Health: "unhealthy"},
			{Id: "k8s:service:shop/api", Type: "k8s_service", Name: "api", Parent: "k8s:namespace:shop"},
			{Id: "pg", Type: "postgres", Name: `orders "main"`, Health: "degraded"},
		},
		Edges: []edges.Edge{
			{Id: "e1", Source: "k8s:namespace:shop", Target: "k8s:deployment:shop/api", Type: "contains"},
			{Id: "e2", Source: "k8s:deployment:shop/api", Target: "k8s:pod:shop/api-1", Type: "contains"},
			{Id: "e3", Source: "k8s:service:shop/api", Target: "k8s:pod:shop/api-1", Type: edges.TypeRoutesTo},
			{Id: "e4", Source: "k8s:pod:shop/api-1", Target: "pg", Type: "depends_on", Label: "orders db"},
			{Id: "e5", Source: "pg", Target: "missing", Type: "depends_on"},
		},
	}
}

func mermaid(t *testing.T, g *graph.Graph, fenced bool) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, render.Mermaid(&buf, g, fenced), "Mermaid")
	return buf.String()
}

func dot(t *testing.T, g *graph.Graph) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, render.DOT(&buf, g), "DOT")
	return buf.String()
}

func TestMermaid(t *testing.T) {
	out := mermaid(t, testGraph(), false)

	// Sorted by Id: n0 deployment, n1 namespace, n2 pod, n3 service, n4 pg.
	tests := []struct {
		name string
		want string
	}{
		{name: "header", want: "flowchart LR\n"},
		{name: "parent becomes subgraph", want: `  subgraph n1["shop (namespace)"]`},
		{name: "children nested", want: `    n0["api (deployment)"]`},
		{name: "subgraph closed", want: "  end\n"},
		{name: "store drawn as cylinder with quotes escaped", want: `  n4[("orders #quot;main#quot; (postgres)")]`},
		{name: "edge uses type when unlabeled", want: `  n3 -->|"routes_to"| n2`},
		{name: "edge prefers label", want: `  n2 -->|"orders db"| n4`},
		{name: "partial contains edge kept", want: `  n0 -->|"contains"| n2`},
		{name: "health classes", want: "  class n2 unhealthy\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, out, tt.want)
		})
	}

	assert.NotContains(t, out, `n1 -->|"contains"| n0`, "contains edge into a subgraph child should be dropped")
	assert.NotContains(t, out, "missing", "edge to unknown node should be dropped")
	assert.NotContains(t, out, "```", "unfenced output should not contain a code fence")
}

func TestMermaidFenced(t *testing.T) {
	out := mermaid(t, testGraph(), true)
	assert.True(t, strings.HasPrefix(out, "```mermaid\nflowchart LR\n"), "fenced output should open a mermaid block:\n%s", out)
	assert.True(t, strings.HasSuffix(out, "```\n"), "fenced output should close the block:\n%s", out)
}

func TestDOT(t *testing.T) {
	out := dot(t, testGraph())

	for _, want := range []string{
		"digraph graphgo {\n",
		"  subgraph cluster_n1 {\n",
		`    label="shop (namespace)";`,
		`    n1 [label="shop (namespace)", shape=folder];`,
		`  n4 [label="orders \"main\" (postgres)", shape=cylinder, style="rounded,filled", fillcolor="#fff4e5"];`,
		`  n2 -> n4 [label="orders db"];`,
	} {
		assert.Contains(t, out, want)
	}
}

// When Graphviz is installed, the output must be something dot accepts.
func TestDOTIsValidGraphviz(t *testing.T) {
	bin, err := exec.LookPath("dot")
	if err != nil {
		t.Skip("graphviz not installed")
	}
	cmd := exec.Command(bin, "-Tsvg")
	cmd.Stdin = strings.NewReader(dot(t, testGraph()))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "dot rejected output:\n%s", out)
}

func TestDeterministic(t *testing.T) {
	g := testGraph()
	reversed := &graph.Graph{Edges: g.Edges}
	for i := len(g.Nodes) - 1; i >= 0; i-- {
		reversed.Nodes = append(reversed.Nodes, g.Nodes[i])
	}

	assert.Equal(t, mermaid(t, g, false), mermaid(t, reversed, false), "mermaid output should not depend on node order")
	assert.Equal(t, dot(t, g), dot(t, reversed), "dot output should not depend on node order")
}

func TestParentCycleStillRendersEveryNode(t *testing.T) {
	g := &graph.Graph{Nodes: []nodes.Node{
		{Id: "a", Name: "a", Parent: "b"},
		{Id: "b", Name: "b", Parent: "a"},
	}}
	out := mermaid(t, g, false)
	for _, want := range []string{`["a"]`, `["b"]`} {
		assert.Contains(t, out, want)
	}
}

func TestParallelEdgesSortByLabel(t *testing.T) {
	g := &graph.Graph{
		Nodes: []nodes.Node{{Id: "api", Name: "api"}, {Id: "db", Name: "db"}},
		Edges: []edges.Edge{
			{Id: "e1", Source: "api", Target: "db", Type: "depends_on", Label: "writes"},
			{Id: "e2", Source: "api", Target: "db", Type: "depends_on", Label: "reads"},
		},
	}
	swapped := &graph.Graph{Nodes: g.Nodes, Edges: []edges.Edge{g.Edges[1], g.Edges[0]}}

	assert.Equal(t, mermaid(t, g, false), mermaid(t, swapped, false), "mermaid output should not depend on edge order")
	assert.Equal(t, dot(t, g), dot(t, swapped), "dot output should not depend on edge order")
}

func TestMultilineLabelsStayInsideFence(t *testing.T) {
	g := &graph.Graph{
		Nodes: []nodes.Node{
			{Id: "a", Name: "evil\n```\nafter"},
			{Id: "b", Name: "b\r\nc"},
		},
		Edges: []edges.Edge{{Id: "e", Source: "a", Target: "b", Type: "x", Label: "one\ntwo"}},
	}
	out := mermaid(t, g, true)

	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for i, line := range lines[1 : len(lines)-1] {
		assert.NotEqual(t, "```", strings.TrimSpace(line), "line %d closes the fence early:\n%s", i+1, out)
	}
	for _, want := range []string{"[\"evil ``` after\"]", `["b c"]`, `-->|"one two"|`} {
		assert.Contains(t, out, want)
	}

	// DOT keeps the breaks, escaped, so labels stay on one source line.
	d := dot(t, g)
	for _, want := range []string{"label=\"evil\\n```\\nafter\"", `label="b\nc"`, `label="one\ntwo"`} {
		assert.Contains(t, d, want)
	}
}

func TestMermaidEscapesPipes(t *testing.T) {
	g := &graph.Graph{
		Nodes: []nodes.Node{{Id: "a", Name: "a|b"}, {Id: "c", Name: "c"}},
		Edges: []edges.Edge{{Id: "e", Source: "a", Target: "c", Type: "depends_on", Label: "reads|writes"}},
	}
	out := mermaid(t, g, false)

	assert.Contains(t, out, `n0["a#124;b"]`)
	assert.Contains(t, out, `-->|"reads#124;writes"| n1`)
	assert.NotContains(t, out, "reads|writes")
}

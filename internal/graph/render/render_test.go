package render_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

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
	if err := render.Mermaid(&buf, g, fenced); err != nil {
		t.Fatalf("Mermaid: %v", err)
	}
	return buf.String()
}

func dot(t *testing.T, g *graph.Graph) string {
	t.Helper()
	var buf bytes.Buffer
	if err := render.DOT(&buf, g); err != nil {
		t.Fatalf("DOT: %v", err)
	}
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
			if !strings.Contains(out, tt.want) {
				t.Errorf("output missing %q\nfull output:\n%s", tt.want, out)
			}
		})
	}

	if strings.Contains(out, `n1 -->|"contains"| n0`) {
		t.Errorf("contains edge into a subgraph child should be dropped\n%s", out)
	}
	if strings.Contains(out, "missing") {
		t.Errorf("edge to unknown node should be dropped\n%s", out)
	}
	if strings.Contains(out, "```") {
		t.Errorf("unfenced output should not contain a code fence\n%s", out)
	}
}

func TestMermaidFenced(t *testing.T) {
	out := mermaid(t, testGraph(), true)
	if !strings.HasPrefix(out, "```mermaid\nflowchart LR\n") || !strings.HasSuffix(out, "```\n") {
		t.Errorf("fenced output not wrapped in a mermaid block:\n%s", out)
	}
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
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
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
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dot rejected output: %v\n%s", err, out)
	}
}

func TestDeterministic(t *testing.T) {
	g := testGraph()
	reversed := &graph.Graph{Edges: g.Edges}
	for i := len(g.Nodes) - 1; i >= 0; i-- {
		reversed.Nodes = append(reversed.Nodes, g.Nodes[i])
	}

	if a, b := mermaid(t, g, false), mermaid(t, reversed, false); a != b {
		t.Errorf("mermaid output depends on node order:\n%s\nvs\n%s", a, b)
	}
	if a, b := dot(t, g), dot(t, reversed); a != b {
		t.Errorf("dot output depends on node order:\n%s\nvs\n%s", a, b)
	}
}

func TestParentCycleStillRendersEveryNode(t *testing.T) {
	g := &graph.Graph{Nodes: []nodes.Node{
		{Id: "a", Name: "a", Parent: "b"},
		{Id: "b", Name: "b", Parent: "a"},
	}}
	out := mermaid(t, g, false)
	for _, want := range []string{`["a"]`, `["b"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

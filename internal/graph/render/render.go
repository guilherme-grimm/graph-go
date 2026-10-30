// Package render turns a discovered graph into diagram source: Mermaid
// flowcharts and Graphviz DOT. Nodes with children (a namespace's workloads,
// a database's tables) become nested subgraphs, so the "contains" edges they
// replace are not drawn.
package render

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/health"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// storeTypes are drawn as cylinders.
var storeTypes = map[string]bool{
	string(nodes.TypePostgres):      true,
	string(nodes.TypeMongodb):       true,
	string(nodes.TypeMySQL):         true,
	string(nodes.TypeRedis):         true,
	string(nodes.TypeElasticsearch): true,
	string(nodes.TypeDatabase):      true,
	string(nodes.TypeCache):         true,
	string(nodes.TypeS3):            true,
	string(nodes.TypeStorage):       true,
}

// tree indexes a graph for rendering: stable short IDs (the real ones carry
// characters neither format accepts), children per parent, and the edges
// worth drawing. Everything is sorted so output is deterministic.
type tree struct {
	nodes    []nodes.Node
	ids      map[string]string // node Id → n0, n1, ...
	children map[string][]int  // parent Id → indexes into nodes
	edges    []edges.Edge
}

func index(g *graph.Graph) *tree {
	t := &tree{
		nodes:    slices.Clone(g.Nodes),
		ids:      make(map[string]string, len(g.Nodes)),
		children: make(map[string][]int),
	}
	slices.SortFunc(t.nodes, func(a, b nodes.Node) int { return cmp.Compare(a.Id, b.Id) })

	for i, n := range t.nodes {
		t.ids[n.Id] = fmt.Sprintf("n%d", i)
	}

	parents := make(map[string]string, len(t.nodes))
	for i, n := range t.nodes {
		if _, ok := t.ids[n.Parent]; ok && n.Parent != n.Id {
			t.children[n.Parent] = append(t.children[n.Parent], i)
			parents[n.Id] = n.Parent
		}
	}

	for _, e := range g.Edges {
		_, okSrc := t.ids[e.Source]
		_, okDst := t.ids[e.Target]
		if !okSrc || !okDst {
			continue
		}
		if e.Type == "contains" && parents[e.Target] == e.Source {
			continue // nesting already shows it
		}
		t.edges = append(t.edges, e)
	}
	slices.SortFunc(t.edges, func(a, b edges.Edge) int {
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.Target, b.Target), cmp.Compare(a.Type, b.Type))
	})

	return t
}

// walk visits every node once, parents before children. enter reports whether
// the node opens a group; leave closes it. A parent cycle cannot hide nodes:
// anything not reached from a root is visited as a root afterwards.
func (t *tree) walk(enter func(n nodes.Node, depth int, group bool), leave func(depth int)) {
	visited := make([]bool, len(t.nodes))
	var visit func(i, depth int)
	visit = func(i, depth int) {
		if visited[i] {
			return
		}
		visited[i] = true
		n := t.nodes[i]
		kids := t.children[n.Id]
		enter(n, depth, len(kids) > 0)
		if len(kids) == 0 {
			return
		}
		for _, k := range kids {
			visit(k, depth+1)
		}
		leave(depth)
	}

	for i, n := range t.nodes {
		if _, hasParent := t.ids[n.Parent]; !hasParent || n.Parent == n.Id {
			visit(i, 0)
		}
	}
	for i := range t.nodes {
		visit(i, 0)
	}
}

func label(n nodes.Node) string {
	name := n.Name
	if name == "" {
		name = n.Id
	}
	if n.Type == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, n.Type)
}

func edgeLabel(e edges.Edge) string {
	if e.Label != "" {
		return e.Label
	}
	return e.Type
}

func healthClass(h string) string {
	switch health.Status(h) {
	case health.Healthy, health.Degraded, health.Unhealthy:
		return h
	}
	return ""
}

var mermaidEscaper = strings.NewReplacer(`"`, "#quot;", "<", "#lt;", ">", "#gt;")

// Mermaid writes g as a Mermaid flowchart. With fenced set, the diagram is
// wrapped in a ```mermaid block so it renders when saved as Markdown.
func Mermaid(w io.Writer, g *graph.Graph, fenced bool) error {
	t := index(g)
	var b strings.Builder

	if fenced {
		b.WriteString("```mermaid\n")
	}
	b.WriteString("flowchart LR\n")

	classes := map[string][]string{}
	t.walk(func(n nodes.Node, depth int, group bool) {
		id := t.ids[n.Id]
		pad := strings.Repeat("  ", depth+1)
		text := mermaidEscaper.Replace(label(n))
		switch {
		case group:
			fmt.Fprintf(&b, "%ssubgraph %s[\"%s\"]\n", pad, id, text)
		case storeTypes[n.Type]:
			fmt.Fprintf(&b, "%s%s[(\"%s\")]\n", pad, id, text)
		default:
			fmt.Fprintf(&b, "%s%s[\"%s\"]\n", pad, id, text)
		}
		if c := healthClass(n.Health); c != "" {
			classes[c] = append(classes[c], id)
		}
	}, func(depth int) {
		fmt.Fprintf(&b, "%send\n", strings.Repeat("  ", depth+1))
	})

	for _, e := range t.edges {
		fmt.Fprintf(&b, "  %s -->|\"%s\"| %s\n", t.ids[e.Source], mermaidEscaper.Replace(edgeLabel(e)), t.ids[e.Target])
	}

	for _, c := range []string{"healthy", "degraded", "unhealthy"} {
		if len(classes[c]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  classDef %s %s\n", c, mermaidStyles[c])
		fmt.Fprintf(&b, "  class %s %s\n", strings.Join(classes[c], ","), c)
	}

	if fenced {
		b.WriteString("```\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

var mermaidStyles = map[string]string{
	"healthy":   "fill:#e6f4ea,stroke:#2e7d32",
	"degraded":  "fill:#fff4e5,stroke:#ed6c02",
	"unhealthy": "fill:#fdecea,stroke:#c62828",
}

var dotFill = map[string]string{
	"healthy":   "#e6f4ea",
	"degraded":  "#fff4e5",
	"unhealthy": "#fdecea",
}

var dotEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// DOT writes g as a Graphviz digraph. Groups become clusters; the parent node
// is also drawn inside its cluster so edges can point at it.
func DOT(w io.Writer, g *graph.Graph) error {
	t := index(g)
	var b strings.Builder

	b.WriteString("digraph graphgo {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=rounded, fontname=\"Helvetica\"];\n")
	b.WriteString("  edge [fontname=\"Helvetica\", fontsize=10];\n")

	t.walk(func(n nodes.Node, depth int, group bool) {
		id := t.ids[n.Id]
		pad := strings.Repeat("  ", depth+1)
		text := dotEscaper.Replace(label(n))

		attrs := []string{fmt.Sprintf("label=\"%s\"", text)}
		switch {
		case group:
			fmt.Fprintf(&b, "%ssubgraph cluster_%s {\n", pad, id)
			fmt.Fprintf(&b, "%s  label=\"%s\";\n", pad, text)
			pad += "  "
			attrs = append(attrs, "shape=folder")
		case storeTypes[n.Type]:
			attrs = append(attrs, "shape=cylinder")
		}
		if fill, ok := dotFill[healthClass(n.Health)]; ok {
			attrs = append(attrs, "style=\"rounded,filled\"", fmt.Sprintf("fillcolor=\"%s\"", fill))
		}
		fmt.Fprintf(&b, "%s%s [%s];\n", pad, id, strings.Join(attrs, ", "))
	}, func(depth int) {
		fmt.Fprintf(&b, "%s}\n", strings.Repeat("  ", depth+1))
	})

	for _, e := range t.edges {
		fmt.Fprintf(&b, "  %s -> %s [label=\"%s\"];\n", t.ids[e.Source], t.ids[e.Target], dotEscaper.Replace(edgeLabel(e)))
	}

	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}

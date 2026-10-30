package mcp

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// ToolNames is every tool the server registers. All are read-only; a test
// pins this list so adding one that acts is a deliberate change (ADR-0003).
var ToolNames = []string{"get_topology", "get_node", "find_nodes"}

// emptyHint does not guess why: Docker may be reachable with every service
// failing to connect, or there may be no source at all. stderr has the cause.
const emptyHint = "graph-go has nothing it could connect to. It looks at Docker (DOCKER_HOST or /var/run/docker.sock), " +
	"the current Kubernetes context, and connections in its config file; services it found but could not reach, " +
	"and sources that were unavailable, are logged on the server's stderr."

// Options configures the server.
type Options struct {
	Version string
	// MaxResponseBytes caps every tool response; 0 means DefaultMaxResponseBytes.
	MaxResponseBytes int
}

// NewServer returns an MCP server exposing graph-go's read-only tools over b.
func NewServer(b *Backend, opts Options) *sdk.Server {
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = DefaultMaxResponseBytes
	}
	t := &tools{b: b, maxBytes: opts.MaxResponseBytes}

	s := sdk.NewServer(&sdk.Implementation{Name: "graph-go", Version: opts.Version}, nil)
	readOnly := &sdk.ToolAnnotations{ReadOnlyHint: true}

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_topology",
		Annotations: readOnly,
		Description: "Get the infrastructure topology graph-go discovered: services, databases, caches, storage, " +
			"Kubernetes workloads and pods, how they connect, and their current health. On large systems this " +
			"returns a summary (counts per namespace and type, plus every unhealthy or degraded node in full). " +
			"Use the filters to get full detail for a namespace, node type or health state, or use find_nodes / " +
			"get_node for specific services.",
	}, t.getTopology)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_node",
		Annotations: readOnly,
		Description: "Get one node by id with its direct neighbours: the node itself, every edge into or out of it, " +
			"and the nodes on the other end of those edges. Use this to answer \"what does X depend on / what " +
			"depends on X\".",
	}, t.getNode)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "find_nodes",
		Annotations: readOnly,
		Description: "Search nodes by name or id (case-insensitive substring). Returns compact matches (id, name, " +
			"type, namespace, health), best matches first. Use the ids with get_node.",
	}, t.findNodes)

	return s
}

type tools struct {
	b        *Backend
	maxBytes int
}

// ── get_topology ──────────────────────────────────────────────────────────

type TopologyInput struct {
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"only nodes in these Kubernetes namespaces; nodes outside Kubernetes are in (none)"`
	Types      []string `json:"types,omitempty" jsonschema:"only these node types, e.g. postgres, redis, service, pod, deployment, k8s_service"`
	Health     []string `json:"health,omitempty" jsonschema:"only these health states: healthy, degraded, unhealthy, unknown"`
	Name       string   `json:"name,omitempty" jsonschema:"case-insensitive substring of node name or id"`
	Detail     string   `json:"detail,omitempty" jsonschema:"auto (default): full detail up to 300 nodes, summary above; summary; or full (still size-capped)"`
}

type Totals struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
}

type TopologyOutput struct {
	ObservedAt time.Time    `json:"observed_at" jsonschema:"time of the health sweep these states reflect"`
	Total      Totals       `json:"total" jsonschema:"size of the whole graph before filtering"`
	Matched    int          `json:"matched" jsonschema:"nodes matching the filters"`
	Truncated  bool         `json:"truncated" jsonschema:"true when nodes were left out: a summary, or the response size cap"`
	Nodes      []nodes.Node `json:"nodes"`
	Edges      []edges.Edge `json:"edges" jsonschema:"edges between the returned nodes"`
	Summary    *Summary     `json:"summary,omitempty" jsonschema:"present when nodes holds only the unhealthy and degraded matches"`
	Hint       string       `json:"hint,omitempty"`
}

func (t *tools) getTopology(ctx context.Context, _ *sdk.CallToolRequest, in TopologyInput) (*sdk.CallToolResult, TopologyOutput, error) {
	g, observedAt, err := t.b.Snapshot(ctx)
	if err != nil {
		return nil, TopologyOutput{}, fmt.Errorf("discovery failed: %w", err)
	}
	out := TopologyOutput{ObservedAt: observedAt, Total: Totals{len(g.Nodes), len(g.Edges)}, Nodes: []nodes.Node{}, Edges: []edges.Edge{}}
	if len(g.Nodes) == 0 {
		out.Hint = emptyHint
		return nil, out, nil
	}

	f := newFilter(in.Namespaces, in.Types, in.Health, in.Name)
	var matched []nodes.Node
	for _, n := range g.Nodes {
		if f.match(n) {
			matched = append(matched, n)
		}
	}
	out.Matched = len(matched)
	if len(matched) == 0 {
		out.Hint = "No nodes match these filters. Call get_topology without filters for the namespace and type counts."
		return nil, out, nil
	}

	detail := strings.ToLower(in.Detail)
	if detail == "" {
		detail = "auto"
	}
	if detail != "auto" && detail != "summary" && detail != "full" {
		return nil, TopologyOutput{}, fmt.Errorf("detail must be auto, summary or full, not %q", in.Detail)
	}

	selected := matched
	if detail == "summary" || (detail == "auto" && len(matched) > LargeGraphNodes) {
		out.Summary = summarize(matched)
		out.Truncated = true
		selected = nil
		for _, n := range matched {
			if h := healthOf(n); h == "unhealthy" || h == "degraded" {
				selected = append(selected, n)
			}
		}
		out.Hint = fmt.Sprintf("Summary of %d nodes; nodes lists only the unhealthy and degraded ones. "+
			"Narrow with namespaces/types/health/name for full detail (largest namespaces: %s), or use find_nodes.",
			len(matched), strings.Join(largestNamespaces(out.Summary, 5), ", "))
		switch {
		case detail == "summary":
			// Narrowing never yields detail here; the caller asked for a summary.
			out.Hint = fmt.Sprintf("Summary of %d matching nodes, as requested; nodes lists only the unhealthy and degraded ones. "+
				"Call again with detail auto or full for the nodes themselves.", len(matched))
		case !f.empty():
			out.Hint = fmt.Sprintf("Summary of %d matching nodes; nodes lists only the unhealthy and degraded ones. "+
				"Add filters to get under %d nodes for full detail, or use find_nodes.", len(matched), LargeGraphNodes)
		}
	}

	// Build the response inside the measurement, cap hint included, so what
	// is measured is exactly what is sent.
	build := func(ns []nodes.Node) TopologyOutput {
		o := out
		o.Nodes, o.Edges = nonNil(ns), edgesWithin(g.Edges, ns)
		if len(ns) < len(selected) {
			o.Truncated = true
			o.Hint = strings.TrimSpace(o.Hint + fmt.Sprintf(" Response size cap reached: returned %d of %d selected nodes, "+
				"unhealthy and degraded first. Narrow the filters to see the rest.", len(ns), len(selected)))
		}
		return o
	}
	fitted, _, err := fitNodes(selected, t.maxBytes, func(ns []nodes.Node) ([]byte, error) { return wireBytes(build(ns)) })
	if err != nil {
		return nil, TopologyOutput{}, err
	}
	return nil, build(fitted), nil
}

func nonNil(ns []nodes.Node) []nodes.Node {
	if ns == nil {
		return []nodes.Node{}
	}
	return ns
}

// ── get_node ──────────────────────────────────────────────────────────────

type NodeInput struct {
	ID string `json:"id" jsonschema:"node id, as returned by get_topology or find_nodes"`
}

type NodeOutput struct {
	ObservedAt time.Time    `json:"observed_at" jsonschema:"time of the health sweep these states reflect"`
	Node       *nodes.Node  `json:"node,omitempty"`
	Edges      []edges.Edge `json:"edges" jsonschema:"every edge into or out of the node"`
	Neighbors  []nodes.Node `json:"neighbors" jsonschema:"the nodes on the other end of those edges"`
	Truncated  bool         `json:"truncated"`
	Hint       string       `json:"hint,omitempty"`
}

func (t *tools) getNode(ctx context.Context, _ *sdk.CallToolRequest, in NodeInput) (*sdk.CallToolResult, NodeOutput, error) {
	g, observedAt, err := t.b.Snapshot(ctx)
	if err != nil {
		return nil, NodeOutput{}, fmt.Errorf("discovery failed: %w", err)
	}
	out := NodeOutput{ObservedAt: observedAt, Edges: []edges.Edge{}, Neighbors: []nodes.Node{}}
	if len(g.Nodes) == 0 {
		out.Hint = emptyHint
		return nil, out, nil
	}

	idx := nodeIndex(g)
	n, ok := idx[in.ID]
	if !ok {
		out.Hint = fmt.Sprintf("No node with id %q. Use find_nodes to look ids up by name.", clip(in.ID, 200))
		return nil, out, nil
	}
	out.Node = &n

	var incident []edges.Edge
	seen := map[string]bool{}
	var neighbors []nodes.Node
	for _, e := range g.Edges {
		other := ""
		switch in.ID {
		case e.Source:
			other = e.Target
		case e.Target:
			other = e.Source
		default:
			continue
		}
		incident = append(incident, e)
		if nb, ok := idx[other]; ok && !seen[other] {
			seen[other] = true
			neighbors = append(neighbors, nb)
		}
	}

	build := func(ns []nodes.Node) NodeOutput {
		o := out
		o.Neighbors, o.Edges = nonNil(ns), incidentTo(incident, in.ID, ns)
		if len(ns) < len(neighbors) {
			o.Truncated = true
			o.Hint = fmt.Sprintf("Response size cap reached: returned %d of %d neighbours, unhealthy and degraded first. "+
				"Use get_topology with filters to see the rest.", len(ns), len(neighbors))
		}
		return o
	}
	fitted, _, err := fitNodes(neighbors, t.maxBytes, func(ns []nodes.Node) ([]byte, error) { return wireBytes(build(ns)) })
	if err != nil {
		return nil, NodeOutput{}, err
	}
	return nil, build(fitted), nil
}

// incidentTo keeps the edges between id and the kept neighbours.
func incidentTo(es []edges.Edge, id string, kept []nodes.Node) []edges.Edge {
	ids := map[string]bool{id: true}
	for _, n := range kept {
		ids[n.Id] = true
	}
	out := []edges.Edge{}
	for _, e := range es {
		if ids[e.Source] && ids[e.Target] {
			out = append(out, e)
		}
	}
	return out
}

// ── find_nodes ────────────────────────────────────────────────────────────

type FindInput struct {
	Query string `json:"query" jsonschema:"case-insensitive substring of node name or id"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum matches to return (default 20, max 100)"`
}

type Match struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Namespace string `json:"namespace,omitempty"`
	Health    string `json:"health"`
}

type FindOutput struct {
	ObservedAt time.Time `json:"observed_at" jsonschema:"time of the health sweep these states reflect"`
	Matches    []Match   `json:"matches"`
	Total      int       `json:"total" jsonschema:"all matches, including those beyond limit"`
	Hint       string    `json:"hint,omitempty"`
}

func (t *tools) findNodes(ctx context.Context, _ *sdk.CallToolRequest, in FindInput) (*sdk.CallToolResult, FindOutput, error) {
	q := strings.ToLower(strings.TrimSpace(in.Query))
	if q == "" {
		return nil, FindOutput{}, fmt.Errorf("query must not be empty")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)

	g, observedAt, err := t.b.Snapshot(ctx)
	if err != nil {
		return nil, FindOutput{}, fmt.Errorf("discovery failed: %w", err)
	}
	out := FindOutput{ObservedAt: observedAt, Matches: []Match{}}
	if len(g.Nodes) == 0 {
		out.Hint = emptyHint
		return nil, out, nil
	}

	type scored struct {
		n     nodes.Node
		score int
	}
	var hits []scored
	for _, n := range g.Nodes {
		if s := matchScore(n, q); s > 0 {
			hits = append(hits, scored{n, s})
		}
	}
	slices.SortFunc(hits, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.n.Name, b.n.Name), cmp.Compare(a.n.Id, b.n.Id))
	})
	out.Total = len(hits)
	var matches []Match
	for _, h := range hits[:min(limit, len(hits))] {
		m := Match{ID: h.n.Id, Name: h.n.Name, Type: h.n.Type, Health: healthOf(h.n)}
		if ns := namespaceOf(h.n); ns != noneNamespace {
			m.Namespace = ns
		}
		matches = append(matches, m)
	}
	if out.Total == 0 {
		out.Hint = "No node name or id contains that text. get_topology without filters shows the namespaces and types that exist."
		return nil, out, nil
	}

	build := func(ms []Match) FindOutput {
		o := out
		o.Matches = append([]Match{}, ms...)
		if len(ms) < len(matches) {
			o.Hint = fmt.Sprintf("Response size cap reached: returned %d of %d matches, best first. Use a longer query.", len(ms), len(matches))
		}
		return o
	}
	fitted, _, err := fitPrefix(matches, t.maxBytes, func(ms []Match) ([]byte, error) { return wireBytes(build(ms)) })
	if err != nil {
		return nil, FindOutput{}, err
	}
	return nil, build(fitted), nil
}

// matchScore ranks a node for a lowercase query, as the UI search does:
// exact name, then name prefix, then name substring, then id substring.
func matchScore(n nodes.Node, q string) int {
	name, id := strings.ToLower(n.Name), strings.ToLower(n.Id)
	switch {
	case name == q:
		return 100
	case strings.HasPrefix(name, q):
		return 80
	case strings.Contains(name, q):
		return 60
	case strings.Contains(id, q):
		return 20
	}
	return 0
}

// clip bounds caller input echoed back in a hint, so it cannot push a
// response past the byte cap.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

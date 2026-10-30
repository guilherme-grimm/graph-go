package mcp

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// LargeGraphNodes is where get_topology switches from full detail to a
// summary in auto mode. It matches the UI's LARGE_GRAPH_NODES.
const LargeGraphNodes = 300

// DefaultMaxResponseBytes keeps a response near 20k tokens, under what agent
// clients accept from one tool call.
const DefaultMaxResponseBytes = 80 << 10

const noneNamespace = "(none)"

// filter selects nodes; empty fields match everything.
type filter struct {
	namespaces map[string]bool
	types      map[string]bool
	health     map[string]bool
	name       string
}

func newFilter(namespaces, types, healthStates []string, name string) filter {
	set := func(xs []string) map[string]bool {
		if len(xs) == 0 {
			return nil
		}
		m := make(map[string]bool, len(xs))
		for _, x := range xs {
			m[strings.ToLower(strings.TrimSpace(x))] = true
		}
		return m
	}
	return filter{set(namespaces), set(types), set(healthStates), strings.ToLower(strings.TrimSpace(name))}
}

func (f filter) empty() bool {
	return f.namespaces == nil && f.types == nil && f.health == nil && f.name == ""
}

func (f filter) match(n nodes.Node) bool {
	if f.namespaces != nil && !f.namespaces[strings.ToLower(namespaceOf(n))] {
		return false
	}
	if f.types != nil && !f.types[strings.ToLower(n.Type)] {
		return false
	}
	if f.health != nil && !f.health[healthOf(n)] {
		return false
	}
	if f.name != "" && !strings.Contains(strings.ToLower(n.Name), f.name) && !strings.Contains(strings.ToLower(n.Id), f.name) {
		return false
	}
	return true
}

func namespaceOf(n nodes.Node) string {
	if ns, _ := n.Metadata["namespace"].(string); ns != "" {
		return ns
	}
	return noneNamespace
}

func healthOf(n nodes.Node) string {
	if n.Health == "" {
		return "unknown"
	}
	return n.Health
}

// healthRank orders nodes by how much an agent needs to see them.
func healthRank(n nodes.Node) int {
	switch healthOf(n) {
	case "unhealthy":
		return 0
	case "degraded":
		return 1
	case "unknown":
		return 2
	default:
		return 3
	}
}

// edgesWithin returns the edges whose ends are both in ns.
func edgesWithin(es []edges.Edge, ns []nodes.Node) []edges.Edge {
	ids := make(map[string]bool, len(ns))
	for _, n := range ns {
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

// Summary is what get_topology returns instead of every node on a large
// system.
type Summary struct {
	ByNamespace map[string]map[string]int `json:"by_namespace" jsonschema:"node count per Kubernetes namespace and node type; nodes outside Kubernetes are under (none)"`
	ByHealth    map[string]int            `json:"by_health" jsonschema:"node count per health state"`
}

func summarize(ns []nodes.Node) *Summary {
	s := &Summary{ByNamespace: map[string]map[string]int{}, ByHealth: map[string]int{}}
	for _, n := range ns {
		nsName := namespaceOf(n)
		if s.ByNamespace[nsName] == nil {
			s.ByNamespace[nsName] = map[string]int{}
		}
		s.ByNamespace[nsName][n.Type]++
		s.ByHealth[healthOf(n)]++
	}
	return s
}

// largestNamespaces names the namespaces with the most nodes, for hints.
func largestNamespaces(s *Summary, k int) []string {
	type nsCount struct {
		name  string
		count int
	}
	var all []nsCount
	for name, types := range s.ByNamespace {
		total := 0
		for _, c := range types {
			total += c
		}
		all = append(all, nsCount{name, total})
	}
	slices.SortFunc(all, func(a, b nsCount) int { return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.name, b.name)) })
	var out []string
	for i := 0; i < len(all) && i < k; i++ {
		out = append(out, fmt.Sprintf("%s (%d)", all[i].name, all[i].count))
	}
	return out
}

// fitNodes shrinks ns, least important first, until encode(ns) fits in
// maxBytes. It reports whether anything was dropped. encode must produce the
// whole response as sent (see wireBytes), so the cap covers everything.
func fitNodes(ns []nodes.Node, maxBytes int, encode func([]nodes.Node) ([]byte, error)) ([]nodes.Node, bool, error) {
	ranked := slices.Clone(ns)
	slices.SortStableFunc(ranked, func(a, b nodes.Node) int {
		return cmp.Or(cmp.Compare(healthRank(a), healthRank(b)), cmp.Compare(a.Id, b.Id))
	})
	fitted, dropped, err := fitPrefix(ranked, maxBytes, encode)
	if err != nil || !dropped {
		// Nothing dropped: keep the caller's order.
		return ns, false, err
	}
	return fitted, true, nil
}

// fitPrefix returns the longest prefix of items whose encoding fits in
// maxBytes, and whether it had to cut anything.
func fitPrefix[T any](items []T, maxBytes int, encode func([]T) ([]byte, error)) ([]T, bool, error) {
	b, err := encode(items)
	if err != nil {
		return nil, false, err
	}
	if len(b) <= maxBytes {
		return items, false, nil
	}
	lo, hi := 0, len(items)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		b, err := encode(items[:mid])
		if err != nil {
			return nil, false, err
		}
		if len(b) <= maxBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return items[:lo], true, nil
}

// wireBytes encodes a tool output the way the SDK sends it: the value as
// structuredContent and again as JSON text content. The byte cap applies to
// this, not to the bare output.
func wireBytes(out any) ([]byte, error) {
	text, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return json.Marshal(&sdk.CallToolResult{
		Content:           []sdk.Content{&sdk.TextContent{Text: string(text)}},
		StructuredContent: json.RawMessage(text),
	})
}

func nodeIndex(g *graph.Graph) map[string]nodes.Node {
	m := make(map[string]nodes.Node, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.Id] = n
	}
	return m
}

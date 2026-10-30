package server

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/guilherme-grimm/graph-go/internal/graph"
)

// reportLine is one row of the startup report: an adapter or a topology source.
type reportLine struct {
	ok   bool
	name string
	kind string // connection type, or "topology"
	stat string // what it found, or why it failed
}

// startupReport turns the first sync's result and the first discovered graph
// into report rows. An adapter that registered but whose first Discover
// failed shows as failed with that error: DiscoverAll keeps it in the graph
// as a service node carrying Metadata["error"].
func startupReport(res syncResult, g *graph.Graph) []reportLine {
	type found struct {
		errMsg string
		counts map[string]int // child node type → count
	}
	byAdapter := make(map[string]*found)
	for _, n := range g.Nodes {
		name, _ := n.Metadata["adapter"].(string)
		if name == "" {
			continue
		}
		f, ok := byAdapter[name]
		if !ok {
			f = &found{counts: map[string]int{}}
			byAdapter[name] = f
		}
		// The adapter's own node: "service-<name>" from the registry, or
		// "<name>" for HTTP adapters, which build their top-level node.
		if n.Id == "service-"+name || n.Id == name {
			f.errMsg, _ = n.Metadata["error"].(string)
			continue
		}
		f.counts[n.Type]++
	}

	lines := make([]reportLine, 0, len(res.adapters)+len(res.topology))
	for _, a := range res.adapters {
		line := reportLine{name: a.name, kind: a.connType}
		switch f := byAdapter[a.name]; {
		case a.err != nil:
			// The row already names the adapter; drop the registry's prefix.
			line.stat = strings.TrimPrefix(a.err.Error(), fmt.Sprintf("registry: failed to connect adapter %q: ", a.name))
		case f != nil && f.errMsg != "":
			line.stat = f.errMsg
		default:
			line.ok = true
			line.stat = "connected"
			if f != nil && len(f.counts) > 0 {
				line.stat = countSummary(f.counts)
			}
		}
		lines = append(lines, line)
	}
	slices.SortFunc(lines, func(a, b reportLine) int { return cmp.Compare(a.name, b.name) })

	for _, t := range res.topology {
		lines = append(lines, reportLine{
			ok:   true,
			name: t.source,
			kind: "topology",
			stat: fmt.Sprintf("%s, %s", plural(t.nodes, "node"), plural(t.edges, "edge")),
		})
	}
	return lines
}

// countSummary renders child counts largest first, e.g. "12 tables, 1 database".
func countSummary(counts map[string]int) string {
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	slices.SortFunc(types, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = plural(counts[t], strings.ReplaceAll(t, "_", " "))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	switch {
	case strings.HasSuffix(noun, "s"):
	case strings.HasSuffix(noun, "x"):
		noun += "es"
	default:
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}

// writeStartupReport prints the report as an aligned table. It writes nothing
// when there is nothing to report, so an empty start stays quiet.
func writeStartupReport(w io.Writer, lines []reportLine) error {
	if len(lines) == 0 {
		return nil
	}

	var connected, adapters int
	for _, l := range lines {
		if l.kind == "topology" {
			continue
		}
		adapters++
		if l.ok {
			connected++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "graph-go: %d of %d adapters connected\n", connected, adapters)
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, l := range lines {
		mark := "✓"
		if !l.ok {
			mark = "✗"
		}
		if _, err := fmt.Fprintf(tw, "  %s %s\t%s\t%s\n", mark, l.name, l.kind, l.stat); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}

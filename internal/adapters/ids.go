package adapters

import (
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// adapterResult is one adapter's discovery output inside DiscoverAll.
type adapterResult struct {
	name, connType string
	nodes          []nodes.Node
	edges          []edges.Edge
}

// qualifyCollidingIDs makes node and edge IDs unique across adapters.
// Adapters build IDs from what they find, not from which server they found
// it on, so two Postgres servers that both have a `shop` database both emit
// `pg-shop` (#62). Every ID that more than one adapter emits, or that a
// topology contribution already uses, becomes `<id>@<adapter>` in each
// adapter that emits it, and that adapter's parent refs and edge endpoints
// follow. Unique IDs are left alone, so the common single-server case, and
// any depends_on targets and links that name those IDs, keep working. The
// result depends only on the set of results, not on their order.
func qualifyCollidingIDs(results []adapterResult, topoNodes []nodes.Node, topoEdges []edges.Edge) {
	nodeOwners := map[string]int{}
	edgeOwners := map[string]int{}
	for _, n := range topoNodes {
		nodeOwners[n.Id]++
	}
	for _, e := range topoEdges {
		edgeOwners[e.Id]++
	}
	for _, res := range results {
		for id := range uniqueNodeIDs(res.nodes) {
			nodeOwners[id]++
		}
		for id := range uniqueEdgeIDs(res.edges) {
			edgeOwners[id]++
		}
	}

	for i := range results {
		res := &results[i]
		renamed := map[string]string{}
		for id := range uniqueNodeIDs(res.nodes) {
			if nodeOwners[id] > 1 {
				renamed[id] = id + "@" + res.name
			}
		}
		for j := range res.nodes {
			n := &res.nodes[j]
			if q, ok := renamed[n.Id]; ok {
				n.Id = q
			}
			if q, ok := renamed[n.Parent]; ok {
				n.Parent = q
			}
		}
		for j := range res.edges {
			e := &res.edges[j]
			if edgeOwners[e.Id] > 1 {
				e.Id = e.Id + "@" + res.name
			}
			if q, ok := renamed[e.Source]; ok {
				e.Source = q
			}
			if q, ok := renamed[e.Target]; ok {
				e.Target = q
			}
		}
	}
}

func uniqueNodeIDs(ns []nodes.Node) map[string]struct{} {
	ids := make(map[string]struct{}, len(ns))
	for _, n := range ns {
		ids[n.Id] = struct{}{}
	}
	return ids
}

func uniqueEdgeIDs(es []edges.Edge) map[string]struct{} {
	ids := make(map[string]struct{}, len(es))
	for _, e := range es {
		ids[e.Id] = struct{}{}
	}
	return ids
}

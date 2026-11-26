package adapters

import (
	"cmp"
	"fmt"
	"slices"

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
// follow. `@` is legal in the names IDs are built from (a table can be named
// `orders@trader.db`), so a qualified ID that is already taken gets a `#<n>`
// suffix. Unique IDs are left alone, so the common single-server case, and
// any depends_on targets and links that name those IDs, keep working. The
// result depends only on the set of results, not on their order.
func qualifyCollidingIDs(results []adapterResult, topoNodes []nodes.Node, topoEdges []edges.Edge) {
	nodeOwners := map[string]int{}
	edgeOwners := map[string]int{}
	takenNodes := map[string]bool{}
	takenEdges := map[string]bool{}
	for _, n := range topoNodes {
		nodeOwners[n.Id]++
		takenNodes[n.Id] = true
	}
	for _, e := range topoEdges {
		edgeOwners[e.Id]++
		takenEdges[e.Id] = true
	}
	for _, res := range results {
		for id := range uniqueNodeIDs(res.nodes) {
			nodeOwners[id]++
		}
		for id := range uniqueEdgeIDs(res.edges) {
			edgeOwners[id]++
		}
	}
	// IDs that stay bare are final, so qualified IDs must avoid them.
	for id, c := range nodeOwners {
		if c == 1 {
			takenNodes[id] = true
		}
	}
	for id, c := range edgeOwners {
		if c == 1 {
			takenEdges[id] = true
		}
	}

	// Sorted adapters (and sorted IDs in qualify) keep `#<n>` suffixes stable.
	order := make([]int, len(results))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(results[a].name, results[b].name) })

	for _, i := range order {
		res := &results[i]
		renamed := qualify(uniqueNodeIDs(res.nodes), nodeOwners, takenNodes, res.name)
		renamedEdges := qualify(uniqueEdgeIDs(res.edges), edgeOwners, takenEdges, res.name)
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
			if q, ok := renamedEdges[e.Id]; ok {
				e.Id = q
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

// qualify maps every ID in ids with more than one owner to `<id>@<adapter>`,
// or `<id>@<adapter>#<n>` when that is taken, and marks the result taken.
func qualify(ids map[string]struct{}, owners map[string]int, taken map[string]bool, adapter string) map[string]string {
	colliding := make([]string, 0)
	for id := range ids {
		if owners[id] > 1 {
			colliding = append(colliding, id)
		}
	}
	slices.Sort(colliding)
	renamed := make(map[string]string, len(colliding))
	for _, id := range colliding {
		q := id + "@" + adapter
		for k := 2; taken[q]; k++ {
			q = fmt.Sprintf("%s@%s#%d", id, adapter, k)
		}
		taken[q] = true
		renamed[id] = q
	}
	return renamed
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

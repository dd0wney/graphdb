package algorithms

import (
	"context"
	"sort"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// LabelPropagationForTenant runs label propagation within the caller's
// tenant subgraph (audit A6c pattern, see StronglyConnectedComponentsForTenant).
// The tenant-blind LabelPropagation reads every node in the store, so a route
// must call this variant. ctx is checked once per iteration.
//
// Ties between neighbour labels go to the lowest label, so the result is
// deterministic for a given graph.
func LabelPropagationForTenant(ctx context.Context, graph storage.Storage, tenantID string, maxIterations int) (*CommunityDetectionResult, error) {
	return labelPropagationView(ctx, newTenantScopedView(graph, tenantID), maxIterations)
}

// ConnectedComponentsForTenant finds the weakly connected components of the
// caller's tenant subgraph: edges count in both directions.
func ConnectedComponentsForTenant(ctx context.Context, graph storage.Storage, tenantID string) (*CommunityDetectionResult, error) {
	return connectedComponentsView(ctx, newTenantScopedView(graph, tenantID))
}

func sortedNodeIDs(view graphView) ([]uint64, error) {
	// An incomplete node set gives a confidently wrong partition, so ADR 0003's
	// error is fatal here, as in sccView.
	allNodes, err := view.AllNodes()
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		ids = append(ids, n.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// neighbours returns the node IDs adjacent to id in either direction. An edge
// read error degrades to "no edges" for that node, as in sccView.
func neighbours(view graphView, id uint64) []uint64 {
	var out []uint64
	if edges, err := view.OutgoingEdges(id); err == nil {
		for _, e := range edges {
			out = append(out, e.ToNodeID)
		}
	}
	if edges, err := view.IncomingEdges(id); err == nil {
		for _, e := range edges {
			out = append(out, e.FromNodeID)
		}
	}
	return out
}

func labelPropagationView(ctx context.Context, view graphView, maxIterations int) (*CommunityDetectionResult, error) {
	nodeIDs, err := sortedNodeIDs(view)
	if err != nil {
		return nil, err
	}
	labels := make(map[uint64]int, len(nodeIDs))
	for i, id := range nodeIDs {
		labels[id] = i
	}

	for iter := 0; iter < maxIterations; iter++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		changed := false
		for _, id := range nodeIDs {
			counts := make(map[int]int)
			for _, n := range neighbours(view, id) {
				if l, ok := labels[n]; ok {
					counts[l]++
				}
			}
			best, bestCount := labels[id], 0
			for l, c := range counts {
				if c > bestCount || (c == bestCount && l < best) {
					best, bestCount = l, c
				}
			}
			if bestCount > 0 && best != labels[id] {
				labels[id] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return buildCommunities(nodeIDs, labels), nil
}

func connectedComponentsView(ctx context.Context, view graphView) (*CommunityDetectionResult, error) {
	nodeIDs, err := sortedNodeIDs(view)
	if err != nil {
		return nil, err
	}
	inView := make(map[uint64]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		inView[id] = true
	}
	labels := make(map[uint64]int, len(nodeIDs))
	next := 0
	for _, start := range nodeIDs {
		if _, done := labels[start]; done {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		labels[start] = next
		queue := []uint64{start}
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			for _, v := range neighbours(view, u) {
				if _, done := labels[v]; done || !inView[v] {
					continue
				}
				labels[v] = next
				queue = append(queue, v)
			}
		}
		next++
	}
	return buildCommunities(nodeIDs, labels), nil
}

// buildCommunities groups nodeIDs by label. Community IDs are assigned in the
// order the first member appears in the sorted node list, so they are stable.
// Modularity is left 0: CalculateModularity reads the whole store, not the view.
func buildCommunities(nodeIDs []uint64, labels map[uint64]int) *CommunityDetectionResult {
	var order []int
	members := make(map[int][]uint64)
	for _, id := range nodeIDs {
		l := labels[id]
		if _, seen := members[l]; !seen {
			order = append(order, l)
		}
		members[l] = append(members[l], id)
	}
	res := &CommunityDetectionResult{NodeCommunity: make(map[uint64]int, len(nodeIDs))}
	for cid, l := range order {
		nodes := members[l]
		res.Communities = append(res.Communities, &Community{ID: cid, Nodes: nodes, Size: len(nodes)})
		for _, n := range nodes {
			res.NodeCommunity[n] = cid
		}
	}
	return res
}

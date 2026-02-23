package algorithms

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

// SCCResult holds the result of Tarjan's strongly connected components algorithm.
// It embeds CommunityDetectionResult for compatibility with the existing community
// infrastructure. Modularity is always 0.0 since SCCs are structural, not quality-optimized.
type SCCResult struct {
	*CommunityDetectionResult
	LargestSCC     *Community
	SingletonCount int
}

// CondensationEdge represents a directed edge in the condensation DAG, where each
// SCC has been contracted to a single node.
type CondensationEdge struct {
	FromSCCID int
	ToSCCID   int
	EdgeCount int
}

// tarjanState holds per-node state during Tarjan's DFS.
type tarjanState struct {
	index   int
	lowlink int
	onStack bool
}

// StronglyConnectedComponents finds all SCCs using Tarjan's algorithm in O(V+E) time.
// Only outgoing edges are followed (directed graph semantics).
func StronglyConnectedComponents(graph *storage.GraphStorage) (*SCCResult, error) {
	stats := graph.GetStatistics()

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	maxID := stats.NodeCount + 10
	if maxID < 100 {
		maxID = 100
	}
	for i := uint64(1); i <= maxID; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
		if uint64(len(nodeIDs)) >= stats.NodeCount && stats.NodeCount > 0 {
			break
		}
	}

	state := make(map[uint64]*tarjanState, len(nodeIDs))
	var stack []uint64
	indexCounter := 0
	var communities []*Community
	nodeCommunity := make(map[uint64]int, len(nodeIDs))

	var strongconnect func(u uint64)
	strongconnect = func(u uint64) {
		state[u] = &tarjanState{
			index:   indexCounter,
			lowlink: indexCounter,
			onStack: true,
		}
		indexCounter++
		stack = append(stack, u)

		outEdges, _ := graph.GetOutgoingEdges(u)
		for _, edge := range outEdges {
			v := edge.ToNodeID
			if _, exists := state[v]; !exists {
				strongconnect(v)
				if state[v].lowlink < state[u].lowlink {
					state[u].lowlink = state[v].lowlink
				}
			} else if state[v].onStack {
				if state[v].index < state[u].lowlink {
					state[u].lowlink = state[v].index
				}
			}
		}

		// If u is a root node, pop the stack to form an SCC
		if state[u].lowlink == state[u].index {
			sccID := len(communities)
			var members []uint64
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				state[w].onStack = false
				members = append(members, w)
				nodeCommunity[w] = sccID
				if w == u {
					break
				}
			}

			communities = append(communities, &Community{
				ID:    sccID,
				Nodes: members,
				Size:  len(members),
			})
		}
	}

	for _, nodeID := range nodeIDs {
		if _, exists := state[nodeID]; !exists {
			strongconnect(nodeID)
		}
	}

	// Compute LargestSCC and SingletonCount
	var largestSCC *Community
	singletonCount := 0
	for _, c := range communities {
		if c.Size == 1 {
			singletonCount++
		}
		if largestSCC == nil || c.Size > largestSCC.Size {
			largestSCC = c
		}
	}

	return &SCCResult{
		CommunityDetectionResult: &CommunityDetectionResult{
			Communities:   communities,
			NodeCommunity: nodeCommunity,
			Modularity:    0.0,
		},
		LargestSCC:     largestSCC,
		SingletonCount: singletonCount,
	}, nil
}

// Condensation builds the condensation DAG from an SCC result. Each SCC becomes
// a single node; edges between SCCs are aggregated with their count.
// Runs in O(E) time over all original edges.
func Condensation(graph *storage.GraphStorage, scc *SCCResult) ([]CondensationEdge, error) {
	// Collect all edges and group by (fromSCC, toSCC)
	type edgeKey struct{ from, to int }
	counts := make(map[edgeKey]int)

	for nodeID, sccID := range scc.NodeCommunity {
		outEdges, _ := graph.GetOutgoingEdges(nodeID)
		for _, edge := range outEdges {
			targetSCC, ok := scc.NodeCommunity[edge.ToNodeID]
			if !ok {
				continue
			}
			if targetSCC == sccID {
				continue // intra-SCC edge
			}
			counts[edgeKey{sccID, targetSCC}]++
		}
	}

	result := make([]CondensationEdge, 0, len(counts))
	for key, count := range counts {
		result = append(result, CondensationEdge{
			FromSCCID: key.from,
			ToSCCID:   key.to,
			EdgeCount: count,
		})
	}

	return result, nil
}

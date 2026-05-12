package gnn

import (
	"context"
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// MessagePass runs one round of neighbor aggregation.
// For each node in nodeIDs, it gathers neighbors within 'hops' distance,
// aggregates their 'featureProp' vectors, and writes the result to 'outProp'.
func MessagePass(
	ctx context.Context,
	graph storage.Storage,
	tenantID string,
	nodeIDs []uint64,
	featureProp string,
	outProp string,
	hops int,
	agg AggregationType,
) error {
	for _, id := range nodeIDs {
		// 1. Gather neighbors
		// For the spike, we'll use a simple BFS. 
		// In production, we'd use pkg/query.ParallelTraversal.
		neighbors, err := gatherNeighbors(graph, tenantID, id, hops)
		if err != nil {
			return fmt.Errorf("failed to gather neighbors for node %d: %w", id, err)
		}

		// 2. Gather features
		var neighborVectors [][]float32
		for _, nID := range neighbors {
			err := graph.WithNodeRefForTenant(nID, tenantID, func(node *storage.Node) error {
				if val, ok := node.Properties[featureProp]; ok {
					if vec, err := val.AsVector(); err == nil {
						neighborVectors = append(neighborVectors, vec)
					}
				}
				return nil
			})
			if err != nil {
				continue
			}
		}

		// 3. Aggregate
		aggregated, err := AggregateVectors(neighborVectors, agg)
		if err != nil {
			return fmt.Errorf("aggregation failed for node %d: %w", id, err)
		}

		if aggregated == nil {
			continue // No neighbors with features
		}

		// 4. Write back
		props := map[string]storage.Value{
			outProp: storage.VectorValue(aggregated),
		}
		if err := graph.UpdateNodeForTenant(id, props, tenantID); err != nil {
			return fmt.Errorf("failed to update node %d: %w", id, err)
		}
	}

	return nil
}

func gatherNeighbors(graph storage.StorageReader, tenantID string, startID uint64, hops int) ([]uint64, error) {
	visited := make(map[uint64]bool)
	queue := []uint64{startID}
	visited[startID] = true

	for h := 0; h < hops; h++ {
		levelSize := len(queue)
		for i := 0; i < levelSize; i++ {
			curr := queue[0]
			queue = queue[1:]

			edges, err := graph.GetOutgoingEdgesForTenant(curr, tenantID)
			if err != nil {
				continue
			}

			for _, edge := range edges {
				if !visited[edge.ToNodeID] {
					visited[edge.ToNodeID] = true
					queue = append(queue, edge.ToNodeID)
				}
			}
		}
	}

	// Return all visited except the start node? 
	// Standard GNN usually includes self if self-loops exist, 
	// or as a separate term. For this spike, we'll return all found nodes.
	results := make([]uint64, 0, len(visited))
	for id := range visited {
		results = append(results, id)
	}
	return results, nil
}

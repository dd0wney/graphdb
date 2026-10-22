package query

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// StreamingQuery executes queries with streaming results
type StreamingQuery struct {
	graph *storage.GraphStorage
}

// NewStreamingQuery creates a streaming query executor
func NewStreamingQuery(graph *storage.GraphStorage) *StreamingQuery {
	return &StreamingQuery{graph: graph}
}

// StreamNodes streams all nodes matching a filter, in ascending ID order.
//
// It enumerates the live IDs rather than scanning 1..NodeCount: IDs are never
// reused, so after a delete the highest ID exceeds the live count and a scan
// stops short. The IDs are taken once, under the store's read lock, and each
// node is then read on its own, so a slow consumer never holds that lock. A
// node deleted after the IDs were taken is skipped; any other read error ends
// the stream with that error, because a skipped record makes a short result
// look complete.
func (sq *StreamingQuery) StreamNodes(
	filter func(*storage.Node) bool,
) *ResultStream {
	stream := NewResultStream(100)

	go func() {
		defer stream.Close()

		nodeIDs := sq.graph.GetAllNodeIDs()
		slices.Sort(nodeIDs)

		for _, nodeID := range nodeIDs {
			// Respect context cancellation
			select {
			case <-stream.ctx.Done():
				return
			default:
			}

			node, err := sq.graph.GetNode(nodeID)
			if errors.Is(err, storage.ErrNodeNotFound) {
				continue
			}
			if err != nil {
				stream.SendError(fmt.Errorf("stream nodes: node %d: %w", nodeID, err))
				return
			}

			if filter == nil || filter(node) {
				if !stream.Send(node) {
					return // Stream cancelled
				}
			}
		}
	}()

	return stream
}

// StreamTraversal streams nodes discovered during traversal
func (sq *StreamingQuery) StreamTraversal(
	startID uint64,
	maxDepth int,
) *ResultStream {
	stream := NewResultStream(100)

	go func() {
		defer stream.Close()

		visited := make(map[uint64]bool)
		sq.streamTraverseFrom(startID, 0, maxDepth, visited, stream)
	}()

	return stream
}

// streamTraverseFrom performs streaming BFS
func (sq *StreamingQuery) streamTraverseFrom(
	nodeID uint64,
	depth int,
	maxDepth int,
	visited map[uint64]bool,
	stream *ResultStream,
) {
	// Respect context cancellation
	select {
	case <-stream.ctx.Done():
		return
	default:
	}

	if depth > maxDepth || visited[nodeID] {
		return
	}

	visited[nodeID] = true

	node, err := sq.graph.GetNode(nodeID)
	if err != nil {
		return
	}

	if !stream.Send(node) {
		return // Stream cancelled
	}

	edges, err := sq.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return
	}

	for _, edge := range edges {
		sq.streamTraverseFrom(edge.ToNodeID, depth+1, maxDepth, visited, stream)
	}
}

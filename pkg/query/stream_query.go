package query

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// StreamingQuery executes queries with streaming results
type StreamingQuery struct {
	graph *storage.GraphStorage
}

// NewStreamingQuery creates a streaming query executor
func NewStreamingQuery(graph *storage.GraphStorage) *StreamingQuery {
	return &StreamingQuery{graph: graph}
}

// StreamNodes streams all nodes matching a filter
func (sq *StreamingQuery) StreamNodes(
	filter func(*storage.Node) bool,
) *ResultStream {
	stream := NewResultStream(100)

	go func() {
		defer stream.Close()

		stats := sq.graph.GetStatistics()
		nodeCount := int(stats.NodeCount)

		for nodeID := uint64(1); nodeID <= uint64(nodeCount); nodeID++ {
			// Respect context cancellation
			select {
			case <-stream.ctx.Done():
				return
			default:
			}

			node, err := sq.graph.GetNode(nodeID)
			if err != nil {
				continue
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

package graphql

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// createNodesResolverWithSorting creates a resolver with sorting support
func createNodesResolverWithSorting(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Query nodes by label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		nodes = sortNodes(nodes, orderBy)

		// Apply pagination if specified
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		// Apply offset
		if offsetOk && offset > 0 {
			if offset >= len(nodes) {
				return []*storage.Node{}, nil
			}
			nodes = nodes[offset:]
		}

		// Apply limit
		if limitOk && limit >= 0 {
			if limit < len(nodes) {
				nodes = nodes[:limit]
			}
		}

		return nodes, nil
	}
}

// createEdgesResolverWithSorting creates an edge resolver with sorting support
func createEdgesResolverWithSorting(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		stats := gs.GetStatistics()
		edges := make([]*storage.Edge, 0)

		for edgeID := uint64(1); edgeID <= stats.EdgeCount; edgeID++ {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				continue
			}
			edges = append(edges, edge)
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		edges = sortEdges(edges, orderBy)

		// Apply pagination if specified
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		// Apply offset
		if offsetOk && offset > 0 {
			if offset >= len(edges) {
				return []*storage.Edge{}, nil
			}
			edges = edges[offset:]
		}

		// Apply limit
		if limitOk && limit >= 0 {
			if limit < len(edges) {
				edges = edges[:limit]
			}
		}

		return edges, nil
	}
}

// createNodeConnectionResolverWithSorting creates a connection resolver with sorting
func createNodeConnectionResolverWithSorting(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Fetch all nodes with the label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		nodes = sortNodes(nodes, orderBy)

		// Parse pagination arguments
		first, firstOk := p.Args["first"].(int)
		after, afterOk := p.Args["after"].(string)
		last, lastOk := p.Args["last"].(int)
		before, beforeOk := p.Args["before"].(string)

		// Decode after cursor if provided
		startIndex := 0
		if afterOk {
			afterIndex, err := decodeCursor(after)
			if err != nil {
				return nil, err
			}
			startIndex = afterIndex + 1
		}

		// Decode before cursor if provided
		endIndex := len(nodes)
		if beforeOk {
			beforeIndex, err := decodeCursor(before)
			if err != nil {
				return nil, err
			}
			endIndex = beforeIndex
		}

		// Apply cursors to slice
		if startIndex > len(nodes) {
			startIndex = len(nodes)
		}
		if endIndex > len(nodes) {
			endIndex = len(nodes)
		}
		if startIndex > endIndex {
			startIndex = endIndex
		}

		slicedNodes := nodes[startIndex:endIndex]

		// Apply first (forward pagination)
		if firstOk {
			if first < 0 {
				first = 0
			}
			if first < len(slicedNodes) {
				slicedNodes = slicedNodes[:first]
			}
		}

		// Apply last (backward pagination)
		if lastOk {
			if last < 0 {
				last = 0
			}
			if last < len(slicedNodes) {
				slicedNodes = slicedNodes[len(slicedNodes)-last:]
				startIndex = endIndex - last
				if startIndex < 0 {
					startIndex = 0
				}
			}
		}

		// Build edges with cursors
		edges := make([]map[string]any, len(slicedNodes))
		for i, node := range slicedNodes {
			edges[i] = map[string]any{
				"cursor": encodeCursor(startIndex + i),
				"node":   node,
			}
		}

		// Calculate pageInfo
		var startCursor, endCursor *string

		if len(edges) > 0 {
			start := encodeCursor(startIndex)
			end := encodeCursor(startIndex + len(slicedNodes) - 1)
			startCursor = &start
			endCursor = &end
		}

		// Has next if we didn't reach the end (calculate even if edges is empty)
		hasNextPage := startIndex+len(slicedNodes) < len(nodes)
		// Has previous if we didn't start at the beginning
		hasPreviousPage := startIndex > 0

		pageInfo := map[string]any{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		}

		return map[string]any{
			"edges":    edges,
			"pageInfo": pageInfo,
		}, nil
	}
}

// Package main provides the telecom provider model for betweenness centrality analysis.
// Model 4 demonstrates that the invisible node pattern scales to realistic complexity.
package main

import (
	"github.com/dd0wney/cluso-graphdb/examples/ot-representative-models/models"
)

// BuildTelecomProvider creates Model 4: Telecommunications Provider (114 nodes, 253 undirected edges)
// Demonstrates cross-sector critical infrastructure dependencies and the invisible node pattern
// at scale. The telecom sector is the "infrastructure of infrastructures".
func BuildTelecomProvider(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	// Add all nodes
	b.AddNodes(models.TelecomProviderNodes)

	// Add all edge groups
	for _, group := range models.TelecomEdgeGroups {
		b.AddEdgePairs(group.Edges, group.EdgeType)
	}

	if b.Error() != nil {
		b.graph.Close()
		return nil, b.Error()
	}

	return b.Build()
}

// BuildTelecomProviderWithoutSeniorEng creates Model 4 without the Senior Network Engineer
// for removal analysis. Demonstrates how one key human node absorbs critical path traffic.
func BuildTelecomProviderWithoutSeniorEng(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	// Add filtered nodes
	b.AddNodes(models.TelecomNodesWithoutSeniorEng())

	// Add filtered edges
	for _, group := range models.TelecomEdgeGroupsWithoutSeniorEng() {
		for _, edge := range group.Edges {
			b.AddUndirectedEdge(edge[0], edge[1], group.EdgeType)
		}
	}

	if b.Error() != nil {
		b.graph.Close()
		return nil, b.Error()
	}

	return b.Build()
}

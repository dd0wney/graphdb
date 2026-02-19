// Package main provides graph building utilities for OT representative models.
package main

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// NodeDef defines a node with its properties
type NodeDef struct {
	Name     string
	Labels   []string
	Level    string
	NodeType string // NodeTypeTechnical, NodeTypeHuman, NodeTypeProcess, NodeTypeExternal
	Function string // optional function description (for telecom model)
}

// EdgeDef defines an edge between two nodes
type EdgeDef struct {
	From     string
	To       string
	EdgeType string // e.g., "TECHNICAL", "HUMAN_ACCESS", "PROCESS"
}

// GraphBuilder provides a fluent interface for building OT network models
type GraphBuilder struct {
	graph         *storage.GraphStorage
	nodeNames     map[uint64]string
	nodeTypes     map[uint64]string
	nodeLevels    map[uint64]string
	nodeFunctions map[uint64]string
	nodeIDs       map[string]uint64
	err           error // captures first error for deferred checking
}

// NewGraphBuilder creates a new graph builder with storage at the given path
func NewGraphBuilder(dataPath string) (*GraphBuilder, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	return &GraphBuilder{
		graph:         graph,
		nodeNames:     make(map[uint64]string),
		nodeTypes:     make(map[uint64]string),
		nodeLevels:    make(map[uint64]string),
		nodeFunctions: make(map[uint64]string),
		nodeIDs:       make(map[string]uint64),
	}, nil
}

// AddNode creates a single node and tracks its metadata
func (b *GraphBuilder) AddNode(def NodeDef) *GraphBuilder {
	if b.err != nil {
		return b
	}

	props := map[string]storage.Value{
		"name":      storage.StringValue(def.Name),
		"level":     storage.StringValue(def.Level),
		"node_type": storage.StringValue(def.NodeType),
	}
	if def.Function != "" {
		props["function"] = storage.StringValue(def.Function)
	}

	node, err := b.graph.CreateNode(def.Labels, props)
	if err != nil {
		b.err = fmt.Errorf("failed to create node %s: %w", def.Name, err)
		return b
	}

	b.nodeNames[node.ID] = def.Name
	b.nodeTypes[node.ID] = def.NodeType
	b.nodeLevels[node.ID] = def.Level
	if def.Function != "" {
		b.nodeFunctions[node.ID] = def.Function
	}
	b.nodeIDs[def.Name] = node.ID

	return b
}

// AddNodes creates multiple nodes from definitions
func (b *GraphBuilder) AddNodes(defs []NodeDef) *GraphBuilder {
	for _, def := range defs {
		b.AddNode(def)
		if b.err != nil {
			return b
		}
	}
	return b
}

// AddUndirectedEdge creates edges in both directions to simulate undirected behaviour
func (b *GraphBuilder) AddUndirectedEdge(from, to, edgeType string) *GraphBuilder {
	if b.err != nil {
		return b
	}

	fromID, ok := b.nodeIDs[from]
	if !ok {
		b.err = fmt.Errorf("node not found: %s", from)
		return b
	}

	toID, ok := b.nodeIDs[to]
	if !ok {
		b.err = fmt.Errorf("node not found: %s", to)
		return b
	}

	props := map[string]storage.Value{}

	if _, err := b.graph.CreateEdge(fromID, toID, edgeType, props, 1.0); err != nil {
		b.err = fmt.Errorf("failed to create forward edge %s -> %s: %w", from, to, err)
		return b
	}

	if _, err := b.graph.CreateEdge(toID, fromID, edgeType, props, 1.0); err != nil {
		b.err = fmt.Errorf("failed to create reverse edge %s -> %s: %w", to, from, err)
		return b
	}

	return b
}

// AddEdges creates multiple undirected edges from definitions
func (b *GraphBuilder) AddEdges(defs []EdgeDef) *GraphBuilder {
	for _, def := range defs {
		b.AddUndirectedEdge(def.From, def.To, def.EdgeType)
		if b.err != nil {
			return b
		}
	}
	return b
}

// AddEdgePairs creates undirected edges from simple [from, to] pairs with a fixed edge type
func (b *GraphBuilder) AddEdgePairs(pairs [][2]string, edgeType string) *GraphBuilder {
	for _, pair := range pairs {
		b.AddUndirectedEdge(pair[0], pair[1], edgeType)
		if b.err != nil {
			return b
		}
	}
	return b
}

// AddEdgePairsWithAutoType creates undirected edges with automatic type detection:
// - PROCESS if either node is NodeTypeProcess
// - HUMAN_ACCESS if either node is NodeTypeHuman
// - defaultType otherwise
func (b *GraphBuilder) AddEdgePairsWithAutoType(pairs [][2]string, defaultType string) *GraphBuilder {
	for _, pair := range pairs {
		edgeType := defaultType
		fromID := b.nodeIDs[pair[0]]
		toID := b.nodeIDs[pair[1]]

		if b.nodeTypes[fromID] == NodeTypeProcess || b.nodeTypes[toID] == NodeTypeProcess {
			edgeType = "PROCESS"
		} else if b.nodeTypes[fromID] == NodeTypeHuman || b.nodeTypes[toID] == NodeTypeHuman {
			edgeType = "HUMAN_ACCESS"
		}

		b.AddUndirectedEdge(pair[0], pair[1], edgeType)
		if b.err != nil {
			return b
		}
	}
	return b
}

// Error returns any error that occurred during building
func (b *GraphBuilder) Error() error {
	return b.err
}

// Build finalizes and returns the Metadata (closes the builder, but not the graph).
// The NodeFunctions map is always included but may be empty for non-telecom models.
func (b *GraphBuilder) Build() (*Metadata, error) {
	if b.err != nil {
		b.graph.Close()
		return nil, b.err
	}

	return &Metadata{
		Graph:         b.graph,
		NodeNames:     b.nodeNames,
		NodeTypes:     b.nodeTypes,
		NodeLevels:    b.nodeLevels,
		NodeFunctions: b.nodeFunctions,
		NodeIDs:       b.nodeIDs,
	}, nil
}

// FilteredGraphBuilder extends GraphBuilder to only include specified edge types
type FilteredGraphBuilder struct {
	*GraphBuilder
	allowedTypes map[string]bool
}

// NewFilteredGraphBuilder creates a builder that filters edges by type
func NewFilteredGraphBuilder(dataPath string, allowedTypes []string) (*FilteredGraphBuilder, error) {
	gb, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]bool, len(allowedTypes))
	for _, t := range allowedTypes {
		allowed[t] = true
	}

	return &FilteredGraphBuilder{
		GraphBuilder: gb,
		allowedTypes: allowed,
	}, nil
}

// AddEdges creates edges only if their type is in the allowed set
func (fb *FilteredGraphBuilder) AddEdges(defs []EdgeDef) *FilteredGraphBuilder {
	for _, def := range defs {
		if fb.allowedTypes[def.EdgeType] {
			fb.AddUndirectedEdge(def.From, def.To, def.EdgeType)
			if fb.err != nil {
				return fb
			}
		}
	}
	return fb
}

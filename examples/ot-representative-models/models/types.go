// Package models provides node and edge definitions for OT network models.
// These definitions are used by the builder to construct graph instances.
package models

// NodeDef defines a node with its properties.
type NodeDef struct {
	Name     string
	Labels   []string
	Level    string
	NodeType string // NodeTypeTechnical, NodeTypeHuman, NodeTypeProcess, NodeTypeExternal
	Function string // optional function description (for telecom model)
}

// EdgeDef defines an edge between two nodes.
type EdgeDef struct {
	From     string
	To       string
	EdgeType string // EdgeTypeTechnical, EdgeTypeHumanAccess, EdgeTypeProcess
}

// Node type constants used throughout the codebase.
const (
	NodeTypeTechnical = "technical"
	NodeTypeHuman     = "human"
	NodeTypeProcess   = "process"
	NodeTypeExternal  = "external"
)

// Edge type constants used for layer analysis.
const (
	EdgeTypeTechnical   = "TECHNICAL"
	EdgeTypeHumanAccess = "HUMAN_ACCESS"
	EdgeTypeProcess     = "PROCESS"
)

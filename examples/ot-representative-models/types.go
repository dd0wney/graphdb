// Package main provides unified types for OT representative models.
package main

import (
	"github.com/dd0wney/cluso-graphdb/examples/ot-representative-models/models"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Re-export node and edge type constants from models package for convenience.
const (
	NodeTypeTechnical = models.NodeTypeTechnical
	NodeTypeHuman     = models.NodeTypeHuman
	NodeTypeProcess   = models.NodeTypeProcess
	NodeTypeExternal  = models.NodeTypeExternal

	EdgeTypeTechnical   = models.EdgeTypeTechnical
	EdgeTypeHumanAccess = models.EdgeTypeHumanAccess
	EdgeTypeProcess     = models.EdgeTypeProcess
)

// Type aliases for model types.
type (
	NodeDef   = models.NodeDef
	EdgeDef   = models.EdgeDef
	EdgeGroup = models.EdgeGroup
)

// Metadata holds node mappings for a graph model.
// This unified type replaces the former ModelMetadata and TelecomMetadata.
type Metadata struct {
	Graph         *storage.GraphStorage
	NodeNames     map[uint64]string // ID -> display name
	NodeTypes     map[uint64]string // ID -> "technical", "human", "process", "external"
	NodeLevels    map[uint64]string // ID -> level description
	NodeFunctions map[uint64]string // ID -> function description (optional, used by telecom)
	NodeIDs       map[string]uint64 // name -> ID (reverse lookup)
}

// BCResult holds betweenness centrality results for a single node.
type BCResult struct {
	Name     string  `json:"name"`
	BC       float64 `json:"bc"`
	NodeType string  `json:"node_type"`
	Level    string  `json:"level"`
	Rank     int     `json:"rank"`
}

// AnalysisOptions configures optional analysis features.
type AnalysisOptions struct {
	IncludeTypeCounts   bool // Include node type counts (for telecom)
	IncludeGatewayStats bool // Include gateway analysis (for telecom)
}

// GatewayResult holds BC analysis for sector gateways.
type GatewayResult struct {
	Name              string   `json:"name"`
	BC                float64  `json:"bc"`
	ExternalNodeCount int      `json:"external_node_count"`
	ExternalNodes     []string `json:"external_nodes"`
}

// CascadeResult holds cascade failure analysis for a node.
type CascadeResult struct {
	NodeName          string              `json:"node_name"`
	SectorsAffected   int                 `json:"sectors_affected"`
	ExternalNodesLost int                 `json:"external_nodes_lost"`
	SectorDetails     map[string][]string `json:"sector_details"`
}

// ModelResult holds complete analysis results for a model.
// This unified type replaces the former ModelResult and TelecomResult.
type ModelResult struct {
	ModelName           string          `json:"model_name"`
	NodeCount           int             `json:"node_count"`
	EdgeCount           int             `json:"edge_count"`
	Rankings            []BCResult      `json:"rankings"`
	InvisibleNodeShare  float64         `json:"invisible_node_share"`
	TopInvisibleNode    string          `json:"top_invisible_node,omitempty"`
	TopInvisibleBC      float64         `json:"top_invisible_bc,omitempty"`
	TopTechnicalNode    string          `json:"top_technical_node,omitempty"`
	TopTechnicalBC      float64         `json:"top_technical_bc,omitempty"`
	InvisibleMultiplier float64         `json:"invisible_multiplier,omitempty"`
	NodeTypeCounts      map[string]int  `json:"node_type_counts,omitempty"`
	GatewayAnalysis     []GatewayResult `json:"gateway_analysis,omitempty"`
	CascadeFailures     []CascadeResult `json:"cascade_failures,omitempty"`
}

// AllResults contains results from all models.
type AllResults struct {
	StevesUtility    ModelResult  `json:"steves_utility"`
	StevesRemoval    ModelResult  `json:"steves_utility_without_steve"`
	ChemicalFacility ModelResult  `json:"chemical_facility"`
	WaterFlat        ModelResult  `json:"water_treatment_flat"`
	WaterVLAN        ModelResult  `json:"water_treatment_vlan"`
	TelecomProvider  *ModelResult `json:"telecom_provider,omitempty"`
}

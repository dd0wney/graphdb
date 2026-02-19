// Package main provides representative OT network models for betweenness centrality analysis.
// These models demonstrate the "invisible node" problem in critical infrastructure security.
package main

import (
	"github.com/dd0wney/cluso-graphdb/examples/ot-representative-models/models"
)

// BuildStevesUtility creates Model 1: Steve's Utility (33 nodes, 70 undirected edges)
// Demonstrates how one helpful senior OT technician accumulates cross-domain access,
// creating an invisible single point of failure.
func BuildStevesUtility(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(models.StevesUtilityNodes).
		AddEdges(models.StevesUtilityEdges).
		Build()
}

// BuildStevesUtilityFiltered creates Model 1 with all 33 nodes but only edges
// whose type is in the allowedTypes set. This enables layer-by-layer BC analysis:
//   - ["TECHNICAL"]                          → data plane only (things)
//   - ["TECHNICAL", "HUMAN_ACCESS"]          → things + people
//   - ["TECHNICAL", "PROCESS"]               → things + organisational processes
//   - ["TECHNICAL", "HUMAN_ACCESS", "PROCESS"] → composite (all)
func BuildStevesUtilityFiltered(dataPath string, allowedTypes []string) (*Metadata, error) {
	fb, err := NewFilteredGraphBuilder(dataPath, allowedTypes)
	if err != nil {
		return nil, err
	}

	fb.AddNodes(models.StevesUtilityNodes)
	fb.AddEdges(models.StevesUtilityEdges)

	return fb.Build()
}

// BuildStevesUtilityWithoutSteve creates Model 1 without Steve for removal analysis.
func BuildStevesUtilityWithoutSteve(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(models.StevesUtilityNodesWithoutSteve()).
		AddEdges(models.StevesUtilityEdgesWithoutSteve()).
		Build()
}

// BuildChemicalFacility creates Model 2: Chemical Facility (24 nodes, 37 undirected edges)
// Demonstrates IT/OT bridge concentration through the IT_OT_Coord role.
func BuildChemicalFacility(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(models.ChemicalFacilityNodes).
		AddEdgePairsWithAutoType(models.ChemicalFacilityEdgePairs, "TECHNICAL").
		Build()
}

// BuildWaterTreatmentFlat creates Model 3a: Water Treatment Flat (13 nodes, 13 undirected edges)
// Three switches in full mesh topology.
func BuildWaterTreatmentFlat(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(models.WaterTreatmentNodes).
		AddEdgePairs(models.WaterFlatEdgePairs, "TECHNICAL").
		Build()
}

// BuildWaterTreatmentVLAN creates Model 3b: Water Treatment VLAN (14 nodes, 13 undirected edges)
// Star topology through L3 core switch. Demonstrates how VLAN segmentation
// concentrates betweenness centrality.
func BuildWaterTreatmentVLAN(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(models.WaterVLANNodes()).
		AddEdgePairs(models.WaterVLANEdgePairs, "TECHNICAL").
		Build()
}

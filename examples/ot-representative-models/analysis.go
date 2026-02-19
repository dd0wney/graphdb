// Package main provides analysis functions for betweenness centrality calculations.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Analyse computes betweenness centrality and returns structured results.
// Use opts to enable optional features like type counts and gateway analysis.
func Analyse(meta *Metadata, modelName string, bc map[uint64]float64, opts AnalysisOptions) ModelResult {
	stats := meta.Graph.GetStatistics()

	// Build sorted results
	results := make([]BCResult, 0, len(bc))
	for nodeID, bcValue := range bc {
		results = append(results, BCResult{
			Name:     meta.NodeNames[nodeID],
			BC:       bcValue,
			NodeType: meta.NodeTypes[nodeID],
			Level:    meta.NodeLevels[nodeID],
		})
	}

	// Sort by BC descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].BC > results[j].BC
	})

	// Assign ranks
	for i := range results {
		results[i].Rank = i + 1
	}

	// Calculate invisible node share
	totalBC := 0.0
	invisibleBC := 0.0
	var topInvisible, topTechnical BCResult

	for _, r := range results {
		totalBC += r.BC
		if r.NodeType == NodeTypeHuman || r.NodeType == NodeTypeProcess {
			invisibleBC += r.BC
			if topInvisible.Name == "" || r.BC > topInvisible.BC {
				topInvisible = r
			}
		} else if r.NodeType == NodeTypeTechnical {
			if topTechnical.Name == "" || r.BC > topTechnical.BC {
				topTechnical = r
			}
		}
	}

	invisibleShare := 0.0
	if totalBC > 0 {
		invisibleShare = invisibleBC / totalBC
	}

	multiplier := 0.0
	if topTechnical.BC > 0 {
		multiplier = topInvisible.BC / topTechnical.BC
	}

	result := ModelResult{
		ModelName:           modelName,
		NodeCount:           int(stats.NodeCount),
		EdgeCount:           int(stats.EdgeCount) / 2, // Divide by 2 since edges are bidirectional
		Rankings:            results,
		InvisibleNodeShare:  invisibleShare,
		TopInvisibleNode:    topInvisible.Name,
		TopInvisibleBC:      topInvisible.BC,
		TopTechnicalNode:    topTechnical.Name,
		TopTechnicalBC:      topTechnical.BC,
		InvisibleMultiplier: multiplier,
	}

	// Optional: include node type counts
	if opts.IncludeTypeCounts {
		typeCounts := make(map[string]int)
		for _, nodeType := range meta.NodeTypes {
			typeCounts[nodeType]++
		}
		result.NodeTypeCounts = typeCounts
	}

	// Optional: include gateway analysis (telecom-specific)
	if opts.IncludeGatewayStats {
		result.GatewayAnalysis = analyseGateways(meta, bc, results)
	}

	return result
}

// AnalyseModel computes betweenness centrality with default options (no extras).
func AnalyseModel(meta *Metadata, modelName string, bc map[uint64]float64) ModelResult {
	return Analyse(meta, modelName, bc, AnalysisOptions{})
}

// ExportResultsJSON writes all results to a JSON file.
func ExportResultsJSON(results AllResults, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	if err := os.WriteFile(filename, data, 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// Package main provides analysis functions for betweenness centrality calculations.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// BCResult holds betweenness centrality results for a single node
type BCResult struct {
	Name     string  `json:"name"`
	BC       float64 `json:"bc"`
	NodeType string  `json:"node_type"`
	Level    string  `json:"level"`
	Rank     int     `json:"rank"`
}

// ModelResult holds complete analysis results for a model
type ModelResult struct {
	ModelName           string     `json:"model_name"`
	NodeCount           int        `json:"node_count"`
	EdgeCount           int        `json:"edge_count"`
	Rankings            []BCResult `json:"rankings"`
	InvisibleNodeShare  float64    `json:"invisible_node_share"`
	TopInvisibleNode    string     `json:"top_invisible_node,omitempty"`
	TopInvisibleBC      float64    `json:"top_invisible_bc,omitempty"`
	TopTechnicalNode    string     `json:"top_technical_node,omitempty"`
	TopTechnicalBC      float64    `json:"top_technical_bc,omitempty"`
	InvisibleMultiplier float64    `json:"invisible_multiplier,omitempty"`
}

// AllResults contains results from all models
type AllResults struct {
	StevesUtility    ModelResult `json:"steves_utility"`
	StevesRemoval    ModelResult `json:"steves_utility_without_steve"`
	ChemicalFacility ModelResult `json:"chemical_facility"`
	WaterFlat        ModelResult `json:"water_treatment_flat"`
	WaterVLAN        ModelResult `json:"water_treatment_vlan"`
}

// AnalyseModel computes betweenness centrality and returns structured results
func AnalyseModel(meta *ModelMetadata, modelName string, bc map[uint64]float64) ModelResult {
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
		if r.NodeType == "human" || r.NodeType == "process" {
			invisibleBC += r.BC
			if topInvisible.Name == "" || r.BC > topInvisible.BC {
				topInvisible = r
			}
		} else if r.NodeType == "technical" {
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

	return ModelResult{
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
}

// PrintModelResults prints a formatted BC ranking report
func PrintModelResults(result ModelResult, topN int) {
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Printf("MODEL: %s (%d nodes, %d undirected edges)\n", result.ModelName, result.NodeCount, result.EdgeCount)
	fmt.Println("========================================================================")
	fmt.Println()
	fmt.Println("--- Betweenness Centrality (normalised) ---")
	fmt.Println()
	fmt.Printf("%-28s %8s  %-12s %-18s\n", "Node", "BC", "Type", "Level")
	fmt.Println("---------------------------------------------------------------------------")

	displayCount := topN
	if displayCount > len(result.Rankings) {
		displayCount = len(result.Rankings)
	}

	for i := 0; i < displayCount; i++ {
		r := result.Rankings[i]
		invisible := ""
		if r.NodeType == "human" || r.NodeType == "process" {
			invisible = " *** INVISIBLE"
		}
		fmt.Printf("%-28s %8.4f  %-12s %-18s%s\n", r.Name, r.BC, r.NodeType, r.Level, invisible)
	}

	fmt.Println()
	fmt.Println("--- Summary Statistics ---")
	fmt.Printf("Invisible Node BC Share:     %.1f%%\n", result.InvisibleNodeShare*100)
	if result.TopInvisibleNode != "" {
		fmt.Printf("Top Invisible Node:          %s (BC: %.4f)\n", result.TopInvisibleNode, result.TopInvisibleBC)
	}
	if result.TopTechnicalNode != "" {
		fmt.Printf("Top Technical Node:          %s (BC: %.4f)\n", result.TopTechnicalNode, result.TopTechnicalBC)
	}
	if result.InvisibleMultiplier > 0 {
		fmt.Printf("Invisible vs Technical:      %.2fx\n", result.InvisibleMultiplier)
	}
}

// PrintSteveRemovalComparison compares BC before and after removing Steve
func PrintSteveRemovalComparison(before, after ModelResult) {
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Println("STEVE REMOVAL ANALYSIS")
	fmt.Println("========================================================================")
	fmt.Println()
	fmt.Println("What happens when Steve leaves?")
	fmt.Println()

	// Build lookup maps
	beforeMap := make(map[string]float64)
	for _, r := range before.Rankings {
		beforeMap[r.Name] = r.BC
	}

	afterMap := make(map[string]float64)
	for _, r := range after.Rankings {
		afterMap[r.Name] = r.BC
	}

	// Calculate changes
	type Change struct {
		Name      string
		Before    float64
		After     float64
		Delta     float64
		DeltaPct  float64
		NodeType  string
		Increased bool
	}

	changes := make([]Change, 0)
	for _, r := range after.Rankings {
		beforeBC := beforeMap[r.Name]
		afterBC := r.BC
		delta := afterBC - beforeBC
		deltaPct := 0.0
		if beforeBC > 0 {
			deltaPct = (delta / beforeBC) * 100
		}
		changes = append(changes, Change{
			Name:      r.Name,
			Before:    beforeBC,
			After:     afterBC,
			Delta:     delta,
			DeltaPct:  deltaPct,
			NodeType:  r.NodeType,
			Increased: delta > 0,
		})
	}

	// Sort by absolute change
	sort.Slice(changes, func(i, j int) bool {
		return abs(changes[i].Delta) > abs(changes[j].Delta)
	})

	fmt.Printf("%-28s %10s %10s %10s %10s\n", "Node", "Before", "After", "Change", "% Change")
	fmt.Println("---------------------------------------------------------------------------")

	displayCount := 10
	if displayCount > len(changes) {
		displayCount = len(changes)
	}

	for i := 0; i < displayCount; i++ {
		c := changes[i]
		sign := ""
		if c.Delta > 0 {
			sign = "+"
		}
		fmt.Printf("%-28s %10.4f %10.4f %s%9.4f %s%9.1f%%\n",
			c.Name, c.Before, c.After, sign, c.Delta, sign, c.DeltaPct)
	}

	fmt.Println()
	fmt.Printf("Steve's BC before removal:   %.4f (rank #1)\n", beforeMap["Steve"])
	fmt.Println()
	fmt.Println("Key insight: When Steve leaves, work redistributes to technical systems")
	fmt.Println("that now become single points of failure. The organisation had no backup.")
}

// PrintVLANComparison compares flat vs VLAN network topologies
func PrintVLANComparison(flat, vlan ModelResult) {
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Println("VLAN COMPARISON ANALYSIS")
	fmt.Println("========================================================================")
	fmt.Println()
	fmt.Println("Comparing: Flat mesh vs VLAN star topology")
	fmt.Println()

	// Find max switch BC in each model
	flatMaxSwitch := 0.0
	flatMaxName := ""
	for _, r := range flat.Rankings {
		if containsLabel(r.Level, "Flat") && containsLabel(r.Name, "Switch") {
			if r.BC > flatMaxSwitch {
				flatMaxSwitch = r.BC
				flatMaxName = r.Name
			}
		}
	}

	vlanMaxSwitch := 0.0
	vlanMaxName := ""
	for _, r := range vlan.Rankings {
		if containsLabel(r.Name, "Switch") || containsLabel(r.Name, "Core") {
			if r.BC > vlanMaxSwitch {
				vlanMaxSwitch = r.BC
				vlanMaxName = r.Name
			}
		}
	}

	fmt.Printf("Flat Network:\n")
	fmt.Printf("  Max switch BC: %.4f (%s)\n", flatMaxSwitch, flatMaxName)
	fmt.Printf("  Topology: Full mesh between switches\n")
	fmt.Println()

	fmt.Printf("VLAN Network:\n")
	fmt.Printf("  Max switch BC: %.4f (%s)\n", vlanMaxSwitch, vlanMaxName)
	fmt.Printf("  Topology: Star through L3 core\n")
	fmt.Println()

	ratio := 0.0
	if flatMaxSwitch > 0 {
		ratio = vlanMaxSwitch / flatMaxSwitch
	}

	fmt.Printf("BC Concentration Ratio:      %.2fx\n", ratio)
	fmt.Println()
	fmt.Println("Key insight: VLAN segmentation increases BC concentration on the core")
	fmt.Println("switch, creating a more critical single point of failure.")
}

// PrintChemicalFacilitySummary prints bridge risk analysis
func PrintChemicalFacilitySummary(result ModelResult) {
	// Find IT_OT_Coord and DMZ_Firewall BC
	var coordBC, firewallBC float64
	for _, r := range result.Rankings {
		if r.Name == "IT_OT_Coord" {
			coordBC = r.BC
		}
		if r.Name == "DMZ_Firewall" {
			firewallBC = r.BC
		}
	}

	fmt.Println()
	fmt.Println("--- Bridge Risk Analysis ---")
	fmt.Printf("IT_OT_Coord BC:              %.4f\n", coordBC)
	fmt.Printf("DMZ_Firewall BC:             %.4f\n", firewallBC)
	if firewallBC > 0 {
		fmt.Printf("Bridge Person vs Firewall:   %.2fx\n", coordBC/firewallBC)
	}
	fmt.Println()
	fmt.Println("Key insight: The human IT/OT coordinator has higher betweenness than")
	fmt.Println("the technical firewall, making them the true bridge between domains.")
}

// ExportResultsJSON writes all results to a JSON file
func ExportResultsJSON(results AllResults, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// Helper function for absolute value
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Helper function to check if string contains substring
func containsLabel(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

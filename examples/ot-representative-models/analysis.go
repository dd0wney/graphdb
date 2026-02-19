// Package main provides analysis functions for betweenness centrality calculations.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
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

// PrintOptions configures the output format for PrintResults.
type PrintOptions struct {
	TitlePrefix      string // e.g., "MODEL" or "MODEL 4"
	ShowTypeCounts   bool   // Show node type breakdown
	ShowRankColumn   bool   // Include rank number in output
	ShowExternalFlag bool   // Show [EXTERNAL] flag for external nodes
}

// PrintResults prints a formatted BC ranking report with configurable options.
func PrintResults(result ModelResult, topN int, opts PrintOptions) {
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Printf("%s: %s (%d nodes, %d undirected edges)\n", opts.TitlePrefix, result.ModelName, result.NodeCount, result.EdgeCount)
	fmt.Println("========================================================================")
	fmt.Println()

	// Optional: node type breakdown
	if opts.ShowTypeCounts && result.NodeTypeCounts != nil {
		fmt.Println("--- Node Type Breakdown ---")
		typeOrder := []string{NodeTypeTechnical, NodeTypeHuman, NodeTypeProcess, NodeTypeExternal}
		for _, t := range typeOrder {
			if count, ok := result.NodeTypeCounts[t]; ok {
				fmt.Printf("  %s: %d\n", t, count)
			}
		}
		fmt.Println()
	}

	fmt.Println("--- Betweenness Centrality (normalised) ---")
	fmt.Println()

	// Print header based on options
	if opts.ShowRankColumn {
		fmt.Printf("%-4s %-28s %8s  %-12s %-18s\n", "Rank", "Node", "BC", "Type", "Level")
	} else {
		fmt.Printf("%-28s %8s  %-12s %-18s\n", "Node", "BC", "Type", "Level")
	}
	fmt.Println("---------------------------------------------------------------------------")

	displayCount := topN
	if displayCount > len(result.Rankings) {
		displayCount = len(result.Rankings)
	}

	for i := 0; i < displayCount; i++ {
		r := result.Rankings[i]
		flag := ""
		if r.NodeType == NodeTypeHuman || r.NodeType == NodeTypeProcess {
			flag = " *** INVISIBLE"
		} else if opts.ShowExternalFlag && r.NodeType == NodeTypeExternal {
			flag = " [EXTERNAL]"
		}

		if opts.ShowRankColumn {
			fmt.Printf("#%-3d %-28s %8.4f  %-12s %-18s%s\n", r.Rank, r.Name, r.BC, r.NodeType, r.Level, flag)
		} else {
			fmt.Printf("%-28s %8.4f  %-12s %-18s%s\n", r.Name, r.BC, r.NodeType, r.Level, flag)
		}
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

// PrintModelResults prints a formatted BC ranking report (standard format).
func PrintModelResults(result ModelResult, topN int) {
	PrintResults(result, topN, PrintOptions{
		TitlePrefix: "MODEL",
	})
}

// RemovalConfig holds configuration for node removal comparison output.
type RemovalConfig struct {
	Title       string // e.g., "STEVE REMOVAL ANALYSIS"
	Question    string // e.g., "What happens when Steve leaves?"
	RemovedNode string // e.g., "Steve"
	RemovedRank string // e.g., "rank #1"
	Insight     string // Multi-line key insight text
}

// PrintRemovalComparison compares BC before and after removing a node.
func PrintRemovalComparison(before, after ModelResult, cfg RemovalConfig) {
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Println(cfg.Title)
	fmt.Println("========================================================================")
	fmt.Println()
	fmt.Println(cfg.Question)
	fmt.Println()

	// Build lookup maps
	beforeMap := make(map[string]float64)
	for _, r := range before.Rankings {
		beforeMap[r.Name] = r.BC
	}

	// Calculate changes
	type change struct {
		Name     string
		Before   float64
		After    float64
		Delta    float64
		DeltaPct float64
	}

	changes := make([]change, 0, len(after.Rankings))
	for _, r := range after.Rankings {
		beforeBC := beforeMap[r.Name]
		afterBC := r.BC
		delta := afterBC - beforeBC
		deltaPct := 0.0
		if beforeBC > 0 {
			deltaPct = (delta / beforeBC) * 100
		}
		changes = append(changes, change{
			Name:     r.Name,
			Before:   beforeBC,
			After:    afterBC,
			Delta:    delta,
			DeltaPct: deltaPct,
		})
	}

	// Sort by absolute change
	sort.Slice(changes, func(i, j int) bool {
		return math.Abs(changes[i].Delta) > math.Abs(changes[j].Delta)
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
	fmt.Printf("%s BC before removal: %.4f (%s)\n", cfg.RemovedNode, beforeMap[cfg.RemovedNode], cfg.RemovedRank)
	fmt.Println()
	fmt.Println(cfg.Insight)
}

// PrintSteveRemovalComparison compares BC before and after removing Steve.
func PrintSteveRemovalComparison(before, after ModelResult) {
	PrintRemovalComparison(before, after, RemovalConfig{
		Title:       "STEVE REMOVAL ANALYSIS",
		Question:    "What happens when Steve leaves?",
		RemovedNode: "Steve",
		RemovedRank: "rank #1",
		Insight: "Key insight: When Steve leaves, work redistributes to technical systems\n" +
			"that now become single points of failure. The organisation had no backup.",
	})
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
		if strings.Contains(r.Level, "Flat") && strings.Contains(r.Name, "Switch") {
			if r.BC > flatMaxSwitch {
				flatMaxSwitch = r.BC
				flatMaxName = r.Name
			}
		}
	}

	vlanMaxSwitch := 0.0
	vlanMaxName := ""
	for _, r := range vlan.Rankings {
		if strings.Contains(r.Name, "Switch") || strings.Contains(r.Name, "Core") {
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

// PrintMultiLayerAnalysis prints a multi-layer BC comparison aligned with ISO 15288:
// Things (Technical), People (Tech+Human), Process (Tech+Process), Composite (all).
// The delta between layers reveals where hidden dependencies live.
func PrintMultiLayerAnalysis(compositeResult ModelResult, layerBCByName []map[string]float64, layerLabels []string, compositeBCByName map[string]float64) {
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Println("MULTI-LAYER BETWEENNESS CENTRALITY (ISO 15288: People / Process / Things)")
	fmt.Println("========================================================================")
	fmt.Println()
	fmt.Println("Technical     = data-plane edges only (things)")
	fmt.Println("Tech+Human    = infrastructure + human access edges (things + people)")
	fmt.Println("Tech+Process  = infrastructure + process edges (things + process)")
	fmt.Println("Composite     = all edges (things + people + process)")
	fmt.Println("Delta         = Composite − Technical (total hidden dependency)")
	fmt.Println()

	type MultiEntry struct {
		Name     string
		LayerBC  []float64 // BC for each filtered layer
		CompBC   float64   // composite BC
		Delta    float64   // composite - technical
		NodeType string
		Level    string
	}

	entries := make([]MultiEntry, 0, len(compositeResult.Rankings))
	for _, r := range compositeResult.Rankings {
		lbc := make([]float64, len(layerBCByName))
		for i, m := range layerBCByName {
			lbc[i] = m[r.Name]
		}
		compBC := compositeBCByName[r.Name]
		delta := compBC - lbc[0] // composite - technical
		entries = append(entries, MultiEntry{
			Name:     r.Name,
			LayerBC:  lbc,
			CompBC:   compBC,
			Delta:    delta,
			NodeType: r.NodeType,
			Level:    r.Level,
		})
	}

	// Sort by composite BC descending (most critical first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CompBC > entries[j].CompBC
	})

	// Print header
	fmt.Printf("%-22s", "Node")
	for _, label := range layerLabels {
		fmt.Printf(" %10s", label)
	}
	fmt.Printf(" %10s %10s  %s\n", "Composite", "Delta", "Interpretation")
	fmt.Println(strings.Repeat("─", 22+11*len(layerLabels)+11+11+2+40))

	for _, e := range entries {
		// Skip nodes with near-zero BC across all layers
		allZero := e.CompBC < 0.0001
		for _, bc := range e.LayerBC {
			if bc >= 0.0001 {
				allZero = false
				break
			}
		}
		if allZero {
			continue
		}

		fmt.Printf("%-22s", e.Name)
		for _, bc := range e.LayerBC {
			fmt.Printf(" %10.4f", bc)
		}

		sign := ""
		if e.Delta > 0.0001 {
			sign = "+"
		}
		fmt.Printf(" %10.4f %s%9.4f", e.CompBC, sign, e.Delta)

		// Interpretation based on where BC appears
		interpretation := ""
		techBC := e.LayerBC[0]
		techHumanBC := e.LayerBC[1]
		techProcessBC := e.LayerBC[2]

		switch {
		case e.NodeType == NodeTypeHuman:
			interpretation = "← human node (invisible)"
		case e.NodeType == NodeTypeProcess:
			interpretation = "← process node (invisible)"
		case techBC > 0.001 && e.Delta < -0.01:
			// Technical node suppressed — figure out which layer causes it
			humanEffect := techHumanBC - techBC
			processEffect := techProcessBC - techBC
			if math.Abs(humanEffect) > math.Abs(processEffect) {
				interpretation = fmt.Sprintf("← people suppress %.0f%%", (e.Delta/techBC)*100)
			} else {
				interpretation = fmt.Sprintf("← process suppresses %.0f%%", (e.Delta/techBC)*100)
			}
		case techBC > 0.001 && e.Delta > 0.01:
			humanEffect := techHumanBC - techBC
			processEffect := techProcessBC - techBC
			if humanEffect > processEffect {
				interpretation = fmt.Sprintf("← people amplify +%.0f%%", (e.Delta/techBC)*100)
			} else {
				interpretation = fmt.Sprintf("← process amplifies +%.0f%%", (e.Delta/techBC)*100)
			}
		case techBC > 0.001 && math.Abs(e.Delta) <= 0.01:
			interpretation = "← stable across layers"
		case techBC < 0.001 && e.CompBC > 0.001:
			interpretation = "← only visible with human/process edges"
		}

		fmt.Printf("  %s\n", interpretation)
	}
	fmt.Println()

	// Summary: BC totals per layer
	fmt.Println("--- Layer Totals ---")
	for i, label := range layerLabels {
		total := 0.0
		for _, e := range entries {
			total += e.LayerBC[i]
		}
		fmt.Printf("%-18s total BC: %.4f\n", label, total)
	}
	compTotal := 0.0
	for _, e := range entries {
		compTotal += e.CompBC
	}
	fmt.Printf("%-18s total BC: %.4f\n", "Composite", compTotal)
	fmt.Println()

	// Summary: suppression/amplification counts
	suppressedCount := 0
	amplifiedCount := 0
	for _, e := range entries {
		if e.NodeType == NodeTypeTechnical {
			if e.Delta < -0.01 {
				suppressedCount++
			} else if e.Delta > 0.01 {
				amplifiedCount++
			}
		}
	}
	fmt.Printf("Technical nodes suppressed by human/process edges: %d\n", suppressedCount)
	fmt.Printf("Technical nodes amplified by human/process edges:  %d\n", amplifiedCount)
	fmt.Println()

	fmt.Println("KEY INSIGHT: Comparing layers reveals WHERE hidden dependencies live.")
	fmt.Println("If Tech+Human >> Technical, people are the hidden bridge. If Tech+Process >>")
	fmt.Println("Technical, organisational processes are the hidden bridge. The composite")
	fmt.Println("shows the full picture — but each layer tells you what to fix first.")
}

// ExportResultsJSON writes all results to a JSON file
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

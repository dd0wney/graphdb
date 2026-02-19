// Package main provides print functions for OT model analysis results.
package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

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

// PrintTelecomResults prints formatted BC ranking for the telecom model.
func PrintTelecomResults(result ModelResult, topN int) {
	PrintResults(result, topN, PrintOptions{
		TitlePrefix:      "MODEL 4",
		ShowTypeCounts:   true,
		ShowRankColumn:   true,
		ShowExternalFlag: true,
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

// PrintSeniorEngRemovalComparison compares BC before and after removing Senior_Network_Eng.
func PrintSeniorEngRemovalComparison(before ModelResult, after ModelResult) {
	PrintRemovalComparison(before, after, RemovalConfig{
		Title:       "SENIOR NETWORK ENGINEER REMOVAL ANALYSIS",
		Question:    "What happens when the Senior Network Engineer leaves?",
		RemovedNode: "Senior_Network_Eng",
		RemovedRank: "rank #2",
		Insight: "Key insight: When the senior engineer leaves, BC redistributes to\n" +
			"technical infrastructure and remaining human nodes. The CAB process\n" +
			"sees massive increase as it becomes the primary coordination point.",
	})
}

// PrintVLANComparison compares flat vs VLAN network topologies.
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

// PrintChemicalFacilitySummary prints bridge risk analysis.
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

// PrintGatewayAnalysis prints cross-sector gateway BC analysis.
func PrintGatewayAnalysis(result ModelResult) {
	fmt.Println()
	fmt.Println("--- Cross-Sector Gateway BC ---")
	for _, gw := range result.GatewayAnalysis {
		fmt.Printf("  %-24s BC = %.4f  (serves %d external nodes)\n", gw.Name, gw.BC, gw.ExternalNodeCount)
	}
}

// PrintCascadeFailureAnalysis prints cascade failure results.
func PrintCascadeFailureAnalysis(cascades []CascadeResult) {
	fmt.Println()
	fmt.Println("--- Cascade Failure Analysis ---")
	fmt.Println()

	// Nodes that disconnect 2+ sectors
	fmt.Println("Nodes whose failure disconnects 2+ sectors:")
	for _, c := range cascades {
		if c.SectorsAffected >= 2 {
			fmt.Printf("  %s: %d sectors, %d external nodes\n", c.NodeName, c.SectorsAffected, c.ExternalNodesLost)
			for sector, nodes := range c.SectorDetails {
				fmt.Printf("    %s: %s\n", sector, strings.Join(nodes, ", "))
			}
		}
	}
	fmt.Println()

	// Nodes that disconnect exactly 1 sector (gateways)
	fmt.Println("Nodes whose failure disconnects exactly 1 sector (gateway SPOFs):")
	for _, c := range cascades {
		if c.SectorsAffected == 1 {
			for sector, nodes := range c.SectorDetails {
				fmt.Printf("  %s -> %s (%d nodes)\n", c.NodeName, sector, len(nodes))
			}
		}
	}
}

// PrintTelecomFinalSummary prints the final summary for Model 4.
func PrintTelecomFinalSummary(result ModelResult, cascades []CascadeResult) {
	fmt.Println()
	fmt.Println("--- Model 4 Key Findings ---")
	fmt.Println()
	fmt.Printf("1. Senior_Network_Eng (human) has BC %.4f, nearly matching the core router\n", result.TopInvisibleBC)
	fmt.Printf("   (%.2fx of top technical node). This demonstrates the invisible node\n", result.InvisibleMultiplier)
	fmt.Println("   pattern scales to realistic network complexity.")
	fmt.Println()
	fmt.Printf("2. Invisible node BC share: %.1f%% (human + process nodes)\n", result.InvisibleNodeShare*100)
	fmt.Println("   Over a third of network criticality is in non-technical nodes.")
	fmt.Println()
	fmt.Println("3. Each sector gateway is a single point of failure for its dependent")
	fmt.Println("   infrastructure. Gateway_Emergency failure disconnects 000 services.")
	fmt.Println()
	fmt.Println("4. The telecom provider is the 'infrastructure of infrastructures' with")
	fmt.Printf("   %d external sector nodes depending on it.\n", result.NodeTypeCounts[NodeTypeExternal])
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

	type multiEntry struct {
		Name     string
		LayerBC  []float64 // BC for each filtered layer
		CompBC   float64   // composite BC
		Delta    float64   // composite - technical
		NodeType string
		Level    string
	}

	entries := make([]multiEntry, 0, len(compositeResult.Rankings))
	for _, r := range compositeResult.Rankings {
		lbc := make([]float64, len(layerBCByName))
		for i, m := range layerBCByName {
			lbc[i] = m[r.Name]
		}
		compBC := compositeBCByName[r.Name]
		delta := compBC - lbc[0] // composite - technical
		entries = append(entries, multiEntry{
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

// Package main implements representative OT network models for betweenness centrality analysis.
//
// These models demonstrate the "invisible node" problem in critical infrastructure security,
// where human dependencies and organisational processes create single points of failure
// that don't appear on traditional network diagrams.
//
// Part of the book "Protecting Critical Infrastructure" by Darragh Downey.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" OT Representative Models: Betweenness Centrality Analysis")
	fmt.Println(" Part of 'Protecting Critical Infrastructure' by Darragh Downey")
	fmt.Println("=========================================================================")
	fmt.Println()

	// Clean up data directory before each run
	if err := os.RemoveAll("./data"); err != nil {
		log.Printf("Warning: failed to clean data directory: %v", err)
	}

	var allResults AllResults

	// ========================================
	// MODEL 1: Steve's Utility
	// ========================================
	fmt.Println("Building Model 1: Steve's Utility...")
	stevesMeta, err := BuildStevesUtility("./data/steves_utility")
	if err != nil {
		return fmt.Errorf("failed to build Steve's Utility: %w", err)
	}
	defer stevesMeta.Graph.Close()

	stevesBC, err := algorithms.BetweennessCentrality(stevesMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for Steve's Utility: %w", err)
	}

	stevesResult := AnalyseModel(stevesMeta, "Steve's Utility", stevesBC)
	allResults.StevesUtility = stevesResult
	PrintModelResults(stevesResult, 15)

	// ========================================
	// MODEL 1-ML: Multi-Layer BC Analysis
	// ========================================
	layerBCByName, layerLabels, err := buildMultiLayerAnalysis()
	if err != nil {
		return err
	}

	// Composite BC by name (from the full model)
	compositeBCByName := make(map[string]float64)
	for nodeID, bcVal := range stevesBC {
		if name, ok := stevesMeta.NodeNames[nodeID]; ok {
			compositeBCByName[name] = bcVal
		}
	}

	PrintMultiLayerAnalysis(stevesResult, layerBCByName, layerLabels, compositeBCByName)

	// ========================================
	// MODEL 1b: Steve's Utility WITHOUT Steve
	// ========================================
	fmt.Println()
	fmt.Println("Building Model 1b: Steve's Utility WITHOUT Steve...")
	noSteveMeta, err := BuildStevesUtilityWithoutSteve("./data/steves_utility_no_steve")
	if err != nil {
		return fmt.Errorf("failed to build Steve's Utility without Steve: %w", err)
	}
	defer noSteveMeta.Graph.Close()

	noSteveBC, err := algorithms.BetweennessCentrality(noSteveMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for model without Steve: %w", err)
	}

	noSteveResult := AnalyseModel(noSteveMeta, "Steve's Utility (Without Steve)", noSteveBC)
	allResults.StevesRemoval = noSteveResult
	PrintSteveRemovalComparison(stevesResult, noSteveResult)

	// ========================================
	// MODEL 2: Chemical Facility
	// ========================================
	fmt.Println()
	fmt.Println("Building Model 2: Chemical Facility...")
	chemMeta, err := BuildChemicalFacility("./data/chemical_facility")
	if err != nil {
		return fmt.Errorf("failed to build Chemical Facility: %w", err)
	}
	defer chemMeta.Graph.Close()

	chemBC, err := algorithms.BetweennessCentrality(chemMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for Chemical Facility: %w", err)
	}

	chemResult := AnalyseModel(chemMeta, "Chemical Facility", chemBC)
	allResults.ChemicalFacility = chemResult
	PrintModelResults(chemResult, 15)
	PrintChemicalFacilitySummary(chemResult)

	// ========================================
	// MODEL 3a: Water Treatment FLAT
	// ========================================
	fmt.Println()
	fmt.Println("Building Model 3a: Water Treatment (Flat)...")
	flatMeta, err := BuildWaterTreatmentFlat("./data/water_flat")
	if err != nil {
		return fmt.Errorf("failed to build Water Treatment Flat: %w", err)
	}
	defer flatMeta.Graph.Close()

	flatBC, err := algorithms.BetweennessCentrality(flatMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for Water Treatment Flat: %w", err)
	}

	flatResult := AnalyseModel(flatMeta, "Water Treatment (Flat)", flatBC)
	allResults.WaterFlat = flatResult
	PrintModelResults(flatResult, 13)

	// ========================================
	// MODEL 3b: Water Treatment VLAN
	// ========================================
	fmt.Println()
	fmt.Println("Building Model 3b: Water Treatment (VLAN)...")
	vlanMeta, err := BuildWaterTreatmentVLAN("./data/water_vlan")
	if err != nil {
		return fmt.Errorf("failed to build Water Treatment VLAN: %w", err)
	}
	defer vlanMeta.Graph.Close()

	vlanBC, err := algorithms.BetweennessCentrality(vlanMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for Water Treatment VLAN: %w", err)
	}

	vlanResult := AnalyseModel(vlanMeta, "Water Treatment (VLAN)", vlanBC)
	allResults.WaterVLAN = vlanResult
	PrintModelResults(vlanResult, 14)

	// ========================================
	// VLAN Comparison
	// ========================================
	PrintVLANComparison(flatResult, vlanResult)

	// ========================================
	// MODEL 4: Telecommunications Provider
	// ========================================
	fmt.Println()
	fmt.Println("Building Model 4: Telecommunications Provider...")
	telecomMeta, err := BuildTelecomProvider("./data/telecom")
	if err != nil {
		return fmt.Errorf("failed to build Telecom Provider: %w", err)
	}
	defer telecomMeta.Graph.Close()

	telecomBC, err := algorithms.BetweennessCentrality(telecomMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for Telecom Provider: %w", err)
	}

	telecomResult := AnalyseTelecomModel(telecomMeta, "Telecommunications Provider", telecomBC)
	allResults.TelecomProvider = &telecomResult
	PrintTelecomResults(telecomResult, 20)
	PrintGatewayAnalysis(telecomResult)

	// Cascade failure analysis
	cascades := AnalyseCascadeFailures(telecomMeta)
	telecomResult.CascadeFailures = cascades
	PrintCascadeFailureAnalysis(cascades)

	// ========================================
	// MODEL 4b: Telecom WITHOUT Senior Engineer
	// ========================================
	fmt.Println()
	fmt.Println("Building Model 4b: Telecom WITHOUT Senior Network Engineer...")
	noSeniorEngMeta, err := BuildTelecomProviderWithoutSeniorEng("./data/telecom_no_senior")
	if err != nil {
		return fmt.Errorf("failed to build Telecom without Senior Eng: %w", err)
	}
	defer noSeniorEngMeta.Graph.Close()

	noSeniorEngBC, err := algorithms.BetweennessCentrality(noSeniorEngMeta.Graph)
	if err != nil {
		return fmt.Errorf("failed to compute BC for Telecom without Senior Eng: %w", err)
	}

	noSeniorEngResult := AnalyseTelecomModel(noSeniorEngMeta, "Telecom (Without Senior Eng)", noSeniorEngBC)
	PrintSeniorEngRemovalComparison(telecomResult, noSeniorEngResult)
	PrintTelecomFinalSummary(telecomResult, cascades)

	// ========================================
	// Export Results
	// ========================================
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Println("EXPORTING RESULTS")
	fmt.Println("========================================================================")

	if err := ExportResultsJSON(allResults, "results.json"); err != nil {
		log.Printf("Warning: failed to export results: %v", err)
	} else {
		fmt.Println("Results exported to results.json")
	}

	// ========================================
	// Final Summary
	// ========================================
	fmt.Println()
	fmt.Println("========================================================================")
	fmt.Println("FINAL SUMMARY")
	fmt.Println("========================================================================")
	fmt.Println()
	fmt.Println("Key Findings:")
	fmt.Println()
	fmt.Printf("1. Steve's Utility: Steve (human) has BC %.4f, which is %.2fx higher\n",
		stevesResult.TopInvisibleBC, stevesResult.InvisibleMultiplier)
	fmt.Println("   than the top technical node. He is the invisible single point of failure.")
	fmt.Println()
	fmt.Printf("2. Chemical Facility: The IT/OT Coordinator has higher BC than technical\n")
	fmt.Println("   firewalls, making them the true bridge between IT and OT domains.")
	fmt.Println()
	fmt.Println("3. Water Treatment: VLAN segmentation concentrates BC on the L3 core")
	fmt.Println("   switch, creating a more critical chokepoint than flat mesh topology.")
	fmt.Println()
	fmt.Printf("4. Telecom Provider: Senior_Network_Eng has BC %.4f (%.2fx of core router).\n",
		telecomResult.TopInvisibleBC, telecomResult.InvisibleMultiplier)
	fmt.Println("   The invisible node pattern scales to realistic network complexity.")
	fmt.Println()
	fmt.Printf("5. Cross-sector dependencies: %d external sector nodes depend on telecom.\n",
		telecomResult.NodeTypeCounts["external"])
	fmt.Println("   Each gateway is a single point of failure for its dependent sector.")
	fmt.Println()
	fmt.Printf("6. Invisible node BC share across models:\n")
	fmt.Printf("   Steve's Utility: %.1f%% | Chemical: %.1f%% | Telecom: %.1f%%\n",
		stevesResult.InvisibleNodeShare*100,
		chemResult.InvisibleNodeShare*100,
		telecomResult.InvisibleNodeShare*100)
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Analysis Complete")
	fmt.Println("=========================================================================")

	return nil
}

// buildMultiLayerAnalysis builds filtered graph variants for multi-layer BC analysis.
func buildMultiLayerAnalysis() ([]map[string]float64, []string, error) {
	type layerSpec struct {
		Label     string
		DataDir   string
		EdgeTypes []string
	}
	layers := []layerSpec{
		{"Technical", "./data/layer_technical", []string{"TECHNICAL"}},
		{"Tech+Human", "./data/layer_tech_human", []string{"TECHNICAL", "HUMAN_ACCESS"}},
		{"Tech+Process", "./data/layer_tech_process", []string{"TECHNICAL", "PROCESS"}},
	}

	fmt.Println()
	fmt.Println("Building multi-layer BC analysis (Things / People / Process / Composite)...")

	layerBCByName := make([]map[string]float64, len(layers))
	layerLabels := make([]string, len(layers))

	for i, layer := range layers {
		layerLabels[i] = layer.Label
		meta, err := BuildStevesUtilityFiltered(layer.DataDir, layer.EdgeTypes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build %s layer: %w", layer.Label, err)
		}

		bc, err := algorithms.BetweennessCentrality(meta.Graph)
		if err != nil {
			meta.Graph.Close()
			return nil, nil, fmt.Errorf("failed to compute BC for %s layer: %w", layer.Label, err)
		}

		bcByName := make(map[string]float64)
		for nodeID, bcVal := range bc {
			if name, ok := meta.NodeNames[nodeID]; ok {
				bcByName[name] = bcVal
			}
		}
		layerBCByName[i] = bcByName
		meta.Graph.Close()
	}

	return layerBCByName, layerLabels, nil
}

// Package main models a regional power grid attack scenario inspired by the 2015/2016
// Ukraine power grid attacks (BlackEnergy/Industroyer). The Sandworm threat actor
// (GRU Unit 74455) used spear-phishing to gain access to corporate IT networks,
// pivoted through VPN and SCADA systems, and simultaneously opened breakers at
// multiple distribution substations — causing a cascading blackout affecting
// 230,000 customers.
//
// This example demonstrates how graph analysis (shortest paths, connected components,
// betweenness centrality, blast radius) exposes the structural vulnerabilities that
// made such an attack possible.
//
// Part of the book "Protecting Critical Infrastructure" by Darragh Downey.
package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// GridModel holds the graph and metadata for a power grid scenario.
type GridModel struct {
	Graph    *storage.GraphStorage
	Nodes    map[string]*NodeInfo
	NodeByID map[uint64]string
}

// NodeInfo stores metadata about a single node in the grid model.
type NodeInfo struct {
	ID     uint64
	Name   string
	Zone   string
	Labels []string
}

// nodeSpec defines a node to be created in the grid.
type nodeSpec struct {
	Name   string
	Labels []string
	Zone   string
}

// edgeSpec defines an edge to be created in the grid.
type edgeSpec struct {
	From       string
	To         string
	Type       string
	Undirected bool
}

// allNodeSpecs returns the complete list of nodes in the power grid model.
func allNodeSpecs() []nodeSpec {
	return []nodeSpec{
		// Internet / Entry (zone: external)
		{"Internet", []string{"Gateway"}, "external"},
		{"Phishing_Email", []string{"ThreatVector"}, "external"},

		// Corporate IT (zone: corporate)
		{"Corp_Firewall", []string{"Firewall"}, "corporate"},
		{"Corp_Switch", []string{"NetworkSwitch"}, "corporate"},
		{"Email_Server", []string{"Server"}, "corporate"},
		{"AD_Server", []string{"Server"}, "corporate"},
		{"User_PC_1", []string{"Workstation"}, "corporate"},
		{"User_PC_2", []string{"Workstation"}, "corporate"},
		{"User_PC_3", []string{"Workstation"}, "corporate"},

		// Remote Access (zone: remote)
		{"VPN_Gateway", []string{"Gateway"}, "remote"},
		{"Contractor_Laptop", []string{"Workstation"}, "remote"},

		// DMZ (zone: dmz)
		{"DMZ_Firewall", []string{"Firewall"}, "dmz"},
		{"Jump_Server", []string{"Server"}, "dmz"},
		{"Historian_Replica", []string{"Database"}, "dmz"},

		// Control Center (zone: control_center)
		{"CC_Firewall", []string{"Firewall"}, "control_center"},
		{"SCADA_Master", []string{"SCADA", "CriticalAsset"}, "control_center"},
		{"EMS_Server", []string{"Server"}, "control_center"},
		{"Operator_Console_1", []string{"HMI"}, "control_center"},
		{"Operator_Console_2", []string{"HMI"}, "control_center"},
		{"Operator_Console_3", []string{"HMI"}, "control_center"},

		// Transmission Substations (zone: transmission)
		{"Trans_Sub_A", []string{"Substation"}, "transmission"},
		{"Trans_Sub_A_RTU", []string{"RTU"}, "transmission"},
		{"Trans_Sub_A_Breaker", []string{"Breaker", "SafetyCritical"}, "transmission"},
		{"Trans_Sub_B", []string{"Substation"}, "transmission"},
		{"Trans_Sub_B_RTU", []string{"RTU"}, "transmission"},
		{"Trans_Sub_B_Breaker", []string{"Breaker", "SafetyCritical"}, "transmission"},

		// Distribution Substations (zone: distribution)
		{"Dist_Sub_1", []string{"Substation"}, "distribution"},
		{"Dist_Sub_1_RTU", []string{"RTU"}, "distribution"},
		{"Dist_Sub_1_Breaker", []string{"Breaker", "SafetyCritical"}, "distribution"},
		{"Dist_Sub_2", []string{"Substation"}, "distribution"},
		{"Dist_Sub_2_RTU", []string{"RTU"}, "distribution"},
		{"Dist_Sub_2_Breaker", []string{"Breaker", "SafetyCritical"}, "distribution"},
		{"Dist_Sub_3", []string{"Substation"}, "distribution"},
		{"Dist_Sub_3_RTU", []string{"RTU"}, "distribution"},
		{"Dist_Sub_3_Breaker", []string{"Breaker", "SafetyCritical"}, "distribution"},

		// Generation (zone: generation)
		{"Gen_Coal_Plant", []string{"Generator"}, "generation"},
		{"Gen_Wind_Farm", []string{"Generator"}, "generation"},
		{"Gen_Solar_Array", []string{"Generator"}, "generation"},

		// Critical Loads (zone: customers)
		{"Hospital_Feed", []string{"CriticalLoad"}, "customers"},
		{"Water_Plant_Feed", []string{"CriticalLoad"}, "customers"},
		{"Residential_North", []string{"Load"}, "customers"},
		{"Residential_South", []string{"Load"}, "customers"},
		{"Commercial_District", []string{"Load"}, "customers"},
		{"Industrial_Park", []string{"Load"}, "customers"},
	}
}

// allEdgeSpecs returns the complete list of edges in the power grid model.
func allEdgeSpecs() []edgeSpec {
	return []edgeSpec{
		// Attack path edges (directed, type: NETWORK)
		{"Internet", "Phishing_Email", "NETWORK", false},
		{"Phishing_Email", "Email_Server", "NETWORK", false},
		{"Email_Server", "Corp_Switch", "NETWORK", false},
		{"Corp_Switch", "AD_Server", "NETWORK", false},
		{"Corp_Switch", "User_PC_1", "NETWORK", false},
		{"Corp_Switch", "User_PC_2", "NETWORK", false},
		{"Corp_Switch", "User_PC_3", "NETWORK", false},
		{"Corp_Switch", "VPN_Gateway", "NETWORK", false},
		{"VPN_Gateway", "Contractor_Laptop", "NETWORK", true},
		{"Corp_Switch", "DMZ_Firewall", "NETWORK", false},
		{"DMZ_Firewall", "Jump_Server", "NETWORK", false},
		{"Jump_Server", "Historian_Replica", "NETWORK", false},
		{"DMZ_Firewall", "CC_Firewall", "NETWORK", false},
		{"CC_Firewall", "SCADA_Master", "NETWORK", false},

		// SCADA control edges (undirected, type: SCADA_CONTROL)
		{"SCADA_Master", "EMS_Server", "SCADA_CONTROL", true},
		{"SCADA_Master", "Operator_Console_1", "SCADA_CONTROL", true},
		{"SCADA_Master", "Operator_Console_2", "SCADA_CONTROL", true},
		{"SCADA_Master", "Operator_Console_3", "SCADA_CONTROL", true},
		{"SCADA_Master", "Trans_Sub_A_RTU", "SCADA_CONTROL", true},
		{"SCADA_Master", "Trans_Sub_B_RTU", "SCADA_CONTROL", true},
		{"SCADA_Master", "Dist_Sub_1_RTU", "SCADA_CONTROL", true},
		{"SCADA_Master", "Dist_Sub_2_RTU", "SCADA_CONTROL", true},
		{"SCADA_Master", "Dist_Sub_3_RTU", "SCADA_CONTROL", true},

		// Substation internal edges (undirected, type: ELECTRICAL)
		{"Trans_Sub_A", "Trans_Sub_A_RTU", "ELECTRICAL", true},
		{"Trans_Sub_A_RTU", "Trans_Sub_A_Breaker", "ELECTRICAL", true},
		{"Trans_Sub_B", "Trans_Sub_B_RTU", "ELECTRICAL", true},
		{"Trans_Sub_B_RTU", "Trans_Sub_B_Breaker", "ELECTRICAL", true},
		{"Dist_Sub_1", "Dist_Sub_1_RTU", "ELECTRICAL", true},
		{"Dist_Sub_1_RTU", "Dist_Sub_1_Breaker", "ELECTRICAL", true},
		{"Dist_Sub_2", "Dist_Sub_2_RTU", "ELECTRICAL", true},
		{"Dist_Sub_2_RTU", "Dist_Sub_2_Breaker", "ELECTRICAL", true},
		{"Dist_Sub_3", "Dist_Sub_3_RTU", "ELECTRICAL", true},
		{"Dist_Sub_3_RTU", "Dist_Sub_3_Breaker", "ELECTRICAL", true},

		// Power flow edges (undirected, type: POWER_FLOW)
		{"Gen_Coal_Plant", "Trans_Sub_A", "POWER_FLOW", true},
		{"Gen_Wind_Farm", "Trans_Sub_A", "POWER_FLOW", true},
		{"Gen_Solar_Array", "Trans_Sub_B", "POWER_FLOW", true},
		{"Trans_Sub_A", "Dist_Sub_1", "POWER_FLOW", true},
		{"Trans_Sub_A", "Dist_Sub_2", "POWER_FLOW", true},
		{"Trans_Sub_B", "Dist_Sub_3", "POWER_FLOW", true},
		{"Dist_Sub_1", "Hospital_Feed", "POWER_FLOW", true},
		{"Dist_Sub_1", "Residential_North", "POWER_FLOW", true},
		{"Dist_Sub_2", "Commercial_District", "POWER_FLOW", true},
		{"Dist_Sub_2", "Industrial_Park", "POWER_FLOW", true},
		{"Dist_Sub_3", "Water_Plant_Feed", "POWER_FLOW", true},
		{"Dist_Sub_3", "Residential_South", "POWER_FLOW", true},
	}
}

// buildFullGrid constructs the complete power grid graph with all nodes and edges.
func buildFullGrid(dataPath string) (*GridModel, error) {
	return buildGrid(dataPath, nil)
}

// buildDegradedGrid constructs a power grid graph with specific nodes removed,
// simulating the cascading failure of substations after an attacker opens breakers.
func buildDegradedGrid(dataPath string, removedNodes []string) (*GridModel, error) {
	return buildGrid(dataPath, removedNodes)
}

// buildGrid is the shared builder that optionally excludes nodes from the model.
func buildGrid(dataPath string, removedNodes []string) (*GridModel, error) {
	gs, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	excluded := make(map[string]bool, len(removedNodes))
	for _, name := range removedNodes {
		excluded[name] = true
	}

	model := &GridModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	// Create nodes, skipping any in the exclusion set
	for _, spec := range allNodeSpecs() {
		if excluded[spec.Name] {
			continue
		}

		props := map[string]storage.Value{
			"name": storage.StringValue(spec.Name),
			"zone": storage.StringValue(spec.Zone),
		}

		// Mark criticality on the SCADA master
		if spec.Name == "SCADA_Master" {
			props["criticality"] = storage.StringValue("critical")
		}

		node, createErr := gs.CreateNode(spec.Labels, props)
		if createErr != nil {
			gs.Close()
			return nil, fmt.Errorf("failed to create node %s: %w", spec.Name, createErr)
		}

		info := &NodeInfo{
			ID:     node.ID,
			Name:   spec.Name,
			Zone:   spec.Zone,
			Labels: spec.Labels,
		}
		model.Nodes[spec.Name] = info
		model.NodeByID[node.ID] = spec.Name
	}

	// Create edges, skipping any that reference excluded nodes
	for _, spec := range allEdgeSpecs() {
		if excluded[spec.From] || excluded[spec.To] {
			continue
		}

		fromInfo, fromOK := model.Nodes[spec.From]
		toInfo, toOK := model.Nodes[spec.To]
		if !fromOK || !toOK {
			continue
		}

		props := map[string]storage.Value{}

		_, createErr := gs.CreateEdge(fromInfo.ID, toInfo.ID, spec.Type, props, 1.0)
		if createErr != nil {
			gs.Close()
			return nil, fmt.Errorf("failed to create edge %s -> %s: %w", spec.From, spec.To, createErr)
		}

		// Undirected edges get a reverse edge as well
		if spec.Undirected {
			_, createErr = gs.CreateEdge(toInfo.ID, fromInfo.ID, spec.Type, props, 1.0)
			if createErr != nil {
				gs.Close()
				return nil, fmt.Errorf("failed to create reverse edge %s -> %s: %w", spec.To, spec.From, createErr)
			}
		}
	}

	return model, nil
}

// loadNames returns a list of node names (load + critical load) served by the grid.
func loadNames() []string {
	return []string{
		"Hospital_Feed",
		"Water_Plant_Feed",
		"Residential_North",
		"Residential_South",
		"Commercial_District",
		"Industrial_Park",
	}
}

// criticalLoadNames returns just the critical load node names.
func criticalLoadNames() []string {
	return []string{
		"Hospital_Feed",
		"Water_Plant_Feed",
	}
}

// generatorNames returns the generation node names.
func generatorNames() []string {
	return []string{
		"Gen_Coal_Plant",
		"Gen_Wind_Farm",
		"Gen_Solar_Array",
	}
}

func main() {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Power Grid Cascade Failure Analysis")
	fmt.Println(" Model 6: Protecting Critical Infrastructure -- Darragh Downey")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Inspired by: Ukraine Power Grid Attacks (2015-2016)")
	fmt.Println(" Threat Actor: Sandworm (GRU Unit 74455)")
	fmt.Println(" Attack Vector: Spear-phishing -> SCADA compromise -> Breaker manipulation")
	fmt.Println()
	fmt.Println(" On December 23, 2015, attackers simultaneously opened breakers at three")
	fmt.Println(" Ukrainian distribution substations, plunging 230,000 customers into darkness.")
	fmt.Println(" This model reconstructs that attack path and demonstrates how graph analysis")
	fmt.Println(" exposes the structural vulnerabilities that made it possible.")
	fmt.Println()

	if err := os.RemoveAll("./data"); err != nil {
		log.Printf("Warning: failed to clean data directory: %v", err)
	}

	// Build the full baseline grid
	fullModel, err := buildFullGrid("./data/full_grid")
	if err != nil {
		log.Fatalf("Failed to build full grid: %v", err)
	}
	defer fullModel.Graph.Close()

	stats := fullModel.Graph.GetStatistics()
	fmt.Printf(" Grid model: %d nodes, %d edges (directed)\n", stats.NodeCount, stats.EdgeCount)
	fmt.Println()

	analyseAttackPaths(fullModel)
	analyseCascadeFailure()
	analyseBlastRadius(fullModel)
	analyseBetweenness(fullModel)

	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Analysis Complete")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Key Takeaways:")
	fmt.Println("  1. A single compromised SCADA master controls ALL substations.")
	fmt.Println("  2. The attack path from Internet to any breaker is alarmingly short.")
	fmt.Println("  3. Removing just 3 distribution substations causes total blackout.")
	fmt.Println("  4. CC_Firewall and SCADA_Master are the critical chokepoints that")
	fmt.Println("     defenders must monitor and protect above all else.")
	fmt.Println()
}

// analyseAttackPaths finds and displays the shortest attack paths through the grid.
func analyseAttackPaths(model *GridModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 1: Attack Path Analysis")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" The attacker's objective: reach the SCADA master, then open breakers")
	fmt.Println(" at distribution substations to cause a cascading blackout.")
	fmt.Println()

	// Path 1: Internet -> SCADA_Master
	printAttackPath(model, "Internet", "SCADA_Master",
		"Primary objective: compromise the SCADA master station")

	// Path 2: Internet -> Dist_Sub_1_Breaker (actual physical target)
	printAttackPath(model, "Internet", "Dist_Sub_1_Breaker",
		"Ultimate target: open Dist_Sub_1 breaker (Hospital/Residential feed)")

	// Path 3: SCADA_Master -> Dist_Sub_3_Breaker (lateral from SCADA to breaker)
	printAttackPath(model, "SCADA_Master", "Dist_Sub_3_Breaker",
		"Lateral movement: SCADA to Dist_Sub_3 breaker (Water Plant feed)")

	fmt.Println()
}

// printAttackPath finds and prints a single shortest path with context.
func printAttackPath(model *GridModel, fromName, toName, description string) {
	fromInfo, fromOK := model.Nodes[fromName]
	toInfo, toOK := model.Nodes[toName]
	if !fromOK || !toOK {
		fmt.Printf("  [!] Cannot find nodes: %s or %s\n", fromName, toName)
		return
	}

	path, err := algorithms.ShortestPath(model.Graph, fromInfo.ID, toInfo.ID)
	if err != nil {
		fmt.Printf("  [!] Error finding path %s -> %s: %v\n", fromName, toName, err)
		return
	}

	fmt.Printf(" --- %s ---\n", description)

	if path == nil {
		fmt.Printf("  NO PATH FOUND: %s -> %s\n", fromName, toName)
		fmt.Println()
		return
	}

	fmt.Printf("  Path (%d hops): ", len(path)-1)
	names := make([]string, 0, len(path))
	for _, nodeID := range path {
		name, ok := model.NodeByID[nodeID]
		if !ok {
			name = fmt.Sprintf("Unknown(%d)", nodeID)
		}
		names = append(names, name)
	}
	fmt.Println(strings.Join(names, " -> "))

	// Show zone transitions to highlight boundary crossings
	fmt.Print("  Zones:          ")
	zones := make([]string, 0, len(path))
	for _, nodeID := range path {
		name := model.NodeByID[nodeID]
		info := model.Nodes[name]
		zones = append(zones, info.Zone)
	}
	fmt.Println(strings.Join(zones, " -> "))
	fmt.Println()
}

// cascadeScenario describes a single step in the progressive cascade failure.
type cascadeScenario struct {
	Name         string
	RemovedNodes []string
}

// analyseCascadeFailure builds progressively degraded grids to show how removing
// distribution substations causes cascading blackout -- the centrepiece of the analysis.
func analyseCascadeFailure() {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 2: Progressive Cascade Failure (THE CENTREPIECE)")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" On December 23, 2015, Sandworm operators simultaneously opened breakers")
	fmt.Println(" at three distribution substations. We now simulate what happens when")
	fmt.Println(" substations are removed one at a time, just as the operators did.")
	fmt.Println()

	scenarios := []cascadeScenario{
		{
			Name:         "Baseline (no failure)",
			RemovedNodes: nil,
		},
		{
			Name:         "Dist_Sub_1 removed",
			RemovedNodes: []string{"Dist_Sub_1", "Dist_Sub_1_RTU", "Dist_Sub_1_Breaker"},
		},
		{
			Name:         "Dist_Sub_1+2 removed",
			RemovedNodes: []string{"Dist_Sub_1", "Dist_Sub_1_RTU", "Dist_Sub_1_Breaker", "Dist_Sub_2", "Dist_Sub_2_RTU", "Dist_Sub_2_Breaker"},
		},
		{
			Name: "All 3 Dist_Subs removed",
			RemovedNodes: []string{
				"Dist_Sub_1", "Dist_Sub_1_RTU", "Dist_Sub_1_Breaker",
				"Dist_Sub_2", "Dist_Sub_2_RTU", "Dist_Sub_2_Breaker",
				"Dist_Sub_3", "Dist_Sub_3_RTU", "Dist_Sub_3_Breaker",
			},
		},
	}

	type scenarioResult struct {
		Name           string
		Components     int
		LoadsServed    int
		TotalLoads     int
		CriticalServed int
		TotalCritical  int
		CustomerPct    int
		ServedLoads    []string
		LostLoads      []string
	}

	results := make([]scenarioResult, 0, len(scenarios))

	for i, scenario := range scenarios {
		dataPath := fmt.Sprintf("./data/cascade_%d", i)
		model, err := buildDegradedGrid(dataPath, scenario.RemovedNodes)
		if err != nil {
			log.Fatalf("Failed to build scenario %q: %v", scenario.Name, err)
		}

		components, compErr := algorithms.ConnectedComponents(model.Graph)
		if compErr != nil {
			model.Graph.Close()
			log.Fatalf("Failed to compute components for %q: %v", scenario.Name, compErr)
		}

		// Determine which loads are still connected to at least one generator
		genIDs := make(map[uint64]bool)
		for _, genName := range generatorNames() {
			if info, ok := model.Nodes[genName]; ok {
				genIDs[info.ID] = true
			}
		}

		// Build a set of community IDs that contain a generator
		genCommunities := make(map[int]bool)
		for genID := range genIDs {
			if commID, ok := components.NodeCommunity[genID]; ok {
				genCommunities[commID] = true
			}
		}

		allLoads := loadNames()
		allCritical := criticalLoadNames()
		servedLoads := make([]string, 0)
		lostLoads := make([]string, 0)

		for _, loadName := range allLoads {
			info, ok := model.Nodes[loadName]
			if !ok {
				// Node was removed
				lostLoads = append(lostLoads, loadName)
				continue
			}
			commID, inGraph := components.NodeCommunity[info.ID]
			if inGraph && genCommunities[commID] {
				servedLoads = append(servedLoads, loadName)
			} else {
				lostLoads = append(lostLoads, loadName)
			}
		}

		criticalServed := 0
		for _, critName := range allCritical {
			info, ok := model.Nodes[critName]
			if !ok {
				continue
			}
			commID, inGraph := components.NodeCommunity[info.ID]
			if inGraph && genCommunities[commID] {
				criticalServed++
			}
		}

		customerPct := 0
		if len(allLoads) > 0 {
			customerPct = (len(servedLoads) * 100) / len(allLoads)
		}

		results = append(results, scenarioResult{
			Name:           scenario.Name,
			Components:     len(components.Communities),
			LoadsServed:    len(servedLoads),
			TotalLoads:     len(allLoads),
			CriticalServed: criticalServed,
			TotalCritical:  len(allCritical),
			CustomerPct:    customerPct,
			ServedLoads:    servedLoads,
			LostLoads:      lostLoads,
		})

		model.Graph.Close()
	}

	// Print the progressive cascade table
	fmt.Println(" --- Progressive Cascade Failure ---")
	fmt.Println()
	header := fmt.Sprintf(" %-26s %10s %13s %15s %10s",
		"Scenario", "Components", "Loads Served", "Critical Loads", "Customers")
	fmt.Println(header)
	fmt.Println(" " + strings.Repeat("\u2500", 78))

	for _, r := range results {
		fmt.Printf(" %-26s %10d %9d/%-3d %10d/%-4d %8d%%\n",
			r.Name,
			r.Components,
			r.LoadsServed, r.TotalLoads,
			r.CriticalServed, r.TotalCritical,
			r.CustomerPct,
		)
	}

	fmt.Println()

	// Detailed narrative for each degradation step
	for i, r := range results {
		if i == 0 {
			continue
		}

		fmt.Printf(" Step %d: %s\n", i, r.Name)
		if len(r.LostLoads) > 0 {
			fmt.Printf("   LOST: %s\n", strings.Join(r.LostLoads, ", "))
		}
		if len(r.ServedLoads) > 0 {
			fmt.Printf("   Still served: %s\n", strings.Join(r.ServedLoads, ", "))
		} else {
			fmt.Println("   TOTAL BLACKOUT -- no loads receiving power")
		}

		if r.CriticalServed == 0 && r.TotalCritical > 0 {
			fmt.Println("   *** CRITICAL: Hospital and Water Plant have lost power ***")
		}
		fmt.Println()
	}

	// Final dramatic summary
	last := results[len(results)-1]
	if last.CustomerPct == 0 {
		fmt.Println(" RESULT: Three simultaneous breaker openings cause TOTAL DISTRIBUTION BLACKOUT.")
		fmt.Println(" 230,000 customers lose power. Hospital and Water Plant are offline.")
		fmt.Println(" This is exactly what happened in Ukraine on December 23, 2015.")
	}
	fmt.Println()
}

// analyseBlastRadius shows the reach of a compromised SCADA master by computing
// shortest distances to all reachable nodes.
func analyseBlastRadius(model *GridModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 3: SCADA Master Blast Radius")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" If an attacker compromises the SCADA master, how far can they reach?")
	fmt.Println(" AllShortestPaths reveals every node reachable and the hop distance.")
	fmt.Println()

	scadaInfo, ok := model.Nodes["SCADA_Master"]
	if !ok {
		fmt.Println(" [!] SCADA_Master not found in model")
		return
	}

	distances, err := algorithms.AllShortestPaths(model.Graph, scadaInfo.ID)
	if err != nil {
		fmt.Printf(" [!] Error computing blast radius: %v\n", err)
		return
	}

	// Count reachable assets by type
	breakerCount := 0
	rtuCount := 0
	substationCount := 0
	totalReachable := len(distances) - 1 // exclude SCADA itself

	type reachableNode struct {
		Name     string
		Distance int
		Zone     string
	}

	reachableBreakers := make([]reachableNode, 0)
	reachableRTUs := make([]reachableNode, 0)
	reachableSubs := make([]reachableNode, 0)

	for nodeID, dist := range distances {
		if nodeID == scadaInfo.ID {
			continue
		}

		name, nameOK := model.NodeByID[nodeID]
		if !nameOK {
			continue
		}

		info := model.Nodes[name]

		for _, label := range info.Labels {
			switch label {
			case "Breaker":
				breakerCount++
				reachableBreakers = append(reachableBreakers, reachableNode{name, dist, info.Zone})
			case "RTU":
				rtuCount++
				reachableRTUs = append(reachableRTUs, reachableNode{name, dist, info.Zone})
			case "Substation":
				substationCount++
				reachableSubs = append(reachableSubs, reachableNode{name, dist, info.Zone})
			}
		}
	}

	fmt.Printf(" From SCADA_Master, %d of %d nodes are reachable:\n",
		totalReachable, len(model.Nodes)-1)
	fmt.Println()
	fmt.Printf("   Substations:  %d reachable\n", substationCount)
	fmt.Printf("   RTUs:         %d reachable\n", rtuCount)
	fmt.Printf("   Breakers:     %d reachable (SafetyCritical)\n", breakerCount)
	fmt.Println()

	// Show specific breaker distances
	sort.Slice(reachableBreakers, func(i, j int) bool {
		return reachableBreakers[i].Distance < reachableBreakers[j].Distance
	})

	fmt.Println(" Breaker reachability from SCADA_Master:")
	for _, b := range reachableBreakers {
		fmt.Printf("   %s (%s) -- %d hops\n", b.Name, b.Zone, b.Distance)
	}

	fmt.Println()
	fmt.Println(" FINDING: One compromised SCADA master can reach ALL breakers in the grid.")
	fmt.Println(" The attacker does not need to compromise each substation individually.")
	fmt.Println(" This single-point-of-control is the architectural flaw that Sandworm exploited.")
	fmt.Println()
}

// analyseBetweenness computes betweenness centrality for the entire grid and
// highlights the critical chokepoint nodes.
func analyseBetweenness(model *GridModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 4: Betweenness Centrality Analysis")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Betweenness centrality measures how often a node sits on the shortest")
	fmt.Println(" path between all other pairs of nodes. High BC = critical chokepoint.")
	fmt.Println()

	bc, err := algorithms.BetweennessCentrality(model.Graph)
	if err != nil {
		fmt.Printf(" [!] Error computing betweenness centrality: %v\n", err)
		return
	}

	// Build ranked list
	type rankedNode struct {
		Name string
		BC   float64
		Zone string
	}

	ranked := make([]rankedNode, 0, len(bc))
	for nodeID, score := range bc {
		name, ok := model.NodeByID[nodeID]
		if !ok {
			continue
		}
		info := model.Nodes[name]
		ranked = append(ranked, rankedNode{name, score, info.Zone})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].BC > ranked[j].BC
	})

	// Print top 15
	limit := 15
	if len(ranked) < limit {
		limit = len(ranked)
	}

	fmt.Printf(" %-4s %-28s %-18s %12s\n", "Rank", "Node", "Zone", "BC Score")
	fmt.Println(" " + strings.Repeat("\u2500", 66))

	for i := 0; i < limit; i++ {
		r := ranked[i]
		marker := ""
		if r.Name == "SCADA_Master" || r.Name == "CC_Firewall" {
			marker = " <-- CRITICAL CHOKEPOINT"
		}
		fmt.Printf(" %-4d %-28s %-18s %12.6f%s\n",
			i+1, r.Name, r.Zone, r.BC, marker)
	}

	fmt.Println()

	// Highlight key findings
	scadaRank := -1
	ccfwRank := -1
	for i, r := range ranked {
		if r.Name == "SCADA_Master" && scadaRank == -1 {
			scadaRank = i + 1
		}
		if r.Name == "CC_Firewall" && ccfwRank == -1 {
			ccfwRank = i + 1
		}
	}

	fmt.Println(" Key Findings:")
	if scadaRank > 0 {
		fmt.Printf("   - SCADA_Master ranks #%d -- it bridges the control center to ALL substations\n", scadaRank)
	}
	if ccfwRank > 0 {
		fmt.Printf("   - CC_Firewall ranks #%d -- the sole gateway between IT/DMZ and the control center\n", ccfwRank)
	}
	fmt.Println()
	fmt.Println(" These two nodes are the defensive choke points. Monitoring and hardening")
	fmt.Println(" them is the single most impactful mitigation against the Sandworm attack pattern.")
	fmt.Println()
}

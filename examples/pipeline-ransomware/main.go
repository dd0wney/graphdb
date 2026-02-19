// Package main models a fuel pipeline IT/OT network inspired by the 2021 Colonial Pipeline
// ransomware attack. It demonstrates how IT dependencies can force OT shutdown even when the
// OT network is never directly compromised.
//
// The attack narrative: DarkSide ransomware enters via a compromised VPN credential (no MFA),
// spreads laterally through the IT network via Active Directory, and encrypts billing and
// scheduling systems. The company preemptively shuts down pipeline operations because they
// cannot bill for fuel -- even though the OT network remains physically intact.
//
// Model 7 in "Protecting Critical Infrastructure" by Darragh Downey.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// PipelineModel holds the graph and metadata for the pipeline network.
type PipelineModel struct {
	Graph    *storage.GraphStorage
	Nodes    map[string]*NodeInfo
	NodeByID map[uint64]string
}

// NodeInfo stores metadata about each node for analysis and display.
type NodeInfo struct {
	ID     uint64
	Name   string
	Zone   string
	Labels []string
}

// zoneOrder defines the display ordering for network zones.
var zoneOrder = []string{
	"corporate_it",
	"boundary",
	"ot_network",
	"pump_station",
	"field",
	"terminal",
	"external",
}

func main() {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Pipeline Ransomware: IT/OT Boundary Analysis")
	fmt.Println(" Model 7: Protecting Critical Infrastructure — Darragh Downey")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Inspired by: Colonial Pipeline Ransomware Attack (May 2021)")
	fmt.Println(" Threat Actor: DarkSide Ransomware-as-a-Service")
	fmt.Println(" Attack Vector: Compromised VPN credential → Lateral movement → Ransomware")
	fmt.Println(" Key Lesson: IT dependencies can force OT shutdown without OT compromise")
	fmt.Println()

	// Clean slate for each run
	if err := os.RemoveAll("./data"); err != nil {
		log.Printf("Warning: failed to clean data directory: %v", err)
	}

	model, err := buildPipelineModel("./data/pipeline")
	if err != nil {
		log.Fatalf("Failed to build pipeline model: %v", err)
	}
	defer model.Graph.Close()

	stats := model.Graph.GetStatistics()
	fmt.Printf(" Network built: %d nodes, %d edges\n", stats.NodeCount, stats.EdgeCount)
	fmt.Println()

	// ================================================================
	// ANALYSIS 1: Ransomware Blast Radius
	// ================================================================
	analyseRansomwareBlastRadius(model)

	// ================================================================
	// ANALYSIS 2: IT/OT Boundary Analysis
	// ================================================================
	analyseITOTBoundary(model)

	// ================================================================
	// ANALYSIS 3: Operational Dependency (The Colonial Pipeline Lesson)
	// ================================================================
	analyseOperationalDependency(model)

	// ================================================================
	// ANALYSIS 4: Betweenness Centrality
	// ================================================================
	analyseBetweennessCentrality(model)

	// ================================================================
	// Final Summary
	// ================================================================
	printFinalSummary()
}

// buildPipelineModel constructs the full pipeline IT/OT graph.
func buildPipelineModel(dataDir string) (*PipelineModel, error) {
	gs, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	model := &PipelineModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	// Helper to create a node and register it in our lookup maps.
	addNode := func(name, zone string, labels []string) uint64 {
		props := map[string]storage.Value{
			"name": storage.StringValue(name),
			"zone": storage.StringValue(zone),
		}
		node, err := gs.CreateNode(labels, props)
		if err != nil {
			log.Fatalf("Failed to create node %s: %v", name, err)
		}
		info := &NodeInfo{
			ID:     node.ID,
			Name:   name,
			Zone:   zone,
			Labels: labels,
		}
		model.Nodes[name] = info
		model.NodeByID[node.ID] = name
		return node.ID
	}

	// Helper to create a directed edge.
	addEdge := func(fromName, toName, edgeType string, weight float64) {
		from := model.Nodes[fromName]
		to := model.Nodes[toName]
		if from == nil || to == nil {
			log.Fatalf("Edge references unknown node: %s -> %s", fromName, toName)
		}
		_, err := gs.CreateEdge(from.ID, to.ID, edgeType, map[string]storage.Value{}, weight)
		if err != nil {
			log.Fatalf("Failed to create edge %s -> %s: %v", fromName, toName, err)
		}
	}

	// Helper to create a bidirectional (undirected) edge pair.
	addBiEdge := func(aName, bName, edgeType string, weight float64) {
		addEdge(aName, bName, edgeType, weight)
		addEdge(bName, aName, edgeType, weight)
	}

	// ---------------------------------------------------------------
	// NODES
	// ---------------------------------------------------------------

	// Internet / Entry (zone: external)
	addNode("Internet", "external", []string{"Gateway"})
	addNode("Compromised_VPN_Cred", "external", []string{"ThreatVector"})

	// Corporate IT (zone: corporate_it)
	addNode("Corp_Firewall", "corporate_it", []string{"Firewall"})
	addNode("Corp_Switch", "corporate_it", []string{"NetworkSwitch"})
	addNode("Email_Server", "corporate_it", []string{"Server"})
	addNode("AD_Server", "corporate_it", []string{"Server"})
	addNode("Billing_System", "corporate_it", []string{"Server", "BusinessCritical"})
	addNode("Scheduling_System", "corporate_it", []string{"Server", "BusinessCritical"})
	addNode("ERP_System", "corporate_it", []string{"Server"})
	addNode("Finance_PC", "corporate_it", []string{"Workstation"})
	addNode("Admin_PC", "corporate_it", []string{"Workstation"})
	addNode("Exec_PC", "corporate_it", []string{"Workstation"})
	addNode("Backup_Server", "corporate_it", []string{"Server"})

	// IT/OT Boundary (zone: boundary)
	addNode("Historian_Bridge", "boundary", []string{"Database"})
	addNode("Data_Diode", "boundary", []string{"SecurityDevice"})
	addNode("Jump_Host", "boundary", []string{"Server"})

	// OT Network (zone: ot_network)
	addNode("OT_Firewall", "ot_network", []string{"Firewall"})
	addNode("OT_Switch", "ot_network", []string{"NetworkSwitch"})
	addNode("SCADA_Server", "ot_network", []string{"SCADA"})
	addNode("Eng_Workstation", "ot_network", []string{"Workstation"})
	addNode("Leak_Det_Server", "ot_network", []string{"Server", "SafetyCritical"})

	// Pump Stations (zone: pump_station)
	for i := 1; i <= 5; i++ {
		prefix := fmt.Sprintf("PS%d", i)
		addNode(prefix+"_RTU", "pump_station", []string{"RTU"})
		addNode(prefix+"_PLC", "pump_station", []string{"PLC"})
		addNode(prefix+"_VFD", "pump_station", []string{"VFD"})
	}

	// Leak Detection Sensors (zone: field)
	for i := 1; i <= 5; i++ {
		addNode(fmt.Sprintf("Leak_Sensor_%d", i), "field", []string{"Sensor"})
	}

	// Storage & Delivery (zone: terminal)
	addNode("Tank_Farm_North", "terminal", []string{"Storage"})
	addNode("Tank_Farm_South", "terminal", []string{"Storage"})
	addNode("Terminal_East", "terminal", []string{"Delivery"})
	addNode("Terminal_West", "terminal", []string{"Delivery"})

	// ---------------------------------------------------------------
	// EDGES
	// ---------------------------------------------------------------

	// IT Network (directed, type: NETWORK) -- ransomware can traverse these
	addEdge("Internet", "Corp_Firewall", "NETWORK", 1.0)
	addEdge("Corp_Firewall", "Corp_Switch", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Email_Server", "NETWORK", 1.0)
	addEdge("Corp_Switch", "AD_Server", "NETWORK", 1.0)
	addEdge("Corp_Switch", "ERP_System", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Billing_System", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Scheduling_System", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Finance_PC", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Admin_PC", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Exec_PC", "NETWORK", 1.0)
	addEdge("Corp_Switch", "Backup_Server", "NETWORK", 1.0)

	// VPN access (directed)
	addEdge("Compromised_VPN_Cred", "Corp_Firewall", "VPN_ACCESS", 1.0)

	// IT Lateral Movement (bidirectional, type: LATERAL) -- credential reuse paths
	addBiEdge("AD_Server", "Email_Server", "LATERAL", 1.0)
	addBiEdge("AD_Server", "Billing_System", "LATERAL", 1.0)
	addBiEdge("AD_Server", "Scheduling_System", "LATERAL", 1.0)
	addBiEdge("AD_Server", "ERP_System", "LATERAL", 1.0)
	addBiEdge("AD_Server", "Backup_Server", "LATERAL", 1.0)
	addBiEdge("Email_Server", "Finance_PC", "LATERAL", 1.0)
	addBiEdge("Email_Server", "Admin_PC", "LATERAL", 1.0)

	// IT/OT Boundary (directed, type: BOUNDARY)
	addEdge("Corp_Switch", "Historian_Bridge", "BOUNDARY", 1.0)
	addEdge("Historian_Bridge", "Data_Diode", "BOUNDARY", 1.0)
	addEdge("Data_Diode", "Corp_Switch", "BOUNDARY", 1.0)
	addEdge("Corp_Switch", "Jump_Host", "BOUNDARY", 1.0)
	addEdge("Jump_Host", "OT_Firewall", "BOUNDARY", 1.0)

	// OT Network (bidirectional, type: SCADA_CONTROL)
	addBiEdge("OT_Firewall", "OT_Switch", "SCADA_CONTROL", 1.0)
	addBiEdge("OT_Switch", "SCADA_Server", "SCADA_CONTROL", 1.0)
	addBiEdge("OT_Switch", "Eng_Workstation", "SCADA_CONTROL", 1.0)
	addBiEdge("OT_Switch", "Leak_Det_Server", "SCADA_CONTROL", 1.0)
	for i := 1; i <= 5; i++ {
		addBiEdge("SCADA_Server", fmt.Sprintf("PS%d_RTU", i), "SCADA_CONTROL", 1.0)
	}

	// Pump Station Internal (bidirectional, type: CONTROLS)
	for i := 1; i <= 5; i++ {
		prefix := fmt.Sprintf("PS%d", i)
		addBiEdge(prefix+"_RTU", prefix+"_PLC", "CONTROLS", 1.0)
		addBiEdge(prefix+"_PLC", prefix+"_VFD", "CONTROLS", 1.0)
	}

	// Leak Detection (bidirectional, type: MONITORS)
	for i := 1; i <= 5; i++ {
		addBiEdge("Leak_Det_Server", fmt.Sprintf("Leak_Sensor_%d", i), "MONITORS", 1.0)
	}

	// Pipeline Flow (bidirectional, type: PIPELINE)
	addBiEdge("Tank_Farm_North", "PS1_PLC", "PIPELINE", 1.0)
	addBiEdge("PS1_PLC", "PS2_PLC", "PIPELINE", 1.0)
	addBiEdge("PS2_PLC", "PS3_PLC", "PIPELINE", 1.0)
	addBiEdge("PS3_PLC", "PS4_PLC", "PIPELINE", 1.0)
	addBiEdge("PS4_PLC", "PS5_PLC", "PIPELINE", 1.0)
	addBiEdge("PS5_PLC", "Tank_Farm_South", "PIPELINE", 1.0)
	addBiEdge("PS1_PLC", "Terminal_West", "PIPELINE", 1.0)
	addBiEdge("PS5_PLC", "Terminal_East", "PIPELINE", 1.0)

	return model, nil
}

// ========================================================================
// ANALYSIS 1: Ransomware Blast Radius
// ========================================================================

func analyseRansomwareBlastRadius(model *PipelineModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" ANALYSIS 1: Ransomware Blast Radius")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Attack path: Compromised VPN → Corp Firewall → AD Server → Lateral movement")
	fmt.Println()

	adServer := model.Nodes["AD_Server"]
	if adServer == nil {
		log.Fatalf("AD_Server not found in model")
	}

	// BFS from AD_Server to find all reachable nodes (simulating lateral spread)
	distances, err := algorithms.AllShortestPaths(model.Graph, adServer.ID)
	if err != nil {
		log.Fatalf("Failed to compute ransomware blast radius: %v", err)
	}

	// Count nodes reached per zone
	zoneTotals := make(map[string]int)
	zoneReached := make(map[string]int)
	reachedNames := make(map[string][]string)

	for name, info := range model.Nodes {
		zoneTotals[info.Zone]++
		if _, reached := distances[info.ID]; reached {
			zoneReached[info.Zone]++
			reachedNames[info.Zone] = append(reachedNames[info.Zone], name)
		}
	}

	// Sort reached names for deterministic output
	for zone := range reachedNames {
		sort.Strings(reachedNames[zone])
	}

	// Identify business-critical systems that were encrypted
	businessCritical := []string{"Billing_System", "Scheduling_System", "ERP_System", "Backup_Server"}
	encryptedCritical := []string{}
	for _, name := range businessCritical {
		info := model.Nodes[name]
		if info == nil {
			continue
		}
		if _, reached := distances[info.ID]; reached {
			encryptedCritical = append(encryptedCritical, name)
		}
	}

	fmt.Println("--- Ransomware Blast Radius from AD_Server ---")
	fmt.Println()
	fmt.Printf("%-22s %13s %12s %11s\n", "Zone", "Nodes Reached", "Total Nodes", "Encrypted %")
	fmt.Println(strings.Repeat("\u2500", 62))

	totalReached := 0
	totalNodes := 0
	for _, zone := range zoneOrder {
		total := zoneTotals[zone]
		if total == 0 {
			continue
		}
		reached := zoneReached[zone]
		pct := 0
		if total > 0 {
			pct = int(math.Round(float64(reached) / float64(total) * 100))
		}
		fmt.Printf("%-22s %13d %12d %10d%%\n", zone, reached, total, pct)
		totalReached += reached
		totalNodes += total
	}
	fmt.Println(strings.Repeat("\u2500", 62))
	totalPct := 0
	if totalNodes > 0 {
		totalPct = int(math.Round(float64(totalReached) / float64(totalNodes) * 100))
	}
	fmt.Printf("%-22s %13d %12d %10d%%\n", "TOTAL", totalReached, totalNodes, totalPct)
	fmt.Println()

	// Show what was encrypted in each zone
	fmt.Println("--- Encrypted Systems Detail ---")
	fmt.Println()
	for _, zone := range zoneOrder {
		names := reachedNames[zone]
		if len(names) == 0 {
			continue
		}
		fmt.Printf("  %s:\n", zone)
		for _, n := range names {
			marker := ""
			for _, bc := range businessCritical {
				if n == bc {
					marker = " [BUSINESS CRITICAL]"
					break
				}
			}
			dist := distances[model.Nodes[n].ID]
			fmt.Printf("    - %s (distance: %d)%s\n", n, dist, marker)
		}
	}
	fmt.Println()

	// OT reachability check
	otReached := zoneReached["ot_network"] + zoneReached["pump_station"] + zoneReached["field"] + zoneReached["terminal"]
	if otReached == 0 {
		fmt.Println("  ** CRITICAL FINDING: Ransomware did NOT reach any OT systems **")
		fmt.Println("     The OT network, pump stations, sensors, and terminals are INTACT.")
		fmt.Println("     Yet the pipeline will shut down anyway. (See Analysis 3)")
	} else {
		fmt.Printf("  WARNING: Ransomware reached %d OT-side nodes!\n", otReached)
	}
	fmt.Println()

	fmt.Printf("  Business-critical systems encrypted: %d of %d\n", len(encryptedCritical), len(businessCritical))
	for _, name := range encryptedCritical {
		fmt.Printf("    [ENCRYPTED] %s\n", name)
	}
	fmt.Println()
}

// ========================================================================
// ANALYSIS 2: IT/OT Boundary Analysis
// ========================================================================

func analyseITOTBoundary(model *PipelineModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" ANALYSIS 2: IT/OT Boundary Analysis")
	fmt.Println("=========================================================================")
	fmt.Println()

	// Identify boundary-crossing edges: edges where from-zone and to-zone differ
	// between IT-side zones and OT-side zones
	itZones := map[string]bool{"corporate_it": true, "external": true}
	otZones := map[string]bool{"ot_network": true, "pump_station": true, "field": true, "terminal": true}

	type boundaryEdge struct {
		FromName string
		ToName   string
		FromZone string
		ToZone   string
		EdgeType string
	}

	var crossings []boundaryEdge
	boundaryNodes := make(map[string]bool)

	// Check every node's outgoing edges for boundary crossings
	for name, info := range model.Nodes {
		edges, err := model.Graph.GetOutgoingEdges(info.ID)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			toName, ok := model.NodeByID[edge.ToNodeID]
			if !ok {
				continue
			}
			toInfo := model.Nodes[toName]
			if toInfo == nil {
				continue
			}

			// Crossing: edge spans between IT-side and OT-side (or boundary to either)
			crosses := (itZones[info.Zone] && (otZones[toInfo.Zone] || toInfo.Zone == "boundary")) ||
				(otZones[info.Zone] && (itZones[toInfo.Zone] || toInfo.Zone == "boundary")) ||
				(info.Zone == "boundary" && otZones[toInfo.Zone]) ||
				(info.Zone == "boundary" && itZones[toInfo.Zone])

			if crosses {
				crossings = append(crossings, boundaryEdge{
					FromName: name,
					ToName:   toName,
					FromZone: info.Zone,
					ToZone:   toInfo.Zone,
					EdgeType: edge.Type,
				})
				boundaryNodes[name] = true
				boundaryNodes[toName] = true
			}
		}
	}

	fmt.Println("--- Boundary-Crossing Edges ---")
	fmt.Println()
	fmt.Printf("  Found %d edges crossing the IT/OT boundary:\n\n", len(crossings))

	sort.Slice(crossings, func(i, j int) bool {
		return crossings[i].FromName < crossings[j].FromName
	})

	fmt.Printf("  %-24s %-6s %-24s %-14s %-15s\n", "From", "", "To", "Edge Type", "Direction")
	fmt.Println("  " + strings.Repeat("\u2500", 88))
	for _, c := range crossings {
		direction := fmt.Sprintf("%s → %s", c.FromZone, c.ToZone)
		fmt.Printf("  %-24s   →   %-24s %-14s %s\n", c.FromName, c.ToName, c.EdgeType, direction)
	}
	fmt.Println()

	// Compute betweenness centrality for boundary nodes
	bc, err := algorithms.BetweennessCentrality(model.Graph)
	if err != nil {
		log.Fatalf("Failed to compute betweenness centrality: %v", err)
	}

	fmt.Println("--- Boundary Node Betweenness Centrality ---")
	fmt.Println()
	fmt.Printf("  %-24s %-12s %s\n", "Node", "BC Score", "Zone")
	fmt.Println("  " + strings.Repeat("\u2500", 55))

	type bcEntry struct {
		Name  string
		Zone  string
		Score float64
	}
	var boundaryBC []bcEntry
	for name := range boundaryNodes {
		info := model.Nodes[name]
		if info == nil {
			continue
		}
		boundaryBC = append(boundaryBC, bcEntry{
			Name:  name,
			Zone:  info.Zone,
			Score: bc[info.ID],
		})
	}
	sort.Slice(boundaryBC, func(i, j int) bool {
		return boundaryBC[i].Score > boundaryBC[j].Score
	})
	for _, entry := range boundaryBC {
		fmt.Printf("  %-24s %-12.6f %s\n", entry.Name, entry.Score, entry.Zone)
	}
	fmt.Println()

	// Count paths from any IT node to any OT node
	itNodeCount := 0
	otNodeCount := 0
	pathsFound := 0
	pathsChecked := 0

	var itNodes []string
	var otNodes []string
	for name, info := range model.Nodes {
		if itZones[info.Zone] {
			itNodeCount++
			itNodes = append(itNodes, name)
		}
		if otZones[info.Zone] {
			otNodeCount++
			otNodes = append(otNodes, name)
		}
	}

	for _, itName := range itNodes {
		for _, otName := range otNodes {
			pathsChecked++
			itInfo := model.Nodes[itName]
			otInfo := model.Nodes[otName]
			path, err := algorithms.ShortestPath(model.Graph, itInfo.ID, otInfo.ID)
			if err == nil && path != nil {
				pathsFound++
			}
		}
	}

	fmt.Println("--- IT → OT Path Reachability ---")
	fmt.Println()
	fmt.Printf("  IT nodes: %d | OT nodes: %d | Possible pairs: %d\n",
		itNodeCount, otNodeCount, pathsChecked)
	fmt.Printf("  Pairs with a path: %d (%.1f%%)\n",
		pathsFound, float64(pathsFound)/float64(pathsChecked)*100)
	fmt.Println()

	if pathsFound > 0 {
		fmt.Println("  The IT/OT boundary is THIN. Multiple IT nodes can reach OT nodes")
		fmt.Println("  through the boundary systems (Jump_Host, Historian_Bridge).")
	} else {
		fmt.Println("  The IT/OT boundary is STRONG. No IT node can reach any OT node.")
	}
	fmt.Println()
}

// ========================================================================
// ANALYSIS 3: Operational Dependency (The Colonial Pipeline Lesson)
// ========================================================================

func analyseOperationalDependency(model *PipelineModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" ANALYSIS 3: Operational Dependency — The Colonial Pipeline Lesson")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" \"We can't bill for fuel, so we have to shut down the pipeline.\"")
	fmt.Println("    — Colonial Pipeline management, May 2021")
	fmt.Println()

	// First, show the physical pipeline is intact by checking connectivity
	// in the full graph
	fmt.Println("--- Physical Pipeline Connectivity (BEFORE attack) ---")
	fmt.Println()

	components, err := algorithms.ConnectedComponents(model.Graph)
	if err != nil {
		log.Fatalf("Failed to compute connected components: %v", err)
	}
	fmt.Printf("  Connected components: %d (entire network is %s)\n",
		len(components.Communities),
		func() string {
			if len(components.Communities) == 1 {
				return "fully connected"
			}
			return "fragmented"
		}())
	fmt.Println()

	// Show the pipeline path
	fmt.Println("  Physical pipeline route:")
	pipelineRoute := []string{
		"Tank_Farm_North", "PS1_PLC", "PS2_PLC", "PS3_PLC",
		"PS4_PLC", "PS5_PLC", "Tank_Farm_South",
	}
	for i := 0; i < len(pipelineRoute)-1; i++ {
		from := model.Nodes[pipelineRoute[i]]
		to := model.Nodes[pipelineRoute[i+1]]
		path, err := algorithms.ShortestPath(model.Graph, from.ID, to.ID)
		status := "CONNECTED"
		if err != nil || path == nil {
			status = "BROKEN"
		}
		fmt.Printf("    %s → %s [%s]\n", pipelineRoute[i], pipelineRoute[i+1], status)
	}
	fmt.Println()

	// Now build a SECOND graph without Billing_System and Scheduling_System
	// to simulate the operational impact. We rebuild the graph without those nodes.
	fmt.Println("--- Simulating Ransomware Impact: Removing Billing + Scheduling ---")
	fmt.Println()

	removedNodes := []string{"Billing_System", "Scheduling_System"}
	for _, name := range removedNodes {
		fmt.Printf("  [ENCRYPTED] %s — system unavailable\n", name)
	}
	fmt.Println()

	// Build a second graph for the "post-attack" scenario
	postAttackModel, err := buildPostAttackModel(model, removedNodes)
	if err != nil {
		log.Fatalf("Failed to build post-attack model: %v", err)
	}
	defer postAttackModel.Graph.Close()

	postComponents, err := algorithms.ConnectedComponents(postAttackModel.Graph)
	if err != nil {
		log.Fatalf("Failed to compute post-attack components: %v", err)
	}

	fmt.Printf("  Connected components after encryption: %d\n", len(postComponents.Communities))
	fmt.Println()

	// Check if the physical pipeline route is still intact in the post-attack graph
	fmt.Println("--- Physical Pipeline Status (AFTER attack) ---")
	fmt.Println()

	allPipelineConnected := true
	for i := 0; i < len(pipelineRoute)-1; i++ {
		from := postAttackModel.Nodes[pipelineRoute[i]]
		to := postAttackModel.Nodes[pipelineRoute[i+1]]
		if from == nil || to == nil {
			fmt.Printf("    %s → %s [NODE MISSING]\n", pipelineRoute[i], pipelineRoute[i+1])
			allPipelineConnected = false
			continue
		}
		path, err := algorithms.ShortestPath(postAttackModel.Graph, from.ID, to.ID)
		status := "CONNECTED"
		if err != nil || path == nil {
			status = "BROKEN"
			allPipelineConnected = false
		}
		fmt.Printf("    %s → %s [%s]\n", pipelineRoute[i], pipelineRoute[i+1], status)
	}
	fmt.Println()

	// Check SCADA connectivity
	scadaConnected := true
	scadaServer := postAttackModel.Nodes["SCADA_Server"]
	if scadaServer != nil {
		for i := 1; i <= 5; i++ {
			rtuName := fmt.Sprintf("PS%d_RTU", i)
			rtu := postAttackModel.Nodes[rtuName]
			if rtu == nil {
				continue
			}
			path, err := algorithms.ShortestPath(postAttackModel.Graph, scadaServer.ID, rtu.ID)
			if err != nil || path == nil {
				scadaConnected = false
				break
			}
		}
	}

	// Check leak detection
	leakDetConnected := true
	leakDet := postAttackModel.Nodes["Leak_Det_Server"]
	if leakDet != nil {
		for i := 1; i <= 5; i++ {
			sensorName := fmt.Sprintf("Leak_Sensor_%d", i)
			sensor := postAttackModel.Nodes[sensorName]
			if sensor == nil {
				continue
			}
			path, err := algorithms.ShortestPath(postAttackModel.Graph, leakDet.ID, sensor.ID)
			if err != nil || path == nil {
				leakDetConnected = false
				break
			}
		}
	}

	fmt.Println("--- Operational Status Summary ---")
	fmt.Println()
	fmt.Println("  System                     Status")
	fmt.Println("  " + strings.Repeat("\u2500", 50))
	printStatus("Physical pipeline", allPipelineConnected)
	printStatus("SCADA control", scadaConnected)
	printStatus("Leak detection", leakDetConnected)
	printStatus("Billing system", false)
	printStatus("Scheduling system", false)
	printStatus("Fuel dispatch capability", false)
	printStatus("Revenue collection", false)
	fmt.Println()

	fmt.Println("=========================================================================")
	fmt.Println(" THE COLONIAL PIPELINE PARADOX")
	fmt.Println("=========================================================================")
	fmt.Println()
	if allPipelineConnected && scadaConnected && leakDetConnected {
		fmt.Println("  The physical pipeline is FULLY OPERATIONAL.")
		fmt.Println("  SCADA control is INTACT. Leak detection is WORKING.")
		fmt.Println("  All 5 pump stations can move fuel. All sensors report normally.")
		fmt.Println()
		fmt.Println("  BUT: Without billing, you can't charge customers.")
		fmt.Println("       Without scheduling, you can't dispatch deliveries.")
		fmt.Println("       Without ERP, you can't track inventory.")
		fmt.Println()
		fmt.Println("  RESULT: The pipeline shuts down VOLUNTARILY.")
		fmt.Println("          Not because it CAN'T run — because it can't BILL.")
		fmt.Println()
		fmt.Println("  Impact: 45%% of East Coast fuel supply offline for 6 days.")
		fmt.Println("          Panic buying. Gas shortages. $4.4M ransom paid.")
		fmt.Println()
		fmt.Println("  This is the attack surface that network diagrams don't show:")
		fmt.Println("  BUSINESS LOGIC DEPENDENCIES that bridge IT and OT.")
	}
	fmt.Println()
}

func printStatus(name string, operational bool) {
	status := "OPERATIONAL"
	marker := "  [OK]  "
	if !operational {
		status = "DOWN"
		marker = "  [XX]  "
	}
	fmt.Printf("  %-28s %s %s\n", name, marker, status)
}

// buildPostAttackModel reconstructs the pipeline graph minus the encrypted nodes.
// We create a fresh graph and copy everything except the removed nodes and their edges.
func buildPostAttackModel(original *PipelineModel, removedNodes []string) (*PipelineModel, error) {
	removedSet := make(map[string]bool)
	for _, name := range removedNodes {
		removedSet[name] = true
	}

	gs, err := storage.NewGraphStorage("./data/pipeline_post_attack")
	if err != nil {
		return nil, fmt.Errorf("failed to create post-attack graph: %w", err)
	}

	postModel := &PipelineModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	// Recreate all nodes except removed ones
	for name, info := range original.Nodes {
		if removedSet[name] {
			continue
		}
		props := map[string]storage.Value{
			"name": storage.StringValue(info.Name),
			"zone": storage.StringValue(info.Zone),
		}
		node, err := gs.CreateNode(info.Labels, props)
		if err != nil {
			return nil, fmt.Errorf("failed to recreate node %s: %w", name, err)
		}
		postModel.Nodes[name] = &NodeInfo{
			ID:     node.ID,
			Name:   info.Name,
			Zone:   info.Zone,
			Labels: info.Labels,
		}
		postModel.NodeByID[node.ID] = name
	}

	// Recreate all edges that don't touch removed nodes
	for _, info := range original.Nodes {
		edges, err := original.Graph.GetOutgoingEdges(info.ID)
		if err != nil {
			continue
		}
		fromName := original.NodeByID[info.ID]
		if removedSet[fromName] {
			continue
		}
		for _, edge := range edges {
			toName := original.NodeByID[edge.ToNodeID]
			if removedSet[toName] {
				continue
			}
			newFrom := postModel.Nodes[fromName]
			newTo := postModel.Nodes[toName]
			if newFrom == nil || newTo == nil {
				continue
			}
			_, err := gs.CreateEdge(newFrom.ID, newTo.ID, edge.Type, map[string]storage.Value{}, edge.Weight)
			if err != nil {
				return nil, fmt.Errorf("failed to recreate edge %s -> %s: %w", fromName, toName, err)
			}
		}
	}

	return postModel, nil
}

// ========================================================================
// ANALYSIS 4: Betweenness Centrality
// ========================================================================

func analyseBetweennessCentrality(model *PipelineModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" ANALYSIS 4: Betweenness Centrality — Network Chokepoints")
	fmt.Println("=========================================================================")
	fmt.Println()

	bc, err := algorithms.BetweennessCentrality(model.Graph)
	if err != nil {
		log.Fatalf("Failed to compute betweenness centrality: %v", err)
	}

	// Build sorted list
	type bcEntry struct {
		Name  string
		Zone  string
		Score float64
	}
	var entries []bcEntry
	for name, info := range model.Nodes {
		entries = append(entries, bcEntry{
			Name:  name,
			Zone:  info.Zone,
			Score: bc[info.ID],
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})

	// Print top 15
	fmt.Println("--- Top 15 Nodes by Betweenness Centrality ---")
	fmt.Println()
	fmt.Printf("  %-4s %-24s %-16s %s\n", "Rank", "Node", "Zone", "BC Score")
	fmt.Println("  " + strings.Repeat("\u2500", 62))
	limit := 15
	if len(entries) < limit {
		limit = len(entries)
	}
	for i := 0; i < limit; i++ {
		e := entries[i]
		marker := ""
		switch e.Name {
		case "AD_Server":
			marker = "  ← LATERAL MOVEMENT HUB"
		case "Corp_Switch":
			marker = "  ← NETWORK CHOKEPOINT"
		case "Historian_Bridge":
			marker = "  ← IT/OT BOUNDARY"
		case "Jump_Host":
			marker = "  ← IT/OT BOUNDARY"
		case "OT_Switch":
			marker = "  ← OT CHOKEPOINT"
		case "SCADA_Server":
			marker = "  ← SCADA CHOKEPOINT"
		}
		fmt.Printf("  %-4d %-24s %-16s %.6f%s\n", i+1, e.Name, e.Zone, e.Score, marker)
	}
	fmt.Println()

	// Compare average BC: IT vs OT
	itZones := map[string]bool{"corporate_it": true, "external": true, "boundary": true}
	otZones := map[string]bool{"ot_network": true, "pump_station": true, "field": true, "terminal": true}

	var itSum, otSum float64
	var itCount, otCount int

	for _, e := range entries {
		if itZones[e.Zone] {
			itSum += e.Score
			itCount++
		}
		if otZones[e.Zone] {
			otSum += e.Score
			otCount++
		}
	}

	itAvg := 0.0
	if itCount > 0 {
		itAvg = itSum / float64(itCount)
	}
	otAvg := 0.0
	if otCount > 0 {
		otAvg = otSum / float64(otCount)
	}

	fmt.Println("--- IT vs OT Betweenness Centrality ---")
	fmt.Println()
	fmt.Printf("  IT-side (%d nodes):  Average BC = %.6f  Total BC = %.6f\n", itCount, itAvg, itSum)
	fmt.Printf("  OT-side (%d nodes):  Average BC = %.6f  Total BC = %.6f\n", otCount, otAvg, otSum)
	fmt.Println()

	if itAvg > otAvg {
		ratio := 0.0
		if otAvg > 0 {
			ratio = itAvg / otAvg
		}
		fmt.Printf("  IT network average BC is %.1fx higher than OT network.\n", ratio)
		fmt.Println("  This reflects the IT network's greater interconnectedness —")
		fmt.Println("  more lateral movement opportunity for attackers.")
	} else {
		fmt.Println("  OT network has comparable or higher BC than IT network.")
	}
	fmt.Println()

	// Highlight key nodes
	fmt.Println("--- Key Node Analysis ---")
	fmt.Println()

	keyNodes := []string{
		"AD_Server", "Corp_Switch", "Historian_Bridge",
		"Jump_Host", "OT_Switch", "SCADA_Server",
		"Billing_System", "Scheduling_System",
	}
	for _, name := range keyNodes {
		info := model.Nodes[name]
		if info == nil {
			continue
		}
		score := bc[info.ID]

		description := ""
		switch name {
		case "AD_Server":
			description = "Controls authentication for all IT systems. Compromising AD means\n" +
				"                          compromising everything it authenticates."
		case "Corp_Switch":
			description = "Layer 2/3 chokepoint. All IT traffic flows through here.\n" +
				"                          Single point of failure for the entire corporate network."
		case "Historian_Bridge":
			description = "Straddles IT and OT. Collects process data from OT and\n" +
				"                          shares it with IT for business intelligence."
		case "Jump_Host":
			description = "RDP gateway to OT network. The only authorized IT→OT\n" +
				"                          access path. Compromise here = OT access."
		case "OT_Switch":
			description = "Central OT network switch. Controls SCADA, engineering\n" +
				"                          workstations, and leak detection."
		case "SCADA_Server":
			description = "Commands all 5 pump station RTUs. Loss of SCADA means\n" +
				"                          loss of automated pipeline control."
		case "Billing_System":
			description = "Revenue system. Without billing, fuel deliveries generate\n" +
				"                          no revenue — forcing voluntary shutdown."
		case "Scheduling_System":
			description = "Dispatch system. Without scheduling, fuel can't be routed\n" +
				"                          to the right terminals at the right time."
		}

		fmt.Printf("  %s (BC: %.6f, Zone: %s)\n", name, score, info.Zone)
		fmt.Printf("                          %s\n\n", description)
	}
}

// ========================================================================
// Final Summary
// ========================================================================

func printFinalSummary() {
	fmt.Println("=========================================================================")
	fmt.Println(" FINAL SUMMARY: Lessons from Colonial Pipeline")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println("  1. NETWORK SEGMENTATION IS NECESSARY BUT NOT SUFFICIENT")
	fmt.Println("     The IT/OT boundary held. Ransomware never touched SCADA, PLCs, or")
	fmt.Println("     pump stations. But the pipeline shut down anyway.")
	fmt.Println()
	fmt.Println("  2. BUSINESS LOGIC IS AN ATTACK SURFACE")
	fmt.Println("     Billing and scheduling systems are IT systems, but they're")
	fmt.Println("     operationally coupled to the physical pipeline. Encrypting them")
	fmt.Println("     is operationally equivalent to encrypting the PLCs.")
	fmt.Println()
	fmt.Println("  3. BETWEENNESS CENTRALITY REVEALS CHOKEPOINTS")
	fmt.Println("     AD_Server and Corp_Switch dominate IT-side BC, making them the")
	fmt.Println("     highest-value targets for lateral movement. Hardening these two")
	fmt.Println("     nodes would dramatically reduce blast radius.")
	fmt.Println()
	fmt.Println("  4. DEFENSE RECOMMENDATIONS")
	fmt.Println("     a. MFA on ALL remote access (the VPN credential was the entry point)")
	fmt.Println("     b. Segment billing/scheduling into their own security zone")
	fmt.Println("     c. Air-gapped or offline backups (Backup_Server was reachable)")
	fmt.Println("     d. Separate OT billing from IT billing (manual fallback process)")
	fmt.Println("     e. Reduce AD_Server lateral connectivity (tiered admin model)")
	fmt.Println()
	fmt.Println("  5. THE REAL LESSON")
	fmt.Println("     Graph analysis reveals that the most dangerous attack paths aren't")
	fmt.Println("     always the ones that cross the IT/OT boundary. Sometimes the most")
	fmt.Println("     devastating attack stays entirely within IT — and shuts down OT")
	fmt.Println("     through business dependencies that never appear on a network diagram.")
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Analysis Complete — Model 7: Pipeline Ransomware")
	fmt.Println("=========================================================================")
}

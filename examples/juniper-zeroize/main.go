// Package main models a Volt Typhoon-style network infrastructure destruction attack against
// a regional ISP backbone. An APT compromises the centralised TACACS+ authentication server,
// gaining CLI access to every Juniper device, then executes `request system zeroize media` —
// physically overwriting all storage media and permanently bricking the devices.
//
// Unlike ransomware (potentially reversible) or SCADA manipulation (software-level), zeroize
// destroys the device at a hardware level. Recovery requires physical replacement (weeks of
// supply chain lead time) plus configuration restoration from backups — which are themselves
// unreachable because the network that connects to them has been destroyed.
//
// Model 9 in "Protecting Critical Infrastructure" by Darragh Downey.
package main

import (
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ISPModel holds the graph and metadata for the ISP backbone network.
type ISPModel struct {
	Graph    *storage.GraphStorage
	Nodes    map[string]*NodeInfo
	NodeByID map[uint64]string
}

// NodeInfo stores metadata about each node for analysis and display.
type NodeInfo struct {
	ID            uint64
	Name          string
	Zone          string
	Labels        []string
	Critical      bool
	JuniperDevice bool
}

// juniperDevices lists all Juniper switches that can be bricked via zeroize.
var juniperDevices = []string{
	"Agg_Switch_Hub",
	"Agg_Switch_PoP1",
	"Agg_Switch_PoP2",
	"Agg_Switch_PoP3",
	"Agg_Switch_PoP4",
}

// criticalSystems lists life-safety and mission-critical customer systems.
var criticalSystems = []string{
	"Hospital_EHR",
	"Water_SCADA",
	"E911_System",
	"Power_EMS",
	"Police_CAD",
	"Fire_Dispatch",
}

// managementTools lists the infrastructure management systems needed for recovery.
var managementTools = []string{
	"TACACS_Server",
	"Oxidized_Server",
	"SNMP_Monitor",
	"Syslog_Server",
	"Jump_Host",
}

func main() {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Network Infrastructure Destruction: Juniper Zeroize Attack")
	fmt.Println(" Model 9: Protecting Critical Infrastructure — Darragh Downey")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Inspired by: Volt Typhoon (PRC-linked APT pre-positioning in US ISPs)")
	fmt.Println(" Attack Vector: TACACS+ compromise → CLI access → request system zeroize media")
	fmt.Println(" Key Lesson: Centralised authentication turns every device into a single")
	fmt.Println("             point of failure, and the tools needed to recover from the")
	fmt.Println("             attack are themselves victims of the attack.")
	fmt.Println()

	// Clean slate for each run
	if err := os.RemoveAll("./data"); err != nil {
		log.Printf("Warning: failed to clean data directory: %v", err)
	}

	model, err := buildISPNetwork("./data/isp", nil)
	if err != nil {
		log.Fatalf("Failed to build ISP network: %v", err)
	}
	defer model.Graph.Close()

	stats := model.Graph.GetStatistics()
	fmt.Printf(" Network built: %d nodes, %d edges\n", stats.NodeCount, stats.EdgeCount)
	fmt.Println()

	// ================================================================
	// PHASE 1: Attack Path — TACACS+ Compromise
	// ================================================================
	analyseAttackPath(model)

	// ================================================================
	// PHASE 2: Betweenness Centrality — Aggregation as Chokepoint
	// ================================================================
	analyseBetweennessCentrality(model)

	// ================================================================
	// PHASE 3: Single Switch Destruction — Hub Bricked
	// ================================================================
	analyseHubDestruction(model)

	// ================================================================
	// PHASE 4: Progressive Destruction (Volt Typhoon Scenario)
	// ================================================================
	analyseProgressiveDestruction(model)

	// ================================================================
	// PHASE 5: Recovery Paradox
	// ================================================================
	analyseRecoveryParadox(model)

	// ================================================================
	// PHASE 6: Redundancy Analysis
	// ================================================================
	analyseRedundancy()

	// ================================================================
	// Final Summary
	// ================================================================
	printFinalSummary()
}

// buildISPNetwork constructs the ISP backbone graph, excluding any nodes in excludedNodes.
func buildISPNetwork(dataDir string, excludedNodes []string) (*ISPModel, error) {
	excluded := make(map[string]bool)
	for _, name := range excludedNodes {
		excluded[name] = true
	}

	gs, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	model := &ISPModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	// Helper to create a node and register it in our lookup maps.
	addNode := func(name, zone string, labels []string, critical, juniper bool) {
		if excluded[name] {
			return
		}
		props := map[string]storage.Value{
			"name":           storage.StringValue(name),
			"zone":           storage.StringValue(zone),
			"critical":       storage.BoolValue(critical),
			"juniper_device": storage.BoolValue(juniper),
		}
		node, err := gs.CreateNode(labels, props)
		if err != nil {
			log.Fatalf("Failed to create node %s: %v", name, err)
		}
		info := &NodeInfo{
			ID:            node.ID,
			Name:          name,
			Zone:          zone,
			Labels:        labels,
			Critical:      critical,
			JuniperDevice: juniper,
		}
		model.Nodes[name] = info
		model.NodeByID[node.ID] = name
	}

	// Helper to create a directed edge.
	addEdge := func(fromName, toName, edgeType string) {
		from := model.Nodes[fromName]
		to := model.Nodes[toName]
		if from == nil || to == nil {
			return // skip edges involving excluded nodes
		}
		_, err := gs.CreateEdge(from.ID, to.ID, edgeType, map[string]storage.Value{}, 1.0)
		if err != nil {
			log.Fatalf("Failed to create edge %s -> %s: %v", fromName, toName, err)
		}
	}

	// Helper to create a bidirectional (undirected) edge pair.
	addBiEdge := func(aName, bName, edgeType string) {
		addEdge(aName, bName, edgeType)
		addEdge(bName, aName, edgeType)
	}

	// ---------------------------------------------------------------
	// NODES
	// ---------------------------------------------------------------

	// External (zone: external)
	addNode("Internet_Exchange", "external", []string{"IXP"}, false, false)
	addNode("Upstream_Provider_A", "external", []string{"Transit"}, false, false)
	addNode("Upstream_Provider_B", "external", []string{"Transit"}, false, false)

	// Core (zone: core)
	addNode("Core_Router_A", "core", []string{"Router", "MX960"}, false, false)
	addNode("Core_Router_B", "core", []string{"Router", "MX960"}, false, false)
	addNode("Agg_Switch_Hub", "core", []string{"Switch", "QFX5200", "JuniperDevice"}, false, true)
	addNode("Route_Reflector", "core", []string{"Router", "RouteReflector"}, false, false)

	// Management Plane (zone: management)
	addNode("TACACS_Server", "management", []string{"Server", "Authentication"}, true, false)
	addNode("Oxidized_Server", "management", []string{"Server", "ConfigBackup"}, true, false)
	addNode("SNMP_Monitor", "management", []string{"Server", "Monitoring"}, true, false)
	addNode("Syslog_Server", "management", []string{"Server", "Logging"}, true, false)
	addNode("Jump_Host", "management", []string{"Server", "Bastion"}, true, false)
	addNode("NOC_Workstation", "management", []string{"Workstation", "NOC"}, false, false)

	// PoP 1 — Metro East (zone: pop1)
	addNode("Agg_Switch_PoP1", "pop1", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP1", "pop1", []string{"Switch", "Access"}, false, false)
	addNode("Hospital_CE", "pop1", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Hospital_EHR", "pop1", []string{"System", "EHR", "LifeCritical"}, true, false)
	addNode("Clinic_CE", "pop1", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Clinic_Systems", "pop1", []string{"System", "Telemedicine"}, false, false)

	// PoP 2 — Metro West (zone: pop2)
	addNode("Agg_Switch_PoP2", "pop2", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP2", "pop2", []string{"Switch", "Access"}, false, false)
	addNode("Water_Utility_CE", "pop2", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Water_SCADA", "pop2", []string{"System", "SCADA", "LifeSafety"}, true, false)
	addNode("Emergency_CE", "pop2", []string{"Router", "CustomerEdge"}, false, false)
	addNode("E911_System", "pop2", []string{"System", "E911", "LifeSafety"}, true, false)

	// PoP 3 — Industrial (zone: pop3)
	addNode("Agg_Switch_PoP3", "pop3", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP3", "pop3", []string{"Switch", "Access"}, false, false)
	addNode("Power_Company_CE", "pop3", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Power_EMS", "pop3", []string{"System", "EMS", "LifeCritical"}, true, false)
	addNode("Manufacturer_CE", "pop3", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Manufacturing_MES", "pop3", []string{"System", "MES"}, false, false)

	// PoP 4 — Government (zone: pop4)
	addNode("Agg_Switch_PoP4", "pop4", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP4", "pop4", []string{"Switch", "Access"}, false, false)
	addNode("Gov_Firewall", "pop4", []string{"Firewall", "Government"}, false, false)
	addNode("Municipal_Systems", "pop4", []string{"System", "Municipal"}, false, false)
	addNode("Police_CAD", "pop4", []string{"System", "CAD", "LifeSafety"}, true, false)
	addNode("Fire_Dispatch", "pop4", []string{"System", "Dispatch", "LifeSafety"}, true, false)

	// ---------------------------------------------------------------
	// EDGES
	// ---------------------------------------------------------------

	// External → Core (directed BGP peering)
	addEdge("Internet_Exchange", "Upstream_Provider_A", "BGP_PEER")
	addEdge("Internet_Exchange", "Upstream_Provider_B", "BGP_PEER")
	addEdge("Upstream_Provider_A", "Core_Router_A", "BGP_PEER")
	addEdge("Upstream_Provider_B", "Core_Router_B", "BGP_PEER")

	// Core interconnections (bidirectional trunks)
	addBiEdge("Core_Router_A", "Core_Router_B", "TRUNK")
	addBiEdge("Core_Router_A", "Agg_Switch_Hub", "TRUNK")
	addBiEdge("Core_Router_B", "Agg_Switch_Hub", "TRUNK")
	addBiEdge("Core_Router_A", "Route_Reflector", "TRUNK")
	addBiEdge("Core_Router_B", "Route_Reflector", "TRUNK")

	// Hub aggregation to PoP aggregation (bidirectional distribution)
	addBiEdge("Agg_Switch_Hub", "Agg_Switch_PoP1", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub", "Agg_Switch_PoP2", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub", "Agg_Switch_PoP3", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub", "Agg_Switch_PoP4", "DISTRIBUTION")

	// PoP aggregation to access switches (bidirectional)
	addBiEdge("Agg_Switch_PoP1", "Access_Switch_PoP1", "ACCESS")
	addBiEdge("Agg_Switch_PoP2", "Access_Switch_PoP2", "ACCESS")
	addBiEdge("Agg_Switch_PoP3", "Access_Switch_PoP3", "ACCESS")
	addBiEdge("Agg_Switch_PoP4", "Access_Switch_PoP4", "ACCESS")

	// Access switches to customer edge routers (bidirectional)
	addBiEdge("Access_Switch_PoP1", "Hospital_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP1", "Clinic_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP2", "Water_Utility_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP2", "Emergency_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP3", "Power_Company_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP3", "Manufacturer_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP4", "Gov_Firewall", "CUSTOMER_PE")

	// Customer edge to customer systems (bidirectional)
	addBiEdge("Hospital_CE", "Hospital_EHR", "CUSTOMER_LAN")
	addBiEdge("Clinic_CE", "Clinic_Systems", "CUSTOMER_LAN")
	addBiEdge("Water_Utility_CE", "Water_SCADA", "CUSTOMER_LAN")
	addBiEdge("Emergency_CE", "E911_System", "CUSTOMER_LAN")
	addBiEdge("Power_Company_CE", "Power_EMS", "CUSTOMER_LAN")
	addBiEdge("Manufacturer_CE", "Manufacturing_MES", "CUSTOMER_LAN")
	addBiEdge("Gov_Firewall", "Municipal_Systems", "CUSTOMER_LAN")
	addBiEdge("Gov_Firewall", "Police_CAD", "CUSTOMER_LAN")
	addBiEdge("Gov_Firewall", "Fire_Dispatch", "CUSTOMER_LAN")

	// Management plane connections (bidirectional)
	addBiEdge("Agg_Switch_Hub", "NOC_Workstation", "MANAGEMENT")
	addBiEdge("NOC_Workstation", "Jump_Host", "MANAGEMENT")
	addBiEdge("NOC_Workstation", "SNMP_Monitor", "MANAGEMENT")
	addBiEdge("NOC_Workstation", "Syslog_Server", "MANAGEMENT")
	addBiEdge("Jump_Host", "TACACS_Server", "MANAGEMENT")
	addBiEdge("Jump_Host", "Oxidized_Server", "MANAGEMENT")

	return model, nil
}

// buildDualAggNetwork constructs a redundant ISP backbone with dual hub aggregation.
func buildDualAggNetwork(dataDir string, excludedNodes []string) (*ISPModel, error) {
	excluded := make(map[string]bool)
	for _, name := range excludedNodes {
		excluded[name] = true
	}

	gs, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	model := &ISPModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	addNode := func(name, zone string, labels []string, critical, juniper bool) {
		if excluded[name] {
			return
		}
		props := map[string]storage.Value{
			"name":           storage.StringValue(name),
			"zone":           storage.StringValue(zone),
			"critical":       storage.BoolValue(critical),
			"juniper_device": storage.BoolValue(juniper),
		}
		node, err := gs.CreateNode(labels, props)
		if err != nil {
			log.Fatalf("Failed to create node %s: %v", name, err)
		}
		info := &NodeInfo{
			ID:            node.ID,
			Name:          name,
			Zone:          zone,
			Labels:        labels,
			Critical:      critical,
			JuniperDevice: juniper,
		}
		model.Nodes[name] = info
		model.NodeByID[node.ID] = name
	}

	addEdge := func(fromName, toName, edgeType string) {
		from := model.Nodes[fromName]
		to := model.Nodes[toName]
		if from == nil || to == nil {
			return
		}
		_, err := gs.CreateEdge(from.ID, to.ID, edgeType, map[string]storage.Value{}, 1.0)
		if err != nil {
			log.Fatalf("Failed to create edge %s -> %s: %v", fromName, toName, err)
		}
	}

	addBiEdge := func(aName, bName, edgeType string) {
		addEdge(aName, bName, edgeType)
		addEdge(bName, aName, edgeType)
	}

	// ---------------------------------------------------------------
	// NODES — same as standard network but with dual hub aggregation
	// ---------------------------------------------------------------

	// External
	addNode("Internet_Exchange", "external", []string{"IXP"}, false, false)
	addNode("Upstream_Provider_A", "external", []string{"Transit"}, false, false)
	addNode("Upstream_Provider_B", "external", []string{"Transit"}, false, false)

	// Core — DUAL hub aggregation
	addNode("Core_Router_A", "core", []string{"Router", "MX960"}, false, false)
	addNode("Core_Router_B", "core", []string{"Router", "MX960"}, false, false)
	addNode("Agg_Switch_Hub_A", "core", []string{"Switch", "QFX5200", "JuniperDevice"}, false, true)
	addNode("Agg_Switch_Hub_B", "core", []string{"Switch", "QFX5200", "JuniperDevice"}, false, true)
	addNode("Route_Reflector", "core", []string{"Router", "RouteReflector"}, false, false)

	// Management Plane
	addNode("TACACS_Server", "management", []string{"Server", "Authentication"}, true, false)
	addNode("Oxidized_Server", "management", []string{"Server", "ConfigBackup"}, true, false)
	addNode("SNMP_Monitor", "management", []string{"Server", "Monitoring"}, true, false)
	addNode("Syslog_Server", "management", []string{"Server", "Logging"}, true, false)
	addNode("Jump_Host", "management", []string{"Server", "Bastion"}, true, false)
	addNode("NOC_Workstation", "management", []string{"Workstation", "NOC"}, false, false)

	// PoP switches
	addNode("Agg_Switch_PoP1", "pop1", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP1", "pop1", []string{"Switch", "Access"}, false, false)
	addNode("Hospital_CE", "pop1", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Hospital_EHR", "pop1", []string{"System", "EHR", "LifeCritical"}, true, false)
	addNode("Clinic_CE", "pop1", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Clinic_Systems", "pop1", []string{"System", "Telemedicine"}, false, false)

	addNode("Agg_Switch_PoP2", "pop2", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP2", "pop2", []string{"Switch", "Access"}, false, false)
	addNode("Water_Utility_CE", "pop2", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Water_SCADA", "pop2", []string{"System", "SCADA", "LifeSafety"}, true, false)
	addNode("Emergency_CE", "pop2", []string{"Router", "CustomerEdge"}, false, false)
	addNode("E911_System", "pop2", []string{"System", "E911", "LifeSafety"}, true, false)

	addNode("Agg_Switch_PoP3", "pop3", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP3", "pop3", []string{"Switch", "Access"}, false, false)
	addNode("Power_Company_CE", "pop3", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Power_EMS", "pop3", []string{"System", "EMS", "LifeCritical"}, true, false)
	addNode("Manufacturer_CE", "pop3", []string{"Router", "CustomerEdge"}, false, false)
	addNode("Manufacturing_MES", "pop3", []string{"System", "MES"}, false, false)

	addNode("Agg_Switch_PoP4", "pop4", []string{"Switch", "EX4650", "JuniperDevice"}, false, true)
	addNode("Access_Switch_PoP4", "pop4", []string{"Switch", "Access"}, false, false)
	addNode("Gov_Firewall", "pop4", []string{"Firewall", "Government"}, false, false)
	addNode("Municipal_Systems", "pop4", []string{"System", "Municipal"}, false, false)
	addNode("Police_CAD", "pop4", []string{"System", "CAD", "LifeSafety"}, true, false)
	addNode("Fire_Dispatch", "pop4", []string{"System", "Dispatch", "LifeSafety"}, true, false)

	// ---------------------------------------------------------------
	// EDGES — dual hub topology
	// ---------------------------------------------------------------

	// External → Core
	addEdge("Internet_Exchange", "Upstream_Provider_A", "BGP_PEER")
	addEdge("Internet_Exchange", "Upstream_Provider_B", "BGP_PEER")
	addEdge("Upstream_Provider_A", "Core_Router_A", "BGP_PEER")
	addEdge("Upstream_Provider_B", "Core_Router_B", "BGP_PEER")

	// Core interconnections
	addBiEdge("Core_Router_A", "Core_Router_B", "TRUNK")
	addBiEdge("Core_Router_A", "Agg_Switch_Hub_A", "TRUNK")
	addBiEdge("Core_Router_A", "Agg_Switch_Hub_B", "TRUNK")
	addBiEdge("Core_Router_B", "Agg_Switch_Hub_A", "TRUNK")
	addBiEdge("Core_Router_B", "Agg_Switch_Hub_B", "TRUNK")
	addBiEdge("Agg_Switch_Hub_A", "Agg_Switch_Hub_B", "TRUNK")
	addBiEdge("Core_Router_A", "Route_Reflector", "TRUNK")
	addBiEdge("Core_Router_B", "Route_Reflector", "TRUNK")

	// DUAL distribution — each PoP connects to BOTH hubs
	addBiEdge("Agg_Switch_Hub_A", "Agg_Switch_PoP1", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_B", "Agg_Switch_PoP1", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_A", "Agg_Switch_PoP2", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_B", "Agg_Switch_PoP2", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_A", "Agg_Switch_PoP3", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_B", "Agg_Switch_PoP3", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_A", "Agg_Switch_PoP4", "DISTRIBUTION")
	addBiEdge("Agg_Switch_Hub_B", "Agg_Switch_PoP4", "DISTRIBUTION")

	// PoP aggregation to access switches
	addBiEdge("Agg_Switch_PoP1", "Access_Switch_PoP1", "ACCESS")
	addBiEdge("Agg_Switch_PoP2", "Access_Switch_PoP2", "ACCESS")
	addBiEdge("Agg_Switch_PoP3", "Access_Switch_PoP3", "ACCESS")
	addBiEdge("Agg_Switch_PoP4", "Access_Switch_PoP4", "ACCESS")

	// Access to customer edge
	addBiEdge("Access_Switch_PoP1", "Hospital_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP1", "Clinic_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP2", "Water_Utility_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP2", "Emergency_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP3", "Power_Company_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP3", "Manufacturer_CE", "CUSTOMER_PE")
	addBiEdge("Access_Switch_PoP4", "Gov_Firewall", "CUSTOMER_PE")

	// Customer LAN
	addBiEdge("Hospital_CE", "Hospital_EHR", "CUSTOMER_LAN")
	addBiEdge("Clinic_CE", "Clinic_Systems", "CUSTOMER_LAN")
	addBiEdge("Water_Utility_CE", "Water_SCADA", "CUSTOMER_LAN")
	addBiEdge("Emergency_CE", "E911_System", "CUSTOMER_LAN")
	addBiEdge("Power_Company_CE", "Power_EMS", "CUSTOMER_LAN")
	addBiEdge("Manufacturer_CE", "Manufacturing_MES", "CUSTOMER_LAN")
	addBiEdge("Gov_Firewall", "Municipal_Systems", "CUSTOMER_LAN")
	addBiEdge("Gov_Firewall", "Police_CAD", "CUSTOMER_LAN")
	addBiEdge("Gov_Firewall", "Fire_Dispatch", "CUSTOMER_LAN")

	// Management plane — connects via BOTH hubs
	addBiEdge("Agg_Switch_Hub_A", "NOC_Workstation", "MANAGEMENT")
	addBiEdge("Agg_Switch_Hub_B", "NOC_Workstation", "MANAGEMENT")
	addBiEdge("NOC_Workstation", "Jump_Host", "MANAGEMENT")
	addBiEdge("NOC_Workstation", "SNMP_Monitor", "MANAGEMENT")
	addBiEdge("NOC_Workstation", "Syslog_Server", "MANAGEMENT")
	addBiEdge("Jump_Host", "TACACS_Server", "MANAGEMENT")
	addBiEdge("Jump_Host", "Oxidized_Server", "MANAGEMENT")

	return model, nil
}

// ========================================================================
// PHASE 1: Attack Path — TACACS+ Compromise
// ========================================================================

func analyseAttackPath(model *ISPModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 1: Attack Path — TACACS+ Compromise")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Attack narrative: APT compromises TACACS+ server via supply chain or")
	fmt.Println(" credential theft, gaining CLI authentication for EVERY Juniper device.")
	fmt.Println(" From Jump_Host, the attacker can SSH to any network device and execute")
	fmt.Println(" `request system zeroize media` — permanently destroying the device.")
	fmt.Println()

	// Show the path from Internet to TACACS+
	fmt.Println("--- Path: Internet → TACACS+ Server ---")
	fmt.Println()

	internetExchange := model.Nodes["Internet_Exchange"]
	tacacsServer := model.Nodes["TACACS_Server"]

	if internetExchange != nil && tacacsServer != nil {
		path, err := algorithms.ShortestPath(model.Graph, internetExchange.ID, tacacsServer.ID)
		if err != nil || path == nil {
			fmt.Println("  No path found from Internet_Exchange to TACACS_Server")
		} else {
			fmt.Printf("  Hops: %d\n", len(path)-1)
			fmt.Print("  Path: ")
			for i, nodeID := range path {
				name := model.NodeByID[nodeID]
				if i > 0 {
					fmt.Print(" → ")
				}
				fmt.Print(name)
			}
			fmt.Println()
		}
	}
	fmt.Println()

	// Show paths from Jump_Host to each Juniper device (via management plane → hub)
	fmt.Println("--- Post-Compromise: Jump_Host → Juniper Devices (via network) ---")
	fmt.Println()
	fmt.Printf("  %-22s %6s   %s\n", "Target Device", "Hops", "Path")
	fmt.Println("  " + strings.Repeat("─", 70))

	jumpHost := model.Nodes["Jump_Host"]
	if jumpHost == nil {
		fmt.Println("  Jump_Host not found")
		fmt.Println()
		return
	}

	for _, deviceName := range juniperDevices {
		device := model.Nodes[deviceName]
		if device == nil {
			continue
		}
		path, err := algorithms.ShortestPath(model.Graph, jumpHost.ID, device.ID)
		if err != nil || path == nil {
			fmt.Printf("  %-22s %6s   %s\n", deviceName, "N/A", "NO PATH")
			continue
		}

		var pathNames []string
		for _, nodeID := range path {
			pathNames = append(pathNames, model.NodeByID[nodeID])
		}
		fmt.Printf("  %-22s %6d   %s\n", deviceName, len(path)-1, strings.Join(pathNames, " → "))
	}
	fmt.Println()

	fmt.Println("  CRITICAL: With TACACS+ credentials, the attacker has authenticated")
	fmt.Println("  CLI access to ALL 5 Juniper switches simultaneously. A single")
	fmt.Println("  compromised authentication server = 5 devices ready to brick.")
	fmt.Println()
}

// ========================================================================
// PHASE 2: Betweenness Centrality — Aggregation as Chokepoint
// ========================================================================

func analyseBetweennessCentrality(model *ISPModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 2: Betweenness Centrality — Aggregation as Chokepoint")
	fmt.Println("=========================================================================")
	fmt.Println()

	bc, err := algorithms.BetweennessCentrality(model.Graph)
	if err != nil {
		log.Fatalf("Failed to compute betweenness centrality: %v", err)
	}

	type bcEntry struct {
		Name  string
		Zone  string
		Score float64
		Info  *NodeInfo
	}
	var entries []bcEntry
	for name, info := range model.Nodes {
		entries = append(entries, bcEntry{
			Name:  name,
			Zone:  info.Zone,
			Score: bc[info.ID],
			Info:  info,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})

	fmt.Println("--- Top 15 Nodes by Betweenness Centrality ---")
	fmt.Println()
	fmt.Printf("  %-4s %-22s %-14s %-12s %s\n", "Rank", "Node", "Zone", "BC Score", "Annotation")
	fmt.Println("  " + strings.Repeat("─", 82))

	limit := min(len(entries), 15)
	for i := range limit {
		e := entries[i]
		annotation := ""
		if e.Info.JuniperDevice {
			annotation = "** JUNIPER — ZEROIZE TARGET **"
		} else if e.Info.Critical {
			annotation = "[CRITICAL INFRASTRUCTURE]"
		}
		switch e.Name {
		case "TACACS_Server":
			annotation = "[MANAGEMENT PLANE BRIDGE]"
		case "NOC_Workstation":
			annotation = "[MANAGEMENT PLANE]"
		case "Jump_Host":
			annotation = "[SSH BASTION — ATTACK VECTOR]"
		case "Core_Router_A", "Core_Router_B":
			annotation = "[CORE ROUTER]"
		case "Route_Reflector":
			annotation = "[BGP ROUTE REFLECTOR]"
		}
		fmt.Printf("  %-4d %-22s %-14s %-12.6f %s\n", i+1, e.Name, e.Zone, e.Score, annotation)
	}
	fmt.Println()

	// Highlight that Agg_Switch_Hub should be #1
	if len(entries) > 0 && entries[0].Name == "Agg_Switch_Hub" {
		fmt.Println("  Agg_Switch_Hub ranks #1 — it bridges ALL PoPs to the core network.")
		fmt.Println("  Destroying this single switch fragments the entire ISP backbone.")
	} else if len(entries) > 0 {
		fmt.Printf("  Top node: %s (BC: %.6f)\n", entries[0].Name, entries[0].Score)
	}
	fmt.Println()
}

// ========================================================================
// PHASE 3: Single Switch Destruction — Hub Bricked
// ========================================================================

func analyseHubDestruction(_ *ISPModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 3: Hub Destruction — `request system zeroize media`")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" The attacker SSHs to Agg_Switch_Hub (Juniper QFX5200) and executes:")
	fmt.Println("   root@Agg_Switch_Hub> request system zeroize media")
	fmt.Println()
	fmt.Println(" This command overwrites ALL storage media. The switch is permanently")
	fmt.Println(" bricked. No software recovery is possible. Physical replacement required.")
	fmt.Println()

	// Build degraded graph without the hub
	degraded, err := buildISPNetwork("./data/phase3_hub_bricked", []string{"Agg_Switch_Hub"})
	if err != nil {
		log.Fatalf("Failed to build degraded network: %v", err)
	}
	defer degraded.Graph.Close()

	components, err := algorithms.ConnectedComponents(degraded.Graph)
	if err != nil {
		log.Fatalf("Failed to compute connected components: %v", err)
	}

	fmt.Printf("  Connected components after hub destruction: %d\n", len(components.Communities))
	fmt.Println()

	// Print each component with its members
	fmt.Println("--- Network Fragments ---")
	fmt.Println()

	// Sort components by size descending
	sortedComms := make([]*algorithms.Community, len(components.Communities))
	copy(sortedComms, components.Communities)
	sort.Slice(sortedComms, func(i, j int) bool {
		return sortedComms[i].Size > sortedComms[j].Size
	})

	for i, comm := range sortedComms {
		var names []string
		hasCritical := false
		for _, nodeID := range comm.Nodes {
			name := degraded.NodeByID[nodeID]
			names = append(names, name)
			info := degraded.Nodes[name]
			if info != nil && info.Critical {
				hasCritical = true
			}
		}
		sort.Strings(names)

		criticalMarker := ""
		if hasCritical {
			criticalMarker = " [CONTAINS CRITICAL SERVICES]"
		}
		fmt.Printf("  Component %d (%d nodes)%s:\n", i+1, comm.Size, criticalMarker)
		for _, name := range names {
			info := degraded.Nodes[name]
			marker := ""
			if info != nil && info.Critical {
				marker = " ** CRITICAL **"
			}
			if info != nil && info.JuniperDevice {
				marker = " [JUNIPER]"
			}
			fmt.Printf("    - %s%s\n", name, marker)
		}
		fmt.Println()
	}

	// Count unreachable critical services
	internetNode := degraded.Nodes["Internet_Exchange"]
	unreachableCritical := 0
	for _, sysName := range criticalSystems {
		sys := degraded.Nodes[sysName]
		if sys == nil {
			unreachableCritical++
			continue
		}
		if internetNode == nil {
			unreachableCritical++
			continue
		}
		path, err := algorithms.ShortestPath(degraded.Graph, internetNode.ID, sys.ID)
		if err != nil || path == nil {
			unreachableCritical++
		}
	}

	fmt.Printf("  Critical services unreachable from Internet: %d of %d\n",
		unreachableCritical, len(criticalSystems))
	fmt.Println()
}

// ========================================================================
// PHASE 4: Progressive Destruction (Volt Typhoon Scenario)
// ========================================================================

func analyseProgressiveDestruction(_ *ISPModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 4: Progressive Destruction — Volt Typhoon Scenario")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" The attacker executes `zeroize media` on all Juniper switches in sequence,")
	fmt.Println(" starting with the central hub and progressing through each PoP.")
	fmt.Println()

	// Define progressive destruction phases
	phases := []struct {
		Name     string
		Excluded []string
	}{
		{"Baseline (no attack)", nil},
		{"Hub bricked", []string{"Agg_Switch_Hub"}},
		{"+ PoP1 bricked", []string{"Agg_Switch_Hub", "Agg_Switch_PoP1"}},
		{"+ PoP2 bricked", []string{"Agg_Switch_Hub", "Agg_Switch_PoP1", "Agg_Switch_PoP2"}},
		{"+ PoP3 bricked", []string{"Agg_Switch_Hub", "Agg_Switch_PoP1", "Agg_Switch_PoP2", "Agg_Switch_PoP3"}},
		{"All Juniper bricked", juniperDevices},
	}

	// Print table header
	fmt.Printf("  %-25s %8s %12s %12s %10s\n",
		"Phase", "Bricked", "Components", "Reachable", "Critical")
	fmt.Println("  " + strings.Repeat("─", 70))

	for i, phase := range phases {
		dataDir := fmt.Sprintf("./data/phase4_step%d", i)
		degraded, err := buildISPNetwork(dataDir, phase.Excluded)
		if err != nil {
			log.Fatalf("Failed to build degraded network for phase %d: %v", i, err)
		}

		components, err := algorithms.ConnectedComponents(degraded.Graph)
		if err != nil {
			degraded.Graph.Close()
			log.Fatalf("Failed to compute components for phase %d: %v", i, err)
		}

		// Count customers reachable from Internet
		internetNode := degraded.Nodes["Internet_Exchange"]
		reachableCustomers := 0
		criticalReachable := 0
		customerSystems := []string{
			"Hospital_EHR", "Clinic_Systems", "Water_SCADA", "E911_System",
			"Power_EMS", "Manufacturing_MES", "Municipal_Systems", "Police_CAD", "Fire_Dispatch",
		}

		for _, sysName := range customerSystems {
			sys := degraded.Nodes[sysName]
			if sys == nil || internetNode == nil {
				continue
			}
			path, err := algorithms.ShortestPath(degraded.Graph, internetNode.ID, sys.ID)
			if err == nil && path != nil {
				reachableCustomers++
				if slices.Contains(criticalSystems, sysName) {
					criticalReachable++
				}
			}
		}

		fmt.Printf("  %-25s %8d %12d %12d %10d\n",
			phase.Name,
			len(phase.Excluded),
			len(components.Communities),
			reachableCustomers,
			criticalReachable,
		)

		degraded.Graph.Close()
	}
	fmt.Println()

	fmt.Println("  Reachable = customer systems reachable from Internet_Exchange")
	fmt.Println("  Critical  = life-safety systems still reachable (of 6 total)")
	fmt.Println()
	fmt.Println("  The fragmentation is MONOTONICALLY INCREASING. Each bricked switch")
	fmt.Println("  isolates more customers. By the final phase, no customer system")
	fmt.Println("  can reach the Internet — the regional ISP backbone is destroyed.")
	fmt.Println()
}

// ========================================================================
// PHASE 5: Recovery Paradox
// ========================================================================

func analyseRecoveryParadox(_ *ISPModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 5: Recovery Paradox — The Tools Are Also Victims")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" After bricking Agg_Switch_Hub, check whether the management tools")
	fmt.Println(" needed to recover from the attack are still reachable.")
	fmt.Println()

	// Build degraded network (hub bricked)
	degraded, err := buildISPNetwork("./data/phase5_recovery", []string{"Agg_Switch_Hub"})
	if err != nil {
		log.Fatalf("Failed to build degraded network: %v", err)
	}
	defer degraded.Graph.Close()

	// Check reachability of each management tool from a PoP device
	// (i.e., can a technician at a PoP reach the management plane?)
	fmt.Println("--- Management Tool Reachability (from PoP1 Access Switch) ---")
	fmt.Println()
	fmt.Printf("  %-22s %-14s %s\n", "Management Tool", "Status", "Recovery Impact")
	fmt.Println("  " + strings.Repeat("─", 75))

	accessSwitch := degraded.Nodes["Access_Switch_PoP1"]

	toolDescriptions := map[string]string{
		"TACACS_Server":  "Can't authenticate to remaining devices",
		"Oxidized_Server": "Can't pull backup configs for replacement hardware",
		"SNMP_Monitor":   "Can't see what's happening on the network",
		"Syslog_Server":  "Can't see attack logs or forensic evidence",
		"Jump_Host":      "Can't SSH to any surviving network device",
	}

	allUnreachable := true
	for _, toolName := range managementTools {
		tool := degraded.Nodes[toolName]
		description := toolDescriptions[toolName]

		if tool == nil || accessSwitch == nil {
			fmt.Printf("  %-22s %-14s %s\n", toolName, "UNREACHABLE", description)
			continue
		}

		path, err := algorithms.ShortestPath(degraded.Graph, accessSwitch.ID, tool.ID)
		if err != nil || path == nil {
			fmt.Printf("  %-22s %-14s %s\n", toolName, "UNREACHABLE", description)
		} else {
			fmt.Printf("  %-22s %-14s Reachable (%d hops)\n", toolName, "REACHABLE", len(path)-1)
			allUnreachable = false
		}
	}
	fmt.Println()

	// Also check from Internet
	fmt.Println("--- Management Tool Reachability (from Internet) ---")
	fmt.Println()
	fmt.Printf("  %-22s %-14s\n", "Management Tool", "Status")
	fmt.Println("  " + strings.Repeat("─", 40))

	internetNode := degraded.Nodes["Internet_Exchange"]
	for _, toolName := range managementTools {
		tool := degraded.Nodes[toolName]
		status := "UNREACHABLE"
		if tool != nil && internetNode != nil {
			path, err := algorithms.ShortestPath(degraded.Graph, internetNode.ID, tool.ID)
			if err == nil && path != nil {
				status = fmt.Sprintf("REACHABLE (%d hops)", len(path)-1)
			}
		}
		fmt.Printf("  %-22s %-14s\n", toolName, status)
	}
	fmt.Println()

	if allUnreachable {
		fmt.Println("  ╔══════════════════════════════════════════════════════════════════╗")
		fmt.Println("  ║  THE RECOVERY PARADOX                                           ║")
		fmt.Println("  ║                                                                 ║")
		fmt.Println("  ║  The tools needed to recover from the attack are themselves      ║")
		fmt.Println("  ║  victims of the attack.                                         ║")
		fmt.Println("  ║                                                                 ║")
		fmt.Println("  ║  • TACACS+ is unreachable → can't authenticate to devices       ║")
		fmt.Println("  ║  • Oxidized is unreachable → can't retrieve backup configs      ║")
		fmt.Println("  ║  • SNMP/Syslog unreachable → can't monitor or investigate       ║")
		fmt.Println("  ║  • Jump Host unreachable → can't SSH to anything                ║")
		fmt.Println("  ║                                                                 ║")
		fmt.Println("  ║  Recovery requires PHYSICAL truck rolls to every PoP, with       ║")
		fmt.Println("  ║  replacement hardware that has weeks of supply chain lead time.  ║")
		fmt.Println("  ╚══════════════════════════════════════════════════════════════════╝")
	}
	fmt.Println()
}

// ========================================================================
// PHASE 6: Redundancy Analysis — Dual Aggregation
// ========================================================================

func analyseRedundancy() {
	fmt.Println("=========================================================================")
	fmt.Println(" PHASE 6: Redundancy Analysis — Dual Hub Aggregation")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" What if the ISP had deployed redundant hub aggregation switches?")
	fmt.Println(" We compare single-hub vs dual-hub architectures.")
	fmt.Println()

	// Build dual-agg network (no exclusions) for baseline BC
	dualFull, err := buildDualAggNetwork("./data/phase6_dual_full", nil)
	if err != nil {
		log.Fatalf("Failed to build dual-agg network: %v", err)
	}
	defer dualFull.Graph.Close()

	dualStats := dualFull.Graph.GetStatistics()
	fmt.Printf("  Dual-hub network: %d nodes, %d edges\n", dualStats.NodeCount, dualStats.EdgeCount)
	fmt.Println()

	// Scenario A: Brick one hub (Agg_Switch_Hub_A)
	fmt.Println("--- Scenario A: Single Hub Bricked (Agg_Switch_Hub_A) ---")
	fmt.Println()

	dualOneDown, err := buildDualAggNetwork("./data/phase6_dual_one_down", []string{"Agg_Switch_Hub_A"})
	if err != nil {
		log.Fatalf("Failed to build dual-agg (one down): %v", err)
	}
	defer dualOneDown.Graph.Close()

	compsOneDown, err := algorithms.ConnectedComponents(dualOneDown.Graph)
	if err != nil {
		log.Fatalf("Failed to compute components (one hub down): %v", err)
	}

	internetOneDown := dualOneDown.Nodes["Internet_Exchange"]
	reachableOneDown := 0
	for _, sysName := range criticalSystems {
		sys := dualOneDown.Nodes[sysName]
		if sys == nil || internetOneDown == nil {
			continue
		}
		path, err := algorithms.ShortestPath(dualOneDown.Graph, internetOneDown.ID, sys.ID)
		if err == nil && path != nil {
			reachableOneDown++
		}
	}

	fmt.Printf("  Connected components: %d\n", len(compsOneDown.Communities))
	fmt.Printf("  Critical services reachable: %d of %d\n", reachableOneDown, len(criticalSystems))
	if len(compsOneDown.Communities) == 1 {
		fmt.Println("  Network remains FULLY CONNECTED via Agg_Switch_Hub_B")
	}
	fmt.Println()

	// Scenario B: Brick both hubs
	fmt.Println("--- Scenario B: Both Hubs Bricked (Agg_Switch_Hub_A + Hub_B) ---")
	fmt.Println()

	dualBothDown, err := buildDualAggNetwork("./data/phase6_dual_both_down",
		[]string{"Agg_Switch_Hub_A", "Agg_Switch_Hub_B"})
	if err != nil {
		log.Fatalf("Failed to build dual-agg (both down): %v", err)
	}
	defer dualBothDown.Graph.Close()

	compsBothDown, err := algorithms.ConnectedComponents(dualBothDown.Graph)
	if err != nil {
		log.Fatalf("Failed to compute components (both hubs down): %v", err)
	}

	internetBothDown := dualBothDown.Nodes["Internet_Exchange"]
	reachableBothDown := 0
	for _, sysName := range criticalSystems {
		sys := dualBothDown.Nodes[sysName]
		if sys == nil || internetBothDown == nil {
			continue
		}
		path, err := algorithms.ShortestPath(dualBothDown.Graph, internetBothDown.ID, sys.ID)
		if err == nil && path != nil {
			reachableBothDown++
		}
	}

	fmt.Printf("  Connected components: %d\n", len(compsBothDown.Communities))
	fmt.Printf("  Critical services reachable: %d of %d\n", reachableBothDown, len(criticalSystems))
	fmt.Println()

	// BC comparison
	fmt.Println("--- Betweenness Centrality Comparison: Single vs Dual Hub ---")
	fmt.Println()

	// Single-hub BC (use original model)
	singleFull, err := buildISPNetwork("./data/phase6_single_bc", nil)
	if err != nil {
		log.Fatalf("Failed to build single-hub network for BC: %v", err)
	}
	defer singleFull.Graph.Close()

	singleBC, err := algorithms.BetweennessCentrality(singleFull.Graph)
	if err != nil {
		log.Fatalf("Failed to compute single-hub BC: %v", err)
	}

	dualBC, err := algorithms.BetweennessCentrality(dualFull.Graph)
	if err != nil {
		log.Fatalf("Failed to compute dual-hub BC: %v", err)
	}

	// Get BC for hub(s) in each topology
	singleHubBC := singleBC[singleFull.Nodes["Agg_Switch_Hub"].ID]

	dualHubABC := dualBC[dualFull.Nodes["Agg_Switch_Hub_A"].ID]
	dualHubBBC := dualBC[dualFull.Nodes["Agg_Switch_Hub_B"].ID]

	fmt.Printf("  %-30s %s\n", "Topology", "Hub BC Score")
	fmt.Println("  " + strings.Repeat("─", 50))
	fmt.Printf("  %-30s %.6f\n", "Single hub (Agg_Switch_Hub)", singleHubBC)
	fmt.Printf("  %-30s %.6f\n", "Dual hub A (Agg_Switch_Hub_A)", dualHubABC)
	fmt.Printf("  %-30s %.6f\n", "Dual hub B (Agg_Switch_Hub_B)", dualHubBBC)
	fmt.Println()

	if singleHubBC > dualHubABC {
		reduction := (1 - dualHubABC/singleHubBC) * 100
		fmt.Printf("  Dual aggregation reduces hub BC by %.1f%%.\n", reduction)
		fmt.Println("  The criticality of each hub is SHARED, so bricking one is survivable.")
	}
	fmt.Println()

	fmt.Println("  CONCLUSION: Dual aggregation forces the attacker to brick BOTH hubs")
	fmt.Println("  to achieve the same effect as bricking one in the single-hub design.")
	fmt.Println("  This doubles the attacker's work and detection window.")
	fmt.Println()
}

// ========================================================================
// Final Summary
// ========================================================================

func printFinalSummary() {
	fmt.Println("=========================================================================")
	fmt.Println(" FINAL SUMMARY: Lessons from Volt Typhoon")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println("  1. CENTRALISED AUTHENTICATION IS A SINGLE POINT OF COMPROMISE")
	fmt.Println("     TACACS+ grants CLI access to every device. Compromising it once")
	fmt.Println("     means compromising the entire infrastructure simultaneously.")
	fmt.Println()
	fmt.Println("  2. PHYSICAL DESTRUCTION IS QUALITATIVELY DIFFERENT")
	fmt.Println("     `request system zeroize media` overwrites storage at the hardware")
	fmt.Println("     level. Unlike ransomware, there is no decryption key. Recovery")
	fmt.Println("     requires physical hardware replacement with weeks of lead time.")
	fmt.Println()
	fmt.Println("  3. THE RECOVERY PARADOX")
	fmt.Println("     The management tools needed to diagnose, respond to, and recover")
	fmt.Println("     from the attack (TACACS+, Oxidized, SNMP, Syslog, Jump Host) are")
	fmt.Println("     themselves unreachable because the network connecting to them is")
	fmt.Println("     destroyed. You can't SSH to fix a switch that no longer exists.")
	fmt.Println()
	fmt.Println("  4. BETWEENNESS CENTRALITY REVEALS THE TARGET")
	fmt.Println("     Agg_Switch_Hub dominates BC because it bridges all PoPs to the")
	fmt.Println("     core. An attacker with graph analysis would target it first,")
	fmt.Println("     maximising fragmentation with a single command.")
	fmt.Println()
	fmt.Println("  5. DEFENSE: REDUNDANCY AND SEGMENTATION")
	fmt.Println("     a. Dual hub aggregation survives single-device destruction")
	fmt.Println("     b. Out-of-band management network (separate from data plane)")
	fmt.Println("     c. Local authentication fallback (not dependent on TACACS+)")
	fmt.Println("     d. Offline/air-gapped config backups (not on the same network)")
	fmt.Println("     e. Hardware diversity (not all Juniper — attacker needs different")
	fmt.Println("        exploits for different vendors)")
	fmt.Println()
	fmt.Println("  6. THE REAL LESSON")
	fmt.Println("     Network infrastructure is treated as invisible plumbing — until")
	fmt.Println("     it's destroyed. A nation-state APT pre-positioned on ISP backbone")
	fmt.Println("     devices can, with five commands, disconnect hospitals from EHRs,")
	fmt.Println("     disable 911 dispatch, blind water treatment SCADA, and isolate")
	fmt.Println("     power grid management — all without touching those systems directly.")
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Analysis Complete — Model 9: Juniper Zeroize (Volt Typhoon)")
	fmt.Println("=========================================================================")
}

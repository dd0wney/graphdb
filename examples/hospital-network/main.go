// Package main models a hospital network attack scenario inspired by
// WannaCry (2017) hitting the UK National Health Service.
//
// It demonstrates how a flat network topology allows a ransomware worm
// to traverse from an unpatched admin workstation to life-critical medical
// devices in shockingly few hops, and how proper network segmentation
// dramatically reduces the blast radius.
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

// HospitalModel holds the graph and metadata for a hospital network model.
type HospitalModel struct {
	Graph    *storage.GraphStorage
	Nodes    map[string]*NodeInfo
	NodeByID map[uint64]string
}

// NodeInfo tracks metadata about each node in the hospital model.
type NodeInfo struct {
	ID           uint64
	Name         string
	Zone         string
	LifeCritical bool
	Labels       []string
}

// ZoneStats tracks infection statistics for a single network zone.
type ZoneStats struct {
	Total    int
	Infected int
}

func main() {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Hospital Network: Life-Safety Impact Analysis")
	fmt.Println(" Model 8: Protecting Critical Infrastructure -- Darragh Downey")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Inspired by: WannaCry Ransomware vs NHS (May 2017)")
	fmt.Println(" Impact: 80 NHS trusts affected, 19,000 appointments cancelled,")
	fmt.Println("         600 GP surgeries locked out, 5 A&E departments diverted")
	fmt.Println(" Root Cause: Flat network + unpatched Windows 7 + SMB lateral movement")
	fmt.Println()

	// Clean slate for reproducible results
	if err := os.RemoveAll("./data"); err != nil {
		log.Printf("Warning: failed to clean data directory: %v", err)
	}

	// Build the flat (pre-segmentation) network
	fmt.Println("Building FLAT network (pre-segmentation)...")
	flat, err := buildFlatNetwork("./data/flat")
	if err != nil {
		log.Fatalf("Failed to build flat network: %v", err)
	}
	defer flat.Graph.Close()

	stats := flat.Graph.GetStatistics()
	fmt.Printf("  Nodes: %d  Edges: %d\n\n", stats.NodeCount, stats.EdgeCount)

	// 1. Attack path analysis
	analyseAttackPaths(flat)

	// 2. Life-safety scoring
	analyseLifeSafety(flat)

	// 3. WannaCry blast radius on flat network
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" 3. WannaCry Blast Radius -- FLAT Network")
	fmt.Println("=========================================================================")
	fmt.Println()
	flatZones := analyseBlastRadius(flat, "FLAT")

	// Build the segmented (post-remediation) network
	fmt.Println()
	fmt.Println("Building SEGMENTED network (post-remediation)...")
	segmented, err := buildSegmentedNetwork("./data/segmented")
	if err != nil {
		log.Fatalf("Failed to build segmented network: %v", err)
	}
	defer segmented.Graph.Close()

	segStats := segmented.Graph.GetStatistics()
	fmt.Printf("  Nodes: %d  Edges: %d\n", segStats.NodeCount, segStats.EdgeCount)

	// 4. Segmentation impact comparison
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" 4. WannaCry Blast Radius -- SEGMENTED Network")
	fmt.Println("=========================================================================")
	fmt.Println()
	segZones := analyseBlastRadius(segmented, "SEGMENTED")

	analyseSegmentationImpact(flat, segmented, flatZones, segZones)

	// 5. Betweenness centrality comparison
	analyseBetweenness(flat, "FLAT")
	analyseBetweenness(segmented, "SEGMENTED")

	// Final summary
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" CONCLUSION")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" Network segmentation is not optional for healthcare.")
	fmt.Println()
	fmt.Println(" In the flat network, a single unpatched Windows 7 admin PC gives")
	fmt.Println(" an internet-born worm a direct path to ventilators, infusion pumps,")
	fmt.Println(" and patient monitors. In the segmented network, the blast radius is")
	fmt.Println(" contained to the admin zone -- medical devices remain operational.")
	fmt.Println()
	fmt.Println(" The 19,000 cancelled appointments and diverted ambulances of May 2017")
	fmt.Println(" were not inevitable. They were the cost of a flat network.")
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" Analysis Complete")
	fmt.Println("=========================================================================")
}

// buildFlatNetwork constructs the pre-segmentation hospital network where
// almost everything sits on the same broadcast domain. This is the topology
// that allowed WannaCry to reach life-critical devices.
func buildFlatNetwork(dataPath string) (*HospitalModel, error) {
	gs, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	model := &HospitalModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	// Helper to create a node and register it in metadata
	addNode := func(name string, labels []string, zone string, lifeCritical bool) error {
		node, createErr := gs.CreateNode(labels, map[string]storage.Value{
			"name":          storage.StringValue(name),
			"zone":          storage.StringValue(zone),
			"life_critical": storage.BoolValue(lifeCritical),
		})
		if createErr != nil {
			return fmt.Errorf("failed to create node %s: %w", name, createErr)
		}
		info := &NodeInfo{
			ID:           node.ID,
			Name:         name,
			Zone:         zone,
			LifeCritical: lifeCritical,
			Labels:       labels,
		}
		model.Nodes[name] = info
		model.NodeByID[node.ID] = name
		return nil
	}

	// ---------- Internet / Entry (zone: external) ----------
	if err := addNode("Internet", []string{"Gateway"}, "external", false); err != nil {
		return nil, err
	}
	if err := addNode("NHS_N3_Network", []string{"WAN"}, "external", false); err != nil {
		return nil, err
	}

	// ---------- Perimeter (zone: perimeter) ----------
	if err := addNode("Main_Firewall", []string{"Firewall"}, "perimeter", false); err != nil {
		return nil, err
	}
	if err := addNode("Web_Proxy", []string{"Server"}, "perimeter", false); err != nil {
		return nil, err
	}
	if err := addNode("Email_Gateway", []string{"Server"}, "perimeter", false); err != nil {
		return nil, err
	}

	// ---------- Core Infrastructure (zone: core) ----------
	if err := addNode("Core_Switch_A", []string{"NetworkSwitch"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("Core_Switch_B", []string{"NetworkSwitch"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("AD_Server", []string{"Server"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("DNS_Server", []string{"Server"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("DHCP_Server", []string{"Server"}, "core", false); err != nil {
		return nil, err
	}

	// ---------- Clinical IT Systems (zone: clinical_it) ----------
	if err := addNode("EHR_Server", []string{"Server", "CriticalSystem"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("PACS_Server", []string{"Server"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("Lab_System", []string{"Server"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("Pharmacy_System", []string{"Server"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("Blood_Bank_System", []string{"Server", "SafetyCritical"}, "clinical_it", false); err != nil {
		return nil, err
	}

	// ---------- Admin / Business (zone: admin) ----------
	if err := addNode("Admin_PC_1", []string{"Workstation", "Windows7"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("Admin_PC_2", []string{"Workstation", "Windows7"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("Finance_PC", []string{"Workstation"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("HR_PC", []string{"Workstation"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("Reception_PC", []string{"Workstation"}, "admin", false); err != nil {
		return nil, err
	}

	// ---------- Medical Imaging (zone: imaging) ----------
	if err := addNode("MRI_Scanner", []string{"MedicalDevice", "Windows7"}, "imaging", false); err != nil {
		return nil, err
	}
	if err := addNode("CT_Scanner", []string{"MedicalDevice", "Windows7"}, "imaging", false); err != nil {
		return nil, err
	}
	if err := addNode("Xray_Digital", []string{"MedicalDevice"}, "imaging", false); err != nil {
		return nil, err
	}
	if err := addNode("Ultrasound_1", []string{"MedicalDevice"}, "imaging", false); err != nil {
		return nil, err
	}

	// ---------- Patient Monitoring (zone: patient_monitoring) ----------
	if err := addNode("Monitor_ICU_1", []string{"MedicalDevice", "LifeCritical"}, "patient_monitoring", true); err != nil {
		return nil, err
	}
	if err := addNode("Monitor_ICU_2", []string{"MedicalDevice", "LifeCritical"}, "patient_monitoring", true); err != nil {
		return nil, err
	}
	if err := addNode("Monitor_Ward_1", []string{"MedicalDevice"}, "patient_monitoring", false); err != nil {
		return nil, err
	}
	if err := addNode("Monitor_Ward_2", []string{"MedicalDevice"}, "patient_monitoring", false); err != nil {
		return nil, err
	}
	if err := addNode("Telemetry_Server", []string{"Server"}, "patient_monitoring", false); err != nil {
		return nil, err
	}

	// ---------- Life-Critical Devices (zone: life_critical) ----------
	if err := addNode("Infusion_Pump_1", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Infusion_Pump_2", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Ventilator_1", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Ventilator_2", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Anaesthesia_Machine", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}

	// ---------- Building Management (zone: building) ----------
	if err := addNode("BMS_Controller", []string{"BuildingSystem"}, "building", false); err != nil {
		return nil, err
	}
	if err := addNode("HVAC_System", []string{"BuildingSystem"}, "building", false); err != nil {
		return nil, err
	}
	if err := addNode("Access_Control", []string{"BuildingSystem"}, "building", false); err != nil {
		return nil, err
	}
	if err := addNode("Nurse_Call_System", []string{"BuildingSystem", "LifeCritical"}, "building", true); err != nil {
		return nil, err
	}
	if err := addNode("Generator_Controller", []string{"BuildingSystem", "SafetyCritical"}, "building", false); err != nil {
		return nil, err
	}

	// ---------- Wireless (zone: wireless) ----------
	if err := addNode("WiFi_Clinical", []string{"WirelessAP"}, "wireless", false); err != nil {
		return nil, err
	}
	if err := addNode("WiFi_Guest", []string{"WirelessAP"}, "wireless", false); err != nil {
		return nil, err
	}

	// ================================================================
	// EDGES -- FLAT NETWORK
	// ================================================================

	// Helper for directed edge
	directed := func(from, to, edgeType string) error {
		fromID := model.Nodes[from].ID
		toID := model.Nodes[to].ID
		_, createErr := gs.CreateEdge(fromID, toID, edgeType, map[string]storage.Value{}, 1.0)
		if createErr != nil {
			return fmt.Errorf("failed to create edge %s -> %s: %w", from, to, createErr)
		}
		return nil
	}

	// Helper for undirected edge (two directed edges)
	undirected := func(a, b, edgeType string) error {
		aID := model.Nodes[a].ID
		bID := model.Nodes[b].ID
		if _, createErr := gs.CreateEdge(aID, bID, edgeType, map[string]storage.Value{}, 1.0); createErr != nil {
			return fmt.Errorf("failed to create edge %s -> %s: %w", a, b, createErr)
		}
		if _, createErr := gs.CreateEdge(bID, aID, edgeType, map[string]storage.Value{}, 1.0); createErr != nil {
			return fmt.Errorf("failed to create edge %s -> %s: %w", b, a, createErr)
		}
		return nil
	}

	// --- Perimeter edges (directed, type: NETWORK) ---
	perimeterEdges := [][2]string{
		{"Internet", "Main_Firewall"},
		{"Main_Firewall", "Core_Switch_A"},
		{"NHS_N3_Network", "Main_Firewall"},
		{"Main_Firewall", "Web_Proxy"},
		{"Main_Firewall", "Email_Gateway"},
		{"Email_Gateway", "Core_Switch_A"},
	}
	for _, e := range perimeterEdges {
		if err := directed(e[0], e[1], "NETWORK"); err != nil {
			return nil, err
		}
	}

	// --- Core distribution (undirected, type: LAN) -- THE FLAT NETWORK PROBLEM ---
	// Core_Switch_A <-> Core_Switch_B trunk
	if err := undirected("Core_Switch_A", "Core_Switch_B", "LAN"); err != nil {
		return nil, err
	}

	// Everything hanging off Core_Switch_A
	switchADevices := []string{
		"AD_Server", "DNS_Server", "DHCP_Server",
		"EHR_Server", "PACS_Server", "Lab_System", "Pharmacy_System", "Blood_Bank_System",
		"Admin_PC_1", "Admin_PC_2", "Finance_PC", "HR_PC", "Reception_PC",
		"MRI_Scanner", "CT_Scanner", "Xray_Digital", "Ultrasound_1",
		"WiFi_Clinical", "WiFi_Guest",
	}
	for _, dev := range switchADevices {
		if err := undirected("Core_Switch_A", dev, "LAN"); err != nil {
			return nil, err
		}
	}

	// Everything hanging off Core_Switch_B
	switchBDevices := []string{
		"Monitor_ICU_1", "Monitor_ICU_2", "Monitor_Ward_1", "Monitor_Ward_2",
		"Telemetry_Server",
		"Infusion_Pump_1", "Infusion_Pump_2",
		"Ventilator_1", "Ventilator_2", "Anaesthesia_Machine",
		"BMS_Controller", "HVAC_System", "Access_Control", "Nurse_Call_System", "Generator_Controller",
	}
	for _, dev := range switchBDevices {
		if err := undirected("Core_Switch_B", dev, "LAN"); err != nil {
			return nil, err
		}
	}

	// --- Lateral movement (undirected, type: SMB_LATERAL) -- WannaCry spread ---
	smbEdges := [][2]string{
		{"Admin_PC_1", "Admin_PC_2"},
		{"Admin_PC_1", "Finance_PC"},
		{"Admin_PC_1", "HR_PC"},
		{"Admin_PC_1", "MRI_Scanner"}, // THE DISASTER: admin PC to medical device
		{"Admin_PC_2", "CT_Scanner"},  // Same flat subnet
		{"AD_Server", "EHR_Server"},   // Domain controller to EHR
		{"AD_Server", "PACS_Server"},  // Domain controller to PACS
		{"AD_Server", "Lab_System"},   // Domain controller to Lab
	}
	for _, e := range smbEdges {
		if err := undirected(e[0], e[1], "SMB_LATERAL"); err != nil {
			return nil, err
		}
	}

	return model, nil
}

// buildSegmentedNetwork constructs the post-remediation hospital network with
// proper VLAN segmentation. Medical devices, building systems, and admin PCs
// are on separate switches with no direct lateral movement paths between zones.
func buildSegmentedNetwork(dataPath string) (*HospitalModel, error) {
	gs, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	model := &HospitalModel{
		Graph:    gs,
		Nodes:    make(map[string]*NodeInfo),
		NodeByID: make(map[uint64]string),
	}

	addNode := func(name string, labels []string, zone string, lifeCritical bool) error {
		node, createErr := gs.CreateNode(labels, map[string]storage.Value{
			"name":          storage.StringValue(name),
			"zone":          storage.StringValue(zone),
			"life_critical": storage.BoolValue(lifeCritical),
		})
		if createErr != nil {
			return fmt.Errorf("failed to create node %s: %w", name, createErr)
		}
		info := &NodeInfo{
			ID:           node.ID,
			Name:         name,
			Zone:         zone,
			LifeCritical: lifeCritical,
			Labels:       labels,
		}
		model.Nodes[name] = info
		model.NodeByID[node.ID] = name
		return nil
	}

	// Same nodes as flat network
	// --- External ---
	if err := addNode("Internet", []string{"Gateway"}, "external", false); err != nil {
		return nil, err
	}
	if err := addNode("NHS_N3_Network", []string{"WAN"}, "external", false); err != nil {
		return nil, err
	}

	// --- Perimeter ---
	if err := addNode("Main_Firewall", []string{"Firewall"}, "perimeter", false); err != nil {
		return nil, err
	}
	if err := addNode("Web_Proxy", []string{"Server"}, "perimeter", false); err != nil {
		return nil, err
	}
	if err := addNode("Email_Gateway", []string{"Server"}, "perimeter", false); err != nil {
		return nil, err
	}

	// --- Core ---
	if err := addNode("Core_Switch_A", []string{"NetworkSwitch"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("Core_Switch_B", []string{"NetworkSwitch"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("AD_Server", []string{"Server"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("DNS_Server", []string{"Server"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("DHCP_Server", []string{"Server"}, "core", false); err != nil {
		return nil, err
	}

	// --- Clinical IT ---
	if err := addNode("EHR_Server", []string{"Server", "CriticalSystem"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("PACS_Server", []string{"Server"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("Lab_System", []string{"Server"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("Pharmacy_System", []string{"Server"}, "clinical_it", false); err != nil {
		return nil, err
	}
	if err := addNode("Blood_Bank_System", []string{"Server", "SafetyCritical"}, "clinical_it", false); err != nil {
		return nil, err
	}

	// --- Admin ---
	if err := addNode("Admin_PC_1", []string{"Workstation", "Windows7"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("Admin_PC_2", []string{"Workstation", "Windows7"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("Finance_PC", []string{"Workstation"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("HR_PC", []string{"Workstation"}, "admin", false); err != nil {
		return nil, err
	}
	if err := addNode("Reception_PC", []string{"Workstation"}, "admin", false); err != nil {
		return nil, err
	}

	// --- Imaging ---
	if err := addNode("MRI_Scanner", []string{"MedicalDevice", "Windows7"}, "imaging", false); err != nil {
		return nil, err
	}
	if err := addNode("CT_Scanner", []string{"MedicalDevice", "Windows7"}, "imaging", false); err != nil {
		return nil, err
	}
	if err := addNode("Xray_Digital", []string{"MedicalDevice"}, "imaging", false); err != nil {
		return nil, err
	}
	if err := addNode("Ultrasound_1", []string{"MedicalDevice"}, "imaging", false); err != nil {
		return nil, err
	}

	// --- Patient Monitoring ---
	if err := addNode("Monitor_ICU_1", []string{"MedicalDevice", "LifeCritical"}, "patient_monitoring", true); err != nil {
		return nil, err
	}
	if err := addNode("Monitor_ICU_2", []string{"MedicalDevice", "LifeCritical"}, "patient_monitoring", true); err != nil {
		return nil, err
	}
	if err := addNode("Monitor_Ward_1", []string{"MedicalDevice"}, "patient_monitoring", false); err != nil {
		return nil, err
	}
	if err := addNode("Monitor_Ward_2", []string{"MedicalDevice"}, "patient_monitoring", false); err != nil {
		return nil, err
	}
	if err := addNode("Telemetry_Server", []string{"Server"}, "patient_monitoring", false); err != nil {
		return nil, err
	}

	// --- Life-Critical ---
	if err := addNode("Infusion_Pump_1", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Infusion_Pump_2", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Ventilator_1", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Ventilator_2", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}
	if err := addNode("Anaesthesia_Machine", []string{"MedicalDevice", "LifeCritical"}, "life_critical", true); err != nil {
		return nil, err
	}

	// --- Building ---
	if err := addNode("BMS_Controller", []string{"BuildingSystem"}, "building", false); err != nil {
		return nil, err
	}
	if err := addNode("HVAC_System", []string{"BuildingSystem"}, "building", false); err != nil {
		return nil, err
	}
	if err := addNode("Access_Control", []string{"BuildingSystem"}, "building", false); err != nil {
		return nil, err
	}
	if err := addNode("Nurse_Call_System", []string{"BuildingSystem", "LifeCritical"}, "building", true); err != nil {
		return nil, err
	}
	if err := addNode("Generator_Controller", []string{"BuildingSystem", "SafetyCritical"}, "building", false); err != nil {
		return nil, err
	}

	// --- Wireless ---
	if err := addNode("WiFi_Clinical", []string{"WirelessAP"}, "wireless", false); err != nil {
		return nil, err
	}
	if err := addNode("WiFi_Guest", []string{"WirelessAP"}, "wireless", false); err != nil {
		return nil, err
	}

	// --- NEW: Segmentation switches (VLAN boundaries) ---
	if err := addNode("Medical_Switch", []string{"NetworkSwitch", "VLAN"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("BMS_Switch", []string{"NetworkSwitch", "VLAN"}, "core", false); err != nil {
		return nil, err
	}
	if err := addNode("Admin_Switch", []string{"NetworkSwitch", "VLAN"}, "core", false); err != nil {
		return nil, err
	}

	// ================================================================
	// EDGES -- SEGMENTED NETWORK
	// ================================================================

	directed := func(from, to, edgeType string) error {
		fromID := model.Nodes[from].ID
		toID := model.Nodes[to].ID
		_, createErr := gs.CreateEdge(fromID, toID, edgeType, map[string]storage.Value{}, 1.0)
		if createErr != nil {
			return fmt.Errorf("failed to create edge %s -> %s: %w", from, to, createErr)
		}
		return nil
	}

	undirected := func(a, b, edgeType string) error {
		aID := model.Nodes[a].ID
		bID := model.Nodes[b].ID
		if _, createErr := gs.CreateEdge(aID, bID, edgeType, map[string]storage.Value{}, 1.0); createErr != nil {
			return fmt.Errorf("failed to create edge %s -> %s: %w", a, b, createErr)
		}
		if _, createErr := gs.CreateEdge(bID, aID, edgeType, map[string]storage.Value{}, 1.0); createErr != nil {
			return fmt.Errorf("failed to create edge %s -> %s: %w", b, a, createErr)
		}
		return nil
	}

	// --- Perimeter (same as flat) ---
	perimeterEdges := [][2]string{
		{"Internet", "Main_Firewall"},
		{"Main_Firewall", "Core_Switch_A"},
		{"NHS_N3_Network", "Main_Firewall"},
		{"Main_Firewall", "Web_Proxy"},
		{"Main_Firewall", "Email_Gateway"},
		{"Email_Gateway", "Core_Switch_A"},
	}
	for _, e := range perimeterEdges {
		if err := directed(e[0], e[1], "NETWORK"); err != nil {
			return nil, err
		}
	}

	// --- Core trunk ---
	if err := undirected("Core_Switch_A", "Core_Switch_B", "LAN"); err != nil {
		return nil, err
	}

	// --- Core services still on core switch ---
	coreServices := []string{"AD_Server", "DNS_Server", "DHCP_Server"}
	for _, svc := range coreServices {
		if err := undirected("Core_Switch_A", svc, "LAN"); err != nil {
			return nil, err
		}
	}

	// --- Clinical IT on Core_Switch_A (servers are managed infrastructure) ---
	clinicalServers := []string{"EHR_Server", "PACS_Server", "Lab_System", "Pharmacy_System", "Blood_Bank_System"}
	for _, svc := range clinicalServers {
		if err := undirected("Core_Switch_A", svc, "LAN"); err != nil {
			return nil, err
		}
	}

	// --- WiFi on Core_Switch_A ---
	if err := undirected("Core_Switch_A", "WiFi_Clinical", "LAN"); err != nil {
		return nil, err
	}
	if err := undirected("Core_Switch_A", "WiFi_Guest", "LAN"); err != nil {
		return nil, err
	}

	// --- VLAN switches connect to Core_Switch_A ---
	// Medical and BMS VLANs use directed edges (VLAN → Core only).
	// ACLs block inbound traffic from IT/admin zones to medical devices.
	// Medical devices can initiate connections to servers (EHR queries, etc.)
	// but worms cannot traverse from core into the medical VLAN.
	if err := directed("Medical_Switch", "Core_Switch_A", "VLAN_TRUNK"); err != nil {
		return nil, err
	}
	if err := directed("BMS_Switch", "Core_Switch_A", "VLAN_TRUNK"); err != nil {
		return nil, err
	}
	// Admin VLAN is bidirectional — same trust zone as core IT
	if err := undirected("Core_Switch_A", "Admin_Switch", "VLAN_TRUNK"); err != nil {
		return nil, err
	}

	// --- Admin PCs on Admin_Switch (isolated from medical devices) ---
	adminPCs := []string{"Admin_PC_1", "Admin_PC_2", "Finance_PC", "HR_PC", "Reception_PC"}
	for _, pc := range adminPCs {
		if err := undirected("Admin_Switch", pc, "LAN"); err != nil {
			return nil, err
		}
	}

	// --- ALL medical and life-critical devices on Medical_Switch ---
	medicalDevices := []string{
		"MRI_Scanner", "CT_Scanner", "Xray_Digital", "Ultrasound_1",
		"Monitor_ICU_1", "Monitor_ICU_2", "Monitor_Ward_1", "Monitor_Ward_2",
		"Telemetry_Server",
		"Infusion_Pump_1", "Infusion_Pump_2",
		"Ventilator_1", "Ventilator_2", "Anaesthesia_Machine",
	}
	for _, dev := range medicalDevices {
		if err := undirected("Medical_Switch", dev, "LAN"); err != nil {
			return nil, err
		}
	}

	// --- Building systems on BMS_Switch ---
	buildingSystems := []string{
		"BMS_Controller", "HVAC_System", "Access_Control",
		"Nurse_Call_System", "Generator_Controller",
	}
	for _, sys := range buildingSystems {
		if err := undirected("BMS_Switch", sys, "LAN"); err != nil {
			return nil, err
		}
	}

	// --- SMB lateral movement within admin VLAN only ---
	// No cross-VLAN SMB because ACLs block it
	adminSMB := [][2]string{
		{"Admin_PC_1", "Admin_PC_2"},
		{"Admin_PC_1", "Finance_PC"},
		{"Admin_PC_1", "HR_PC"},
	}
	for _, e := range adminSMB {
		if err := undirected(e[0], e[1], "SMB_LATERAL"); err != nil {
			return nil, err
		}
	}

	// AD server lateral movement stays within core (no cross-VLAN)
	adSMB := [][2]string{
		{"AD_Server", "EHR_Server"},
		{"AD_Server", "PACS_Server"},
		{"AD_Server", "Lab_System"},
	}
	for _, e := range adSMB {
		if err := undirected(e[0], e[1], "SMB_LATERAL"); err != nil {
			return nil, err
		}
	}

	return model, nil
}

// analyseAttackPaths finds shortest paths from the internet to critical targets.
// In a flat network, these paths are terrifyingly short.
func analyseAttackPaths(model *HospitalModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" 1. Attack Path Analysis: Internet to Life-Critical Devices")
	fmt.Println("=========================================================================")
	fmt.Println()

	targets := []struct {
		name  string
		label string
	}{
		{"Ventilator_1", "VENTILATOR"},
		{"MRI_Scanner", "MRI SCANNER"},
		{"Infusion_Pump_1", "INFUSION PUMP"},
		{"Blood_Bank_System", "BLOOD BANK"},
	}

	internetID := model.Nodes["Internet"].ID

	for _, target := range targets {
		targetInfo := model.Nodes[target.name]
		path, err := algorithms.ShortestPath(model.Graph, internetID, targetInfo.ID)
		if err != nil {
			log.Printf("Warning: failed to find path to %s: %v", target.name, err)
			continue
		}

		if path == nil {
			fmt.Printf("  Internet -> %s: NO PATH FOUND\n", target.label)
			continue
		}

		hops := len(path) - 1
		fmt.Printf("  Internet -> %s: %d hops\n", target.label, hops)
		fmt.Printf("    Path: ")
		for i, nodeID := range path {
			name := model.NodeByID[nodeID]
			if i > 0 {
				fmt.Printf(" -> ")
			}
			fmt.Printf("%s", name)
		}
		fmt.Println()

		if targetInfo.LifeCritical {
			fmt.Printf("    ** LIFE-CRITICAL DEVICE reachable in %d hops **\n", hops)
		}
		fmt.Println()
	}
}

// analyseLifeSafety computes a life-safety score for every node. The score
// counts how many life-critical devices are reachable within 2 hops. A high
// score means compromising that node puts more patients at risk.
func analyseLifeSafety(model *HospitalModel) {
	fmt.Println("=========================================================================")
	fmt.Println(" 2. Life-Safety Scoring")
	fmt.Println("    (How many life-critical devices are within 2 hops of each node?)")
	fmt.Println("=========================================================================")
	fmt.Println()

	// Identify all life-critical devices
	var lifeCriticalIDs []uint64
	for _, info := range model.Nodes {
		if info.LifeCritical {
			lifeCriticalIDs = append(lifeCriticalIDs, info.ID)
		}
	}

	// For each node, count how many life-critical devices are within 2 hops
	type scoredNode struct {
		Name  string
		Zone  string
		Score int
	}

	var scores []scoredNode

	for _, info := range model.Nodes {
		score := 0
		for _, lcID := range lifeCriticalIDs {
			// Use AllShortestPaths from the life-critical device and check
			// if the target node is within 2 hops
			distances, err := algorithms.AllShortestPaths(model.Graph, lcID)
			if err != nil {
				continue
			}
			if dist, found := distances[info.ID]; found && dist <= 2 {
				score++
			}
		}
		scores = append(scores, scoredNode{
			Name:  info.Name,
			Zone:  info.Zone,
			Score: score,
		})
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Score != scores[j].Score {
			return scores[i].Score > scores[j].Score
		}
		return scores[i].Name < scores[j].Name
	})

	fmt.Printf("  %-28s %-22s %s\n", "Node", "Zone", "Life-Safety Score")
	fmt.Println("  " + strings.Repeat("-", 70))

	for i, s := range scores {
		marker := ""
		if i < 10 {
			marker = " <-- TOP 10"
		}
		if s.Score > 0 {
			fmt.Printf("  %-28s %-22s %d%s\n", s.Name, s.Zone, s.Score, marker)
		}
	}

	// Count nodes with zero score
	zeroCount := 0
	for _, s := range scores {
		if s.Score == 0 {
			zeroCount++
		}
	}
	if zeroCount > 0 {
		fmt.Printf("\n  (%d nodes with life-safety score of 0 omitted)\n", zeroCount)
	}
	fmt.Println()
}

// analyseBlastRadius calculates the WannaCry blast radius from Admin_PC_1
// (patient zero). It uses AllShortestPaths to find every reachable node
// and groups results by zone.
func analyseBlastRadius(model *HospitalModel, label string) map[string]ZoneStats {
	patientZero := model.Nodes["Admin_PC_1"]
	distances, err := algorithms.AllShortestPaths(model.Graph, patientZero.ID)
	if err != nil {
		log.Fatalf("Failed to compute blast radius for %s: %v", label, err)
	}

	// Build zone totals from the model
	zoneTotals := make(map[string]int)
	for _, info := range model.Nodes {
		zoneTotals[info.Zone]++
	}

	// Count infected nodes per zone
	zoneInfected := make(map[string]int)
	infectedLifeCritical := 0
	infectedMedical := 0
	infectedClinical := 0
	totalInfected := 0

	for nodeID := range distances {
		name, exists := model.NodeByID[nodeID]
		if !exists {
			continue
		}
		info := model.Nodes[name]
		zoneInfected[info.Zone]++
		totalInfected++

		if info.LifeCritical {
			infectedLifeCritical++
		}
		for _, lbl := range info.Labels {
			if lbl == "MedicalDevice" {
				infectedMedical++
				break
			}
		}
		if info.Zone == "clinical_it" {
			infectedClinical++
		}
	}

	// Count totals for special categories
	totalLifeCritical := 0
	totalMedical := 0
	totalClinical := 0
	for _, info := range model.Nodes {
		if info.LifeCritical {
			totalLifeCritical++
		}
		for _, lbl := range info.Labels {
			if lbl == "MedicalDevice" {
				totalMedical++
				break
			}
		}
		if info.Zone == "clinical_it" {
			totalClinical++
		}
	}

	// Print zone breakdown
	zoneOrder := []string{
		"external", "perimeter", "core", "admin", "clinical_it",
		"imaging", "patient_monitoring", "life_critical", "building", "wireless",
	}

	fmt.Printf("  Blast Radius from Admin_PC_1 (%s Network):\n\n", label)
	fmt.Printf("  %-25s %10s %10s\n", "Zone", "Infected", "Total")
	fmt.Println("  " + strings.Repeat("-", 50))

	result := make(map[string]ZoneStats)
	for _, zone := range zoneOrder {
		total := zoneTotals[zone]
		if total == 0 {
			continue
		}
		infected := zoneInfected[zone]
		result[zone] = ZoneStats{Total: total, Infected: infected}
		marker := ""
		if infected > 0 && (zone == "life_critical" || zone == "patient_monitoring" || zone == "imaging") {
			marker = " ***"
		}
		fmt.Printf("  %-25s %7d/%-3d%s\n", zone, infected, total, marker)
	}

	fmt.Println("  " + strings.Repeat("-", 50))
	fmt.Printf("  %-25s %7d/%-3d\n", "TOTAL", totalInfected, len(model.Nodes))
	fmt.Println()
	fmt.Printf("  Medical Devices Infected:      %d/%d\n", infectedMedical, totalMedical)
	fmt.Printf("  Life-Critical Devices Infected: %d/%d\n", infectedLifeCritical, totalLifeCritical)
	fmt.Printf("  Clinical Systems Infected:      %d/%d\n", infectedClinical, totalClinical)
	fmt.Println()

	return result
}

// analyseBetweenness computes betweenness centrality and shows the most
// critical chokepoints in the network.
func analyseBetweenness(model *HospitalModel, label string) {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Printf(" 5. Betweenness Centrality -- %s Network\n", label)
	fmt.Println("=========================================================================")
	fmt.Println()

	bc, err := algorithms.BetweennessCentrality(model.Graph)
	if err != nil {
		log.Fatalf("Failed to compute betweenness centrality for %s: %v", label, err)
	}

	type rankedNode struct {
		Name  string
		Zone  string
		Score float64
	}

	var ranked []rankedNode
	for nodeID, score := range bc {
		name, exists := model.NodeByID[nodeID]
		if !exists {
			continue
		}
		info := model.Nodes[name]
		ranked = append(ranked, rankedNode{
			Name:  name,
			Zone:  info.Zone,
			Score: score,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	fmt.Printf("  %-4s %-28s %-22s %s\n", "Rank", "Node", "Zone", "BC Score")
	fmt.Println("  " + strings.Repeat("-", 70))

	limit := 15
	if len(ranked) < limit {
		limit = len(ranked)
	}
	for i := 0; i < limit; i++ {
		r := ranked[i]
		fmt.Printf("  %-4d %-28s %-22s %.6f\n", i+1, r.Name, r.Zone, r.Score)
	}
	fmt.Println()
}

// analyseSegmentationImpact prints a side-by-side comparison of blast radius
// between the flat and segmented networks. This is the emotional peak of the
// analysis -- showing how segmentation prevents patient harm.
func analyseSegmentationImpact(flat, segmented *HospitalModel, flatZones, segZones map[string]ZoneStats) {
	fmt.Println()
	fmt.Println("=========================================================================")
	fmt.Println(" SEGMENTATION IMPACT ON BLAST RADIUS")
	fmt.Println("=========================================================================")
	fmt.Println()
	fmt.Println(" What changes when you segment the network?")
	fmt.Println(" Admin_PC_1 is STILL patient zero. WannaCry STILL executes.")
	fmt.Println(" But the blast radius is contained.")
	fmt.Println()

	zoneOrder := []string{
		"external", "perimeter", "core", "admin", "clinical_it",
		"imaging", "patient_monitoring", "life_critical", "building", "wireless",
	}

	fmt.Printf("  %-25s %18s %18s %10s\n", "", "FLAT Network", "SEGMENTED Network", "Reduction")
	fmt.Printf("  %-25s %18s %18s %10s\n", "Zone", "Infected", "Infected", "")
	fmt.Println("  " + strings.Repeat("-", 75))

	totalFlatInfected := 0
	totalSegInfected := 0

	// Count life-critical stats using pre-computed blast radii
	flatLC := 0
	segLC := 0
	totalLC := 0

	// Compute distances once rather than per-zone
	flatDistances, _ := algorithms.AllShortestPaths(flat.Graph, flat.Nodes["Admin_PC_1"].ID)
	segDistances, _ := algorithms.AllShortestPaths(segmented.Graph, segmented.Nodes["Admin_PC_1"].ID)

	for _, info := range flat.Nodes {
		if info.LifeCritical {
			totalLC++
			if _, found := flatDistances[info.ID]; found {
				flatLC++
			}
		}
	}
	for _, info := range segmented.Nodes {
		if info.LifeCritical {
			if _, found := segDistances[info.ID]; found {
				segLC++
			}
		}
	}

	for _, zone := range zoneOrder {
		fz := flatZones[zone]
		sz := segZones[zone]

		if fz.Total == 0 && sz.Total == 0 {
			continue
		}

		reduction := ""
		if fz.Infected > 0 {
			pct := float64(fz.Infected-sz.Infected) / float64(fz.Infected) * 100
			reduction = fmt.Sprintf("%.0f%%", pct)
		} else {
			reduction = "-"
		}

		fmt.Printf("  %-25s %10d/%-7d %10d/%-7d %10s\n",
			zone, fz.Infected, fz.Total, sz.Infected, sz.Total, reduction)

		totalFlatInfected += fz.Infected
		totalSegInfected += sz.Infected
	}

	fmt.Println("  " + strings.Repeat("-", 75))

	totalReduction := ""
	if totalFlatInfected > 0 {
		pct := float64(totalFlatInfected-totalSegInfected) / float64(totalFlatInfected) * 100
		totalReduction = fmt.Sprintf("%.0f%%", pct)
	}
	fmt.Printf("  %-25s %10d/%-7d %10d/%-7d %10s\n",
		"TOTAL INFECTED", totalFlatInfected, len(flat.Nodes), totalSegInfected, len(segmented.Nodes), totalReduction)

	lcReduction := ""
	if flatLC > 0 {
		pct := float64(flatLC-segLC) / float64(flatLC) * 100
		lcReduction = fmt.Sprintf("%.0f%%", pct)
	}
	fmt.Printf("  %-25s %10d/%-7d %10d/%-7d %10s\n",
		"Life-Critical Devices", flatLC, totalLC, segLC, totalLC, lcReduction)

	fmt.Println()
	fmt.Println("  =========================================")
	if segLC == 0 && flatLC > 0 {
		fmt.Println("  ZERO life-critical devices compromised")
		fmt.Println("  in the segmented network.")
		fmt.Println()
		fmt.Println("  Ventilators keep running.")
		fmt.Println("  Infusion pumps keep dosing.")
		fmt.Println("  Patient monitors keep monitoring.")
		fmt.Println("  Surgeries are NOT cancelled.")
		fmt.Println("  A&E does NOT divert ambulances.")
	} else {
		fmt.Printf("  Life-critical infections reduced from %d to %d.\n", flatLC, segLC)
	}
	fmt.Println("  =========================================")
	fmt.Println()
}

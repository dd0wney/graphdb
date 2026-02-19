// Package main provides representative OT network models for betweenness centrality analysis.
// These models demonstrate the "invisible node" problem in critical infrastructure security.
package main

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ModelMetadata holds node mappings for a graph model
type ModelMetadata struct {
	Graph      *storage.GraphStorage
	NodeNames  map[uint64]string // ID -> display name
	NodeTypes  map[uint64]string // ID -> "technical", "human", "process"
	NodeLevels map[uint64]string // ID -> level description
	NodeIDs    map[string]uint64 // name -> ID (reverse lookup)
}

// createUndirectedEdge creates edges in both directions to simulate undirected behaviour
func createUndirectedEdge(graph *storage.GraphStorage, fromID, toID uint64, edgeType string, props map[string]storage.Value) error {
	_, err := graph.CreateEdge(fromID, toID, edgeType, props, 1.0)
	if err != nil {
		return fmt.Errorf("failed to create forward edge: %w", err)
	}
	_, err = graph.CreateEdge(toID, fromID, edgeType, props, 1.0)
	if err != nil {
		return fmt.Errorf("failed to create reverse edge: %w", err)
	}
	return nil
}

// BuildStevesUtility creates Model 1: Steve's Utility (33 nodes, 70 undirected edges)
// Demonstrates how one helpful senior OT technician accumulates cross-domain access,
// creating an invisible single point of failure.
func BuildStevesUtility(dataPath string) (*ModelMetadata, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &ModelMetadata{
		Graph:      graph,
		NodeNames:  make(map[uint64]string),
		NodeTypes:  make(map[uint64]string),
		NodeLevels: make(map[uint64]string),
		NodeIDs:    make(map[string]uint64),
	}

	// Helper to create a node and track metadata
	createNode := func(name string, labels []string, level, nodeType string) (*storage.Node, error) {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"level":     storage.StringValue(level),
			"node_type": storage.StringValue(nodeType),
		})
		if err != nil {
			return nil, err
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = level
		meta.NodeIDs[name] = node.ID
		return node, nil
	}

	// ========================================
	// TECHNICAL NODES (22)
	// ========================================

	// Level 0: Process
	createNode("PLC_Turbine1", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("PLC_Turbine2", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("PLC_Substation", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("RTU_Remote1", []string{"Technical", "RTU"}, "L0_Process", "technical")
	createNode("RTU_Remote2", []string{"Technical", "RTU"}, "L0_Process", "technical")

	// Level 1: Control
	createNode("HMI_Control1", []string{"Technical", "HMI"}, "L1_Control", "technical")
	createNode("HMI_Control2", []string{"Technical", "HMI"}, "L1_Control", "technical")
	createNode("Safety_PLC", []string{"Technical", "PLC", "SafetyCritical"}, "L1_Control", "technical")

	// Level 2: Supervisory
	createNode("SCADA_Server", []string{"Technical", "SCADA"}, "L2_Supervisory", "technical")
	createNode("Historian_OT", []string{"Technical", "Database"}, "L2_Supervisory", "technical")
	createNode("Eng_Workstation", []string{"Technical", "Workstation"}, "L2_Supervisory", "technical")

	// Level 3: Site Operations
	createNode("OT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L3_SiteOps", "technical")
	createNode("Patch_Server", []string{"Technical", "Server"}, "L3_SiteOps", "technical")
	createNode("AD_Server_OT", []string{"Technical", "Server"}, "L3_SiteOps", "technical")

	// Level 3.5: DMZ
	createNode("Firewall_ITOT", []string{"Technical", "Firewall"}, "L3.5_DMZ", "technical")
	createNode("Jump_Server", []string{"Technical", "Server"}, "L3.5_DMZ", "technical")
	createNode("Data_Diode", []string{"Technical", "SecurityDevice"}, "L3.5_DMZ", "technical")

	// Level 4: IT
	createNode("IT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L4_IT", "technical")
	createNode("Email_Server", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("ERP_System", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("AD_Server_IT", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("VPN_Gateway", []string{"Technical", "Gateway"}, "L4_IT", "technical")

	// ========================================
	// HUMAN NODES (7)
	// ========================================
	createNode("Steve", []string{"Human", "Operator"}, "Human", "human")
	createNode("OT_Manager", []string{"Human", "Manager"}, "Human", "human")
	createNode("IT_Admin", []string{"Human", "Admin"}, "Human", "human")
	createNode("Control_Op1", []string{"Human", "Operator"}, "Human", "human")
	createNode("Control_Op2", []string{"Human", "Operator"}, "Human", "human")
	createNode("Plant_Manager", []string{"Human", "Manager"}, "Human", "human")
	createNode("Vendor_Rep", []string{"Human", "Vendor"}, "Human", "human")

	// ========================================
	// PROCESS NODES (4)
	// ========================================
	createNode("Change_Mgmt_Process", []string{"Process", "ChangeManagement"}, "Process", "process")
	createNode("Incident_Response", []string{"Process", "IncidentResponse"}, "Process", "process")
	createNode("Vendor_Access_Process", []string{"Process", "VendorManagement"}, "Process", "process")
	createNode("Patch_Approval", []string{"Process", "PatchManagement"}, "Process", "process")

	// ========================================
	// TECHNICAL EDGES (26 undirected)
	// ========================================
	technicalEdges := [][2]string{
		{"PLC_Turbine1", "HMI_Control1"},
		{"PLC_Turbine2", "HMI_Control2"},
		{"PLC_Substation", "HMI_Control1"},
		{"RTU_Remote1", "SCADA_Server"},
		{"RTU_Remote2", "SCADA_Server"},
		{"Safety_PLC", "HMI_Control1"},
		{"Safety_PLC", "HMI_Control2"},
		{"HMI_Control1", "SCADA_Server"},
		{"HMI_Control2", "SCADA_Server"},
		{"SCADA_Server", "Historian_OT"},
		{"SCADA_Server", "Eng_Workstation"},
		{"SCADA_Server", "OT_Switch_Core"},
		{"Historian_OT", "OT_Switch_Core"},
		{"Eng_Workstation", "OT_Switch_Core"},
		{"OT_Switch_Core", "Patch_Server"},
		{"OT_Switch_Core", "AD_Server_OT"},
		{"OT_Switch_Core", "Firewall_ITOT"},
		{"Firewall_ITOT", "Jump_Server"},
		{"Firewall_ITOT", "Data_Diode"},
		{"Data_Diode", "Historian_OT"},
		{"Firewall_ITOT", "IT_Switch_Core"},
		{"Jump_Server", "IT_Switch_Core"},
		{"IT_Switch_Core", "Email_Server"},
		{"IT_Switch_Core", "ERP_System"},
		{"IT_Switch_Core", "AD_Server_IT"},
		{"IT_Switch_Core", "VPN_Gateway"},
	}

	props := map[string]storage.Value{}
	for _, edge := range technicalEdges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		if err := createUndirectedEdge(graph, fromID, toID, "TECHNICAL", props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	// ========================================
	// STEVE'S ACCESS EDGES (23 undirected)
	// ========================================
	steveEdges := [][2]string{
		{"Steve", "PLC_Turbine1"},
		{"Steve", "PLC_Turbine2"},
		{"Steve", "PLC_Substation"},
		{"Steve", "HMI_Control1"},
		{"Steve", "HMI_Control2"},
		{"Steve", "SCADA_Server"},
		{"Steve", "Eng_Workstation"},
		{"Steve", "Historian_OT"},
		{"Steve", "OT_Switch_Core"},
		{"Steve", "Patch_Server"},
		{"Steve", "Jump_Server"},
		{"Steve", "Firewall_ITOT"},
		{"Steve", "VPN_Gateway"},
		{"Steve", "AD_Server_OT"},
		{"Steve", "Change_Mgmt_Process"},
		{"Steve", "Incident_Response"},
		{"Steve", "Vendor_Access_Process"},
		{"Steve", "Patch_Approval"},
		{"Steve", "Vendor_Rep"},
		{"Steve", "OT_Manager"},
		{"Steve", "Control_Op1"},
		{"Steve", "Control_Op2"},
		{"Steve", "IT_Admin"},
	}

	// Tag edges involving process nodes as PROCESS, others as HUMAN_ACCESS
	for _, edge := range steveEdges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		edgeType := "HUMAN_ACCESS"
		if meta.NodeTypes[fromID] == "process" || meta.NodeTypes[toID] == "process" {
			edgeType = "PROCESS"
		}
		if err := createUndirectedEdge(graph, fromID, toID, edgeType, props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	// ========================================
	// OTHER HUMAN EDGES (21 undirected)
	// ========================================
	otherHumanEdges := [][2]string{
		{"Control_Op1", "HMI_Control1"},
		{"Control_Op1", "HMI_Control2"},
		{"Control_Op1", "Incident_Response"},
		{"Control_Op2", "HMI_Control1"},
		{"Control_Op2", "HMI_Control2"},
		{"Control_Op2", "Incident_Response"},
		{"OT_Manager", "SCADA_Server"},
		{"OT_Manager", "Change_Mgmt_Process"},
		{"OT_Manager", "Patch_Approval"},
		{"OT_Manager", "Plant_Manager"},
		{"IT_Admin", "IT_Switch_Core"},
		{"IT_Admin", "Email_Server"},
		{"IT_Admin", "ERP_System"},
		{"IT_Admin", "AD_Server_IT"},
		{"IT_Admin", "VPN_Gateway"},
		{"IT_Admin", "Firewall_ITOT"},
		{"Plant_Manager", "ERP_System"},
		{"Plant_Manager", "Email_Server"},
		{"Vendor_Rep", "VPN_Gateway"},
		{"Vendor_Rep", "Jump_Server"},
		{"Vendor_Rep", "Vendor_Access_Process"},
	}

	for _, edge := range otherHumanEdges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		edgeType := "HUMAN_ACCESS"
		if meta.NodeTypes[fromID] == "process" || meta.NodeTypes[toID] == "process" {
			edgeType = "PROCESS"
		}
		if err := createUndirectedEdge(graph, fromID, toID, edgeType, props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	return meta, nil
}

// BuildStevesUtilityFiltered creates Model 1 with all 33 nodes but only edges
// whose type is in the allowedTypes set. This enables layer-by-layer BC analysis:
//   - ["TECHNICAL"]                          → data plane only (things)
//   - ["TECHNICAL", "HUMAN_ACCESS"]          → things + people
//   - ["TECHNICAL", "PROCESS"]               → things + organisational processes
//   - ["TECHNICAL", "HUMAN_ACCESS", "PROCESS"] → composite (all)
func BuildStevesUtilityFiltered(dataPath string, allowedTypes []string) (*ModelMetadata, error) {
	allowed := make(map[string]bool, len(allowedTypes))
	for _, t := range allowedTypes {
		allowed[t] = true
	}

	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &ModelMetadata{
		Graph:      graph,
		NodeNames:  make(map[uint64]string),
		NodeTypes:  make(map[uint64]string),
		NodeLevels: make(map[uint64]string),
		NodeIDs:    make(map[string]uint64),
	}

	createNode := func(name string, labels []string, level, nodeType string) {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"level":     storage.StringValue(level),
			"node_type": storage.StringValue(nodeType),
		})
		if err != nil {
			// Reuse log.Fatalf pattern from other builders
			panic(fmt.Sprintf("failed to create node %s: %v", name, err))
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = level
		meta.NodeIDs[name] = node.ID
	}

	// All 33 nodes (same N for normalization across all layers)
	createNode("PLC_Turbine1", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("PLC_Turbine2", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("PLC_Substation", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("RTU_Remote1", []string{"Technical", "RTU"}, "L0_Process", "technical")
	createNode("RTU_Remote2", []string{"Technical", "RTU"}, "L0_Process", "technical")
	createNode("HMI_Control1", []string{"Technical", "HMI"}, "L1_Control", "technical")
	createNode("HMI_Control2", []string{"Technical", "HMI"}, "L1_Control", "technical")
	createNode("Safety_PLC", []string{"Technical", "PLC", "SafetyCritical"}, "L1_Control", "technical")
	createNode("SCADA_Server", []string{"Technical", "SCADA"}, "L2_Supervisory", "technical")
	createNode("Historian_OT", []string{"Technical", "Database"}, "L2_Supervisory", "technical")
	createNode("Eng_Workstation", []string{"Technical", "Workstation"}, "L2_Supervisory", "technical")
	createNode("OT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L3_SiteOps", "technical")
	createNode("Patch_Server", []string{"Technical", "Server"}, "L3_SiteOps", "technical")
	createNode("AD_Server_OT", []string{"Technical", "Server"}, "L3_SiteOps", "technical")
	createNode("Firewall_ITOT", []string{"Technical", "Firewall"}, "L3.5_DMZ", "technical")
	createNode("Jump_Server", []string{"Technical", "Server"}, "L3.5_DMZ", "technical")
	createNode("Data_Diode", []string{"Technical", "SecurityDevice"}, "L3.5_DMZ", "technical")
	createNode("IT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L4_IT", "technical")
	createNode("Email_Server", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("ERP_System", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("AD_Server_IT", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("VPN_Gateway", []string{"Technical", "Gateway"}, "L4_IT", "technical")
	createNode("Steve", []string{"Human", "Operator"}, "Human", "human")
	createNode("OT_Manager", []string{"Human", "Manager"}, "Human", "human")
	createNode("IT_Admin", []string{"Human", "Admin"}, "Human", "human")
	createNode("Control_Op1", []string{"Human", "Operator"}, "Human", "human")
	createNode("Control_Op2", []string{"Human", "Operator"}, "Human", "human")
	createNode("Plant_Manager", []string{"Human", "Manager"}, "Human", "human")
	createNode("Vendor_Rep", []string{"Human", "Vendor"}, "Human", "human")
	createNode("Change_Mgmt_Process", []string{"Process", "ChangeManagement"}, "Process", "process")
	createNode("Incident_Response", []string{"Process", "IncidentResponse"}, "Process", "process")
	createNode("Vendor_Access_Process", []string{"Process", "VendorManagement"}, "Process", "process")
	createNode("Patch_Approval", []string{"Process", "PatchManagement"}, "Process", "process")

	// All edges with their types — only include if type is in allowed set
	type taggedEdge struct {
		From, To, Type string
	}

	allEdges := []taggedEdge{
		// TECHNICAL (26)
		{"PLC_Turbine1", "HMI_Control1", "TECHNICAL"},
		{"PLC_Turbine2", "HMI_Control2", "TECHNICAL"},
		{"PLC_Substation", "HMI_Control1", "TECHNICAL"},
		{"RTU_Remote1", "SCADA_Server", "TECHNICAL"},
		{"RTU_Remote2", "SCADA_Server", "TECHNICAL"},
		{"Safety_PLC", "HMI_Control1", "TECHNICAL"},
		{"Safety_PLC", "HMI_Control2", "TECHNICAL"},
		{"HMI_Control1", "SCADA_Server", "TECHNICAL"},
		{"HMI_Control2", "SCADA_Server", "TECHNICAL"},
		{"SCADA_Server", "Historian_OT", "TECHNICAL"},
		{"SCADA_Server", "Eng_Workstation", "TECHNICAL"},
		{"SCADA_Server", "OT_Switch_Core", "TECHNICAL"},
		{"Historian_OT", "OT_Switch_Core", "TECHNICAL"},
		{"Eng_Workstation", "OT_Switch_Core", "TECHNICAL"},
		{"OT_Switch_Core", "Patch_Server", "TECHNICAL"},
		{"OT_Switch_Core", "AD_Server_OT", "TECHNICAL"},
		{"OT_Switch_Core", "Firewall_ITOT", "TECHNICAL"},
		{"Firewall_ITOT", "Jump_Server", "TECHNICAL"},
		{"Firewall_ITOT", "Data_Diode", "TECHNICAL"},
		{"Data_Diode", "Historian_OT", "TECHNICAL"},
		{"Firewall_ITOT", "IT_Switch_Core", "TECHNICAL"},
		{"Jump_Server", "IT_Switch_Core", "TECHNICAL"},
		{"IT_Switch_Core", "Email_Server", "TECHNICAL"},
		{"IT_Switch_Core", "ERP_System", "TECHNICAL"},
		{"IT_Switch_Core", "AD_Server_IT", "TECHNICAL"},
		{"IT_Switch_Core", "VPN_Gateway", "TECHNICAL"},
		// HUMAN_ACCESS — Steve's edges to technical/human nodes (19)
		{"Steve", "PLC_Turbine1", "HUMAN_ACCESS"},
		{"Steve", "PLC_Turbine2", "HUMAN_ACCESS"},
		{"Steve", "PLC_Substation", "HUMAN_ACCESS"},
		{"Steve", "HMI_Control1", "HUMAN_ACCESS"},
		{"Steve", "HMI_Control2", "HUMAN_ACCESS"},
		{"Steve", "SCADA_Server", "HUMAN_ACCESS"},
		{"Steve", "Eng_Workstation", "HUMAN_ACCESS"},
		{"Steve", "Historian_OT", "HUMAN_ACCESS"},
		{"Steve", "OT_Switch_Core", "HUMAN_ACCESS"},
		{"Steve", "Patch_Server", "HUMAN_ACCESS"},
		{"Steve", "Jump_Server", "HUMAN_ACCESS"},
		{"Steve", "Firewall_ITOT", "HUMAN_ACCESS"},
		{"Steve", "VPN_Gateway", "HUMAN_ACCESS"},
		{"Steve", "AD_Server_OT", "HUMAN_ACCESS"},
		{"Steve", "Vendor_Rep", "HUMAN_ACCESS"},
		{"Steve", "OT_Manager", "HUMAN_ACCESS"},
		{"Steve", "Control_Op1", "HUMAN_ACCESS"},
		{"Steve", "Control_Op2", "HUMAN_ACCESS"},
		{"Steve", "IT_Admin", "HUMAN_ACCESS"},
		// HUMAN_ACCESS — other human edges to technical/human nodes (16)
		{"Control_Op1", "HMI_Control1", "HUMAN_ACCESS"},
		{"Control_Op1", "HMI_Control2", "HUMAN_ACCESS"},
		{"Control_Op2", "HMI_Control1", "HUMAN_ACCESS"},
		{"Control_Op2", "HMI_Control2", "HUMAN_ACCESS"},
		{"OT_Manager", "SCADA_Server", "HUMAN_ACCESS"},
		{"OT_Manager", "Plant_Manager", "HUMAN_ACCESS"},
		{"IT_Admin", "IT_Switch_Core", "HUMAN_ACCESS"},
		{"IT_Admin", "Email_Server", "HUMAN_ACCESS"},
		{"IT_Admin", "ERP_System", "HUMAN_ACCESS"},
		{"IT_Admin", "AD_Server_IT", "HUMAN_ACCESS"},
		{"IT_Admin", "VPN_Gateway", "HUMAN_ACCESS"},
		{"IT_Admin", "Firewall_ITOT", "HUMAN_ACCESS"},
		{"Plant_Manager", "ERP_System", "HUMAN_ACCESS"},
		{"Plant_Manager", "Email_Server", "HUMAN_ACCESS"},
		{"Vendor_Rep", "VPN_Gateway", "HUMAN_ACCESS"},
		{"Vendor_Rep", "Jump_Server", "HUMAN_ACCESS"},
		// PROCESS — edges involving process nodes (9)
		{"Steve", "Change_Mgmt_Process", "PROCESS"},
		{"Steve", "Incident_Response", "PROCESS"},
		{"Steve", "Vendor_Access_Process", "PROCESS"},
		{"Steve", "Patch_Approval", "PROCESS"},
		{"Control_Op1", "Incident_Response", "PROCESS"},
		{"Control_Op2", "Incident_Response", "PROCESS"},
		{"OT_Manager", "Change_Mgmt_Process", "PROCESS"},
		{"OT_Manager", "Patch_Approval", "PROCESS"},
		{"Vendor_Rep", "Vendor_Access_Process", "PROCESS"},
	}

	props := map[string]storage.Value{}
	for _, e := range allEdges {
		if !allowed[e.Type] {
			continue
		}
		fromID := meta.NodeIDs[e.From]
		toID := meta.NodeIDs[e.To]
		if err := createUndirectedEdge(graph, fromID, toID, e.Type, props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", e.From, e.To, err)
		}
	}

	return meta, nil
}

// BuildStevesUtilityWithoutSteve creates Model 1 without Steve for removal analysis
func BuildStevesUtilityWithoutSteve(dataPath string) (*ModelMetadata, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &ModelMetadata{
		Graph:      graph,
		NodeNames:  make(map[uint64]string),
		NodeTypes:  make(map[uint64]string),
		NodeLevels: make(map[uint64]string),
		NodeIDs:    make(map[string]uint64),
	}

	createNode := func(name string, labels []string, level, nodeType string) (*storage.Node, error) {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"level":     storage.StringValue(level),
			"node_type": storage.StringValue(nodeType),
		})
		if err != nil {
			return nil, err
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = level
		meta.NodeIDs[name] = node.ID
		return node, nil
	}

	// Technical Nodes (same as full model)
	createNode("PLC_Turbine1", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("PLC_Turbine2", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("PLC_Substation", []string{"Technical", "PLC"}, "L0_Process", "technical")
	createNode("RTU_Remote1", []string{"Technical", "RTU"}, "L0_Process", "technical")
	createNode("RTU_Remote2", []string{"Technical", "RTU"}, "L0_Process", "technical")
	createNode("HMI_Control1", []string{"Technical", "HMI"}, "L1_Control", "technical")
	createNode("HMI_Control2", []string{"Technical", "HMI"}, "L1_Control", "technical")
	createNode("Safety_PLC", []string{"Technical", "PLC", "SafetyCritical"}, "L1_Control", "technical")
	createNode("SCADA_Server", []string{"Technical", "SCADA"}, "L2_Supervisory", "technical")
	createNode("Historian_OT", []string{"Technical", "Database"}, "L2_Supervisory", "technical")
	createNode("Eng_Workstation", []string{"Technical", "Workstation"}, "L2_Supervisory", "technical")
	createNode("OT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L3_SiteOps", "technical")
	createNode("Patch_Server", []string{"Technical", "Server"}, "L3_SiteOps", "technical")
	createNode("AD_Server_OT", []string{"Technical", "Server"}, "L3_SiteOps", "technical")
	createNode("Firewall_ITOT", []string{"Technical", "Firewall"}, "L3.5_DMZ", "technical")
	createNode("Jump_Server", []string{"Technical", "Server"}, "L3.5_DMZ", "technical")
	createNode("Data_Diode", []string{"Technical", "SecurityDevice"}, "L3.5_DMZ", "technical")
	createNode("IT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L4_IT", "technical")
	createNode("Email_Server", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("ERP_System", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("AD_Server_IT", []string{"Technical", "Server"}, "L4_IT", "technical")
	createNode("VPN_Gateway", []string{"Technical", "Gateway"}, "L4_IT", "technical")

	// Human Nodes (WITHOUT Steve)
	createNode("OT_Manager", []string{"Human", "Manager"}, "Human", "human")
	createNode("IT_Admin", []string{"Human", "Admin"}, "Human", "human")
	createNode("Control_Op1", []string{"Human", "Operator"}, "Human", "human")
	createNode("Control_Op2", []string{"Human", "Operator"}, "Human", "human")
	createNode("Plant_Manager", []string{"Human", "Manager"}, "Human", "human")
	createNode("Vendor_Rep", []string{"Human", "Vendor"}, "Human", "human")

	// Process Nodes
	createNode("Change_Mgmt_Process", []string{"Process", "ChangeManagement"}, "Process", "process")
	createNode("Incident_Response", []string{"Process", "IncidentResponse"}, "Process", "process")
	createNode("Vendor_Access_Process", []string{"Process", "VendorManagement"}, "Process", "process")
	createNode("Patch_Approval", []string{"Process", "PatchManagement"}, "Process", "process")

	// Technical Edges (same as full model)
	technicalEdges := [][2]string{
		{"PLC_Turbine1", "HMI_Control1"},
		{"PLC_Turbine2", "HMI_Control2"},
		{"PLC_Substation", "HMI_Control1"},
		{"RTU_Remote1", "SCADA_Server"},
		{"RTU_Remote2", "SCADA_Server"},
		{"Safety_PLC", "HMI_Control1"},
		{"Safety_PLC", "HMI_Control2"},
		{"HMI_Control1", "SCADA_Server"},
		{"HMI_Control2", "SCADA_Server"},
		{"SCADA_Server", "Historian_OT"},
		{"SCADA_Server", "Eng_Workstation"},
		{"SCADA_Server", "OT_Switch_Core"},
		{"Historian_OT", "OT_Switch_Core"},
		{"Eng_Workstation", "OT_Switch_Core"},
		{"OT_Switch_Core", "Patch_Server"},
		{"OT_Switch_Core", "AD_Server_OT"},
		{"OT_Switch_Core", "Firewall_ITOT"},
		{"Firewall_ITOT", "Jump_Server"},
		{"Firewall_ITOT", "Data_Diode"},
		{"Data_Diode", "Historian_OT"},
		{"Firewall_ITOT", "IT_Switch_Core"},
		{"Jump_Server", "IT_Switch_Core"},
		{"IT_Switch_Core", "Email_Server"},
		{"IT_Switch_Core", "ERP_System"},
		{"IT_Switch_Core", "AD_Server_IT"},
		{"IT_Switch_Core", "VPN_Gateway"},
	}

	props := map[string]storage.Value{}
	for _, edge := range technicalEdges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		if err := createUndirectedEdge(graph, fromID, toID, "TECHNICAL", props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	// Other Human Edges (WITHOUT any Steve edges)
	otherHumanEdges := [][2]string{
		{"Control_Op1", "HMI_Control1"},
		{"Control_Op1", "HMI_Control2"},
		{"Control_Op1", "Incident_Response"},
		{"Control_Op2", "HMI_Control1"},
		{"Control_Op2", "HMI_Control2"},
		{"Control_Op2", "Incident_Response"},
		{"OT_Manager", "SCADA_Server"},
		{"OT_Manager", "Change_Mgmt_Process"},
		{"OT_Manager", "Patch_Approval"},
		{"OT_Manager", "Plant_Manager"},
		{"IT_Admin", "IT_Switch_Core"},
		{"IT_Admin", "Email_Server"},
		{"IT_Admin", "ERP_System"},
		{"IT_Admin", "AD_Server_IT"},
		{"IT_Admin", "VPN_Gateway"},
		{"IT_Admin", "Firewall_ITOT"},
		{"Plant_Manager", "ERP_System"},
		{"Plant_Manager", "Email_Server"},
		{"Vendor_Rep", "VPN_Gateway"},
		{"Vendor_Rep", "Jump_Server"},
		{"Vendor_Rep", "Vendor_Access_Process"},
	}

	for _, edge := range otherHumanEdges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		edgeType := "HUMAN_ACCESS"
		if meta.NodeTypes[fromID] == "process" || meta.NodeTypes[toID] == "process" {
			edgeType = "PROCESS"
		}
		if err := createUndirectedEdge(graph, fromID, toID, edgeType, props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	return meta, nil
}

// BuildChemicalFacility creates Model 2: Chemical Facility (24 nodes, 37 undirected edges)
// Demonstrates IT/OT bridge concentration through the IT_OT_Coord role.
func BuildChemicalFacility(dataPath string) (*ModelMetadata, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &ModelMetadata{
		Graph:      graph,
		NodeNames:  make(map[uint64]string),
		NodeTypes:  make(map[uint64]string),
		NodeLevels: make(map[uint64]string),
		NodeIDs:    make(map[string]uint64),
	}

	createNode := func(name string, labels []string, level, nodeType string) (*storage.Node, error) {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"level":     storage.StringValue(level),
			"node_type": storage.StringValue(nodeType),
		})
		if err != nil {
			return nil, err
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = level
		meta.NodeIDs[name] = node.ID
		return node, nil
	}

	// ========================================
	// TECHNICAL NODES (19)
	// ========================================

	// Safety Layer
	createNode("SIS_Controller", []string{"Technical", "SIS", "SafetyCritical"}, "Safety", "technical")
	createNode("SIS_Logic_Solver", []string{"Technical", "SIS"}, "Safety", "technical")
	createNode("ESD_Panel", []string{"Technical", "SIS"}, "Safety", "technical")

	// DCS Layer
	createNode("DCS_Controller1", []string{"Technical", "DCS"}, "DCS", "technical")
	createNode("DCS_Controller2", []string{"Technical", "DCS"}, "DCS", "technical")
	createNode("DCS_Server", []string{"Technical", "DCS", "Server"}, "DCS", "technical")
	createNode("Op_Console1", []string{"Technical", "Console"}, "DCS", "technical")
	createNode("Op_Console2", []string{"Technical", "Console"}, "DCS", "technical")

	// Site Layer
	createNode("OT_Firewall", []string{"Technical", "Firewall"}, "Site", "technical")
	createNode("Historian", []string{"Technical", "Database"}, "Site", "technical")
	createNode("MES_Server", []string{"Technical", "Server"}, "Site", "technical")
	createNode("Eng_Station", []string{"Technical", "Workstation"}, "Site", "technical")

	// DMZ Layer
	createNode("DMZ_Firewall", []string{"Technical", "Firewall"}, "DMZ", "technical")
	createNode("Patch_Relay", []string{"Technical", "Server"}, "DMZ", "technical")
	createNode("Remote_Access", []string{"Technical", "Gateway"}, "DMZ", "technical")

	// Corporate Layer
	createNode("Corp_Firewall", []string{"Technical", "Firewall"}, "Corporate", "technical")
	createNode("Corp_Network", []string{"Technical", "Network"}, "Corporate", "technical")
	createNode("ERP", []string{"Technical", "Server"}, "Corporate", "technical")
	createNode("Internet_GW", []string{"Technical", "Gateway"}, "Corporate", "technical")

	// ========================================
	// HUMAN NODES (5)
	// ========================================
	createNode("DCS_Engineer", []string{"Human", "Engineer"}, "Human", "human")
	createNode("Process_Operator", []string{"Human", "Operator"}, "Human", "human")
	createNode("Safety_Engineer", []string{"Human", "Engineer", "SafetyCertified"}, "Human", "human")
	createNode("IT_OT_Coord", []string{"Human", "Coordinator"}, "Human", "human")
	createNode("Site_IT", []string{"Human", "Admin"}, "Human", "human")

	// ========================================
	// ALL EDGES (37 undirected)
	// ========================================
	edges := [][2]string{
		{"SIS_Controller", "SIS_Logic_Solver"},
		{"SIS_Logic_Solver", "ESD_Panel"},
		{"SIS_Controller", "DCS_Server"},
		{"DCS_Controller1", "DCS_Server"},
		{"DCS_Controller2", "DCS_Server"},
		{"DCS_Server", "Op_Console1"},
		{"DCS_Server", "Op_Console2"},
		{"DCS_Server", "OT_Firewall"},
		{"OT_Firewall", "Historian"},
		{"OT_Firewall", "MES_Server"},
		{"OT_Firewall", "Eng_Station"},
		{"OT_Firewall", "DMZ_Firewall"},
		{"DMZ_Firewall", "Patch_Relay"},
		{"DMZ_Firewall", "Remote_Access"},
		{"DMZ_Firewall", "Corp_Firewall"},
		{"Corp_Firewall", "Corp_Network"},
		{"Corp_Network", "ERP"},
		{"Corp_Network", "Internet_GW"},
		{"DCS_Engineer", "Eng_Station"},
		{"DCS_Engineer", "DCS_Server"},
		{"DCS_Engineer", "DCS_Controller1"},
		{"DCS_Engineer", "DCS_Controller2"},
		{"Process_Operator", "Op_Console1"},
		{"Process_Operator", "Op_Console2"},
		{"Safety_Engineer", "SIS_Controller"},
		{"Safety_Engineer", "SIS_Logic_Solver"},
		{"Safety_Engineer", "DCS_Server"},
		{"IT_OT_Coord", "OT_Firewall"},
		{"IT_OT_Coord", "DMZ_Firewall"},
		{"IT_OT_Coord", "Corp_Firewall"},
		{"IT_OT_Coord", "Remote_Access"},
		{"IT_OT_Coord", "Patch_Relay"},
		{"IT_OT_Coord", "DCS_Engineer"},
		{"IT_OT_Coord", "Site_IT"},
		{"Site_IT", "Corp_Network"},
		{"Site_IT", "Corp_Firewall"},
		{"Site_IT", "DMZ_Firewall"},
	}

	props := map[string]storage.Value{}
	for _, edge := range edges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		edgeType := "TECHNICAL"
		if meta.NodeTypes[fromID] == "human" || meta.NodeTypes[toID] == "human" {
			edgeType = "HUMAN_ACCESS"
		}
		if err := createUndirectedEdge(graph, fromID, toID, edgeType, props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	return meta, nil
}

// BuildWaterTreatmentFlat creates Model 3a: Water Treatment Flat (13 nodes, 13 undirected edges)
// Three switches in full mesh topology.
func BuildWaterTreatmentFlat(dataPath string) (*ModelMetadata, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &ModelMetadata{
		Graph:      graph,
		NodeNames:  make(map[uint64]string),
		NodeTypes:  make(map[uint64]string),
		NodeLevels: make(map[uint64]string),
		NodeIDs:    make(map[string]uint64),
	}

	createNode := func(name string, labels []string, nodeType string) (*storage.Node, error) {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"node_type": storage.StringValue(nodeType),
		})
		if err != nil {
			return nil, err
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = "Flat"
		meta.NodeIDs[name] = node.ID
		return node, nil
	}

	// ========================================
	// ALL NODES (13)
	// ========================================
	createNode("PLC_Chlorine", []string{"Technical", "PLC"}, "technical")
	createNode("PLC_Filtration", []string{"Technical", "PLC"}, "technical")
	createNode("PLC_Pumping", []string{"Technical", "PLC"}, "technical")
	createNode("HMI_1", []string{"Technical", "HMI"}, "technical")
	createNode("HMI_2", []string{"Technical", "HMI"}, "technical")
	createNode("SCADA_Server", []string{"Technical", "SCADA"}, "technical")
	createNode("Historian", []string{"Technical", "Database"}, "technical")
	createNode("Switch_A", []string{"Technical", "NetworkSwitch"}, "technical")
	createNode("Switch_B", []string{"Technical", "NetworkSwitch"}, "technical")
	createNode("Switch_C", []string{"Technical", "NetworkSwitch"}, "technical")
	createNode("Eng_Laptop", []string{"Technical", "Workstation"}, "technical")
	createNode("Operator_PC", []string{"Technical", "Workstation"}, "technical")
	createNode("Router_WAN", []string{"Technical", "Router"}, "technical")

	// ========================================
	// ALL EDGES (13 undirected)
	// ========================================
	edges := [][2]string{
		{"Switch_A", "Switch_B"},
		{"Switch_B", "Switch_C"},
		{"Switch_A", "Switch_C"},
		{"PLC_Chlorine", "Switch_A"},
		{"PLC_Filtration", "Switch_A"},
		{"PLC_Pumping", "Switch_B"},
		{"HMI_1", "Switch_A"},
		{"HMI_2", "Switch_B"},
		{"SCADA_Server", "Switch_B"},
		{"Historian", "Switch_C"},
		{"Eng_Laptop", "Switch_C"},
		{"Operator_PC", "Switch_C"},
		{"Router_WAN", "Switch_C"},
	}

	props := map[string]storage.Value{}
	for _, edge := range edges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		if err := createUndirectedEdge(graph, fromID, toID, "TECHNICAL", props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	return meta, nil
}

// BuildWaterTreatmentVLAN creates Model 3b: Water Treatment VLAN (14 nodes, 13 undirected edges)
// Star topology through L3 core switch. Demonstrates how VLAN segmentation
// concentrates betweenness centrality.
func BuildWaterTreatmentVLAN(dataPath string) (*ModelMetadata, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &ModelMetadata{
		Graph:      graph,
		NodeNames:  make(map[uint64]string),
		NodeTypes:  make(map[uint64]string),
		NodeLevels: make(map[uint64]string),
		NodeIDs:    make(map[string]uint64),
	}

	createNode := func(name string, labels []string, nodeType string) (*storage.Node, error) {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"node_type": storage.StringValue(nodeType),
		})
		if err != nil {
			return nil, err
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = "VLAN"
		meta.NodeIDs[name] = node.ID
		return node, nil
	}

	// ========================================
	// ALL NODES (14)
	// ========================================
	createNode("PLC_Chlorine", []string{"Technical", "PLC"}, "technical")
	createNode("PLC_Filtration", []string{"Technical", "PLC"}, "technical")
	createNode("PLC_Pumping", []string{"Technical", "PLC"}, "technical")
	createNode("HMI_1", []string{"Technical", "HMI"}, "technical")
	createNode("HMI_2", []string{"Technical", "HMI"}, "technical")
	createNode("SCADA_Server", []string{"Technical", "SCADA"}, "technical")
	createNode("Historian", []string{"Technical", "Database"}, "technical")
	createNode("Switch_A", []string{"Technical", "NetworkSwitch"}, "technical")
	createNode("Switch_B", []string{"Technical", "NetworkSwitch"}, "technical")
	createNode("Switch_C", []string{"Technical", "NetworkSwitch"}, "technical")
	createNode("L3_Core_Switch", []string{"Technical", "NetworkSwitch", "CoreRouter"}, "technical")
	createNode("Eng_Laptop", []string{"Technical", "Workstation"}, "technical")
	createNode("Operator_PC", []string{"Technical", "Workstation"}, "technical")
	createNode("Router_WAN", []string{"Technical", "Router"}, "technical")

	// ========================================
	// ALL EDGES (13 undirected)
	// ========================================
	edges := [][2]string{
		{"Switch_A", "L3_Core_Switch"},
		{"Switch_B", "L3_Core_Switch"},
		{"Switch_C", "L3_Core_Switch"},
		{"PLC_Chlorine", "Switch_A"},
		{"PLC_Filtration", "Switch_A"},
		{"PLC_Pumping", "Switch_A"},
		{"HMI_1", "Switch_B"},
		{"HMI_2", "Switch_B"},
		{"SCADA_Server", "Switch_B"},
		{"Historian", "Switch_C"},
		{"Eng_Laptop", "Switch_C"},
		{"Operator_PC", "Switch_C"},
		{"Router_WAN", "L3_Core_Switch"},
	}

	props := map[string]storage.Value{}
	for _, edge := range edges {
		fromID := meta.NodeIDs[edge[0]]
		toID := meta.NodeIDs[edge[1]]
		if err := createUndirectedEdge(graph, fromID, toID, "TECHNICAL", props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	return meta, nil
}

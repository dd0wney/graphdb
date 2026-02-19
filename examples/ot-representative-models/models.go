// Package main provides representative OT network models for betweenness centrality analysis.
// These models demonstrate the "invisible node" problem in critical infrastructure security.
package main

// ============================================================================
// STEVE'S UTILITY NODE AND EDGE DEFINITIONS
// ============================================================================

// stevesUtilityNodes defines all 33 nodes for Steve's Utility model
var stevesUtilityNodes = []NodeDef{
	// Technical Nodes (22)
	// Level 0: Process
	{"PLC_Turbine1", []string{"Technical", "PLC"}, "L0_Process", NodeTypeTechnical, ""},
	{"PLC_Turbine2", []string{"Technical", "PLC"}, "L0_Process", NodeTypeTechnical, ""},
	{"PLC_Substation", []string{"Technical", "PLC"}, "L0_Process", NodeTypeTechnical, ""},
	{"RTU_Remote1", []string{"Technical", "RTU"}, "L0_Process", NodeTypeTechnical, ""},
	{"RTU_Remote2", []string{"Technical", "RTU"}, "L0_Process", NodeTypeTechnical, ""},
	// Level 1: Control
	{"HMI_Control1", []string{"Technical", "HMI"}, "L1_Control", NodeTypeTechnical, ""},
	{"HMI_Control2", []string{"Technical", "HMI"}, "L1_Control", NodeTypeTechnical, ""},
	{"Safety_PLC", []string{"Technical", "PLC", "SafetyCritical"}, "L1_Control", NodeTypeTechnical, ""},
	// Level 2: Supervisory
	{"SCADA_Server", []string{"Technical", "SCADA"}, "L2_Supervisory", NodeTypeTechnical, ""},
	{"Historian_OT", []string{"Technical", "Database"}, "L2_Supervisory", NodeTypeTechnical, ""},
	{"Eng_Workstation", []string{"Technical", "Workstation"}, "L2_Supervisory", NodeTypeTechnical, ""},
	// Level 3: Site Operations
	{"OT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L3_SiteOps", NodeTypeTechnical, ""},
	{"Patch_Server", []string{"Technical", "Server"}, "L3_SiteOps", NodeTypeTechnical, ""},
	{"AD_Server_OT", []string{"Technical", "Server"}, "L3_SiteOps", NodeTypeTechnical, ""},
	// Level 3.5: DMZ
	{"Firewall_ITOT", []string{"Technical", "Firewall"}, "L3.5_DMZ", NodeTypeTechnical, ""},
	{"Jump_Server", []string{"Technical", "Server"}, "L3.5_DMZ", NodeTypeTechnical, ""},
	{"Data_Diode", []string{"Technical", "SecurityDevice"}, "L3.5_DMZ", NodeTypeTechnical, ""},
	// Level 4: IT
	{"IT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "L4_IT", NodeTypeTechnical, ""},
	{"Email_Server", []string{"Technical", "Server"}, "L4_IT", NodeTypeTechnical, ""},
	{"ERP_System", []string{"Technical", "Server"}, "L4_IT", NodeTypeTechnical, ""},
	{"AD_Server_IT", []string{"Technical", "Server"}, "L4_IT", NodeTypeTechnical, ""},
	{"VPN_Gateway", []string{"Technical", "Gateway"}, "L4_IT", NodeTypeTechnical, ""},

	// Human Nodes (7)
	{"Steve", []string{"Human", "Operator"}, "Human", NodeTypeHuman, ""},
	{"OT_Manager", []string{"Human", "Manager"}, "Human", NodeTypeHuman, ""},
	{"IT_Admin", []string{"Human", "Admin"}, "Human", NodeTypeHuman, ""},
	{"Control_Op1", []string{"Human", "Operator"}, "Human", NodeTypeHuman, ""},
	{"Control_Op2", []string{"Human", "Operator"}, "Human", NodeTypeHuman, ""},
	{"Plant_Manager", []string{"Human", "Manager"}, "Human", NodeTypeHuman, ""},
	{"Vendor_Rep", []string{"Human", "Vendor"}, "Human", NodeTypeHuman, ""},

	// Process Nodes (4)
	{"Change_Mgmt_Process", []string{"Process", "ChangeManagement"}, "Process", NodeTypeProcess, ""},
	{"Incident_Response", []string{"Process", "IncidentResponse"}, "Process", NodeTypeProcess, ""},
	{"Vendor_Access_Process", []string{"Process", "VendorManagement"}, "Process", NodeTypeProcess, ""},
	{"Patch_Approval", []string{"Process", "PatchManagement"}, "Process", NodeTypeProcess, ""},
}

// stevesUtilityEdges defines all edges with their types for layer analysis
var stevesUtilityEdges = []EdgeDef{
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

// BuildStevesUtility creates Model 1: Steve's Utility (33 nodes, 70 undirected edges)
// Demonstrates how one helpful senior OT technician accumulates cross-domain access,
// creating an invisible single point of failure.
func BuildStevesUtility(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(stevesUtilityNodes).
		AddEdges(stevesUtilityEdges).
		Build()
}

// BuildStevesUtilityFiltered creates Model 1 with all 33 nodes but only edges
// whose type is in the allowedTypes set. This enables layer-by-layer BC analysis:
//   - ["TECHNICAL"]                          → data plane only (things)
//   - ["TECHNICAL", "HUMAN_ACCESS"]          → things + people
//   - ["TECHNICAL", "PROCESS"]               → things + organisational processes
//   - ["TECHNICAL", "HUMAN_ACCESS", "PROCESS"] → composite (all)
func BuildStevesUtilityFiltered(dataPath string, allowedTypes []string) (*Metadata, error) {
	fb, err := NewFilteredGraphBuilder(dataPath, allowedTypes)
	if err != nil {
		return nil, err
	}

	fb.AddNodes(stevesUtilityNodes)
	fb.AddEdges(stevesUtilityEdges)

	return fb.Build()
}

// BuildStevesUtilityWithoutSteve creates Model 1 without Steve for removal analysis
func BuildStevesUtilityWithoutSteve(dataPath string) (*Metadata, error) {
	// Filter out Steve from nodes
	nodesWithoutSteve := make([]NodeDef, 0, len(stevesUtilityNodes)-1)
	for _, n := range stevesUtilityNodes {
		if n.Name != "Steve" {
			nodesWithoutSteve = append(nodesWithoutSteve, n)
		}
	}

	// Filter out Steve's edges
	edgesWithoutSteve := make([]EdgeDef, 0)
	for _, e := range stevesUtilityEdges {
		if e.From != "Steve" && e.To != "Steve" {
			edgesWithoutSteve = append(edgesWithoutSteve, e)
		}
	}

	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(nodesWithoutSteve).
		AddEdges(edgesWithoutSteve).
		Build()
}

// ============================================================================
// CHEMICAL FACILITY NODE AND EDGE DEFINITIONS
// ============================================================================

var chemicalFacilityNodes = []NodeDef{
	// Technical Nodes (19)
	// Safety Layer
	{"SIS_Controller", []string{"Technical", "SIS", "SafetyCritical"}, "Safety", NodeTypeTechnical, ""},
	{"SIS_Logic_Solver", []string{"Technical", "SIS"}, "Safety", NodeTypeTechnical, ""},
	{"ESD_Panel", []string{"Technical", "SIS"}, "Safety", NodeTypeTechnical, ""},
	// DCS Layer
	{"DCS_Controller1", []string{"Technical", "DCS"}, "DCS", NodeTypeTechnical, ""},
	{"DCS_Controller2", []string{"Technical", "DCS"}, "DCS", NodeTypeTechnical, ""},
	{"DCS_Server", []string{"Technical", "DCS", "Server"}, "DCS", NodeTypeTechnical, ""},
	{"Op_Console1", []string{"Technical", "Console"}, "DCS", NodeTypeTechnical, ""},
	{"Op_Console2", []string{"Technical", "Console"}, "DCS", NodeTypeTechnical, ""},
	// Site Layer
	{"OT_Firewall", []string{"Technical", "Firewall"}, "Site", NodeTypeTechnical, ""},
	{"Historian", []string{"Technical", "Database"}, "Site", NodeTypeTechnical, ""},
	{"MES_Server", []string{"Technical", "Server"}, "Site", NodeTypeTechnical, ""},
	{"Eng_Station", []string{"Technical", "Workstation"}, "Site", NodeTypeTechnical, ""},
	// DMZ Layer
	{"DMZ_Firewall", []string{"Technical", "Firewall"}, "DMZ", NodeTypeTechnical, ""},
	{"Patch_Relay", []string{"Technical", "Server"}, "DMZ", NodeTypeTechnical, ""},
	{"Remote_Access", []string{"Technical", "Gateway"}, "DMZ", NodeTypeTechnical, ""},
	// Corporate Layer
	{"Corp_Firewall", []string{"Technical", "Firewall"}, "Corporate", NodeTypeTechnical, ""},
	{"Corp_Network", []string{"Technical", "Network"}, "Corporate", NodeTypeTechnical, ""},
	{"ERP", []string{"Technical", "Server"}, "Corporate", NodeTypeTechnical, ""},
	{"Internet_GW", []string{"Technical", "Gateway"}, "Corporate", NodeTypeTechnical, ""},

	// Human Nodes (5)
	{"DCS_Engineer", []string{"Human", "Engineer"}, "Human", NodeTypeHuman, ""},
	{"Process_Operator", []string{"Human", "Operator"}, "Human", NodeTypeHuman, ""},
	{"Safety_Engineer", []string{"Human", "Engineer", "SafetyCertified"}, "Human", NodeTypeHuman, ""},
	{"IT_OT_Coord", []string{"Human", "Coordinator"}, "Human", NodeTypeHuman, ""},
	{"Site_IT", []string{"Human", "Admin"}, "Human", NodeTypeHuman, ""},
}

var chemicalFacilityEdges = [][2]string{
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

// BuildChemicalFacility creates Model 2: Chemical Facility (24 nodes, 37 undirected edges)
// Demonstrates IT/OT bridge concentration through the IT_OT_Coord role.
func BuildChemicalFacility(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(chemicalFacilityNodes).
		AddEdgePairsWithAutoType(chemicalFacilityEdges, "TECHNICAL").
		Build()
}

// ============================================================================
// WATER TREATMENT NODE AND EDGE DEFINITIONS
// ============================================================================

var waterTreatmentNodes = []NodeDef{
	{"PLC_Chlorine", []string{"Technical", "PLC"}, "Flat", NodeTypeTechnical, ""},
	{"PLC_Filtration", []string{"Technical", "PLC"}, "Flat", NodeTypeTechnical, ""},
	{"PLC_Pumping", []string{"Technical", "PLC"}, "Flat", NodeTypeTechnical, ""},
	{"HMI_1", []string{"Technical", "HMI"}, "Flat", NodeTypeTechnical, ""},
	{"HMI_2", []string{"Technical", "HMI"}, "Flat", NodeTypeTechnical, ""},
	{"SCADA_Server", []string{"Technical", "SCADA"}, "Flat", NodeTypeTechnical, ""},
	{"Historian", []string{"Technical", "Database"}, "Flat", NodeTypeTechnical, ""},
	{"Switch_A", []string{"Technical", "NetworkSwitch"}, "Flat", NodeTypeTechnical, ""},
	{"Switch_B", []string{"Technical", "NetworkSwitch"}, "Flat", NodeTypeTechnical, ""},
	{"Switch_C", []string{"Technical", "NetworkSwitch"}, "Flat", NodeTypeTechnical, ""},
	{"Eng_Laptop", []string{"Technical", "Workstation"}, "Flat", NodeTypeTechnical, ""},
	{"Operator_PC", []string{"Technical", "Workstation"}, "Flat", NodeTypeTechnical, ""},
	{"Router_WAN", []string{"Technical", "Router"}, "Flat", NodeTypeTechnical, ""},
}

var waterFlatEdges = [][2]string{
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

var waterVLANEdges = [][2]string{
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

// BuildWaterTreatmentFlat creates Model 3a: Water Treatment Flat (13 nodes, 13 undirected edges)
// Three switches in full mesh topology.
func BuildWaterTreatmentFlat(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	return b.AddNodes(waterTreatmentNodes).
		AddEdgePairs(waterFlatEdges, "TECHNICAL").
		Build()
}

// BuildWaterTreatmentVLAN creates Model 3b: Water Treatment VLAN (14 nodes, 13 undirected edges)
// Star topology through L3 core switch. Demonstrates how VLAN segmentation
// concentrates betweenness centrality.
func BuildWaterTreatmentVLAN(dataPath string) (*Metadata, error) {
	b, err := NewGraphBuilder(dataPath)
	if err != nil {
		return nil, err
	}

	// Add the L3 core switch (not in flat model)
	vlanNodes := append([]NodeDef{}, waterTreatmentNodes...)
	vlanNodes = append(vlanNodes, NodeDef{
		Name:     "L3_Core_Switch",
		Labels:   []string{"Technical", "NetworkSwitch", "CoreRouter"},
		Level:    "VLAN",
		NodeType: "technical",
	})

	return b.AddNodes(vlanNodes).
		AddEdgePairs(waterVLANEdges, "TECHNICAL").
		Build()
}

// Package models provides OT network model definitions.
package models

// StevesUtilityNodes defines all 33 nodes for Steve's Utility model.
// Demonstrates how one helpful senior OT technician accumulates cross-domain access,
// creating an invisible single point of failure.
var StevesUtilityNodes = []NodeDef{
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

// StevesUtilityEdges defines all edges with their types for layer analysis.
var StevesUtilityEdges = []EdgeDef{
	// TECHNICAL (26)
	{"PLC_Turbine1", "HMI_Control1", EdgeTypeTechnical},
	{"PLC_Turbine2", "HMI_Control2", EdgeTypeTechnical},
	{"PLC_Substation", "HMI_Control1", EdgeTypeTechnical},
	{"RTU_Remote1", "SCADA_Server", EdgeTypeTechnical},
	{"RTU_Remote2", "SCADA_Server", EdgeTypeTechnical},
	{"Safety_PLC", "HMI_Control1", EdgeTypeTechnical},
	{"Safety_PLC", "HMI_Control2", EdgeTypeTechnical},
	{"HMI_Control1", "SCADA_Server", EdgeTypeTechnical},
	{"HMI_Control2", "SCADA_Server", EdgeTypeTechnical},
	{"SCADA_Server", "Historian_OT", EdgeTypeTechnical},
	{"SCADA_Server", "Eng_Workstation", EdgeTypeTechnical},
	{"SCADA_Server", "OT_Switch_Core", EdgeTypeTechnical},
	{"Historian_OT", "OT_Switch_Core", EdgeTypeTechnical},
	{"Eng_Workstation", "OT_Switch_Core", EdgeTypeTechnical},
	{"OT_Switch_Core", "Patch_Server", EdgeTypeTechnical},
	{"OT_Switch_Core", "AD_Server_OT", EdgeTypeTechnical},
	{"OT_Switch_Core", "Firewall_ITOT", EdgeTypeTechnical},
	{"Firewall_ITOT", "Jump_Server", EdgeTypeTechnical},
	{"Firewall_ITOT", "Data_Diode", EdgeTypeTechnical},
	{"Data_Diode", "Historian_OT", EdgeTypeTechnical},
	{"Firewall_ITOT", "IT_Switch_Core", EdgeTypeTechnical},
	{"Jump_Server", "IT_Switch_Core", EdgeTypeTechnical},
	{"IT_Switch_Core", "Email_Server", EdgeTypeTechnical},
	{"IT_Switch_Core", "ERP_System", EdgeTypeTechnical},
	{"IT_Switch_Core", "AD_Server_IT", EdgeTypeTechnical},
	{"IT_Switch_Core", "VPN_Gateway", EdgeTypeTechnical},

	// HUMAN_ACCESS — Steve's edges to technical/human nodes (19)
	{"Steve", "PLC_Turbine1", EdgeTypeHumanAccess},
	{"Steve", "PLC_Turbine2", EdgeTypeHumanAccess},
	{"Steve", "PLC_Substation", EdgeTypeHumanAccess},
	{"Steve", "HMI_Control1", EdgeTypeHumanAccess},
	{"Steve", "HMI_Control2", EdgeTypeHumanAccess},
	{"Steve", "SCADA_Server", EdgeTypeHumanAccess},
	{"Steve", "Eng_Workstation", EdgeTypeHumanAccess},
	{"Steve", "Historian_OT", EdgeTypeHumanAccess},
	{"Steve", "OT_Switch_Core", EdgeTypeHumanAccess},
	{"Steve", "Patch_Server", EdgeTypeHumanAccess},
	{"Steve", "Jump_Server", EdgeTypeHumanAccess},
	{"Steve", "Firewall_ITOT", EdgeTypeHumanAccess},
	{"Steve", "VPN_Gateway", EdgeTypeHumanAccess},
	{"Steve", "AD_Server_OT", EdgeTypeHumanAccess},
	{"Steve", "Vendor_Rep", EdgeTypeHumanAccess},
	{"Steve", "OT_Manager", EdgeTypeHumanAccess},
	{"Steve", "Control_Op1", EdgeTypeHumanAccess},
	{"Steve", "Control_Op2", EdgeTypeHumanAccess},
	{"Steve", "IT_Admin", EdgeTypeHumanAccess},

	// HUMAN_ACCESS — other human edges to technical/human nodes (16)
	{"Control_Op1", "HMI_Control1", EdgeTypeHumanAccess},
	{"Control_Op1", "HMI_Control2", EdgeTypeHumanAccess},
	{"Control_Op2", "HMI_Control1", EdgeTypeHumanAccess},
	{"Control_Op2", "HMI_Control2", EdgeTypeHumanAccess},
	{"OT_Manager", "SCADA_Server", EdgeTypeHumanAccess},
	{"OT_Manager", "Plant_Manager", EdgeTypeHumanAccess},
	{"IT_Admin", "IT_Switch_Core", EdgeTypeHumanAccess},
	{"IT_Admin", "Email_Server", EdgeTypeHumanAccess},
	{"IT_Admin", "ERP_System", EdgeTypeHumanAccess},
	{"IT_Admin", "AD_Server_IT", EdgeTypeHumanAccess},
	{"IT_Admin", "VPN_Gateway", EdgeTypeHumanAccess},
	{"IT_Admin", "Firewall_ITOT", EdgeTypeHumanAccess},
	{"Plant_Manager", "ERP_System", EdgeTypeHumanAccess},
	{"Plant_Manager", "Email_Server", EdgeTypeHumanAccess},
	{"Vendor_Rep", "VPN_Gateway", EdgeTypeHumanAccess},
	{"Vendor_Rep", "Jump_Server", EdgeTypeHumanAccess},

	// PROCESS — edges involving process nodes (9)
	{"Steve", "Change_Mgmt_Process", EdgeTypeProcess},
	{"Steve", "Incident_Response", EdgeTypeProcess},
	{"Steve", "Vendor_Access_Process", EdgeTypeProcess},
	{"Steve", "Patch_Approval", EdgeTypeProcess},
	{"Control_Op1", "Incident_Response", EdgeTypeProcess},
	{"Control_Op2", "Incident_Response", EdgeTypeProcess},
	{"OT_Manager", "Change_Mgmt_Process", EdgeTypeProcess},
	{"OT_Manager", "Patch_Approval", EdgeTypeProcess},
	{"Vendor_Rep", "Vendor_Access_Process", EdgeTypeProcess},
}

// StevesUtilityNodesWithoutSteve returns nodes filtered to exclude Steve.
func StevesUtilityNodesWithoutSteve() []NodeDef {
	result := make([]NodeDef, 0, len(StevesUtilityNodes)-1)
	for _, n := range StevesUtilityNodes {
		if n.Name != "Steve" {
			result = append(result, n)
		}
	}
	return result
}

// StevesUtilityEdgesWithoutSteve returns edges filtered to exclude Steve.
func StevesUtilityEdgesWithoutSteve() []EdgeDef {
	result := make([]EdgeDef, 0)
	for _, e := range StevesUtilityEdges {
		if e.From != "Steve" && e.To != "Steve" {
			result = append(result, e)
		}
	}
	return result
}

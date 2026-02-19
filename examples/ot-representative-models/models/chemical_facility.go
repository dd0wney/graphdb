// Package models provides OT network model definitions.
package models

// ChemicalFacilityNodes defines all 24 nodes for the Chemical Facility model.
// Demonstrates IT/OT bridge concentration through the IT_OT_Coord role.
var ChemicalFacilityNodes = []NodeDef{
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

// ChemicalFacilityEdgePairs defines edges as simple [from, to] pairs.
// Edge types are auto-detected based on node types.
var ChemicalFacilityEdgePairs = [][2]string{
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

// Package models provides OT network model definitions.
package models

// WaterTreatmentNodes defines all 13 nodes for the Water Treatment model.
// Used by both flat and VLAN variants.
var WaterTreatmentNodes = []NodeDef{
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

// WaterFlatEdgePairs defines edges for the flat mesh topology.
// Three switches in full mesh topology (13 nodes, 13 undirected edges).
var WaterFlatEdgePairs = [][2]string{
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

// WaterVLANEdgePairs defines edges for the VLAN star topology.
// Star topology through L3 core switch (14 nodes, 13 undirected edges).
// Demonstrates how VLAN segmentation concentrates betweenness centrality.
var WaterVLANEdgePairs = [][2]string{
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

// WaterVLANNodes returns nodes for the VLAN variant, including L3 core switch.
func WaterVLANNodes() []NodeDef {
	nodes := make([]NodeDef, len(WaterTreatmentNodes)+1)
	copy(nodes, WaterTreatmentNodes)
	nodes[len(WaterTreatmentNodes)] = NodeDef{
		Name:     "L3_Core_Switch",
		Labels:   []string{"Technical", "NetworkSwitch", "CoreRouter"},
		Level:    "VLAN",
		NodeType: NodeTypeTechnical,
	}
	return nodes
}

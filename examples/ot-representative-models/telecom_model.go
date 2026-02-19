// Package main provides the telecom provider model for betweenness centrality analysis.
// Model 4 demonstrates that the invisible node pattern scales to realistic complexity.
package main

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TelecomMetadata extends ModelMetadata with telecom-specific fields
type TelecomMetadata struct {
	Graph         *storage.GraphStorage
	NodeNames     map[uint64]string // ID -> display name
	NodeTypes     map[uint64]string // ID -> "technical", "human", "process", "external"
	NodeLevels    map[uint64]string // ID -> level description
	NodeFunctions map[uint64]string // ID -> function description
	NodeIDs       map[string]uint64 // name -> ID (reverse lookup)
}

// BuildTelecomProvider creates Model 4: Telecommunications Provider (114 nodes, 253 undirected edges)
// Demonstrates cross-sector critical infrastructure dependencies and the invisible node pattern
// at scale. The telecom sector is the "infrastructure of infrastructures".
func BuildTelecomProvider(dataPath string) (*TelecomMetadata, error) {
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	meta := &TelecomMetadata{
		Graph:         graph,
		NodeNames:     make(map[uint64]string),
		NodeTypes:     make(map[uint64]string),
		NodeLevels:    make(map[uint64]string),
		NodeFunctions: make(map[uint64]string),
		NodeIDs:       make(map[string]uint64),
	}

	// Helper to create a node and track metadata
	createNode := func(name string, labels []string, level, nodeType, function string) error {
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"level":     storage.StringValue(level),
			"node_type": storage.StringValue(nodeType),
			"function":  storage.StringValue(function),
		})
		if err != nil {
			return err
		}
		meta.NodeNames[node.ID] = name
		meta.NodeTypes[node.ID] = nodeType
		meta.NodeLevels[node.ID] = level
		meta.NodeFunctions[node.ID] = function
		meta.NodeIDs[name] = node.ID
		return nil
	}

	// ========================================
	// CORE_NETWORK (9 nodes)
	// ========================================
	createNode("Core_Router_SYD", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_MEL", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_BNE", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_PER", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_ADL", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("DWDM_SYD_MEL", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")
	createNode("DWDM_SYD_BNE", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")
	createNode("DWDM_MEL_ADL", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")
	createNode("DWDM_ADL_PER", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")

	// ========================================
	// MOBILE_CORE (8 nodes)
	// ========================================
	createNode("MME_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("MME_Secondary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("SGW_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("SGW_Secondary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("PGW_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("PGW_Secondary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("HSS_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("IMS_Core", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")

	// ========================================
	// ACCESS_NETWORK (19 nodes)
	// ========================================
	createNode("Exchange_CBD", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_North", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_South", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_West", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_Industrial", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Cell_CBD_1", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_CBD_2", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_CBD_3", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Suburban_1", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Suburban_2", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Regional_1", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Regional_2", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Hospital", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Emergency_HQ", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("FDH_CBD", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Business_Park", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Hospital", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Gov_Precinct", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Data_Centre", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")

	// ========================================
	// NOC (7 nodes)
	// ========================================
	createNode("NMS_Primary", []string{"Technical", "NetworkManagement"}, "NOC", "technical", "Network operations")
	createNode("NMS_Secondary", []string{"Technical", "NetworkManagement"}, "NOC", "technical", "Network operations")
	createNode("NOC_Dashboard", []string{"Technical", "Dashboard"}, "NOC", "technical", "Network operations")
	createNode("Fault_Mgmt_System", []string{"Technical", "FaultManagement"}, "NOC", "technical", "Network operations")
	createNode("Performance_Monitor", []string{"Technical", "Monitoring"}, "NOC", "technical", "Network operations")
	createNode("SIEM_Telecom", []string{"Technical", "Security"}, "NOC", "technical", "Network operations")
	createNode("Ticketing_System", []string{"Technical", "ServiceDesk"}, "NOC", "technical", "Network operations")

	// ========================================
	// BSS_OSS (9 nodes)
	// ========================================
	createNode("Billing_System", []string{"Technical", "BSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("CRM_System", []string{"Technical", "BSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("Provisioning_System", []string{"Technical", "OSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("Inventory_System", []string{"Technical", "OSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("Workforce_Mgmt", []string{"Technical", "OSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("DNS_Primary", []string{"Technical", "DNS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("DNS_Secondary", []string{"Technical", "DNS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("AAA_Server", []string{"Technical", "Authentication"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("RADIUS_Server", []string{"Technical", "Authentication"}, "BSS_OSS", "technical", "Business/operations support")

	// ========================================
	// CORPORATE_IT (6 nodes)
	// ========================================
	createNode("Corp_Firewall", []string{"Technical", "Firewall"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("IT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_AD", []string{"Technical", "ActiveDirectory"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_Email", []string{"Technical", "Email"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_ERP", []string{"Technical", "ERP"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_VPN", []string{"Technical", "VPN"}, "Corporate_IT", "technical", "Corporate IT systems")

	// ========================================
	// INTERCONNECTION (8 nodes)
	// ========================================
	createNode("IX_Peering", []string{"Technical", "InternetExchange"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Internet", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Banking", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Emergency", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Healthcare", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Transport", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Energy", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("SIP_Trunk_Emergency", []string{"Technical", "SIP"}, "Interconnection", "technical", "Sector interconnection")

	// ========================================
	// HUMAN NODES (22)
	// ========================================
	createNode("Senior_Network_Eng", []string{"Human", "Engineer"}, "Human", "human", "Senior Network Engineer")
	createNode("IP_Engineer", []string{"Human", "Engineer"}, "Human", "human", "IP/MPLS Engineer")
	createNode("Mobile_Engineer", []string{"Human", "Engineer"}, "Human", "human", "Mobile Core Engineer")
	createNode("Optical_Engineer", []string{"Human", "Engineer"}, "Human", "human", "Optical Transport Engineer")
	createNode("NOC_Shift_Lead", []string{"Human", "NOC"}, "Human", "human", "NOC Shift Leader")
	createNode("NOC_Operator_1", []string{"Human", "NOC"}, "Human", "human", "NOC Operator")
	createNode("NOC_Operator_2", []string{"Human", "NOC"}, "Human", "human", "NOC Operator")
	createNode("NOC_Security_Analyst", []string{"Human", "Security"}, "Human", "human", "Security Analyst")
	createNode("NOC_Manager", []string{"Human", "Manager"}, "Human", "human", "NOC Manager")
	createNode("Network_Director", []string{"Human", "Director"}, "Human", "human", "Network Operations Director")
	createNode("CTO", []string{"Human", "Executive"}, "Human", "human", "Chief Technology Officer")
	createNode("Security_Manager", []string{"Human", "Manager"}, "Human", "human", "Security Manager")
	createNode("Field_Tech_1", []string{"Human", "FieldTech"}, "Human", "human", "Field Technician")
	createNode("Field_Tech_2", []string{"Human", "FieldTech"}, "Human", "human", "Field Technician")
	createNode("BSS_Admin", []string{"Human", "Admin"}, "Human", "human", "BSS/OSS Administrator")
	createNode("IT_Admin_Corp", []string{"Human", "Admin"}, "Human", "human", "Corporate IT Admin")
	createNode("Vendor_Ericsson", []string{"Human", "Vendor"}, "Human", "human", "Ericsson Support Engineer")
	createNode("Vendor_Cisco", []string{"Human", "Vendor"}, "Human", "human", "Cisco TAC Engineer")
	createNode("Vendor_Nokia", []string{"Human", "Vendor"}, "Human", "human", "Nokia Optical Support")
	createNode("Managed_SOC_Analyst", []string{"Human", "ManagedService"}, "Human", "human", "Managed SOC Analyst")
	createNode("Banking_Liaison", []string{"Human", "Liaison"}, "Human", "human", "Banking Sector Account Manager")
	createNode("Emergency_Liaison", []string{"Human", "Liaison"}, "Human", "human", "Emergency Services Liaison")

	// ========================================
	// PROCESS NODES (8)
	// ========================================
	createNode("Change_Advisory_Board", []string{"Process", "ChangeManagement"}, "Process", "process", "Change advisory board")
	createNode("Incident_Mgmt_Process", []string{"Process", "IncidentManagement"}, "Process", "process", "Incident management process")
	createNode("Vendor_Access_Control", []string{"Process", "VendorManagement"}, "Process", "process", "Vendor remote access governance")
	createNode("Patch_Management", []string{"Process", "PatchManagement"}, "Process", "process", "Patch management process")
	createNode("Capacity_Planning", []string{"Process", "CapacityPlanning"}, "Process", "process", "Capacity planning process")
	createNode("Disaster_Recovery", []string{"Process", "DisasterRecovery"}, "Process", "process", "Disaster recovery process")
	createNode("Regulatory_Compliance", []string{"Process", "Compliance"}, "Process", "process", "ACMA/critical infrastructure obligations")
	createNode("SLA_Management", []string{"Process", "SLA"}, "Process", "process", "Service level agreement monitoring")

	// ========================================
	// EXTERNAL SECTOR NODES (18)
	// ========================================
	// Banking (4)
	createNode("Bank_ATM_Network", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Bank_Branch_WAN", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Bank_Trading_Floor", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Bank_SWIFT_Gateway", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")

	// Emergency (5)
	createNode("Triple_Zero_Centre", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("CAD_System", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Police_Radio_GW", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Fire_Dispatch", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Ambulance_Dispatch", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")

	// Healthcare (3)
	createNode("Hospital_Network", []string{"External", "Healthcare"}, "Sector_Healthcare", "external", "Healthcare infrastructure")
	createNode("Telehealth_Platform", []string{"External", "Healthcare"}, "Sector_Healthcare", "external", "Healthcare infrastructure")
	createNode("Pathology_WAN", []string{"External", "Healthcare"}, "Sector_Healthcare", "external", "Healthcare infrastructure")

	// Transport (3)
	createNode("Rail_SCADA_Comms", []string{"External", "Transport"}, "Sector_Transport", "external", "Transport infrastructure")
	createNode("Traffic_Mgmt_System", []string{"External", "Transport"}, "Sector_Transport", "external", "Transport infrastructure")
	createNode("Port_Operations", []string{"External", "Transport"}, "Sector_Transport", "external", "Transport infrastructure")

	// Energy (3)
	createNode("Grid_SCADA_Comms", []string{"External", "Energy"}, "Sector_Energy", "external", "Energy infrastructure")
	createNode("Gas_Pipeline_Comms", []string{"External", "Energy"}, "Sector_Energy", "external", "Energy infrastructure")
	createNode("Substation_Comms", []string{"External", "Energy"}, "Sector_Energy", "external", "Energy infrastructure")

	// ========================================
	// ALL EDGES (253 undirected)
	// ========================================

	// backbone (10 edges)
	backboneEdges := [][2]string{
		{"Core_Router_SYD", "DWDM_SYD_MEL"},
		{"Core_Router_SYD", "DWDM_SYD_BNE"},
		{"Core_Router_SYD", "Core_Router_MEL"},
		{"Core_Router_SYD", "Core_Router_BNE"},
		{"Core_Router_MEL", "DWDM_SYD_MEL"},
		{"Core_Router_MEL", "DWDM_MEL_ADL"},
		{"Core_Router_BNE", "DWDM_SYD_BNE"},
		{"Core_Router_PER", "DWDM_ADL_PER"},
		{"Core_Router_ADL", "DWDM_MEL_ADL"},
		{"Core_Router_ADL", "DWDM_ADL_PER"},
	}

	// mobile_core (12 edges)
	mobileCoreEdges := [][2]string{
		{"Core_Router_SYD", "PGW_Primary"},
		{"Core_Router_SYD", "MME_Primary"},
		{"Core_Router_MEL", "PGW_Secondary"},
		{"Core_Router_MEL", "MME_Secondary"},
		{"MME_Primary", "SGW_Primary"},
		{"MME_Primary", "HSS_Primary"},
		{"MME_Secondary", "SGW_Secondary"},
		{"MME_Secondary", "HSS_Primary"},
		{"SGW_Primary", "PGW_Primary"},
		{"SGW_Primary", "IMS_Core"},
		{"SGW_Secondary", "PGW_Secondary"},
		{"IMS_Core", "SIP_Trunk_Emergency"},
	}

	// access (28 edges)
	accessEdges := [][2]string{
		{"Core_Router_SYD", "Exchange_CBD"},
		{"Core_Router_SYD", "Exchange_North"},
		{"Core_Router_SYD", "Exchange_South"},
		{"Core_Router_SYD", "Exchange_West"},
		{"Core_Router_SYD", "Exchange_Industrial"},
		{"MME_Primary", "Cell_CBD_1"},
		{"MME_Primary", "Cell_CBD_2"},
		{"MME_Primary", "Cell_CBD_3"},
		{"MME_Primary", "Cell_Suburban_1"},
		{"MME_Primary", "Cell_Hospital"},
		{"MME_Primary", "Cell_Emergency_HQ"},
		{"MME_Secondary", "Cell_Suburban_2"},
		{"MME_Secondary", "Cell_Regional_1"},
		{"MME_Secondary", "Cell_Regional_2"},
		{"Exchange_CBD", "Cell_CBD_1"},
		{"Exchange_CBD", "Cell_CBD_2"},
		{"Exchange_CBD", "Cell_CBD_3"},
		{"Exchange_CBD", "Cell_Emergency_HQ"},
		{"Exchange_CBD", "FDH_CBD"},
		{"Exchange_CBD", "FDH_Gov_Precinct"},
		{"Exchange_North", "Cell_Suburban_1"},
		{"Exchange_North", "FDH_Business_Park"},
		{"Exchange_South", "Cell_Suburban_2"},
		{"Exchange_South", "Cell_Hospital"},
		{"Exchange_South", "FDH_Hospital"},
		{"Exchange_West", "Cell_Regional_1"},
		{"Exchange_West", "Cell_Regional_2"},
		{"Exchange_Industrial", "FDH_Data_Centre"},
	}

	// management (32 edges)
	managementEdges := [][2]string{
		{"Core_Router_SYD", "NMS_Primary"},
		{"Core_Router_SYD", "Performance_Monitor"},
		{"NMS_Primary", "NMS_Secondary"},
		{"NMS_Primary", "NOC_Dashboard"},
		{"NMS_Primary", "Fault_Mgmt_System"},
		{"NMS_Primary", "Performance_Monitor"},
		{"NMS_Primary", "SIEM_Telecom"},
		{"NMS_Secondary", "NOC_Dashboard"},
		{"SIEM_Telecom", "Corp_Firewall"},
		{"SIEM_Telecom", "AAA_Server"},
		{"SIEM_Telecom", "Security_Manager"},
		{"Fault_Mgmt_System", "NOC_Dashboard"},
		{"Fault_Mgmt_System", "Ticketing_System"},
		{"NOC_Dashboard", "Ticketing_System"},
		{"Ticketing_System", "Workforce_Mgmt"},
		{"Corp_Email", "CTO"},
		{"Corp_ERP", "CTO"},
		{"NOC_Shift_Lead", "NOC_Manager"},
		{"Security_Manager", "CTO"},
		{"Security_Manager", "Regulatory_Compliance"},
		{"Security_Manager", "Vendor_Access_Control"},
		{"Security_Manager", "Patch_Management"},
		{"CTO", "Network_Director"},
		{"CTO", "NOC_Manager"},
		{"CTO", "Change_Advisory_Board"},
		{"CTO", "Regulatory_Compliance"},
		{"NOC_Manager", "Network_Director"},
		{"NOC_Manager", "Incident_Mgmt_Process"},
		{"NOC_Manager", "SLA_Management"},
		{"Network_Director", "Change_Advisory_Board"},
		{"Network_Director", "Capacity_Planning"},
		{"Network_Director", "Disaster_Recovery"},
	}

	// bss_oss (12 edges)
	bssOssEdges := [][2]string{
		{"Core_Router_SYD", "DNS_Primary"},
		{"Core_Router_SYD", "AAA_Server"},
		{"Core_Router_SYD", "RADIUS_Server"},
		{"Core_Router_MEL", "DNS_Secondary"},
		{"NMS_Primary", "Provisioning_System"},
		{"Billing_System", "CRM_System"},
		{"Billing_System", "Provisioning_System"},
		{"CRM_System", "Corp_ERP"},
		{"Provisioning_System", "Inventory_System"},
		{"Inventory_System", "Workforce_Mgmt"},
		{"DNS_Primary", "DNS_Secondary"},
		{"AAA_Server", "RADIUS_Server"},
	}

	// corporate (6 edges)
	corporateEdges := [][2]string{
		{"Core_Router_SYD", "Corp_Firewall"},
		{"Corp_Firewall", "IT_Switch_Core"},
		{"Corp_Firewall", "Corp_VPN"},
		{"Corp_AD", "IT_Switch_Core"},
		{"Corp_Email", "IT_Switch_Core"},
		{"Corp_ERP", "IT_Switch_Core"},
	}

	// interconnection (16 edges)
	interconnectionEdges := [][2]string{
		{"Core_Router_SYD", "IX_Peering"},
		{"Core_Router_SYD", "Gateway_Internet"},
		{"Core_Router_SYD", "Gateway_Banking"},
		{"Core_Router_SYD", "Gateway_Emergency"},
		{"Core_Router_SYD", "Gateway_Healthcare"},
		{"Core_Router_SYD", "Gateway_Transport"},
		{"Core_Router_SYD", "Gateway_Energy"},
		{"Core_Router_MEL", "IX_Peering"},
		{"Exchange_Industrial", "Gateway_Transport"},
		{"Exchange_Industrial", "Gateway_Energy"},
		{"FDH_CBD", "Gateway_Banking"},
		{"FDH_Business_Park", "Gateway_Banking"},
		{"FDH_Hospital", "Gateway_Healthcare"},
		{"FDH_Gov_Precinct", "Gateway_Emergency"},
		{"IX_Peering", "Gateway_Internet"},
		{"Gateway_Emergency", "SIP_Trunk_Emergency"},
	}

	// sector_dependency (20 edges)
	sectorDependencyEdges := [][2]string{
		{"FDH_Hospital", "Hospital_Network"},
		{"Gateway_Banking", "Bank_ATM_Network"},
		{"Gateway_Banking", "Bank_Branch_WAN"},
		{"Gateway_Banking", "Bank_Trading_Floor"},
		{"Gateway_Banking", "Bank_SWIFT_Gateway"},
		{"Gateway_Emergency", "Triple_Zero_Centre"},
		{"Gateway_Emergency", "CAD_System"},
		{"Gateway_Emergency", "Police_Radio_GW"},
		{"Gateway_Healthcare", "Hospital_Network"},
		{"Gateway_Healthcare", "Telehealth_Platform"},
		{"Gateway_Healthcare", "Pathology_WAN"},
		{"Gateway_Transport", "Rail_SCADA_Comms"},
		{"Gateway_Transport", "Traffic_Mgmt_System"},
		{"Gateway_Transport", "Port_Operations"},
		{"Gateway_Energy", "Grid_SCADA_Comms"},
		{"Gateway_Energy", "Gas_Pipeline_Comms"},
		{"Gateway_Energy", "Substation_Comms"},
		{"SIP_Trunk_Emergency", "Triple_Zero_Centre"},
		{"CAD_System", "Fire_Dispatch"},
		{"CAD_System", "Ambulance_Dispatch"},
	}

	// human_access (92 edges)
	humanAccessEdges := [][2]string{
		{"Core_Router_SYD", "Senior_Network_Eng"},
		{"Core_Router_SYD", "IP_Engineer"},
		{"Core_Router_MEL", "Senior_Network_Eng"},
		{"Core_Router_MEL", "IP_Engineer"},
		{"Core_Router_BNE", "Senior_Network_Eng"},
		{"Core_Router_BNE", "IP_Engineer"},
		{"Core_Router_PER", "IP_Engineer"},
		{"Core_Router_ADL", "IP_Engineer"},
		{"DWDM_SYD_MEL", "Optical_Engineer"},
		{"DWDM_SYD_BNE", "Optical_Engineer"},
		{"DWDM_MEL_ADL", "Optical_Engineer"},
		{"DWDM_ADL_PER", "Optical_Engineer"},
		{"MME_Primary", "Senior_Network_Eng"},
		{"MME_Primary", "Mobile_Engineer"},
		{"MME_Secondary", "Senior_Network_Eng"},
		{"MME_Secondary", "Mobile_Engineer"},
		{"SGW_Primary", "Mobile_Engineer"},
		{"SGW_Secondary", "Mobile_Engineer"},
		{"PGW_Primary", "Mobile_Engineer"},
		{"HSS_Primary", "Mobile_Engineer"},
		{"IMS_Core", "Mobile_Engineer"},
		{"Exchange_CBD", "Senior_Network_Eng"},
		{"Exchange_CBD", "Field_Tech_1"},
		{"Exchange_North", "Field_Tech_1"},
		{"Exchange_South", "Field_Tech_2"},
		{"Exchange_West", "Field_Tech_2"},
		{"Cell_CBD_1", "Field_Tech_1"},
		{"Cell_CBD_2", "Field_Tech_1"},
		{"Cell_Suburban_1", "Field_Tech_1"},
		{"Cell_Suburban_2", "Field_Tech_2"},
		{"Cell_Regional_1", "Field_Tech_2"},
		{"Cell_Regional_2", "Field_Tech_2"},
		{"NMS_Primary", "Senior_Network_Eng"},
		{"NMS_Primary", "NOC_Shift_Lead"},
		{"SIEM_Telecom", "NOC_Security_Analyst"},
		{"Fault_Mgmt_System", "Senior_Network_Eng"},
		{"Fault_Mgmt_System", "NOC_Shift_Lead"},
		{"Fault_Mgmt_System", "NOC_Operator_1"},
		{"Fault_Mgmt_System", "NOC_Operator_2"},
		{"NOC_Dashboard", "NOC_Shift_Lead"},
		{"NOC_Dashboard", "NOC_Operator_1"},
		{"NOC_Dashboard", "NOC_Operator_2"},
		{"NOC_Dashboard", "NOC_Security_Analyst"},
		{"Ticketing_System", "NOC_Shift_Lead"},
		{"Ticketing_System", "NOC_Operator_1"},
		{"Ticketing_System", "NOC_Operator_2"},
		{"Billing_System", "BSS_Admin"},
		{"CRM_System", "BSS_Admin"},
		{"Provisioning_System", "Senior_Network_Eng"},
		{"Provisioning_System", "BSS_Admin"},
		{"Inventory_System", "BSS_Admin"},
		{"Workforce_Mgmt", "Field_Tech_1"},
		{"Workforce_Mgmt", "Field_Tech_2"},
		{"DNS_Primary", "Senior_Network_Eng"},
		{"AAA_Server", "Senior_Network_Eng"},
		{"AAA_Server", "IP_Engineer"},
		{"Corp_Firewall", "IT_Admin_Corp"},
		{"Corp_AD", "IT_Admin_Corp"},
		{"Corp_Email", "IT_Admin_Corp"},
		{"Corp_VPN", "Senior_Network_Eng"},
		{"Corp_VPN", "IT_Admin_Corp"},
		{"IT_Switch_Core", "IT_Admin_Corp"},
		{"Gateway_Banking", "Senior_Network_Eng"},
		{"Gateway_Emergency", "Senior_Network_Eng"},
		{"NOC_Shift_Lead", "Senior_Network_Eng"},
		{"NOC_Shift_Lead", "NOC_Operator_1"},
		{"NOC_Shift_Lead", "NOC_Operator_2"},
		{"NOC_Shift_Lead", "NOC_Security_Analyst"},
		{"NOC_Shift_Lead", "Incident_Mgmt_Process"},
		{"NOC_Security_Analyst", "Security_Manager"},
		{"NOC_Security_Analyst", "Incident_Mgmt_Process"},
		{"Senior_Network_Eng", "Change_Advisory_Board"},
		{"Senior_Network_Eng", "Incident_Mgmt_Process"},
		{"Senior_Network_Eng", "Vendor_Access_Control"},
		{"Senior_Network_Eng", "Capacity_Planning"},
		{"Senior_Network_Eng", "Disaster_Recovery"},
		{"Senior_Network_Eng", "IP_Engineer"},
		{"Senior_Network_Eng", "Mobile_Engineer"},
		{"Senior_Network_Eng", "Optical_Engineer"},
		{"Senior_Network_Eng", "Vendor_Ericsson"},
		{"Senior_Network_Eng", "Vendor_Cisco"},
		{"Senior_Network_Eng", "Vendor_Nokia"},
		{"Senior_Network_Eng", "Network_Director"},
		{"Senior_Network_Eng", "CTO"},
		{"Senior_Network_Eng", "Emergency_Liaison"},
		{"Senior_Network_Eng", "Banking_Liaison"},
		{"IP_Engineer", "Change_Advisory_Board"},
		{"Mobile_Engineer", "Change_Advisory_Board"},
		{"Optical_Engineer", "Change_Advisory_Board"},
		{"IT_Admin_Corp", "Patch_Management"},
		{"BSS_Admin", "SLA_Management"},
		{"BSS_Admin", "Patch_Management"},
	}

	// vendor_access (17 edges)
	vendorAccessEdges := [][2]string{
		{"Core_Router_SYD", "Vendor_Cisco"},
		{"Core_Router_MEL", "Vendor_Cisco"},
		{"DWDM_SYD_MEL", "Vendor_Nokia"},
		{"DWDM_SYD_BNE", "Vendor_Nokia"},
		{"MME_Primary", "Vendor_Ericsson"},
		{"MME_Secondary", "Vendor_Ericsson"},
		{"HSS_Primary", "Vendor_Ericsson"},
		{"SIEM_Telecom", "Managed_SOC_Analyst"},
		{"Corp_VPN", "Vendor_Ericsson"},
		{"Corp_VPN", "Vendor_Cisco"},
		{"Corp_VPN", "Vendor_Nokia"},
		{"Corp_VPN", "Managed_SOC_Analyst"},
		{"Security_Manager", "Managed_SOC_Analyst"},
		{"Vendor_Ericsson", "Vendor_Access_Control"},
		{"Vendor_Cisco", "Vendor_Access_Control"},
		{"Vendor_Nokia", "Vendor_Access_Control"},
		{"Managed_SOC_Analyst", "Vendor_Access_Control"},
	}

	// liaison (8 edges)
	liaisonEdges := [][2]string{
		{"CRM_System", "Banking_Liaison"},
		{"Gateway_Banking", "Banking_Liaison"},
		{"Gateway_Emergency", "Emergency_Liaison"},
		{"SIP_Trunk_Emergency", "Emergency_Liaison"},
		{"Banking_Liaison", "SLA_Management"},
		{"Banking_Liaison", "Incident_Mgmt_Process"},
		{"Emergency_Liaison", "Incident_Mgmt_Process"},
		{"Emergency_Liaison", "Regulatory_Compliance"},
	}

	// Create all edges
	props := map[string]storage.Value{}

	allEdgeGroups := [][][2]string{
		backboneEdges,
		mobileCoreEdges,
		accessEdges,
		managementEdges,
		bssOssEdges,
		corporateEdges,
		interconnectionEdges,
		sectorDependencyEdges,
		humanAccessEdges,
		vendorAccessEdges,
		liaisonEdges,
	}

	edgeTypes := []string{
		"BACKBONE",
		"MOBILE_CORE",
		"ACCESS",
		"MANAGEMENT",
		"BSS_OSS",
		"CORPORATE",
		"INTERCONNECTION",
		"SECTOR_DEPENDENCY",
		"HUMAN_ACCESS",
		"VENDOR_ACCESS",
		"LIAISON",
	}

	for i, edges := range allEdgeGroups {
		edgeType := edgeTypes[i]
		for _, edge := range edges {
			fromID := meta.NodeIDs[edge[0]]
			toID := meta.NodeIDs[edge[1]]
			if fromID == 0 {
				return nil, fmt.Errorf("unknown node: %s", edge[0])
			}
			if toID == 0 {
				return nil, fmt.Errorf("unknown node: %s", edge[1])
			}
			if err := createUndirectedEdge(graph, fromID, toID, edgeType, props); err != nil {
				return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
			}
		}
	}

	return meta, nil
}

// BuildTelecomProviderWithoutSeniorEng creates Model 4 without the Senior Network Engineer
// for removal analysis. Demonstrates how one key human node absorbs critical path traffic.
func BuildTelecomProviderWithoutSeniorEng(dataPath string) (*TelecomMetadata, error) {
	// Build the full model first
	meta, err := BuildTelecomProvider(dataPath + "_temp")
	if err != nil {
		return nil, err
	}
	meta.Graph.Close()

	// Now rebuild without Senior_Network_Eng
	graph, err := storage.NewGraphStorage(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph storage: %w", err)
	}

	newMeta := &TelecomMetadata{
		Graph:         graph,
		NodeNames:     make(map[uint64]string),
		NodeTypes:     make(map[uint64]string),
		NodeLevels:    make(map[uint64]string),
		NodeFunctions: make(map[uint64]string),
		NodeIDs:       make(map[string]uint64),
	}

	// Helper to create a node
	createNode := func(name string, labels []string, level, nodeType, function string) error {
		if name == "Senior_Network_Eng" {
			return nil // Skip this node
		}
		node, err := graph.CreateNode(labels, map[string]storage.Value{
			"name":      storage.StringValue(name),
			"level":     storage.StringValue(level),
			"node_type": storage.StringValue(nodeType),
			"function":  storage.StringValue(function),
		})
		if err != nil {
			return err
		}
		newMeta.NodeNames[node.ID] = name
		newMeta.NodeTypes[node.ID] = nodeType
		newMeta.NodeLevels[node.ID] = level
		newMeta.NodeFunctions[node.ID] = function
		newMeta.NodeIDs[name] = node.ID
		return nil
	}

	// Create all nodes except Senior_Network_Eng (copying from original build)
	// Core Network
	createNode("Core_Router_SYD", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_MEL", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_BNE", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_PER", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("Core_Router_ADL", []string{"Technical", "Router", "MPLS"}, "Core_Network", "technical", "MPLS backbone routing")
	createNode("DWDM_SYD_MEL", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")
	createNode("DWDM_SYD_BNE", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")
	createNode("DWDM_MEL_ADL", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")
	createNode("DWDM_ADL_PER", []string{"Technical", "Optical"}, "Core_Network", "technical", "Optical transport")

	// Mobile Core
	createNode("MME_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("MME_Secondary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("SGW_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("SGW_Secondary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("PGW_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("PGW_Secondary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("HSS_Primary", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")
	createNode("IMS_Core", []string{"Technical", "MobileCore"}, "Mobile_Core", "technical", "Mobile network core")

	// Access Network
	createNode("Exchange_CBD", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_North", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_South", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_West", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Exchange_Industrial", []string{"Technical", "Exchange"}, "Access_Network", "technical", "Local exchange")
	createNode("Cell_CBD_1", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_CBD_2", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_CBD_3", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Suburban_1", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Suburban_2", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Regional_1", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Regional_2", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Hospital", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("Cell_Emergency_HQ", []string{"Technical", "CellSite"}, "Access_Network", "technical", "Cell site/base station")
	createNode("FDH_CBD", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Business_Park", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Hospital", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Gov_Precinct", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")
	createNode("FDH_Data_Centre", []string{"Technical", "FibreDistribution"}, "Access_Network", "technical", "Fibre distribution")

	// NOC
	createNode("NMS_Primary", []string{"Technical", "NetworkManagement"}, "NOC", "technical", "Network operations")
	createNode("NMS_Secondary", []string{"Technical", "NetworkManagement"}, "NOC", "technical", "Network operations")
	createNode("NOC_Dashboard", []string{"Technical", "Dashboard"}, "NOC", "technical", "Network operations")
	createNode("Fault_Mgmt_System", []string{"Technical", "FaultManagement"}, "NOC", "technical", "Network operations")
	createNode("Performance_Monitor", []string{"Technical", "Monitoring"}, "NOC", "technical", "Network operations")
	createNode("SIEM_Telecom", []string{"Technical", "Security"}, "NOC", "technical", "Network operations")
	createNode("Ticketing_System", []string{"Technical", "ServiceDesk"}, "NOC", "technical", "Network operations")

	// BSS/OSS
	createNode("Billing_System", []string{"Technical", "BSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("CRM_System", []string{"Technical", "BSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("Provisioning_System", []string{"Technical", "OSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("Inventory_System", []string{"Technical", "OSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("Workforce_Mgmt", []string{"Technical", "OSS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("DNS_Primary", []string{"Technical", "DNS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("DNS_Secondary", []string{"Technical", "DNS"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("AAA_Server", []string{"Technical", "Authentication"}, "BSS_OSS", "technical", "Business/operations support")
	createNode("RADIUS_Server", []string{"Technical", "Authentication"}, "BSS_OSS", "technical", "Business/operations support")

	// Corporate IT
	createNode("Corp_Firewall", []string{"Technical", "Firewall"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("IT_Switch_Core", []string{"Technical", "NetworkSwitch"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_AD", []string{"Technical", "ActiveDirectory"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_Email", []string{"Technical", "Email"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_ERP", []string{"Technical", "ERP"}, "Corporate_IT", "technical", "Corporate IT systems")
	createNode("Corp_VPN", []string{"Technical", "VPN"}, "Corporate_IT", "technical", "Corporate IT systems")

	// Interconnection
	createNode("IX_Peering", []string{"Technical", "InternetExchange"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Internet", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Banking", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Emergency", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Healthcare", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Transport", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("Gateway_Energy", []string{"Technical", "Gateway"}, "Interconnection", "technical", "Sector interconnection")
	createNode("SIP_Trunk_Emergency", []string{"Technical", "SIP"}, "Interconnection", "technical", "Sector interconnection")

	// Human nodes (without Senior_Network_Eng)
	createNode("IP_Engineer", []string{"Human", "Engineer"}, "Human", "human", "IP/MPLS Engineer")
	createNode("Mobile_Engineer", []string{"Human", "Engineer"}, "Human", "human", "Mobile Core Engineer")
	createNode("Optical_Engineer", []string{"Human", "Engineer"}, "Human", "human", "Optical Transport Engineer")
	createNode("NOC_Shift_Lead", []string{"Human", "NOC"}, "Human", "human", "NOC Shift Leader")
	createNode("NOC_Operator_1", []string{"Human", "NOC"}, "Human", "human", "NOC Operator")
	createNode("NOC_Operator_2", []string{"Human", "NOC"}, "Human", "human", "NOC Operator")
	createNode("NOC_Security_Analyst", []string{"Human", "Security"}, "Human", "human", "Security Analyst")
	createNode("NOC_Manager", []string{"Human", "Manager"}, "Human", "human", "NOC Manager")
	createNode("Network_Director", []string{"Human", "Director"}, "Human", "human", "Network Operations Director")
	createNode("CTO", []string{"Human", "Executive"}, "Human", "human", "Chief Technology Officer")
	createNode("Security_Manager", []string{"Human", "Manager"}, "Human", "human", "Security Manager")
	createNode("Field_Tech_1", []string{"Human", "FieldTech"}, "Human", "human", "Field Technician")
	createNode("Field_Tech_2", []string{"Human", "FieldTech"}, "Human", "human", "Field Technician")
	createNode("BSS_Admin", []string{"Human", "Admin"}, "Human", "human", "BSS/OSS Administrator")
	createNode("IT_Admin_Corp", []string{"Human", "Admin"}, "Human", "human", "Corporate IT Admin")
	createNode("Vendor_Ericsson", []string{"Human", "Vendor"}, "Human", "human", "Ericsson Support Engineer")
	createNode("Vendor_Cisco", []string{"Human", "Vendor"}, "Human", "human", "Cisco TAC Engineer")
	createNode("Vendor_Nokia", []string{"Human", "Vendor"}, "Human", "human", "Nokia Optical Support")
	createNode("Managed_SOC_Analyst", []string{"Human", "ManagedService"}, "Human", "human", "Managed SOC Analyst")
	createNode("Banking_Liaison", []string{"Human", "Liaison"}, "Human", "human", "Banking Sector Account Manager")
	createNode("Emergency_Liaison", []string{"Human", "Liaison"}, "Human", "human", "Emergency Services Liaison")

	// Process nodes
	createNode("Change_Advisory_Board", []string{"Process", "ChangeManagement"}, "Process", "process", "Change advisory board")
	createNode("Incident_Mgmt_Process", []string{"Process", "IncidentManagement"}, "Process", "process", "Incident management process")
	createNode("Vendor_Access_Control", []string{"Process", "VendorManagement"}, "Process", "process", "Vendor remote access governance")
	createNode("Patch_Management", []string{"Process", "PatchManagement"}, "Process", "process", "Patch management process")
	createNode("Capacity_Planning", []string{"Process", "CapacityPlanning"}, "Process", "process", "Capacity planning process")
	createNode("Disaster_Recovery", []string{"Process", "DisasterRecovery"}, "Process", "process", "Disaster recovery process")
	createNode("Regulatory_Compliance", []string{"Process", "Compliance"}, "Process", "process", "ACMA/critical infrastructure obligations")
	createNode("SLA_Management", []string{"Process", "SLA"}, "Process", "process", "Service level agreement monitoring")

	// External sector nodes
	createNode("Bank_ATM_Network", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Bank_Branch_WAN", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Bank_Trading_Floor", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Bank_SWIFT_Gateway", []string{"External", "Banking"}, "Sector_Banking", "external", "Banking infrastructure")
	createNode("Triple_Zero_Centre", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("CAD_System", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Police_Radio_GW", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Fire_Dispatch", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Ambulance_Dispatch", []string{"External", "Emergency"}, "Sector_Emergency", "external", "Emergency services")
	createNode("Hospital_Network", []string{"External", "Healthcare"}, "Sector_Healthcare", "external", "Healthcare infrastructure")
	createNode("Telehealth_Platform", []string{"External", "Healthcare"}, "Sector_Healthcare", "external", "Healthcare infrastructure")
	createNode("Pathology_WAN", []string{"External", "Healthcare"}, "Sector_Healthcare", "external", "Healthcare infrastructure")
	createNode("Rail_SCADA_Comms", []string{"External", "Transport"}, "Sector_Transport", "external", "Transport infrastructure")
	createNode("Traffic_Mgmt_System", []string{"External", "Transport"}, "Sector_Transport", "external", "Transport infrastructure")
	createNode("Port_Operations", []string{"External", "Transport"}, "Sector_Transport", "external", "Transport infrastructure")
	createNode("Grid_SCADA_Comms", []string{"External", "Energy"}, "Sector_Energy", "external", "Energy infrastructure")
	createNode("Gas_Pipeline_Comms", []string{"External", "Energy"}, "Sector_Energy", "external", "Energy infrastructure")
	createNode("Substation_Comms", []string{"External", "Energy"}, "Sector_Energy", "external", "Energy infrastructure")

	// Now create edges, skipping any that involve Senior_Network_Eng
	allEdges := [][2]string{
		// backbone
		{"Core_Router_SYD", "DWDM_SYD_MEL"},
		{"Core_Router_SYD", "DWDM_SYD_BNE"},
		{"Core_Router_SYD", "Core_Router_MEL"},
		{"Core_Router_SYD", "Core_Router_BNE"},
		{"Core_Router_MEL", "DWDM_SYD_MEL"},
		{"Core_Router_MEL", "DWDM_MEL_ADL"},
		{"Core_Router_BNE", "DWDM_SYD_BNE"},
		{"Core_Router_PER", "DWDM_ADL_PER"},
		{"Core_Router_ADL", "DWDM_MEL_ADL"},
		{"Core_Router_ADL", "DWDM_ADL_PER"},
		// mobile_core
		{"Core_Router_SYD", "PGW_Primary"},
		{"Core_Router_SYD", "MME_Primary"},
		{"Core_Router_MEL", "PGW_Secondary"},
		{"Core_Router_MEL", "MME_Secondary"},
		{"MME_Primary", "SGW_Primary"},
		{"MME_Primary", "HSS_Primary"},
		{"MME_Secondary", "SGW_Secondary"},
		{"MME_Secondary", "HSS_Primary"},
		{"SGW_Primary", "PGW_Primary"},
		{"SGW_Primary", "IMS_Core"},
		{"SGW_Secondary", "PGW_Secondary"},
		{"IMS_Core", "SIP_Trunk_Emergency"},
		// access
		{"Core_Router_SYD", "Exchange_CBD"},
		{"Core_Router_SYD", "Exchange_North"},
		{"Core_Router_SYD", "Exchange_South"},
		{"Core_Router_SYD", "Exchange_West"},
		{"Core_Router_SYD", "Exchange_Industrial"},
		{"MME_Primary", "Cell_CBD_1"},
		{"MME_Primary", "Cell_CBD_2"},
		{"MME_Primary", "Cell_CBD_3"},
		{"MME_Primary", "Cell_Suburban_1"},
		{"MME_Primary", "Cell_Hospital"},
		{"MME_Primary", "Cell_Emergency_HQ"},
		{"MME_Secondary", "Cell_Suburban_2"},
		{"MME_Secondary", "Cell_Regional_1"},
		{"MME_Secondary", "Cell_Regional_2"},
		{"Exchange_CBD", "Cell_CBD_1"},
		{"Exchange_CBD", "Cell_CBD_2"},
		{"Exchange_CBD", "Cell_CBD_3"},
		{"Exchange_CBD", "Cell_Emergency_HQ"},
		{"Exchange_CBD", "FDH_CBD"},
		{"Exchange_CBD", "FDH_Gov_Precinct"},
		{"Exchange_North", "Cell_Suburban_1"},
		{"Exchange_North", "FDH_Business_Park"},
		{"Exchange_South", "Cell_Suburban_2"},
		{"Exchange_South", "Cell_Hospital"},
		{"Exchange_South", "FDH_Hospital"},
		{"Exchange_West", "Cell_Regional_1"},
		{"Exchange_West", "Cell_Regional_2"},
		{"Exchange_Industrial", "FDH_Data_Centre"},
		// management
		{"Core_Router_SYD", "NMS_Primary"},
		{"Core_Router_SYD", "Performance_Monitor"},
		{"NMS_Primary", "NMS_Secondary"},
		{"NMS_Primary", "NOC_Dashboard"},
		{"NMS_Primary", "Fault_Mgmt_System"},
		{"NMS_Primary", "Performance_Monitor"},
		{"NMS_Primary", "SIEM_Telecom"},
		{"NMS_Secondary", "NOC_Dashboard"},
		{"SIEM_Telecom", "Corp_Firewall"},
		{"SIEM_Telecom", "AAA_Server"},
		{"SIEM_Telecom", "Security_Manager"},
		{"Fault_Mgmt_System", "NOC_Dashboard"},
		{"Fault_Mgmt_System", "Ticketing_System"},
		{"NOC_Dashboard", "Ticketing_System"},
		{"Ticketing_System", "Workforce_Mgmt"},
		{"Corp_Email", "CTO"},
		{"Corp_ERP", "CTO"},
		{"NOC_Shift_Lead", "NOC_Manager"},
		{"Security_Manager", "CTO"},
		{"Security_Manager", "Regulatory_Compliance"},
		{"Security_Manager", "Vendor_Access_Control"},
		{"Security_Manager", "Patch_Management"},
		{"CTO", "Network_Director"},
		{"CTO", "NOC_Manager"},
		{"CTO", "Change_Advisory_Board"},
		{"CTO", "Regulatory_Compliance"},
		{"NOC_Manager", "Network_Director"},
		{"NOC_Manager", "Incident_Mgmt_Process"},
		{"NOC_Manager", "SLA_Management"},
		{"Network_Director", "Change_Advisory_Board"},
		{"Network_Director", "Capacity_Planning"},
		{"Network_Director", "Disaster_Recovery"},
		// bss_oss
		{"Core_Router_SYD", "DNS_Primary"},
		{"Core_Router_SYD", "AAA_Server"},
		{"Core_Router_SYD", "RADIUS_Server"},
		{"Core_Router_MEL", "DNS_Secondary"},
		{"NMS_Primary", "Provisioning_System"},
		{"Billing_System", "CRM_System"},
		{"Billing_System", "Provisioning_System"},
		{"CRM_System", "Corp_ERP"},
		{"Provisioning_System", "Inventory_System"},
		{"Inventory_System", "Workforce_Mgmt"},
		{"DNS_Primary", "DNS_Secondary"},
		{"AAA_Server", "RADIUS_Server"},
		// corporate
		{"Core_Router_SYD", "Corp_Firewall"},
		{"Corp_Firewall", "IT_Switch_Core"},
		{"Corp_Firewall", "Corp_VPN"},
		{"Corp_AD", "IT_Switch_Core"},
		{"Corp_Email", "IT_Switch_Core"},
		{"Corp_ERP", "IT_Switch_Core"},
		// interconnection
		{"Core_Router_SYD", "IX_Peering"},
		{"Core_Router_SYD", "Gateway_Internet"},
		{"Core_Router_SYD", "Gateway_Banking"},
		{"Core_Router_SYD", "Gateway_Emergency"},
		{"Core_Router_SYD", "Gateway_Healthcare"},
		{"Core_Router_SYD", "Gateway_Transport"},
		{"Core_Router_SYD", "Gateway_Energy"},
		{"Core_Router_MEL", "IX_Peering"},
		{"Exchange_Industrial", "Gateway_Transport"},
		{"Exchange_Industrial", "Gateway_Energy"},
		{"FDH_CBD", "Gateway_Banking"},
		{"FDH_Business_Park", "Gateway_Banking"},
		{"FDH_Hospital", "Gateway_Healthcare"},
		{"FDH_Gov_Precinct", "Gateway_Emergency"},
		{"IX_Peering", "Gateway_Internet"},
		{"Gateway_Emergency", "SIP_Trunk_Emergency"},
		// sector_dependency
		{"FDH_Hospital", "Hospital_Network"},
		{"Gateway_Banking", "Bank_ATM_Network"},
		{"Gateway_Banking", "Bank_Branch_WAN"},
		{"Gateway_Banking", "Bank_Trading_Floor"},
		{"Gateway_Banking", "Bank_SWIFT_Gateway"},
		{"Gateway_Emergency", "Triple_Zero_Centre"},
		{"Gateway_Emergency", "CAD_System"},
		{"Gateway_Emergency", "Police_Radio_GW"},
		{"Gateway_Healthcare", "Hospital_Network"},
		{"Gateway_Healthcare", "Telehealth_Platform"},
		{"Gateway_Healthcare", "Pathology_WAN"},
		{"Gateway_Transport", "Rail_SCADA_Comms"},
		{"Gateway_Transport", "Traffic_Mgmt_System"},
		{"Gateway_Transport", "Port_Operations"},
		{"Gateway_Energy", "Grid_SCADA_Comms"},
		{"Gateway_Energy", "Gas_Pipeline_Comms"},
		{"Gateway_Energy", "Substation_Comms"},
		{"SIP_Trunk_Emergency", "Triple_Zero_Centre"},
		{"CAD_System", "Fire_Dispatch"},
		{"CAD_System", "Ambulance_Dispatch"},
		// human_access (excluding Senior_Network_Eng edges)
		{"Core_Router_SYD", "IP_Engineer"},
		{"Core_Router_MEL", "IP_Engineer"},
		{"Core_Router_BNE", "IP_Engineer"},
		{"Core_Router_PER", "IP_Engineer"},
		{"Core_Router_ADL", "IP_Engineer"},
		{"DWDM_SYD_MEL", "Optical_Engineer"},
		{"DWDM_SYD_BNE", "Optical_Engineer"},
		{"DWDM_MEL_ADL", "Optical_Engineer"},
		{"DWDM_ADL_PER", "Optical_Engineer"},
		{"MME_Primary", "Mobile_Engineer"},
		{"MME_Secondary", "Mobile_Engineer"},
		{"SGW_Primary", "Mobile_Engineer"},
		{"SGW_Secondary", "Mobile_Engineer"},
		{"PGW_Primary", "Mobile_Engineer"},
		{"HSS_Primary", "Mobile_Engineer"},
		{"IMS_Core", "Mobile_Engineer"},
		{"Exchange_CBD", "Field_Tech_1"},
		{"Exchange_North", "Field_Tech_1"},
		{"Exchange_South", "Field_Tech_2"},
		{"Exchange_West", "Field_Tech_2"},
		{"Cell_CBD_1", "Field_Tech_1"},
		{"Cell_CBD_2", "Field_Tech_1"},
		{"Cell_Suburban_1", "Field_Tech_1"},
		{"Cell_Suburban_2", "Field_Tech_2"},
		{"Cell_Regional_1", "Field_Tech_2"},
		{"Cell_Regional_2", "Field_Tech_2"},
		{"NMS_Primary", "NOC_Shift_Lead"},
		{"SIEM_Telecom", "NOC_Security_Analyst"},
		{"Fault_Mgmt_System", "NOC_Shift_Lead"},
		{"Fault_Mgmt_System", "NOC_Operator_1"},
		{"Fault_Mgmt_System", "NOC_Operator_2"},
		{"NOC_Dashboard", "NOC_Shift_Lead"},
		{"NOC_Dashboard", "NOC_Operator_1"},
		{"NOC_Dashboard", "NOC_Operator_2"},
		{"NOC_Dashboard", "NOC_Security_Analyst"},
		{"Ticketing_System", "NOC_Shift_Lead"},
		{"Ticketing_System", "NOC_Operator_1"},
		{"Ticketing_System", "NOC_Operator_2"},
		{"Billing_System", "BSS_Admin"},
		{"CRM_System", "BSS_Admin"},
		{"Provisioning_System", "BSS_Admin"},
		{"Inventory_System", "BSS_Admin"},
		{"Workforce_Mgmt", "Field_Tech_1"},
		{"Workforce_Mgmt", "Field_Tech_2"},
		{"AAA_Server", "IP_Engineer"},
		{"Corp_Firewall", "IT_Admin_Corp"},
		{"Corp_AD", "IT_Admin_Corp"},
		{"Corp_Email", "IT_Admin_Corp"},
		{"Corp_VPN", "IT_Admin_Corp"},
		{"IT_Switch_Core", "IT_Admin_Corp"},
		{"NOC_Shift_Lead", "NOC_Operator_1"},
		{"NOC_Shift_Lead", "NOC_Operator_2"},
		{"NOC_Shift_Lead", "NOC_Security_Analyst"},
		{"NOC_Shift_Lead", "Incident_Mgmt_Process"},
		{"NOC_Security_Analyst", "Security_Manager"},
		{"NOC_Security_Analyst", "Incident_Mgmt_Process"},
		{"IP_Engineer", "Change_Advisory_Board"},
		{"Mobile_Engineer", "Change_Advisory_Board"},
		{"Optical_Engineer", "Change_Advisory_Board"},
		{"IT_Admin_Corp", "Patch_Management"},
		{"BSS_Admin", "SLA_Management"},
		{"BSS_Admin", "Patch_Management"},
		// vendor_access
		{"Core_Router_SYD", "Vendor_Cisco"},
		{"Core_Router_MEL", "Vendor_Cisco"},
		{"DWDM_SYD_MEL", "Vendor_Nokia"},
		{"DWDM_SYD_BNE", "Vendor_Nokia"},
		{"MME_Primary", "Vendor_Ericsson"},
		{"MME_Secondary", "Vendor_Ericsson"},
		{"HSS_Primary", "Vendor_Ericsson"},
		{"SIEM_Telecom", "Managed_SOC_Analyst"},
		{"Corp_VPN", "Vendor_Ericsson"},
		{"Corp_VPN", "Vendor_Cisco"},
		{"Corp_VPN", "Vendor_Nokia"},
		{"Corp_VPN", "Managed_SOC_Analyst"},
		{"Security_Manager", "Managed_SOC_Analyst"},
		{"Vendor_Ericsson", "Vendor_Access_Control"},
		{"Vendor_Cisco", "Vendor_Access_Control"},
		{"Vendor_Nokia", "Vendor_Access_Control"},
		{"Managed_SOC_Analyst", "Vendor_Access_Control"},
		// liaison
		{"CRM_System", "Banking_Liaison"},
		{"Gateway_Banking", "Banking_Liaison"},
		{"Gateway_Emergency", "Emergency_Liaison"},
		{"SIP_Trunk_Emergency", "Emergency_Liaison"},
		{"Banking_Liaison", "SLA_Management"},
		{"Banking_Liaison", "Incident_Mgmt_Process"},
		{"Emergency_Liaison", "Incident_Mgmt_Process"},
		{"Emergency_Liaison", "Regulatory_Compliance"},
	}

	props := map[string]storage.Value{}
	for _, edge := range allEdges {
		// Skip edges involving Senior_Network_Eng
		if edge[0] == "Senior_Network_Eng" || edge[1] == "Senior_Network_Eng" {
			continue
		}
		fromID := newMeta.NodeIDs[edge[0]]
		toID := newMeta.NodeIDs[edge[1]]
		if fromID == 0 || toID == 0 {
			continue // Node doesn't exist in this graph
		}
		if err := createUndirectedEdge(graph, fromID, toID, "EDGE", props); err != nil {
			return nil, fmt.Errorf("failed to create edge %s <-> %s: %w", edge[0], edge[1], err)
		}
	}

	return newMeta, nil
}

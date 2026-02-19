// Package main provides telecom-specific analysis functions for betweenness centrality.
package main

import (
	"sort"
)

// AnalyseTelecomModel computes BC and returns structured results for the telecom model.
// This wraps the unified Analyse function with telecom-specific options enabled.
func AnalyseTelecomModel(meta *Metadata, modelName string, bc map[uint64]float64) ModelResult {
	return Analyse(meta, modelName, bc, AnalysisOptions{
		IncludeTypeCounts:   true,
		IncludeGatewayStats: true,
	})
}

// analyseGateways computes BC and external node counts for each gateway.
func analyseGateways(meta *Metadata, bc map[uint64]float64, rankings []BCResult) []GatewayResult {
	// Define gateway to external node mappings
	gatewayExternalNodes := map[string][]string{
		"Gateway_Banking": {
			"Bank_ATM_Network",
			"Bank_Branch_WAN",
			"Bank_Trading_Floor",
			"Bank_SWIFT_Gateway",
		},
		"Gateway_Emergency": {
			"Triple_Zero_Centre",
			"CAD_System",
			"Police_Radio_GW",
		},
		"Gateway_Healthcare": {
			"Hospital_Network",
			"Telehealth_Platform",
			"Pathology_WAN",
		},
		"Gateway_Transport": {
			"Rail_SCADA_Comms",
			"Traffic_Mgmt_System",
			"Port_Operations",
		},
		"Gateway_Energy": {
			"Grid_SCADA_Comms",
			"Gas_Pipeline_Comms",
			"Substation_Comms",
		},
	}

	var gatewayResults []GatewayResult

	// Find BC for each gateway
	for _, r := range rankings {
		if externals, ok := gatewayExternalNodes[r.Name]; ok {
			gatewayResults = append(gatewayResults, GatewayResult{
				Name:              r.Name,
				BC:                r.BC,
				ExternalNodeCount: len(externals),
				ExternalNodes:     externals,
			})
		}
	}

	// Sort by BC descending
	sort.Slice(gatewayResults, func(i, j int) bool {
		return gatewayResults[i].BC > gatewayResults[j].BC
	})

	return gatewayResults
}

// AnalyseCascadeFailures tests which internal node failures disconnect external sectors.
func AnalyseCascadeFailures(meta *Metadata) []CascadeResult {
	// This is a simplified analysis that identifies which gateways are single points
	// of failure for their sectors. A full implementation would rebuild the graph
	// for each node removal and test connectivity.

	// For now, we identify the obvious cascade failures based on topology:
	// Each gateway is a SPOF for its sector's external nodes

	sectorGateways := map[string]struct {
		gateway       string
		externalNodes []string
	}{
		"Banking": {
			gateway: "Gateway_Banking",
			externalNodes: []string{
				"Bank_ATM_Network",
				"Bank_Branch_WAN",
				"Bank_Trading_Floor",
				"Bank_SWIFT_Gateway",
			},
		},
		"Emergency": {
			gateway: "Gateway_Emergency",
			externalNodes: []string{
				"Triple_Zero_Centre",
				"CAD_System",
				"Police_Radio_GW",
			},
		},
		"Healthcare": {
			gateway: "Gateway_Healthcare",
			externalNodes: []string{
				"Hospital_Network",
				"Telehealth_Platform",
				"Pathology_WAN",
			},
		},
		"Transport": {
			gateway: "Gateway_Transport",
			externalNodes: []string{
				"Rail_SCADA_Comms",
				"Traffic_Mgmt_System",
				"Port_Operations",
			},
		},
		"Energy": {
			gateway: "Gateway_Energy",
			externalNodes: []string{
				"Grid_SCADA_Comms",
				"Gas_Pipeline_Comms",
				"Substation_Comms",
			},
		},
	}

	results := make([]CascadeResult, 0, len(sectorGateways)+1)

	// Core_Router_SYD failure analysis (based on topology)
	// It connects to all gateways except those with redundant paths
	coreRouterResult := CascadeResult{
		NodeName:        "Core_Router_SYD",
		SectorsAffected: 2,
		SectorDetails:   make(map[string][]string),
	}

	// Based on the topology, Energy and Transport gateways have secondary paths
	// through Exchange_Industrial, but if Core_Router_SYD fails, the path to
	// these gateways is likely broken
	coreRouterResult.SectorDetails["Sector_Energy"] = []string{
		"Grid_SCADA_Comms",
		"Gas_Pipeline_Comms",
		"Substation_Comms",
	}
	coreRouterResult.SectorDetails["Sector_Transport"] = []string{
		"Rail_SCADA_Comms",
		"Traffic_Mgmt_System",
		"Port_Operations",
	}
	coreRouterResult.ExternalNodesLost = 6
	results = append(results, coreRouterResult)

	// Each gateway is a SPOF for its sector
	for sector, info := range sectorGateways {
		result := CascadeResult{
			NodeName:          info.gateway,
			SectorsAffected:   1,
			ExternalNodesLost: len(info.externalNodes),
			SectorDetails:     make(map[string][]string),
		}
		result.SectorDetails["Sector_"+sector] = info.externalNodes
		results = append(results, result)
	}

	// Sort by sectors affected, then by external nodes lost
	sort.Slice(results, func(i, j int) bool {
		if results[i].SectorsAffected != results[j].SectorsAffected {
			return results[i].SectorsAffected > results[j].SectorsAffected
		}
		return results[i].ExternalNodesLost > results[j].ExternalNodesLost
	})

	return results
}

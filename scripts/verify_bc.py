#!/usr/bin/env python3
"""Cross-reference betweenness centrality values using NetworkX.

Builds Steve's Utility model (33 nodes, 70 undirected edges) and computes
normalized betweenness centrality. These values serve as ground truth for
the Go implementation tests.

Usage:
    python3 scripts/verify_bc.py
"""

import networkx as nx


def build_steves_utility() -> nx.Graph:
    """Build Steve's Utility as an undirected graph matching models.go exactly."""
    G = nx.Graph()

    # 33 nodes
    nodes = [
        # Technical (22)
        "PLC_Turbine1", "PLC_Turbine2", "PLC_Substation",
        "RTU_Remote1", "RTU_Remote2",
        "HMI_Control1", "HMI_Control2", "Safety_PLC",
        "SCADA_Server", "Historian_OT", "Eng_Workstation",
        "OT_Switch_Core", "Patch_Server", "AD_Server_OT",
        "Firewall_ITOT", "Jump_Server", "Data_Diode",
        "IT_Switch_Core", "Email_Server", "ERP_System", "AD_Server_IT", "VPN_Gateway",
        # Human (7)
        "Steve", "OT_Manager", "IT_Admin",
        "Control_Op1", "Control_Op2", "Plant_Manager", "Vendor_Rep",
        # Process (4)
        "Change_Mgmt_Process", "Incident_Response",
        "Vendor_Access_Process", "Patch_Approval",
    ]
    G.add_nodes_from(nodes)

    # 70 undirected edges (matching models.go lines 127-243)
    edges = [
        # Technical edges (26)
        ("PLC_Turbine1", "HMI_Control1"),
        ("PLC_Turbine2", "HMI_Control2"),
        ("PLC_Substation", "HMI_Control1"),
        ("RTU_Remote1", "SCADA_Server"),
        ("RTU_Remote2", "SCADA_Server"),
        ("Safety_PLC", "HMI_Control1"),
        ("Safety_PLC", "HMI_Control2"),
        ("HMI_Control1", "SCADA_Server"),
        ("HMI_Control2", "SCADA_Server"),
        ("SCADA_Server", "Historian_OT"),
        ("SCADA_Server", "Eng_Workstation"),
        ("SCADA_Server", "OT_Switch_Core"),
        ("Historian_OT", "OT_Switch_Core"),
        ("Eng_Workstation", "OT_Switch_Core"),
        ("OT_Switch_Core", "Patch_Server"),
        ("OT_Switch_Core", "AD_Server_OT"),
        ("OT_Switch_Core", "Firewall_ITOT"),
        ("Firewall_ITOT", "Jump_Server"),
        ("Firewall_ITOT", "Data_Diode"),
        ("Data_Diode", "Historian_OT"),
        ("Firewall_ITOT", "IT_Switch_Core"),
        ("Jump_Server", "IT_Switch_Core"),
        ("IT_Switch_Core", "Email_Server"),
        ("IT_Switch_Core", "ERP_System"),
        ("IT_Switch_Core", "AD_Server_IT"),
        ("IT_Switch_Core", "VPN_Gateway"),
        # Steve's edges (23)
        ("Steve", "PLC_Turbine1"),
        ("Steve", "PLC_Turbine2"),
        ("Steve", "PLC_Substation"),
        ("Steve", "HMI_Control1"),
        ("Steve", "HMI_Control2"),
        ("Steve", "SCADA_Server"),
        ("Steve", "Eng_Workstation"),
        ("Steve", "Historian_OT"),
        ("Steve", "OT_Switch_Core"),
        ("Steve", "Patch_Server"),
        ("Steve", "Jump_Server"),
        ("Steve", "Firewall_ITOT"),
        ("Steve", "VPN_Gateway"),
        ("Steve", "AD_Server_OT"),
        ("Steve", "Change_Mgmt_Process"),
        ("Steve", "Incident_Response"),
        ("Steve", "Vendor_Access_Process"),
        ("Steve", "Patch_Approval"),
        ("Steve", "Vendor_Rep"),
        ("Steve", "OT_Manager"),
        ("Steve", "Control_Op1"),
        ("Steve", "Control_Op2"),
        ("Steve", "IT_Admin"),
        # Other human edges (21)
        ("Control_Op1", "HMI_Control1"),
        ("Control_Op1", "HMI_Control2"),
        ("Control_Op1", "Incident_Response"),
        ("Control_Op2", "HMI_Control1"),
        ("Control_Op2", "HMI_Control2"),
        ("Control_Op2", "Incident_Response"),
        ("OT_Manager", "SCADA_Server"),
        ("OT_Manager", "Change_Mgmt_Process"),
        ("OT_Manager", "Patch_Approval"),
        ("OT_Manager", "Plant_Manager"),
        ("IT_Admin", "IT_Switch_Core"),
        ("IT_Admin", "Email_Server"),
        ("IT_Admin", "ERP_System"),
        ("IT_Admin", "AD_Server_IT"),
        ("IT_Admin", "VPN_Gateway"),
        ("IT_Admin", "Firewall_ITOT"),
        ("Plant_Manager", "ERP_System"),
        ("Plant_Manager", "Email_Server"),
        ("Vendor_Rep", "VPN_Gateway"),
        ("Vendor_Rep", "Jump_Server"),
        ("Vendor_Rep", "Vendor_Access_Process"),
    ]
    G.add_edges_from(edges)

    return G


def main():
    G = build_steves_utility()

    print(f"Nodes: {G.number_of_nodes()}")
    print(f"Edges: {G.number_of_edges()}")
    print()

    bc = nx.betweenness_centrality(G, normalized=True)

    # Sort by BC descending
    sorted_bc = sorted(bc.items(), key=lambda x: x[1], reverse=True)

    print(f"{'Node':<25} {'BC':>8}")
    print("-" * 35)
    for node, value in sorted_bc:
        print(f"{node:<25} {value:>8.4f}")

    # Print Go test-ready format
    print()
    print("// Go test assertions (copy-paste ready):")
    print("expected := map[string]float64{")
    for node, value in sorted_bc:
        print(f'\t"{node}": {value:.4f},')
    print("}")


if __name__ == "__main__":
    main()

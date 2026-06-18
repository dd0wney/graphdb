package algorithms

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Edge Betweenness Centrality tests
// ---------------------------------------------------------------------------

// TestEdgeBetweennessCentrality_EmptyGraph verifies edge BC returns an empty
// result when the graph has no nodes.
func TestEdgeBetweennessCentrality_EmptyGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	result, err := EdgeBetweennessCentrality(gs)
	if err != nil {
		t.Fatalf("EdgeBetweennessCentrality failed: %v", err)
	}

	if len(result.ByEdgeID) != 0 {
		t.Errorf("Expected 0 edge scores for empty graph, got %d", len(result.ByEdgeID))
	}
	if len(result.ByNodePair) != 0 {
		t.Errorf("Expected 0 node-pair scores for empty graph, got %d", len(result.ByNodePair))
	}
	if len(result.TopEdges) != 0 {
		t.Errorf("Expected 0 top edges for empty graph, got %d", len(result.TopEdges))
	}
}

// TestEdgeBetweennessCentrality_SingleNode verifies edge BC on a single node
// with no edges.
func TestEdgeBetweennessCentrality_SingleNode(t *testing.T) {
	gs := setupCentralityTestGraph(t)
	_, _ = gs.CreateNode([]string{"Node"}, nil)

	result, err := EdgeBetweennessCentrality(gs)
	if err != nil {
		t.Fatalf("EdgeBetweennessCentrality failed: %v", err)
	}

	if len(result.ByEdgeID) != 0 {
		t.Errorf("Expected 0 edge scores for single-node graph, got %d", len(result.ByEdgeID))
	}
}

// TestEdgeBetweennessCentrality_ExactValues verifies edge BC against analytically
// derived values for known topologies. All graphs use bidirectional edges to match
// the real usage pattern. Expected values from NetworkX edge_betweenness_centrality
// on a DiGraph with normalized=True (factor: 1/(n*(n-1))).
func TestEdgeBetweennessCentrality_ExactValues(t *testing.T) {
	const epsilon = 0.0001

	type edgeExpectation struct {
		from     int
		to       int
		expected float64
	}

	tests := []struct {
		name  string
		nodes int
		edges [][2]int
		want  []edgeExpectation
	}{
		{
			name:  "Path_A-B-C",
			nodes: 3,
			edges: [][2]int{{0, 1}, {1, 2}},
			// All 4 directed edges carry equal flow: BC = 1/3
			want: []edgeExpectation{
				{0, 1, 0.3333},
				{1, 0, 0.3333},
				{1, 2, 0.3333},
				{2, 1, 0.3333},
			},
		},
		{
			name:  "Star_H-ABCD",
			nodes: 5,
			edges: [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}},
			// All hub edges carry equal flow: BC = 1/5
			want: []edgeExpectation{
				{0, 1, 0.2},
				{1, 0, 0.2},
				{0, 2, 0.2},
				{2, 0, 0.2},
				{0, 3, 0.2},
				{3, 0, 0.2},
				{0, 4, 0.2},
				{4, 0, 0.2},
			},
		},
		{
			name:  "Diamond_A-BC-D",
			nodes: 4,
			edges: [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}},
			// All edges carry equal flow due to symmetric parallel paths
			want: []edgeExpectation{
				{0, 1, 0.1667},
				{1, 0, 0.1667},
				{0, 2, 0.1667},
				{2, 0, 0.1667},
				{1, 3, 0.1667},
				{3, 1, 0.1667},
				{2, 3, 0.1667},
				{3, 2, 0.1667},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := setupCentralityTestGraph(t)

			nodeIDs := make([]uint64, tt.nodes)
			for i := 0; i < tt.nodes; i++ {
				node, err := gs.CreateNode([]string{"Node"}, nil)
				if err != nil {
					t.Fatalf("Failed to create node %d: %v", i, err)
				}
				nodeIDs[i] = node.ID
			}

			// Track edge IDs for lookup: edgesByPair[[from_idx, to_idx]] = edgeID
			type pair struct{ from, to int }
			edgesByPair := make(map[pair]uint64)

			for _, e := range tt.edges {
				fwd, err := gs.CreateEdge(nodeIDs[e[0]], nodeIDs[e[1]], "LINKS", nil, 1.0)
				if err != nil {
					t.Fatalf("Failed to create edge %d->%d: %v", e[0], e[1], err)
				}
				edgesByPair[pair{e[0], e[1]}] = fwd.ID

				rev, err := gs.CreateEdge(nodeIDs[e[1]], nodeIDs[e[0]], "LINKS", nil, 1.0)
				if err != nil {
					t.Fatalf("Failed to create edge %d->%d: %v", e[1], e[0], err)
				}
				edgesByPair[pair{e[1], e[0]}] = rev.ID
			}

			result, err := EdgeBetweennessCentrality(gs)
			if err != nil {
				t.Fatalf("EdgeBetweennessCentrality failed: %v", err)
			}

			for _, w := range tt.want {
				edgeID := edgesByPair[pair{w.from, w.to}]
				got := result.ByEdgeID[edgeID]
				if math.Abs(got-w.expected) > epsilon {
					t.Errorf("edge %d->%d: got BC %.6f, want %.4f (delta %.6f)",
						w.from, w.to, got, w.expected, math.Abs(got-w.expected))
				}

				// Verify ByNodePair agrees
				gotPair := result.ByNodePair[[2]uint64{nodeIDs[w.from], nodeIDs[w.to]}]
				if math.Abs(gotPair-w.expected) > epsilon {
					t.Errorf("ByNodePair [%d->%d]: got BC %.6f, want %.4f",
						w.from, w.to, gotPair, w.expected)
				}
			}
		})
	}
}

// TestEdgeBetweennessCentrality_TopEdgesOrdering verifies that TopEdges is
// sorted descending by score.
func TestEdgeBetweennessCentrality_TopEdgesOrdering(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Path graph: A-B-C-D-E (bidirectional)
	nodes := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		n, _ := gs.CreateNode([]string{"Node"}, nil)
		nodes[i] = n.ID
	}
	for i := 0; i < 4; i++ {
		_, _ = gs.CreateEdge(nodes[i], nodes[i+1], "LINKS", nil, 1.0)
		_, _ = gs.CreateEdge(nodes[i+1], nodes[i], "LINKS", nil, 1.0)
	}

	result, err := EdgeBetweennessCentrality(gs)
	if err != nil {
		t.Fatalf("EdgeBetweennessCentrality failed: %v", err)
	}

	if len(result.TopEdges) == 0 {
		t.Fatal("Expected non-empty TopEdges")
	}

	for i := 1; i < len(result.TopEdges); i++ {
		if result.TopEdges[i].Score > result.TopEdges[i-1].Score {
			t.Errorf("TopEdges not sorted descending at index %d: %.6f > %.6f",
				i, result.TopEdges[i].Score, result.TopEdges[i-1].Score)
		}
	}
}

// TestEdgeBetweennessCentrality_StevesUtility reconstructs the full 33-node,
// 70-undirected-edge model and verifies edge BC values against NetworkX ground
// truth (nx.edge_betweenness_centrality on DiGraph, normalized=True).
func TestEdgeBetweennessCentrality_StevesUtility(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeNames := []string{
		"PLC_Turbine1", "PLC_Turbine2", "PLC_Substation",
		"RTU_Remote1", "RTU_Remote2",
		"HMI_Control1", "HMI_Control2", "Safety_PLC",
		"SCADA_Server", "Historian_OT", "Eng_Workstation",
		"OT_Switch_Core", "Patch_Server", "AD_Server_OT",
		"Firewall_ITOT", "Jump_Server", "Data_Diode",
		"IT_Switch_Core", "Email_Server", "ERP_System", "AD_Server_IT", "VPN_Gateway",
		"Steve", "OT_Manager", "IT_Admin",
		"Control_Op1", "Control_Op2", "Plant_Manager", "Vendor_Rep",
		"Change_Mgmt_Process", "Incident_Response",
		"Vendor_Access_Process", "Patch_Approval",
	}

	nameToID := make(map[string]uint64)
	for _, name := range nodeNames {
		node, err := gs.CreateNode([]string{"Node"}, nil)
		if err != nil {
			t.Fatalf("Failed to create node %s: %v", name, err)
		}
		nameToID[name] = node.ID
	}

	edges := [][2]string{
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

	if len(nodeNames) != 33 {
		t.Fatalf("Expected 33 nodes, got %d", len(nodeNames))
	}
	if len(edges) != 70 {
		t.Fatalf("Expected 70 undirected edges, got %d", len(edges))
	}

	for _, edge := range edges {
		fromID := nameToID[edge[0]]
		toID := nameToID[edge[1]]
		if _, err := gs.CreateEdge(fromID, toID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("Failed to create edge %s->%s: %v", edge[0], edge[1], err)
		}
		if _, err := gs.CreateEdge(toID, fromID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("Failed to create edge %s->%s: %v", edge[1], edge[0], err)
		}
	}

	result, err := EdgeBetweennessCentrality(gs)
	if err != nil {
		t.Fatalf("EdgeBetweennessCentrality failed: %v", err)
	}

	// NetworkX ground truth (DiGraph, normalized=True)
	// Generated by: python3 /tmp/verify_edge_bc.py
	//
	// We verify a representative subset: top edges, leaf edges, and non-Steve
	// edges to confirm the full Brandes accumulation is correct.
	type edgeCase struct {
		from, to string
		expected float64
	}
	expected := []edgeCase{
		// Top edges (Steve's connections)
		{"Steve", "IT_Admin", 0.0803},
		{"IT_Admin", "Steve", 0.0803},
		{"Steve", "SCADA_Server", 0.0411},
		{"Steve", "OT_Manager", 0.0335},
		{"Steve", "Firewall_ITOT", 0.0292},
		{"Steve", "HMI_Control2", 0.0284},
		{"Steve", "Vendor_Access_Process", 0.0281},
		{"Steve", "Historian_OT", 0.0280},
		{"Steve", "HMI_Control1", 0.0274},

		// Non-Steve edges
		{"RTU_Remote1", "SCADA_Server", 0.0303},
		{"SCADA_Server", "RTU_Remote1", 0.0303},
		{"OT_Manager", "Plant_Manager", 0.0298},
		{"Plant_Manager", "OT_Manager", 0.0298},
		{"Firewall_ITOT", "Data_Diode", 0.0169},
		{"Data_Diode", "Historian_OT", 0.0143},
		{"Firewall_ITOT", "IT_Switch_Core", 0.0137},
		{"IT_Admin", "AD_Server_IT", 0.0248},

		// Leaf edges with lower BC
		{"PLC_Turbine1", "HMI_Control1", 0.0047},
		{"Vendor_Rep", "Vendor_Access_Process", 0.0022},
		{"Control_Op1", "Incident_Response", 0.0021},
	}

	const epsilon = 0.001
	for _, ec := range expected {
		fromID := nameToID[ec.from]
		toID := nameToID[ec.to]
		key := [2]uint64{fromID, toID}
		got, ok := result.ByNodePair[key]
		if !ok {
			t.Errorf("Missing edge %s -> %s in ByNodePair", ec.from, ec.to)
			continue
		}
		if math.Abs(got-ec.expected) > epsilon {
			t.Errorf("%-25s -> %-25s got BC %.6f, want %.4f (delta %.6f)",
				ec.from, ec.to, got, ec.expected, math.Abs(got-ec.expected))
		}
	}

	// Verify symmetry: both directed edges in each pair have equal BC
	for _, edge := range edges {
		fromID := nameToID[edge[0]]
		toID := nameToID[edge[1]]
		fwd := result.ByNodePair[[2]uint64{fromID, toID}]
		rev := result.ByNodePair[[2]uint64{toID, fromID}]
		if math.Abs(fwd-rev) > 0.0001 {
			t.Errorf("Asymmetric edge BC: %s->%s=%.6f, %s->%s=%.6f",
				edge[0], edge[1], fwd, edge[1], edge[0], rev)
		}
	}

	// Verify all edge BC values are non-negative
	for edgeID, score := range result.ByEdgeID {
		if score < 0 {
			t.Errorf("Negative edge BC for edge %d: %f", edgeID, score)
		}
	}

	// Verify the top edge is Steve -> IT_Admin
	if len(result.TopEdges) == 0 {
		t.Fatal("Expected non-empty TopEdges")
	}
	topFrom := result.TopEdges[0].FromNodeID
	topTo := result.TopEdges[0].ToNodeID
	steveID := nameToID["Steve"]
	itAdminID := nameToID["IT_Admin"]
	matchesSteveITAdmin := (topFrom == steveID && topTo == itAdminID) ||
		(topFrom == itAdminID && topTo == steveID)
	if !matchesSteveITAdmin {
		t.Errorf("Expected top edge to be Steve<->IT_Admin, got %d->%d", topFrom, topTo)
	}
}

// TestComputeAllCentrality_IncludesEdgeBetweenness verifies that
// ComputeAllCentrality now populates edge betweenness results.
func TestComputeAllCentrality_IncludesEdgeBetweenness(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	_, _ = gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	_, _ = gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	_, _ = gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	_, _ = gs.CreateEdge(nodeC.ID, nodeB.ID, "LINKS", nil, 1.0)

	result, err := ComputeAllCentrality(gs)
	if err != nil {
		t.Fatalf("ComputeAllCentrality failed: %v", err)
	}

	if result.EdgeBetweenness == nil {
		t.Fatal("Expected EdgeBetweenness to be populated")
	}

	if len(result.EdgeBetweenness.ByEdgeID) != 4 {
		t.Errorf("Expected 4 edge BC scores, got %d", len(result.EdgeBetweenness.ByEdgeID))
	}

	if len(result.TopByEdgeBetweenness) == 0 {
		t.Error("Expected TopByEdgeBetweenness to be populated")
	}

	// Verify edge BC values match standalone EdgeBetweennessCentrality
	standalone, err := EdgeBetweennessCentrality(gs)
	if err != nil {
		t.Fatalf("EdgeBetweennessCentrality failed: %v", err)
	}

	for edgeID, combinedScore := range result.EdgeBetweenness.ByEdgeID {
		standaloneScore := standalone.ByEdgeID[edgeID]
		if math.Abs(combinedScore-standaloneScore) > 0.0001 {
			t.Errorf("Edge %d: ComputeAllCentrality=%.6f, standalone=%.6f",
				edgeID, combinedScore, standaloneScore)
		}
	}
}

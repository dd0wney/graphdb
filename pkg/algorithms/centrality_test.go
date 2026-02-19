package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupCentralityTestGraph creates a test graph for centrality tests
func setupCentralityTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "centrality-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Create graph storage
	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}
	t.Cleanup(func() { gs.Close() })

	return gs
}

// TestDegreeCentrality_EmptyGraph tests degree centrality on empty graph
func TestDegreeCentrality_EmptyGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 scores for empty graph, got %d", len(result))
	}
}

// TestDegreeCentrality_SingleNode tests degree centrality on single node
func TestDegreeCentrality_SingleNode(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 score, got %d", len(result))
	}

	// Single node has no connections, so degree should be 0
	degree := result[node.ID]
	if degree != 0.0 {
		t.Errorf("Expected degree 0 for single node, got %f", degree)
	}
}

// TestDegreeCentrality_LinearChain tests degree centrality on A->B->C
func TestDegreeCentrality_LinearChain(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	degreeA := result[nodeA.ID]
	degreeB := result[nodeB.ID]
	degreeC := result[nodeC.ID]

	// Node B has highest degree (1 in + 1 out = 2 total)
	// Nodes A and C have degree 1 each
	if degreeB <= degreeA || degreeB <= degreeC {
		t.Errorf("Expected B degree (%f) > A degree (%f) and C degree (%f)", degreeB, degreeA, degreeC)
	}
}

// TestDegreeCentrality_Star tests degree centrality on star topology
func TestDegreeCentrality_Star(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create star: A <- B, A <- C, A <- D (A is hub)
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	degreeA := result[nodeA.ID]
	degreeB := result[nodeB.ID]

	// Hub should have highest degree (3 incoming edges)
	if degreeA <= degreeB {
		t.Errorf("Expected hub degree (%f) > spoke degree (%f)", degreeA, degreeB)
	}
}

// TestClosenessCentrality_LinearChain tests closeness centrality on A->B->C
func TestClosenessCentrality_LinearChain(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ClosenessCentrality(gs)

	if err != nil {
		t.Fatalf("ClosenessCentrality failed: %v", err)
	}

	closenessA := result[nodeA.ID]
	closenessB := result[nodeB.ID]
	closenessC := result[nodeC.ID]

	// Node B is most central (can reach both A and C quickly)
	// Nodes A and C are at the ends
	if closenessB <= closenessA || closenessB <= closenessC {
		t.Errorf("Expected B closeness (%f) > A closeness (%f) and C closeness (%f)", closenessB, closenessA, closenessC)
	}
}

// TestClosenessCentrality_Star tests closeness centrality on star topology
func TestClosenessCentrality_Star(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create star with bidirectional edges so all nodes can reach hub
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	// Bidirectional edges
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := ClosenessCentrality(gs)

	if err != nil {
		t.Fatalf("ClosenessCentrality failed: %v", err)
	}

	closenessA := result[nodeA.ID]
	closenessB := result[nodeB.ID]

	// Hub should have highest closeness (1-hop to everyone)
	if closenessA <= closenessB {
		t.Errorf("Expected hub closeness (%f) > spoke closeness (%f)", closenessA, closenessB)
	}
}

// TestClosenessCentrality_Isolated tests closeness with unreachable nodes
func TestClosenessCentrality_Isolated(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	// No edges - nodes are isolated

	result, err := ClosenessCentrality(gs)

	if err != nil {
		t.Fatalf("ClosenessCentrality failed: %v", err)
	}

	closenessA := result[nodeA.ID]
	closenessB := result[nodeB.ID]

	// Isolated nodes should have closeness 0
	if closenessA != 0.0 || closenessB != 0.0 {
		t.Errorf("Expected closeness 0 for isolated nodes, got A=%f, B=%f", closenessA, closenessB)
	}
}

// TestBetweennessCentrality_LinearChain tests betweenness on A->B->C
func TestBetweennessCentrality_LinearChain(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	betweennessA := result[nodeA.ID]
	betweennessB := result[nodeB.ID]
	betweennessC := result[nodeC.ID]

	// Node B is on the path from A to C, so it should have highest betweenness
	// Nodes A and C are endpoints, so they have 0 betweenness
	if betweennessB <= betweennessA || betweennessB <= betweennessC {
		t.Errorf("Expected B betweenness (%f) > A (%f) and C (%f)", betweennessB, betweennessA, betweennessC)
	}
}

// TestBetweennessCentrality_Star tests betweenness on star topology
func TestBetweennessCentrality_Star(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create star with bidirectional edges
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	// Bidirectional edges so paths go through hub
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	betweennessA := result[nodeA.ID]
	betweennessB := result[nodeB.ID]

	// Hub should have highest betweenness (all paths between spokes go through it)
	if betweennessA <= betweennessB {
		t.Errorf("Expected hub betweenness (%f) > spoke betweenness (%f)", betweennessA, betweennessB)
	}
}

// TestBetweennessCentrality_Diamond tests betweenness on diamond graph
func TestBetweennessCentrality_Diamond(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create diamond: A -> B -> D, A -> C -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeD.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	betweennessB := result[nodeB.ID]
	betweennessC := result[nodeC.ID]

	// B and C should have equal betweenness (both are on parallel paths)
	if math.Abs(betweennessB-betweennessC) > 0.001 {
		t.Errorf("Expected equal betweenness for B (%f) and C (%f)", betweennessB, betweennessC)
	}
}

// TestBetweennessCentrality_EmptyGraph tests betweenness on empty graph
func TestBetweennessCentrality_EmptyGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 scores for empty graph, got %d", len(result))
	}
}

// TestComputeAllCentrality tests computing all centrality measures
func TestComputeAllCentrality(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create small graph
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ComputeAllCentrality(gs)

	if err != nil {
		t.Fatalf("ComputeAllCentrality failed: %v", err)
	}

	// Verify all three centrality measures are computed
	if len(result.Betweenness) != 3 {
		t.Errorf("Expected 3 betweenness scores, got %d", len(result.Betweenness))
	}

	if len(result.Closeness) != 3 {
		t.Errorf("Expected 3 closeness scores, got %d", len(result.Closeness))
	}

	if len(result.Degree) != 3 {
		t.Errorf("Expected 3 degree scores, got %d", len(result.Degree))
	}

	// Verify TopNodes are populated
	if len(result.TopByBetweenness) == 0 {
		t.Error("Expected TopByBetweenness to be populated")
	}

	if len(result.TopByCloseness) == 0 {
		t.Error("Expected TopByCloseness to be populated")
	}

	if len(result.TopByDegree) == 0 {
		t.Error("Expected TopByDegree to be populated")
	}
}

// TestComputeAllCentrality_ComplexGraph tests all centrality on larger graph
func TestComputeAllCentrality_ComplexGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create more complex graph
	nodes := make([]*storage.Node, 6)
	for i := 0; i < 6; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodes[i] = node
	}

	// Create edges forming interesting topology
	gs.CreateEdge(nodes[0].ID, nodes[1].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[3].ID, nodes[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[4].ID, nodes[5].ID, "LINKS", nil, 1.0)
	// Add shortcuts
	gs.CreateEdge(nodes[0].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[4].ID, "LINKS", nil, 1.0)

	result, err := ComputeAllCentrality(gs)

	if err != nil {
		t.Fatalf("ComputeAllCentrality failed: %v", err)
	}

	// Node 2 should be important (central position with shortcuts)
	degree2 := result.Degree[nodes[2].ID]

	// Check that node 2 has relatively high degree
	if degree2 < 0.1 {
		t.Errorf("Expected node 2 to have decent degree centrality, got %f", degree2)
	}

	// Verify all measures are non-negative
	for nodeID, score := range result.Betweenness {
		if score < 0 {
			t.Errorf("Negative betweenness for node %d: %f", nodeID, score)
		}
	}

	for nodeID, score := range result.Closeness {
		if score < 0 {
			t.Errorf("Negative closeness for node %d: %f", nodeID, score)
		}
	}

	for nodeID, score := range result.Degree {
		if score < 0 {
			t.Errorf("Negative degree for node %d: %f", nodeID, score)
		}
	}
}

// TestDegreeCentrality_Normalization tests that degree centrality is normalized
func TestDegreeCentrality_Normalization(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create complete graph K3 (all nodes connected to all others)
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeB.ID, "LINKS", nil, 1.0)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	// In complete graph, all nodes should have maximum normalized degree (1.0)
	// Each node has 2 in + 2 out = 4 total edges
	// Normalized by (n-1) = 2, so 4/2 = 2.0
	degreeA := result[nodeA.ID]

	// All nodes should have equal degree
	for _, degree := range result {
		if math.Abs(degree-degreeA) > 0.001 {
			t.Errorf("Expected equal degrees in complete graph, got %f and %f", degreeA, degree)
		}
	}
}

// TestBetweennessCentrality_ExactValues verifies BC against analytically-derived
// values for known topologies. All graphs use bidirectional edges to match the
// real usage pattern (directed Brandes on bidirectional = undirected normalized BC).
func TestBetweennessCentrality_ExactValues(t *testing.T) {
	const epsilon = 0.0001

	type nodeExpectation struct {
		label    string
		expected float64
	}

	tests := []struct {
		name  string
		edges [][2]int // node index pairs (bidirectional edges created for each)
		nodes int
		want  []nodeExpectation
	}{
		{
			name:  "Path_A-B-C",
			nodes: 3,
			edges: [][2]int{{0, 1}, {1, 2}},
			want: []nodeExpectation{
				{"A", 0.0},
				{"B", 1.0},
				{"C", 0.0},
			},
		},
		{
			name:  "Star_H-ABCD",
			nodes: 5,
			edges: [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}},
			want: []nodeExpectation{
				{"H", 1.0},
				{"A", 0.0},
				{"B", 0.0},
				{"C", 0.0},
				{"D", 0.0},
			},
		},
		{
			name:  "Path_A-B-C-D-E",
			nodes: 5,
			edges: [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}},
			want: []nodeExpectation{
				{"A", 0.0},
				{"B", 0.5},
				{"C", 0.6667},
				{"D", 0.5},
				{"E", 0.0},
			},
		},
		{
			name:  "Diamond_A-BC-D",
			nodes: 4,
			edges: [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}},
			want: []nodeExpectation{
				{"A", 0.1667},
				{"B", 0.1667},
				{"C", 0.1667},
				{"D", 0.1667},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := setupCentralityTestGraph(t)

			// Create nodes
			nodeIDs := make([]uint64, tt.nodes)
			for i := 0; i < tt.nodes; i++ {
				node, err := gs.CreateNode([]string{"Node"}, nil)
				if err != nil {
					t.Fatalf("Failed to create node %d: %v", i, err)
				}
				nodeIDs[i] = node.ID
			}

			// Create bidirectional edges
			for _, edge := range tt.edges {
				if _, err := gs.CreateEdge(nodeIDs[edge[0]], nodeIDs[edge[1]], "LINKS", nil, 1.0); err != nil {
					t.Fatalf("Failed to create edge %d->%d: %v", edge[0], edge[1], err)
				}
				if _, err := gs.CreateEdge(nodeIDs[edge[1]], nodeIDs[edge[0]], "LINKS", nil, 1.0); err != nil {
					t.Fatalf("Failed to create edge %d->%d: %v", edge[1], edge[0], err)
				}
			}

			result, err := BetweennessCentrality(gs)
			if err != nil {
				t.Fatalf("BetweennessCentrality failed: %v", err)
			}

			for i, w := range tt.want {
				got := result[nodeIDs[i]]
				if math.Abs(got-w.expected) > epsilon {
					t.Errorf("node %s: got BC %.6f, want %.4f (delta %.6f)",
						w.label, got, w.expected, math.Abs(got-w.expected))
				}
			}
		})
	}
}

// TestBetweennessCentrality_StevesUtility reconstructs the full 33-node, 70-undirected-edge
// model and verifies BC values against NetworkX ground truth (nx.betweenness_centrality,
// normalized=True). This is the definitive test for the published book values.
func TestBetweennessCentrality_StevesUtility(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// All 33 nodes in creation order
	nodeNames := []string{
		// Technical (22)
		"PLC_Turbine1", "PLC_Turbine2", "PLC_Substation",
		"RTU_Remote1", "RTU_Remote2",
		"HMI_Control1", "HMI_Control2", "Safety_PLC",
		"SCADA_Server", "Historian_OT", "Eng_Workstation",
		"OT_Switch_Core", "Patch_Server", "AD_Server_OT",
		"Firewall_ITOT", "Jump_Server", "Data_Diode",
		"IT_Switch_Core", "Email_Server", "ERP_System", "AD_Server_IT", "VPN_Gateway",
		// Human (7)
		"Steve", "OT_Manager", "IT_Admin",
		"Control_Op1", "Control_Op2", "Plant_Manager", "Vendor_Rep",
		// Process (4)
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

	// All 70 undirected edges (each becomes 2 directed edges)
	edges := [][2]string{
		// Technical edges (26)
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
		// Steve's edges (23)
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
		// Other human edges (21)
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

	// --- Model integrity checks ---
	if len(nodeNames) != 33 {
		t.Fatalf("Expected 33 nodes, got %d", len(nodeNames))
	}
	if len(edges) != 70 {
		t.Fatalf("Expected 70 undirected edges, got %d", len(edges))
	}

	directedEdgeCount := 0
	for _, edge := range edges {
		fromID := nameToID[edge[0]]
		toID := nameToID[edge[1]]
		if _, err := gs.CreateEdge(fromID, toID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("Failed to create edge %s->%s: %v", edge[0], edge[1], err)
		}
		if _, err := gs.CreateEdge(toID, fromID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("Failed to create edge %s->%s: %v", edge[1], edge[0], err)
		}
		directedEdgeCount += 2
	}
	if directedEdgeCount != 140 {
		t.Fatalf("Expected 140 directed edges, got %d", directedEdgeCount)
	}

	// Verify edge symmetry: every A->B has a matching B->A
	for _, edge := range edges {
		fromID := nameToID[edge[0]]
		toID := nameToID[edge[1]]

		outEdges, err := gs.GetOutgoingEdges(fromID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges(%s) failed: %v", edge[0], err)
		}
		found := false
		for _, e := range outEdges {
			if e.ToNodeID == toID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing forward edge %s -> %s", edge[0], edge[1])
		}

		reverseEdges, err := gs.GetOutgoingEdges(toID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges(%s) failed: %v", edge[1], err)
		}
		found = false
		for _, e := range reverseEdges {
			if e.ToNodeID == fromID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing reverse edge %s -> %s", edge[1], edge[0])
		}
	}

	// --- Compute BC ---
	result, err := BetweennessCentrality(gs)
	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	if len(result) != 33 {
		t.Fatalf("Expected 33 BC scores, got %d", len(result))
	}

	// NetworkX ground truth (nx.betweenness_centrality, normalized=True)
	// Generated by: python3 scripts/verify_bc.py
	expected := map[string]float64{
		"Steve":                 0.6682,
		"SCADA_Server":         0.1486,
		"IT_Admin":             0.1456,
		"OT_Manager":           0.0638,
		"Firewall_ITOT":        0.0543,
		"HMI_Control1":         0.0469,
		"HMI_Control2":         0.0388,
		"IT_Switch_Core":       0.0299,
		"Historian_OT":         0.0275,
		"OT_Switch_Core":       0.0241,
		"Plant_Manager":        0.0155,
		"VPN_Gateway":          0.0149,
		"Jump_Server":          0.0140,
		"Email_Server":         0.0062,
		"ERP_System":           0.0062,
		"Vendor_Rep":           0.0034,
		"Control_Op1":          0.0024,
		"Control_Op2":          0.0024,
		"Data_Diode":           0.0010,
		"Incident_Response":    0.0005,
		"Safety_PLC":           0.0004,
		"PLC_Turbine1":         0.0,
		"PLC_Turbine2":         0.0,
		"PLC_Substation":       0.0,
		"RTU_Remote1":          0.0,
		"RTU_Remote2":          0.0,
		"Eng_Workstation":      0.0,
		"Patch_Server":         0.0,
		"AD_Server_OT":         0.0,
		"AD_Server_IT":         0.0,
		"Change_Mgmt_Process":  0.0,
		"Vendor_Access_Process": 0.0,
		"Patch_Approval":       0.0,
	}

	const epsilon = 0.001
	for name, want := range expected {
		id, ok := nameToID[name]
		if !ok {
			t.Errorf("Node %q not found in nameToID map", name)
			continue
		}
		got := result[id]
		if math.Abs(got-want) > epsilon {
			t.Errorf("%-25s got BC %.6f, want %.4f (delta %.6f)",
				name, got, want, math.Abs(got-want))
		}
	}
}

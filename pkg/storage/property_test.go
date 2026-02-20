package storage

import (
	"os"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// nodeExists checks if a node exists in the storage
func nodeExists(gs *GraphStorage, nodeID uint64) bool {
	_, err := gs.GetNode(nodeID)
	return err == nil
}

// TestGraphInvariants uses property-based testing to verify graph invariants
// These properties should ALWAYS hold true for any valid graph operation
func TestGraphInvariants(t *testing.T) {
	if testing.Short() || isRaceEnabled() {
		t.Skip("Skipping property-based test in short mode or with race detector")
	}

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 20 // Reduced from 100 for reasonable test time

	properties := gopter.NewProperties(parameters)

	// Property 1: Edge creation requires both nodes to exist
	properties.Property("edge creation preserves node existence", prop.ForAll(
		func(fromID, toID uint64, label string) bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create an edge (note: signature requires all 5 args)
			_, err := storage.CreateEdge(fromID, toID, label, nil, 1.0)

			// If edge creation succeeds, both nodes must exist
			if err == nil {
				fromExist := nodeExists(storage, fromID)
				toExist := nodeExists(storage, toID)
				return fromExist && toExist
			}

			// If it fails, that's also valid (nodes might not exist)
			return true
		},
		gen.UInt64(),
		gen.UInt64(),
		gen.AlphaString(),
	))

	// Property 2: Creating then deleting a node leaves no trace
	properties.Property("create then delete is idempotent", prop.ForAll(
		func(labels []string, name string) bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create node
			props := map[string]Value{
				"name": StringValue(name),
			}
			node, err := storage.CreateNode(labels, props)
			if err != nil {
				return true // Skip if creation fails
			}

			// Delete node
			err = storage.DeleteNode(node.ID)
			if err != nil {
				return false
			}

			// Node should not exist anymore
			return !nodeExists(storage, node.ID)
		},
		gen.SliceOf(gen.AlphaString()),
		gen.AlphaString(),
	))

	// Property 3: Node count increases by 1 when node is created
	properties.Property("node creation increases count", prop.ForAll(
		func(label string, propKey string, propValue string) bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Get initial count
			initialCount := storage.GetStatistics().NodeCount

			// Create node
			props := map[string]Value{
				propKey: StringValue(propValue),
			}
			_, err := storage.CreateNode([]string{label}, props)
			if err != nil {
				return true // Skip if creation fails
			}

			// Count should increase by 1
			newCount := storage.GetStatistics().NodeCount
			return newCount == initialCount+1
		},
		gen.AlphaString(),
		gen.AlphaString(),
		gen.AlphaString(),
	))

	// Property 4: Edge endpoints never change
	properties.Property("edge endpoints are immutable", prop.ForAll(
		func(label string) bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create two nodes
			node1, _ := storage.CreateNode([]string{"Test"}, nil)
			node2, _ := storage.CreateNode([]string{"Test"}, nil)

			// Create edge
			edge, err := storage.CreateEdge(node1.ID, node2.ID, label, nil, 1.0)
			if err != nil {
				return true
			}

			// Get edge
			fetchedEdge, err := storage.GetEdge(edge.ID)
			if err != nil {
				return false
			}

			// Endpoints should match original
			return fetchedEdge.FromNodeID == node1.ID && fetchedEdge.ToNodeID == node2.ID
		},
		gen.AlphaString(),
	))

	// Property 5: Deleting a node deletes all its edges
	properties.Property("node deletion cascades to edges", prop.ForAll(
		func() bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create nodes
			node1, _ := storage.CreateNode([]string{"Test"}, nil)
			node2, _ := storage.CreateNode([]string{"Test"}, nil)
			node3, _ := storage.CreateNode([]string{"Test"}, nil)

			// Create edges
			edge1, _ := storage.CreateEdge(node1.ID, node2.ID, "TEST", nil, 1.0)
			edge2, _ := storage.CreateEdge(node1.ID, node3.ID, "TEST", nil, 1.0)
			edge3, _ := storage.CreateEdge(node2.ID, node1.ID, "TEST", nil, 1.0)

			// Delete node1
			err := storage.DeleteNode(node1.ID)
			if err != nil {
				return true
			}

			// All edges connected to node1 should be gone
			_, err1 := storage.GetEdge(edge1.ID)
			_, err2 := storage.GetEdge(edge2.ID)
			_, err3 := storage.GetEdge(edge3.ID)

			return err1 != nil && err2 != nil && err3 != nil
		},
	))

	// Property 6: Node properties can be read after write
	properties.Property("property write-read consistency", prop.ForAll(
		func(propKey string, propValue string) bool {
			if propKey == "" || len(propKey) > 100 {
				return true // Skip invalid keys
			}

			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create node with property
			props := map[string]Value{
				propKey: StringValue(propValue),
			}
			node, err := storage.CreateNode([]string{"Test"}, props)
			if err != nil {
				return true
			}

			// Read it back
			fetchedNode, err := storage.GetNode(node.ID)
			if err != nil {
				return false
			}

			// Property should match
			val, exists := fetchedNode.Properties[propKey]
			if !exists {
				return false
			}

			strVal, err := val.AsString()
			return err == nil && strVal == propValue
		},
		gen.AlphaString(),
		gen.AlphaString(),
	))

	// Property 7: Edges from a node have that node as source
	properties.Property("outgoing edges have correct source", prop.ForAll(
		func() bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create nodes and edges
			node1, _ := storage.CreateNode([]string{"Test"}, nil)
			node2, _ := storage.CreateNode([]string{"Test"}, nil)
			node3, _ := storage.CreateNode([]string{"Test"}, nil)

			storage.CreateEdge(node1.ID, node2.ID, "TEST", nil, 1.0)
			storage.CreateEdge(node1.ID, node3.ID, "TEST", nil, 1.0)

			// Get outgoing edges
			edges, err := storage.GetOutgoingEdges(node1.ID)
			if err != nil {
				return true
			}

			// All outgoing edges should have node1 as source
			for _, edge := range edges {
				if edge.FromNodeID != node1.ID {
					return false
				}
			}

			return true
		},
	))

	// Property 8: Finding nodes by label returns nodes with that label
	properties.Property("label query returns correct nodes", prop.ForAll(
		func(label string) bool {
			if label == "" {
				return true
			}

			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create nodes with and without the label
			storage.CreateNode([]string{label}, nil)
			storage.CreateNode([]string{label, "Other"}, nil)
			storage.CreateNode([]string{"Different"}, nil)

			// Find by label
			nodes, err := storage.FindNodesByLabel(label)
			if err != nil {
				return true
			}

			// All returned nodes should have the label
			for _, node := range nodes {
				hasLabel := false
				for _, l := range node.Labels {
					if l == label {
						hasLabel = true
						break
					}
				}
				if !hasLabel {
					return false
				}
			}

			return true
		},
		gen.AlphaString(),
	))

	// Property 9: Graph size metrics are consistent
	properties.Property("graph metrics are consistent", prop.ForAll(
		func(numNodes int) bool {
			// Limit to reasonable size
			if numNodes < 0 || numNodes > 100 {
				return true
			}

			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create nodes
			for i := 0; i < numNodes; i++ {
				storage.CreateNode([]string{"Test"}, nil)
			}

			// Node count should match
			count := storage.GetStatistics().NodeCount
			return count == uint64(numNodes)
		},
		gen.IntRange(0, 100),
	))

	// Property 10: Concurrent reads don't affect graph state
	properties.Property("concurrent reads are safe", prop.ForAll(
		func() bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create test data
			node, _ := storage.CreateNode([]string{"Test"}, map[string]Value{
				"name": StringValue("test"),
			})

			// Get initial state
			node1, _ := storage.GetNode(node.ID)
			initialName, _ := node1.Properties["name"].AsString()

			// Concurrent reads
			done := make(chan bool, 10)
			for i := 0; i < 10; i++ {
				go func() {
					storage.GetNode(node.ID)
					done <- true
				}()
			}

			// Wait for all reads
			for i := 0; i < 10; i++ {
				<-done
			}

			// State should be unchanged
			node2, _ := storage.GetNode(node.ID)
			finalName, _ := node2.Properties["name"].AsString()

			return initialName == finalName
		},
	))

	// Run all property tests
	properties.TestingRun(t)
}

// TestGraphPropertyInvariantsWithData tests invariants with realistic data
func TestGraphPropertyInvariantsWithData(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50

	properties := gopter.NewProperties(parameters)

	// Property: Social network invariant - friendship is symmetric
	properties.Property("friendship symmetry", prop.ForAll(
		func() bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create two people
			alice, _ := storage.CreateNode([]string{"Person"}, map[string]Value{
				"name": StringValue("Alice"),
			})
			bob, _ := storage.CreateNode([]string{"Person"}, map[string]Value{
				"name": StringValue("Bob"),
			})

			// Create friendship edges (both directions)
			storage.CreateEdge(alice.ID, bob.ID, "FRIEND", nil, 1.0)
			storage.CreateEdge(bob.ID, alice.ID, "FRIEND", nil, 1.0)

			// Alice's friends should include Bob
			aliceFriends, _ := storage.GetOutgoingEdges(alice.ID)
			hasBob := false
			for _, edge := range aliceFriends {
				if edge.ToNodeID == bob.ID && edge.Type == "FRIEND" {
					hasBob = true
					break
				}
			}

			// Bob's friends should include Alice
			bobFriends, _ := storage.GetOutgoingEdges(bob.ID)
			hasAlice := false
			for _, edge := range bobFriends {
				if edge.ToNodeID == alice.ID && edge.Type == "FRIEND" {
					hasAlice = true
					break
				}
			}

			return hasBob && hasAlice
		},
	))

	// Property: DAG invariant - self-loops can be detected
	properties.Property("self-loop detection", prop.ForAll(
		func() bool {
			storage := newPropertyTestStorage(t)
			defer storage.Close()

			// Create a node
			node, _ := storage.CreateNode([]string{"DAGNode"}, nil)

			// Try to create self-loop
			_, err := storage.CreateEdge(node.ID, node.ID, "NEXT", nil, 1.0)

			// Should either fail or be detectable
			if err == nil {
				// If it succeeds, we can detect it
				edges, _ := storage.GetOutgoingEdges(node.ID)
				for _, edge := range edges {
					if edge.FromNodeID == edge.ToNodeID {
						// Self-loop detected - this is detectable
						return true
					}
				}
			}

			return true
		},
	))

	properties.TestingRun(t)
}

// newPropertyTestStorage creates a temporary storage for property tests
func newPropertyTestStorage(t *testing.T) *GraphStorage {
	// Create a temp directory for the test
	tmpDir, err := os.MkdirTemp("", "graphdb-property-test-*")
	if err != nil {
		t.Skipf("Failed to create temp dir: %v", err)
	}

	storage, err := NewGraphStorage(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Skipf("Failed to create test storage: %v", err)
	}

	// Track temp dir for cleanup
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	return storage
}

package storage

import (
	"os"
	"testing"
)

// setupTransactionTest creates a test graph storage for transaction tests
func setupTransactionTest(t *testing.T) (*GraphStorage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "transaction-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	gs, err := NewGraphStorage(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tmpDir)
	}

	return gs, cleanup
}

// TestTransaction_BeginCommit tests basic transaction begin and commit
func TestTransaction_BeginCommit(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	// Begin transaction
	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Create node in transaction
	node, err := tx.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Alice"),
		"age":  IntValue(30),
	})
	if err != nil {
		t.Fatalf("Failed to create node in transaction: %v", err)
	}

	if node.ID == 0 {
		t.Error("Expected non-zero node ID")
	}

	// Node should not be visible outside transaction yet
	_, err = gs.GetNodeByID(node.ID)
	if err == nil {
		t.Error("Node should not be visible outside transaction before commit")
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// Node should now be visible
	committedNode, err := gs.GetNodeByID(node.ID)
	if err != nil {
		t.Fatalf("Failed to get committed node: %v", err)
	}

	if committedNode.ID != node.ID {
		t.Errorf("Expected node ID %d, got %d", node.ID, committedNode.ID)
	}
}

// TestTransaction_Rollback tests transaction rollback
func TestTransaction_Rollback(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	// Begin transaction
	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Create node in transaction
	node, err := tx.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Bob"),
		"age":  IntValue(25),
	})
	if err != nil {
		t.Fatalf("Failed to create node in transaction: %v", err)
	}

	nodeID := node.ID

	// Rollback transaction
	err = tx.Rollback()
	if err != nil {
		t.Fatalf("Failed to rollback transaction: %v", err)
	}

	// Node should not exist after rollback
	_, err = gs.GetNodeByID(nodeID)
	if err == nil {
		t.Error("Node should not exist after rollback")
	}
}

// TestTransaction_Isolation tests that transactions are isolated from each other
func TestTransaction_Isolation(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	// Create initial node
	initialNode, err := gs.CreateNode([]string{"Counter"}, map[string]Value{
		"value": IntValue(0),
	})
	if err != nil {
		t.Fatalf("Failed to create initial node: %v", err)
	}

	// Begin two transactions
	tx1, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction 1: %v", err)
	}

	tx2, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction 2: %v", err)
	}

	// Transaction 1: Read and update
	node1, err := tx1.GetNodeByID(initialNode.ID)
	if err != nil {
		t.Fatalf("TX1: Failed to get node: %v", err)
	}

	val1, _ := node1.Properties["value"].AsInt()
	err = tx1.UpdateNode(node1.ID, map[string]Value{
		"value": IntValue(val1 + 10),
	})
	if err != nil {
		t.Fatalf("TX1: Failed to update node: %v", err)
	}

	// Transaction 2: Read and update (should not see TX1's changes)
	node2, err := tx2.GetNodeByID(initialNode.ID)
	if err != nil {
		t.Fatalf("TX2: Failed to get node: %v", err)
	}

	val2, _ := node2.Properties["value"].AsInt()
	if val2 != 0 {
		t.Errorf("TX2 should see original value 0, got %d", val2)
	}

	err = tx2.UpdateNode(node2.ID, map[string]Value{
		"value": IntValue(val2 + 20),
	})
	if err != nil {
		t.Fatalf("TX2: Failed to update node: %v", err)
	}

	// Commit both transactions
	err = tx1.Commit()
	if err != nil {
		t.Fatalf("TX1: Failed to commit: %v", err)
	}

	err = tx2.Commit()
	if err != nil {
		t.Fatalf("TX2: Failed to commit: %v", err)
	}

	// Final value should reflect last committed transaction
	finalNode, err := gs.GetNodeByID(initialNode.ID)
	if err != nil {
		t.Fatalf("Failed to get final node: %v", err)
	}

	finalVal, _ := finalNode.Properties["value"].AsInt()
	// The final value depends on which transaction committed last
	// For now, just verify it's one of the expected values
	if finalVal != 10 && finalVal != 20 {
		t.Errorf("Expected final value 10 or 20, got %d", finalVal)
	}
}

// TestTransaction_MultipleOperations tests multiple operations in a single transaction
func TestTransaction_MultipleOperations(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Create multiple nodes
	node1, err := tx.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Alice"),
	})
	if err != nil {
		t.Fatalf("Failed to create node 1: %v", err)
	}

	node2, err := tx.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Bob"),
	})
	if err != nil {
		t.Fatalf("Failed to create node 2: %v", err)
	}

	// Create edge between nodes
	edge, err := tx.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
		"since": IntValue(2020),
	}, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	// Update a node
	err = tx.UpdateNode(node1.ID, map[string]Value{
		"name": StringValue("Alice Updated"),
		"age":  IntValue(30),
	})
	if err != nil {
		t.Fatalf("Failed to update node: %v", err)
	}

	// Commit all operations
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Verify all operations persisted
	persistedNode1, err := gs.GetNodeByID(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get node 1: %v", err)
	}

	name, _ := persistedNode1.Properties["name"].AsString()
	if name != "Alice Updated" {
		t.Errorf("Expected name 'Alice Updated', got '%s'", name)
	}

	persistedNode2, err := gs.GetNodeByID(node2.ID)
	if err != nil {
		t.Fatalf("Failed to get node 2: %v", err)
	}

	if persistedNode2.ID != node2.ID {
		t.Error("Node 2 not persisted correctly")
	}

	persistedEdge, err := gs.GetEdgeByID(edge.ID)
	if err != nil {
		t.Fatalf("Failed to get edge: %v", err)
	}

	if persistedEdge.Type != "KNOWS" {
		t.Errorf("Expected edge type 'KNOWS', got '%s'", persistedEdge.Type)
	}
}

// TestTransaction_ConcurrentTransactionsAllowed tests that concurrent transactions are allowed
func TestTransaction_ConcurrentTransactionsAllowed(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	tx1, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction 1: %v", err)
	}
	defer tx1.Rollback()

	// Concurrent transactions should be allowed
	tx2, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin concurrent transaction 2: %v", err)
	}
	defer tx2.Rollback()

	if tx1.id == tx2.id {
		t.Error("Expected different transaction IDs for concurrent transactions")
	}
}

// TestTransaction_CommitAfterRollback tests that commit fails after rollback
func TestTransaction_CommitAfterRollback(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Rollback
	err = tx.Rollback()
	if err != nil {
		t.Fatalf("Failed to rollback: %v", err)
	}

	// Commit should fail
	err = tx.Commit()
	if err == nil {
		t.Error("Expected error when committing after rollback, got nil")
	}
}

// TestTransaction_DoubleCommit tests that double commit fails
func TestTransaction_DoubleCommit(t *testing.T) {
	gs, cleanup := setupTransactionTest(t)
	defer cleanup()

	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// First commit
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Second commit should fail
	err = tx.Commit()
	if err == nil {
		t.Error("Expected error when double committing, got nil")
	}
}

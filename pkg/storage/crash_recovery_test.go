package storage

import (
	"testing"
)

// TestCrashRecovery simulates a crash and verifies WAL replay works
func TestCrashRecovery(t *testing.T) {
	dataDir := t.TempDir()

	// Phase 1: Create database and add data
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		// Create nodes
		node1, _ := gs.CreateNode(
			[]string{"User"},
			map[string]Value{"id": StringValue("user1")},
		)

		node2, _ := gs.CreateNode(
			[]string{"User"},
			map[string]Value{"id": StringValue("user2")},
		)

		// Create edge
		gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)

		// Save snapshot
		if err := gs.Snapshot(); err != nil {
			t.Fatalf("Failed to snapshot: %v", err)
		}

		// Add more data AFTER snapshot (these will only be in WAL)
		node3, _ := gs.CreateNode(
			[]string{"User"},
			map[string]Value{"id": StringValue("user3")},
		)

		gs.CreateEdge(node2.ID, node3.ID, "VERIFIED_BY", map[string]Value{}, 1.0)

		// SIMULATE CRASH: Don't call Close(), just let it go out of scope
		// This means the last 2 operations are only in WAL, not in snapshot
	}

	// Phase 2: Reopen database (simulates recovery after crash)
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to reopen storage: %v", err)
		}
		defer gs.Close()

		// Verify all data is present (from snapshot + WAL replay)
		stats := gs.GetStatistics()

		if stats.NodeCount != 3 {
			t.Errorf("Expected 3 nodes after recovery, got %d", stats.NodeCount)
		}

		if stats.EdgeCount != 2 {
			t.Errorf("Expected 2 edges after recovery, got %d", stats.EdgeCount)
		}

		// Verify specific nodes exist
		users, _ := gs.FindNodesByLabel("User")
		if len(users) != 3 {
			t.Errorf("Expected 3 User nodes, got %d", len(users))
		}

		// Verify user3 (which was only in WAL) exists
		user3Found := false
		for _, user := range users {
			if prop, ok := user.GetProperty("id"); ok {
				if id, _ := prop.AsString(); id == "user3" {
					user3Found = true
					break
				}
			}
		}

		if !user3Found {
			t.Error("user3 not recovered from WAL")
		}

		// Verify edges
		edges, _ := gs.FindEdgesByType("VERIFIED_BY")
		if len(edges) != 1 {
			t.Errorf("Expected 1 VERIFIED_BY edge, got %d", len(edges))
		}
	}
}

// TestWALReplayOrder verifies operations are replayed in correct order
func TestWALReplayOrder(t *testing.T) {
	dataDir := t.TempDir()

	// Create and populate
	{
		gs, _ := NewGraphStorage(dataDir)

		// Create nodes in specific order
		for i := 1; i <= 5; i++ {
			gs.CreateNode(
				[]string{"User"},
				map[string]Value{"order": IntValue(int64(i))},
			)
		}

		// Don't snapshot - force recovery from WAL only
	}

	// Recover
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify order is maintained
		users, _ := gs.FindNodesByLabel("User")
		if len(users) != 5 {
			t.Fatalf("Expected 5 users, got %d", len(users))
		}

		// Node IDs should be sequential
		for i, user := range users {
			expectedID := uint64(i + 1)
			if user.ID != expectedID {
				t.Errorf("Node %d: expected ID %d, got %d", i, expectedID, user.ID)
			}
		}
	}
}

// TestPartialSnapshot verifies recovery with partial snapshot
func TestPartialSnapshot(t *testing.T) {
	dataDir := t.TempDir()

	{
		gs, _ := NewGraphStorage(dataDir)

		// Create 3 nodes
		gs.CreateNode([]string{"User"}, map[string]Value{"name": StringValue("Alice")})
		gs.CreateNode([]string{"User"}, map[string]Value{"name": StringValue("Bob")})
		gs.Snapshot() // Snapshot with 2 nodes

		// Add 2 more nodes after snapshot
		gs.CreateNode([]string{"User"}, map[string]Value{"name": StringValue("Charlie")})
		gs.CreateNode([]string{"User"}, map[string]Value{"name": StringValue("David")})

		// Crash without saving
	}

	{
		gs, _ := NewGraphStorage(dataDir)
		defer gs.Close()

		users, _ := gs.FindNodesByLabel("User")
		if len(users) != 4 {
			t.Errorf("Expected 4 users (2 from snapshot + 2 from WAL), got %d", len(users))
		}
	}
}

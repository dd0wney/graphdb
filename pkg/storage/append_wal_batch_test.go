package storage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// TestAppendWALBatch_DurableAcrossCrash pins that gs.appendWALBatch writes a
// batch of WAL entries that survive a crash and replay correctly — across both
// the plain and batched WAL modes. appendWALBatch writes only the WAL (no
// in-memory mutation), so we must simulate a crash (DON'T Close — Close
// snapshots in-memory state, which lacks these nodes, and truncates the WAL).
// Reopening on the same dir replays the WAL; the nodes existing afterward
// proves the batch was durable and routed to the configured WAL.
func TestAppendWALBatch_DurableAcrossCrash(t *testing.T) {
	cases := []struct {
		name    string
		batched bool
	}{
		{"plain", false},
		{"batched", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			cfg := StorageConfig{DataDir: dataDir, EnableBatching: tc.batched, BatchSize: 100, FlushInterval: 10 * time.Millisecond}

			// Phase 1: write a batch via appendWALBatch, then "crash" (no Close).
			{
				gs := testCrashableStorage(t, dataDir, cfg)
				entries := make([]wal.BatchEntry, 0, 2)
				for _, id := range []uint64{101, 102} {
					n := &Node{ID: id, TenantID: "acme", Labels: []string{"Doc"}}
					data, err := json.Marshal(n)
					if err != nil {
						t.Fatalf("marshal: %v", err)
					}
					entries = append(entries, wal.BatchEntry{OpType: wal.OpCreateNode, Data: data})
				}
				if err := gs.appendWALBatch(entries); err != nil {
					t.Fatalf("appendWALBatch: %v", err)
				}
				// DON'T Close — crash sim (testCrashableStorage handles cleanup).
			}

			// Phase 2: reopen — replay must restore both nodes from the batch.
			gs2, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = gs2.Close() }()

			for _, id := range []uint64{101, 102} {
				if _, err := gs2.GetNode(id); err != nil {
					t.Errorf("node %d not recovered after crash (batch not durable/replayed): %v", id, err)
				}
			}
		})
	}
}

// TestAppendWALBatch_EmptyNoop confirms an empty batch is a no-op error-free.
func TestAppendWALBatch_EmptyNoop(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	if err := gs.appendWALBatch(nil); err != nil {
		t.Fatalf("appendWALBatch(nil): %v", err)
	}
}

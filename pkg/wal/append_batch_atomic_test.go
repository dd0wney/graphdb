package wal

import (
	"bytes"
	"testing"
)

// TestAppendBatchAtomic_DurableAfterReopen pins that AppendBatchAtomic makes
// the whole batch durable with a single fsync: after close + reopen, every
// entry is recovered, in order, with the right opType and data.
func TestAppendBatchAtomic_DurableAfterReopen(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	batch := []BatchEntry{
		{OpType: OpCreateNode, Data: []byte("node-1")},
		{OpType: OpCreateEdge, Data: []byte("edge-1")},
		{OpType: OpUpdateNode, Data: []byte("update-1")},
	}
	if err := w.AppendBatchAtomic(batch); err != nil {
		t.Fatalf("AppendBatchAtomic: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	w2, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer w2.Close()

	entries, err := w2.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != len(batch) {
		t.Fatalf("recovered %d entries, want %d (batch not fully durable)", len(entries), len(batch))
	}
	for i, e := range entries {
		if e.OpType != batch[i].OpType {
			t.Errorf("entry %d opType=%v, want %v", i, e.OpType, batch[i].OpType)
		}
		if !bytes.Equal(e.Data, batch[i].Data) {
			t.Errorf("entry %d data=%q, want %q", i, e.Data, batch[i].Data)
		}
		// LSNs are assigned sequentially within the batch.
		if e.LSN != uint64(i+1) {
			t.Errorf("entry %d LSN=%d, want %d", i, e.LSN, i+1)
		}
	}
}

// TestAppendBatchAtomic_Empty is a no-op (no entries, no error, no LSN bump).
func TestAppendBatchAtomic_Empty(t *testing.T) {
	w, err := NewWAL(t.TempDir())
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w.Close()

	before := w.GetCurrentLSN()
	if err := w.AppendBatchAtomic(nil); err != nil {
		t.Fatalf("AppendBatchAtomic(nil): %v", err)
	}
	if err := w.AppendBatchAtomic([]BatchEntry{}); err != nil {
		t.Fatalf("AppendBatchAtomic(empty): %v", err)
	}
	if after := w.GetCurrentLSN(); after != before {
		t.Errorf("empty batch advanced LSN %d -> %d", before, after)
	}
}

// TestAppendBatch_DelegatesToAtomic pins that the existing pendingEntry-based
// AppendBatch (used by BatchedWAL.flush) still works after being refactored to
// delegate to AppendBatchAtomic — same durability, same ordering.
func TestAppendBatch_DelegatesToAtomic(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	pending := []*pendingEntry{
		{opType: OpCreateNode, data: []byte("a")},
		{opType: OpCreateNode, data: []byte("b")},
	}
	if err := w.AppendBatch(pending); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	w.Close()

	w2, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer w2.Close()
	entries, err := w2.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 2 || !bytes.Equal(entries[0].Data, []byte("a")) || !bytes.Equal(entries[1].Data, []byte("b")) {
		t.Fatalf("AppendBatch did not durably write both entries in order: %+v", entries)
	}
}

package wal

// F1 from the boundary-guard review (graphdb:v1.4-boundary-lsn-downgrade-guard):
// AppendBatchAtomic's mid-loop failure branch subtracted len(entries) from
// currentLSN unconditionally. That is only correct when the failure lands on
// the LAST entry of the batch — every earlier entry position leaves
// currentLSN below every durably-written LSN, or makes it underflow past
// zero for a small enough start. The reviewer measured before=0
// after=18446744073709551614, and before=5 after=3, on two runs of the
// pre-fix code.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// TestAppendBatchAtomic_WriteFailureOnSecondEntryRestoresPreBatchLSN arms a
// write fault to fire on the second entry of a three-entry batch and asserts
// GetCurrentLSN afterward equals the value before the call — the whole batch
// failed, so none of it advanced the counter.
//
// The fault is armed to fire on the FIRST real write the underlying file
// sees after arming. The batch's first entry is small enough to stay fully
// buffered inside the WAL's 4 KB bufio writer, with no real write of its
// own; the second entry's Data is larger than that buffer, so writing it
// forces bufio to flush what is already buffered — the first entry's
// trailer plus the second entry's header — before writing the second
// entry's payload directly. That flush is the first real write the driver
// sees, and it happens while writeEntry is processing entry index 1 (the
// second entry), not the first or the third. fs.Writes() == 1 at the end
// pins that only one real write was attempted, which is the positive
// control that the fault landed where this test says it did.
func TestAppendBatchAtomic_WriteFailureOnSecondEntryRestoresPreBatchLSN(t *testing.T) {
	dataDir := t.TempDir()
	fs := vfstest.NewFaults(vfs.OS(), "append-batch-atomic-second-entry-fault")

	w, err := NewWALWithFS(dataDir, fs)
	if err != nil {
		t.Fatalf("NewWALWithFS: %v", err)
	}
	defer func() { fs.Clear(); _ = w.Close() }()

	// Prime with one durable entry so "before" is not zero. The bug's
	// underflow shape is one symptom of the same defect, not the only one;
	// starting from a non-zero LSN pins the plainer undercount shape
	// (before=N, buggy after<N) for this specific failure position.
	if _, err := w.Append(OpCreateNode, []byte("prime")); err != nil {
		t.Fatalf("prime append: %v", err)
	}
	before := w.GetCurrentLSN()

	fs.FailWrite(vfstest.Once, 0)

	batch := []BatchEntry{
		{OpType: OpCreateNode, Data: []byte("entry-one-small")},
		{OpType: OpCreateNode, Data: bytes.Repeat([]byte("x"), 8192)},
		{OpType: OpCreateNode, Data: []byte("entry-three-small")},
	}
	err = w.AppendBatchAtomic(batch)
	if err == nil {
		t.Fatal("AppendBatchAtomic returned a nil error: the fault did not reach the batch write")
	}
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("AppendBatchAtomic error = %v, want the injected fault", err)
	}
	if !fs.Fired() {
		t.Fatal("the write fault never fired, so this test proves nothing about the rollback path")
	}
	if writes := fs.Writes(); writes != 1 {
		t.Fatalf("test premise broken: %d real writes reached the driver before the fault fired, want 1 "+
			"(the flush entry two's large payload forces) — the fault did not land on the second entry", writes)
	}

	if after := w.GetCurrentLSN(); after != before {
		t.Fatalf("GetCurrentLSN after a failed batch = %d, want %d (the pre-batch value): a write failure "+
			"partway through the batch must restore it exactly, not subtract the whole batch length", after, before)
	}
}

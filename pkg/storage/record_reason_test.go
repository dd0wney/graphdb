package storage

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
	"github.com/dd0wney/graphdb/pkg/alloc/alloctest"
)

// A decoder that refuses to say WHY it failed turns a damaged record and a
// refused allocation into a missing one. These two tests pin the difference at
// the lowest layer that knows it.

func TestDecodeNodeRecordAt_DamageReportsUnreadable(t *testing.T) {
	buf := encodeNodeRecord(&Node{
		ID: 7, TenantID: "default", Labels: []string{"Thing"},
		Properties: map[string]Value{"name": StringValue("alpha")},
	})
	// Positive control: the record decodes before it is damaged.
	if _, err := decodeNodeRecordAt(buf, 0); err != nil {
		t.Fatalf("undamaged record must decode: %v", err)
	}

	binary.LittleEndian.PutUint16(buf[8:], 0xFFFF) // tenant length past the buffer

	_, err := decodeNodeRecordAt(buf, 0)
	if err == nil {
		t.Fatal("a damaged record must not decode")
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}
	if errors.Is(err, alloc.ErrNoMemory) {
		t.Fatalf("damage must not be reported as a refusal: %v", err)
	}
}

func TestDecodeNodeRecordAt_RefusalReportsNoMemory(t *testing.T) {
	buf := encodeNodeRecord(&Node{
		ID: 7, TenantID: "default", Labels: []string{"Thing"},
		Properties: map[string]Value{"name": StringValue("alpha")},
	})

	alloc.Install(alloctest.New(alloctest.FailAllFrom, 1))
	defer alloc.Reset()

	_, err := decodeNodeRecordAt(buf, 0)
	if err == nil {
		t.Fatal("a refused allocation must not decode")
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}
	if !errors.Is(err, alloc.ErrNoMemory) {
		t.Fatalf("want the alloc cause to survive, got %v", err)
	}
	// Negative control: without this the test can pass on an error from
	// somewhere else entirely.
	if alloc.Allocs() == 0 {
		t.Fatal("no allocation was attempted, so this proves nothing")
	}
}

func TestMmapSnapshotGetNode_AbsentIsNotUnreadable(t *testing.T) {
	path := snapshotOnDisk(t) // helper in mmap_oom_test.go
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = snap.close() }()

	// Positive control: a node the directory lists must come back.
	var present uint64
	snap.forEachNodeID(func(id uint64, _ int64) {
		if present == 0 {
			present = id
		}
	})
	if present == 0 {
		t.Fatal("fixture has no nodes, so this test proves nothing")
	}
	if _, err := snap.getNode(present); err != nil {
		t.Fatalf("node %d must decode: %v", present, err)
	}

	_, maxID := snap.nodeIDRange()
	_, err = snap.getNode(maxID + 1000)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("an ID outside the directory must be ErrNodeNotFound, got %v", err)
	}
	if errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("absence must never be reported as unreadable: %v", err)
	}
}

func TestResolveNodeRefLocked_AbsentIsNotFound(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Positive control: a node that exists resolves with no error.
	if _, err := gs.resolveNodeRefLocked(n.ID); err != nil {
		t.Fatalf("existing node must resolve: %v", err)
	}
	// Absence is ErrNodeNotFound, never a bare nil or an unreadable.
	_, err = gs.resolveNodeRefLocked(n.ID + 9999)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("want ErrNodeNotFound, got %v", err)
	}
}

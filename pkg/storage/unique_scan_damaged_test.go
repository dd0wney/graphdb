package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

// CreateNodeWithUniquePropertyForTenant scans every node under the target
// label (membershipNodeIDsByLabelLocked) and resolves each ID to check for a
// conflicting property value. Before this fix, a record that failed to
// decode was treated as "not a match" — the scan loop ran `continue` on any
// resolveNodeRefLocked error — so a damaged record read as "no conflict" and
// a duplicate was created.
//
// This method is the atomic-claim primitive behind graphdb-coord's :Claim
// resolver (docs/COORD_DEPLOY_SPIKE_2026-05-10.md). A duplicate here means
// two agents each believe they hold the only claim on the same for_task.
//
// Reuses the corruption technique from damaged_record_test.go: reopen the
// mmap snapshot file directly, overwrite the tenant-length prefix (byte 8 of
// a node record) with a value that asks for more bytes than the file holds,
// so the bounds check refuses the record while the file-level CRC (which
// does not cover record bodies) still lets the store open.
// CONSUMER CONTRACT: CC10-unique-create-after-mmap-reopen — graphdb-coord
func TestUniqueCreateRefusesWhenScanHitsUnreadableRecord(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	const label = "Claim"
	const propKey = "for_task"

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	props := map[string]Value{propKey: StringValue("task-42")}
	target, err := gs.CreateNodeWithUniquePropertyForTenant(tenant, []string{label}, props, label, propKey)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(target.ID)
	if !ok {
		t.Fatalf("node %d has no directory entry", target.ID)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Byte 8 of a node record is the tenant-length prefix. 0xFFFF asks for
	// more bytes than the file holds, so the bounds check refuses the
	// record. The CRC does not cover this byte, so the file still opens.
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption. Without it, a test that stopped
	// corrupting anything would still pass.
	if _, decErr := decodeNodeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	tid := effectiveTenantID(tenant)
	countByLabel := func() int {
		gs2.mu.RLock()
		defer gs2.mu.RUnlock()
		return len(gs2.membershipNodeIDsByLabelLocked(tid, label))
	}

	// Setup check: the damaged record is still listed under the label (its
	// presence lives in the membership directory, separate from whether its
	// bytes decode). One member, and it is the damaged one.
	if before := countByLabel(); before != 1 {
		t.Fatalf("setup: want 1 member under label %q before the duplicate attempt, got %d", label, before)
	}

	dupProps := map[string]Value{propKey: StringValue("task-42")}
	dup, err := gs2.CreateNodeWithUniquePropertyForTenant(tenant, []string{label}, dupProps, label, propKey)

	// THE DEFECT: pre-fix, this call returns (node, nil). The scan reached
	// the damaged record, resolveNodeRefLocked returned an error, and the
	// loop's `continue` read that as "no conflict" — so a second Claim for
	// "task-42" was created here.
	if err == nil {
		t.Fatalf("DEFECT REPRODUCED: duplicate create succeeded past a damaged conflicting record — "+
			"new node %d alongside original %d, membership count now %d",
			dup.ID, target.ID, countByLabel())
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want an error wrapping ErrRecordUnreadable, got %v", err)
	}
	if dup != nil {
		t.Fatalf("want a nil node when the create is refused, got %+v", dup)
	}

	// The membership count must be unchanged: no duplicate landed.
	if after := countByLabel(); after != 1 {
		t.Fatalf("a duplicate landed: membership count under label %q went from 1 to %d", label, after)
	}
}

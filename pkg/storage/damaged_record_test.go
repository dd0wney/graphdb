package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The snapshot CRC covers the header, the directories and the metadata blob —
// NOT record bodies (mmap_snapshot_format.go, "bounds-checked record reading").
// So a record damaged by bit rot, a partial write or a truncated copy survives
// open and reaches a decoder. Until this test, that record read as a MISSING
// node, and a query returned an incomplete result with nothing to signal it.
//
// SQLite's equivalent cannot happen: sqlite3_malloc failure returns
// SQLITE_NOMEM and a malformed page returns SQLITE_CORRUPT, neither of which
// can be mistaken for an empty result (sqlite.org/malloc.html).
// CONSUMER CONTRACT: CC11-unreadable-not-missing — all readers (PR B)
func TestDamagedRecordIsNotReportedAsMissing(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	survivor, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("beta")})
	if err != nil {
		t.Fatalf("create survivor: %v", err)
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
	// Byte 8 of a node record is the tenant-length prefix. 0xFFFF asks for more
	// bytes than the file holds, so the bounds check refuses the record. The
	// CRC does not cover this byte, so the file still opens.
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

	_, err = gs2.GetNodeForTenant(target.ID, "")
	if err == nil {
		t.Fatal("a damaged record must not read as a successful lookup")
	}
	if errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a damaged record was reported as missing: %v", err)
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}

	// NEGATIVE CONTROL: the undamaged neighbour still reads. Without this the
	// test would pass just as well if reopen had failed entirely.
	if _, err := gs2.GetNodeForTenant(survivor.ID, ""); err != nil {
		t.Fatalf("the undamaged node must still read: %v", err)
	}

	// An ID that was never in the directory is still plain absence.
	if _, err := gs2.GetNodeForTenant(survivor.ID+9999, ""); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a genuinely missing node must stay ErrNodeNotFound, got %v", err)
	}
}

// A damaged record must not tell a caller from another tenant that the ID
// exists. The record's tenant string lives inside the record that would not
// decode, so the tenant check cannot come from the record. The answer comes
// from the per-tenant membership run instead, which the CRC does not protect
// either — so the containment test fails closed and the stranger gets plain
// absence.
//
// BOTH assertions matter. Without the owner assertion, collapsing every
// unreadable record back to ErrNodeNotFound would pass this test and silently
// undo the fix in the previous commit.
func TestDamagedRecordDoesNotLeakAcrossTenants(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	// The stranger owns a node of its own, so its membership run EXISTS. The
	// stranger must be refused because the target ID is absent from that run,
	// not because the run is missing — a weaker fixture would pass on the
	// wrong reason.
	decoy, err := gs.CreateNodeWithTenant("stranger", []string{"Thing"}, map[string]Value{"name": StringValue("beta")})
	if err != nil {
		t.Fatalf("create decoy: %v", err)
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
	// Byte 8 of a node record is the tenant-length prefix, as in
	// TestDamagedRecordIsNotReportedAsMissing.
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption.
	if _, decErr := decodeNodeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	// The OWNING tenant still learns that the record is unreadable. This
	// assertion is what stops the fix from being a silent regression.
	if _, err := gs2.GetNodeForTenant(target.ID, "owner"); !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("the owning tenant must still get ErrRecordUnreadable, got %v", err)
	}

	// The STRANGER learns nothing. Absence and refusal must look the same.
	_, err = gs2.GetNodeForTenant(target.ID, "stranger")
	if errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("a damaged record leaked its existence across tenants: %v", err)
	}
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("want ErrNodeNotFound for a cross-tenant read, got %v", err)
	}

	// NEGATIVE CONTROL: the stranger's own node still reads, so the reopen
	// worked and the stranger's membership run is real.
	if _, err := gs2.GetNodeForTenant(decoy.ID, "stranger"); err != nil {
		t.Fatalf("the stranger's own node must still read: %v", err)
	}
}

// The edge readers carry the same guard, so they need the same test. Without
// it, GetEdgeForTenant and getEdgeRefForTenant would hold a security check
// that nobody has watched work.
func TestDamagedEdgeRecordDoesNotLeakAcrossTenants(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	from, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create from: %v", err)
	}
	to, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create to: %v", err)
	}
	target, err := gs.CreateEdgeWithTenant("owner", from.ID, to.ID, "LINK", nil, 1)
	if err != nil {
		t.Fatalf("create target edge: %v", err)
	}
	// The stranger owns an edge too, so its edge membership run exists.
	sf, err := gs.CreateNodeWithTenant("stranger", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create stranger from: %v", err)
	}
	st, err := gs.CreateNodeWithTenant("stranger", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create stranger to: %v", err)
	}
	decoy, err := gs.CreateEdgeWithTenant("stranger", sf.ID, st.ID, "LINK", nil, 1)
	if err != nil {
		t.Fatalf("create decoy edge: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.edgeOffset(target.ID)
	if !ok {
		t.Fatalf("edge %d has no directory entry", target.ID)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Byte 8 of an edge record is the tenant-length prefix, as for a node.
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption.
	if _, decErr := decodeEdgeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	if _, err := gs2.GetEdgeForTenant(target.ID, "owner"); !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("the owning tenant must still get ErrRecordUnreadable, got %v", err)
	}

	_, err = gs2.GetEdgeForTenant(target.ID, "stranger")
	if errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("a damaged edge leaked its existence across tenants: %v", err)
	}
	if !errors.Is(err, ErrEdgeNotFound) {
		t.Fatalf("want ErrEdgeNotFound for a cross-tenant read, got %v", err)
	}

	// NEGATIVE CONTROL: the stranger's own edge still reads.
	if _, err := gs2.GetEdgeForTenant(decoy.ID, "stranger"); err != nil {
		t.Fatalf("the stranger's own edge must still read: %v", err)
	}
}

// verifyNodeExistsForTenant is the third guard site and the only one no reader
// test reaches. CreateEdgeWithTenant calls it with a caller-supplied node ID,
// and pkg/api/handlers_edges.go branches POST /edges on the error class, so an
// unguarded site lets tenant "stranger" learn that a node of tenant "owner"
// exists by sending an edge create and reading 500 instead of 404.
//
// The site also inverts the condition against the four readers, because it
// reports the unreadable error in the POSITIVE branch. An inverted polarity is
// the easiest error to make in this shape, so both assertions below matter: a
// blanket ErrNodeNotFound fails the owner case, and an inverted guard fails
// both.
func TestDamagedRecordDoesNotLeakThroughEdgeCreate(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	damaged, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create damaged: %v", err)
	}
	ownerOther, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create owner other: %v", err)
	}
	// The stranger owns nodes of its own, so its membership run exists and the
	// refusal below is about the target ID, not about a missing run.
	strangerA, err := gs.CreateNodeWithTenant("stranger", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create stranger a: %v", err)
	}
	strangerB, err := gs.CreateNodeWithTenant("stranger", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create stranger b: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(damaged.ID)
	if !ok {
		t.Fatalf("node %d has no directory entry", damaged.ID)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption.
	if _, decErr := decodeNodeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	// The OWNING tenant still learns that the endpoint is unreadable. Without
	// this assertion a blanket ErrNodeNotFound would pass.
	_, err = gs2.CreateEdgeWithTenant("owner", damaged.ID, ownerOther.ID, "LINK", nil, 1)
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("the owning tenant must still get ErrRecordUnreadable, got %v", err)
	}

	// The STRANGER learns nothing about the owner's node.
	_, err = gs2.CreateEdgeWithTenant("stranger", damaged.ID, strangerA.ID, "LINK", nil, 1)
	if errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("edge create leaked a damaged node across tenants: %v", err)
	}
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("want ErrNodeNotFound for a cross-tenant endpoint, got %v", err)
	}

	// The same in the target position, because CreateEdgeWithTenant verifies
	// the two endpoints in separate calls.
	_, err = gs2.CreateEdgeWithTenant("stranger", strangerA.ID, damaged.ID, "LINK", nil, 1)
	if errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("edge create leaked a damaged target node across tenants: %v", err)
	}
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("want ErrNodeNotFound for a cross-tenant target, got %v", err)
	}

	// NEGATIVE CONTROL: the stranger can still create an edge between its own
	// nodes, so the reopen worked and the guard refuses only what it should.
	if _, err := gs2.CreateEdgeWithTenant("stranger", strangerA.ID, strangerB.ID, "LINK", nil, 1); err != nil {
		t.Fatalf("the stranger's own edge must still create: %v", err)
	}
}

// TestCheckInvariantsReportsADamagedRecord is the fifth guard site: the
// invariant checker itself. checkInvariantsMmap walked every base record and
// silently dropped the ones that would not decode, so a damaged record read
// as a missing node to the checker too, and CheckInvariants reported a clean
// store over a damaged file.
//
// This test calls CheckInvariants across a reopen. USAGE CONSTRAINT 3 on
// CheckInvariants warns against that in general, because a reopen rebuilds
// the shard-path derived indexes and self-heals drift — the thing the shard
// path hunts for. That warning does not apply here. The mmap path this test
// exercises only exists after a reopen, and the damaged record lives in the
// base snapshot, so a reopen is the only way to reach the code under test.
// CONSUMER CONTRACT: CC11-unreadable-not-missing — all readers (PR B)
func TestCheckInvariantsReportsADamagedRecord(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
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
	// Byte 8 of a node record is the tenant-length prefix, as in
	// TestDamagedRecordIsNotReportedAsMissing.
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

	violations, err := CheckInvariants(gs2)
	if err != nil {
		t.Fatalf("CheckInvariants refused to run: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("a damaged record must be an invariant violation, not a missing node")
	}
	// A plain ID-in-the-message check is too weak: even before the fix, the
	// checker already reports a DIFFERENT violation that happens to name the
	// ID ("membershipNodeIDsForTenant returned id 1, which is not a live
	// record") — a real finding about ground truth being under-populated,
	// but not the specific "the record does not decode" report this fix
	// adds. Require the decode-failure wording so the test cannot pass on
	// that other violation.
	idStr := strconv.FormatUint(target.ID, 10)
	found := false
	for _, v := range violations {
		if strings.Contains(v, idStr) && strings.Contains(v, recordDoesNotDecodePhrase) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no violation reports record %d as undecodable: %v", target.ID, violations)
	}
}

// TestCheckInvariantsCleanOnUndamagedStore is the POSITIVE CONTROL for
// TestCheckInvariantsReportsADamagedRecord. Without it, a checker that
// reported a violation for every record — damaged or not — would also pass
// the test above.
//
// This test alone does not prove the checker inspects anything. A VACUOUS
// checker that always returns zero violations would also pass it. What
// rules that out is the sibling test's own assertion, which requires
// len(violations) != 0 for a damaged store. Read the two tests as one
// pair; neither proves the checker's behaviour on its own.
func TestCheckInvariantsCleanOnUndamagedStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	violations, err := CheckInvariants(gs2)
	if err != nil {
		t.Fatalf("CheckInvariants refused to run: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("an undamaged store must report no violations, got: %v", violations)
	}
}

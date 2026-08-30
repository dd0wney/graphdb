package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The gate for ADR 0003.
//
// damaged_record_test.go covers the SINGLE-record readers: GetNodeForTenant on
// a damaged ID returns ErrRecordUnreadable instead of collapsing to
// ErrNodeNotFound. That says nothing about the enumerations, which never named
// an ID at all: each walked a membership list, skipped what would not decode,
// and returned a shorter slice with no error to carry the news. An incomplete
// enumeration was byte-indistinguishable from a complete one, so no assertion
// in this repository could tell them apart.
//
// The tests below assert BOTH halves of the ADR's contract, and the second
// half is the one that stops a lazy fix:
//
//   - the enumeration REPORTS the skipped record — a non-nil error that wraps
//     ErrRecordUnreadable and names the offending ID;
//   - the enumeration still RETURNS the records that did decode — one damaged
//     record must not make a whole tenant unreadable.
//
// A fix that returned (nil, err) would pass the first assertion and fail the
// second. A fix that kept the old silence would fail the first.

// enumerationFixture is a reopened mmap store with one damaged node record and
// one damaged edge record, both owned by tenant "owner".
type enumerationFixture struct {
	gs *GraphStorage

	// damagedNode and damagedEdge no longer decode.
	damagedNode uint64
	damagedEdge uint64

	// intactNodes and intactEdges are what a complete enumeration must still
	// return, in ascending ID order.
	intactNodes []uint64
	intactEdges []uint64
}

// newEnumerationFixture builds three "Thing" nodes and two "LINK" edges for
// tenant "owner", closes the store so the snapshot lands on disk, damages the
// SECOND node and the FIRST edge, and reopens.
//
// The damaged node is the middle one on purpose. A first or last record would
// let an off-by-one in the walk look like a correct skip.
func newEnumerationFixture(t *testing.T) enumerationFixture {
	t.Helper()

	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	nodes := make([]uint64, 0, 3)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		n, cerr := gs.CreateNodeWithTenant("owner", []string{"Thing"},
			map[string]Value{"name": StringValue(name)})
		if cerr != nil {
			t.Fatalf("create node %q: %v", name, cerr)
		}
		nodes = append(nodes, n.ID)
	}

	edges := make([]uint64, 0, 2)
	for i := 0; i < 2; i++ {
		// Both edges join the two UNDAMAGED nodes. An edge hanging off the
		// damaged node would give every edge assertion two possible causes.
		e, cerr := gs.CreateEdgeWithTenant("owner", nodes[0], nodes[2], "LINK", nil, 1)
		if cerr != nil {
			t.Fatalf("create edge %d: %v", i, cerr)
		}
		edges = append(edges, e.ID)
	}

	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	damagedNode, damagedEdge := nodes[1], edges[0]
	path := mmapSnapshotPath(dir)

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	nodeOff, ok := snap.nodeOffset(damagedNode)
	if !ok {
		t.Fatalf("node %d has no directory entry", damagedNode)
	}
	edgeOff, ok := snap.edgeOffset(damagedEdge)
	if !ok {
		t.Fatalf("edge %d has no directory entry", damagedEdge)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	// Byte 8 of a node OR edge record is the uint16 tenant-length prefix.
	// 0xFFFF asks for more bytes than the file holds, so the decoder's bounds
	// check refuses the record. The CRC does not cover record bodies, so the
	// file still opens.
	binary.LittleEndian.PutUint16(raw[nodeOff+8:], 0xFFFF)
	binary.LittleEndian.PutUint16(raw[edgeOff+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	// POSITIVE CONTROL on the injector. Without it, a test whose corruption
	// silently stopped working would report a clean pass for every row below.
	if _, decErr := decodeNodeRecordAt(raw, nodeOff); decErr == nil {
		t.Fatalf("the node fault did not take: record at %d still decodes", nodeOff)
	}
	if _, decErr := decodeEdgeRecordAt(raw, edgeOff); decErr == nil {
		t.Fatalf("the edge fault did not take: record at %d still decodes", edgeOff)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = gs2.Close() })

	// NEGATIVE CONTROL on the reopen. A reopen that lost everything, or a
	// fault that damaged the whole file, would look exactly like a clean pass
	// on the "reports the damage" half.
	if _, err := gs2.GetNodeForTenant(nodes[0], "owner"); err != nil {
		t.Fatalf("the first undamaged node must still read: %v", err)
	}
	if _, err := gs2.GetNodeForTenant(nodes[2], "owner"); err != nil {
		t.Fatalf("the third undamaged node must still read: %v", err)
	}

	return enumerationFixture{
		gs:          gs2,
		damagedNode: damagedNode,
		damagedEdge: damagedEdge,
		intactNodes: []uint64{nodes[0], nodes[2]},
		intactEdges: []uint64{edges[1]},
	}
}

// assertReportsDamage checks the "the error is non-nil and names the ID" half.
func assertReportsDamage(t *testing.T, what string, err error, damagedID uint64) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s returned a short result and a nil error: the enumeration is "+
			"incomplete and the caller cannot tell", what)
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Errorf("%s: want an error wrapping ErrRecordUnreadable, got %v", what, err)
	}
	if id := strconv.FormatUint(damagedID, 10); !strings.Contains(err.Error(), id) {
		t.Errorf("%s: the error does not name the offending record %d: %v", what, damagedID, err)
	}
}

// assertNodeIDs checks the "the partial result is still usable" half.
func assertNodeIDs(t *testing.T, what string, got []*Node, want []uint64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s returned %d nodes, want %d: a damaged record must not cost "+
			"the caller the records that DID decode", what, len(got), len(want))
	}
	for i, n := range got {
		if n.ID != want[i] {
			t.Errorf("%s node %d: got ID %d, want %d", what, i, n.ID, want[i])
		}
	}
}

func assertEdgeIDs(t *testing.T, what string, got []*Edge, want []uint64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s returned %d edges, want %d: a damaged record must not cost "+
			"the caller the records that DID decode", what, len(got), len(want))
	}
	for i, e := range got {
		if e.ID != want[i] {
			t.Errorf("%s edge %d: got ID %d, want %d", what, i, e.ID, want[i])
		}
	}
}

// TestEnumerationsReportADamagedRecord is the ADR 0003 gate. It covers all
// seven methods, because the defect was in all seven and a fix to one proves
// nothing about the other six.
// CONSUMER CONTRACT: CC13-enumeration-reports-incompleteness — all readers (ADR 0003)
func TestEnumerationsReportADamagedRecord(t *testing.T) {
	t.Run("GetAllNodesForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		nodes, err := f.gs.GetAllNodesForTenant("owner")
		assertReportsDamage(t, "GetAllNodesForTenant", err, f.damagedNode)
		assertNodeIDs(t, "GetAllNodesForTenant", nodes, f.intactNodes)
	})

	t.Run("GetNodesByLabelForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		nodes, err := f.gs.GetNodesByLabelForTenant("owner", "Thing")
		assertReportsDamage(t, "GetNodesByLabelForTenant", err, f.damagedNode)
		assertNodeIDs(t, "GetNodesByLabelForTenant", nodes, f.intactNodes)
	})

	t.Run("GetAllEdgesForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		edges, err := f.gs.GetAllEdgesForTenant("owner")
		assertReportsDamage(t, "GetAllEdgesForTenant", err, f.damagedEdge)
		assertEdgeIDs(t, "GetAllEdgesForTenant", edges, f.intactEdges)
	})

	// The page methods take a limit large enough to reach every record, so the
	// damaged one is inside the scanned window rather than beyond it.
	const wholeTenant = 100

	t.Run("NodesPageForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		nodes, _, err := f.gs.NodesPageForTenant("owner", 0, wholeTenant)
		assertReportsDamage(t, "NodesPageForTenant", err, f.damagedNode)
		assertNodeIDs(t, "NodesPageForTenant", nodes, f.intactNodes)
	})

	t.Run("NodesByLabelPageForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		nodes, _, err := f.gs.NodesByLabelPageForTenant("owner", "Thing", 0, wholeTenant)
		assertReportsDamage(t, "NodesByLabelPageForTenant", err, f.damagedNode)
		assertNodeIDs(t, "NodesByLabelPageForTenant", nodes, f.intactNodes)
	})

	t.Run("EdgesPageForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		edges, _, err := f.gs.EdgesPageForTenant("owner", 0, wholeTenant)
		assertReportsDamage(t, "EdgesPageForTenant", err, f.damagedEdge)
		assertEdgeIDs(t, "EdgesPageForTenant", edges, f.intactEdges)
	})

	t.Run("EdgesByTypePageForTenant", func(t *testing.T) {
		f := newEnumerationFixture(t)
		edges, _, err := f.gs.EdgesByTypePageForTenant("owner", "LINK", 0, wholeTenant)
		assertReportsDamage(t, "EdgesByTypePageForTenant", err, f.damagedEdge)
		assertEdgeIDs(t, "EdgesByTypePageForTenant", edges, f.intactEdges)
	})
}

// TestEnumerationsReportNilOnAnIntactStore is the NEGATIVE CONTROL for the
// test above.
//
// Without it, an implementation that returned a non-nil error from every
// enumeration — damaged store or not — would pass every row above while
// destroying the signal. nil now MEANS complete, and that claim needs its own
// assertion.
// CONSUMER CONTRACT: CC13-enumeration-reports-incompleteness — all readers (ADR 0003)
func TestEnumerationsReportNilOnAnIntactStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	var nodeIDs []uint64
	for _, name := range []string{"alpha", "beta", "gamma"} {
		n, cerr := gs.CreateNodeWithTenant("owner", []string{"Thing"},
			map[string]Value{"name": StringValue(name)})
		if cerr != nil {
			t.Fatalf("create node %q: %v", name, cerr)
		}
		nodeIDs = append(nodeIDs, n.ID)
	}
	e, err := gs.CreateEdgeWithTenant("owner", nodeIDs[0], nodeIDs[2], "LINK", nil, 1)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
	edgeIDs := []uint64{e.ID}

	const wholeTenant = 100

	nodes, err := gs.GetAllNodesForTenant("owner")
	if err != nil {
		t.Errorf("GetAllNodesForTenant on an intact store must report nil: %v", err)
	}
	assertNodeIDs(t, "GetAllNodesForTenant", nodes, nodeIDs)

	nodes, err = gs.GetNodesByLabelForTenant("owner", "Thing")
	if err != nil {
		t.Errorf("GetNodesByLabelForTenant on an intact store must report nil: %v", err)
	}
	assertNodeIDs(t, "GetNodesByLabelForTenant", nodes, nodeIDs)

	edges, err := gs.GetAllEdgesForTenant("owner")
	if err != nil {
		t.Errorf("GetAllEdgesForTenant on an intact store must report nil: %v", err)
	}
	assertEdgeIDs(t, "GetAllEdgesForTenant", edges, edgeIDs)

	nodes, _, err = gs.NodesPageForTenant("owner", 0, wholeTenant)
	if err != nil {
		t.Errorf("NodesPageForTenant on an intact store must report nil: %v", err)
	}
	assertNodeIDs(t, "NodesPageForTenant", nodes, nodeIDs)

	nodes, _, err = gs.NodesByLabelPageForTenant("owner", "Thing", 0, wholeTenant)
	if err != nil {
		t.Errorf("NodesByLabelPageForTenant on an intact store must report nil: %v", err)
	}
	assertNodeIDs(t, "NodesByLabelPageForTenant", nodes, nodeIDs)

	edges, _, err = gs.EdgesPageForTenant("owner", 0, wholeTenant)
	if err != nil {
		t.Errorf("EdgesPageForTenant on an intact store must report nil: %v", err)
	}
	assertEdgeIDs(t, "EdgesPageForTenant", edges, edgeIDs)

	edges, _, err = gs.EdgesByTypePageForTenant("owner", "LINK", 0, wholeTenant)
	if err != nil {
		t.Errorf("EdgesByTypePageForTenant on an intact store must report nil: %v", err)
	}
	assertEdgeIDs(t, "EdgesByTypePageForTenant", edges, edgeIDs)
}

// TestEnumerationDoesNotReportAConcurrentDelete separates the two reasons a
// record can be missing from an enumeration.
//
// A record deleted between the ID collection and the read is the documented
// non-atomic-snapshot tradeoff, and it has been correct behaviour since A4. If
// note() reported it as damage, every concurrent delete would look like
// corruption to a caller, and the new error would be noise rather than a
// signal. This test is what keeps the classifier honest.
func TestEnumerationDoesNotReportAConcurrentDelete(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	var ids []uint64
	for i := 0; i < 3; i++ {
		n, cerr := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
		if cerr != nil {
			t.Fatalf("create node %d: %v", i, cerr)
		}
		ids = append(ids, n.ID)
	}
	if derr := gs.DeleteNodeForTenant(ids[1], "owner"); derr != nil {
		t.Fatalf("delete the middle node: %v", derr)
	}

	nodes, err := gs.GetAllNodesForTenant("owner")
	if err != nil {
		t.Fatalf("a deleted node is absence, not damage, and must not produce an "+
			"enumeration error: %v", err)
	}
	assertNodeIDs(t, "GetAllNodesForTenant after a delete", nodes,
		[]uint64{ids[0], ids[2]})
}

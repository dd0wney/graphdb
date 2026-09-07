package storage

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// assertGraphInvariants verifies that every DERIVED representation of the graph
// agrees with the AUTHORITATIVE shard maps, failing the test with one error per
// divergence. See CheckInvariants for the full contract; this is the thin
// *testing.T wrapper retrofitted into tests.
func assertGraphInvariants(t *testing.T, gs *GraphStorage) {
	t.Helper()
	for _, v := range mustCheckInvariants(t, gs) {
		t.Error("invariant violation: " + v)
	}
}

// mustCheckInvariants runs CheckInvariants and fails the test if the store is
// one the checker cannot inspect. A test that silently skipped the check would
// look identical to a test that passed it.
func mustCheckInvariants(t *testing.T, gs *GraphStorage) []string {
	t.Helper()
	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	return violations
}

// TestCheckInvariants_InspectsMmapBackedStore is REVERSED from what it asserted
// when it was written. It used to pin the refusal: an mmap-backed store got
// ErrInvariantsUnsupported because ground truth was rebuilt from the shard maps,
// which an mmap store fills on demand.
//
// The checker now builds mmap ground truth from the raw records instead, so the
// refusal is gone for a snapshot that carries a membership directory. The test
// is kept rather than replaced so the comment records what changed.
func TestCheckInvariants_InspectsMmapBackedStore(t *testing.T) {
	reopened := mmapStoreWithData(t)

	violations, err := CheckInvariants(reopened)
	if err != nil {
		t.Fatalf("CheckInvariants on an mmap-backed store: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none on a healthy mmap store", violations)
	}
}

// TestCheckInvariants_MmapCleanAfterOverlayWrites is the correctness test for
// the base-union-overlay-minus-tombstone rule. A store that has been written to
// since reopen holds records in three states at once: untouched in the mmap
// base, promoted into a shard, and tombstoned. All three must reconcile.
func TestCheckInvariants_MmapCleanAfterOverlayWrites(t *testing.T) {
	gs := mmapStoreWithData(t)

	// Promote a base-resident node into the overlay by updating it.
	if err := gs.UpdateNode(1, map[string]Value{"n": StringValue("updated")}); err != nil {
		t.Fatalf("UpdateNode(1): %v", err)
	}
	// Tombstone a base-resident node.
	if err := gs.DeleteNode(2); err != nil {
		t.Fatalf("DeleteNode(2): %v", err)
	}
	// Create a node that exists only in the overlay.
	if _, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"n": StringValue("new")}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants after overlay writes: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none after update + delete + create", violations)
	}
}

// --- teeth: the checker must actually fire ---------------------------------
//
// A checker nobody has watched fail is not yet a checker. Each test below
// corrupts one index by hand and requires a violation naming it.

// TestCheckInvariantsMmap_TeethLabelIndexInvention catches an index that claims
// a node it does not have.
func TestCheckInvariantsMmap_TeethLabelIndexInvention(t *testing.T) {
	gs := mmapStoreWithData(t)

	tid := effectiveTenantID(DefaultTenantID)
	gs.mu.Lock()
	if gs.tenantNodesByLabel[tid] == nil {
		gs.tenantNodesByLabel[tid] = map[string]map[uint64]struct{}{}
	}
	if gs.tenantNodesByLabel[tid]["Thing"] == nil {
		gs.tenantNodesByLabel[tid]["Thing"] = map[uint64]struct{}{}
	}
	gs.tenantNodesByLabel[tid]["Thing"][999999] = struct{}{}
	gs.mu.Unlock()

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !anyContains(violations, "999999") {
		t.Errorf("no violation named the invented id 999999; got %v", violations)
	}
}

// TestCheckInvariantsMmap_TeethTenantIndexInvention is the same corruption one
// level up, in the per-tenant node set.
func TestCheckInvariantsMmap_TeethTenantIndexInvention(t *testing.T) {
	gs := mmapStoreWithData(t)

	tid := effectiveTenantID(DefaultTenantID)
	gs.mu.Lock()
	if gs.tenantNodeIDs[tid] == nil {
		gs.tenantNodeIDs[tid] = map[uint64]struct{}{}
	}
	gs.tenantNodeIDs[tid][888888] = struct{}{}
	gs.mu.Unlock()

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !anyContains(violations, "888888") {
		t.Errorf("no violation named the invented id 888888; got %v", violations)
	}
}

// TestCheckInvariantsMmap_TeethDanglingEdge catches an edge whose endpoint does
// not resolve — the shape a cascade delete leaves if it misses a reference.
func TestCheckInvariantsMmap_TeethDanglingEdge(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	gs.storeEdgeInShard(&Edge{
		ID:         777777,
		TenantID:   DefaultTenantID,
		FromNodeID: 1,
		ToNodeID:   555555, // no such node
		Type:       "POINTS_NOWHERE",
	})
	gs.mu.Unlock()

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !anyContains(violations, "555555") {
		t.Errorf("no violation named the dangling endpoint 555555; got %v", violations)
	}
}

// TestCheckInvariantsMmap_TeethIndexOmission is the direction the other teeth
// tests cannot prove. They corrupt an index by ADDING an id, which fires even
// if ground truth is empty — so passing them is consistent with a checker that
// reconstructs nothing. This test removes a live record from the index instead,
// so it can only pass if ground truth actually holds that record.
//
// Without this, the mmap checker could repeat the exact defect it was built to
// fix: comparing an empty ground truth and reporting health.
func TestCheckInvariantsMmap_TeethIndexOmission(t *testing.T) {
	gs := mmapStoreWithData(t)

	// A node created after reopen lives in the overlay shard, so ground truth
	// must find it there.
	created, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"n": StringValue("overlay")})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	tid := effectiveTenantID(DefaultTenantID)
	gs.mu.Lock()
	delete(gs.tenantNodeIDs[tid], created.ID)
	if lm := gs.tenantNodesByLabel[tid]; lm != nil && lm["Thing"] != nil {
		delete(lm["Thing"], created.ID)
	}
	gs.mu.Unlock()

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !anyContains(violations, "omitted live id") {
		t.Errorf("checker did not report the omitted live record; got %v", violations)
	}
	if !anyContains(violations, fmt.Sprintf("%d", created.ID)) {
		t.Errorf("no violation named the omitted id %d; got %v", created.ID, violations)
	}
}

// mmapStoreWithData builds a store, closes it so snapshot.mmap is written, and
// returns it reopened on the mmap path with data resident in the base.
func mmapStoreWithData(t *testing.T) *GraphStorage {
	t.Helper()
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	// Every derived structure the mmap checker covers has a member here, so
	// TestCheckInvariants_InspectsMmapBackedStore is a positive control for each
	// of them: a property index on "n", a vector index on "embedding", two
	// labels, one edge type and one edge.
	if err := gs.CreatePropertyIndex("n", TypeString); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	if err := gs.CreateVectorIndexForTenant(DefaultTenantID, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	n1, err := gs.CreateNode([]string{"Thing"}, map[string]Value{
		"n":         StringValue("one"),
		"embedding": VectorValue([]float32{1, 0, 0}),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	n2, err := gs.CreateNode([]string{"Thing", "Other"}, map[string]Value{
		"n":         StringValue("two"),
		"embedding": VectorValue([]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if _, err := gs.CreateEdge(n1.ID, n2.ID, "LINKS", nil, 1.0); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if reopened.mmapSnap == nil {
		t.Skip("reopen did not take the mmap path; this test has nothing to check")
	}
	return reopened
}

func anyContains(violations []string, want string) bool {
	for _, v := range violations {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}

// TestCheckInvariants_InspectsJSONBackedStore is the positive control for the
// test above: the guard must be specific to the mmap path, not a blanket
// refusal that quietly disables the checker everywhere.
func TestCheckInvariants_InspectsJSONBackedStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if _, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"n": StringValue("one")}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants on a JSON-backed store: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none on a healthy store", violations)
	}
}

// --- teeth for the derived structures the mmap path gained after #474 --------
//
// Each test corrupts one derived structure by hand and requires a violation
// naming what it broke. The healthy fixture carries a member of every one of
// them (see mmapStoreWithData), so TestCheckInvariants_InspectsMmapBackedStore
// is the positive control: it proves each check runs and passes on a good
// store, and these prove each check can fail.

// TestCheckInvariantsMmap_TeethAdjacencyOverlayOmission removes a post-reopen
// edge from the overlay adjacency map while its record stays live. That is the
// shape a write path leaves when it stores the record and forgets the index.
func TestCheckInvariantsMmap_TeethAdjacencyOverlayOmission(t *testing.T) {
	gs := mmapStoreWithData(t)

	back, err := gs.CreateEdge(2, 1, "BACK", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	gs.mu.Lock()
	gs.outgoingEdges[2] = withoutID(gs.outgoingEdges[2], back.ID)
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	want := fmt.Sprintf("edge %d missing from node 2 outgoing adjacency", back.ID)
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// TestCheckInvariantsMmap_TeethAdjacencyBaseMismatch proves the checker reads
// the CSR base and not only the overlay map. Edge 1 (node 1 -> node 2) is
// served from the snapshot's CSR run. Shadowing its record in the shard with
// the endpoints reversed makes ground truth say its source is node 2, while
// node 1's outgoing run still lists it.
func TestCheckInvariantsMmap_TeethAdjacencyBaseMismatch(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	gs.storeEdgeInShard(&Edge{
		ID:         1,
		TenantID:   DefaultTenantID,
		FromNodeID: 2,
		ToNodeID:   1,
		Type:       "LINKS",
	})
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	want := "node 1 outgoing lists edge 1 whose source is actually 2"
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// TestCheckInvariantsMmap_TeethAdjacencyDangling plants an edge id in a base
// node's overlay adjacency that no record backs: the #307 class, a cascade
// delete that misses a reference.
func TestCheckInvariantsMmap_TeethAdjacencyDangling(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	gs.incomingEdges[1] = append(gs.incomingEdges[1], 777777)
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	want := "node 1 incoming lists edge 777777 that no longer exists (dangling)"
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// TestCheckInvariantsMmap_TeethVectorIndexOmission drops one base node's vector
// from the HNSW graph. The count check must see Len() fall below the number of
// live nodes carrying a decodable vector on the indexed property.
func TestCheckInvariantsMmap_TeethVectorIndexOmission(t *testing.T) {
	gs := mmapStoreWithData(t)

	tid := effectiveTenantID(DefaultTenantID)
	if err := gs.vectorIndex.RemoveVectorForTenant(tid, "embedding", 1); err != nil {
		t.Fatalf("RemoveVectorForTenant: %v", err)
	}

	violations := mustCheckInvariants(t, gs)
	want := fmt.Sprintf("vector: index (tenant %q, prop %q) Len()=1 != decodable-vector node count=2", tid, "embedding")
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// TestCheckInvariantsMmap_TeethVectorIndexInvention adds a vector under a node
// id that no record backs.
func TestCheckInvariantsMmap_TeethVectorIndexInvention(t *testing.T) {
	gs := mmapStoreWithData(t)

	tid := effectiveTenantID(DefaultTenantID)
	if err := gs.vectorIndex.AddVectorForTenant(tid, "embedding", 999999, []float32{0, 0, 1}); err != nil {
		t.Fatalf("AddVectorForTenant: %v", err)
	}

	violations := mustCheckInvariants(t, gs)
	want := fmt.Sprintf("vector: index (tenant %q, prop %q) Len()=3 != decodable-vector node count=2", tid, "embedding")
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// TestCheckInvariantsMmap_TeethPropertyIndexOmission deletes the bucket that
// holds node 1's value, so the index no longer lists a live node that carries
// an indexed value.
func TestCheckInvariantsMmap_TeethPropertyIndexOmission(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	idx := gs.propertyIndexes["n"]
	key := idx.valueToKey(StringValue("one"))
	idx.mu.Lock()
	delete(idx.index, key)
	idx.mu.Unlock()
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	want := fmt.Sprintf("property %q: node 1 missing from bucket %q", "n", key)
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// TestCheckInvariantsMmap_TeethPropertyIndexInvention appends an id no live
// node backs to a real bucket.
func TestCheckInvariantsMmap_TeethPropertyIndexInvention(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	idx := gs.propertyIndexes["n"]
	key := idx.valueToKey(StringValue("one"))
	idx.mu.Lock()
	idx.index[key] = append(idx.index[key], 999999)
	idx.mu.Unlock()
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	want := fmt.Sprintf("property %q bucket %q: lists id 999999 not backed by a live node carrying that value", "n", key)
	if !anyContains(violations, want) {
		t.Errorf("no violation %q; got %v", want, violations)
	}
}

// --- teeth for the count chains the mmap path gained after #569 ------------
//
// checkInvariantsMmap already built the live node/edge sets these tests need;
// only the comparison against stats/tenantStats was missing. Each test
// corrupts one counter in isolation and requires a violation naming it.

// TestCheckInvariantsMmap_TeethStatsNodeCountDrift bumps the global node
// counter without adding a node, the shape a write path leaves if it updates
// the record but forgets the counter.
func TestCheckInvariantsMmap_TeethStatsNodeCountDrift(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	atomic.AddUint64(&gs.stats.NodeCount, 1)
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	if !anyContains(violations, "stats.NodeCount") {
		t.Errorf("no violation named stats.NodeCount; got %v", violations)
	}
}

// TestCheckInvariantsMmap_TeethTenantStatsNodeCountDrift is the same
// corruption one level down, in the per-tenant counter.
func TestCheckInvariantsMmap_TeethTenantStatsNodeCountDrift(t *testing.T) {
	gs := mmapStoreWithData(t)

	tid := effectiveTenantID(DefaultTenantID)
	gs.mu.Lock()
	atomic.AddUint64(&gs.tenantStats[tid].NodeCount, 1)
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	if !anyContains(violations, "tenantStats.NodeCount") {
		t.Errorf("no violation named tenantStats.NodeCount; got %v", violations)
	}
	if !anyContains(violations, string(tid)) {
		t.Errorf("no violation named tenant %q; got %v", tid, violations)
	}
}

// TestCheckInvariantsMmap_TeethTenantStatsMissing drops a tenant's whole
// tenantStats entry while the tenant still has live nodes — the shape left
// behind if a counter map is cleared without clearing the nodes it counts.
func TestCheckInvariantsMmap_TeethTenantStatsMissing(t *testing.T) {
	gs := mmapStoreWithData(t)

	tid := effectiveTenantID(DefaultTenantID)
	gs.mu.Lock()
	delete(gs.tenantStats, tid)
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	if !anyContains(violations, "tenantStats.NodeCount") {
		t.Errorf("no violation named tenantStats.NodeCount; got %v", violations)
	}
	if !anyContains(violations, string(tid)) {
		t.Errorf("no violation named tenant %q; got %v", tid, violations)
	}
}

// --- teeth for the sticky global label/type keys the mmap path gained after
// #569 ------------------------------------------------------------------
//
// loadFromDiskMmap registers a sticky KEY (possibly with an empty bucket) for
// every label/type live at the last Close (meta.StickyNodeLabels /
// StickyEdgeTypes); writes made after open add real members through the same
// addToLabelIndex the shard path uses. gs.nodesByLabel backs GetAllLabels
// (query_operations.go), read directly on both representations. gs.edgesByType
// has no direct external reader today — its only reader is
// mmap_snapshot_writer.go, which uses it to build the sticky-type list for the
// next snapshot write. Either way a key dropped from either map is invisible
// to the membership-section checks above, which never look at these maps.

// TestCheckInvariantsMmap_TeethStickyLabelKeyDropped deletes a live label's
// key from the global index that backs GetAllLabels.
func TestCheckInvariantsMmap_TeethStickyLabelKeyDropped(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	delete(gs.nodesByLabel, "Thing")
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	if !anyContains(violations, "Thing") {
		t.Errorf("no violation named the dropped label %q; got %v", "Thing", violations)
	}
}

// TestCheckInvariantsMmap_TeethStickyTypeKeyDropped is the same corruption for
// the global edge-type index that the mmap snapshot writer reads to build the
// sticky-type list for the next write.
func TestCheckInvariantsMmap_TeethStickyTypeKeyDropped(t *testing.T) {
	gs := mmapStoreWithData(t)

	gs.mu.Lock()
	delete(gs.edgesByType, "LINKS")
	gs.mu.Unlock()

	violations := mustCheckInvariants(t, gs)
	if !anyContains(violations, "LINKS") {
		t.Errorf("no violation named the dropped type %q; got %v", "LINKS", violations)
	}
}

// withoutID returns a copy of ids with every occurrence of id removed.
func withoutID(ids []uint64, id uint64) []uint64 {
	out := make([]uint64, 0, len(ids))
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}

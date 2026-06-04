package storage

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// This file is Phase C of the parallel-invariant test work (Phases A/B live in
// invariants_test.go + invariant_matrix_test.go). Those drive the count-derived
// invariant checker through the write-path × op matrix; this file closes the gap
// the checker CANNOT see.
//
// The checker's vector assertion is COUNT-ONLY (HNSWIndex.Len() vs the number of
// decodable-vector nodes). A write path that re-indexes a vector under the WRONG
// VALUE but the right count — the exact failure mode #305's persistence bug could
// have produced — passes the checker silently. The only observable that exposes
// it is the search RESULT: which nodes come back, in which order.
//
// So this file is METAMORPHIC: apply one logical op-script through each write
// path (live / batch / transaction / WAL-replay) and assert the paths produce
// OBSERVATIONALLY IDENTICAL graphs — equal per-tenant counts, equal per-label
// membership, and — the load-bearing check — identical VectorSearchForTenant
// top-k neighbours. No path is the oracle; they cross-check each other. A path
// that drops a vector update or mishandles a cascade delete breaks the relation.
//
// CROSS-PATH EQUIVALENCE (internal invariant — deliberately NOT a `CONSUMER
// CONTRACT`: no downstream consumer depends on it, so it earns no
// docs/CONSUMER_CONTRACTS.md row). The pinned guarantee: live / batch /
// transaction / WAL-replay produce observationally identical graphs for the same
// op-script. Grep "CROSS-PATH EQUIVALENCE" to find where it is enforced.
//
// Op-support differs per path (transactions cannot delete), so the script is run
// in two groups: a no-delete group across all four paths, and a with-delete group
// across the three that support deletes (transaction logged-skipped). The query
// (1, 0.5, 0) and the maximally-separated planted vectors are chosen so the top-k
// ordering is unambiguous (no HNSW tie-flake across insertion orders) AND so the
// script's vector update is search-observable — proven non-vacuous by
// TestMetamorphic_VectorUpdateIsSearchObservable below.

// mmQuery is the fixed search query for every metamorphic comparison. With the
// planted vectors it yields a strict, tie-free neighbour ordering.
var mmQuery = []float32{1, 0.5, 0}

// mmProps builds an embedding-carrying property map (nil vec => no properties).
func mmProps(vec []float32) map[string]Value {
	if vec == nil {
		return nil
	}
	return map[string]Value{"embedding": VectorValue(vec)}
}

// --- the shared op-script -------------------------------------------------
//
// Logical ops reference nodes/edges by string handle so a path that assigns
// different concrete IDs is still comparable (results are translated back to
// handles before comparison). Phase 1 plants three maximally-separated vectors
// + one edge; phase 2 moves n2 to become the query's nearest neighbour (the
// search-observable update) and, in the with-delete variant, deletes n0 while
// its edge is live — exercising the cascade path that produced #307/#308.

func runMetamorphicScript(d metamorphicDriver, withDelete bool) {
	d.beginPhase()
	d.createNode("n0", []string{"Doc"}, []float32{1, 0, 0})
	d.createNode("n1", []string{"Doc"}, []float32{0, 1, 0})
	d.createNode("n2", []string{"Doc"}, []float32{0, 0, 1})
	d.createEdge("e0", "n0", "n1")
	d.commitPhase()

	d.beginPhase()
	d.updateVec("n2", []float32{1, 0.9, 0}) // n2 -> the query's nearest neighbour
	if withDelete {
		d.deleteNode("n0") // cascades edge e0 (n0 -> n1)
	}
	d.commitPhase()
}

// metamorphicDriver applies the logical script through one concrete write path.
// beginPhase/commitPhase bracket a barrier (a no-op for the immediately-applied
// live and WAL paths; a Batch/Transaction boundary for the buffered paths).
// finalize returns the storage to QUERY after the path's work is durable (for
// WAL-replay it crashes and recovers, returning the recovered instance).
type metamorphicDriver interface {
	name() string
	beginPhase()
	createNode(handle string, labels []string, vec []float32)
	createEdge(handle, from, to string)
	updateVec(handle string, vec []float32)
	deleteNode(handle string)
	commitPhase()
	finalize() *GraphStorage
	nodeIDs() map[string]uint64
}

// --- shared apply helpers for the immediately-applied paths (live, WAL) ---

func liveCreateNode(t *testing.T, gs *GraphStorage, nid map[string]uint64, handle string, labels []string, vec []float32) {
	t.Helper()
	n, err := gs.CreateNodeWithTenant(DefaultTenantID, labels, mmProps(vec))
	if err != nil {
		t.Fatalf("createNode %s: %v", handle, err)
	}
	nid[handle] = n.ID
}

func liveCreateEdge(t *testing.T, gs *GraphStorage, nid, eid map[string]uint64, handle, from, to string) {
	t.Helper()
	e, err := gs.CreateEdgeWithTenant(DefaultTenantID, nid[from], nid[to], "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("createEdge %s: %v", handle, err)
	}
	eid[handle] = e.ID
}

func liveUpdateVec(t *testing.T, gs *GraphStorage, nid map[string]uint64, handle string, vec []float32) {
	t.Helper()
	if err := gs.UpdateNodeForTenant(nid[handle], mmProps(vec), DefaultTenantID); err != nil {
		t.Fatalf("updateVec %s: %v", handle, err)
	}
}

func liveDeleteNode(t *testing.T, gs *GraphStorage, nid map[string]uint64, handle string) {
	t.Helper()
	if err := gs.DeleteNodeForTenant(nid[handle], DefaultTenantID); err != nil {
		t.Fatalf("deleteNode %s: %v", handle, err)
	}
}

// --- LIVE driver: each op applied directly, no barrier semantics ----------

type liveMDriver struct {
	t   *testing.T
	gs  *GraphStorage
	nid map[string]uint64
	eid map[string]uint64
}

func newLiveMDriver(t *testing.T) *liveMDriver {
	gs := newVectorGraph(t, DefaultTenantID)
	t.Cleanup(func() { _ = gs.Close() })
	return &liveMDriver{t: t, gs: gs, nid: map[string]uint64{}, eid: map[string]uint64{}}
}

func (d *liveMDriver) name() string { return "live" }
func (d *liveMDriver) beginPhase()  {}
func (d *liveMDriver) commitPhase() {}
func (d *liveMDriver) createNode(h string, l []string, v []float32) {
	liveCreateNode(d.t, d.gs, d.nid, h, l, v)
}
func (d *liveMDriver) createEdge(h, from, to string) {
	liveCreateEdge(d.t, d.gs, d.nid, d.eid, h, from, to)
}
func (d *liveMDriver) updateVec(h string, v []float32) { liveUpdateVec(d.t, d.gs, d.nid, h, v) }
func (d *liveMDriver) deleteNode(h string)             { liveDeleteNode(d.t, d.gs, d.nid, h) }
func (d *liveMDriver) finalize() *GraphStorage         { return d.gs }
func (d *liveMDriver) nodeIDs() map[string]uint64      { return d.nid }

// --- BATCH driver: ops buffered per phase, flushed at commitPhase ---------

type batchMDriver struct {
	t   *testing.T
	gs  *GraphStorage
	b   *Batch
	nid map[string]uint64
	eid map[string]uint64
}

func newBatchMDriver(t *testing.T) *batchMDriver {
	gs := newVectorGraph(t, DefaultTenantID)
	t.Cleanup(func() { _ = gs.Close() })
	return &batchMDriver{t: t, gs: gs, nid: map[string]uint64{}, eid: map[string]uint64{}}
}

func (d *batchMDriver) name() string { return "batch" }
func (d *batchMDriver) beginPhase()  { d.b = d.gs.BeginBatch() }
func (d *batchMDriver) commitPhase() {
	if err := d.b.Commit(); err != nil {
		d.t.Fatalf("batch commit: %v", err)
	}
	d.b = nil
}

func (d *batchMDriver) createNode(h string, l []string, v []float32) {
	id, err := d.b.AddNode(l, mmProps(v))
	if err != nil {
		d.t.Fatalf("batch AddNode %s: %v", h, err)
	}
	d.nid[h] = id
}

func (d *batchMDriver) createEdge(h, from, to string) {
	id, err := d.b.AddEdge(d.nid[from], d.nid[to], "LINKS", nil, 1.0)
	if err != nil {
		d.t.Fatalf("batch AddEdge %s: %v", h, err)
	}
	d.eid[h] = id
}

func (d *batchMDriver) updateVec(h string, v []float32) { d.b.UpdateNode(d.nid[h], mmProps(v)) }
func (d *batchMDriver) deleteNode(h string)             { d.b.DeleteNode(d.nid[h]) }
func (d *batchMDriver) finalize() *GraphStorage         { return d.gs }
func (d *batchMDriver) nodeIDs() map[string]uint64      { return d.nid }

// --- TRANSACTION driver: per-phase transaction; no delete support ---------

type txMDriver struct {
	t   *testing.T
	gs  *GraphStorage
	tx  *Transaction
	nid map[string]uint64
	eid map[string]uint64
}

func newTxMDriver(t *testing.T) *txMDriver {
	gs := newVectorGraph(t, DefaultTenantID)
	t.Cleanup(func() { _ = gs.Close() })
	return &txMDriver{t: t, gs: gs, nid: map[string]uint64{}, eid: map[string]uint64{}}
}

func (d *txMDriver) name() string { return "transaction" }

func (d *txMDriver) beginPhase() {
	tx, err := d.gs.BeginTransactionForTenant(DefaultTenantID)
	if err != nil {
		d.t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	d.tx = tx
}

func (d *txMDriver) commitPhase() {
	if err := d.tx.Commit(); err != nil {
		d.t.Fatalf("tx commit: %v", err)
	}
	d.tx = nil
}

func (d *txMDriver) createNode(h string, l []string, v []float32) {
	n, err := d.tx.CreateNode(l, mmProps(v))
	if err != nil {
		d.t.Fatalf("tx CreateNode %s: %v", h, err)
	}
	d.nid[h] = n.ID
}

func (d *txMDriver) createEdge(h, from, to string) {
	e, err := d.tx.CreateEdge(d.nid[from], d.nid[to], "LINKS", nil, 1.0)
	if err != nil {
		d.t.Fatalf("tx CreateEdge %s: %v", h, err)
	}
	d.eid[h] = e.ID
}

func (d *txMDriver) updateVec(h string, v []float32) {
	if err := d.tx.UpdateNode(d.nid[h], mmProps(v)); err != nil {
		d.t.Fatalf("tx UpdateNode %s: %v", h, err)
	}
}

// deleteNode must never be reached: the transaction driver only runs the
// no-delete group. Fail loudly rather than silently no-op if that changes.
func (d *txMDriver) deleteNode(h string) {
	d.t.Fatalf("transaction path has no DeleteNode op (handle %q) — it must not be in a with-delete group", h)
}

func (d *txMDriver) finalize() *GraphStorage    { return d.gs }
func (d *txMDriver) nodeIDs() map[string]uint64 { return d.nid }

// --- WAL-REPLAY driver: ops applied live, then crash + recover ------------

type walMDriver struct {
	t   *testing.T
	dir string
	gs  *GraphStorage // crashable until finalize, then the recovered instance
	nid map[string]uint64
	eid map[string]uint64
}

func newWALMDriver(t *testing.T) *walMDriver {
	dir := t.TempDir()
	// Phase 0: snapshot the vector-index DEFINITION via a clean close, so recovery
	// rebuilds the HNSW graph from the WAL-replayed nodes (the #305 fix) rather
	// than entangling the separate, still-open "CreateVectorIndex not WAL-logged"
	// gap. Matches the matrix WALReplay cell's discipline.
	seed, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("wal seed NewGraphStorage: %v", err)
	}
	if err := seed.CreateVectorIndexForTenant(DefaultTenantID, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("wal seed CreateVectorIndexForTenant: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("wal seed Close: %v", err)
	}

	gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
	return &walMDriver{t: t, dir: dir, gs: gs, nid: map[string]uint64{}, eid: map[string]uint64{}}
}

func (d *walMDriver) name() string { return "wal-replay" }
func (d *walMDriver) beginPhase()  {}
func (d *walMDriver) commitPhase() {}
func (d *walMDriver) createNode(h string, l []string, v []float32) {
	liveCreateNode(d.t, d.gs, d.nid, h, l, v)
}
func (d *walMDriver) createEdge(h, from, to string) {
	liveCreateEdge(d.t, d.gs, d.nid, d.eid, h, from, to)
}
func (d *walMDriver) updateVec(h string, v []float32) { liveUpdateVec(d.t, d.gs, d.nid, h, v) }
func (d *walMDriver) deleteNode(h string)             { liveDeleteNode(d.t, d.gs, d.nid, h) }
func (d *walMDriver) nodeIDs() map[string]uint64      { return d.nid }

func (d *walMDriver) finalize() *GraphStorage {
	// Crash: deliberately do NOT Close the crashable instance (its
	// testCrashableStorage cleanup closes it after the test). Recover from the
	// same dir — WAL replay is the path under test, and this is the one cell that
	// queries after a reopen (the CC6-inverse: rebuild-on-load is the subject).
	rec, err := NewGraphStorage(d.dir)
	if err != nil {
		d.t.Fatalf("wal recover NewGraphStorage: %v", err)
	}
	d.t.Cleanup(func() { _ = rec.Close() })
	d.gs = rec
	return rec
}

// --- observation + comparison helpers -------------------------------------

func reverseIDs(nid map[string]uint64) map[uint64]string {
	rev := make(map[uint64]string, len(nid))
	for h, id := range nid {
		rev[id] = h
	}
	return rev
}

// handleFor maps a concrete ID back to its logical handle, or an "unknown:N"
// sentinel so a result the script never created surfaces as a divergence rather
// than being silently dropped.
func handleFor(rev map[uint64]string, id uint64) string {
	if h, ok := rev[id]; ok {
		return h
	}
	return fmt.Sprintf("unknown:%d", id)
}

// searchHandles returns the top-k neighbours of mmQuery as an ORDERED slice of
// logical handles (order = ranking; do not sort).
func searchHandles(t *testing.T, gs *GraphStorage, rev map[uint64]string, k int) []string {
	t.Helper()
	res, err := gs.VectorSearchForTenant(DefaultTenantID, "embedding", mmQuery, k, 200)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, handleFor(rev, r.ID))
	}
	return out
}

// labelHandleSet returns the SORTED set of logical handles carrying a label
// (order-independent membership).
func labelHandleSet(t *testing.T, gs *GraphStorage, rev map[uint64]string, label string) []string {
	t.Helper()
	nodes := gs.GetNodesByLabelForTenant(DefaultTenantID, label)
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, handleFor(rev, n.ID))
	}
	sort.Strings(out)
	return out
}

func sameIDMap(a, b map[string]uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertMetamorphicEquivalence runs the script through every driver, asserts
// per-path internal invariants, then asserts every non-reference path is
// observationally identical to the first (reference) path.
func assertMetamorphicEquivalence(t *testing.T, drivers []metamorphicDriver, withDelete bool) {
	t.Helper()

	type pathResult struct {
		name string
		gs   *GraphStorage
		nid  map[string]uint64
		rev  map[uint64]string
	}

	results := make([]pathResult, 0, len(drivers))
	for _, d := range drivers {
		runMetamorphicScript(d, withDelete)
		gs := d.finalize()
		// Each path must first be internally consistent (every derived index
		// agrees with its own shards) before cross-path comparison is meaningful.
		assertGraphInvariants(t, gs)
		nid := d.nodeIDs()
		results = append(results, pathResult{d.name(), gs, nid, reverseIDs(nid)})
	}

	ref := results[0]
	refNodes := ref.gs.CountNodesForTenant(DefaultTenantID)
	refEdges := ref.gs.CountEdgesForTenant(DefaultTenantID)
	refLabels := labelHandleSet(t, ref.gs, ref.rev, "Doc")
	refSearch := searchHandles(t, ref.gs, ref.rev, 3)

	for _, r := range results[1:] {
		// (advisor) Pin ID assignment as path-invariant. The handle translation
		// keeps the comparisons below correct even if this ever diverges, but a
		// divergence is itself surprising in this deterministic-counter codebase
		// and worth a failure to investigate.
		if !sameIDMap(ref.nid, r.nid) {
			t.Errorf("%s vs %s: handle→ID maps differ:\n  %s=%v\n  %s=%v",
				ref.name, r.name, ref.name, ref.nid, r.name, r.nid)
		}
		if got := r.gs.CountNodesForTenant(DefaultTenantID); got != refNodes {
			t.Errorf("%s node count=%d != %s=%d", r.name, got, ref.name, refNodes)
		}
		if got := r.gs.CountEdgesForTenant(DefaultTenantID); got != refEdges {
			t.Errorf("%s edge count=%d != %s=%d", r.name, got, ref.name, refEdges)
		}
		if got := labelHandleSet(t, r.gs, r.rev, "Doc"); !equalStrings(got, refLabels) {
			t.Errorf("%s Doc-label set=%v != %s=%v", r.name, got, ref.name, refLabels)
		}
		// The load-bearing assertion: identical top-k neighbours by logical
		// handle. This is the ONLY check that sees a vector re-indexed under the
		// wrong VALUE (with the right count) — the count-only invariant checker
		// is blind to it, which is Phase C's whole reason to exist.
		if got := searchHandles(t, r.gs, r.rev, 3); !equalStrings(got, refSearch) {
			t.Errorf("%s search top-k=%v != %s=%v", r.name, got, ref.name, refSearch)
		}
	}
}

// TestMetamorphic_NoDelete runs create + edge + vector-update through all four
// write paths and asserts they produce an observationally identical graph.
func TestMetamorphic_NoDelete(t *testing.T) {
	drivers := []metamorphicDriver{
		newLiveMDriver(t),
		newBatchMDriver(t),
		newTxMDriver(t),
		newWALMDriver(t),
	}
	assertMetamorphicEquivalence(t, drivers, false)
}

// TestMetamorphic_WithDelete adds a cascade delete (delete a node while its edge
// is live — the #307/#308 class) and asserts the three delete-capable paths stay
// observationally identical. The transaction path has no delete op, so it is
// logged-skipped (covered by the no-delete group) rather than silently omitted.
func TestMetamorphic_WithDelete(t *testing.T) {
	t.Logf("skip: transaction path has no DeleteNode op — excluded from the with-delete group (covered by TestMetamorphic_NoDelete)")
	drivers := []metamorphicDriver{
		newLiveMDriver(t),
		newBatchMDriver(t),
		newWALMDriver(t),
	}
	assertMetamorphicEquivalence(t, drivers, true)
}

// TestMetamorphic_VectorUpdateIsSearchObservable is the non-vacuity guard for
// the metamorphic search comparison (advisor). It proves the script's vector
// UPDATE actually moves the top-k ranking: if it did not, every path would
// return the same (possibly stale) neighbours and TestMetamorphic_* would pass
// even against a stale-vector bug — i.e. for the wrong reason. This is the
// metamorphic analogue of the Phase-A invariant teeth-test.
func TestMetamorphic_VectorUpdateIsSearchObservable(t *testing.T) {
	gs := newVectorGraph(t, DefaultTenantID)
	defer func() { _ = gs.Close() }()

	nid := map[string]uint64{}
	liveCreateNode(t, gs, nid, "n0", []string{"Doc"}, []float32{1, 0, 0})
	liveCreateNode(t, gs, nid, "n1", []string{"Doc"}, []float32{0, 1, 0})
	liveCreateNode(t, gs, nid, "n2", []string{"Doc"}, []float32{0, 0, 1})
	rev := reverseIDs(nid)

	before := searchHandles(t, gs, rev, 3)
	if err := gs.UpdateNodeForTenant(nid["n2"], mmProps([]float32{1, 0.9, 0}), DefaultTenantID); err != nil {
		t.Fatalf("update n2: %v", err)
	}
	after := searchHandles(t, gs, rev, 3)

	if equalStrings(before, after) {
		t.Fatalf("vector update did not change search ranking (before=%v after=%v); the "+
			"metamorphic search comparison would be vacuous — re-pick the planted vectors/query", before, after)
	}
	t.Logf("teeth OK: top-k before=%v, after n2 update=%v", before, after)
}

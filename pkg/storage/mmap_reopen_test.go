package storage

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"
)

// Phase 1 correctness gate for the mmap reopen mode. checkGraphInvariants can't be
// used here (it rebuilds ground truth from nodeShards, which are empty under the lazy
// representation, and is documented not-for-use across a reopen). Instead we assert
// PUBLIC-INTERFACE PARITY: the same operations against a mmap-mode store and a JSON-mode
// store must return identical results, before and after reopen and mutation.

const (
	rtTenantA = "tenant-a"
	rtTenantB = "tenant-b"
)

// buildReopenFixture creates a deterministic multi-tenant graph: 40 nodes per tenant
// (alternating Person/Org labels, a name + idx property) and a chain of edges.
func buildReopenFixture(t *testing.T, gs *GraphStorage) {
	t.Helper()
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		var ids []uint64
		for i := 0; i < 40; i++ {
			label := "Person"
			if i%2 == 1 {
				label = "Org"
			}
			n, err := gs.CreateNodeWithTenant(tenant, []string{label}, map[string]Value{
				"name": StringValue(tenant + "-" + itoa(i)),
				"idx":  IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNodeWithTenant: %v", err)
			}
			ids = append(ids, n.ID)
		}
		for i := 0; i+1 < len(ids); i++ {
			if _, err := gs.CreateEdgeWithTenant(tenant, ids[i], ids[i+1], "NEXT",
				map[string]Value{"w": IntValue(int64(i))}, float64(i)); err != nil {
				t.Fatalf("CreateEdgeWithTenant: %v", err)
			}
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// renderValue renders a Value to a deterministic string in the form "type:hexbytes".
//
// It fingerprints the RAW Data bytes (not the semantic v.String()) on purpose: the
// oracle's promise is that an mmap-reopened store enumerates BYTE-IDENTICALLY to the
// JSON path. v.String() is lossy for several types — floats go through %g, timestamps
// through time.Time.String() (drops sub-second + timezone), vectors/arrays through %v —
// so a store bug that perturbed low mantissa bits or a timestamp's nanoseconds could
// slip past a String()-based signature. Comparing type + raw bytes is byte-exact and is
// what makes the widened value-type coverage (floats, timestamps, bytes, arrays, JSON)
// actually trustworthy.
func renderValue(v Value) string {
	return fmt.Sprintf("%d:%x", v.Type, v.Data)
}

// renderProps returns a deterministic string for a property map: keys sorted,
// each rendered as "key=type:canonical", joined by ";".
func renderProps(props map[string]Value) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+renderValue(props[k]))
	}
	return strings.Join(parts, ";")
}

// renderEdgeSig builds a per-edge token: "peerID:type:weight:{sorted props}".
// peerID is ToNodeID for outgoing edges and FromNodeID for incoming edges.
func renderEdgeSig(peerID uint64, edgeType string, weight float64, props map[string]Value) string {
	return fmt.Sprintf("%d:%s:%g:{%s}", peerID, edgeType, weight, renderProps(props))
}

// fingerprint captures the full public-interface view of a tenant for equality
// comparison between JSON-mode and mmap-mode stores.
//
// Legacy fields (personIDs, orgIDs, nameByID, outDegree) are preserved so that
// every existing call site compiles and passes without change.  The new fields
// (nodeSig, outEdgeSig, inEdgeSig) sign the complete per-node state and replace
// the coarse "to:weight"-only edge token with a richer "to:type:weight:{props}" form.
type fingerprint struct {
	nodeCount uint64
	edgeCount uint64
	// Legacy label-bucket fields — still populated for compatibility.
	personIDs []uint64
	orgIDs    []uint64
	// Legacy per-node property field — still populated for compatibility.
	nameByID map[uint64]string
	// Legacy out-degree — still populated for compatibility.
	outDegree map[uint64]int
	// outEdgeSig: node ID -> sorted "toID:type:weight:{props}" of its outgoing edges.
	// This supersedes the old "to:weight"-only token.
	outEdgeSig map[uint64]string
	// nodeSig: node ID -> "sortedLabels|{sorted props}" covering all labels and
	// all properties generically (not just "name").
	nodeSig map[uint64]string
	// inEdgeSig: node ID -> sorted "fromID:type:weight:{props}" of its incoming edges.
	inEdgeSig map[uint64]string
}

func fingerprintTenant(t *testing.T, gs *GraphStorage, tenant string) fingerprint {
	t.Helper()
	fp := fingerprint{
		nodeCount:  gs.CountNodesForTenant(tenant),
		edgeCount:  gs.CountEdgesForTenant(tenant),
		nameByID:   map[uint64]string{},
		outDegree:  map[uint64]int{},
		outEdgeSig: map[uint64]string{},
		nodeSig:    map[uint64]string{},
		inEdgeSig:  map[uint64]string{},
	}
	// Populate legacy label-bucket IDs for backward-compat assertions.
	for _, n := range gs.GetNodesByLabelForTenant(tenant, "Person") {
		fp.personIDs = append(fp.personIDs, n.ID)
	}
	for _, n := range gs.GetNodesByLabelForTenant(tenant, "Org") {
		fp.orgIDs = append(fp.orgIDs, n.ID)
	}
	sort.Slice(fp.personIDs, func(i, j int) bool { return fp.personIDs[i] < fp.personIDs[j] })
	sort.Slice(fp.orgIDs, func(i, j int) bool { return fp.orgIDs[i] < fp.orgIDs[j] })

	for _, n := range gs.GetAllNodesForTenant(tenant) {
		got, err := gs.GetNodeForTenant(n.ID, tenant)
		if err != nil {
			t.Fatalf("GetNodeForTenant(%d): %v", n.ID, err)
		}

		// Legacy name field (from the single-getter path for backward compat).
		s, _ := got.Properties["name"].AsString()
		fp.nameByID[n.ID] = s

		// Full label+property signature built from the enumerated node n (not got),
		// so a bug specific to GetAllNodesForTenant (e.g. stale labels there) is not
		// masked by falling back to the single-getter path.
		lbls := make([]string, len(n.Labels))
		copy(lbls, n.Labels)
		sort.Strings(lbls)
		fp.nodeSig[n.ID] = strings.Join(lbls, ",") + "|" + renderProps(n.Properties)

		// Cross-check: the two read paths must agree on labels and properties.
		gotLbls := make([]string, len(got.Labels))
		copy(gotLbls, got.Labels)
		sort.Strings(gotLbls)
		gotSig := strings.Join(gotLbls, ",") + "|" + renderProps(got.Properties)
		if fp.nodeSig[n.ID] != gotSig {
			t.Errorf("node %d: GetAllNodesForTenant and GetNodeForTenant disagree:\n  enumerated: %q\n  single-get: %q",
				n.ID, fp.nodeSig[n.ID], gotSig)
		}

		// Outgoing edges: richer token includes type and full property bag.
		out, err := gs.GetOutgoingEdgesForTenant(n.ID, tenant)
		if err != nil {
			t.Fatalf("GetOutgoingEdgesForTenant(%d): %v", n.ID, err)
		}
		fp.outDegree[n.ID] = len(out)
		outSigs := make([]string, 0, len(out))
		for _, e := range out {
			outSigs = append(outSigs, renderEdgeSig(e.ToNodeID, e.Type, e.Weight, e.Properties))
		}
		sort.Strings(outSigs)
		fp.outEdgeSig[n.ID] = strings.Join(outSigs, "|")

		// Incoming edges.
		in, err := gs.GetIncomingEdgesForTenant(n.ID, tenant)
		if err != nil {
			t.Fatalf("GetIncomingEdgesForTenant(%d): %v", n.ID, err)
		}
		inSigs := make([]string, 0, len(in))
		for _, e := range in {
			inSigs = append(inSigs, renderEdgeSig(e.FromNodeID, e.Type, e.Weight, e.Properties))
		}
		sort.Strings(inSigs)
		fp.inEdgeSig[n.ID] = strings.Join(inSigs, "|")
	}
	return fp
}

func assertFingerprintEqual(t *testing.T, want, got fingerprint, ctx string) {
	t.Helper()
	if want.nodeCount != got.nodeCount || want.edgeCount != got.edgeCount {
		t.Errorf("%s: counts differ: want %d/%d got %d/%d", ctx, want.nodeCount, want.edgeCount, got.nodeCount, got.edgeCount)
	}
	if !equalU64(want.personIDs, got.personIDs) {
		t.Errorf("%s: Person IDs differ:\n want %v\n got %v", ctx, want.personIDs, got.personIDs)
	}
	if !equalU64(want.orgIDs, got.orgIDs) {
		t.Errorf("%s: Org IDs differ", ctx)
	}

	// Assert the node key sets match in both directions (catches nodes present in
	// one but not the other, with per-node detail for extra nodes in got).
	wantIDs := make([]uint64, 0, len(want.nodeSig))
	for id := range want.nodeSig {
		wantIDs = append(wantIDs, id)
	}
	gotIDs := make([]uint64, 0, len(got.nodeSig))
	for id := range got.nodeSig {
		gotIDs = append(gotIDs, id)
	}
	sort.Slice(wantIDs, func(i, j int) bool { return wantIDs[i] < wantIDs[j] })
	sort.Slice(gotIDs, func(i, j int) bool { return gotIDs[i] < gotIDs[j] })
	if !equalU64(wantIDs, gotIDs) {
		t.Errorf("%s: node ID sets differ:\n want %v\n  got %v", ctx, wantIDs, gotIDs)
	}
	// want→got: report nodes missing from got.
	for id := range want.nodeSig {
		if _, ok := got.nodeSig[id]; !ok {
			t.Errorf("%s: node %d present in want but missing from got", ctx, id)
		}
	}
	// got→want: report extra nodes present only in got (e.g. mmap-mode phantom nodes).
	for id, sig := range got.nodeSig {
		if _, ok := want.nodeSig[id]; !ok {
			t.Errorf("%s: node %d present in got but not in want (sig=%q)", ctx, id, sig)
		}
	}

	for id, name := range want.nameByID {
		if got.nameByID[id] != name {
			t.Errorf("%s: node %d name: want %q got %q", ctx, id, name, got.nameByID[id])
		}
		if got.outDegree[id] != want.outDegree[id] {
			t.Errorf("%s: node %d out-degree: want %d got %d", ctx, id, want.outDegree[id], got.outDegree[id])
		}
	}

	// Per-node full-signature comparisons (labels + props + incoming edges).
	for id, sig := range want.nodeSig {
		if got.nodeSig[id] != sig {
			t.Errorf("%s: node %d labels/props: want %q got %q", ctx, id, sig, got.nodeSig[id])
		}
		if got.inEdgeSig[id] != want.inEdgeSig[id] {
			t.Errorf("%s: node %d in-edges: want %q got %q", ctx, id, want.inEdgeSig[id], got.inEdgeSig[id])
		}
		if got.outEdgeSig[id] != want.outEdgeSig[id] {
			t.Errorf("%s: node %d out-edges (full): want %q got %q", ctx, id, want.outEdgeSig[id], got.outEdgeSig[id])
		}
	}
}

func equalU64(a, b []uint64) bool {
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

func mmapConfig(dir string) StorageConfig {
	c := DefaultStorageConfig(dir)
	c.UseMmapSnapshot = true
	return c
}

// applyMutations exercises every write path against (mostly) base-resident entities:
// update + remove-property (CoW promote), delete-with-cascade (tombstone), and create.
func applyMutations(t *testing.T, gs *GraphStorage) {
	t.Helper()
	// Update a base-resident node's property.
	if err := gs.UpdateNodeForTenant(1, map[string]Value{"name": StringValue("updated-1")}, rtTenantA); err != nil {
		t.Fatalf("UpdateNodeForTenant(1): %v", err)
	}
	// Delete a base-resident node (cascades its NEXT edges).
	if err := gs.DeleteNodeForTenant(3, rtTenantA); err != nil {
		t.Fatalf("DeleteNodeForTenant(3): %v", err)
	}
	// Create a brand-new node + edge from a base node to it.
	n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("brand-new")})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant(rtTenantA, 1, n.ID, "NEXT", nil, 1); err != nil {
		t.Fatalf("CreateEdgeWithTenant: %v", err)
	}
	// Update a base-resident edge's weight (CoW promote): edge 5 is node5->node6.
	w := 99.0
	if err := gs.UpdateEdgeForTenant(5, nil, &w, rtTenantA); err != nil {
		t.Fatalf("UpdateEdgeForTenant(5): %v", err)
	}
	// Delete a base-resident edge (tombstone): edge 7 is node7->node8.
	if err := gs.DeleteEdgeForTenant(7, rtTenantA); err != nil {
		t.Fatalf("DeleteEdgeForTenant(7): %v", err)
	}
}

// TestMmapReopen_WritesAfterReopen: identical mutations applied to a JSON-mode store
// and a mmap-mode store (after a reopen, so writes hit the overlay/tombstone path)
// must yield identical public-interface fingerprints — live AND after a second reopen.
func TestMmapReopen_WritesAfterReopen(t *testing.T) {
	jsonDir, mmapDir := t.TempDir(), t.TempDir()

	// Seed identical data in both, close.
	jgs, _ := NewGraphStorage(jsonDir)
	buildReopenFixture(t, jgs)
	if err := jgs.Close(); err != nil {
		t.Fatal(err)
	}
	mgs, _ := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	buildReopenFixture(t, mgs)
	if err := mgs.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen both, mutate (mmap writes now hit base-resident entities), compare live.
	jr, _ := NewGraphStorage(jsonDir)
	mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	if mr.mmapSnap == nil {
		t.Fatal("mmap reopen did not take the mmap path")
	}
	applyMutations(t, jr)
	applyMutations(t, mr)
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "live-after-mutate "+tenant)
	}
	if err := jr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mr.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen both again: the mutated state must have persisted identically.
	jr2, _ := NewGraphStorage(jsonDir)
	defer jr2.Close()
	mr2, _ := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	defer mr2.Close()
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		assertFingerprintEqual(t, fingerprintTenant(t, jr2, tenant), fingerprintTenant(t, mr2, tenant), "reopen-after-mutate "+tenant)
	}
}

// applyBatch exercises the batch executor's overlay/tombstone paths against
// base-resident entities (tenant-blind by ID; all chosen IDs are tenant-a).
func applyBatch(t *testing.T, gs *GraphStorage) {
	t.Helper()
	b := gs.BeginBatch()
	b.UpdateNode(9, map[string]Value{"name": StringValue("batch-updated-9")})
	b.DeleteNode(11) // cascades node 11's edges
	b.DeleteEdge(13) // node13->node14
	if err := b.Commit(); err != nil {
		t.Fatalf("batch commit: %v", err)
	}
}

// TestMmapReopen_BatchParity: identical batch mutations after reopen must yield
// identical fingerprints in JSON and mmap mode, live and after a second reopen.
func TestMmapReopen_BatchParity(t *testing.T) {
	jsonDir, mmapDir := t.TempDir(), t.TempDir()
	jgs, _ := NewGraphStorage(jsonDir)
	buildReopenFixture(t, jgs)
	jgs.Close()
	mgs, _ := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	buildReopenFixture(t, mgs)
	mgs.Close()

	jr, _ := NewGraphStorage(jsonDir)
	mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	applyBatch(t, jr)
	applyBatch(t, mr)
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "batch-live "+tenant)
	}
	jr.Close()
	mr.Close()

	jr2, _ := NewGraphStorage(jsonDir)
	defer jr2.Close()
	mr2, _ := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	defer mr2.Close()
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		assertFingerprintEqual(t, fingerprintTenant(t, jr2, tenant), fingerprintTenant(t, mr2, tenant), "batch-reopen "+tenant)
	}
}

// TestMmapReopen_CrashRecovery: Snapshot() (no truncate) then post-snapshot mutations
// land in the WAL; reopening WITHOUT a clean Close must replay them via the mmap-aware
// replay mutators. JSON and mmap recoveries must converge to identical state.
func TestMmapReopen_CrashRecovery(t *testing.T) {
	// crashBuild seeds, checkpoints, mutates, then abandons the store (no Close) so
	// the WAL retains post-snapshot entries — a crash before the next checkpoint.
	crashBuild := func(cfg StorageConfig) {
		gs, err := NewGraphStorageWithConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		buildReopenFixture(t, gs)
		if err := gs.Snapshot(); err != nil { // checkpoint; WAL intact (no truncate)
			t.Fatal(err)
		}
		applyMutations(t, gs) // post-snapshot writes -> WAL only
		// no Close: simulate crash
	}

	jsonDir, mmapDir := t.TempDir(), t.TempDir()
	crashBuild(DefaultStorageConfig(jsonDir))
	crashBuild(mmapConfig(mmapDir))

	jr, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatalf("json recovery: %v", err)
	}
	defer jr.Close()
	mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatalf("mmap recovery: %v", err)
	}
	defer mr.Close()

	for _, tenant := range []string{rtTenantA, rtTenantB} {
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "crash-recovery "+tenant)
	}
}

// TestMmapReopen_RoundTrip: build in mmap mode, snapshot, reopen in mmap mode, and
// assert the public-interface fingerprint survives (Phase 1b load + 1d snapshot).
func TestMmapReopen_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	buildReopenFixture(t, gs)
	wantA := fingerprintTenant(t, gs, rtTenantA)
	wantB := fingerprintTenant(t, gs, rtTenantB)
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !fileExists(mmapSnapshotPath(dir)) {
		t.Fatal("Close did not write snapshot.mmap in mmap mode")
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer gs2.Close()
	if gs2.mmapSnap == nil {
		t.Fatal("reopen did not take the mmap path")
	}
	assertFingerprintEqual(t, wantA, fingerprintTenant(t, gs2, rtTenantA), "reopen tenant-a")
	assertFingerprintEqual(t, wantB, fingerprintTenant(t, gs2, rtTenantB), "reopen tenant-b")

	// Cross-tenant isolation must hold after reopen: a tenant-a node is not
	// visible to tenant-b (ErrNodeNotFound, no existence leak).
	aNode := wantA.personIDs[0]
	if _, err := gs2.GetNodeForTenant(aNode, rtTenantB); err != ErrNodeNotFound {
		t.Fatalf("cross-tenant read leaked: got %v want ErrNodeNotFound", err)
	}
}

// TestMmapReopen_ParityWithJSON: identical data built in JSON mode and mmap mode must
// present identical public-interface fingerprints after reopen.
func TestMmapReopen_ParityWithJSON(t *testing.T) {
	jsonDir := t.TempDir()
	mmapDir := t.TempDir()

	jgs, err := NewGraphStorage(jsonDir) // default = JSON mode
	if err != nil {
		t.Fatal(err)
	}
	buildReopenFixture(t, jgs)
	if err := jgs.Close(); err != nil {
		t.Fatal(err)
	}

	mgs, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	buildReopenFixture(t, mgs)
	if err := mgs.Close(); err != nil {
		t.Fatal(err)
	}

	jr, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	defer jr.Close()
	mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	for _, tenant := range []string{rtTenantA, rtTenantB} {
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "json-vs-mmap "+tenant)
	}
}

func TestMmapStage2_LazyMembershipParity(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := gs.CreateNodeWithTenant(tenant, []string{"Alpha"}, map[string]Value{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := gs.CreateNodeWithTenant(tenant, []string{"Beta"}, map[string]Value{}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := gs.CreateNodeWithTenant(tenant, []string{"Gamma"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := gs.CreateNodeWithTenant(tenant, []string{"Gamma"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "LINK", map[string]Value{}, 1.0); err != nil {
			t.Fatal(err)
		}
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	// Counts correct WITHOUT triggering an enumeration (stats decoupled from the build).
	if got := mr.CountNodesForTenant(tenant); got != 10 {
		t.Errorf("CountNodesForTenant=%d want 10 (must not need membership build)", got)
	}
	// Enumeration is served from the persisted membership section; results match.
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Alpha")); got != 5 {
		t.Errorf("Alpha=%d want 5", got)
	}
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Beta")); got != 3 {
		t.Errorf("Beta=%d want 3", got)
	}
	// A post-open create is reflected (overlay indexed at write time).
	if _, err := mr.CreateNodeWithTenant(tenant, []string{"Alpha"}, map[string]Value{}); err != nil {
		t.Fatal(err)
	}
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Alpha")); got != 6 {
		t.Errorf("Alpha after create=%d want 6", got)
	}
	// Edge type-membership: the 2 LINK edges written before close must be visible
	// via GetEdgesByTypeForTenant (served from the persisted membership section).
	if got := len(mr.GetEdgesByTypeForTenant(tenant, "LINK")); got != 2 {
		t.Errorf("LINK edges=%d want 2 (edge membership from persisted section)", got)
	}
}

func TestMmapStage2_AdjacencyFromCSR(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	mustNode := func(g *GraphStorage) uint64 {
		n, err := g.CreateNodeWithTenant(tenant, []string{"N"}, map[string]Value{})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	n1, n2, n3 := mustNode(gs), mustNode(gs), mustNode(gs)
	mkEdge := func(from, to uint64) uint64 {
		e, err := gs.CreateEdgeWithTenant(tenant, from, to, "E", map[string]Value{}, 1.0)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	e1 := mkEdge(n1, n2) // n1 -> n2
	mkEdge(n1, n3)       // n1 -> n3
	mkEdge(n2, n3)       // n2 -> n3
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	outLen := func(n uint64) int {
		e, _ := mr.GetOutgoingEdgesForTenant(n, tenant)
		return len(e)
	}
	inLen := func(n uint64) int {
		e, _ := mr.GetIncomingEdgesForTenant(n, tenant)
		return len(e)
	}
	if inLen(n2) != 1 { // base CSR incoming from n1
		t.Errorf("base in(n2)=%d want 1", inLen(n2))
	}
	if outLen(n1) != 2 { // base CSR read
		t.Errorf("base out(n1)=%d want 2", outLen(n1))
	}
	if err := mr.DeleteEdgeForTenant(e1, tenant); err != nil { // tombstone filter
		t.Fatal(err)
	}
	if outLen(n1) != 1 {
		t.Errorf("after delete out(n1)=%d want 1", outLen(n1))
	}
	if _, err := mr.CreateEdgeWithTenant(tenant, n1, n3, "E", map[string]Value{}, 1.0); err != nil { // overlay append
		t.Fatal(err)
	}
	if outLen(n1) != 2 {
		t.Errorf("after overlay add out(n1)=%d want 2", outLen(n1))
	}
	if err := mr.Close(); err != nil { // survives second reopen
		t.Fatal(err)
	}
	mr2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr2.Close()
	e, _ := mr2.GetOutgoingEdgesForTenant(n1, tenant)
	if len(e) != 2 {
		t.Errorf("after 2nd reopen out(n1)=%d want 2", len(e))
	}
}

// CONSUMER CONTRACT: coord's claim semantics rely on
// CreateNodeWithUniquePropertyForTenant rejecting duplicates. After an mmap
// reopen the per-tenant membership index is built lazily; the unique-create must
// still see base nodes WITHOUT a prior enumeration call, or a reopened store
// would silently allow duplicate claims.
func TestMmapStage2_UniqueConstraintSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	props := map[string]Value{"for_task": {Type: TypeString, Data: []byte("task-42")}}
	if _, err := gs.CreateNodeWithUniquePropertyForTenant(tenant, []string{"Claim"}, props, "Claim", "for_task"); err != nil {
		t.Fatalf("first unique create: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	// No enumeration call first — the unique-create itself must trigger the build.
	dup := map[string]Value{"for_task": {Type: TypeString, Data: []byte("task-42")}}
	_, err = mr.CreateNodeWithUniquePropertyForTenant(tenant, []string{"Claim"}, dup, "Claim", "for_task")
	if err == nil {
		t.Fatal("duplicate unique-property create succeeded after reopen — uniqueness lost")
	}
	var ucErr *UniqueConstraintError
	if !errors.As(err, &ucErr) {
		t.Fatalf("want *UniqueConstraintError, got %T: %v", err, err)
	}
}

func TestMmapStage2_UpdatedBaseNodeStillEnumerated(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	n, err := gs.CreateNodeWithTenant(tenant, []string{"Widget"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	// Promote the base node into the overlay via a property update (labels unchanged).
	if err := mr.UpdateNode(n.ID, map[string]Value{"k": {Type: TypeString, Data: []byte("v")}}); err != nil {
		t.Fatal(err)
	}
	// Enumeration (served from the persisted section + overlay) must still
	// index the updated base node under its (immutable) label.
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Widget")); got != 1 {
		t.Errorf("Widget=%d want 1 (updated base node dropped from membership)", got)
	}
}

func TestMmapStage2b_MembershipAccessors(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	mk := func(label string) uint64 {
		n, err := gs.CreateNodeWithTenant(tenant, []string{label}, map[string]Value{})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	a1, a2 := mk("Alpha"), mk("Alpha")
	mk("Beta")
	if _, err := gs.CreateEdgeWithTenant(tenant, a1, a2, "LINK", map[string]Value{}, 1.0); err != nil {
		t.Fatal(err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	tid := effectiveTenantID(tenant)
	read := func(fn func() []uint64) []uint64 {
		mr.mu.RLock()
		defer mr.mu.RUnlock()
		return fn()
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsByLabelLocked(tid, "Alpha") }); len(got) != 2 {
		t.Errorf("Alpha base=%d want 2", len(got))
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsForTenantLocked(tid) }); len(got) != 3 {
		t.Errorf("tenant-all base=%d want 3", len(got))
	}
	a3, err := mr.CreateNodeWithTenant(tenant, []string{"Alpha"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsByLabelLocked(tid, "Alpha") }); len(got) != 3 {
		t.Errorf("Alpha after add=%d want 3", len(got))
	}
	if got := read(func() []uint64 { return mr.membershipEdgeIDsForTenantLocked(tid) }); len(got) != 1 {
		t.Errorf("edge tenant-all base=%d want 1", len(got))
	}
	if got := read(func() []uint64 { return mr.membershipEdgeIDsByTypeLocked(tid, "LINK") }); len(got) != 1 {
		t.Errorf("LINK edges base=%d want 1", len(got))
	}
	if err := mr.DeleteNode(a1); err != nil {
		t.Fatal(err)
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsByLabelLocked(tid, "Alpha") }); len(got) != 2 {
		t.Errorf("Alpha after delete=%d want 2", len(got))
	}
	_ = a2
	_ = a3
}

func TestMmapStage2b_EnumerationAtOpenNoBuild(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	// 3 Alpha (i=0,2,4), 3 Beta (i=1,3,5); 6 total.
	for i := 0; i < 6; i++ {
		lbl := "Alpha"
		if i%2 == 1 {
			lbl = "Beta"
		}
		if _, err := gs.CreateNodeWithTenant(tenant, []string{lbl}, map[string]Value{}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := gs.CreateNodeWithTenant(tenant, []string{"Gamma"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := gs.CreateNodeWithTenant(tenant, []string{"Gamma"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "LINK", map[string]Value{}, 1.0); err != nil {
			t.Fatal(err)
		}
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	// Enumerate with NO prior call — results come from the persisted section.
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Alpha")); got != 3 {
		t.Errorf("Alpha=%d want 3", got)
	}
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Beta")); got != 3 {
		t.Errorf("Beta=%d want 3", got)
	}
	if got := len(mr.GetAllNodesForTenant(tenant)); got != 8 {
		t.Errorf("all-nodes=%d want 8", got)
	}
	if got := len(mr.GetEdgesByTypeForTenant(tenant, "LINK")); got != 2 {
		t.Errorf("LINK edges=%d want 2", got)
	}
	if got := len(mr.GetAllEdgesForTenant(tenant)); got != 2 {
		t.Errorf("all-edges=%d want 2", got)
	}
}

// TestMmapStage2c_ReturnedNodeIsOwnedCopy verifies that a node returned from
// enumeration in mmap reopen mode is a fully owned, independently mutable copy:
// mutating the returned node must not corrupt the store's view of that node.
// This is the safety guard for the Stage 2c Clone-skip optimisation: the
// mmap-base decode path must return a fresh heap-owned copy; the overlay shard
// path must still be cloned (that remains correct for both modes).
func TestMmapStage2c_ReturnedNodeIsOwnedCopy(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	n, err := gs.CreateNodeWithTenant(tenant, []string{"Widget"}, map[string]Value{"k": {Type: TypeString, Data: []byte("orig")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	// Enumerate (mmap-base node, served without Clone after this change).
	nodes := mr.GetNodesByLabelForTenant(tenant, "Widget")
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes want 1", len(nodes))
	}
	// Mutate the returned node aggressively.
	nodes[0].Properties["k"] = Value{Type: TypeString, Data: []byte("MUTATED")}
	nodes[0].Properties["injected"] = Value{Type: TypeString, Data: []byte("x")}
	nodes[0].Labels = append(nodes[0].Labels, "Injected")

	// Re-read via the single getter: the store must be UNTOUCHED.
	got, err := mr.GetNodeForTenant(n.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Properties["k"].Data) != "orig" {
		t.Errorf("store property corrupted: k=%q want orig", string(got.Properties["k"].Data))
	}
	if _, bad := got.Properties["injected"]; bad {
		t.Error("store gained an injected property — returned node aliased storage")
	}
	if len(got.Labels) != 1 {
		t.Errorf("store labels corrupted: %v", got.Labels)
	}
	// And a fresh enumeration still sees the original.
	again := mr.GetNodesByLabelForTenant(tenant, "Widget")
	if len(again) != 1 || string(again[0].Properties["k"].Data) != "orig" {
		t.Errorf("second enumeration corrupted")
	}
}

// ---------------------------------------------------------------------------
// Randomized parity test
// ---------------------------------------------------------------------------

// nodeSpec and edgeSpec are pre-generated from a fixed-seed PRNG so that
// BOTH the JSON-mode and mmap-mode stores receive IDENTICAL inputs — no PRNG
// ordering hazards because the sequences are materialised before the first
// store call.
type nodeSpec struct {
	labels []string
	props  map[string]Value
}

type edgeSpec struct {
	fromIdx int // index into the nodeIDs slice
	toIdx   int
	typ     string
	weight  float64
	props   map[string]Value
}

// mutSpec describes a single mutation to apply to a store after the first reopen.
type mutSpec struct {
	kind    string // "updateNode", "deleteNode", "createEdge", "deleteEdge", "updateEdge"
	nodeIdx int    // index into nodeIDs (for node ops)
	edgeIdx int    // index into edgeIDs (for edge ops)
	props   map[string]Value
	weight  float64
}

const (
	rpSeed      = 1337
	rpNodeCount = 400
	rpEdgeCount = 600
	rpMutCount  = 20
	rpTenant    = "rp-tenant"
)

var (
	rpNodeLabels = []string{"Person", "Org", "Widget", "Gadget", "Thing"}
	rpEdgeTypes  = []string{"NEXT", "REF", "OWNS", "LINK"}
	rpPropKeys   = []string{"alpha", "beta", "gamma", "delta"}
)

// rpRandomProps returns a map of 0–2 random properties drawn from rpPropKeys.
// Half of the values are strings, half are ints.
func rpRandomProps(rng *rand.Rand, count int) map[string]Value {
	if count == 0 {
		return map[string]Value{}
	}
	m := make(map[string]Value, count)
	for i := 0; i < count; i++ {
		k := rpPropKeys[rng.Intn(len(rpPropKeys))]
		if rng.Intn(2) == 0 {
			m[k] = StringValue(fmt.Sprintf("sv%d", rng.Intn(1000)))
		} else {
			m[k] = IntValue(int64(rng.Intn(10000)))
		}
	}
	return m
}

// buildRandomSpecs uses a deterministic PRNG to produce node and edge specs.
// Both stores will be fed from the same slices, so they are guaranteed identical.
func buildRandomSpecs(rng *rand.Rand) ([]nodeSpec, []edgeSpec) {
	nodes := make([]nodeSpec, rpNodeCount)
	for i := range nodes {
		numLabels := 1 + rng.Intn(2) // 1 or 2 labels
		lbls := make([]string, numLabels)
		for j := range lbls {
			lbls[j] = rpNodeLabels[rng.Intn(len(rpNodeLabels))]
		}
		nodes[i] = nodeSpec{
			labels: lbls,
			props:  rpRandomProps(rng, 1+rng.Intn(4)), // 1–4 props
		}
	}

	edges := make([]edgeSpec, rpEdgeCount)
	for i := range edges {
		from := rng.Intn(rpNodeCount)
		to := rng.Intn(rpNodeCount)
		edges[i] = edgeSpec{
			fromIdx: from,
			toIdx:   to,
			typ:     rpEdgeTypes[rng.Intn(len(rpEdgeTypes))],
			weight:  float64(rng.Intn(100)) + rng.Float64(),
			props:   rpRandomProps(rng, rng.Intn(3)), // 0–2 props
		}
	}
	return nodes, edges
}

// buildMutSpecs generates mutation specs from the PRNG. nodeCount and edgeCount
// are the actual counts returned after building, so the indices are valid.
func buildMutSpecs(rng *rand.Rand, nodeCount, edgeCount int) []mutSpec {
	kinds := []string{"updateNode", "deleteNode", "createEdge", "deleteEdge", "updateEdge"}
	muts := make([]mutSpec, rpMutCount)
	for i := range muts {
		muts[i] = mutSpec{
			kind:    kinds[rng.Intn(len(kinds))],
			nodeIdx: rng.Intn(nodeCount),
			edgeIdx: rng.Intn(edgeCount),
			props:   rpRandomProps(rng, 1+rng.Intn(3)),
			weight:  float64(rng.Intn(100)) + rng.Float64(),
		}
	}
	return muts
}

// populateStore applies nodeSpecs and edgeSpecs to gs, returning slices of
// created IDs in the same order.
func populateStore(t *testing.T, gs *GraphStorage, nodes []nodeSpec, edges []edgeSpec) (nodeIDs []uint64, edgeIDs []uint64) {
	t.Helper()
	nodeIDs = make([]uint64, len(nodes))
	for i, spec := range nodes {
		n, err := gs.CreateNodeWithTenant(rpTenant, spec.labels, spec.props)
		if err != nil {
			t.Fatalf("populateStore CreateNode[%d]: %v", i, err)
		}
		nodeIDs[i] = n.ID
	}
	edgeIDs = make([]uint64, 0, len(edges))
	for i, spec := range edges {
		e, err := gs.CreateEdgeWithTenant(rpTenant, nodeIDs[spec.fromIdx], nodeIDs[spec.toIdx],
			spec.typ, spec.props, spec.weight)
		if err != nil {
			// Self-loops or duplicate edges may be rejected — skip rather than fatal.
			edgeIDs = append(edgeIDs, 0)
			_ = i
			continue
		}
		edgeIDs = append(edgeIDs, e.ID)
	}
	return nodeIDs, edgeIDs
}

// applyMutSpecs drives the mutation sequence against gs, skipping any
// operation on a zero/deleted ID (tombstoned by an earlier deleteNode/deleteEdge).
func applyMutSpecs(t *testing.T, gs *GraphStorage, muts []mutSpec, nodeIDs, edgeIDs []uint64) {
	t.Helper()
	deletedNodes := map[uint64]bool{}
	deletedEdges := map[uint64]bool{}
	for _, m := range muts {
		switch m.kind {
		case "updateNode":
			id := nodeIDs[m.nodeIdx]
			if id == 0 || deletedNodes[id] {
				continue
			}
			_ = gs.UpdateNodeForTenant(id, m.props, rpTenant)
		case "deleteNode":
			id := nodeIDs[m.nodeIdx]
			if id == 0 || deletedNodes[id] {
				continue
			}
			if err := gs.DeleteNodeForTenant(id, rpTenant); err == nil {
				deletedNodes[id] = true
			}
		case "createEdge":
			fromID := nodeIDs[m.nodeIdx%len(nodeIDs)]
			toID := nodeIDs[(m.nodeIdx+1)%len(nodeIDs)]
			if deletedNodes[fromID] || deletedNodes[toID] {
				continue
			}
			_, _ = gs.CreateEdgeWithTenant(rpTenant, fromID, toID, "LINK", m.props, m.weight)
		case "deleteEdge":
			if m.edgeIdx >= len(edgeIDs) {
				continue
			}
			id := edgeIDs[m.edgeIdx]
			if id == 0 || deletedEdges[id] {
				continue
			}
			if err := gs.DeleteEdgeForTenant(id, rpTenant); err == nil {
				deletedEdges[id] = true
			}
		case "updateEdge":
			if m.edgeIdx >= len(edgeIDs) {
				continue
			}
			id := edgeIDs[m.edgeIdx]
			if id == 0 || deletedEdges[id] {
				continue
			}
			w := m.weight
			_ = gs.UpdateEdgeForTenant(id, m.props, &w, rpTenant)
		}
	}
}

// TestMmapReopen_RandomizedParity builds identical ~400-node / ~600-edge graphs in
// both a JSON-mode store and an mmap-mode store from a fixed PRNG seed (1337) so
// the test is deterministic and reproducible. It then:
//  1. Asserts full-fingerprint parity after first reopen.
//  2. Applies a round of random mutations to both (same sequence).
//  3. Asserts parity live after mutations.
//  4. Closes and reopens both; asserts parity after second reopen.
//
// If this test FAILS, that is a genuine mmap≠JSON divergence — do NOT weaken
// the test; report it as BLOCKED with the diff output.
func TestMmapReopen_RandomizedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(rpSeed))

	// Build specs once from the PRNG so both stores get identical inputs.
	nodeSpecs, edgeSpecs := buildRandomSpecs(rng)

	// Phase 1: populate both stores from the same specs, close.
	jsonDir, mmapDir := t.TempDir(), t.TempDir()

	jgs, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	jNodeIDs, jEdgeIDs := populateStore(t, jgs, nodeSpecs, edgeSpecs)
	if err := jgs.Close(); err != nil {
		t.Fatal(err)
	}

	mgs, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	mNodeIDs, mEdgeIDs := populateStore(t, mgs, nodeSpecs, edgeSpecs)
	if err := mgs.Close(); err != nil {
		t.Fatal(err)
	}

	// Sanity-guard: counts must be non-trivial.
	if len(jNodeIDs) == 0 {
		t.Fatal("randomized parity: no nodes created — test is vacuous")
	}
	if len(jEdgeIDs) == 0 {
		t.Fatal("randomized parity: no edges created — test is vacuous")
	}

	// Phase 2: reopen both, assert full parity.
	jr, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	defer jr.Close()
	mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	if mr.mmapSnap == nil {
		t.Fatal("mmap reopen did not take the mmap path")
	}

	jFP1 := fingerprintTenant(t, jr, rpTenant)
	mFP1 := fingerprintTenant(t, mr, rpTenant)
	t.Logf("randomized parity: nodes=%d edges=%d", jFP1.nodeCount, jFP1.edgeCount)
	if jFP1.nodeCount == 0 || jFP1.edgeCount == 0 {
		t.Fatal("randomized parity: fingerprint shows zero counts — test is vacuous")
	}
	assertFingerprintEqual(t, jFP1, mFP1, "after-first-reopen")

	// Phase 3: apply the same mutation sequence to both reopened stores.
	// Mutation specs are drawn from the same rng (continued), so the sequence
	// is deterministic and applied identically to both.
	mutSpecs := buildMutSpecs(rng, len(jNodeIDs), len(jEdgeIDs))
	applyMutSpecs(t, jr, mutSpecs, jNodeIDs, jEdgeIDs)
	applyMutSpecs(t, mr, mutSpecs, mNodeIDs, mEdgeIDs)

	assertFingerprintEqual(t,
		fingerprintTenant(t, jr, rpTenant),
		fingerprintTenant(t, mr, rpTenant),
		"live-after-mutations",
	)

	// Phase 4: close and reopen both; assert parity survives persistence.
	if err := jr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mr.Close(); err != nil {
		t.Fatal(err)
	}

	jr2, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	defer jr2.Close()
	mr2, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr2.Close()

	assertFingerprintEqual(t,
		fingerprintTenant(t, jr2, rpTenant),
		fingerprintTenant(t, mr2, rpTenant),
		"after-second-reopen",
	)
}

// TestMmapReopen_DeleteAllNodesClears is the regression gate for #416. In mmap mode
// the in-memory shards are an overlay on the still-mapped base; DeleteAllNodes cleared
// only the overlay, leaving the base mapped and un-tombstoned. The result was that
// Snapshot() (via forEachNodeUnlocked) re-persisted every "deleted" node and reads kept
// serving them — so DeleteAllNodes was a silent no-op on an mmap store. A JSON-mode store
// run through the identical sequence (where DeleteAllNodes already works) is the oracle:
// after delete-all both stores must be empty, live and across a reopen.
func TestMmapReopen_DeleteAllNodesClears(t *testing.T) {
	jsonDir, mmapDir := t.TempDir(), t.TempDir()

	// Seed identical data in both modes; close so the mmap snapshot is persisted.
	jgs, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	buildReopenFixture(t, jgs)
	if err := jgs.Close(); err != nil {
		t.Fatal(err)
	}
	mgs, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	buildReopenFixture(t, mgs)
	if err := mgs.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen both. The mmap store must take the mmap path — that live base is the
	// condition under which #416 reproduced.
	jr, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	if mr.mmapSnap == nil {
		t.Fatal("mmap reopen did not take the mmap path")
	}

	// Delete everything in both.
	if err := jr.DeleteAllNodes(); err != nil {
		t.Fatalf("json DeleteAllNodes: %v", err)
	}
	if err := mr.DeleteAllNodes(); err != nil {
		t.Fatalf("mmap DeleteAllNodes: %v", err)
	}

	// Live: both stores must report empty for every tenant. (Pre-fix the mmap store
	// still served the base nodes here.)
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		if c := mr.CountNodesForTenant(tenant); c != 0 {
			t.Errorf("mmap CountNodesForTenant(%s) = %d after DeleteAllNodes, want 0", tenant, c)
		}
		if c := mr.CountEdgesForTenant(tenant); c != 0 {
			t.Errorf("mmap CountEdgesForTenant(%s) = %d after DeleteAllNodes, want 0", tenant, c)
		}
		if n := mr.GetAllNodesForTenant(tenant); len(n) != 0 {
			t.Errorf("mmap GetAllNodesForTenant(%s) = %d nodes after DeleteAllNodes, want 0", tenant, len(n))
		}
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "live-after-deleteall "+tenant)
	}
	if err := jr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mr.Close(); err != nil {
		t.Fatal(err)
	}

	// Across reopen: the cleared state must persist. (Pre-fix the snapshot written by
	// DeleteAllNodes contained the whole old graph, so it reappeared here.)
	jr2, err := NewGraphStorage(jsonDir)
	if err != nil {
		t.Fatal(err)
	}
	defer jr2.Close()
	mr2, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr2.Close()
	for _, tenant := range []string{rtTenantA, rtTenantB} {
		if c := mr2.CountNodesForTenant(tenant); c != 0 {
			t.Errorf("mmap CountNodesForTenant(%s) = %d after reopen, want 0", tenant, c)
		}
		assertFingerprintEqual(t, fingerprintTenant(t, jr2, tenant), fingerprintTenant(t, mr2, tenant), "reopen-after-deleteall "+tenant)
	}
}

// ---------------------------------------------------------------------------
// Per-value-type round-trip parity (explicit type coverage)
// ---------------------------------------------------------------------------

// valueTypeCases enumerates one property value per ValueType (with edge cases:
// empty/zero/negative/NaN/Inf/unicode/empty-collection) so the oracle exercises
// the full type space, not just String+Int. The mmap property codec is
// type-agnostic (type|len|data verbatim, mmap_snapshot_format.go), so any
// per-type divergence that surfaces here is a JSON-path representational gap,
// not an mmap-layout bug — report it, do not weaken the test.
//
// Kept as a package-level func (not an inline literal) so the fuzz target can
// draw from the same set.
func valueTypeCases() []struct {
	name string
	v    Value
} {
	return []struct {
		name string
		v    Value
	}{
		{"string/empty", StringValue("")},
		{"string/ascii", StringValue("hello")},
		{"string/unicode", StringValue("héllo-世界-🌍")},
		{"int/zero", IntValue(0)},
		{"int/negative", IntValue(-42)},
		{"int/max", IntValue(math.MaxInt64)},
		{"int/min", IntValue(math.MinInt64)},
		{"float/zero", FloatValue(0)},
		{"float/negzero", FloatValue(math.Copysign(0, -1))},
		{"float/pi", FloatValue(3.141592653589793)},
		{"float/nan", FloatValue(math.NaN())},
		{"float/posinf", FloatValue(math.Inf(1))},
		{"float/neginf", FloatValue(math.Inf(-1))},
		{"bool/true", BoolValue(true)},
		{"bool/false", BoolValue(false)},
		{"bytes/empty", BytesValue([]byte{})},
		{"bytes/nul", BytesValue([]byte{0x00, 0x01, 0xff, 0x00})},
		{"timestamp/epoch", TimestampValue(time.Unix(0, 0))},
		{"timestamp/y2k", TimestampValue(time.Unix(946684800, 0))},
		{"vector/empty", VectorValue([]float32{})},
		{"vector/vals", VectorValue([]float32{1.5, -2.25, 0})},
		{"strarray/empty", StringArrayValue([]string{})},
		{"strarray/vals", StringArrayValue([]string{"a", "", "c"})},
		{"intarray/vals", IntArrayValue([]int64{0, -1, math.MaxInt64})},
		{"floatarray/vals", FloatArrayValue([]float64{0, 1.5, math.Inf(1)})},
		{"boolarray/vals", BoolArrayValue([]bool{true, false, true})},
	}
}

// TestMmapReopen_ValueTypeParity asserts that a node carrying a property of each
// ValueType enumerates byte-identically from an mmap-reopened store and a JSON-mode
// store — live, and across a reopen. Complements the random fuzzer with explicit,
// named per-type coverage (a failing type names itself).
func TestMmapReopen_ValueTypeParity(t *testing.T) {
	const tenant = "vt-tenant"
	for _, tc := range valueTypeCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			jsonDir, mmapDir := t.TempDir(), t.TempDir()

			seed := func(gs *GraphStorage) {
				if _, err := gs.CreateNodeWithTenant(tenant, []string{"T"}, map[string]Value{"p": tc.v}); err != nil {
					t.Fatalf("CreateNode: %v", err)
				}
			}

			jgs, _ := NewGraphStorage(jsonDir)
			seed(jgs)
			if err := jgs.Close(); err != nil {
				t.Fatal(err)
			}
			mgs, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
			if err != nil {
				t.Fatal(err)
			}
			seed(mgs)
			if err := mgs.Close(); err != nil {
				t.Fatal(err)
			}

			jr, _ := NewGraphStorage(jsonDir)
			defer jr.Close()
			mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
			if err != nil {
				t.Fatal(err)
			}
			defer mr.Close()
			if mr.mmapSnap == nil {
				t.Fatal("reopen did not take the mmap path")
			}
			assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "type "+tc.name)

			// Non-vacuity guard: the property must actually survive the reopen with
			// its exact type + bytes. Without this, a value silently dropped by BOTH
			// stores would make the parity assertion pass trivially (empty == empty).
			nodes := mr.GetNodesByLabelForTenant(tenant, "T")
			if len(nodes) != 1 {
				t.Fatalf("want 1 node after reopen, got %d", len(nodes))
			}
			got, ok := nodes[0].Properties["p"]
			if !ok {
				t.Fatalf("property %q dropped across reopen (parity was vacuous)", "p")
			}
			if got.Type != tc.v.Type || !bytes.Equal(got.Data, tc.v.Data) {
				t.Fatalf("property round-trip corrupted: want %d:%x got %d:%x",
					tc.v.Type, tc.v.Data, got.Type, got.Data)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fuzzed parity (native go test -fuzz)
// ---------------------------------------------------------------------------

// byteReader turns a fuzz-supplied []byte into a deterministic stream of
// structural choices. It wraps around on exhaustion so even short inputs yield
// a full graph (and empty input yields all-zero choices — a minimal graph).
type byteReader struct {
	b   []byte
	pos int
}

func (r *byteReader) u8() byte {
	if len(r.b) == 0 {
		return 0
	}
	v := r.b[r.pos%len(r.b)]
	r.pos++
	return v
}

func (r *byteReader) u64() uint64 {
	var v uint64
	for i := 0; i < 8; i++ {
		v = v<<8 | uint64(r.u8())
	}
	return v
}

// intn returns a value in [0,n) drawn from two consumed bytes (0 for n<=0).
func (r *byteReader) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return (int(r.u8())<<8 | int(r.u8())) % n
}

// finiteWeight returns a finite, varied float64 suitable for an edge weight.
// Edge weight is a NATIVE float64 struct field that the JSON snapshot path
// json.Marshals, so NaN/±Inf are illegal there — keep it finite. (Float
// *properties* are opaque bytes and may be any bit pattern; see fuzzValue.)
func (r *byteReader) finiteWeight() float64 {
	return float64(r.intn(100000)) / 100.0
}

// fuzzValue draws a property Value spanning the full ValueType space, including
// bit patterns the semantic API can't otherwise reach (NaN/±Inf floats via raw
// bits, NUL bytes, empty collections). Values are opaque Data bytes end-to-end,
// so any type/pattern is legal here.
func fuzzValue(r *byteReader) Value {
	switch r.intn(12) {
	case 0:
		n := r.intn(6)
		b := make([]byte, n)
		for i := range b {
			b[i] = r.u8()
		}
		return StringValue(string(b))
	case 1:
		return IntValue(int64(r.u64()))
	case 2:
		return FloatValue(math.Float64frombits(r.u64())) // may be NaN/Inf — fine as bytes
	case 3:
		return BoolValue(r.u8()&1 == 1)
	case 4:
		n := r.intn(6)
		b := make([]byte, n)
		for i := range b {
			b[i] = r.u8()
		}
		return BytesValue(b)
	case 5:
		return TimestampValue(time.Unix(int64(r.u64()>>2), 0))
	case 6:
		n := r.intn(4)
		vec := make([]float32, n)
		for i := range vec {
			vec[i] = math.Float32frombits(uint32(r.u64()))
		}
		return VectorValue(vec)
	case 7:
		n := r.intn(4)
		arr := make([]string, n)
		for i := range arr {
			arr[i] = fmt.Sprintf("s%d", r.intn(1000))
		}
		return StringArrayValue(arr)
	case 8:
		n := r.intn(4)
		arr := make([]int64, n)
		for i := range arr {
			arr[i] = int64(r.u64())
		}
		return IntArrayValue(arr)
	case 9:
		n := r.intn(4)
		arr := make([]float64, n)
		for i := range arr {
			arr[i] = math.Float64frombits(r.u64())
		}
		return FloatArrayValue(arr)
	case 10:
		n := r.intn(4)
		arr := make([]bool, n)
		for i := range arr {
			arr[i] = r.u8()&1 == 1
		}
		return BoolArrayValue(arr)
	default:
		return StringValue("") // empty-Data edge case
	}
}

// fuzzBuildSpecs materialises node/edge/mutation specs ONCE from the byte stream,
// so both the JSON-mode and mmap-mode stores are fed byte-for-byte identical input
// (no PRNG-ordering hazard). Counts are bounded so each fuzz exec stays cheap.
func fuzzBuildSpecs(r *byteReader) ([]nodeSpec, []edgeSpec, []mutSpec) {
	nodeCount := 1 + r.intn(32) // 1..32 (never vacuous)
	nodes := make([]nodeSpec, nodeCount)
	for i := range nodes {
		numLabels := r.intn(3) // 0..2 labels
		lbls := make([]string, numLabels)
		for j := range lbls {
			lbls[j] = rpNodeLabels[r.intn(len(rpNodeLabels))]
		}
		np := r.intn(4) // 0..3 props
		props := make(map[string]Value, np)
		for k := 0; k < np; k++ {
			props[rpPropKeys[r.intn(len(rpPropKeys))]] = fuzzValue(r)
		}
		nodes[i] = nodeSpec{labels: lbls, props: props}
	}

	edgeCount := r.intn(48) // 0..47
	edges := make([]edgeSpec, edgeCount)
	for i := range edges {
		np := r.intn(3)
		props := make(map[string]Value, np)
		for k := 0; k < np; k++ {
			props[rpPropKeys[r.intn(len(rpPropKeys))]] = fuzzValue(r)
		}
		edges[i] = edgeSpec{
			fromIdx: r.intn(nodeCount),
			toIdx:   r.intn(nodeCount),
			typ:     rpEdgeTypes[r.intn(len(rpEdgeTypes))],
			weight:  r.finiteWeight(),
			props:   props,
		}
	}

	mutCount := r.intn(16) // 0..15
	kinds := []string{"updateNode", "deleteNode", "createEdge", "deleteEdge", "updateEdge"}
	muts := make([]mutSpec, mutCount)
	for i := range muts {
		muts[i] = mutSpec{
			kind:    kinds[r.intn(len(kinds))],
			nodeIdx: r.intn(nodeCount),
			edgeIdx: r.intn(edgeCount + 1),
			props:   map[string]Value{rpPropKeys[r.intn(len(rpPropKeys))]: fuzzValue(r)},
			weight:  r.finiteWeight(),
		}
	}
	return nodes, edges, muts
}

// FuzzMmapReopenParity is the property-based generalisation of
// TestMmapReopen_RandomizedParity: instead of a single fixed seed, the fuzz
// engine drives graph shape, value types, and the mutation sequence from its
// corpus. The invariant is unchanged and load-bearing — an mmap-reopened store
// must enumerate BYTE-IDENTICALLY to the same data via the JSON path, live and
// across a second reopen.
//
// Seed-corpus entries run on every `go test` (deterministic, always-on CI
// coverage); `go test -fuzz=FuzzMmapReopenParity` drives coverage-guided search.
//
// If this FAILS it is a genuine mmap≠JSON divergence — do NOT weaken it; the
// failing input is written to testdata/fuzz/ for a deterministic repro. Report
// it as BLOCKED with that corpus entry.
func FuzzMmapReopenParity(f *testing.F) {
	// Structurally-diverse seeds so the always-on (non -fuzz) run is non-trivial.
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90})
	f.Add(bytes.Repeat([]byte{0xff}, 64))
	f.Add(bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 32))
	f.Add([]byte("the quick brown fox jumps over the lazy dog 0123456789"))

	f.Fuzz(func(t *testing.T, data []byte) {
		const tenant = rpTenant
		nodeSpecs, edgeSpecs, mutSpecs := fuzzBuildSpecs(&byteReader{b: data})

		jsonDir, mmapDir := t.TempDir(), t.TempDir()

		// Phase 1: populate both from identical specs, close.
		jgs, err := NewGraphStorage(jsonDir)
		if err != nil {
			t.Fatal(err)
		}
		jNodeIDs, jEdgeIDs := populateStore(t, jgs, nodeSpecs, edgeSpecs)
		if err := jgs.Close(); err != nil {
			t.Fatal(err)
		}

		mgs, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
		if err != nil {
			t.Fatal(err)
		}
		mNodeIDs, mEdgeIDs := populateStore(t, mgs, nodeSpecs, edgeSpecs)
		if err := mgs.Close(); err != nil {
			t.Fatal(err)
		}

		// Phase 2: reopen both, assert parity.
		jr, err := NewGraphStorage(jsonDir)
		if err != nil {
			t.Fatal(err)
		}
		defer jr.Close()
		mr, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
		if err != nil {
			t.Fatal(err)
		}
		defer mr.Close()
		if mr.mmapSnap == nil {
			t.Fatal("mmap reopen did not take the mmap path")
		}
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "fuzz-after-first-reopen")

		// Phase 3: identical mutation sequence, assert parity live.
		applyMutSpecs(t, jr, mutSpecs, jNodeIDs, jEdgeIDs)
		applyMutSpecs(t, mr, mutSpecs, mNodeIDs, mEdgeIDs)
		assertFingerprintEqual(t, fingerprintTenant(t, jr, tenant), fingerprintTenant(t, mr, tenant), "fuzz-live-after-mutations")

		// Phase 4: close + reopen both, assert parity survives persistence.
		if err := jr.Close(); err != nil {
			t.Fatal(err)
		}
		if err := mr.Close(); err != nil {
			t.Fatal(err)
		}
		jr2, err := NewGraphStorage(jsonDir)
		if err != nil {
			t.Fatal(err)
		}
		defer jr2.Close()
		mr2, err := NewGraphStorageWithConfig(mmapConfig(mmapDir))
		if err != nil {
			t.Fatal(err)
		}
		defer mr2.Close()
		assertFingerprintEqual(t, fingerprintTenant(t, jr2, tenant), fingerprintTenant(t, mr2, tenant), "fuzz-after-second-reopen")
	})
}

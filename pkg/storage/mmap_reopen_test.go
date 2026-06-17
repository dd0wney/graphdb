package storage

import (
	"errors"
	"sort"
	"strings"
	"testing"
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

// fingerprint captures the public-interface view of a tenant for equality comparison.
type fingerprint struct {
	nodeCount  uint64
	edgeCount  uint64
	personIDs  []uint64
	orgIDs     []uint64
	nameByID   map[uint64]string
	outDegree  map[uint64]int
	outEdgeSig map[uint64]string // node -> sorted "to:weight" of its outgoing edges
}

func fingerprintTenant(t *testing.T, gs *GraphStorage, tenant string) fingerprint {
	t.Helper()
	fp := fingerprint{
		nodeCount:  gs.CountNodesForTenant(tenant),
		edgeCount:  gs.CountEdgesForTenant(tenant),
		nameByID:   map[uint64]string{},
		outDegree:  map[uint64]int{},
		outEdgeSig: map[uint64]string{},
	}
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
		s, _ := got.Properties["name"].AsString()
		fp.nameByID[n.ID] = s
		out, err := gs.GetOutgoingEdgesForTenant(n.ID, tenant)
		if err != nil {
			t.Fatalf("GetOutgoingEdgesForTenant(%d): %v", n.ID, err)
		}
		fp.outDegree[n.ID] = len(out)
		sigs := make([]string, 0, len(out))
		for _, e := range out {
			sigs = append(sigs, itoa(int(e.ToNodeID))+":"+itoa(int(e.Weight)))
		}
		sort.Strings(sigs)
		fp.outEdgeSig[n.ID] = strings.Join(sigs, "|")
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
	for id, name := range want.nameByID {
		if got.nameByID[id] != name {
			t.Errorf("%s: node %d name: want %q got %q", ctx, id, name, got.nameByID[id])
		}
		if got.outDegree[id] != want.outDegree[id] {
			t.Errorf("%s: node %d out-degree: want %d got %d", ctx, id, want.outDegree[id], got.outDegree[id])
		}
		if got.outEdgeSig[id] != want.outEdgeSig[id] {
			t.Errorf("%s: node %d out-edges: want %q got %q", ctx, id, want.outEdgeSig[id], got.outEdgeSig[id])
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

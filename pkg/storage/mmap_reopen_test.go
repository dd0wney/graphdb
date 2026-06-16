package storage

import (
	"sort"
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
	nodeCount uint64
	edgeCount uint64
	personIDs []uint64
	orgIDs    []uint64
	nameByID  map[uint64]string
	outDegree map[uint64]int
}

func fingerprintTenant(t *testing.T, gs *GraphStorage, tenant string) fingerprint {
	t.Helper()
	fp := fingerprint{
		nodeCount: gs.CountNodesForTenant(tenant),
		edgeCount: gs.CountEdgesForTenant(tenant),
		nameByID:  map[uint64]string{},
		outDegree: map[uint64]int{},
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

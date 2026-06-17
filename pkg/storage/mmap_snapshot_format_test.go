package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func sampleNodes() []*Node {
	return []*Node{
		{ID: 1, TenantID: "t1", Labels: []string{"A"}, Properties: map[string]Value{"n": StringValue("one")}, CreatedAt: 10, UpdatedAt: 11},
		{ID: 2, TenantID: "t1", Labels: []string{"A", "B"}, Properties: map[string]Value{"x": IntValue(7)}, CreatedAt: 20, UpdatedAt: 21},
		{ID: 5, TenantID: "t2", Labels: nil, Properties: map[string]Value{}, CreatedAt: 50, UpdatedAt: 51}, // gap at 3,4
	}
}

func sampleEdges() []*Edge {
	return []*Edge{
		{ID: 1, TenantID: "t1", FromNodeID: 1, ToNodeID: 2, Type: "LINKS", Properties: map[string]Value{"w": IntValue(3)}, Weight: 2.5, CreatedAt: 100},
		{ID: 2, TenantID: "t1", FromNodeID: 2, ToNodeID: 5, Type: "OWNS", Properties: map[string]Value{}, Weight: 0, CreatedAt: 200},
	}
}

func sampleMeta() *mmapMetadata {
	return &mmapMetadata{
		Stats:            Statistics{NodeCount: 3, EdgeCount: 2},
		NextNodeID:       6,
		NextEdgeID:       3,
		StickyNodeLabels: []string{"A", "B", "Empty"},
		StickyEdgeTypes:  []string{"LINKS", "OWNS"},
	}
}

func writeSample(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := writeMmapSnapshotData(path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("writeMmapSnapshotData: %v", err)
	}
	return path
}

func TestMmapSnapshot_DataRoundTrip(t *testing.T) {
	m, err := openMmapSnapshot(writeSample(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer m.close()

	if m.nodeCount() != 3 || m.edgeCount() != 2 {
		t.Fatalf("counts: %d/%d want 3/2", m.nodeCount(), m.edgeCount())
	}

	// Nodes incl. the gap (3, 4 absent).
	for _, want := range sampleNodes() {
		got, ok := m.getNode(want.ID)
		if !ok {
			t.Fatalf("node %d missing", want.ID)
		}
		if got.ID != want.ID || got.TenantID != want.TenantID ||
			!reflect.DeepEqual(got.Labels, want.Labels) ||
			!reflect.DeepEqual(got.Properties, want.Properties) ||
			got.CreatedAt != want.CreatedAt || got.UpdatedAt != want.UpdatedAt {
			t.Fatalf("node %d mismatch:\n got %+v\nwant %+v", want.ID, got, want)
		}
	}
	if _, ok := m.getNode(3); ok {
		t.Fatal("node 3 (gap) should be absent")
	}

	// Edges incl. Weight (a regression vs the prototype, which dropped it).
	for _, want := range sampleEdges() {
		got, ok := m.getEdge(want.ID)
		if !ok {
			t.Fatalf("edge %d missing", want.ID)
		}
		if got.Weight != want.Weight || got.FromNodeID != want.FromNodeID ||
			got.ToNodeID != want.ToNodeID || got.Type != want.Type ||
			!reflect.DeepEqual(got.Properties, want.Properties) {
			t.Fatalf("edge %d mismatch:\n got %+v\nwant %+v", want.ID, got, want)
		}
	}

	// Metadata round-trip.
	md := m.metadata()
	want := sampleMeta()
	if md.NextNodeID != want.NextNodeID || md.NextEdgeID != want.NextEdgeID ||
		!reflect.DeepEqual(md.StickyNodeLabels, want.StickyNodeLabels) ||
		!reflect.DeepEqual(md.StickyEdgeTypes, want.StickyEdgeTypes) ||
		md.Stats.NodeCount != want.Stats.NodeCount || md.Stats.EdgeCount != want.Stats.EdgeCount {
		t.Fatalf("metadata mismatch:\n got %+v\nwant %+v", md, want)
	}
}

func TestMmapSnapshot_FieldScan(t *testing.T) {
	m, err := openMmapSnapshot(writeSample(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer m.close()

	seen := map[uint64]string{}
	m.forEachNodeID(func(id uint64, off int64) {
		gotID, tenant, labels := scanNodeFields(m.data, off)
		if gotID != id {
			t.Fatalf("scanNodeFields id %d != dir id %d", gotID, id)
		}
		seen[id] = tenant
		if id == 2 && !reflect.DeepEqual(labels, []string{"A", "B"}) {
			t.Fatalf("node 2 labels via scan = %v", labels)
		}
	})
	if len(seen) != 3 || seen[1] != "t1" || seen[5] != "t2" {
		t.Fatalf("forEachNodeID/scan mismatch: %v", seen)
	}

	edges := 0
	m.forEachEdgeID(func(id uint64, off int64) {
		gotID, from, to, tenant, etype := scanEdgeFields(m.data, off)
		if gotID != id || tenant == "" || etype == "" {
			t.Fatalf("scanEdgeFields bad: id=%d from=%d to=%d t=%q ty=%q", gotID, from, to, tenant, etype)
		}
		edges++
	})
	if edges != 2 {
		t.Fatalf("forEachEdgeID count %d want 2", edges)
	}
}

func TestMmapSnapshot_CopyOnRead(t *testing.T) {
	// Decode from a writable buffer, mutate the buffer, confirm the decoded node is
	// unaffected — proving Value.Data is copied, not aliased (safe after munmap).
	buf := encodeNodeRecord(&Node{ID: 1, TenantID: "t", Labels: []string{"L"},
		Properties: map[string]Value{"k": StringValue("orig")}})
	n := decodeNodeRecordAt(buf, 0)
	for i := range buf {
		buf[i] = 0xFF
	}
	if s, _ := n.Properties["k"].AsString(); s != "orig" {
		t.Fatalf("copy-on-read failed: property mutated to %q after buffer overwrite", s)
	}
	if n.TenantID != "t" || !reflect.DeepEqual(n.Labels, []string{"L"}) {
		t.Fatalf("copy-on-read failed: node header mutated: %+v", n)
	}
}

func TestMmapSnapshot_CRCDetectsCorruption(t *testing.T) {
	path := writeSample(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 0xFF // corrupt the metadata blob (CRC-covered)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openMmapSnapshot(path); err == nil {
		t.Fatal("expected CRC mismatch error on corrupted metadata")
	}
}

func TestMmapSnapshot_TruncatedFile(t *testing.T) {
	path := writeSample(t)
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, b[:mmapHeaderSize+5], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openMmapSnapshot(path); err == nil {
		t.Fatal("expected error opening truncated snapshot")
	}
}

func TestMmapSnapshot_VersionMismatch(t *testing.T) {
	path := writeSample(t)
	b, _ := os.ReadFile(path)
	b[hVersion] = 0xFF // bump version; unmarshalMmapHeader rejects before CRC
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openMmapSnapshot(path); err == nil {
		t.Fatal("expected version-unsupported error")
	}
}

func TestMmapSnapshot_Empty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := writeMmapSnapshotData(path, nil, nil, &mmapMetadata{NextNodeID: 1, NextEdgeID: 1}); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	m, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	defer m.close()
	if m.nodeCount() != 0 || m.edgeCount() != 0 {
		t.Fatalf("empty counts %d/%d", m.nodeCount(), m.edgeCount())
	}
	if _, ok := m.getNode(1); ok {
		t.Fatal("empty store should have no nodes")
	}
}

func TestCSRRunCodec_RoundTrip(t *testing.T) {
	// A CSR run is a length-prefixed []uint64: count(4) then count*uint64.
	in := []uint64{7, 11, 13, 9000000001}
	buf := appendCSRRun(nil, in)

	got, n := readCSRRun(buf, 0)
	if n != len(buf) {
		t.Fatalf("readCSRRun consumed %d, want %d", n, len(buf))
	}
	if len(got) != len(in) {
		t.Fatalf("len got %d want %d", len(got), len(in))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("got[%d]=%d want %d", i, got[i], in[i])
		}
	}

	// Empty run encodes to a 4-byte zero count and decodes to nil.
	empty := appendCSRRun(nil, nil)
	if len(empty) != 4 {
		t.Fatalf("empty run len %d want 4", len(empty))
	}
	if got, _ := readCSRRun(empty, 0); got != nil {
		t.Errorf("empty run decoded to %v want nil", got)
	}

	// Non-zero start offset: two runs back-to-back, decode the second via the
	// offset returned from the first.
	buf2 := appendCSRRun(nil, []uint64{1, 2})
	buf2 = appendCSRRun(buf2, []uint64{3})
	_, after := readCSRRun(buf2, 0)
	second, _ := readCSRRun(buf2, after)
	if len(second) != 1 || second[0] != 3 {
		t.Errorf("second run = %v, want [3]", second)
	}
}

func TestMmapMetadata_TenantStatsRoundTrip(t *testing.T) {
	m := &mmapMetadata{TenantStats: map[string]TenantStats{
		"acme": {NodeCount: 5, EdgeCount: 9, StorageBytes: 100, LastUpdated: 42},
	}}
	b, err := m.marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := unmarshalMmapMetadata(b)
	if err != nil {
		t.Fatal(err)
	}
	want := TenantStats{NodeCount: 5, EdgeCount: 9, StorageBytes: 100, LastUpdated: 42}
	if !reflect.DeepEqual(got.TenantStats["acme"], want) {
		t.Errorf("TenantStats[\"acme\"] = %+v, want %+v", got.TenantStats["acme"], want)
	}
}

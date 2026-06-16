package storage

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestMmapProtoFile_RoundTrip exercises the writer + mmap reader end-to-end on a
// small store, so CI (unix) covers the file format and lazy decode. Not gated.
func TestMmapProtoFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	gs, _ := buildSyntheticStore(t, dir, 500, 800)

	path := filepath.Join(dir, "snapshot.mmap")
	if err := writeMmapSnapshot(path, gs); err != nil {
		t.Fatalf("writeMmapSnapshot: %v", err)
	}

	m, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("openMmapSnapshot: %v", err)
	}
	defer m.close()

	if m.nodeCount() != 500 || m.edgeCount() != 800 {
		t.Fatalf("counts: got %d nodes / %d edges, want 500/800", m.nodeCount(), m.edgeCount())
	}

	// Every node decoded via mmap must equal the live store's node.
	minID, maxID := m.nodeIDRange()
	for id := minID; id <= maxID; id++ {
		want, err := gs.GetNode(id)
		if err != nil {
			t.Fatalf("GetNode(%d): %v", id, err)
		}
		got, ok := m.getNode(id)
		if !ok {
			t.Fatalf("mmap getNode(%d) missing", id)
		}
		if got.ID != want.ID || got.TenantID != want.TenantID ||
			!reflect.DeepEqual(got.Labels, want.Labels) ||
			!reflect.DeepEqual(got.Properties, want.Properties) {
			t.Fatalf("node %d mismatch:\n mmap %+v\n live %+v", id, got, want)
		}
	}

	// Spot-check an edge.
	eMin, _ := m.edgeIDRange()
	wantE, err := gs.GetEdge(eMin)
	if err != nil {
		t.Fatalf("GetEdge(%d): %v", eMin, err)
	}
	gotE, ok := m.getEdge(eMin)
	if !ok || gotE.FromNodeID != wantE.FromNodeID || gotE.ToNodeID != wantE.ToNodeID || gotE.Type != wantE.Type {
		t.Fatalf("edge %d mismatch:\n mmap %+v\n live %+v", eMin, gotE, wantE)
	}

	// Out-of-range / absent lookups must report not-found, not panic.
	if _, ok := m.getNode(maxID + 1); ok {
		t.Fatalf("getNode(%d) should be absent", maxID+1)
	}
	_ = gs.Close()
}

// TestMmapReopen_Synthetic is the head-to-head: JSON reopen vs mmap open + lazy
// materialization at the consumer's scale. Heavy; SKIPPED unless GRAPHDB_REOPEN_BENCH.
//
//	GRAPHDB_REOPEN_BENCH=1 go test ./pkg/storage/ -run TestMmapReopen_Synthetic -count=1 -timeout 600s -v
func TestMmapReopen_Synthetic(t *testing.T) {
	if os.Getenv("GRAPHDB_REOPEN_BENCH") == "" {
		t.Skip("set GRAPHDB_REOPEN_BENCH=1 to run the mmap reopen comparison (heavy)")
	}

	nNodes := envInt("GRAPHDB_REOPEN_NODES", 936908)
	nEdges := envInt("GRAPHDB_REOPEN_EDGES", 1316003)
	dir := t.TempDir()

	gs, _ := buildSyntheticStore(t, dir, nNodes, nEdges)

	// Write the mmap snapshot (store still open), then Close to write the JSON snapshot.
	mmapPath := filepath.Join(dir, "snapshot.mmap")
	if err := writeMmapSnapshot(mmapPath, gs); err != nil {
		t.Fatalf("writeMmapSnapshot: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close/Snapshot: %v", err)
	}

	jsonBytes := fileSize(t, filepath.Join(dir, "snapshot.json"))
	mmapBytes := fileSize(t, mmapPath)
	fmt.Fprintf(os.Stderr, "\n=== mmap vs JSON reopen (%d nodes / %d edges) ===\n", nNodes, nEdges)
	fmt.Fprintf(os.Stderr, "  snapshot sizes: json %.1f MB | mmap %.1f MB\n",
		float64(jsonBytes)/(1<<20), float64(mmapBytes)/(1<<20))
	fmt.Fprintf(os.Stderr, "  %-30s %9s  %10s  %11s  %5s  %9s\n",
		"variant", "wall", "alloc", "mallocs", "numGC", "gcPause")

	// Baseline 1: the production decode (ReadFile + json.Unmarshal), directly
	// comparable to "mmap open + touch-all" since both produce the full graph.
	jsonPayload := readSnapshotPayload(t, filepath.Join(dir, "snapshot.json"))
	measureDecode(t, "JSON ReadFile+Unmarshal", func() int {
		var s benchFullSnapshot
		if err := json.Unmarshal(jsonPayload, &s); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return len(s.Nodes)
	})

	// mmap open — should be ~ms and allocation-free.
	var snap *mmapSnapshot
	measureDecode(t, "mmap open", func() int {
		var err error
		snap, err = openMmapSnapshot(mmapPath)
		if err != nil {
			t.Fatalf("openMmapSnapshot: %v", err)
		}
		return snap.nodeCount()
	})
	defer snap.close()

	// mmap touch-all — materialize every node and edge (full-graph cost).
	nMin, nMax := snap.nodeIDRange()
	eMin, eMax := snap.edgeIDRange()
	measureDecode(t, "mmap touch-all (nodes+edges)", func() int {
		touched := 0
		for id := nMin; id <= nMax; id++ {
			if n, ok := snap.getNode(id); ok {
				parseAllocSink += len(n.Properties)
				touched++
			}
		}
		for id := eMin; id <= eMax; id++ {
			if e, ok := snap.getEdge(id); ok {
				parseAllocSink += int(e.FromNodeID & 1)
				touched++
			}
		}
		return touched
	})

	// mmap random-K — the lazy payoff: open + a handful of reads, K << N.
	const k = 10000
	rng := rand.New(rand.NewSource(7))
	measureDecode(t, fmt.Sprintf("mmap random-%d reads", k), func() int {
		hits := 0
		for i := 0; i < k; i++ {
			id := nMin + uint64(rng.Int63n(int64(nMax-nMin+1)))
			if n, ok := snap.getNode(id); ok {
				parseAllocSink += len(n.Labels)
				hits++
			}
		}
		return hits
	})

	// Correctness: a sample of mmap-decoded nodes must match a fresh JSON reopen.
	gs2, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("NewGraphStorage(reopen): %v", err)
	}
	defer gs2.Close()
	const tenant = "bench-tenant"
	for i := 0; i < 1000; i++ {
		id := nMin + uint64(rng.Int63n(int64(nMax-nMin+1)))
		want, err := gs2.GetNodeForTenant(id, tenant)
		if err != nil {
			t.Fatalf("reopened GetNodeForTenant(%d): %v", id, err)
		}
		got, ok := snap.getNode(id)
		if !ok {
			t.Fatalf("mmap getNode(%d) missing but present in reopen", id)
		}
		if got.ID != want.ID || got.TenantID != want.TenantID ||
			!reflect.DeepEqual(got.Labels, want.Labels) ||
			!reflect.DeepEqual(got.Properties, want.Properties) {
			t.Fatalf("node %d mismatch:\n mmap %+v\n json %+v", id, got, want)
		}
	}
	fmt.Fprintf(os.Stderr, "  correctness: 1000 sampled nodes match the JSON reopen ✓\n")
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Size()
}

func readSnapshotPayload(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	payload, _, _, err := decodeSnapshotEnvelope(raw)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return payload
}

package storage

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// copyDirFiles recursively copies src into dst — simulating "shipping" a data
// directory to a fresh cluster node (e.g. pulled from shared/object storage).
func copyDirFiles(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatalf("copyDirFiles: %v", err)
	}
}

// TestSnapshotHydration_FromShippedFile validates the cluster-bootstrap primitive
// behind "fast reopen → cheap cluster nodes": a snapshot.mmap written by one
// instance, COPIED to a different directory (as a new node would pull it from
// shared/object storage), must map and serve reads correctly and near-instantly
// from a FRESH instance — proving the mmap layout is position-independent (no
// absolute-path or live-process-state assumptions).
//
// This is the smallest evidence that a shipped snapshot serves reads, before any
// cluster layer is designed around it. The position-independence property is
// scale-independent, so the default scale is small enough to run as a normal CI
// gate; scale up for perf numbers via GRAPHDB_HYDRATE_NODES/_EDGES (e.g.
// 936908/1316003 reproduces the ~7ms-map ICIJ-scale result).
func TestSnapshotHydration_FromShippedFile(t *testing.T) {
	nNodes := envInt("GRAPHDB_HYDRATE_NODES", 2000)
	nEdges := envInt("GRAPHDB_HYDRATE_EDGES", 3000)

	// Origin node: build in mmap mode, capture live state, snapshot on Close.
	originDir := t.TempDir()
	origin, _, planted := buildICIJStore(t, mmapConfig(originDir), nNodes, nEdges)
	want := fingerprintTenant(t, origin, coiTenant)
	if err := origin.Close(); err != nil {
		t.Fatalf("origin Close: %v", err)
	}

	// "Ship" the data dir to a fresh node dir (simulates pull-from-object-store).
	nodeDir := t.TempDir()
	copyDirFiles(t, originDir, nodeDir)

	// New node hydrates: map the shipped snapshot, then serve first read.
	mapStart := time.Now()
	node, err := NewGraphStorageWithConfig(mmapConfig(nodeDir))
	if err != nil {
		t.Fatalf("hydrate open: %v", err)
	}
	mapDur := time.Since(mapStart)
	defer node.Close()
	if node.mmapSnap == nil {
		t.Fatal("hydrated node did not take the mmap path — snapshot not served via mmap")
	}

	readStart := time.Now()
	acme, err := node.GetNodeForTenant(planted.acme, coiTenant)
	firstReadDur := time.Since(readStart)
	if err != nil || acme == nil {
		t.Fatalf("hydrated first read failed: %v", err)
	}
	if name, _ := acme.Properties["name"].AsString(); name != "Acme Holdings Ltd" {
		t.Fatalf("hydrated node served wrong data for planted entity: name=%q", name)
	}

	// Correctness gate: the shipped-snapshot node must enumerate byte-identically
	// to the origin's live state (reuses the #440 JSON<->mmap equivalence oracle).
	got := fingerprintTenant(t, node, coiTenant)
	assertFingerprintEqual(t, want, got, "hydrated-from-shipped-snapshot")

	fmt.Fprintf(os.Stderr, "\n=== snapshot hydration (%d nodes / %d edges, snapshot copied to a fresh dir) ===\n", nNodes, nEdges)
	fmt.Fprintf(os.Stderr, "  open (map shipped snapshot)   %10s\n", mapDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  first read served             %10s\n", firstReadDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  parity with origin            OK (byte-identical enumeration)\n")
}

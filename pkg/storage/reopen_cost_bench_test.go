package storage

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

// buildSyntheticStore builds a store of the consumer's reported shape in dir:
// nNodes nodes (label + two packed int locations + a short string), nEdges edges
// across 4 types, one tenant, edge compression at its NewGraphStorage default.
// Returns the open store and the build (cold-rebuild) duration; the caller owns
// Close(). Shared by the reopen-cost and parse-vs-alloc reproductions.
func buildSyntheticStore(tb testing.TB, dir string, nNodes, nEdges int) (*GraphStorage, time.Duration) {
	return buildSyntheticStoreWithConfig(tb, DefaultStorageConfig(dir), nNodes, nEdges)
}

// buildSyntheticStoreWithConfig is buildSyntheticStore parameterized by config, so the
// end-to-end reopen benchmark can build in mmap mode.
func buildSyntheticStoreWithConfig(tb testing.TB, cfg StorageConfig, nNodes, nEdges int) (*GraphStorage, time.Duration) {
	tb.Helper()
	const tenant = "bench-tenant"
	edgeTypes := []string{"LINKS", "MENTIONS", "OWNS", "NEAR"}
	rng := rand.New(rand.NewSource(42)) // deterministic shape across runs

	build := time.Now()
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		tb.Fatalf("NewGraphStorage(build): %v", err)
	}

	nodeIDs := make([]uint64, 0, nNodes)
	const chunk = 50000
	for start := 0; start < nNodes; start += chunk {
		end := start + chunk
		if end > nNodes {
			end = nNodes
		}
		specs := make([]NodeSpec, 0, end-start)
		for i := start; i < end; i++ {
			specs = append(specs, NodeSpec{
				Labels: []string{"Entity"},
				Properties: map[string]Value{
					"name": StringValue("n" + strconv.Itoa(i)),
					"lat":  IntValue(int64(rng.Intn(1 << 20))),
					"lon":  IntValue(int64(rng.Intn(1 << 20))),
					"kind": StringValue("k" + strconv.Itoa(i%32)),
				},
			})
		}
		ids, err := gs.CreateNodesWithTenant(tenant, specs)
		if err != nil {
			tb.Fatalf("CreateNodesWithTenant: %v", err)
		}
		nodeIDs = append(nodeIDs, ids...)
	}

	for start := 0; start < nEdges; start += chunk {
		end := start + chunk
		if end > nEdges {
			end = nEdges
		}
		specs := make([]EdgeSpec, 0, end-start)
		for i := start; i < end; i++ {
			specs = append(specs, EdgeSpec{
				FromID: nodeIDs[rng.Intn(len(nodeIDs))],
				ToID:   nodeIDs[rng.Intn(len(nodeIDs))],
				Type:   edgeTypes[i%len(edgeTypes)],
				Weight: 1.0,
			})
		}
		if _, err := gs.CreateEdgesWithTenant(tenant, specs); err != nil {
			tb.Fatalf("CreateEdgesWithTenant: %v", err)
		}
	}
	return gs, time.Since(build)
}

// TestReopenCost_Synthetic reproduces graphdb ask #1: reopen of a large
// persisted store should cost far less than rebuilding it from scratch. It
// builds the synthetic store, snapshots it, drops the process state, and times
// NewGraphStorage on the populated dir.
//
// Heavy (tens of seconds, ~450MB snapshot) so SKIPPED unless GRAPHDB_REOPEN_BENCH
// is set — must never run in normal CI. Run it with:
//
//	GRAPHDB_REOPEN_BENCH=1 GRAPHDB_LOAD_PROFILE=1 \
//	  go test ./pkg/storage/ -run TestReopenCost_Synthetic -count=1 -timeout 600s -v
//
// Size is tunable for quick iteration: GRAPHDB_REOPEN_NODES / GRAPHDB_REOPEN_EDGES.
func TestReopenCost_Synthetic(t *testing.T) {
	if os.Getenv("GRAPHDB_REOPEN_BENCH") == "" {
		t.Skip("set GRAPHDB_REOPEN_BENCH=1 to run the reopen-cost reproduction (heavy)")
	}

	nNodes := envInt("GRAPHDB_REOPEN_NODES", 936908)
	nEdges := envInt("GRAPHDB_REOPEN_EDGES", 1316003)
	dir := t.TempDir()

	gs, buildDur := buildSyntheticStore(t, dir, nNodes, nEdges)

	// ---- Close() -> snapshot to disk ----
	snap := time.Now()
	if err := gs.Close(); err != nil {
		t.Fatalf("Close/Snapshot: %v", err)
	}
	snapDur := time.Since(snap)

	snapBytes := int64(0)
	if fi, err := os.Stat(dir + "/snapshot.json"); err == nil {
		snapBytes = fi.Size()
	}

	// ---- Reopen (the thing under test) ----
	// Enable the phase profiler for this load so loadFromDisk prints its split.
	t.Setenv("GRAPHDB_LOAD_PROFILE", "1")
	reopen := time.Now()
	gs2, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("NewGraphStorage(reopen): %v", err)
	}
	reopenDur := time.Since(reopen)
	t.Cleanup(func() { _ = gs2.Close() })

	// Cheap correctness floor: counts survived the round-trip.
	if gotNodes := gs2.nodeCount(); gotNodes != nNodes {
		t.Errorf("node count after reopen = %d, want %d", gotNodes, nNodes)
	}
	if gotEdges := gs2.edgeCount(); gotEdges != nEdges {
		t.Errorf("edge count after reopen = %d, want %d", gotEdges, nEdges)
	}

	ratio := float64(reopenDur) / float64(buildDur)
	fmt.Fprintf(os.Stderr, "\n=== Reopen cost (synthetic %d nodes / %d edges) ===\n", nNodes, nEdges)
	fmt.Fprintf(os.Stderr, "  Cold build (from scratch)   %8s\n", buildDur.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "  Close()->snapshot (%6.1f MB) %8s\n", float64(snapBytes)/(1<<20), snapDur.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "  Reopen (NewGraphStorage)    %8s\n", reopenDur.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "  reopen / rebuild ratio      %8.2f   (acceptance: << 1.0)\n", ratio)
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

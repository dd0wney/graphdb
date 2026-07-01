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

// --- coi-screen (ICIJ-shaped) reopen validation -----------------------------
//
// The coi-screen consumer (sibling repo ../coi-screen, not vendored here) does
// NOT enumerate the full graph: it resolves parties via a label-index lookup
// (GetNodesByLabelForTenant on "Officer"/"Entity") then runs a bounded adjacency
// BFS via GetOutgoing/IncomingEdgesForTenant. This harness reproduces that access
// pattern at ICIJ scale to answer decision B-1 — is full-graph enumeration on
// reopen a hot path? — by measuring the coi hot path (label + adjacency) against
// the full-enumeration path it avoids, in both mmap and JSON modes.

type icijPlanted struct {
	acme, smith, doe uint64
}

// buildICIJStore builds an ICIJ-shaped store (Entity/Officer/Intermediary/Address
// nodes; officer_of / intermediary_of / registered_address edges) at the given
// scale, planting a clean 2-hop conflict: officers "Robert Smith" and "Jane Doe"
// both officer_of the shared entity "Acme Holdings Ltd" (mirrors gen-icij-synth.py).
// Deterministic (seed 1729). Returns the open store, cold-build duration, and the
// planted IDs. Caller owns Close().
func buildICIJStore(tb testing.TB, cfg StorageConfig, nNodes, nEdges int) (*GraphStorage, time.Duration, icijPlanted) {
	tb.Helper()
	const tenant = coiTenant
	rng := rand.New(rand.NewSource(1729))

	// Proportions from gen-icij-synth.py (20K/18K/2K/10K).
	nEntity := nNodes * 40 / 100
	nOfficer := nNodes * 36 / 100
	nInter := nNodes * 4 / 100
	nAddress := nNodes - nEntity - nOfficer - nInter

	build := time.Now()
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		tb.Fatalf("NewGraphStorage(build): %v", err)
	}

	mkNodes := func(n int, label string, name func(int) string) []uint64 {
		ids := make([]uint64, 0, n)
		const chunk = 50000
		for start := 0; start < n; start += chunk {
			end := start + chunk
			if end > n {
				end = n
			}
			specs := make([]NodeSpec, 0, end-start)
			for i := start; i < end; i++ {
				specs = append(specs, NodeSpec{
					Labels:     []string{label},
					Properties: map[string]Value{"name": StringValue(name(i))},
				})
			}
			got, err := gs.CreateNodesWithTenant(tenant, specs)
			if err != nil {
				tb.Fatalf("CreateNodesWithTenant(%s): %v", label, err)
			}
			ids = append(ids, got...)
		}
		return ids
	}

	// Plant named parties at deterministic positions so the resolve scan finds them.
	entities := mkNodes(nEntity, "Entity", func(i int) string {
		if i == 0 {
			return "Acme Holdings Ltd"
		}
		return "Entity " + strconv.Itoa(i) + " Ltd"
	})
	officers := mkNodes(nOfficer, "Officer", func(i int) string {
		switch i {
		case 0:
			return "Robert Smith"
		case 1:
			return "Jane Doe"
		}
		return "Officer Person " + strconv.Itoa(i)
	})
	inters := mkNodes(nInter, "Intermediary", func(i int) string { return "Law Firm " + strconv.Itoa(i) })
	addresses := mkNodes(nAddress, "Address", func(i int) string { return strconv.Itoa(i) + " Offshore Plaza" })

	planted := icijPlanted{acme: entities[0], smith: officers[0], doe: officers[1]}

	// Edges: planted conflict first, then a deterministic type mix up to nEdges.
	edges := make([]EdgeSpec, 0, nEdges)
	edges = append(edges,
		EdgeSpec{FromID: planted.smith, ToID: planted.acme, Type: "officer_of", Weight: 1},
		EdgeSpec{FromID: planted.doe, ToID: planted.acme, Type: "officer_of", Weight: 1},
	)
	for len(edges) < nEdges {
		switch rng.Intn(10) {
		case 0, 1, 2, 3, 4, 5: // 60% officer_of (Officer -> Entity)
			edges = append(edges, EdgeSpec{FromID: officers[rng.Intn(nOfficer)], ToID: entities[rng.Intn(nEntity)], Type: "officer_of", Weight: 1})
		case 6, 7: // 20% intermediary_of (Intermediary -> Entity)
			edges = append(edges, EdgeSpec{FromID: inters[rng.Intn(nInter)], ToID: entities[rng.Intn(nEntity)], Type: "intermediary_of", Weight: 1})
		default: // 20% registered_address (Entity -> Address)
			edges = append(edges, EdgeSpec{FromID: entities[rng.Intn(nEntity)], ToID: addresses[rng.Intn(nAddress)], Type: "registered_address", Weight: 1})
		}
	}
	const chunk = 50000
	for start := 0; start < len(edges); start += chunk {
		end := start + chunk
		if end > len(edges) {
			end = len(edges)
		}
		if _, err := gs.CreateEdgesWithTenant(tenant, edges[start:end]); err != nil {
			tb.Fatalf("CreateEdgesWithTenant: %v", err)
		}
	}
	return gs, time.Since(build), planted
}

const coiTenant = "coi-tenant"

// coiResolve mimics the consumer's Resolve stage: a label-index lookup on
// "Officer" followed by an in-bucket scan for the named party. Returns the ID and
// the bucket size scanned (the linear component).
func coiResolve(gs *GraphStorage, name string) (uint64, int) {
	officers := gs.GetNodesByLabelForTenant(coiTenant, "Officer")
	for _, n := range officers {
		if s, _ := n.Properties["name"].AsString(); s == name {
			return n.ID, len(officers)
		}
	}
	return 0, len(officers)
}

// coiConflict mimics the consumer's Connect stage: a bounded adjacency BFS from
// start to target over officer_of edges only (both directions — shared-entity
// detection), capped at maxHops and skipping officer_of-hubs above maxDegree.
func coiConflict(gs *GraphStorage, start, target uint64, maxHops, maxDegree int) (bool, error) {
	type item struct {
		id  uint64
		hop int
	}
	visited := map[uint64]bool{start: true}
	queue := []item{{start, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.id == target {
			return true, nil
		}
		if cur.hop >= maxHops {
			continue
		}
		out, err := gs.GetOutgoingEdgesForTenant(cur.id, coiTenant)
		if err != nil {
			return false, err
		}
		in, err := gs.GetIncomingEdgesForTenant(cur.id, coiTenant)
		if err != nil {
			return false, err
		}
		neighbors := make([]uint64, 0, len(out)+len(in))
		for _, e := range out {
			if e.Type == "officer_of" {
				neighbors = append(neighbors, e.ToNodeID)
			}
		}
		for _, e := range in {
			if e.Type == "officer_of" {
				neighbors = append(neighbors, e.FromNodeID)
			}
		}
		if len(neighbors) > maxDegree { // bound hub expansion, as the consumer does
			continue
		}
		for _, id := range neighbors {
			if !visited[id] {
				visited[id] = true
				queue = append(queue, item{id, cur.hop + 1})
			}
		}
	}
	return false, nil
}

// TestReopenCost_CoiScreen answers decision B-1: at ICIJ scale, is full-graph
// enumeration on reopen a hot path for the coi-screen workload? It builds an
// ICIJ-shaped store in BOTH mmap and JSON modes, reopens, and measures the coi
// hot path (label resolve + bounded adjacency BFS) against the full-enumeration
// path the consumer avoids. Also a correctness gate: both modes must flag the
// planted Smith/Doe/Acme conflict identically.
//
// Heavy — SKIPPED unless GRAPHDB_REOPEN_BENCH is set. Run with:
//
//	GRAPHDB_REOPEN_BENCH=1 go test ./pkg/storage/ -run TestReopenCost_CoiScreen -count=1 -timeout 900s -v
//
// Size tunable via GRAPHDB_REOPEN_NODES / GRAPHDB_REOPEN_EDGES.
func TestReopenCost_CoiScreen(t *testing.T) {
	if os.Getenv("GRAPHDB_REOPEN_BENCH") == "" {
		t.Skip("set GRAPHDB_REOPEN_BENCH=1 to run the coi-screen reopen validation (heavy)")
	}
	nNodes := envInt("GRAPHDB_REOPEN_NODES", 936908)
	nEdges := envInt("GRAPHDB_REOPEN_EDGES", 1316003)

	type modeResult struct {
		mode                                    string
		build, snap, reopen, resolve, bfs, enum time.Duration
		bucket, enumCount                       int
		flagged                                 bool
	}

	run := func(mode string, cfg func(dir string) StorageConfig, mmap bool) modeResult {
		dir := t.TempDir()
		gs, buildDur, planted := buildICIJStore(t, cfg(dir), nNodes, nEdges)

		snap := time.Now()
		if err := gs.Close(); err != nil {
			t.Fatalf("[%s] Close: %v", mode, err)
		}
		snapDur := time.Since(snap)

		reopen := time.Now()
		gs2, err := NewGraphStorageWithConfig(cfg(dir))
		if err != nil {
			t.Fatalf("[%s] reopen: %v", mode, err)
		}
		reopenDur := time.Since(reopen)
		t.Cleanup(func() { _ = gs2.Close() })
		if mmap && gs2.mmapSnap == nil {
			t.Fatalf("[%s] reopen did not take the mmap path", mode)
		}

		// coi hot path: resolve two parties (label lookup) then connect (adjacency BFS).
		rs := time.Now()
		smithID, bucket := coiResolve(gs2, "Robert Smith")
		doeID, _ := coiResolve(gs2, "Jane Doe")
		resolveDur := time.Since(rs)
		if smithID != planted.smith || doeID != planted.doe {
			t.Fatalf("[%s] resolve mismatch: smith=%d/%d doe=%d/%d", mode, smithID, planted.smith, doeID, planted.doe)
		}
		bs := time.Now()
		flagged, err := coiConflict(gs2, smithID, doeID, 2, 1000)
		if err != nil {
			t.Fatalf("[%s] coiConflict: %v", mode, err)
		}
		bfsDur := time.Since(bs)

		// full-graph enumeration: the path coi-screen does NOT use (B-1 contrast).
		es := time.Now()
		all := gs2.GetAllNodesForTenant(coiTenant)
		enumDur := time.Since(es)

		return modeResult{mode, buildDur, snapDur, reopenDur, resolveDur, bfsDur, enumDur, bucket, len(all), flagged}
	}

	results := []modeResult{
		run("mmap", mmapConfig, true),
		run("json", func(dir string) StorageConfig { return DefaultStorageConfig(dir) }, false),
	}

	for _, r := range results {
		if !r.flagged {
			t.Errorf("[%s] planted Smith/Doe/Acme conflict NOT flagged — coi pattern broken", r.mode)
		}
		if r.enumCount != nNodes {
			t.Errorf("[%s] enumeration returned %d nodes, want %d", r.mode, r.enumCount, nNodes)
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== coi-screen reopen validation (ICIJ-shaped %d nodes / %d edges) ===\n", nNodes, nEdges)
	fmt.Fprintf(os.Stderr, "%-6s %10s %10s %10s | %10s %10s | %12s\n",
		"mode", "build", "snapshot", "reopen", "resolve", "bfs(2hop)", "full-enum")
	for _, r := range results {
		fmt.Fprintf(os.Stderr, "%-6s %10s %10s %10s | %10s %10s | %12s  (bucket=%d flagged=%v)\n",
			r.mode,
			r.build.Round(time.Millisecond), r.snap.Round(time.Millisecond), r.reopen.Round(time.Millisecond),
			r.resolve.Round(time.Microsecond), r.bfs.Round(time.Microsecond), r.enum.Round(time.Millisecond),
			r.bucket, r.flagged)
	}
	fmt.Fprintf(os.Stderr, "\nB-1: coi hot path = resolve(label) + bfs(adjacency); full-enum is the path coi AVOIDS.\n")
}

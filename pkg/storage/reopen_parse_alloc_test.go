package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// Parse-vs-allocation decomposition for graphdb ask #1. The reopen-cost spike
// (SPIKE_REOPEN_COST_2026-06-16.md) found json.Unmarshal is ~74% of reopen, but
// that figure conflates two costs: scanning the JSON bytes, and allocating the
// in-memory graph (936k map[string]Value property bags + Value boxing + the flat
// node/edge maps). Which dominates decides the fix:
//
//   - parse-dominated  -> a faster binary/streaming decoder into the same
//     structures suffices.
//   - alloc-dominated  -> the representation must change (mmap + lazy property
//     materialization), because any format that still builds map[string]Value
//     pays the same allocation/GC bill.
//
// This runs four decodes over the SAME snapshot payload:
//  1. json.Valid           — scan only, zero allocation (the parse floor).
//  2. props=RawMessage     — full structure, but property bags left unparsed
//     (isolates the property-bag map/Value cost: Full minus this = that cost).
//  3. full (real types)    — the production decode; the 74% baseline.
//  4. full, GC disabled    — isolates GC overhead within the decode.
//
// Heavy; SKIPPED unless GRAPHDB_REOPEN_BENCH=1. Run with:
//
//	GRAPHDB_REOPEN_BENCH=1 \
//	  go test ./pkg/storage/ -run TestReopenParseVsAlloc_Synthetic -count=1 -timeout 600s -v

// benchFullSnapshot mirrors the anonymous decode target in loadFromDisk (real
// Node/Edge with map[string]Value property bags).
type benchFullSnapshot struct {
	Nodes         map[uint64]*Node
	Edges         map[uint64]*Edge
	NodesByLabel  map[string][]uint64
	EdgesByType   map[string][]uint64
	OutgoingEdges map[uint64][]uint64
	IncomingEdges map[uint64][]uint64
	NextNodeID    uint64
	NextEdgeID    uint64
}

// benchSkelNode/Edge mirror Node/Edge field-for-field EXCEPT Properties, which
// is captured as raw bytes instead of decoded into a map[string]Value. Same JSON
// field names => same parse work, minus the property-bag allocation.
type benchSkelNode struct {
	ID         uint64
	TenantID   string
	Labels     []string
	Properties json.RawMessage
	CreatedAt  int64
	UpdatedAt  int64
}

type benchSkelEdge struct {
	ID         uint64
	TenantID   string
	FromNodeID uint64
	ToNodeID   uint64
	Type       string
	Properties json.RawMessage
	Weight     float64
	CreatedAt  int64
}

type benchSkelSnapshot struct {
	Nodes         map[uint64]*benchSkelNode
	Edges         map[uint64]*benchSkelEdge
	NodesByLabel  map[string][]uint64
	EdgesByType   map[string][]uint64
	OutgoingEdges map[uint64][]uint64
	IncomingEdges map[uint64][]uint64
	NextNodeID    uint64
	NextEdgeID    uint64
}

func TestReopenParseVsAlloc_Synthetic(t *testing.T) {
	if os.Getenv("GRAPHDB_REOPEN_BENCH") == "" {
		t.Skip("set GRAPHDB_REOPEN_BENCH=1 to run the parse-vs-alloc reproduction (heavy)")
	}

	nNodes := envInt("GRAPHDB_REOPEN_NODES", 936908)
	nEdges := envInt("GRAPHDB_REOPEN_EDGES", 1316003)
	dir := t.TempDir()

	gs, _ := buildSyntheticStore(t, dir, nNodes, nEdges)
	if err := gs.Close(); err != nil {
		t.Fatalf("Close/Snapshot: %v", err)
	}

	// Read the snapshot and strip the envelope to get the raw JSON payload — the
	// exact bytes loadFromDisk hands to json.Unmarshal.
	raw, err := os.ReadFile(dir + "/snapshot.json")
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	payload, _, _, err := decodeSnapshotEnvelope(raw)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	fmt.Fprintf(os.Stderr, "\n=== Parse-vs-alloc (synthetic %d nodes / %d edges, payload %.1f MB) ===\n",
		nNodes, nEdges, float64(len(payload))/(1<<20))
	fmt.Fprintf(os.Stderr, "  %-30s %9s  %10s  %11s  %5s  %9s\n",
		"variant", "wall", "alloc", "mallocs", "numGC", "gcPause")

	// 1. Scan only — pure lexing, no tree, no allocation. The parse floor.
	measureDecode(t, "1. json.Valid (scan only)", func() int {
		if !json.Valid(payload) {
			t.Fatal("payload is not valid JSON")
		}
		return len(payload)
	})

	// 2. Full structure, property bags left as raw bytes.
	measureDecode(t, "2. Unmarshal props=RawMessage", func() int {
		var s benchSkelSnapshot
		if err := json.Unmarshal(payload, &s); err != nil {
			t.Fatalf("skeleton unmarshal: %v", err)
		}
		if len(s.Nodes) != nNodes {
			t.Fatalf("skeleton nodes = %d, want %d", len(s.Nodes), nNodes)
		}
		return len(s.Nodes)
	})

	// 3. The production decode into real types (the 74% baseline).
	measureDecode(t, "3. Unmarshal full (real types)", func() int {
		var s benchFullSnapshot
		if err := json.Unmarshal(payload, &s); err != nil {
			t.Fatalf("full unmarshal: %v", err)
		}
		if len(s.Nodes) != nNodes {
			t.Fatalf("full nodes = %d, want %d", len(s.Nodes), nNodes)
		}
		return len(s.Nodes)
	})

	// 4. Full decode with GC disabled — wall-time delta vs (3) is GC overhead.
	old := debug.SetGCPercent(-1)
	measureDecode(t, "4. Unmarshal full (GC off)", func() int {
		var s benchFullSnapshot
		if err := json.Unmarshal(payload, &s); err != nil {
			t.Fatalf("full unmarshal (GC off): %v", err)
		}
		return len(s.Nodes)
	})
	debug.SetGCPercent(old)

	fmt.Fprintf(os.Stderr, "\nReading: (3-1)=alloc+tree over scan; (3-2)=property-bag cost; (3-4)=GC overhead.\n")
}

// measureDecode runs fn under clean GC state and reports wall time plus
// allocation/GC accounting. fn returns a value that is consumed to defeat
// dead-code elimination of the decode.
func measureDecode(t *testing.T, name string, fn func() int) {
	t.Helper()
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	start := time.Now()
	sink := fn()
	d := time.Since(start)
	runtime.ReadMemStats(&m1)
	parseAllocSink += sink

	fmt.Fprintf(os.Stderr, "  %-30s %9s  %8.2fGB  %11d  %5d  %9s\n",
		name,
		d.Round(time.Millisecond),
		float64(m1.TotalAlloc-m0.TotalAlloc)/(1<<30),
		m1.Mallocs-m0.Mallocs,
		m1.NumGC-m0.NumGC,
		time.Duration(m1.PauseTotalNs-m0.PauseTotalNs).Round(time.Millisecond),
	)
}

// parseAllocSink defeats dead-code elimination of decode results.
var parseAllocSink int

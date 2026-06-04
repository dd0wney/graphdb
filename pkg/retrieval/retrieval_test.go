package retrieval

import (
	"context"
	"testing"

	"github.com/dd0wney/graphdb/pkg/search"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// retrievalFixture builds a minimal two-tenant corpus suitable for
// the package's tests. Mirrors the audit-regression-suite pattern:
// parallel shapes in two tenants so isolation tests catch
// coincidence-of-emptiness false negatives.
type retrievalFixture struct {
	graph     *storage.GraphStorage
	searchIdx *search.TenantIndexes
	lsaIdx    *search.TenantLSAIndexes
	retriever *Retriever
	a         fixtureTenant
	b         fixtureTenant
}

type fixtureTenant struct {
	hubID   uint64 // labeled "Doc", body mentions "graph database"
	leaf1ID uint64 // labeled "Doc", connected to hub via REFERENCES
	leaf2ID uint64 // labeled "Note", farther from hub (2 hops)
}

func setupFixture(t *testing.T) *retrievalFixture {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	mkTenant := func(name, suffix string) fixtureTenant {
		hub, err := gs.CreateNodeWithTenant(name, []string{"Doc"}, map[string]storage.Value{
			"body": storage.StringValue("graph database " + suffix + " hub content"),
		})
		if err != nil {
			t.Fatalf("hub %s: %v", name, err)
		}
		leaf1, err := gs.CreateNodeWithTenant(name, []string{"Doc"}, map[string]storage.Value{
			"body": storage.StringValue("first level " + suffix + " content reachable from hub"),
		})
		if err != nil {
			t.Fatalf("leaf1 %s: %v", name, err)
		}
		leaf2, err := gs.CreateNodeWithTenant(name, []string{"Note"}, map[string]storage.Value{
			"body": storage.StringValue("second level " + suffix + " content two hops from hub"),
		})
		if err != nil {
			t.Fatalf("leaf2 %s: %v", name, err)
		}
		if _, err := gs.CreateEdgeWithTenant(name, hub.ID, leaf1.ID, "REFERENCES", nil, 1.0); err != nil {
			t.Fatalf("hub→leaf1 %s: %v", name, err)
		}
		if _, err := gs.CreateEdgeWithTenant(name, leaf1.ID, leaf2.ID, "REFERENCES", nil, 1.0); err != nil {
			t.Fatalf("leaf1→leaf2 %s: %v", name, err)
		}
		return fixtureTenant{hubID: hub.ID, leaf1ID: leaf1.ID, leaf2ID: leaf2.ID}
	}

	a := mkTenant("tenant-A", "A")
	b := mkTenant("tenant-B", "B")

	searchIdx := search.NewTenantIndexes(gs)
	if err := searchIdx.IndexForTenant("tenant-A", []string{"Doc", "Note"}, []string{"body"}); err != nil {
		t.Fatalf("index A: %v", err)
	}
	if err := searchIdx.IndexForTenant("tenant-B", []string{"Doc", "Note"}, []string{"body"}); err != nil {
		t.Fatalf("index B: %v", err)
	}
	lsaIdx := search.NewTenantLSAIndexes()

	return &retrievalFixture{
		graph:     gs,
		searchIdx: searchIdx,
		lsaIdx:    lsaIdx,
		retriever: NewRetriever(gs, searchIdx, lsaIdx),
		a:         a,
		b:         b,
	}
}

// TestRetrieve_SeedAndExpansion is the cardinal happy-path test: a
// query that matches tenant-A's hub should return the hub plus
// 1-hop expansion (leaf1) when MaxHops=1, and additionally leaf2
// when MaxHops=2. Pins the BFS expansion contract.
func TestRetrieve_SeedAndExpansion(t *testing.T) {
	fix := setupFixture(t)

	t.Run("MaxHops=0: seeds only", func(t *testing.T) {
		res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{
			K:       10,
			MaxHops: 0,
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		ids := chunkIDs(res.Chunks)
		if !contains(ids, fix.a.hubID) {
			t.Errorf("hub %d missing from MaxHops=0 result: %v", fix.a.hubID, ids)
		}
		// MaxHops=0 means no BFS expansion. leaf1 isn't in seeds (its
		// body says "first level content reachable from hub", not
		// "graph database"), so it must NOT appear.
		if contains(ids, fix.a.leaf1ID) {
			t.Errorf("leaf1 %d leaked into MaxHops=0 result (must require expansion)", fix.a.leaf1ID)
		}
	})

	t.Run("MaxHops=1: seeds + 1-hop", func(t *testing.T) {
		res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{
			K:       10,
			MaxHops: 1,
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		ids := chunkIDs(res.Chunks)
		if !contains(ids, fix.a.hubID) || !contains(ids, fix.a.leaf1ID) {
			t.Errorf("MaxHops=1: want hub+leaf1, got %v", ids)
		}
		// leaf2 is 2 hops away via leaf1; must NOT appear at MaxHops=1.
		if contains(ids, fix.a.leaf2ID) {
			t.Errorf("leaf2 %d leaked into MaxHops=1 result", fix.a.leaf2ID)
		}
	})

	t.Run("MaxHops=2: seeds + 2-hop reach", func(t *testing.T) {
		res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{
			K:       10,
			MaxHops: 2,
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		ids := chunkIDs(res.Chunks)
		for _, want := range []uint64{fix.a.hubID, fix.a.leaf1ID, fix.a.leaf2ID} {
			if !contains(ids, want) {
				t.Errorf("MaxHops=2: missing node %d (got %v)", want, ids)
			}
		}
	})
}

// TestRetrieve_TenantIsolation pins the cross-tenant contract. A
// query for "graph database" runs against both tenants. Each tenant
// must see only its own nodes — no leak of foreign-tenant IDs.
func TestRetrieve_TenantIsolation(t *testing.T) {
	fix := setupFixture(t)

	t.Run("tenant-A sees only A's nodes", func(t *testing.T) {
		res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{
			K:       10,
			MaxHops: 2,
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		ids := chunkIDs(res.Chunks)
		for _, leak := range []uint64{fix.b.hubID, fix.b.leaf1ID, fix.b.leaf2ID} {
			if contains(ids, leak) {
				t.Errorf("tenant-A leaked tenant-B node %d (got %v)", leak, ids)
			}
		}
	})

	t.Run("tenant-B sees only B's nodes", func(t *testing.T) {
		res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-B", Options{
			K:       10,
			MaxHops: 2,
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		ids := chunkIDs(res.Chunks)
		for _, leak := range []uint64{fix.a.hubID, fix.a.leaf1ID, fix.a.leaf2ID} {
			if contains(ids, leak) {
				t.Errorf("tenant-B leaked tenant-A node %d (got %v)", leak, ids)
			}
		}
	})

	t.Run("tenant-C with no data sees nothing", func(t *testing.T) {
		res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-C", Options{
			K:       10,
			MaxHops: 2,
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if len(res.Chunks) != 0 {
			t.Errorf("tenant-C: want 0 chunks, got %d (%v)", len(res.Chunks), chunkIDs(res.Chunks))
		}
	})
}

// TestRetrieve_SourcePath pins the citation-metadata contract: every
// chunk's SourcePath starts at a seed and ends at the chunk's nodeID.
// For seeds themselves, SourcePath = [seedID] (length 1).
func TestRetrieve_SourcePath(t *testing.T) {
	fix := setupFixture(t)

	res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{
		K:       10,
		MaxHops: 2,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	for _, c := range res.Chunks {
		if len(c.SourcePath) == 0 {
			t.Errorf("chunk %d: SourcePath is empty (must contain at least the node itself)", c.NodeID)
			continue
		}
		if c.SourcePath[len(c.SourcePath)-1] != c.NodeID {
			t.Errorf("chunk %d: SourcePath must end at NodeID, got %v", c.NodeID, c.SourcePath)
		}
		// Seeds have path length 1; expanded nodes have length > 1.
		if c.NodeID == fix.a.hubID && len(c.SourcePath) != 1 {
			t.Errorf("hub seed: SourcePath should be [%d], got %v", c.NodeID, c.SourcePath)
		}
		if c.NodeID == fix.a.leaf2ID && len(c.SourcePath) < 2 {
			t.Errorf("leaf2 (expanded): SourcePath should have ≥2 entries, got %v", c.SourcePath)
		}
	}
}

// TestRetrieve_TokenBudget pins that lower-scored chunks are dropped
// when total content exceeds MaxTokens. Sets a tiny budget that can
// only fit the highest-scored chunk; expects len(chunks) == 1.
func TestRetrieve_TokenBudget(t *testing.T) {
	fix := setupFixture(t)

	res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{
		K:         10,
		MaxHops:   2,
		MaxTokens: 5, // ~3-4 words; only one chunk fits
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	// Per spike §6 risk #1: highest-scored chunk is always returned
	// even if it alone exceeds the budget. So result is non-empty;
	// the contract is "no extra chunks beyond the first that fits."
	if len(res.Chunks) != 1 {
		t.Errorf("MaxTokens=5: want 1 chunk (single-chunk-exceeds-budget case), got %d", len(res.Chunks))
	}
}

// TestRetrieve_HardNodeCap pins the safety cap when expansion would
// otherwise return more than HardNodeCap nodes. Builds a single seed
// with HardNodeCap+10 outgoing edges (a star); expansion at MaxHops=1
// must stop at the cap.
func TestRetrieve_HardNodeCap(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	hub, _ := gs.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("graph database hub"),
	})
	// Create HardNodeCap + 10 leaves, all directly connected to hub.
	for i := 0; i < HardNodeCap+10; i++ {
		leaf, _ := gs.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
			"body": storage.StringValue("leaf"),
		})
		if _, err := gs.CreateEdgeWithTenant("tenant-A", hub.ID, leaf.ID, "REL", nil, 1.0); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	searchIdx := search.NewTenantIndexes(gs)
	if err := searchIdx.IndexForTenant("tenant-A", []string{"Doc"}, []string{"body"}); err != nil {
		t.Fatalf("index: %v", err)
	}
	r := NewRetriever(gs, searchIdx, search.NewTenantLSAIndexes())

	res, err := r.Retrieve(context.Background(), "graph database", "tenant-A", Options{
		K:         HardNodeCap + 100, // ask for more than the cap
		MaxHops:   1,
		MaxTokens: 1 << 30, // budget irrelevant
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	// Total visited ≤ HardNodeCap; chunks are at most that count.
	if len(res.Chunks) > HardNodeCap {
		t.Errorf("HardNodeCap breached: want ≤%d chunks, got %d", HardNodeCap, len(res.Chunks))
	}
}

// TestRetrieve_EmptyCorpus: a query against a tenant with no nodes
// returns no chunks and no error. Forwards the hybrid Degraded flag.
func TestRetrieve_EmptyCorpus(t *testing.T) {
	fix := setupFixture(t)

	res, err := fix.retriever.Retrieve(context.Background(), "anything", "tenant-empty", Options{
		K:       10,
		MaxHops: 2,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.Chunks) != 0 {
		t.Errorf("empty corpus: want 0 chunks, got %d", len(res.Chunks))
	}
}

// TestRetrieve_LabelFilter pins that opts.Labels restricts seed-stage
// candidates. A query that matches both hub (Doc) and leaf2 (Note)
// content with Labels=["Doc"] should only seed from Doc nodes.
func TestRetrieve_LabelFilter(t *testing.T) {
	fix := setupFixture(t)

	res, err := fix.retriever.Retrieve(context.Background(), "content", "tenant-A", Options{
		K:       10,
		MaxHops: 0, // seeds only — exercise the label filter directly
		Labels:  []string{"Doc"},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	for _, c := range res.Chunks {
		if c.Label == "Note" {
			t.Errorf("Labels=[Doc] but result includes Note-labeled node %d", c.NodeID)
		}
	}
}

// TestRetrieve_DefaultsApplied pins that zero-value Options get
// sensible defaults rather than empty results / divide-by-zero.
func TestRetrieve_DefaultsApplied(t *testing.T) {
	fix := setupFixture(t)

	res, err := fix.retriever.Retrieve(context.Background(), "graph database", "tenant-A", Options{})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	// Defaults: K=10, MaxHops=2 → should reach all 3 nodes.
	if len(res.Chunks) == 0 {
		t.Errorf("zero-value Options: want non-empty result, got 0 chunks")
	}
}

// helpers
func chunkIDs(chunks []Chunk) []uint64 {
	out := make([]uint64, len(chunks))
	for i, c := range chunks {
		out[i] = c.NodeID
	}
	return out
}

func contains(ids []uint64, want uint64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

package search

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Audit F2 #2 (2026-05-08): tests for SearchHybridForTenant. The
// existing pkg/api/handlers_hybrid_search_test.go tests the HTTP
// handler end-to-end and stays authoritative for the response-shape
// contract. This file pins the package-level primitive so non-handler
// callers (notably pkg/retrieval/ for F2 GraphRAG) have a stable
// surface.

func TestSearchHybridForTenant_TenantIsolation(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Tenant-A: "alpha" appears only here.
	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("alpha tenantA content"),
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := gs.CreateNodeWithTenant("tenant-B", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("beta tenantB content"),
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("tenant-A", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant A: %v", err)
	}
	if err := ti.IndexForTenant("tenant-B", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant B: %v", err)
	}

	// LSA indexes are optional; the function should degrade gracefully.
	tli := NewTenantLSAIndexes()

	t.Run("tenant-A finds its own", func(t *testing.T) {
		res, err := SearchHybridForTenant(ti, tli, "tenant-A", "alpha", HybridSearchOpts{
			OverFetchK: 10,
			Alpha:      0.5,
		})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res.Hits) != 1 {
			t.Errorf("tenant-A 'alpha': want 1 hit, got %d", len(res.Hits))
		}
		if res.Degraded != "no-lsa-index" {
			t.Errorf("expected 'no-lsa-index' degraded flag (no LSA built), got %q", res.Degraded)
		}
	})

	t.Run("tenant-B does not see A's content", func(t *testing.T) {
		res, err := SearchHybridForTenant(ti, tli, "tenant-B", "alpha", HybridSearchOpts{
			OverFetchK: 10,
			Alpha:      0.5,
		})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(res.Hits) != 0 {
			t.Errorf("tenant-B querying 'alpha' (A's content): want 0 hits, got %d (isolation breach)", len(res.Hits))
		}
	})
}

// TestSearchHybridForTenant_AlphaClamping pins the alpha-clamp
// behavior callers depend on. Out-of-range values shouldn't error or
// produce NaN scores — they get clamped to [0, 1].
func TestSearchHybridForTenant_AlphaClamping(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("graph database content"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("tenant-A", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant: %v", err)
	}
	tli := NewTenantLSAIndexes()

	for _, alpha := range []float64{-0.5, 0, 0.5, 1, 1.5} {
		t.Run("alpha", func(t *testing.T) {
			res, err := SearchHybridForTenant(ti, tli, "tenant-A", "graph", HybridSearchOpts{
				OverFetchK: 10,
				Alpha:      alpha,
			})
			if err != nil {
				t.Fatalf("alpha=%v: %v", alpha, err)
			}
			// At alpha=0 with no LSA, score=0 candidates are dropped → empty.
			// At alpha>0, the FTS contribution is non-zero → at least 1 hit.
			if alpha > 0 && len(res.Hits) == 0 {
				t.Errorf("alpha=%v: want ≥1 hit, got 0", alpha)
			}
			for _, h := range res.Hits {
				if h.Score <= 0 {
					t.Errorf("alpha=%v: hit %d has score=%v (must be > 0 after the drop-zero filter)", alpha, h.NodeID, h.Score)
				}
			}
		})
	}
}

// TestSearchHybridForTenant_OverFetchKDefault: passing OverFetchK<=0
// should fall back to a sane internal default rather than returning
// no candidates.
func TestSearchHybridForTenant_OverFetchKDefault(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("graph content"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("tenant-A", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant: %v", err)
	}
	tli := NewTenantLSAIndexes()

	res, err := SearchHybridForTenant(ti, tli, "tenant-A", "graph", HybridSearchOpts{
		OverFetchK: 0, // explicit zero — fallback path
		Alpha:      0.5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Errorf("OverFetchK=0 fallback: want ≥1 hit, got 0")
	}
}

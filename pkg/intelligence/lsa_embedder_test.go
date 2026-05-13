package intelligence

import (
	"context"
	"errors"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
)

// testLSAConfig matches pkg/search's test-corpus pattern: small Dims
// (10) keeps the SVD math fast while still satisfying the T >= Dims
// guard for a ~8-doc corpus. Production uses DefaultLSAConfig()
// (Dims=200); R2.5's wire-up tests will exercise that path.
func testLSAConfig() search.LSAConfig {
	return search.LSAConfig{
		Dims:       10,
		Oversamp:   4,
		PowerIter:  2,
		MaxVocab:   200,
		MinDocFreq: 1,
		TitleBoost: 3,
		Seed:       42,
	}
}

// testCorpus returns a small but vocabulary-rich corpus that satisfies
// the T >= Dims guard at Dims=10 after stemming/stopword filtering.
// Mirrors pkg/search/lsa_test.go's testCorpus so any pattern the LSA
// engine recognizes there is also recognizable here.
func testCorpus() []search.Document {
	return []search.Document{
		{ID: 1, Title: "cats and dogs", Body: "the quick brown fox jumps over lazy dogs in the garden"},
		{ID: 2, Title: "wild animals", Body: "a fast red fox leaps above sleepy dogs by the fence"},
		{ID: 3, Title: "morning sounds", Body: "cats meow and birds sing early in the morning light"},
		{ID: 4, Title: "guard dogs", Body: "dogs bark when strangers approach the garden gate at night"},
		{ID: 5, Title: "quiet hunters", Body: "felines prowl rooftops while rodents scurry under the porch"},
		{ID: 6, Title: "feeding time", Body: "cats prefer fish while dogs eat whatever scraps humans leave"},
		{ID: 7, Title: "migration", Body: "birds fly south for winter and return in spring to sing again"},
		{ID: 8, Title: "seasons", Body: "winter brings snow and spring brings flowers blooming in fields"},
	}
}

// alternateCorpus is the disjoint corpus used in T4 (tenant isolation):
// distinct domain so the same query text projects differently when
// tenants have different LSA models. Vocabulary overlap is intentionally
// minimal to make the cosine difference test reliable.
func alternateCorpus() []search.Document {
	return []search.Document{
		{ID: 1, Title: "compilers", Body: "lexer tokens build syntax trees that semantic analysis walks"},
		{ID: 2, Title: "scheduling", Body: "kernel processes contend for cpu cycles under preemptive policies"},
		{ID: 3, Title: "garbage collection", Body: "mark sweep collectors traverse reachable objects from root sets"},
		{ID: 4, Title: "memory", Body: "virtual memory paging tlb caches translation lookasides for performance"},
		{ID: 5, Title: "filesystems", Body: "inodes data blocks superblocks journal recovery after crashes"},
		{ID: 6, Title: "concurrency", Body: "locks mutexes condition variables coordinate goroutine synchronization"},
		{ID: 7, Title: "networking", Body: "packets routed through switches reach destinations across tcp ip"},
		{ID: 8, Title: "compilation", Body: "optimizer passes transform intermediate code into machine instructions"},
	}
}

func buildIndex(t *testing.T, docs []search.Document) *search.LSAIndex {
	t.Helper()
	idx, err := search.BuildLSAIndex(docs, testLSAConfig())
	if err != nil {
		t.Fatalf("BuildLSAIndex() error = %v", err)
	}
	return idx
}

// TestLSAEmbedder_HappyPath pins that Embed returns a non-nil vector
// for an in-vocabulary query against a registered tenant.
func TestLSAEmbedder_HappyPath(t *testing.T) {
	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	e := NewLSAEmbedder(registry)
	vec, err := e.Embed(context.Background(), "acme", "dogs in the garden")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if vec == nil {
		t.Fatal("Embed() vec = nil, want non-nil")
	}
	if len(vec) != testLSAConfig().Dims {
		t.Errorf("Embed() len(vec) = %d, want %d", len(vec), testLSAConfig().Dims)
	}
}

// TestLSAEmbedder_RejectsMockShape pins spike T2: an LSAEmbedder return
// has length equal to the configured Dims, NEVER length 3 (the archive's
// mockEmbedding signature). A 3-element result is definitional proof
// that the mock formula leaked back in.
//
// Tests use Dims=10; production uses Dims=200 via DefaultLSAConfig.
// Either way, len(vec) == 3 is the failure pattern this test guards
// against.
func TestLSAEmbedder_RejectsMockShape(t *testing.T) {
	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	e := NewLSAEmbedder(registry)
	vec, err := e.Embed(context.Background(), "acme", "dogs bark")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vec) == 3 {
		t.Fatal("Embed() returned 3-element vector — mockEmbedding pattern detected")
	}
}

// TestLSAEmbedder_NoIndexForTenant pins spike T3: an embedder with an
// empty TenantLSAIndexes registry returns ErrNoIndexForTenant for any
// tenant, not a mock or zero vector.
func TestLSAEmbedder_NoIndexForTenant(t *testing.T) {
	e := NewLSAEmbedder(search.NewTenantLSAIndexes())
	vec, err := e.Embed(context.Background(), "acme", "anything")

	if vec != nil {
		t.Errorf("Embed() vec = %v, want nil for missing index", vec)
	}

	var typed ErrNoIndexForTenant
	if !errors.As(err, &typed) {
		t.Fatalf("Embed() err = %v, want ErrNoIndexForTenant", err)
	}
	if typed.TenantID != "acme" {
		t.Errorf("ErrNoIndexForTenant.TenantID = %q, want \"acme\"", typed.TenantID)
	}
}

// TestLSAEmbedder_EmptyTenantID pins that an empty tenantID is treated
// as "no index" (the registry's Get returns nil for the empty-string
// key when no tenant has explicitly registered an index under it). The
// returned error is ErrNoIndexForTenant with the empty TenantID
// preserved — callers' logs can distinguish "missing tenant" vs "known
// tenant without index."
func TestLSAEmbedder_EmptyTenantID(t *testing.T) {
	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	e := NewLSAEmbedder(registry)
	_, err := e.Embed(context.Background(), "", "dogs bark")

	var typed ErrNoIndexForTenant
	if !errors.As(err, &typed) {
		t.Fatalf("Embed(\"\") err = %v, want ErrNoIndexForTenant", err)
	}
	if typed.TenantID != "" {
		t.Errorf("ErrNoIndexForTenant.TenantID = %q, want empty", typed.TenantID)
	}
}

// Note on determinism: pkg/search's LSAIndex is build-time deterministic
// ("Seed=42 is a feature"), but FoldQuery is NOT bit-deterministic
// across calls. FoldQuery sums vocab-keyed contributions via a Go map
// iteration, whose order is randomized — small floating-point
// differences (~1e-10) accumulate. A two-call equality test would
// flake. The structural contract LSAEmbedder owns is captured by the
// other tests in this file (HappyPath, RejectsMockShape, ctx handling,
// typed-error returns, tenant isolation). Build-determinism is
// pkg/search's responsibility; pkg/search/lsa_test.go's
// TestLSADeterminism is the canonical pin there.

// TestLSAEmbedder_TenantIsolation pins spike T4: two tenants with
// disjoint corpora produce different embeddings for the same query text,
// because each tenant's LSA model sees a different vocabulary and
// projection space.
//
// Note: this test is NOT about preventing cross-tenant data leak (the
// Embedder itself never sees another tenant's data — the registry's
// Get(tenantID) is the boundary). It pins the semantic property that
// per-tenant corpora produce per-tenant projections; without this, all
// tenants would silently collapse to the same embedding space.
func TestLSAEmbedder_TenantIsolation(t *testing.T) {
	registry := search.NewTenantLSAIndexes()
	registry.Set("tenant-A", buildIndex(t, testCorpus()))
	registry.Set("tenant-B", buildIndex(t, alternateCorpus()))

	e := NewLSAEmbedder(registry)

	// A query that's in-vocabulary for tenant-A's corpus. tenant-B's
	// corpus likely projects this differently (different vocabulary +
	// different SVD basis).
	queryA := "dogs bark garden"
	vecA, errA := e.Embed(context.Background(), "tenant-A", queryA)
	if errA != nil {
		t.Fatalf("tenant-A Embed() error = %v", errA)
	}

	// tenant-B's corpus has no overlapping vocabulary with the query;
	// FoldQuery returns an error in this case. That itself is a useful
	// signal — different tenants can have different vocab coverage.
	vecB, errB := e.Embed(context.Background(), "tenant-B", queryA)

	// The tenants are isolated if EITHER the embeddings differ OR
	// tenant-B couldn't fold the query at all (out-of-vocab for its
	// model). Both outcomes prove tenant-A's model isn't being silently
	// shared.
	if errB == nil && vecB != nil {
		if len(vecA) != len(vecB) {
			t.Errorf("dims mismatch: tenant-A=%d, tenant-B=%d", len(vecA), len(vecB))
		}
		// Both vectors exist — confirm they differ.
		identical := true
		for i := range vecA {
			if vecA[i] != vecB[i] {
				identical = false
				break
			}
		}
		if identical {
			t.Error("tenant-A and tenant-B produced identical embeddings for the same query — tenant isolation breached")
		}
	}
	// If errB is non-nil (out-of-vocab for tenant-B), isolation is
	// proven by the absence of an embedding for tenant-B. Test passes.
}

// TestLSAEmbedder_CtxCancelled pins that Embed honors ctx.Err() — a
// pre-cancelled context returns the ctx error without touching the LSA
// index. The check is at the top of Embed, before the registry lookup,
// so this exercises the fast-bail path.
func TestLSAEmbedder_CtxCancelled(t *testing.T) {
	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Embed

	e := NewLSAEmbedder(registry)
	vec, err := e.Embed(ctx, "acme", "dogs bark")
	if vec != nil {
		t.Errorf("Embed(cancelled ctx) vec = %v, want nil", vec)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Embed(cancelled ctx) err = %v, want context.Canceled", err)
	}
}

// TestLSAEmbedder_OutOfVocab pins the failure mode where the query
// contains no terms in the tenant's vocabulary. FoldQuery returns an
// error in that case and Embed propagates it wrapped — callers can use
// errors.Is to detect, but the typed error is the LSAIndex's, not
// ErrNoIndexForTenant (which is for missing-index, not out-of-vocab).
func TestLSAEmbedder_OutOfVocab(t *testing.T) {
	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	e := NewLSAEmbedder(registry)
	_, err := e.Embed(context.Background(), "acme", "xyzzy plugh fnord")
	if err == nil {
		t.Fatal("Embed(out-of-vocab) err = nil, want non-nil")
	}
	// The wrapped error should NOT be ErrNoIndexForTenant — that's for
	// missing-index, not out-of-vocab.
	var noIdx ErrNoIndexForTenant
	if errors.As(err, &noIdx) {
		t.Errorf("Embed(out-of-vocab) wrapped ErrNoIndexForTenant; want non-typed error")
	}
}

// TestLSAEmbedder_InterfaceSatisfied is a compile-time check that
// *LSAEmbedder satisfies Embedder. Mirrors TestEmbedderInterfaceCompiles
// in embedder_test.go.
func TestLSAEmbedder_InterfaceSatisfied(t *testing.T) {
	var _ Embedder = (*LSAEmbedder)(nil)
}

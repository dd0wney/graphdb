package search

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestTenantIndexes_Isolation is the red-first acceptance test for
// per-tenant full-text indexing. Before TenantIndexes existed, a single
// global FullTextIndex saw every node regardless of tenant; a query as
// tenant B would surface tenant A's indexed content. After:
//
//   - tenant A's index contains only tenant A's nodes
//   - tenant B's index contains only tenant B's nodes
//   - a query that would hit tenant A content finds zero results when
//     executed against tenant B's index
func TestTenantIndexes_Isolation(t *testing.T) {
	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer gs.Close()

	// Tenant A: the word "alpha" is tenant-A-only content.
	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("alpha secret tenantA content"),
	}); err != nil {
		t.Fatalf("create tenant-A node: %v", err)
	}

	// Tenant B: different content; "alpha" must not appear here.
	if _, err := gs.CreateNodeWithTenant("tenant-B", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("beta tenantB content"),
	}); err != nil {
		t.Fatalf("create tenant-B node: %v", err)
	}

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("tenant-A", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant A: %v", err)
	}
	if err := ti.IndexForTenant("tenant-B", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant B: %v", err)
	}

	t.Run("A finds its own content", func(t *testing.T) {
		results, err := ti.Get("tenant-A").Search("alpha")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("tenant A: want 1 result for 'alpha', got %d", len(results))
		}
	})

	t.Run("B does not see A's content", func(t *testing.T) {
		results, err := ti.Get("tenant-B").Search("alpha")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("tenant B: want 0 results for 'alpha' (tenant isolation breach); got %d", len(results))
		}
	})

	t.Run("B finds its own content", func(t *testing.T) {
		results, err := ti.Get("tenant-B").Search("beta")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("tenant B: want 1 result for 'beta', got %d", len(results))
		}
	})

	t.Run("A does not see B's content", func(t *testing.T) {
		results, err := ti.Get("tenant-A").Search("beta")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("tenant A: want 0 results for 'beta' (tenant isolation breach); got %d", len(results))
		}
	})
}

// TestTenantIndexes_LazyGet asserts Get returns the same instance for
// repeat calls with the same tenant and distinct instances for
// different tenants.
func TestTenantIndexes_LazyGet(t *testing.T) {
	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer gs.Close()

	ti := NewTenantIndexes(gs)

	a1 := ti.Get("A")
	a2 := ti.Get("A")
	b := ti.Get("B")

	if a1 == nil || a2 == nil || b == nil {
		t.Fatal("Get returned nil")
	}
	if a1 != a2 {
		t.Error("Get should return the same instance for same tenant")
	}
	if a1 == b {
		t.Error("Get should return distinct instances for different tenants")
	}
}

// TestIndexPrepared_FirstTimeDoesNotUnderflow asserts that indexing a
// node via IndexPrepared when it was never indexed before does not
// underflow totalDocs (the removeNodeLocked guard path).
func TestIndexPrepared_FirstTimeDoesNotUnderflow(t *testing.T) {
	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer gs.Close()

	node, err := gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("some content for indexing"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	idx := NewFullTextIndex(gs)
	if err := idx.IndexPrepared([]*storage.Node{node}, []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexPrepared: %v", err)
	}

	// Sanity: the doc should be searchable.
	results, err := idx.Search("content")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("want 1 result for 'content', got %d", len(results))
	}

	// totalDocs should be exactly 1, not 0 (which would indicate the
	// remove-before-index path double-subtracted).
	if idx.totalDocs != 1 {
		t.Errorf("totalDocs = %d, want 1", idx.totalDocs)
	}
}

package search

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestBasicTextSearch tests simple text search functionality
func TestBasicTextSearch(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":        storage.StringValue("Alice Johnson"),
		"description": storage.StringValue("Software engineer"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":        storage.StringValue("Bob Smith"),
		"description": storage.StringValue("Data scientist"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":        storage.StringValue("Charlie Brown"),
		"description": storage.StringValue("Software architect"),
	})

	// Create search index
	index := NewFullTextIndex(gs)
	err = index.IndexNodes([]string{"Person"}, []string{"name", "description"})
	if err != nil {
		t.Fatalf("Failed to index nodes: %v", err)
	}

	// Search for "software"
	results, err := index.Search("software")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Should find Alice and Charlie (both have "software" in description)
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

// TestCaseInsensitiveSearch tests that search is case-insensitive
func TestCaseInsensitiveSearch(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Document"}, map[string]storage.Value{
		"title":   storage.StringValue("The Quick Brown Fox"),
		"content": storage.StringValue("Jumps over the lazy dog"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Document"}, []string{"title", "content"})

	// Search with different cases
	testCases := []string{"QUICK", "quick", "QuIcK", "BROWN", "brown"}

	for _, query := range testCases {
		results, err := index.Search(query)
		if err != nil {
			t.Fatalf("Search for '%s' failed: %v", query, err)
		}
		if len(results) != 1 {
			t.Errorf("Search for '%s': expected 1 result, got %d", query, len(results))
		}
	}
}

// TestMultiWordSearch tests searching for multiple words
func TestMultiWordSearch(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Introduction to Machine Learning"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Deep Learning Fundamentals"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Machine Learning and Deep Learning"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Article"}, []string{"title"})

	// Search for "machine learning" (both words)
	results, err := index.Search("machine learning")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Should find nodes containing both "machine" AND "learning"
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'machine learning', got %d", len(results))
	}
}

// TestPhraseSearch tests exact phrase matching
func TestPhraseSearch(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Document"}, map[string]storage.Value{
		"content": storage.StringValue("New York City is amazing"),
	})
	gs.CreateNode([]string{"Document"}, map[string]storage.Value{
		"content": storage.StringValue("York is a city in England"),
	})
	gs.CreateNode([]string{"Document"}, map[string]storage.Value{
		"content": storage.StringValue("I live in New York"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Document"}, []string{"content"})

	// Search for exact phrase "New York"
	results, err := index.SearchPhrase("New York")
	if err != nil {
		t.Fatalf("Phrase search failed: %v", err)
	}

	// Should only find documents with "New York" as a phrase (not just both words)
	if len(results) != 2 {
		t.Errorf("Expected 2 results for phrase 'New York', got %d", len(results))
	}
}

// TestBooleanSearch tests boolean operators (AND, OR, NOT)
func TestBooleanSearch(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Python programming tutorial"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Java programming guide"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Python data analysis"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("JavaScript tutorial"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Article"}, []string{"title"})

	// Test AND: "python AND programming"
	andResults, err := index.SearchBoolean("python AND programming")
	if err != nil {
		t.Fatalf("AND search failed: %v", err)
	}
	if len(andResults) != 1 {
		t.Errorf("Expected 1 result for 'python AND programming', got %d", len(andResults))
	}

	// Test OR: "python OR java"
	orResults, err := index.SearchBoolean("python OR java")
	if err != nil {
		t.Fatalf("OR search failed: %v", err)
	}
	if len(orResults) != 3 {
		t.Errorf("Expected 3 results for 'python OR java', got %d", len(orResults))
	}

	// Test NOT: "programming NOT java"
	notResults, err := index.SearchBoolean("programming NOT java")
	if err != nil {
		t.Fatalf("NOT search failed: %v", err)
	}
	if len(notResults) != 1 {
		t.Errorf("Expected 1 result for 'programming NOT java', got %d", len(notResults))
	}
}

// TestRelevanceScoring tests that results are ranked by relevance
func TestRelevanceScoring(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Node 1: "database" appears once
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Introduction to database systems"),
	})

	// Node 2: "database" appears three times
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("Database design patterns"),
		"content": storage.StringValue("Database architecture and database optimization"),
	})

	// Node 3: "database" appears twice
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Database fundamentals and database queries"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Article"}, []string{"title", "content"})

	results, err := index.Search("database")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Results should be ordered by score (highest first)
	// Node 2 should be first (3 occurrences)
	t.Logf("Score 0: %f, Score 1: %f, Score 2: %f", results[0].Score, results[1].Score, results[2].Score)
	if results[0].Score <= results[1].Score || results[1].Score <= results[2].Score {
		t.Errorf("Results are not properly ranked by relevance score: [0]=%f, [1]=%f, [2]=%f",
			results[0].Score, results[1].Score, results[2].Score)
	}
}

// TestFuzzySearch tests fuzzy matching for typo tolerance
func TestFuzzySearch(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name": storage.StringValue("Laptop Computer"),
	})
	gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name": storage.StringValue("Desktop Computer"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Product"}, []string{"name"})

	// Search with typo: "compter" instead of "computer"
	results, err := index.SearchFuzzy("compter", 2) // max edit distance of 2
	if err != nil {
		t.Fatalf("Fuzzy search failed: %v", err)
	}

	// Should still find both computers despite the typo
	if len(results) != 2 {
		t.Errorf("Expected 2 results for fuzzy search 'compter', got %d", len(results))
	}
}

// TestSearchOnSpecificProperty tests searching only on specific properties
func TestSearchOnSpecificProperty(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("John Developer"),
		"title": storage.StringValue("Manager"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Jane Manager"),
		"title": storage.StringValue("Developer"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Person"}, []string{"name", "title"})

	// Search only in "title" property
	results, err := index.SearchInProperty("title", "developer")
	if err != nil {
		t.Fatalf("Property search failed: %v", err)
	}

	// Should only find Jane (developer in title), not John (developer in name)
	if len(results) != 1 {
		t.Errorf("Expected 1 result for title search, got %d", len(results))
	}
}

// TestIndexUpdate tests that index can be updated after node changes
func TestIndexUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Original Title"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Article"}, []string{"title"})

	// Search for "original" should find it
	results1, _ := index.Search("original")
	if len(results1) != 1 {
		t.Errorf("Expected to find 'original', got %d results", len(results1))
	}

	// Update the node
	gs.UpdateNode(node.ID, map[string]storage.Value{
		"title": storage.StringValue("Updated Title"),
	})

	// Reindex
	index.UpdateNode(node.ID)

	// Search for "original" should find nothing
	results2, _ := index.Search("original")
	if len(results2) != 0 {
		t.Errorf("Expected 0 results for 'original' after update, got %d", len(results2))
	}

	// Search for "updated" should find it
	results3, _ := index.Search("updated")
	if len(results3) != 1 {
		t.Errorf("Expected to find 'updated', got %d results", len(results3))
	}
}

// TestEmptySearchQuery tests handling of empty search queries
func TestEmptySearchQuery(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	index := NewFullTextIndex(gs)

	results, err := index.Search("")
	if err != nil {
		t.Fatalf("Empty search should not error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Empty search should return 0 results, got %d", len(results))
	}
}

// TestSearchWithNoMatches tests searches that find nothing
func TestSearchWithNoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("GraphDB Tutorial"),
	})

	index := NewFullTextIndex(gs)
	index.IndexNodes([]string{"Article"}, []string{"title"})

	results, err := index.Search("nonexistent")
	if err != nil {
		t.Fatalf("Search should not error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results for nonexistent term, got %d", len(results))
	}
}

// TestSearchBooleanNoOperator exercises the fallthrough branch of SearchBoolean
// where the query contains no AND/OR/NOT — historically this branch called
// RUnlock manually and then returned, letting the deferred RUnlock fire on an
// already-unlocked mutex and panic with "sync: RUnlock of unlocked RWMutex".
func TestSearchBooleanNoOperator(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}
	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("GraphDB Tutorial"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Advanced GraphDB Topics"),
	})

	index := NewFullTextIndex(gs)
	if err := index.IndexNodes([]string{"Article"}, []string{"title"}); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}

	// No AND/OR/NOT — triggers the no-operator fallthrough branch.
	results, err := index.SearchBoolean("graphdb")
	if err != nil {
		t.Fatalf("SearchBoolean returned error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'graphdb', got %d", len(results))
	}
}

// TestNodeContent covers both branches of the exported accessor: an
// indexed node returns its stored content; an unindexed node returns
// ("", false).
func TestNodeContent(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}
	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	node, err := gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Metempsychosis"),
		"body":  storage.StringValue("The word he'd heard from the bookseller"),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	index := NewFullTextIndex(gs)
	if err := index.IndexNodes([]string{"Article"}, []string{"title", "body"}); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}

	content, ok := index.NodeContent(node.ID)
	if !ok {
		t.Fatalf("expected indexed node %d to return content, got ok=false", node.ID)
	}
	// Content is concatenated indexed fields — both title and body text
	// should be present.
	if !strings.Contains(strings.ToLower(content), "metempsychosis") {
		t.Errorf("content missing title token: %q", content)
	}
	if !strings.Contains(strings.ToLower(content), "bookseller") {
		t.Errorf("content missing body token: %q", content)
	}

	missing, ok := index.NodeContent(999999)
	if ok {
		t.Errorf("expected unindexed node to return ok=false, got content=%q", missing)
	}
	if missing != "" {
		t.Errorf("expected empty content for unindexed node, got %q", missing)
	}
}

// TestSearchTopK asserts that only the requested K results are returned,
// and that k <= 0 preserves the all-results behavior of Search.
func TestSearchTopK(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}
	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	for i := 0; i < 5; i++ {
		if _, err := gs.CreateNode([]string{"Article"}, map[string]storage.Value{
			"body": storage.StringValue(fmt.Sprintf("common uniquetoken%d filler", i)),
		}); err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
	}

	index := NewFullTextIndex(gs)
	if err := index.IndexNodes([]string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}

	tests := []struct {
		name    string
		k       int
		wantLen int
	}{
		{"k=3 returns top 3", 3, 3},
		{"k=0 returns all", 0, 5},
		{"k=-1 returns all", -1, 5},
		{"k larger than corpus returns all", 100, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results, err := index.SearchTopK("common", tc.k)
			if err != nil {
				t.Fatalf("SearchTopK: %v", err)
			}
			if len(results) != tc.wantLen {
				t.Errorf("k=%d: got %d results, want %d", tc.k, len(results), tc.wantLen)
			}
		})
	}
}

// BenchmarkSearchVsSearchTopK measures the hydration delta between Search
// (hydrates every candidate) and SearchTopK (hydrates only top k). The
// per-candidate GetNode cost is small with BulkImportMode storage, so the
// absolute delta is modest — but it scales linearly with corpus size and
// LSM read latency, which is what the perf fix targets in production.
func BenchmarkSearchVsSearchTopK(b *testing.B) {
	tmpDir := b.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}
	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer gs.Close()

	const corpusSize = 1000
	for i := 0; i < corpusSize; i++ {
		if _, err := gs.CreateNode([]string{"Article"}, map[string]storage.Value{
			"body": storage.StringValue(fmt.Sprintf("common uniquetoken%d lorem ipsum dolor sit amet", i)),
		}); err != nil {
			b.Fatalf("CreateNode %d: %v", i, err)
		}
	}
	index := NewFullTextIndex(gs)
	if err := index.IndexNodes([]string{"Article"}, []string{"body"}); err != nil {
		b.Fatalf("IndexNodes: %v", err)
	}

	b.Run("Search_hydrate_1000", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r, err := index.Search("common")
			if err != nil || len(r) != corpusSize {
				b.Fatalf("want %d results, got err=%v len=%d", corpusSize, err, len(r))
			}
		}
	})

	b.Run("SearchTopK_10", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r, err := index.SearchTopK("common", 10)
			if err != nil || len(r) != 10 {
				b.Fatalf("want 10 results, got err=%v len=%d", err, len(r))
			}
		}
	})
}

// BenchmarkUpdateNodeLargeVocab measures UpdateNode cost when the corpus
// has a large vocabulary but each individual node contains only a few
// terms. Before the nodeTerms reverse posting, this loop was
// O(vocabulary) because UpdateNode scanned every term in the index to
// find ones that referenced the node. After, it is O(terms-in-document).
func BenchmarkUpdateNodeLargeVocab(b *testing.B) {
	tmpDir := b.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}
	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer gs.Close()

	// 2000 docs × ~6 unique tokens each gives vocab ≈ 12000, doc-terms ≈ 6.
	// Theoretical ratio vocab/doc-terms = 2000×, so post-fix UpdateNode
	// should be dramatically faster than the pre-fix linear-scan version.
	const corpusSize = 2000
	var target uint64
	for i := 0; i < corpusSize; i++ {
		node, err := gs.CreateNode([]string{"Article"}, map[string]storage.Value{
			"body": storage.StringValue(fmt.Sprintf("uniqueA%d uniqueB%d uniqueC%d uniqueD%d uniqueE%d common", i, i, i, i, i)),
		})
		if err != nil {
			b.Fatalf("CreateNode %d: %v", i, err)
		}
		if i == corpusSize/2 {
			target = node.ID
		}
	}
	index := NewFullTextIndex(gs)
	if err := index.IndexNodes([]string{"Article"}, []string{"body"}); err != nil {
		b.Fatalf("IndexNodes: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := index.UpdateNode(target); err != nil {
			b.Fatalf("UpdateNode: %v", err)
		}
	}
}

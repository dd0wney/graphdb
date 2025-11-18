package search

import (
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

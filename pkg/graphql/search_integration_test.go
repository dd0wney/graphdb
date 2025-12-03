package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestGraphQLSearchIntegration tests full-text search through GraphQL
func TestGraphQLSearchIntegration(t *testing.T) {
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

	// Create test data
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("Introduction to GraphQL"),
		"content": storage.StringValue("GraphQL is a query language for APIs"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("Advanced GraphQL Patterns"),
		"content": storage.StringValue("Learn advanced techniques for GraphQL"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("REST vs GraphQL"),
		"content": storage.StringValue("Comparing REST and GraphQL APIs"),
	})

	// Create search index
	searchIndex := search.NewFullTextIndex(gs)
	err = searchIndex.IndexNodes([]string{"Article"}, []string{"title", "content"})
	if err != nil {
		t.Fatalf("Failed to index nodes: %v", err)
	}

	// Create schema with search
	schema, err := GenerateSchemaWithSearch(gs, searchIndex)
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Test search query
	query := `
		{
			search(query: "graphql") {
				score
				node {
					id
					labels
					properties
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	searchResults := data["search"].([]any)

	// Should find all 3 articles containing "graphql"
	if len(searchResults) != 3 {
		t.Errorf("Expected 3 search results, got %d", len(searchResults))
	}

	// Verify results have scores
	for i, r := range searchResults {
		result := r.(map[string]any)
		if score, ok := result["score"].(float64); !ok || score <= 0 {
			t.Errorf("Result %d has invalid score: %v", i, result["score"])
		}
	}
}

// TestGraphQLPhraseSearch tests phrase search through GraphQL
func TestGraphQLPhraseSearch(t *testing.T) {
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
		"content": storage.StringValue("New York City is a great place"),
	})
	gs.CreateNode([]string{"Document"}, map[string]storage.Value{
		"content": storage.StringValue("York is a city in England"),
	})

	searchIndex := search.NewFullTextIndex(gs)
	searchIndex.IndexNodes([]string{"Document"}, []string{"content"})

	schema, err := GenerateSchemaWithSearch(gs, searchIndex)
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	query := `
		{
			searchPhrase(phrase: "New York") {
				score
				node {
					id
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	searchResults := data["searchPhrase"].([]any)

	// Should only find the first document with "New York" as a phrase
	if len(searchResults) != 1 {
		t.Errorf("Expected 1 phrase search result, got %d", len(searchResults))
	}
}

// TestGraphQLBooleanSearch tests boolean search through GraphQL
func TestGraphQLBooleanSearch(t *testing.T) {
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

	searchIndex := search.NewFullTextIndex(gs)
	searchIndex.IndexNodes([]string{"Article"}, []string{"title"})

	schema, err := GenerateSchemaWithSearch(gs, searchIndex)
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Test AND query
	query := `
		{
			searchBoolean(query: "python AND programming") {
				score
				node {
					id
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	searchResults := data["searchBoolean"].([]any)

	// Should find only the Python programming tutorial
	if len(searchResults) != 1 {
		t.Errorf("Expected 1 boolean search result for 'python AND programming', got %d", len(searchResults))
	}
}

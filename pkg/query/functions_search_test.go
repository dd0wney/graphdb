package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestScoreText(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		query     string
		minScore  float64
		maxScore  float64
	}{
		{"full match", "quantum entanglement", "quantum entanglement", 1.0, 1.0},
		{"partial match", "quantum physics and entanglement", "quantum entanglement", 1.0, 1.0},
		{"no match", "classical mechanics", "quantum entanglement", 0.0, 0.0},
		{"single term match", "quantum physics", "quantum entanglement", 0.4, 0.6},
		{"empty query", "anything", "", 0.0, 0.0},
		{"case insensitive", "QUANTUM Entanglement", "quantum entanglement", 1.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreText(tt.text, tt.query)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("scoreText(%q, %q) = %v, want [%v, %v]",
					tt.text, tt.query, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestSearch_InWhere(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("Quantum Entanglement Explained"),
		"content": storage.StringValue("This article explains quantum entanglement in detail"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("Classical Physics"),
		"content": storage.StringValue("Newton's laws of motion and gravity"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title":   storage.StringValue("Quantum Computing"),
		"content": storage.StringValue("Using quantum mechanics for computation"),
	})

	executor := NewExecutor(gs)
	idx := search.NewFullTextIndex(gs)
	executor.SetSearchIndex(idx)

	// WHERE search(n.content, "quantum entanglement") > 0.5
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Article"}},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &FunctionCallExpression{
					Name: "search",
					Args: []Expression{
						&PropertyExpression{Variable: "n", Property: "content"},
						&LiteralExpression{Value: "quantum entanglement"},
					},
				},
				Operator: ">",
				Right:    &LiteralExpression{Value: float64(0.5)},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "title"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Only the first article should match (contains both "quantum" AND "entanglement")
	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.title"] != "Quantum Entanglement Explained" {
		t.Errorf("Expected 'Quantum Entanglement Explained', got %v", result.Rows[0]["n.title"])
	}
}

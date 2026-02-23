package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestParser_ExplainPrefix(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		explain bool
		profile bool
	}{
		{
			name:    "EXPLAIN prefix",
			input:   "EXPLAIN MATCH (n:Person) RETURN n.name",
			explain: true,
		},
		{
			name:    "PROFILE prefix",
			input:   "PROFILE MATCH (n:Person) RETURN n.name",
			profile: true,
		},
		{
			name:  "no prefix",
			input: "MATCH (n:Person) RETURN n.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("Tokenize failed: %v", err)
			}
			parser := NewParser(tokens)
			query, err := parser.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if query.Explain != tt.explain {
				t.Errorf("Explain: got %v, want %v", query.Explain, tt.explain)
			}
			if query.Profile != tt.profile {
				t.Errorf("Profile: got %v, want %v", query.Profile, tt.profile)
			}
		})
	}
}

func TestExplain_ReturnsStepDescriptions(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	query := &Query{
		Explain: true,
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left:     &PropertyExpression{Variable: "n", Property: "age"},
				Operator: ">",
				Right:    &LiteralExpression{Value: int64(25)},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// EXPLAIN should return plan description, not actual data
	if len(result.Columns) < 2 {
		t.Fatalf("Expected at least 2 columns (step, detail), got %d", len(result.Columns))
	}
	if result.Columns[0] != "step" {
		t.Errorf("Expected first column 'step', got %q", result.Columns[0])
	}
	if result.Columns[1] != "detail" {
		t.Errorf("Expected second column 'detail', got %q", result.Columns[1])
	}

	// Should have rows for Match, Filter, Return steps
	if len(result.Rows) < 3 {
		t.Fatalf("Expected at least 3 plan steps, got %d", len(result.Rows))
	}

	// Verify step names
	expectedSteps := []string{"MatchStep", "FilterStep", "ReturnStep"}
	for i, expected := range expectedSteps {
		if i >= len(result.Rows) {
			break
		}
		step := result.Rows[i]["step"]
		if step != expected {
			t.Errorf("Row %d step: got %q, want %q", i, step, expected)
		}
	}
}

func TestProfile_ReturnsTiming(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	for i := 0; i < 10; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Alice"),
		})
	}

	executor := NewExecutor(gs)

	query := &Query{
		Profile: true,
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// PROFILE result should have profile data attached
	if result.Profile == nil {
		t.Fatal("Expected non-nil Profile on result")
	}

	if len(result.Profile) < 2 {
		t.Fatalf("Expected at least 2 profile entries, got %d", len(result.Profile))
	}

	// Each profile entry should have a step name and non-negative duration
	for i, p := range result.Profile {
		if p.StepName == "" {
			t.Errorf("Profile[%d]: empty step name", i)
		}
		if p.Duration < 0 {
			t.Errorf("Profile[%d]: negative duration %v", i, p.Duration)
		}
		if p.RowsOut < 0 {
			t.Errorf("Profile[%d]: negative rows_out %d", i, p.RowsOut)
		}
	}

	// The result should also have the actual query data (not just plan)
	if result.Count == 0 {
		t.Error("PROFILE should also return actual query results")
	}
}

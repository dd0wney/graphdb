package query

import (
	"testing"
)

// parseCallInput is a small helper that lexes + parses a Cypher input string
// and returns the resulting Query (or fails the test on lex/parse error).
func parseCallInput(t *testing.T, input string) *Query {
	t.Helper()
	tokens, lerr := NewLexer(input).Tokenize()
	if lerr != nil {
		t.Fatalf("lex failed: %v", lerr)
	}
	q, perr := NewParser(tokens).Parse()
	if perr != nil {
		t.Fatalf("parse failed: %v", perr)
	}
	return q
}

func TestParseCall_HappyPath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantName  string
		wantArgs  int
		wantYield []string
	}{
		{
			name:      "bare procedure with parens",
			input:     "CALL foo()",
			wantName:  "foo",
			wantArgs:  0,
			wantYield: nil,
		},
		{
			name:      "bare procedure without parens",
			input:     "CALL foo",
			wantName:  "foo",
			wantArgs:  0,
			wantYield: nil,
		},
		{
			name:      "dotted procedure name",
			input:     "CALL algo.shortestPath()",
			wantName:  "algo.shortestPath",
			wantArgs:  0,
			wantYield: nil,
		},
		{
			name:      "multi-dotted procedure name",
			input:     "CALL a.b.c()",
			wantName:  "a.b.c",
			wantArgs:  0,
			wantYield: nil,
		},
		{
			name:      "single string argument",
			input:     `CALL foo("bar")`,
			wantName:  "foo",
			wantArgs:  1,
			wantYield: nil,
		},
		{
			name:      "multiple arguments",
			input:     `CALL foo("a", "b", "c")`,
			wantName:  "foo",
			wantArgs:  3,
			wantYield: nil,
		},
		{
			name:      "single YIELD item",
			input:     "CALL foo() YIELD x",
			wantName:  "foo",
			wantArgs:  0,
			wantYield: []string{"x"},
		},
		{
			name:      "multiple YIELD items",
			input:     "CALL foo() YIELD x, y, z",
			wantName:  "foo",
			wantArgs:  0,
			wantYield: []string{"x", "y", "z"},
		},
		{
			name:      "full combo: dotted name, args, yield",
			input:     `CALL algo.shortestPath("a", "b") YIELD path, cost`,
			wantName:  "algo.shortestPath",
			wantArgs:  2,
			wantYield: []string{"path", "cost"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := parseCallInput(t, tt.input)
			if q.Call == nil {
				t.Fatal("expected Query.Call to be set, got nil")
			}
			if q.Call.ProcedureName != tt.wantName {
				t.Errorf("ProcedureName = %q, want %q", q.Call.ProcedureName, tt.wantName)
			}
			if len(q.Call.Arguments) != tt.wantArgs {
				t.Errorf("len(Arguments) = %d, want %d", len(q.Call.Arguments), tt.wantArgs)
			}
			if len(q.Call.YieldItems) != len(tt.wantYield) {
				t.Fatalf("len(YieldItems) = %d, want %d (items: %v)",
					len(q.Call.YieldItems), len(tt.wantYield), q.Call.YieldItems)
			}
			for i, want := range tt.wantYield {
				if q.Call.YieldItems[i] != want {
					t.Errorf("YieldItems[%d] = %q, want %q", i, q.Call.YieldItems[i], want)
				}
			}
		})
	}
}

func TestParseCall_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing closing paren",
			input: `CALL foo("a"`,
		},
		{
			name:  "dangling dot before paren",
			input: "CALL foo.()",
		},
		{
			name:  "missing identifier after YIELD",
			input: "CALL foo() YIELD",
		},
		{
			name:  "missing identifier after YIELD comma",
			input: "CALL foo() YIELD x,",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, lerr := NewLexer(tt.input).Tokenize()
			if lerr != nil {
				// Lex errors are an acceptable failure mode for these inputs;
				// the contract is "the parser path does not accept malformed CALL."
				return
			}
			_, perr := NewParser(tokens).Parse()
			if perr == nil {
				t.Errorf("expected parse error for %q, got nil", tt.input)
			}
		})
	}
}

package query

import (
	"testing"
)

// FuzzQueryParser tests the query parser with random inputs
// This helps find crashes, panics, and edge cases
//
// Run with: go test -fuzz=FuzzQueryParser -fuzztime=30s
func FuzzQueryParser(f *testing.F) {
	// Seed corpus with valid queries
	f.Add("MATCH (n) RETURN n")
	f.Add("MATCH (n:Person) RETURN n.name")
	f.Add("MATCH (a)-[r:KNOWS]->(b) RETURN a, r, b")
	f.Add("MATCH (n) WHERE n.age > 25 RETURN n")
	f.Add("CREATE (n:Person {name: 'Alice', age: 30})")
	f.Add("DELETE n")
	f.Add("SET n.name = 'Bob'")
	f.Add("")
	f.Add("   ")
	f.Add("MATCH")
	f.Add("()")
	f.Add("[]")
	f.Add("{}")

	f.Fuzz(func(t *testing.T, query string) {
		// Parser should NEVER panic, even on invalid input
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Parser panicked on input %q: %v", query, r)
			}
		}()

		// Try to tokenize
		lexer := NewLexer(query)
		tokens, err := lexer.Tokenize()
		if err != nil {
			// Errors are fine, panics are not
			return
		}

		// Try to parse tokens
		parser := NewParser(tokens)
		_, err = parser.Parse()
		// Errors are fine, panics are not
		_ = err
	})
}

// FuzzQueryLexer tests the lexer with random inputs
func FuzzQueryLexer(f *testing.F) {
	// Seed corpus with various inputs
	f.Add("MATCH")
	f.Add("(n:Person)")
	f.Add("{name: 'test'}")
	f.Add("[r:KNOWS]")
	f.Add("n.name")
	f.Add("'string literal'")
	f.Add("\"string literal\"")
	f.Add("123")
	f.Add("123.456")
	f.Add("true")
	f.Add("false")
	f.Add("null")
	f.Add(">=")
	f.Add("<=")
	f.Add("!=")
	f.Add("//comment")

	f.Fuzz(func(t *testing.T, input string) {
		// Lexer should never panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Lexer panicked on input %q: %v", input, r)
			}
		}()

		lexer := NewLexer(input)
		tokens, err := lexer.Tokenize()
		_ = tokens
		_ = err
	})
}

// FuzzExpressionEval tests expression evaluation with random inputs
func FuzzExpressionEval(f *testing.F) {
	// Seed with various property access patterns
	f.Add("age", int64(25))
	f.Add("name", int64(0))
	f.Add("count", int64(100))
	f.Add("", int64(0))

	f.Fuzz(func(t *testing.T, propName string, value int64) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Expression eval panicked on %q=%d: %v", propName, value, r)
			}
		}()

		// Create a simple comparison expression
		expr := &BinaryExpression{
			Left: &PropertyExpression{
				Variable: "n",
				Property: propName,
			},
			Operator: ">",
			Right:    &LiteralExpression{Value: value},
		}

		// Test with empty bindings
		bindings := map[string]any{}
		_, _ = expr.Eval(bindings)

		// Test with node binding
		bindings["n"] = map[string]any{
			propName: value,
		}
		_, _ = expr.Eval(bindings)
	})
}

// FuzzAggregation tests aggregation functions with random inputs
func FuzzAggregation(f *testing.F) {
	// Seed with numeric values
	f.Add(int64(0))
	f.Add(int64(1))
	f.Add(int64(-1))
	f.Add(int64(9223372036854775807))  // max int64
	f.Add(int64(-9223372036854775808)) // min int64

	f.Fuzz(func(t *testing.T, value int64) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Aggregation panicked on %d: %v", value, r)
			}
		}()

		computer := &AggregationComputer{}

		// Test various aggregations with private methods
		values := []any{value}
		_ = computer.sum(values)
		_ = computer.avg(values)
		_ = computer.min(values)
		_ = computer.max(values)
	})
}

// FuzzBinaryExpressionOperators tests all binary expression operators
func FuzzBinaryExpressionOperators(f *testing.F) {
	// Seed with value pairs
	f.Add(int64(10), int64(20))
	f.Add(int64(-5), int64(5))
	f.Add(int64(0), int64(0))
	f.Add(int64(100), int64(-100))

	f.Fuzz(func(t *testing.T, left, right int64) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("BinaryExpression panicked on %d vs %d: %v", left, right, r)
			}
		}()

		operators := []string{"=", "!=", ">", "<", ">=", "<=", "AND", "OR"}

		for _, op := range operators {
			expr := &BinaryExpression{
				Left:     &LiteralExpression{Value: left},
				Operator: op,
				Right:    &LiteralExpression{Value: right},
			}

			_, _ = expr.Eval(map[string]any{})
		}
	})
}

// FuzzTokenCombinations tests random token sequences
func FuzzTokenCombinations(f *testing.F) {
	// Seed with valid token sequences
	f.Add("MATCH ( n ) RETURN n")
	f.Add("MATCH ( n : Person ) RETURN n . name")
	f.Add("WHERE n . age > 25 AND n . name = 'test'")

	f.Fuzz(func(t *testing.T, input string) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Token parsing panicked on %q: %v", input, r)
			}
		}()

		// Tokenize
		lexer := NewLexer(input)
		tokens, _ := lexer.Tokenize()

		// Try to parse (should handle any token sequence gracefully)
		if len(tokens) > 0 {
			parser := NewParser(tokens)
			_, _ = parser.Parse()
		}
	})
}

package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestBinaryExpression_AND tests AND logic evaluation
func TestBinaryExpression_AND(t *testing.T) {
	// true AND true = true
	expr := &BinaryExpression{
		Left:     &LiteralExpression{Value: true},
		Operator: "AND",
		Right:    &LiteralExpression{Value: true},
	}
	result, err := expr.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected true AND true = true")
	}

	// true AND false = false
	expr2 := &BinaryExpression{
		Left:     &LiteralExpression{Value: true},
		Operator: "AND",
		Right:    &LiteralExpression{Value: false},
	}
	result2, err := expr2.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if result2 {
		t.Error("Expected true AND false = false")
	}

	// false AND true = false (should short-circuit)
	expr3 := &BinaryExpression{
		Left:     &LiteralExpression{Value: false},
		Operator: "AND",
		Right:    &LiteralExpression{Value: true},
	}
	result3, err := expr3.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if result3 {
		t.Error("Expected false AND true = false")
	}
}

// TestBinaryExpression_OR tests OR logic evaluation
func TestBinaryExpression_OR(t *testing.T) {
	// true OR false = true (should short-circuit)
	expr := &BinaryExpression{
		Left:     &LiteralExpression{Value: true},
		Operator: "OR",
		Right:    &LiteralExpression{Value: false},
	}
	result, err := expr.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected true OR false = true")
	}

	// false OR true = true
	expr2 := &BinaryExpression{
		Left:     &LiteralExpression{Value: false},
		Operator: "OR",
		Right:    &LiteralExpression{Value: true},
	}
	result2, err := expr2.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result2 {
		t.Error("Expected false OR true = true")
	}

	// false OR false = false
	expr3 := &BinaryExpression{
		Left:     &LiteralExpression{Value: false},
		Operator: "OR",
		Right:    &LiteralExpression{Value: false},
	}
	result3, err := expr3.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if result3 {
		t.Error("Expected false OR false = false")
	}
}

// TestBinaryExpression_Equals tests equality comparison
func TestBinaryExpression_Equals(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"name": "Alice",
			"age":  int64(30),
		},
	}

	// String equality
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "name"},
		Operator: "=",
		Right:    &LiteralExpression{Value: "Alice"},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected person.name = 'Alice' to be true")
	}

	// Number equality
	expr2 := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "age"},
		Operator: "=",
		Right:    &LiteralExpression{Value: int64(30)},
	}
	result2, err := expr2.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result2 {
		t.Error("Expected person.age = 30 to be true")
	}

	// Not equal
	expr3 := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "name"},
		Operator: "=",
		Right:    &LiteralExpression{Value: "Bob"},
	}
	result3, err := expr3.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if result3 {
		t.Error("Expected person.name = 'Bob' to be false")
	}
}

// TestBinaryExpression_NotEquals tests inequality comparison
func TestBinaryExpression_NotEquals(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"name": "Alice",
		},
	}

	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "name"},
		Operator: "!=",
		Right:    &LiteralExpression{Value: "Bob"},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected person.name != 'Bob' to be true")
	}
}

// TestBinaryExpression_Comparisons tests <, >, <=, >=
func TestBinaryExpression_Comparisons(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"age": int64(30),
		},
	}

	tests := []struct {
		operator string
		value    int64
		expected bool
		desc     string
	}{
		{">", 25, true, "30 > 25"},
		{">", 30, false, "30 > 30"},
		{">", 35, false, "30 > 35"},
		{"<", 25, false, "30 < 25"},
		{"<", 30, false, "30 < 30"},
		{"<", 35, true, "30 < 35"},
		{">=", 25, true, "30 >= 25"},
		{">=", 30, true, "30 >= 30"},
		{">=", 35, false, "30 >= 35"},
		{"<=", 25, false, "30 <= 25"},
		{"<=", 30, true, "30 <= 30"},
		{"<=", 35, true, "30 <= 35"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			expr := &BinaryExpression{
				Left:     &PropertyExpression{Variable: "person", Property: "age"},
				Operator: tt.operator,
				Right:    &LiteralExpression{Value: tt.value},
			}
			result, err := expr.Eval(context)
			if err != nil {
				t.Fatalf("Eval failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Expected %s to be %v, got %v", tt.desc, tt.expected, result)
			}
		})
	}
}

// TestBinaryExpression_FloatComparisons tests float comparisons
func TestBinaryExpression_FloatComparisons(t *testing.T) {
	context := map[string]any{
		"product": map[string]any{
			"price": 29.99,
		},
	}

	// Greater than
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "product", Property: "price"},
		Operator: ">",
		Right:    &LiteralExpression{Value: 20.0},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected 29.99 > 20.0 to be true")
	}

	// Less than
	expr2 := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "product", Property: "price"},
		Operator: "<",
		Right:    &LiteralExpression{Value: 50.0},
	}
	result2, err := expr2.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result2 {
		t.Error("Expected 29.99 < 50.0 to be true")
	}
}

// TestBinaryExpression_StringComparisons tests string comparisons
func TestBinaryExpression_StringComparisons(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"name": "Bob",
		},
	}

	// "Bob" > "Alice" (lexicographic)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "name"},
		Operator: ">",
		Right:    &LiteralExpression{Value: "Alice"},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected 'Bob' > 'Alice' to be true (lexicographic)")
	}

	// "Bob" < "Charlie"
	expr2 := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "name"},
		Operator: "<",
		Right:    &LiteralExpression{Value: "Charlie"},
	}
	result2, err := expr2.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result2 {
		t.Error("Expected 'Bob' < 'Charlie' to be true")
	}
}

// TestBinaryExpression_UnknownOperator tests error handling
func TestBinaryExpression_UnknownOperator(t *testing.T) {
	expr := &BinaryExpression{
		Left:     &LiteralExpression{Value: true},
		Operator: "INVALID",
		Right:    &LiteralExpression{Value: true},
	}
	_, err := expr.Eval(nil)
	if err == nil {
		t.Error("Expected error for unknown operator")
	}
}

// TestPropertyExpression_Eval tests property access
func TestPropertyExpression_Eval(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"name": "Alice",
		},
	}

	// PropertyExpression.Eval should return error (not usable standalone)
	expr := &PropertyExpression{Variable: "person", Property: "name"}
	_, err := expr.Eval(context)
	if err == nil {
		t.Error("Expected error: property expression must be used in comparison")
	}
}

// TestLiteralExpression_Boolean tests boolean literal evaluation
func TestLiteralExpression_Boolean(t *testing.T) {
	// True literal
	expr := &LiteralExpression{Value: true}
	result, err := expr.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected true literal to evaluate to true")
	}

	// False literal
	expr2 := &LiteralExpression{Value: false}
	result2, err := expr2.Eval(nil)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if result2 {
		t.Error("Expected false literal to evaluate to false")
	}
}

// TestLiteralExpression_NonBoolean tests non-boolean literal error
func TestLiteralExpression_NonBoolean(t *testing.T) {
	expr := &LiteralExpression{Value: "string"}
	_, err := expr.Eval(nil)
	if err == nil {
		t.Error("Expected error for non-boolean literal")
	}
}

// TestCompareValues_IntComparison tests integer comparison
func TestCompareValues_IntComparison(t *testing.T) {
	// This tests the internal compareValues function via BinaryExpression
	context := map[string]any{
		"item": map[string]any{
			"count": 5,
		},
	}

	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "item", Property: "count"},
		Operator: ">",
		Right:    &LiteralExpression{Value: 3},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected 5 > 3 to be true")
	}
}

// TestExtractValue_MissingProperty tests extracting non-existent property
func TestExtractValue_MissingProperty(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"name": "Alice",
		},
	}

	// Try to compare non-existent property
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "person", Property: "age"},
		Operator: "=",
		Right:    &LiteralExpression{Value: int64(30)},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	// nil != 30, so should be false
	if result {
		t.Error("Expected comparison with missing property to be false")
	}
}

// TestExtractValue_MissingVariable tests extracting from non-existent variable
func TestExtractValue_MissingVariable(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"name": "Alice",
		},
	}

	// Try to access non-existent variable
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "company", Property: "name"},
		Operator: "=",
		Right:    &LiteralExpression{Value: "Acme"},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	// nil != "Acme", so should be false
	if result {
		t.Error("Expected comparison with missing variable to be false")
	}
}

// TestComplexExpression tests nested AND/OR expressions
func TestComplexExpression(t *testing.T) {
	context := map[string]any{
		"person": map[string]any{
			"age":    int64(25),
			"active": true,
		},
	}

	// (age > 18) AND (active = true)
	expr := &BinaryExpression{
		Left: &BinaryExpression{
			Left:     &PropertyExpression{Variable: "person", Property: "age"},
			Operator: ">",
			Right:    &LiteralExpression{Value: int64(18)},
		},
		Operator: "AND",
		Right: &BinaryExpression{
			Left:     &PropertyExpression{Variable: "person", Property: "active"},
			Operator: "=",
			Right:    &LiteralExpression{Value: true},
		},
	}

	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected (age > 18) AND (active = true) to be true")
	}
}

// TestDirection_String tests Direction enum string representation
func TestDirection_String(t *testing.T) {
	tests := []struct {
		dir      Direction
		expected string
	}{
		{DirectionOutgoing, "->"},
		{DirectionIncoming, "<-"},
		{DirectionBoth, "-"},
		{Direction(999), "?"}, // Unknown direction
	}

	for _, tt := range tests {
		result := tt.dir.String()
		if result != tt.expected {
			t.Errorf("Expected Direction(%d).String() = %s, got %s", tt.dir, tt.expected, result)
		}
	}
}

// TestCompareValues_TypeMismatch tests comparing different types
func TestCompareValues_TypeMismatch(t *testing.T) {
	context := map[string]any{
		"item": map[string]any{
			"name": "Product",
		},
	}

	// Compare string with number (should handle gracefully)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "item", Property: "name"},
		Operator: ">",
		Right:    &LiteralExpression{Value: int64(5)},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	// Type mismatch should return false (compareValues returns 0)
	if result {
		t.Error("Expected type mismatch comparison to be false")
	}
}

// TestBinaryExpression_CompareFloats tests float comparisons including equality
func TestBinaryExpression_CompareFloats(t *testing.T) {
	context := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{"value": storage.FloatValue(3.14)},
		},
		"b": &storage.Node{
			ID:         2,
			Properties: map[string]storage.Value{"value": storage.FloatValue(3.14)},
		},
	}

	// Test equal floats (triggers line 280: return 0)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "value"},
		Operator: "=",
		Right:    &PropertyExpression{Variable: "b", Property: "value"},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected equal floats to be true")
	}
}

// TestBinaryExpression_MixedIntFloat tests mixed int/float comparisons
func TestBinaryExpression_MixedIntFloat(t *testing.T) {
	// Test int on left, float on right (lines 284-291)
	context1 := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{"value": storage.IntValue(5)},
		},
	}
	expr1 := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "value"},
		Operator: "<",
		Right:    &LiteralExpression{Value: float64(5.5)},
	}
	result1, err := expr1.Eval(context1)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result1 {
		t.Error("Expected 5 < 5.5 to be true")
	}

	// Test float on left, int on right (lines 295-302)
	context2 := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{"value": storage.FloatValue(5.5)},
		},
	}
	expr2 := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "value"},
		Operator: ">",
		Right:    &LiteralExpression{Value: int64(5)},
	}
	result2, err := expr2.Eval(context2)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result2 {
		t.Error("Expected 5.5 > 5 to be true")
	}

	// Test equal mixed types - mixed int/float with equality
	// This tests the return 0 paths in lines 291 and 302
	expr3 := &BinaryExpression{
		Left:     &LiteralExpression{Value: int64(10)},
		Operator: ">=",
		Right:    &LiteralExpression{Value: float64(10.0)},
	}
	result3, err := expr3.Eval(map[string]any{})
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result3 {
		t.Error("Expected 10 >= 10.0 to be true")
	}
}

// TestBinaryExpression_CompareStrings tests string comparisons including equality
func TestBinaryExpression_CompareStrings(t *testing.T) {
	context := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{"name": storage.StringValue("alice")},
		},
		"b": &storage.Node{
			ID:         2,
			Properties: map[string]storage.Value{"name": storage.StringValue("alice")},
		},
	}

	// Test equal strings (triggers line 321: return 0)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "name"},
		Operator: "=",
		Right:    &PropertyExpression{Variable: "b", Property: "name"},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected equal strings to be true")
	}
}

// TestBinaryExpression_ExtractBoolValue tests bool value extraction
func TestBinaryExpression_ExtractBoolValue(t *testing.T) {
	context := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{"active": storage.BoolValue(true)},
		},
	}

	// Test bool extraction (lines 236-239)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "active"},
		Operator: "=",
		Right:    &LiteralExpression{Value: true},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	if !result {
		t.Error("Expected bool comparison to be true")
	}
}

// TestBinaryExpression_MissingProperty tests missing property returns nil
func TestBinaryExpression_MissingProperty(t *testing.T) {
	context := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{}, // Empty properties
		},
	}

	// Test missing property (line 242: return nil)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "nonexistent"},
		Operator: "=",
		Right:    &LiteralExpression{Value: int64(0)},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	// Comparing nil to int should return false
	if result {
		t.Error("Expected missing property comparison to be false")
	}
}

// TestBinaryExpression_UnknownComparisonOperator tests unknown operator error
func TestBinaryExpression_UnknownComparisonOperator(t *testing.T) {
	context := map[string]any{
		"a": &storage.Node{
			ID:         1,
			Properties: map[string]storage.Value{"value": storage.IntValue(5)},
		},
	}

	// Test unknown operator (line 209-210)
	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "a", Property: "value"},
		Operator: "~=", // Unknown operator
		Right:    &LiteralExpression{Value: int64(5)},
	}
	_, err := expr.Eval(context)
	if err == nil {
		t.Error("Expected error for unknown operator")
	}
	if err != nil && !errorContains(err.Error(), "unknown operator") {
		t.Errorf("Expected 'unknown operator' error, got: %v", err)
	}
}

// TestBinaryExpression_DefaultExtractValue tests default case in extractValue
func TestBinaryExpression_DefaultExtractValue(t *testing.T) {
	// Create an unsupported expression type for the default case (line 252-253)
	// Since BinaryExpression isn't typically used as a value expression,
	// this tests the defensive programming in extractValue

	// We can't easily construct this scenario since all Expression types are handled,
	// but we can verify nil is returned for missing variables
	context := map[string]any{} // Empty context

	expr := &BinaryExpression{
		Left:     &PropertyExpression{Variable: "nonexistent", Property: "value"},
		Operator: "=",
		Right:    &LiteralExpression{Value: int64(0)},
	}
	result, err := expr.Eval(context)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	// Comparing nil to int should return false
	if result {
		t.Error("Expected comparison with nonexistent variable to be false")
	}
}

// Helper function to check if error contains string
func errorContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && errorContainsSubstring(s, substr))
}

func errorContainsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

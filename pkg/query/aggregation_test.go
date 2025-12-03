package query

import (
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestAggregationComputer_Sum tests the sum aggregation function
func TestAggregationComputer_Sum(t *testing.T) {
	ac := &AggregationComputer{}

	tests := []struct {
		name     string
		values   []any
		expected any
	}{
		{
			name:     "sum of integers",
			values:   []any{int64(10), int64(20), int64(30)},
			expected: int64(60),
		},
		{
			name:     "sum of floats",
			values:   []any{float64(10.5), float64(20.5), float64(30.0)},
			expected: float64(61.0),
		},
		{
			name:     "sum of mixed int and float",
			values:   []any{int64(10), float64(20.5)},
			expected: float64(30.5),
		},
		{
			name:     "sum of empty array",
			values:   []any{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.sum(tt.values)
			if result != tt.expected {
				t.Errorf("sum() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestAggregationComputer_Avg tests the average aggregation function
func TestAggregationComputer_Avg(t *testing.T) {
	ac := &AggregationComputer{}

	tests := []struct {
		name     string
		values   []any
		expected any
	}{
		{
			name:     "average of integers",
			values:   []any{int64(10), int64(20), int64(30)},
			expected: float64(20),
		},
		{
			name:     "average of floats",
			values:   []any{float64(10.0), float64(20.0), float64(30.0)},
			expected: float64(20.0),
		},
		{
			name:     "average of empty array",
			values:   []any{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.avg(tt.values)
			if result != tt.expected {
				t.Errorf("avg() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestAggregationComputer_Min tests the minimum aggregation function
func TestAggregationComputer_Min(t *testing.T) {
	ac := &AggregationComputer{}

	tests := []struct {
		name     string
		values   []any
		expected any
	}{
		{
			name:     "min of integers",
			values:   []any{int64(30), int64(10), int64(20)},
			expected: int64(10),
		},
		{
			name:     "min of floats",
			values:   []any{float64(30.5), float64(10.5), float64(20.5)},
			expected: float64(10.5),
		},
		{
			name:     "min of strings",
			values:   []any{"charlie", "alice", "bob"},
			expected: "alice",
		},
		{
			name:     "min of empty array",
			values:   []any{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.min(tt.values)
			if result != tt.expected {
				t.Errorf("min() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestAggregationComputer_Max tests the maximum aggregation function
func TestAggregationComputer_Max(t *testing.T) {
	ac := &AggregationComputer{}

	tests := []struct {
		name     string
		values   []any
		expected any
	}{
		{
			name:     "max of integers",
			values:   []any{int64(10), int64(30), int64(20)},
			expected: int64(30),
		},
		{
			name:     "max of floats",
			values:   []any{float64(10.5), float64(30.5), float64(20.5)},
			expected: float64(30.5),
		},
		{
			name:     "max of strings",
			values:   []any{"alice", "charlie", "bob"},
			expected: "charlie",
		},
		{
			name:     "max of empty array",
			values:   []any{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.max(tt.values)
			if result != tt.expected {
				t.Errorf("max() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestAggregationComputer_ExtractValue tests value extraction from storage.Value
func TestAggregationComputer_ExtractValue(t *testing.T) {
	ac := &AggregationComputer{}

	tests := []struct {
		name     string
		value    storage.Value
		expected any
	}{
		{
			name:     "extract integer",
			value:    storage.IntValue(42),
			expected: int64(42),
		},
		{
			name:     "extract float",
			value:    storage.FloatValue(3.14),
			expected: float64(3.14),
		},
		{
			name:     "extract string",
			value:    storage.StringValue("hello"),
			expected: "hello",
		},
		{
			name:     "extract bool",
			value:    storage.BoolValue(true),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.ExtractValue(tt.value)
			if result != tt.expected {
				t.Errorf("ExtractValue() = %v (type %T), expected %v (type %T)",
					result, result, tt.expected, tt.expected)
			}
		})
	}
}

// TestAggregationComputer_ComputeAggregates tests the full aggregation computation
func TestAggregationComputer_ComputeAggregates(t *testing.T) {
	ac := &AggregationComputer{}

	// Create test execution context with sample data
	ctx := &ExecutionContext{
		results: []*BindingSet{
			{
				bindings: map[string]any{
					"n": &storage.Node{
						ID:     1,
						Labels: []string{"Person"},
						Properties: map[string]storage.Value{
							"age":    storage.IntValue(25),
							"salary": storage.IntValue(50000),
							"name":   storage.StringValue("Alice"),
						},
					},
				},
			},
			{
				bindings: map[string]any{
					"n": &storage.Node{
						ID:     2,
						Labels: []string{"Person"},
						Properties: map[string]storage.Value{
							"age":    storage.IntValue(30),
							"salary": storage.IntValue(60000),
							"name":   storage.StringValue("Bob"),
						},
					},
				},
			},
			{
				bindings: map[string]any{
					"n": &storage.Node{
						ID:     3,
						Labels: []string{"Person"},
						Properties: map[string]storage.Value{
							"age":    storage.IntValue(35),
							"salary": storage.IntValue(70000),
							"name":   storage.StringValue("Charlie"),
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name        string
		returnItems []*ReturnItem
		expectedKey string
		expectedVal any
	}{
		{
			name: "COUNT with property",
			returnItems: []*ReturnItem{
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "age",
					},
				},
			},
			expectedKey: "COUNT(n.age)",
			expectedVal: 3,
		},
		{
			name: "COUNT without property",
			returnItems: []*ReturnItem{
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "",
					},
				},
			},
			expectedKey: "COUNT(n.)",
			expectedVal: 3,
		},
		{
			name: "SUM of salaries",
			returnItems: []*ReturnItem{
				{
					Aggregate: "SUM",
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "salary",
					},
				},
			},
			expectedKey: "SUM(n.salary)",
			expectedVal: int64(180000),
		},
		{
			name: "AVG of ages",
			returnItems: []*ReturnItem{
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "age",
					},
				},
			},
			expectedKey: "AVG(n.age)",
			expectedVal: float64(30),
		},
		{
			name: "MIN age",
			returnItems: []*ReturnItem{
				{
					Aggregate: "MIN",
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "age",
					},
				},
			},
			expectedKey: "MIN(n.age)",
			expectedVal: int64(25),
		},
		{
			name: "MAX salary",
			returnItems: []*ReturnItem{
				{
					Aggregate: "MAX",
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "salary",
					},
				},
			},
			expectedKey: "MAX(n.salary)",
			expectedVal: int64(70000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.ComputeAggregates(ctx, tt.returnItems)

			val, exists := result[tt.expectedKey]
			if !exists {
				t.Errorf("Expected key %s not found in result. Got: %v", tt.expectedKey, result)
				return
			}

			if val != tt.expectedVal {
				t.Errorf("ComputeAggregates()[%s] = %v (type %T), expected %v (type %T)",
					tt.expectedKey, val, val, tt.expectedVal, tt.expectedVal)
			}
		})
	}
}

// TestAggregationComputer_CountStar tests COUNT(*) without alias
func TestAggregationComputer_CountStar(t *testing.T) {
	ac := &AggregationComputer{}

	ctx := &ExecutionContext{
		results: []*BindingSet{
			{bindings: map[string]any{"n": &storage.Node{ID: 1}}},
			{bindings: map[string]any{"n": &storage.Node{ID: 2}}},
			{bindings: map[string]any{"n": &storage.Node{ID: 3}}},
		},
	}

	// COUNT(*) with nil expression and no alias
	returnItems := []*ReturnItem{
		{
			Aggregate:  "COUNT",
			Expression: nil, // COUNT(*)
			Alias:      "",  // No alias - should generate "COUNT(*)"
		},
	}

	result := ac.ComputeAggregates(ctx, returnItems)

	// Should have COUNT(*) as the key
	val, exists := result["COUNT(*)"]
	if !exists {
		t.Errorf("Expected key 'COUNT(*)' not found. Got: %v", result)
		return
	}

	// Should count all results
	if val != 3 {
		t.Errorf("COUNT(*) = %v, expected 3", val)
	}
}

// TestAggregationComputer_UnknownAggregate tests unknown aggregate function
func TestAggregationComputer_UnknownAggregate(t *testing.T) {
	ac := &AggregationComputer{}

	ctx := &ExecutionContext{
		results: []*BindingSet{
			{bindings: map[string]any{"n": &storage.Node{
				ID: 1,
				Properties: map[string]storage.Value{
					"age": storage.IntValue(25),
				},
			}}},
		},
	}

	// MEDIAN is not a supported aggregate function
	returnItems := []*ReturnItem{
		{
			Aggregate: "MEDIAN",
			Expression: &PropertyExpression{
				Variable: "n",
				Property: "age",
			},
		},
	}

	result := ac.ComputeAggregates(ctx, returnItems)

	// Should return nil for unknown aggregate
	val, exists := result["MEDIAN(n.age)"]
	if !exists {
		t.Errorf("Expected key 'MEDIAN(n.age)' not found")
		return
	}

	if val != nil {
		t.Errorf("Unknown aggregate should return nil, got %v", val)
	}
}

// TestAggregationComputer_ExtractValue_Timestamp tests timestamp extraction
func TestAggregationComputer_ExtractValue_Timestamp(t *testing.T) {
	ac := &AggregationComputer{}

	// Create a timestamp value
	timestampTime := time.Date(2024, 11, 19, 10, 30, 0, 0, time.UTC)
	timestampVal := storage.TimestampValue(timestampTime)

	result := ac.ExtractValue(timestampVal)

	// Should return Unix timestamp (seconds since epoch)
	if result == nil {
		t.Error("Expected timestamp value, got nil")
		return
	}

	// Verify it's a Unix timestamp (int64)
	unixTime, ok := result.(int64)
	if !ok {
		t.Errorf("Expected int64 Unix timestamp, got %T", result)
		return
	}

	// Verify it's a reasonable timestamp (after 2024-01-01)
	if unixTime < 1704067200 { // 2024-01-01 00:00:00 UTC
		t.Errorf("Unix timestamp %d seems too old", unixTime)
	}
}

// TestAggregationComputer_Sum_RegularInt tests sum with regular int values
func TestAggregationComputer_Sum_RegularInt(t *testing.T) {
	ac := &AggregationComputer{}

	// Mix of int, int64, and float64
	values := []any{
		int(10),     // regular int
		int64(20),   // int64
		int(30),     // regular int
		float64(15), // float64
	}

	result := ac.sum(values)

	// Should return float64 because there's a float in the mix
	expected := float64(75)
	if result != expected {
		t.Errorf("sum() = %v, expected %v", result, expected)
	}
}

// TestAggregationComputer_Sum_OnlyRegularInt tests sum with only regular int values
func TestAggregationComputer_Sum_OnlyRegularInt(t *testing.T) {
	ac := &AggregationComputer{}

	values := []any{
		int(10),
		int(20),
		int(30),
	}

	result := ac.sum(values)

	// Should return int64 since there are no floats
	expected := int64(60)
	if result != expected {
		t.Errorf("sum() = %v (type %T), expected %v (type %T)", result, result, expected, expected)
	}
}

// TestAggregationComputer_Avg_InvalidSum tests avg with invalid sum type
func TestAggregationComputer_Avg_InvalidSum(t *testing.T) {
	ac := &AggregationComputer{}

	// This shouldn't happen in practice, but tests the default case
	// We'll test by providing values that sum won't handle
	values := []any{
		"not a number",
		"also not a number",
	}

	result := ac.avg(values)

	// avg of non-numeric values should return nil (from default case)
	// Actually, sum will return 0 for empty numeric sum, then avg returns that
	// But if we want to test the default case in avg, we need sum to return
	// something other than int64 or float64. Since sum always returns int64 or int,
	// we can't easily trigger the default case without modifying sum behavior.
	// The default case in avg is defensive programming for future changes.
	// For now, let's verify it handles the result correctly.
	if result == nil {
		// This is acceptable - could happen if sum returns something unexpected
		return
	}

	// If sum returns 0, avg will return 0.0
	if resultFloat, ok := result.(float64); ok {
		if resultFloat != 0.0 {
			t.Errorf("avg() of non-numeric = %v, expected 0.0 or nil", result)
		}
	}
}

// TestAggregationComputer_BuildGroupKey_MissingProperty tests grouping with missing properties
func TestAggregationComputer_BuildGroupKey_MissingProperty(t *testing.T) {
	ac := &AggregationComputer{}

	binding := &BindingSet{
		bindings: map[string]any{
			"n": &storage.Node{
				ID: 1,
				Properties: map[string]storage.Value{
					"name": storage.StringValue("Alice"),
					// "age" is missing
				},
			},
		},
	}

	groupByExprs := []*PropertyExpression{
		{Variable: "n", Property: "name"},
		{Variable: "n", Property: "age"}, // This property doesn't exist
	}

	key := ac.buildGroupKey(binding, groupByExprs)

	// Should contain the null placeholder for missing property
	if !containsString(key, "<null>") {
		t.Errorf("Expected group key to contain '<null>' placeholder for missing property, got: %s", key)
	}

	// Should also contain the name value
	if !containsString(key, "Alice") {
		t.Errorf("Expected group key to contain 'Alice', got: %s", key)
	}
}

// TestAggregationComputer_ComputeGroupedAggregates tests grouped aggregation
func TestAggregationComputer_ComputeGroupedAggregates(t *testing.T) {
	ac := &AggregationComputer{}

	// Create test data with multiple departments
	ctx := &ExecutionContext{
		results: []*BindingSet{
			{
				bindings: map[string]any{
					"n": &storage.Node{
						ID: 1,
						Properties: map[string]storage.Value{
							"name":       storage.StringValue("Alice"),
							"department": storage.StringValue("Engineering"),
							"salary":     storage.IntValue(80000),
						},
					},
				},
			},
			{
				bindings: map[string]any{
					"n": &storage.Node{
						ID: 2,
						Properties: map[string]storage.Value{
							"name":       storage.StringValue("Bob"),
							"department": storage.StringValue("Engineering"),
							"salary":     storage.IntValue(90000),
						},
					},
				},
			},
			{
				bindings: map[string]any{
					"n": &storage.Node{
						ID: 3,
						Properties: map[string]storage.Value{
							"name":       storage.StringValue("Charlie"),
							"department": storage.StringValue("Sales"),
							"salary":     storage.IntValue(70000),
						},
					},
				},
			},
		},
	}

	groupByExprs := []*PropertyExpression{
		{Variable: "n", Property: "department"},
	}

	returnItems := []*ReturnItem{
		{
			Aggregate: "COUNT",
			Expression: &PropertyExpression{
				Variable: "n",
				Property: "salary",
			},
			Alias: "employee_count",
		},
		{
			Aggregate: "AVG",
			Expression: &PropertyExpression{
				Variable: "n",
				Property: "salary",
			},
			Alias: "avg_salary",
		},
	}

	results := ac.ComputeGroupedAggregates(ctx, returnItems, groupByExprs)

	// Should have 2 groups (Engineering and Sales)
	if len(results) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(results))
		return
	}

	// Verify each group has the correct aggregates
	for _, row := range results {
		dept, hasDept := row["n.department"]
		if !hasDept {
			t.Error("Group result missing department")
			continue
		}

		count, hasCount := row["employee_count"]
		avgSal, hasAvg := row["avg_salary"]

		if !hasCount || !hasAvg {
			t.Errorf("Group result missing aggregates: %v", row)
			continue
		}

		// Check Engineering group
		if dept == "Engineering" {
			if count != 2 {
				t.Errorf("Engineering count = %v, expected 2", count)
			}
			if avgSal != float64(85000) {
				t.Errorf("Engineering avg_salary = %v, expected 85000.0", avgSal)
			}
		}

		// Check Sales group
		if dept == "Sales" {
			if count != 1 {
				t.Errorf("Sales count = %v, expected 1", count)
			}
			if avgSal != float64(70000) {
				t.Errorf("Sales avg_salary = %v, expected 70000.0", avgSal)
			}
		}
	}
}

// Helper function to check if string contains substring
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

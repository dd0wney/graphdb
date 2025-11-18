package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestAggregationComputer_Sum tests the sum aggregation function
func TestAggregationComputer_Sum(t *testing.T) {
	ac := &AggregationComputer{}

	tests := []struct {
		name     string
		values   []interface{}
		expected interface{}
	}{
		{
			name:     "sum of integers",
			values:   []interface{}{int64(10), int64(20), int64(30)},
			expected: int64(60),
		},
		{
			name:     "sum of floats",
			values:   []interface{}{float64(10.5), float64(20.5), float64(30.0)},
			expected: float64(61.0),
		},
		{
			name:     "sum of mixed int and float",
			values:   []interface{}{int64(10), float64(20.5)},
			expected: float64(30.5),
		},
		{
			name:     "sum of empty array",
			values:   []interface{}{},
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
		values   []interface{}
		expected interface{}
	}{
		{
			name:     "average of integers",
			values:   []interface{}{int64(10), int64(20), int64(30)},
			expected: float64(20),
		},
		{
			name:     "average of floats",
			values:   []interface{}{float64(10.0), float64(20.0), float64(30.0)},
			expected: float64(20.0),
		},
		{
			name:     "average of empty array",
			values:   []interface{}{},
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
		values   []interface{}
		expected interface{}
	}{
		{
			name:     "min of integers",
			values:   []interface{}{int64(30), int64(10), int64(20)},
			expected: int64(10),
		},
		{
			name:     "min of floats",
			values:   []interface{}{float64(30.5), float64(10.5), float64(20.5)},
			expected: float64(10.5),
		},
		{
			name:     "min of strings",
			values:   []interface{}{"charlie", "alice", "bob"},
			expected: "alice",
		},
		{
			name:     "min of empty array",
			values:   []interface{}{},
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
		values   []interface{}
		expected interface{}
	}{
		{
			name:     "max of integers",
			values:   []interface{}{int64(10), int64(30), int64(20)},
			expected: int64(30),
		},
		{
			name:     "max of floats",
			values:   []interface{}{float64(10.5), float64(30.5), float64(20.5)},
			expected: float64(30.5),
		},
		{
			name:     "max of strings",
			values:   []interface{}{"alice", "charlie", "bob"},
			expected: "charlie",
		},
		{
			name:     "max of empty array",
			values:   []interface{}{},
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
		expected interface{}
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
				bindings: map[string]interface{}{
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
				bindings: map[string]interface{}{
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
				bindings: map[string]interface{}{
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
		expectedVal interface{}
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

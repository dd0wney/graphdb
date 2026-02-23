package query

import (
	"math"
	"testing"
)

func TestNumericFunctions(t *testing.T) {
	tests := []struct {
		name     string
		fn       string
		args     []any
		expected any
	}{
		{"abs positive int", "abs", []any{int64(5)}, int64(5)},
		{"abs negative int", "abs", []any{int64(-5)}, int64(5)},
		{"abs positive float", "abs", []any{float64(3.14)}, float64(3.14)},
		{"abs negative float", "abs", []any{float64(-3.14)}, float64(3.14)},
		{"abs nil", "abs", []any{nil}, nil},
		{"ceil", "ceil", []any{float64(1.2)}, float64(2)},
		{"ceil negative", "ceil", []any{float64(-1.8)}, float64(-1)},
		{"ceil int", "ceil", []any{int64(5)}, float64(5)},
		{"floor", "floor", []any{float64(1.8)}, float64(1)},
		{"floor negative", "floor", []any{float64(-1.2)}, float64(-2)},
		{"round up", "round", []any{float64(1.6)}, float64(2)},
		{"round down", "round", []any{float64(1.4)}, float64(1)},
		{"round half", "round", []any{float64(2.5)}, float64(3)},
		{"toInteger from float", "toInteger", []any{float64(3.14)}, int64(3)},
		{"toInteger from int", "toInteger", []any{int64(42)}, int64(42)},
		{"toInteger from string", "toInteger", []any{"123"}, int64(123)},
		{"toFloat from int", "toFloat", []any{int64(42)}, float64(42)},
		{"toFloat from float", "toFloat", []any{float64(3.14)}, float64(3.14)},
		{"toFloat from string", "toFloat", []any{"3.14"}, float64(3.14)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, err := GetFunction(tt.fn)
			if err != nil {
				t.Fatalf("GetFunction(%q) failed: %v", tt.fn, err)
			}

			result, err := fn(tt.args)
			if err != nil {
				t.Fatalf("Function call failed: %v", err)
			}

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			// Float comparison with tolerance
			if ef, ok := tt.expected.(float64); ok {
				rf, ok := result.(float64)
				if !ok {
					t.Fatalf("Expected float64, got %T: %v", result, result)
				}
				if math.Abs(ef-rf) > 0.001 {
					t.Errorf("Expected %v, got %v", ef, rf)
				}
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
			}
		})
	}
}

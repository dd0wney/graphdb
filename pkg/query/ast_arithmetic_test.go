package query

import (
	"math"
	"testing"
)

func TestArithmeticExpression_EvalValue(t *testing.T) {
	ctx := map[string]any{}

	tests := []struct {
		name     string
		left     any
		op       string
		right    any
		expected any
		wantErr  bool
	}{
		// int64 + int64
		{"int add", int64(3), "+", int64(4), int64(7), false},
		{"int sub", int64(10), "-", int64(3), int64(7), false},
		{"int mul", int64(5), "*", int64(6), int64(30), false},
		{"int div", int64(10), "/", int64(3), int64(3), false}, // integer division
		{"int mod", int64(10), "%", int64(3), int64(1), false},

		// float64 + float64
		{"float add", float64(1.5), "+", float64(2.5), float64(4.0), false},
		{"float sub", float64(5.5), "-", float64(2.0), float64(3.5), false},
		{"float mul", float64(3.0), "*", float64(2.5), float64(7.5), false},
		{"float div", float64(7.0), "/", float64(2.0), float64(3.5), false},
		{"float mod", float64(7.5), "%", float64(2.0), float64(1.5), false},

		// mixed int64/float64 → promote to float64
		{"mixed int+float", int64(3), "+", float64(1.5), float64(4.5), false},
		{"mixed float+int", float64(1.5), "+", int64(3), float64(4.5), false},
		{"mixed int*float", int64(4), "*", float64(2.5), float64(10.0), false},
		{"mixed float/int", float64(7.0), "/", int64(2), float64(3.5), false},

		// string concatenation
		{"string concat", "hello", "+", " world", "hello world", false},
		{"string concat empty", "abc", "+", "", "abc", false},

		// null propagation
		{"nil left +", nil, "+", int64(5), nil, false},
		{"nil right +", int64(5), "+", nil, nil, false},
		{"nil both +", nil, "+", nil, nil, false},
		{"nil left *", nil, "*", int64(5), nil, false},

		// division by zero
		{"int div by zero", int64(5), "/", int64(0), nil, true},
		{"float div by zero", float64(5.0), "/", float64(0.0), nil, true},
		{"int mod by zero", int64(5), "%", int64(0), nil, true},

		// type mismatch
		{"string * int", "abc", "*", int64(3), nil, true},
		{"int + string", int64(3), "+", "abc", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &ArithmeticExpression{
				Left:     &LiteralExpression{Value: tt.left},
				Operator: tt.op,
				Right:    &LiteralExpression{Value: tt.right},
			}

			result, err := expr.EvalValue(ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got result=%v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			switch exp := tt.expected.(type) {
			case int64:
				got, ok := result.(int64)
				if !ok {
					t.Fatalf("expected int64, got %T(%v)", result, result)
				}
				if got != exp {
					t.Errorf("expected %d, got %d", exp, got)
				}
			case float64:
				got, ok := result.(float64)
				if !ok {
					t.Fatalf("expected float64, got %T(%v)", result, result)
				}
				if math.Abs(got-exp) > 1e-9 {
					t.Errorf("expected %f, got %f", exp, got)
				}
			case string:
				got, ok := result.(string)
				if !ok {
					t.Fatalf("expected string, got %T(%v)", result, result)
				}
				if got != exp {
					t.Errorf("expected %q, got %q", exp, got)
				}
			}
		})
	}
}

func TestArithmeticExpression_Eval_BoolCoercion(t *testing.T) {
	ctx := map[string]any{}

	// Nonzero result → true
	expr := &ArithmeticExpression{
		Left:     &LiteralExpression{Value: int64(3)},
		Operator: "+",
		Right:    &LiteralExpression{Value: int64(4)},
	}
	result, err := expr.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for 3+4=7")
	}

	// Zero result → false
	expr2 := &ArithmeticExpression{
		Left:     &LiteralExpression{Value: int64(5)},
		Operator: "-",
		Right:    &LiteralExpression{Value: int64(5)},
	}
	result2, err := expr2.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2 {
		t.Error("expected false for 5-5=0")
	}
}

func TestUnaryExpression_NOT(t *testing.T) {
	ctx := map[string]any{}

	tests := []struct {
		name     string
		operand  Expression
		expected bool
	}{
		{"NOT true", &LiteralExpression{Value: true}, false},
		{"NOT false", &LiteralExpression{Value: false}, true},
		// NOT nil → true (null is falsy)
		{"NOT nil", &LiteralExpression{Value: nil}, true},
		// NOT (nonzero) → false
		{"NOT nonzero", &LiteralExpression{Value: int64(42)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &UnaryExpression{
				Operator: "NOT",
				Operand:  tt.operand,
			}
			result, err := expr.Eval(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestUnaryExpression_Minus(t *testing.T) {
	ctx := map[string]any{}

	tests := []struct {
		name     string
		operand  any
		expected any
		wantErr  bool
	}{
		{"negate int64", int64(42), int64(-42), false},
		{"negate float64", float64(3.14), float64(-3.14), false},
		{"negate negative int", int64(-10), int64(10), false},
		{"negate zero", int64(0), int64(0), false},
		{"negate nil", nil, nil, false},
		{"negate string", "abc", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &UnaryExpression{
				Operator: "-",
				Operand:  &LiteralExpression{Value: tt.operand},
			}

			result, err := expr.EvalValue(ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			switch exp := tt.expected.(type) {
			case int64:
				got, ok := result.(int64)
				if !ok {
					t.Fatalf("expected int64, got %T(%v)", result, result)
				}
				if got != exp {
					t.Errorf("expected %d, got %d", exp, got)
				}
			case float64:
				got, ok := result.(float64)
				if !ok {
					t.Fatalf("expected float64, got %T(%v)", result, result)
				}
				if math.Abs(got-exp) > 1e-9 {
					t.Errorf("expected %f, got %f", exp, got)
				}
			}
		})
	}
}

func TestUnaryExpression_NOT_EvalValue(t *testing.T) {
	ctx := map[string]any{}

	expr := &UnaryExpression{
		Operator: "NOT",
		Operand:  &LiteralExpression{Value: true},
	}
	result, err := expr.EvalValue(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	boolResult, ok := result.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", result)
	}
	if boolResult != false {
		t.Error("expected NOT true = false")
	}
}

func TestArithmeticExpression_NestedExpressions(t *testing.T) {
	ctx := map[string]any{}

	// (3 + 4) * 2 = 14
	expr := &ArithmeticExpression{
		Left: &ArithmeticExpression{
			Left:     &LiteralExpression{Value: int64(3)},
			Operator: "+",
			Right:    &LiteralExpression{Value: int64(4)},
		},
		Operator: "*",
		Right:    &LiteralExpression{Value: int64(2)},
	}

	result, err := expr.EvalValue(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != int64(14) {
		t.Errorf("expected 14, got %v", result)
	}
}

func TestArithmeticExpression_WithPropertyExpression(t *testing.T) {
	ctx := map[string]any{
		"x": map[string]any{"val": int64(10)},
	}

	// x.val + 5
	expr := &ArithmeticExpression{
		Left:     &PropertyExpression{Variable: "x", Property: "val"},
		Operator: "+",
		Right:    &LiteralExpression{Value: int64(5)},
	}

	result, err := expr.EvalValue(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != int64(15) {
		t.Errorf("expected 15, got %v", result)
	}
}

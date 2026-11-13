package query

import (
	"strings"
	"testing"
)

// loopingExpression is a type that evalTri has no case for and whose Eval calls
// evalPredicate on itself, as every value type does. Without a guard at the end
// of evalTri this recurses until Go stops the process with a stack overflow,
// which recover cannot catch.
type loopingExpression struct{}

func (l *loopingExpression) Eval(context map[string]any) (bool, error) {
	return evalPredicate(l, context)
}

func (l *loopingExpression) EvalValue(context map[string]any) (any, error) { return nil, nil }

func TestEvalTri_UnknownExpressionTypeIsAnErrorNotALoop(t *testing.T) {
	_, err := evalTri(&loopingExpression{}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "no boolean rule") {
		t.Fatalf("err = %v, want a plain error that says no boolean rule", err)
	}
	if isRefusal(err) {
		t.Errorf("err %q is a RefusalError, want a plain error: the fault is in the code, not the query", err)
	}
	if !strings.Contains(err.Error(), "loopingExpression") {
		t.Errorf("err %q does not name the Go type", err)
	}
}

// A new Expression type must get a row here and a case in evalTri. A row that
// answers "no boolean rule" means evalTri has no case for the type.
func TestEvalTri_EveryExpressionTypeHasARule(t *testing.T) {
	lit := func(v any) Expression { return &LiteralExpression{Value: v} }
	ctx := map[string]any{"$p": true}
	rows := []struct {
		name string
		expr Expression
	}{
		{"BinaryExpression", &BinaryExpression{Left: lit(int64(1)), Operator: "IN", Right: lit([]any{int64(1)})}},
		{"UnaryExpression NOT", &UnaryExpression{Operator: "NOT", Operand: lit(true)}},
		{"UnaryExpression minus", &UnaryExpression{Operator: "-", Operand: lit(nil)}},
		{"PropertyExpression", &PropertyExpression{Variable: "n", Property: "b"}},
		{"LiteralExpression", lit(true)},
		{"ParameterExpression", &ParameterExpression{Name: "p"}},
		{"FunctionCallExpression", &FunctionCallExpression{Name: "toBoolean", Args: []Expression{lit("true")}}},
		{"CaseExpression", &CaseExpression{WhenClauses: []CaseWhen{{Condition: lit(true), Result: lit(true)}}}},
		{"ArithmeticExpression", &ArithmeticExpression{Left: lit(nil), Operator: "+", Right: lit(int64(1))}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, err := evalTri(r.expr, ctx)
			if err != nil && strings.Contains(err.Error(), "no boolean rule") {
				t.Fatalf("evalTri has no rule for %T: %v", r.expr, err)
			}
		})
	}
}

func TestEvalTri_UnknownUnaryOperatorIsAnError(t *testing.T) {
	_, err := evalTri(&UnaryExpression{Operator: "~", Operand: &LiteralExpression{Value: true}}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "unknown unary operator") {
		t.Fatalf("err = %v, want the unknown unary operator error", err)
	}
}

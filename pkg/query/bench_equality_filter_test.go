package query

import "testing"

// BenchmarkEqualityFilter measures the predicates a WHERE filter runs on every
// row. The scalar sub-benchmarks must stay at 0 allocs/op: a filter touches
// every candidate row, so a cost here is paid once per row.
func BenchmarkEqualityFilter(b *testing.B) {
	eight := func() []any {
		return []any{int64(1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7), int64(8)}
	}
	ctx := map[string]any{"n": map[string]any{
		"a": int64(3),
		"s": "x",
		"l": eight(),
	}}
	cases := []struct {
		name string
		expr Expression
	}{
		{"eq-int", &BinaryExpression{
			Left: &PropertyExpression{Variable: "n", Property: "a"}, Operator: "=",
			Right: &LiteralExpression{Value: int64(3)}}},
		{"eq-string", &BinaryExpression{
			Left: &PropertyExpression{Variable: "n", Property: "s"}, Operator: "=",
			Right: &LiteralExpression{Value: "x"}}},
		{"in-list", &BinaryExpression{
			Left: &PropertyExpression{Variable: "n", Property: "a"}, Operator: "IN",
			Right: &LiteralExpression{Value: []any{int64(1), int64(2), int64(3)}}}},
		{"eq-list", &BinaryExpression{
			Left: &PropertyExpression{Variable: "n", Property: "l"}, Operator: "=",
			Right: &LiteralExpression{Value: eight()}}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ok, err := c.expr.Eval(ctx)
				if err != nil || !ok {
					b.Fatalf("Eval = %v, %v, want true", ok, err)
				}
			}
		})
	}
}

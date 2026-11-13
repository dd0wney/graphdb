package query

import (
	"fmt"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// BenchmarkOrderBy guards the cost of a sort: the row key of an ORDER BY item
// must be computed once for the query, not once for each comparison.
func BenchmarkOrderBy(b *testing.B) {
	gs, err := storage.NewGraphStorage(b.TempDir())
	if err != nil {
		b.Fatalf("NewGraphStorage: %v", err)
	}
	b.Cleanup(func() { _ = gs.Close() })
	const n = 10000
	for i := 0; i < n; i++ {
		// A multiplier coprime with n scatters the ages, so the input is unsorted.
		age := int64((i * 7919) % n)
		if _, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("p%d", i)),
			"age":  storage.IntValue(age),
		}); err != nil {
			b.Fatalf("CreateNode: %v", err)
		}
	}
	ex := NewExecutor(gs)

	for _, tc := range []struct{ name, text string }{
		{"property", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age`},
		{"expression", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age + 1`},
	} {
		b.Run(tc.name, func(b *testing.B) {
			tokens, err := NewLexer(tc.text).Tokenize()
			if err != nil {
				b.Fatalf("Tokenize: %v", err)
			}
			q, err := NewParser(tokens).Parse()
			if err != nil {
				b.Fatalf("Parse: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rs, err := ex.Execute(q)
				if err != nil || len(rs.Rows) != n {
					b.Fatalf("Execute: rows=%d err=%v", len(rs.Rows), err)
				}
			}
		})
	}
}

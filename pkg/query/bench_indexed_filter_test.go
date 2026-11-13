package query

import (
	"os"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// BenchmarkIndexedFilter runs an indexed read with a second WHERE condition.
// It guards the cost of the optimizer path that picks the index and keeps the
// filter in place: planning, the index lookup and the filter, on 10,000 nodes.
func BenchmarkIndexedFilter(b *testing.B) {
	dir, err := os.MkdirTemp("", "indexed-filter-bench-*")
	if err != nil {
		b.Fatalf("MkdirTemp: %v", err)
	}
	b.Cleanup(func() { _ = os.RemoveAll(dir) })
	gs, err := storage.NewGraphStorage(dir)
	if err != nil {
		b.Fatalf("NewGraphStorage: %v", err)
	}
	b.Cleanup(func() { _ = gs.Close() })

	if err := gs.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
		b.Fatalf("CreatePropertyIndex: %v", err)
	}
	for i := 1; i <= 10000; i++ {
		if _, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{
			"pid": storage.IntValue(int64(i)),
			"k":   storage.IntValue(int64(i % 7)),
		}); err != nil {
			b.Fatalf("CreateNode: %v", err)
		}
	}

	ex := NewExecutor(gs)
	toks, lerr := NewLexer("MATCH (p:T) WHERE p.pid = 5000 AND p.k > 0 RETURN p.pid").Tokenize()
	if lerr != nil {
		b.Fatalf("lex: %v", lerr)
	}
	parsed, perr := NewParser(toks).Parse()
	if perr != nil {
		b.Fatalf("parse: %v", perr)
	}

	if res, err := ex.Execute(parsed); err != nil || len(res.Rows) != 1 {
		b.Fatalf("setup query: rows=%v err=%v, want one row", res, err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ex.Execute(parsed); err != nil {
			b.Fatal(err)
		}
	}
}

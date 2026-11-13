package query

import (
	"os"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// BenchmarkWithChainParams measures what a parameter costs a WITH chain. The
// parameter form must not allocate more than the literal form by more than a
// few percent: the rows of the first segment are projected once, and the
// run's parameters go into the projected rows as they are built.
var (
	withChainStoreOnce sync.Once
	withChainExec      *Executor
)

func withChainBenchExecutor(b *testing.B) *Executor {
	b.Helper()
	withChainStoreOnce.Do(func() {
		dir, err := os.MkdirTemp("", "with-chain-bench-*")
		if err != nil {
			b.Fatal(err)
		}
		gs, err := storage.NewGraphStorage(dir)
		if err != nil {
			b.Fatal(err)
		}
		for i := 0; i < 100000; i++ {
			if _, err := gs.CreateNode([]string{"P"}, map[string]storage.Value{"k": storage.IntValue(int64(i))}); err != nil {
				b.Fatal(err)
			}
		}
		withChainExec = NewExecutor(gs)
	})
	return withChainExec
}

func BenchmarkWithChainParams(b *testing.B) {
	ex := withChainBenchExecutor(b)

	b.Run("parameter", func(b *testing.B) {
		q := parseB(b, `MATCH (a:P) WITH a WHERE a.k > $v RETURN count(a) AS c`)
		params := map[string]any{"v": int64(50000)}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := ex.ExecuteWithParams(q, params); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("literal", func(b *testing.B) {
		q := parseB(b, `MATCH (a:P) WITH a WHERE a.k > 50000 RETURN count(a) AS c`)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := ex.Execute(q); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func parseB(b *testing.B, text string) *Query {
	b.Helper()
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		b.Fatal(err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		b.Fatal(err)
	}
	return q
}

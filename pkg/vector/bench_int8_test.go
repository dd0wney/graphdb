package vector

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
)

var benchDotSink atomic.Int64

func BenchmarkDotInt8(b *testing.B) {
	for _, dim := range []int{768, 1536} {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			rng := rand.New(rand.NewSource(1))
			x := make([]int8, dim)
			y := make([]int8, dim)
			for i := 0; i < dim; i++ {
				x[i] = int8(rng.Intn(255) - 127)
				y[i] = int8(rng.Intn(255) - 127)
			}
			b.SetBytes(int64(2 * dim)) // bytes loaded per dot
			b.ResetTimer()
			var acc int64
			for i := 0; i < b.N; i++ {
				acc += int64(dotInt8(x, y))
			}
			benchDotSink.Store(acc) // defeat dead-code elimination
		})
	}
}

func BenchmarkHNSWSearchInt8(b *testing.B) {
	const (
		dim = 768
		n   = 5000
		k   = 10
		ef  = 64
	)
	rng := rand.New(rand.NewSource(7))
	idx, err := NewHNSWIndex(dim, 16, 200, MetricCosine)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for d := range v {
			v[d] = float32(rng.NormFloat64())
		}
		if err := idx.Insert(uint64(i), v); err != nil {
			b.Fatal(err)
		}
	}
	query := make([]float32, dim)
	for d := range query {
		query[d] = float32(rng.NormFloat64())
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := idx.Search(query, k, ef); err != nil {
			b.Fatal(err)
		}
	}
}

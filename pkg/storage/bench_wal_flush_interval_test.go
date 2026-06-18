package storage

import (
	"sync"
	"testing"
	"time"
)

// BenchmarkWALFlushInterval compares per-write-fsync (baseline) against
// BatchedWAL at several flush intervals, under concurrent writers. Run with:
//
//	go test ./pkg/storage/ -run '^$' -bench BenchmarkWALFlushInterval -benchtime 2s -timeout 600s
func BenchmarkWALFlushInterval(b *testing.B) {
	cases := []struct {
		name     string
		batching bool
		interval time.Duration
	}{
		{"baseline-perwrite-fsync", false, 0},
		{"batched-1ms", true, 1 * time.Millisecond},
		{"batched-2ms", true, 2 * time.Millisecond},
		{"batched-5ms", true, 5 * time.Millisecond},
		{"batched-10ms", true, 10 * time.Millisecond},
	}
	const writers = 8
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			cfg := DefaultStorageConfig(b.TempDir())
			cfg.EnableBatching = tc.batching
			if tc.batching {
				cfg.FlushInterval = tc.interval
			}
			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				b.Fatal(err)
			}
			defer gs.Close()
			b.ResetTimer()
			var wg sync.WaitGroup
			perWriter := b.N / writers
			for w := 0; w < writers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < perWriter; i++ {
						if _, err := gs.CreateNodeWithTenant("t", []string{"N"},
							map[string]Value{"k": IntValue(int64(i))}); err != nil {
							b.Error(err)
							return
						}
					}
				}()
			}
			wg.Wait()
		})
	}
}

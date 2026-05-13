package intelligence

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// slowFakeEmbedder simulates a real Embedder that takes time per call —
// e.g., a network-backed embedder, or LSA over a large index. Used by the
// load test to push the worker pool into saturation predictably.
type slowFakeEmbedder struct {
	delay time.Duration
	calls atomic.Int64
	vec   []float32
}

func (e *slowFakeEmbedder) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	e.calls.Add(1)
	time.Sleep(e.delay)
	return e.vec, nil
}

// TestAutoEmbedObserver_BackpressureUnderLoad exercises the full
// CreateNode → notifyNodeCreated → AutoEmbedObserver → Pool.Submit
// path under sustained pressure to validate the spike §7.5 backpressure
// invariants in production-shaped conditions (R2.1 + R2.2 + R2.5a all
// composed, not just unit-tested in isolation).
//
// What the test pins:
//
//  1. CreateNode latency stays bounded when the pool saturates. The
//     spike's drop-on-full design means CreateNode never blocks waiting
//     for embed work — the load test verifies this end-to-end, not just
//     at the Pool layer (TestPoolBackpressureDrop in worker_test.go
//     covers Pool-direct submits; this covers the observer-dispatched
//     path).
//
//  2. Pool.Dropped() > 0 when load exceeds capacity. If dropped stays
//     zero, the pool isn't actually saturating (queue too large, workers
//     too many, or test load too small).
//
//  3. No goroutine leak after Shutdown drains. runtime.NumGoroutine()
//     measured before + after the test should be within a small delta;
//     a leak indicates Pool workers aren't being reaped on Shutdown.
//
//  4. Race-detector clean. Run with -race; concurrent observer dispatch
//     + concurrent CreateNode + concurrent embedder writes must not
//     surface a race condition.
//
// Test is GRAPHDB_BENCH_LARGE-gated. The full load run takes a few
// seconds + node-create overhead; not CI-friendly given the existing
// CI matrix's existing pressure. Operators run when validating
// architectural assumptions under realistic load.
//
//	go test -v -run TestAutoEmbedObserver_BackpressureUnderLoad ./pkg/intelligence/
//
//	GRAPHDB_BENCH_LARGE=1 go test -v -race -run TestAutoEmbedObserver_BackpressureUnderLoad -timeout 120s ./pkg/intelligence/
func TestAutoEmbedObserver_BackpressureUnderLoad(t *testing.T) {
	if os.Getenv("GRAPHDB_BENCH_LARGE") == "" {
		t.Skip("skipping load test (GRAPHDB_BENCH_LARGE not set; runtime ~seconds-to-minutes)")
	}

	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig() error = %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	// Deliberately small pool to ensure saturation: 2 workers + 10-deep
	// queue. With per-task delay of 50ms, sustained throughput is
	// ~40 tasks/sec — any load above that will accumulate in queue
	// then drop.
	pool := NewPool(PoolConfig{Workers: 2, QueueDepth: 10})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if unfinished := pool.Shutdown(ctx); unfinished > 0 {
			t.Logf("Shutdown reported %d unfinished tasks (timeout)", unfinished)
		}
	})

	embedder := &slowFakeEmbedder{
		delay: 50 * time.Millisecond,
		vec:   []float32{1, 2, 3, 4, 5},
	}

	obs, err := NewAutoEmbedObserver(gs, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	if err != nil {
		t.Fatalf("NewAutoEmbedObserver() error = %v", err)
	}
	gs.AddObserver(obs)

	// Baseline goroutine count BEFORE the load — used to detect leaks
	// after Shutdown drains.
	goroutinesBefore := runtime.NumGoroutine()

	// Load shape: 8 concurrent producers, each creating 50 nodes. Total
	// 400 nodes vs pool capacity (2 workers × ~50ms = ~40 tasks/sec, plus
	// 10-deep queue = ~20 tasks in flight). Most tasks will drop.
	const (
		numProducers     = 8
		nodesPerProducer = 50
	)

	var (
		producerWG    sync.WaitGroup
		maxCreateLat  atomic.Int64 // nanoseconds; tracks worst observed CreateNode time
		totalCreates  atomic.Int64
		totalCreateNs atomic.Int64
	)

	loadStart := time.Now()
	for p := 0; p < numProducers; p++ {
		producerWG.Add(1)
		go func(producerID int) {
			defer producerWG.Done()
			for n := 0; n < nodesPerProducer; n++ {
				start := time.Now()
				_, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]storage.Value{
					"body": storage.StringValue("doc text for producer-pressure"),
				})
				latNs := time.Since(start).Nanoseconds()
				if err != nil {
					t.Errorf("producer %d create %d: %v", producerID, n, err)
					return
				}
				totalCreates.Add(1)
				totalCreateNs.Add(latNs)
				// Track max latency observed — used for the bounded-latency
				// assertion below.
				for {
					prev := maxCreateLat.Load()
					if latNs <= prev || maxCreateLat.CompareAndSwap(prev, latNs) {
						break
					}
				}
			}
		}(p)
	}
	producerWG.Wait()
	loadElapsed := time.Since(loadStart)

	// Allow workers a moment to process whatever's in flight at load-end.
	// We don't drain here — we want to observe drops, not converge.
	time.Sleep(100 * time.Millisecond)

	dropped := pool.Dropped()
	embedderCalls := embedder.calls.Load()
	avgLatMs := float64(totalCreateNs.Load()) / float64(totalCreates.Load()) / 1e6
	maxLatMs := float64(maxCreateLat.Load()) / 1e6

	t.Logf("LOAD_RESULT producers=%d nodes_per_producer=%d total_creates=%d elapsed=%v",
		numProducers, nodesPerProducer, totalCreates.Load(), loadElapsed)
	t.Logf("LOAD_RESULT embedder_calls=%d pool_dropped=%d",
		embedderCalls, dropped)
	t.Logf("LOAD_RESULT create_latency_avg_ms=%.2f create_latency_max_ms=%.2f",
		avgLatMs, maxLatMs)

	// Assertion 1: pool saturated. If dropped == 0, the load shape is
	// too small or the pool is too large — test isn't actually exercising
	// the backpressure path it claims to.
	if dropped == 0 {
		t.Errorf("Pool.Dropped() = 0; load shape did not saturate the pool — backpressure path untested")
	}

	// Assertion 2: CreateNode latency stays bounded. The spike's
	// drop-on-full design means CreateNode should never wait for embed
	// work. Without backpressure, CreateNode latency would balloon to
	// the embedder's delay (50ms) × queue depth. With backpressure, it
	// stays sub-millisecond + storage overhead.
	//
	// Threshold: 100ms is generous — actual observed CreateNode times
	// should be <5ms. 100ms catches the failure mode where Submit
	// blocks waiting for queue space, not the routine variance.
	const maxAcceptableLatMs = 100.0
	if maxLatMs > maxAcceptableLatMs {
		t.Errorf("max CreateNode latency = %.2fms > %.2fms — backpressure may be blocking CreateNode",
			maxLatMs, maxAcceptableLatMs)
	}

	// Assertion 3: tasks-attempted = creates - drops. embedder.calls
	// gives a lower bound on tasks that started Execute; combined with
	// drops, total should equal total_creates.
	//
	// Note: there's a race window where the load loop ended but workers
	// are still draining. The 100ms sleep above + the assertion's
	// "<=" comparison absorb this. We verify the relationship is
	// plausible, not exact.
	if embedderCalls+int64(dropped) > totalCreates.Load() {
		t.Errorf("embedder_calls=%d + dropped=%d > creates=%d — counts don't add up",
			embedderCalls, dropped, totalCreates.Load())
	}

	// Pool drain. Done in t.Cleanup but the explicit call here lets us
	// measure post-drain goroutine count without waiting for the cleanup
	// fn to run.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool.Shutdown(ctx)

	// Give the runtime a moment to fully reap finished worker goroutines.
	time.Sleep(50 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()

	// Assertion 4: no goroutine leak. The pool's 2 workers should be
	// reaped by Shutdown. Allow small delta for runtime-internal
	// goroutines that may have shifted across the test boundary.
	const acceptableGoroutineDelta = 4
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > acceptableGoroutineDelta {
		t.Errorf("goroutine count grew by %d (before=%d, after=%d) — possible leak",
			leaked, goroutinesBefore, goroutinesAfter)
	}
	t.Logf("LOAD_RESULT goroutines_before=%d goroutines_after=%d delta=%d",
		goroutinesBefore, goroutinesAfter, leaked)
}

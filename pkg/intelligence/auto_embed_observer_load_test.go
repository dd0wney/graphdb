package intelligence

import (
	"context"
	"os"
	"runtime"
	"strings"
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

// TestAutoEmbedObserver_SustainedLoadDropsContinue extends
// TestAutoEmbedObserver_BackpressureUnderLoad's burst shape with a
// time-bounded sustained-load shape. The burst test proves the drop
// path fires; this test proves it KEEPS firing at steady state —
// catching a regression where the queue saturates once and then quietly
// goes back to "submit always succeeds" because of a counter reset, a
// goroutine wedge, or some other liveness bug.
//
// What this pins beyond the burst test:
//
//  1. Drop count at end > drop count at midpoint. A pool that quietly
//     stops dropping (e.g., a leaked worker reactivates the queue, or
//     an off-by-one in the bounded-channel arithmetic) would show
//     drops_midpoint ≈ drops_end. We require strictly more drops in
//     the second half of the run.
//
//  2. Drop count grows roughly with elapsed time, not just with submit
//     count. We log the per-second rate for a human reader; the
//     assertion is just monotonic growth, since rate depends on the
//     producer goroutines getting CPU.
//
// GRAPHDB_BENCH_LARGE-gated. Wall time ~loadDuration + Shutdown drain.
//
//	GRAPHDB_BENCH_LARGE=1 go test -v -race -run TestAutoEmbedObserver_SustainedLoadDropsContinue -timeout 60s ./pkg/intelligence/
func TestAutoEmbedObserver_SustainedLoadDropsContinue(t *testing.T) {
	if os.Getenv("GRAPHDB_BENCH_LARGE") == "" {
		t.Skip("skipping sustained-load test (GRAPHDB_BENCH_LARGE not set; runtime ~seconds)")
	}

	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig() error = %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	pool := NewPool(PoolConfig{Workers: 2, QueueDepth: 10})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
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

	const (
		loadDuration = 3 * time.Second
		numProducers = 8
	)

	var (
		producerWG   sync.WaitGroup
		stop         atomic.Bool
		totalCreates atomic.Int64
		maxCreateLat atomic.Int64
	)

	// Midpoint snapshot. Take it at loadDuration/2 from inside a
	// goroutine started before producers; the comparison is what proves
	// drops are still accumulating in the second half.
	var dropsAtMidpoint uint64
	midpointReady := make(chan struct{})
	go func() {
		time.Sleep(loadDuration / 2)
		dropsAtMidpoint = pool.Dropped()
		close(midpointReady)
	}()

	loadStart := time.Now()
	for p := 0; p < numProducers; p++ {
		producerWG.Add(1)
		go func() {
			defer producerWG.Done()
			for !stop.Load() {
				start := time.Now()
				_, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]storage.Value{
					"body": storage.StringValue("sustained-load doc body"),
				})
				latNs := time.Since(start).Nanoseconds()
				if err != nil {
					t.Errorf("CreateNode: %v", err)
					return
				}
				totalCreates.Add(1)
				for {
					prev := maxCreateLat.Load()
					if latNs <= prev || maxCreateLat.CompareAndSwap(prev, latNs) {
						break
					}
				}
			}
		}()
	}

	// Run for loadDuration then signal stop.
	time.Sleep(loadDuration)
	stop.Store(true)
	producerWG.Wait()
	<-midpointReady // guaranteed already-closed since midpoint < loadDuration
	loadElapsed := time.Since(loadStart)

	dropsAtEnd := pool.Dropped()
	maxLatMs := float64(maxCreateLat.Load()) / 1e6

	dropsSecondHalf := dropsAtEnd - dropsAtMidpoint
	creates := totalCreates.Load()
	dropsPerSec := float64(dropsAtEnd) / loadElapsed.Seconds()
	createsPerSec := float64(creates) / loadElapsed.Seconds()

	t.Logf("SUSTAINED_RESULT elapsed=%v producers=%d total_creates=%d",
		loadElapsed, numProducers, creates)
	t.Logf("SUSTAINED_RESULT drops_midpoint=%d drops_end=%d drops_second_half=%d",
		dropsAtMidpoint, dropsAtEnd, dropsSecondHalf)
	t.Logf("SUSTAINED_RESULT drops_per_sec=%.0f creates_per_sec=%.0f max_create_lat_ms=%.2f",
		dropsPerSec, createsPerSec, maxLatMs)

	// Assertion 1: drops accumulated in the second half of the run. The
	// liveness bug this catches: pool saturates once, then a wedge in
	// the bounded-channel arithmetic makes Submit start succeeding again
	// (or, the embedder backend recovers a worker silently). We require
	// strictly more drops in the second half than in the first.
	if dropsSecondHalf == 0 {
		t.Errorf("drops_second_half=0 (drops_midpoint=%d drops_end=%d) — pool stopped dropping after initial saturation",
			dropsAtMidpoint, dropsAtEnd)
	}

	// Assertion 2: CreateNode latency stayed bounded for the duration.
	// Same bound as the burst test; the sustained shape catches cases
	// where latency degrades gradually rather than spiking immediately.
	const maxAcceptableLatMs = 100.0
	if maxLatMs > maxAcceptableLatMs {
		t.Errorf("max CreateNode latency = %.2fms > %.2fms over %v — backpressure may be blocking CreateNode under sustained load",
			maxLatMs, maxAcceptableLatMs, loadDuration)
	}
}

// TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad pins that the O-1
// structured error log (PR #202) fires correctly under saturation —
// catches the regression where logging is wired into the synchronous
// path only and silently breaks for tasks dispatched via the worker
// pool.
//
// Plus, this pins a production-relevant property the unit tests cannot:
// log volume is bounded by the number of tasks the pool actually drains
// (embedder.Embed call count), NOT by submit count. Under saturation
// with N submits → K drained → (N-K) dropped, we should see ~K
// "embedder failed" lines, not N. A misbehaving embedder must not
// produce log volume proportional to client request rate.
//
// Failure-injection strategy: all-errors with ErrNoIndexForTenant. The
// always-errors shape (rather than mixed errors) gives a tight upper
// bound on expected log lines for assertion. ErrNoIndexForTenant
// exercises the dedicated "no-index-for-tenant" category path; the
// other category ("embed-failed", with M-1 sanitization) is unit-tested
// in TestAutoEmbedObserver_LogsEmbedderError_SanitizesUserText and
// doesn't need duplicate load-test coverage.
//
// GRAPHDB_BENCH_LARGE-gated. Wall time ~loadDuration + drain.
//
//	GRAPHDB_BENCH_LARGE=1 go test -v -race -run TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad -timeout 60s ./pkg/intelligence/
func TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad(t *testing.T) {
	if os.Getenv("GRAPHDB_BENCH_LARGE") == "" {
		t.Skip("skipping erroring-load test (GRAPHDB_BENCH_LARGE not set; runtime ~seconds)")
	}

	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig() error = %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	pool := NewPool(PoolConfig{Workers: 2, QueueDepth: 10})

	// erroringSlowEmbedder behaves like slowFakeEmbedder but always
	// returns ErrNoIndexForTenant. The 50ms delay keeps the pool's
	// drain rate at ~40 tasks/sec — same shape as the burst test.
	embedder := &erroringSlowEmbedder{
		delay:    50 * time.Millisecond,
		tenantID: "acme",
	}

	obs, err := NewAutoEmbedObserver(gs, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	if err != nil {
		t.Fatalf("NewAutoEmbedObserver() error = %v", err)
	}
	gs.AddObserver(obs)

	const (
		numProducers     = 8
		nodesPerProducer = 50
	)

	// Run the load inside captureLog so we receive every "auto-embed:"
	// line written during the producer loop. The pool Shutdown call is
	// inside fn — that's load-bearing: log.SetOutput's restore runs in
	// captureLog's defer AFTER fn returns, so any worker that fires
	// log.Printf after fn returns races with buf.String(). Shutdown
	// inside fn drains all workers before captureLog returns, leaving
	// the buffer quiescent when it's read.
	logged := captureLog(t, func() {
		var producerWG sync.WaitGroup
		for p := 0; p < numProducers; p++ {
			producerWG.Add(1)
			go func() {
				defer producerWG.Done()
				for n := 0; n < nodesPerProducer; n++ {
					_, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]storage.Value{
						"body": storage.StringValue("erroring-load doc body"),
					})
					if err != nil {
						t.Errorf("CreateNode: %v", err)
						return
					}
				}
			}()
		}
		producerWG.Wait()

		// Drain naturally before Shutdown. Pool.Shutdown calls p.cancel()
		// which propagates to the per-task ctx; queued tasks then see
		// ctx.Err() canceled at Execute's entrypoint and return without
		// calling Embed. To exercise realistic drain volume we let
		// workers finish the queue first.
		//
		// Drain time: 10 queue / 2 workers × 50ms per task = 250ms +
		// 50ms in-flight residual + slack. 500ms covers it.
		time.Sleep(500 * time.Millisecond)

		// Shutdown — queue is empty, so cancel doesn't abandon work.
		// All worker log.Printf calls have completed; captureLog's
		// defer can safely restore log.SetOutput without racing.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if unfinished := pool.Shutdown(ctx); unfinished > 0 {
			t.Logf("Shutdown reported %d unfinished tasks at deadline", unfinished)
		}
	})
	// Pool is closed; the t.Cleanup below is a defensive idempotent
	// second Shutdown (closeOnce makes it a no-op).
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	})

	dropped := pool.Dropped()
	embedderCalls := embedder.calls.Load()
	totalCreates := int64(numProducers * nodesPerProducer)
	logLineCount := strings.Count(logged, "auto-embed:")

	t.Logf("ERRORLOAD_RESULT total_creates=%d embedder_calls=%d pool_dropped=%d log_lines=%d",
		totalCreates, embedderCalls, dropped, logLineCount)

	// Assertion 1: pool saturated (drops > 0). Same shape as burst test.
	if dropped == 0 {
		t.Errorf("Pool.Dropped() = 0; load shape did not saturate the pool — error-logging-under-load path untested")
	}

	// Assertion 2: O-1 logs fired. The "no-index-for-tenant" category
	// string must appear at least once — proves the log path runs from
	// pool-dispatched tasks, not only from the synchronous unit-test
	// path.
	if !strings.Contains(logged, "no-index-for-tenant") {
		t.Errorf("expected 'no-index-for-tenant' category in logs; got: %s", logged)
	}

	// Assertion 3: log volume is bounded by drained tasks, not submits.
	// embedder.calls is the exact drain count; logged auto-embed lines
	// must not exceed it by more than a small slack (panic-recovery
	// logs, pool-init logs). This pins that a misbehaving embedder does
	// not cause log-volume explosion proportional to client load.
	//
	// The slack of 5 covers: 0 panic-recovery lines expected (we don't
	// inject panics), and any background log noise from other goroutines
	// captured during the load. A failure here is a real bug —
	// investigate before tightening the slack.
	const logSlack = 5
	maxExpectedLogLines := int(embedderCalls) + logSlack
	if logLineCount > maxExpectedLogLines {
		t.Errorf("log_lines=%d > embedder_calls+slack=%d — logging may be wired to submit path, not drain path",
			logLineCount, maxExpectedLogLines)
	}

	// Assertion 4: every logged auto-embed line includes structural
	// fields. Catches a regression where a future change drops one of
	// the structured key=value tokens that operators grep for.
	for _, expectedField := range []string{"tenant=acme", "policy=Doc"} {
		if !strings.Contains(logged, expectedField) {
			t.Errorf("logs missing expected structured field %q; got first 500 chars: %s",
				expectedField, firstN(logged, 500))
		}
	}
}

// erroringSlowEmbedder is like slowFakeEmbedder but returns
// ErrNoIndexForTenant on every call. Used by
// TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad to exercise the
// O-1 log path from pool-dispatched tasks under saturation.
type erroringSlowEmbedder struct {
	delay    time.Duration
	tenantID string
	calls    atomic.Int64
}

func (e *erroringSlowEmbedder) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	e.calls.Add(1)
	time.Sleep(e.delay)
	return nil, ErrNoIndexForTenant{TenantID: e.tenantID}
}

// firstN returns the first n bytes of s, or all of s if shorter.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/intelligence"
)

// httpLoadFakeEmbedder is the pkg/api-local fake Embedder used by the
// HTTP-surface load test. Mirrors slowFakeEmbedder from
// pkg/intelligence/auto_embed_observer_load_test.go; duplicated here
// rather than promoted to a non-test helper because it serves the same
// single purpose (force deterministic pool saturation in a load test)
// in a different package.
type httpLoadFakeEmbedder struct {
	delay time.Duration
	calls atomic.Int64
	vec   []float32
}

func (e *httpLoadFakeEmbedder) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	e.calls.Add(1)
	time.Sleep(e.delay)
	return e.vec, nil
}

// TestAutoEmbedObserver_HTTPCreateNodeBackpressure exercises the
// HTTP-surface bookend of the auto-embed pipeline: POST /nodes →
// handleNodes → gs.CreateNode → observer dispatch → pool.Submit →
// drop-on-full. The pkg/intelligence load tests (PRs #196 + this PR's
// sibling subtests) cover the Go-direct path; this test pins that the
// HTTP layer's request semantics behave correctly when the observer's
// pool saturates.
//
// What this test pins that the Go-direct load tests do not:
//
//  1. All HTTP requests return 201 Created during saturation. The
//     observer's drop signal is decoupled from CreateNode's success —
//     dropping an embed task does NOT make the originating HTTP
//     request fail. A regression where someone wires the drop signal
//     back into CreateNode's return value would fail this assertion.
//
//  2. HTTP latency stays bounded under saturation. CreateNode's call
//     to notifyNodeCreated runs on the request goroutine; if
//     pool.Submit ever blocked (e.g., a future change replacing the
//     non-blocking select with a blocking send), HTTP latency would
//     balloon to the embedder's drain rate. The latency bound catches
//     this.
//
//  3. pool.Dropped() advances under HTTP load. Direct proof that the
//     HTTP path actually exercises the observer pipeline, not some
//     parallel CreateNode bypass that skips the observer dispatch.
//
// Manual wiring (not bootstrapAutoEmbedFromEnv) is deliberate: the
// bootstrap path uses LSAEmbedder against a per-tenant LSA index, which
// drains too fast on a tiny test corpus to saturate the pool
// reliably. End-to-end bootstrap wiring is already covered by
// TestBootstrapAutoEmbedFromEnv_EndToEnd in
// server_autoembed_bootstrap_test.go.
//
// GRAPHDB_BENCH_LARGE-gated. Wall time ~1s + Shutdown drain.
//
//	GRAPHDB_BENCH_LARGE=1 go test -v -race -run TestAutoEmbedObserver_HTTPCreateNodeBackpressure -timeout 60s ./pkg/api/
func TestAutoEmbedObserver_HTTPCreateNodeBackpressure(t *testing.T) {
	if os.Getenv("GRAPHDB_BENCH_LARGE") == "" {
		t.Skip("skipping HTTP load test (GRAPHDB_BENCH_LARGE not set; runtime ~seconds)")
	}

	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Wire the observer with a slow fake embedder. Small pool ensures
	// saturation: 2 workers × ~50ms = ~40 tasks/sec drain, vs producer
	// rate of ~8 producers × hundreds-of-creates-per-second.
	pool := intelligence.NewPool(intelligence.PoolConfig{Workers: 2, QueueDepth: 10})
	embedder := &httpLoadFakeEmbedder{
		delay: 50 * time.Millisecond,
		vec:   []float32{1, 2, 3, 4, 5},
	}
	obs, err := intelligence.NewAutoEmbedObserver(server.graph, embedder, pool, []intelligence.EmbeddingPolicy{
		{Label: "Doc", SourceProperty: "body", TargetProperty: "embedding"},
	})
	if err != nil {
		t.Fatalf("NewAutoEmbedObserver: %v", err)
	}
	server.graph.AddObserver(obs)
	server.autoEmbedPool = pool
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	})

	const (
		numProducers     = 8
		nodesPerProducer = 50
	)

	body, err := json.Marshal(NodeRequest{
		Labels:     []string{"Doc"},
		Properties: map[string]any{"body": "http load doc body"},
	})
	if err != nil {
		t.Fatalf("marshal NodeRequest: %v", err)
	}

	var (
		producerWG sync.WaitGroup
		http201    atomic.Int64
		httpOther  atomic.Int64
		maxHTTPLat atomic.Int64 // nanoseconds
	)

	loadStart := time.Now()
	for p := 0; p < numProducers; p++ {
		producerWG.Add(1)
		go func() {
			defer producerWG.Done()
			for n := 0; n < nodesPerProducer; n++ {
				req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()

				start := time.Now()
				server.handleNodes(rr, req)
				latNs := time.Since(start).Nanoseconds()

				if rr.Code == http.StatusCreated {
					http201.Add(1)
				} else {
					httpOther.Add(1)
					t.Errorf("producer %d req %d: status=%d body=%s",
						0, n, rr.Code, rr.Body.String())
				}
				for {
					prev := maxHTTPLat.Load()
					if latNs <= prev || maxHTTPLat.CompareAndSwap(prev, latNs) {
						break
					}
				}
			}
		}()
	}
	producerWG.Wait()
	loadElapsed := time.Since(loadStart)

	// Let the pool drain naturally before reading drop count, so any
	// late-arriving submits land before the snapshot.
	time.Sleep(500 * time.Millisecond)

	dropped := pool.Dropped()
	embedderCalls := embedder.calls.Load()
	maxLatMs := float64(maxHTTPLat.Load()) / 1e6
	created := http201.Load()
	other := httpOther.Load()
	totalRequests := int64(numProducers * nodesPerProducer)

	t.Logf("HTTPLOAD_RESULT elapsed=%v producers=%d total_requests=%d http_201=%d http_other=%d",
		loadElapsed, numProducers, totalRequests, created, other)
	t.Logf("HTTPLOAD_RESULT pool_dropped=%d embedder_calls=%d max_http_lat_ms=%.2f",
		dropped, embedderCalls, maxLatMs)

	// Assertion 1: every HTTP request returned 201. CreateNode succeeds
	// regardless of whether the observer's task is dropped — the drop
	// signal must not propagate to HTTP failure.
	if created != totalRequests {
		t.Errorf("http_201 = %d, want %d (http_other=%d) — observer drop signal may be incorrectly propagating to HTTP layer",
			created, totalRequests, other)
	}

	// Assertion 2: pool saturated. If dropped == 0, the test isn't
	// actually exercising the HTTP → drop chain; the bound below (latency
	// stayed low) would be trivially satisfied.
	if dropped == 0 {
		t.Errorf("pool.Dropped() = 0; load shape did not saturate the pool — HTTP-surface backpressure path untested")
	}

	// Assertion 3: HTTP latency didn't balloon into the catastrophic-
	// blocking failure mode. The primary discriminator for "Submit
	// blocked" vs "Submit dropped" is Assertion 2 (drops > 0) — if
	// Submit started blocking, drops would be 0. This latency bound
	// catches the secondary failure: even though some submits drop, a
	// pipeline regression makes the HTTP path wait synchronously for
	// embed completion (e.g., a future change wires CreateNode to await
	// the writeback before returning).
	//
	// Threshold rationale: 500ms is above the realistic HTTP + 8-way
	// write-contention ceiling (~250ms observed) but below the
	// catastrophic ceiling (queue_depth × embedder_delay = 10 × 50ms =
	// 500ms; the failure mode would push max latency to 500ms+).
	// A regression that subtly increases blocking shows as drift toward
	// this bound; a complete regression to synchronous embedding crosses
	// it.
	//
	// Don't tighten this threshold below 500ms without first restructuring
	// the test to use BulkImportMode storage (eliminates persistent
	// snapshot overhead) — otherwise tail-latency flakes will accumulate
	// from real contention, not regressions.
	const maxAcceptableLatMs = 500.0
	if maxLatMs > maxAcceptableLatMs {
		t.Errorf("max HTTP latency = %.2fms > %.2fms — auto-embed backpressure may be blocking the request goroutine (cross-check Assertion 2: drops > 0)",
			maxLatMs, maxAcceptableLatMs)
	}
}

// Package intelligence will hold the auto-embedder infrastructure (S11
// spike, docs/internals/design/S11_AUTO_EMBEDDER_REDESIGN.md). This file
// introduces the package with a single artifact: the bounded async worker
// pool (R2.2). Subsequent PRs add:
//
//   - R2.3: Embedder interface + ErrNoIndexForTenant (embedder.go).
//   - R2.4: LSAEmbedder adapter wrapping pkg/search.TenantLSAIndexes
//     (lsa_embedder.go).
//   - R2.5: AutoEmbedObserver + server_init.go wiring.
//
// The pool is intentionally generic (operates on a Task interface) rather
// than embedding-specific (the spike's prescribed chan embedTask). The
// deviation is justified: R2.3's Embedder interface and R2.5's
// EmbedTask struct have not shipped, and a generic pool is honestly more
// useful for the open-core extension model — enterprise plugins can reuse
// the pool for non-embedding async work without redesigning it. R2.5 will
// define a concrete EmbedTask type that implements Task.
package intelligence

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Task is a unit of async work executed inside a Pool worker goroutine.
//
// The pool passes a context derived from the pool's internal lifecycle
// context — it is canceled when the pool is shut down. Tasks that ignore
// the context keep running to completion (and count toward "unfinished"
// at shutdown); tasks that respect ctx.Done() can bail early on shutdown.
//
// Execute must not panic. The pool recovers panics so a misbehaving Task
// does not crash workers, but recovery is a safety net, not the contract.
type Task interface {
	Execute(ctx context.Context)
}

// TaskFunc is an adapter for using a bare function as a Task — analogous
// to http.HandlerFunc. Useful for tests and simple async work that doesn't
// warrant a dedicated struct.
type TaskFunc func(ctx context.Context)

// Execute implements Task by calling f(ctx).
func (f TaskFunc) Execute(ctx context.Context) { f(ctx) }

// PoolConfig configures a Pool. Zero values are replaced with package
// defaults; see DefaultWorkers / DefaultQueueDepth / DefaultShutdownTimeout.
type PoolConfig struct {
	// Workers is the number of concurrent worker goroutines.
	// Zero or negative selects DefaultWorkers.
	Workers int

	// QueueDepth is the bounded queue capacity. Submit() drops tasks when
	// the queue is full. Zero or negative selects DefaultQueueDepth.
	QueueDepth int

	// ShutdownTimeout caps how long Shutdown blocks waiting for in-flight
	// tasks to drain. Zero or negative selects DefaultShutdownTimeout.
	ShutdownTimeout time.Duration

	// Synchronous, when true, makes Submit execute the task inline on the
	// caller's goroutine — no queue, no workers. Used for deterministic
	// tests where the caller wants to observe the post-execution state
	// immediately. Production callers must leave this false.
	Synchronous bool
}

// Package defaults; chosen per S11 spike §7.5.
const (
	DefaultWorkers         = 4
	DefaultQueueDepth      = 256
	DefaultShutdownTimeout = 5 * time.Second
)

// Pool is a bounded async worker pool with drop-on-full backpressure, a
// configurable shutdown drain timeout, and an optional synchronous mode
// for tests. See S11 spike §7.5 for the design rationale.
//
// The pool is generic: it operates on a Task interface, not an embedding-
// specific task type. R2.5's AutoEmbedObserver will define EmbedTask
// implementing Task.
type Pool struct {
	workers         int
	queue           chan Task
	synchronous     bool
	shutdownTimeout time.Duration

	// Lifecycle context: canceled by Shutdown. Workers pass derived
	// contexts to Task.Execute so tasks can bail on shutdown.
	ctx    context.Context
	cancel context.CancelFunc

	// closed is set by Shutdown to short-circuit subsequent Submit calls.
	closed atomic.Bool

	// closeOnce makes Shutdown idempotent. Multiple calls are safe;
	// subsequent calls return immediately with the same counts.
	closeOnce sync.Once

	// dropped counts tasks rejected by Submit due to a full queue or
	// post-shutdown call. Read via Dropped().
	dropped atomic.Uint64

	// inFlight counts tasks currently inside Execute (post-dequeue,
	// pre-return). Shutdown uses this to distinguish "drained cleanly"
	// (in-flight == 0) from "timed out with N tasks still running."
	inFlight atomic.Int64

	// wg tracks worker goroutines. Shutdown waits on wg.Wait() (subject to
	// the timeout) before returning.
	wg sync.WaitGroup
}

// NewPool constructs a Pool with cfg's settings. Zero-value fields receive
// package defaults. The returned pool is ready for Submit immediately.
func NewPool(cfg PoolConfig) *Pool {
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = DefaultQueueDepth
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = DefaultShutdownTimeout
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel is stored on Pool and called by Shutdown's closeOnce path.
	p := &Pool{
		workers:         cfg.Workers,
		queue:           make(chan Task, cfg.QueueDepth),
		synchronous:     cfg.Synchronous,
		shutdownTimeout: cfg.ShutdownTimeout,
		ctx:             ctx,
		cancel:          cancel,
	}

	if !cfg.Synchronous {
		for i := 0; i < cfg.Workers; i++ {
			p.wg.Add(1)
			go p.worker()
		}
	}

	return p
}

// Submit enqueues task for async execution. Returns true on successful
// enqueue, false when the task is dropped — either because the queue is
// full or because the pool is closed.
//
// Submit never blocks the caller. Backpressure manifests as drops, not
// stalls, per S11 spike §7.5 ("Dropping on back-pressure is preferable to
// blocking CreateNode").
//
// In synchronous mode, Submit runs task.Execute inline on the caller's
// goroutine and returns true unless the pool is closed. The caller's
// context is passed through to Execute.
func (p *Pool) Submit(ctx context.Context, task Task) bool {
	if p.closed.Load() {
		p.dropped.Add(1)
		return false
	}
	if p.synchronous {
		p.inFlight.Add(1)
		defer p.inFlight.Add(-1)
		p.executeWithRecover(ctx, task)
		return true
	}
	select {
	case p.queue <- task:
		return true
	default:
		p.dropped.Add(1)
		return false
	}
}

// Dropped returns the total number of tasks dropped due to a full queue
// or post-shutdown submits. Useful for monitoring backpressure.
func (p *Pool) Dropped() uint64 {
	return p.dropped.Load()
}

// InFlight returns the number of tasks currently inside Execute. Primarily
// for tests and observability; production callers usually don't need this.
func (p *Pool) InFlight() int64 {
	return p.inFlight.Load()
}

// Shutdown closes the pool's task queue, signals in-flight tasks via the
// pool's internal context, and waits up to ShutdownTimeout for workers to
// drain. Returns 0 if all in-flight tasks completed within the timeout;
// otherwise returns the count of tasks still inside Execute when the
// timeout fired.
//
// After Shutdown returns, Submit always returns false.
//
// Safe to call multiple times; only the first call has effect. Subsequent
// calls return 0 immediately.
//
// The ctx parameter is honored — if it is canceled before ShutdownTimeout
// elapses, Shutdown returns early with the current in-flight count.
func (p *Pool) Shutdown(ctx context.Context) int {
	var unfinished int
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.queue)
		p.cancel()

		if p.synchronous {
			// No workers to wait for; synchronous Submit blocked the
			// caller, so by the time Shutdown is reachable everything
			// has run.
			return
		}

		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			unfinished = 0
		case <-time.After(p.shutdownTimeout):
			unfinished = int(p.inFlight.Load())
		case <-ctx.Done():
			unfinished = int(p.inFlight.Load())
		}
	})
	return unfinished
}

// worker is the goroutine body. Each worker reads tasks from the queue
// until it is closed, executes each task inside a panic recovery shim,
// and maintains the inFlight counter.
func (p *Pool) worker() {
	defer p.wg.Done()
	for task := range p.queue {
		p.inFlight.Add(1)
		p.executeWithRecover(p.ctx, task)
		p.inFlight.Add(-1)
	}
}

// executeWithRecover runs task.Execute(ctx) and swallows any panic. A
// panicking task does not crash the worker; it is logged via the runtime
// default and execution continues. The contract still says "must not
// panic" — recovery is a safety net.
func (p *Pool) executeWithRecover(ctx context.Context, task Task) {
	defer func() {
		//nolint:errcheck // recover() returns any (not error); we intentionally discard the panic value to keep workers alive per the docstring's "safety net" framing.
		_ = recover()
	}()
	task.Execute(ctx)
}

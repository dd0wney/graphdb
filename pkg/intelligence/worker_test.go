package intelligence

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPoolSubmitExecutesAsync pins the happy path: Submit enqueues, a
// worker dequeues and executes, the task observably ran.
func TestPoolSubmitExecutesAsync(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 2, QueueDepth: 8})
	defer pool.Shutdown(context.Background())

	var ran atomic.Bool
	done := make(chan struct{})
	ok := pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		ran.Store(true)
		close(done)
	}))
	if !ok {
		t.Fatal("Submit returned false on empty queue")
	}

	select {
	case <-done:
		// task ran
	case <-time.After(2 * time.Second):
		t.Fatal("task did not execute within 2 seconds")
	}

	if !ran.Load() {
		t.Error("task signaled done but did not record execution")
	}
}

// TestPoolSubmitReturnsFalseAfterShutdown pins that post-shutdown Submits
// drop with the dropped counter bumped, not panic or block.
func TestPoolSubmitReturnsFalseAfterShutdown(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueDepth: 1})
	pool.Shutdown(context.Background())

	ok := pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {}))
	if ok {
		t.Error("Submit returned true after Shutdown, want false")
	}
	if got := pool.Dropped(); got != 1 {
		t.Errorf("Dropped() = %d after post-shutdown submit, want 1", got)
	}
}

// TestPoolBackpressureDrop pins spike T7: a full queue causes drops, not
// blocking. Single worker + queueDepth=1; a held task occupies the worker,
// a second task fills the queue, a third task drops.
func TestPoolBackpressureDrop(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueDepth: 1})
	defer pool.Shutdown(context.Background())

	release := make(chan struct{})
	taskStarted := make(chan struct{})

	// Task 1: occupies the single worker; blocks until release closes.
	ok := pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		close(taskStarted)
		<-release
	}))
	if !ok {
		t.Fatalf("Submit task1 returned false")
	}
	<-taskStarted // worker is now blocked inside Execute

	// Task 2: lands in the queue (capacity 1).
	ok = pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {}))
	if !ok {
		t.Fatalf("Submit task2 returned false; queue should have one slot")
	}

	// Task 3: drops — queue is full and the worker is busy.
	submitStart := time.Now()
	ok = pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {}))
	submitElapsed := time.Since(submitStart)
	if ok {
		t.Errorf("Submit task3 returned true; want false (drop)")
	}
	if submitElapsed > 50*time.Millisecond {
		t.Errorf("Submit task3 took %v; want non-blocking (<50ms)", submitElapsed)
	}
	if got := pool.Dropped(); got != 1 {
		t.Errorf("Dropped() = %d, want 1", got)
	}

	close(release)
}

// TestPoolShutdownDrain pins spike T6: Shutdown waits for in-flight tasks
// to complete. Submit N tasks, each incrementing a counter, then Shutdown
// — the counter must reach N (no in-flight task is interrupted).
func TestPoolShutdownDrain(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 2, QueueDepth: 16})

	const N = 10
	var executed atomic.Int64
	for i := 0; i < N; i++ {
		ok := pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
			time.Sleep(10 * time.Millisecond)
			executed.Add(1)
		}))
		if !ok {
			t.Fatalf("Submit task %d returned false", i)
		}
	}

	unfinished := pool.Shutdown(context.Background())
	if unfinished != 0 {
		t.Errorf("Shutdown unfinished = %d, want 0", unfinished)
	}
	if got := executed.Load(); got != N {
		t.Errorf("executed = %d, want %d", got, N)
	}
}

// TestPoolShutdownTimeout pins the timeout behavior: when in-flight tasks
// exceed ShutdownTimeout, Shutdown returns a positive unfinished count.
// Uses a deliberately-long-running task with a very short timeout.
func TestPoolShutdownTimeout(t *testing.T) {
	pool := NewPool(PoolConfig{
		Workers:         1,
		QueueDepth:      1,
		ShutdownTimeout: 30 * time.Millisecond,
	})

	taskStarted := make(chan struct{})
	taskDone := make(chan struct{})
	ok := pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		close(taskStarted)
		time.Sleep(500 * time.Millisecond)
		close(taskDone)
	}))
	if !ok {
		t.Fatalf("Submit returned false")
	}
	<-taskStarted

	unfinished := pool.Shutdown(context.Background())
	if unfinished == 0 {
		t.Errorf("Shutdown unfinished = 0, want >0 (long task should have been still running)")
	}

	// Let the task finish so the goroutine doesn't leak past the test.
	<-taskDone
}

// TestPoolShutdownCtxCancel pins that Shutdown honors its ctx argument:
// canceling ctx before ShutdownTimeout elapses returns early.
func TestPoolShutdownCtxCancel(t *testing.T) {
	pool := NewPool(PoolConfig{
		Workers:         1,
		QueueDepth:      1,
		ShutdownTimeout: 10 * time.Second, // long timeout
	})

	taskStarted := make(chan struct{})
	taskDone := make(chan struct{})
	pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		close(taskStarted)
		time.Sleep(500 * time.Millisecond)
		close(taskDone)
	}))
	<-taskStarted

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	unfinished := pool.Shutdown(ctx)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("Shutdown took %v; ctx cancel should have returned early (well under 200ms)", elapsed)
	}
	if unfinished == 0 {
		t.Errorf("unfinished = 0; ctx cancel during in-flight task should have returned a positive count")
	}

	<-taskDone
}

// TestPoolShutdownIdempotent pins that multiple Shutdown calls are safe.
func TestPoolShutdownIdempotent(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueDepth: 1})

	if got := pool.Shutdown(context.Background()); got != 0 {
		t.Errorf("first Shutdown unfinished = %d, want 0", got)
	}
	if got := pool.Shutdown(context.Background()); got != 0 {
		t.Errorf("second Shutdown unfinished = %d, want 0", got)
	}
	if got := pool.Shutdown(context.Background()); got != 0 {
		t.Errorf("third Shutdown unfinished = %d, want 0", got)
	}
}

// TestPoolSynchronousMode pins that synchronous mode runs Submit inline
// on the caller's goroutine and observes post-execution state before
// returning.
func TestPoolSynchronousMode(t *testing.T) {
	pool := NewPool(PoolConfig{Synchronous: true})
	defer pool.Shutdown(context.Background())

	var ran atomic.Bool
	ok := pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		ran.Store(true)
	}))
	if !ok {
		t.Error("synchronous Submit returned false")
	}
	if !ran.Load() {
		t.Error("synchronous Submit returned before task ran")
	}
}

// TestPoolPanicRecovery pins that a panicking task does not crash the
// worker — subsequent Submits still execute normally.
func TestPoolPanicRecovery(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueDepth: 4})
	defer pool.Shutdown(context.Background())

	pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		panic("intentional test panic")
	}))

	// Give the panic time to be recovered and the worker to come back.
	// We can't busy-wait deterministically without observability into the
	// worker; a brief sleep + a follow-up task is the cleanest contract test.
	time.Sleep(20 * time.Millisecond)

	var ran atomic.Bool
	done := make(chan struct{})
	pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
		ran.Store(true)
		close(done)
	}))

	select {
	case <-done:
		if !ran.Load() {
			t.Error("post-panic task signaled done but did not record execution")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-panic task did not execute; worker crashed?")
	}
}

// TestPoolPanicRecovery_LogsPanicValue pins the audit-O-1 observability
// contract for panic recovery: the recovered value MUST surface in the
// operator log stream so a silent panic doesn't masquerade as "production
// is fine but losing work." Synchronous mode keeps the assertion
// deterministic — the panic recovers on the caller's goroutine.
func TestPoolPanicRecovery_LogsPanicValue(t *testing.T) {
	pool := NewPool(PoolConfig{Synchronous: true})
	defer pool.Shutdown(context.Background())

	out := captureLog(t, func() {
		pool.Submit(context.Background(), TaskFunc(func(_ context.Context) {
			panic("intentional test panic")
		}))
	})

	if !contains(out, "worker recovered from task panic") {
		t.Errorf("log should record the recovery event; got: %s", out)
	}
	if !contains(out, "intentional test panic") {
		t.Errorf("log should carry the panic value (Go runtime errors here, not user input by contract); got: %s", out)
	}
}

// TestPoolDefaults pins that zero-value PoolConfig fields receive the
// documented defaults.
func TestPoolDefaults(t *testing.T) {
	pool := NewPool(PoolConfig{})
	defer pool.Shutdown(context.Background())

	if pool.workers != DefaultWorkers {
		t.Errorf("workers = %d, want %d", pool.workers, DefaultWorkers)
	}
	if cap(pool.queue) != DefaultQueueDepth {
		t.Errorf("queue capacity = %d, want %d", cap(pool.queue), DefaultQueueDepth)
	}
	if pool.shutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("shutdownTimeout = %v, want %v", pool.shutdownTimeout, DefaultShutdownTimeout)
	}
}

// TestPoolCtxCancelReachesTask pins that the context passed to Task.Execute
// is canceled by Shutdown — tasks that respect ctx.Done() can bail.
func TestPoolCtxCancelReachesTask(t *testing.T) {
	pool := NewPool(PoolConfig{Workers: 1, QueueDepth: 2, ShutdownTimeout: 2 * time.Second})

	saw := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	pool.Submit(context.Background(), TaskFunc(func(taskCtx context.Context) {
		defer wg.Done()
		select {
		case <-taskCtx.Done():
			close(saw)
		case <-time.After(3 * time.Second):
			// Failed: ctx never canceled.
		}
	}))

	// Give the task time to enter the select.
	time.Sleep(50 * time.Millisecond)

	unfinished := pool.Shutdown(context.Background())

	select {
	case <-saw:
		// Task observed cancellation — Shutdown propagated to Execute's ctx.
	case <-time.After(1 * time.Second):
		t.Fatal("task did not observe ctx cancellation after Shutdown")
	}

	wg.Wait()
	// We don't assert unfinished here; the task ran to completion after
	// observing the cancel, so unfinished may be 0 OR positive depending
	// on exact timing of close(done) vs wg.Wait() inside Shutdown. The
	// load-bearing assertion is just that the task saw Done().
	_ = unfinished
}

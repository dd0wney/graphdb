package query

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

const (
	// DefaultTaskTimeout is the default timeout for individual task execution
	DefaultTaskTimeout = 30 * time.Second
	// MinTaskTimeout is the minimum allowed task timeout
	MinTaskTimeout = 1 * time.Second
)

// WorkerPool manages a pool of worker goroutines for parallel query execution
type WorkerPool struct {
	workers     int
	taskQueue   chan Task
	results     chan TaskResult
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	taskTimeout time.Duration // Timeout for individual task execution

	// Statistics
	tasksProcessed int64
	tasksActive    int64
	tasksTimedOut  int64 // Tasks that timed out
}

// Task represents a unit of work
type Task interface {
	Execute(graph *storage.GraphStorage) (any, error)
	ID() string
}

// TaskResult contains the result of a task execution
type TaskResult struct {
	TaskID   string
	Result   any
	Error    error
	TimedOut bool          // True if task was cancelled due to timeout
	Duration time.Duration // How long the task took
}

// NewWorkerPool creates a worker pool with specified number of workers
func NewWorkerPool(workers int) *WorkerPool {
	return NewWorkerPoolWithTimeout(workers, DefaultTaskTimeout)
}

// NewWorkerPoolWithTimeout creates a worker pool with custom task timeout
func NewWorkerPoolWithTimeout(workers int, taskTimeout time.Duration) *WorkerPool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	ctx, cancel := context.WithCancel(context.Background())
	validatedTimeout := ValidateTaskTimeout(taskTimeout)

	return &WorkerPool{
		workers:     workers,
		taskQueue:   make(chan Task, workers*10), // Buffered queue
		results:     make(chan TaskResult, workers*10),
		ctx:         ctx,
		cancel:      cancel,
		taskTimeout: validatedTimeout,
	}
}

// Start starts the worker pool
func (wp *WorkerPool) Start(graph *storage.GraphStorage) {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i, graph)
	}
}

// worker processes tasks from the queue
//
// Concurrent Safety:
// 1. Multiple workers run concurrently, each processing from shared taskQueue
// 2. Uses atomic operations for tasksActive and tasksProcessed counters
// 3. Select statement prevents blocking on context cancellation
// 4. Safe to call from multiple goroutines (started by Start())
// 5. Includes panic recovery to prevent worker crashes from bringing down pool
// 6. Task timeout prevents hung tasks from blocking workers indefinitely
//
// Concurrent Edge Cases:
// 1. taskQueue closure signals shutdown - worker exits gracefully
// 2. Context cancellation during task execution - worker stops sending results
// 3. Results channel may block if consumer is slow - handled by select/context
// 4. Task panic is caught and converted to error result - worker continues
// 5. Task timeout triggers cancellation and error result
func (wp *WorkerPool) worker(id int, graph *storage.GraphStorage) {
	defer wp.wg.Done()

	for {
		select {
		case task, ok := <-wp.taskQueue:
			if !ok {
				return
			}

			atomic.AddInt64(&wp.tasksActive, 1)
			startTime := time.Now()

			// Execute task with timeout and panic recovery
			result, err, timedOut := wp.executeTaskWithTimeout(task, graph)
			duration := time.Since(startTime)

			atomic.AddInt64(&wp.tasksActive, -1)
			atomic.AddInt64(&wp.tasksProcessed, 1)
			if timedOut {
				atomic.AddInt64(&wp.tasksTimedOut, 1)
			}

			select {
			case wp.results <- TaskResult{
				TaskID:   task.ID(),
				Result:   result,
				Error:    err,
				TimedOut: timedOut,
				Duration: duration,
			}:
			case <-wp.ctx.Done():
				return
			}

		case <-wp.ctx.Done():
			return
		}
	}
}

// executeTaskWithTimeout executes a task with timeout and panic recovery
// Returns (result, error, timedOut)
//
// Note on goroutine lifecycle: When a timeout occurs, the spawned goroutine
// may continue running until the task completes. The result channel is buffered
// to allow the goroutine to exit cleanly without blocking. For tasks that support
// context cancellation, consider using ExecuteWithContext instead.
func (wp *WorkerPool) executeTaskWithTimeout(task Task, graph *storage.GraphStorage) (any, error, bool) {
	// Create a channel for the result - buffered to prevent goroutine leak
	// The buffer allows the goroutine to send its result even if we've already
	// returned due to timeout, preventing it from blocking forever
	type taskResult struct {
		result any
		err    error
	}
	resultChan := make(chan taskResult, 1)

	// Create a done channel to signal the goroutine to stop if possible
	done := make(chan struct{})

	// Run task in goroutine with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Printf("PANIC in worker pool task %s: %v\n%s", task.ID(), r, stack)
				// Use select to avoid blocking if parent has moved on
				select {
				case resultChan <- taskResult{nil, fmt.Errorf("task %s panicked: %v", task.ID(), r)}:
				case <-done:
				}
			}
		}()

		result, err := task.Execute(graph)
		// Use select to avoid blocking if parent has moved on due to timeout
		select {
		case resultChan <- taskResult{result, err}:
		case <-done:
			// Parent timed out or cancelled, discard result
			log.Printf("Task %s completed after timeout/cancel, result discarded", task.ID())
		}
	}()

	// Wait for result or timeout
	select {
	case res := <-resultChan:
		close(done) // Signal goroutine we're done (in case of race)
		return res.result, res.err, false
	case <-time.After(wp.taskTimeout):
		close(done) // Signal goroutine to not block on send
		log.Printf("WARNING: Task %s timed out after %v", task.ID(), wp.taskTimeout)
		return nil, fmt.Errorf("task %s timed out after %v", task.ID(), wp.taskTimeout), true
	case <-wp.ctx.Done():
		close(done) // Signal goroutine to not block on send
		return nil, wp.ctx.Err(), false
	}
}

// Submit submits a task for execution
func (wp *WorkerPool) Submit(task Task) error {
	select {
	case wp.taskQueue <- task:
		return nil
	case <-wp.ctx.Done():
		return wp.ctx.Err()
	}
}

// Results returns the results channel
func (wp *WorkerPool) Results() <-chan TaskResult {
	return wp.results
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop() {
	wp.cancel() // Cancel context first to signal workers to stop
	close(wp.taskQueue)
	wp.wg.Wait()
	close(wp.results)
}

// WorkerPoolStats contains detailed worker pool statistics
type WorkerPoolStats struct {
	Processed   int64
	Active      int64
	TimedOut    int64
	TaskTimeout time.Duration
	Workers     int
}

// Stats returns pool statistics
func (wp *WorkerPool) Stats() (processed, active int64) {
	return atomic.LoadInt64(&wp.tasksProcessed), atomic.LoadInt64(&wp.tasksActive)
}

// DetailedStats returns detailed pool statistics
func (wp *WorkerPool) DetailedStats() WorkerPoolStats {
	return WorkerPoolStats{
		Processed:   atomic.LoadInt64(&wp.tasksProcessed),
		Active:      atomic.LoadInt64(&wp.tasksActive),
		TimedOut:    atomic.LoadInt64(&wp.tasksTimedOut),
		TaskTimeout: wp.taskTimeout,
		Workers:     wp.workers,
	}
}

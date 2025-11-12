package parallel

import (
	"fmt"
	"math"
	"sync"
)

// WorkerPool manages a pool of worker goroutines
type WorkerPool struct {
	workers   int
	taskQueue chan func()
	wg        sync.WaitGroup
	once      sync.Once
	mu        sync.RWMutex // Protects taskQueue from concurrent close during send
	closed    bool         // Protected by mu
}

// NewWorkerPool creates a new worker pool with specified number of workers
func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = 1
	}

	// Prevent overflow in buffer size calculation
	const maxWorkers = math.MaxInt / 2
	if workers > maxWorkers {
		panic(fmt.Sprintf("worker count %d exceeds maximum %d", workers, maxWorkers))
	}

	pool := &WorkerPool{
		workers:   workers,
		taskQueue: make(chan func(), workers*2), // Buffer for 2x workers
	}

	pool.start()
	return pool
}

// start initializes the worker goroutines
func (wp *WorkerPool) start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// worker processes tasks from the queue
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for task := range wp.taskQueue {
		// Recover from panics in tasks to prevent worker crash
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Log panic but don't crash the worker
					fmt.Printf("Worker panic recovered: %v\n", r)
				}
			}()
			task()
		}()
	}
}

// Submit adds a task to the worker pool
// Returns false if the pool is closed, true if task was submitted
func (wp *WorkerPool) Submit(task func()) bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	// Check if pool is closed while holding read lock
	if wp.closed {
		return false
	}

	// Safe to send because we hold the lock and pool is not closed
	wp.taskQueue <- task
	return true
}

// Close shuts down the worker pool
func (wp *WorkerPool) Close() {
	wp.once.Do(func() {
		// Acquire write lock before closing
		wp.mu.Lock()
		wp.closed = true
		close(wp.taskQueue)
		wp.mu.Unlock()
	})
	wp.wg.Wait()
}

// Wait waits for all submitted tasks to complete
func (wp *WorkerPool) Wait() {
	// Close the queue and wait for workers to finish
	wp.Close()
}

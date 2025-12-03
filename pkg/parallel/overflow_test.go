package parallel

import (
	"math"
	"testing"
)

func TestWorkerPoolOverflow(t *testing.T) {
	// Test that extremely large worker counts are rejected with an error
	_, err := NewWorkerPool(math.MaxInt)
	if err == nil {
		t.Error("Expected error for too many workers")
	}
}

func TestWorkerPoolReasonableSize(t *testing.T) {
	// Test that reasonable worker counts work
	testCases := []int{1, 10, 100, 1000, 10000}

	for _, workers := range testCases {
		pool, _ := NewWorkerPool(workers)
		if pool.workers != workers {
			t.Errorf("Expected %d workers, got %d", workers, pool.workers)
		}
		pool.Close()
	}
}

func TestWorkerPoolZeroWorkers(t *testing.T) {
	// Zero workers should default to 1
	pool, _ := NewWorkerPool(0)
	if pool.workers != 1 {
		t.Errorf("Expected 1 worker for zero input, got %d", pool.workers)
	}
	pool.Close()
}

func TestWorkerPoolNegativeWorkers(t *testing.T) {
	// Negative workers should default to 1
	pool, _ := NewWorkerPool(-5)
	if pool.workers != 1 {
		t.Errorf("Expected 1 worker for negative input, got %d", pool.workers)
	}
	pool.Close()
}

func TestWorkerPoolMaxSafe(t *testing.T) {
	// Test a large but realistic worker count
	// math.MaxInt / 2 would pass our check but Go runtime can't allocate
	// a channel buffer that large, so test with a large but realistic value
	largeWorkers := 1000000

	pool, _ := NewWorkerPool(largeWorkers)
	if pool.workers != largeWorkers {
		t.Errorf("Expected %d workers, got %d", largeWorkers, pool.workers)
	}

	// Verify buffer size doesn't overflow
	// Buffer should be workers * 2
	expectedBuffer := largeWorkers * 2
	if cap(pool.taskQueue) != expectedBuffer {
		t.Errorf("Expected buffer capacity %d, got %d", expectedBuffer, cap(pool.taskQueue))
	}

	pool.Close()
}

func TestWorkerPoolSubmitAndExecute(t *testing.T) {
	pool, _ := NewWorkerPool(4)
	defer pool.Close()

	// Test that tasks are executed
	executed := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			executed <- true
		})
	}

	// Wait for all tasks
	pool.Close()

	// Verify all tasks executed
	count := len(executed)
	if count != 10 {
		t.Errorf("Expected 10 tasks executed, got %d", count)
	}
}

func BenchmarkWorkerPoolSmall(b *testing.B) {
	pool, _ := NewWorkerPool(4)
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Minimal work
		})
	}
}

func BenchmarkWorkerPoolLarge(b *testing.B) {
	pool, _ := NewWorkerPool(100)
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			// Minimal work
		})
	}
}

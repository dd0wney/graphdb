package query

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// mockTask implements the Task interface for testing
type mockTask struct {
	id     string
	result interface{}
	err    error
	delay  time.Duration
}

func (t *mockTask) Execute(graph *storage.GraphStorage) (interface{}, error) {
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	return t.result, t.err
}

func (t *mockTask) ID() string {
	return t.id
}

// setupTestGraph creates a test graph storage
func setupTestGraph(t *testing.T) (*storage.GraphStorage, func()) {
	dataDir := t.TempDir()
	gs, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create test graph: %v", err)
	}
	return gs, func() { gs.Close() }
}

// TestNewWorkerPool tests creating a worker pool
func TestNewWorkerPool(t *testing.T) {
	// Test with specific worker count
	wp := NewWorkerPool(4)
	if wp == nil {
		t.Fatal("Expected non-nil worker pool")
	}
	if wp.workers != 4 {
		t.Errorf("Expected 4 workers, got %d", wp.workers)
	}

	// Test with auto-detect (0 or negative)
	wp2 := NewWorkerPool(0)
	if wp2.workers <= 0 {
		t.Error("Expected positive worker count with auto-detect")
	}
}

// TestWorkerPool_BasicExecution tests basic task execution
func TestWorkerPool_BasicExecution(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(2)
	wp.Start(gs)
	defer wp.Stop()

	// Submit a simple task
	task := &mockTask{
		id:     "test1",
		result: "success",
		err:    nil,
	}

	err := wp.Submit(task)
	if err != nil {
		t.Fatalf("Failed to submit task: %v", err)
	}

	// Get result
	select {
	case result := <-wp.Results():
		if result.TaskID != "test1" {
			t.Errorf("Expected task ID 'test1', got '%s'", result.TaskID)
		}
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
		if result.Result != "success" {
			t.Errorf("Expected result 'success', got %v", result.Result)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

// TestWorkerPool_MultipleTasks tests processing multiple tasks
func TestWorkerPool_MultipleTasks(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(4)
	wp.Start(gs)
	defer wp.Stop()

	numTasks := 10
	taskIDs := make(map[string]bool)

	// Submit multiple tasks
	for i := 0; i < numTasks; i++ {
		task := &mockTask{
			id:     fmt.Sprintf("task%d", i),
			result: i,
		}
		taskIDs[task.id] = false

		err := wp.Submit(task)
		if err != nil {
			t.Fatalf("Failed to submit task %s: %v", task.id, err)
		}
	}

	// Collect results
	for i := 0; i < numTasks; i++ {
		select {
		case result := <-wp.Results():
			if result.Error != nil {
				t.Errorf("Task %s failed: %v", result.TaskID, result.Error)
			}
			taskIDs[result.TaskID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for results")
		}
	}

	// Verify all tasks completed
	for id, completed := range taskIDs {
		if !completed {
			t.Errorf("Task %s did not complete", id)
		}
	}
}

// TestWorkerPool_ErrorHandling tests error propagation
func TestWorkerPool_ErrorHandling(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(2)
	wp.Start(gs)
	defer wp.Stop()

	// Submit a task that returns an error
	task := &mockTask{
		id:     "error_task",
		result: nil,
		err:    fmt.Errorf("test error"),
	}

	wp.Submit(task)

	// Get result and verify error
	select {
	case result := <-wp.Results():
		if result.Error == nil {
			t.Error("Expected error, got nil")
		}
		if result.Error.Error() != "test error" {
			t.Errorf("Expected 'test error', got '%v'", result.Error)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

// TestWorkerPool_Stop tests stopping the worker pool
func TestWorkerPool_Stop(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(2)
	wp.Start(gs)

	// Submit some tasks
	for i := 0; i < 5; i++ {
		task := &mockTask{
			id:     fmt.Sprintf("task%d", i),
			result: i,
		}
		wp.Submit(task)
	}

	// Stop the pool
	wp.Stop()

	// Note: Submitting after stop would panic with "send on closed channel"
	// This is expected behavior - users should not submit after calling Stop()
}

// TestWorkerPool_Stats tests statistics tracking
func TestWorkerPool_Stats(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(2)
	wp.Start(gs)
	defer wp.Stop()

	numTasks := 5
	for i := 0; i < numTasks; i++ {
		task := &mockTask{
			id:     fmt.Sprintf("task%d", i),
			result: i,
			delay:  10 * time.Millisecond,
		}
		wp.Submit(task)
	}

	// Wait for tasks to complete
	for i := 0; i < numTasks; i++ {
		<-wp.Results()
	}

	processed, active := wp.Stats()
	if processed != int64(numTasks) {
		t.Errorf("Expected %d tasks processed, got %d", numTasks, processed)
	}

	if active != 0 {
		t.Errorf("Expected 0 active tasks after completion, got %d", active)
	}
}

// TestWorkerPool_ConcurrentSubmission tests concurrent task submission
func TestWorkerPool_ConcurrentSubmission(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(4)
	wp.Start(gs)
	defer wp.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10
	tasksPerGoroutine := 5
	totalTasks := numGoroutines * tasksPerGoroutine

	// Submit tasks from multiple goroutines
	var submitErrors int32
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				task := &mockTask{
					id:     fmt.Sprintf("g%d_task%d", gid, i),
					result: gid*100 + i,
				}
				if err := wp.Submit(task); err != nil {
					atomic.AddInt32(&submitErrors, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	if submitErrors > 0 {
		t.Errorf("Had %d submit errors during concurrent submission", submitErrors)
	}

	// Collect all results
	resultsReceived := 0
	timeout := time.After(3 * time.Second)
	for resultsReceived < totalTasks {
		select {
		case <-wp.Results():
			resultsReceived++
		case <-timeout:
			t.Fatalf("Timeout: received %d/%d results", resultsReceived, totalTasks)
		}
	}
}

// TestWorkerPool_LongRunningTask tests handling of tasks with varying execution times
func TestWorkerPool_LongRunningTask(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(3)
	wp.Start(gs)
	defer wp.Stop()

	// Submit mix of fast and slow tasks
	tasks := []*mockTask{
		{id: "fast1", result: "f1", delay: 10 * time.Millisecond},
		{id: "slow1", result: "s1", delay: 100 * time.Millisecond},
		{id: "fast2", result: "f2", delay: 10 * time.Millisecond},
	}

	for _, task := range tasks {
		wp.Submit(task)
	}

	// Collect results - fast tasks should finish first
	completed := make(map[string]bool)
	for i := 0; i < len(tasks); i++ {
		select {
		case result := <-wp.Results():
			completed[result.TaskID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for results")
		}
	}

	// Verify all completed
	for _, task := range tasks {
		if !completed[task.ID()] {
			t.Errorf("Task %s did not complete", task.ID())
		}
	}
}

// TestWorkerPool_ResultsChannel tests the results channel behavior
func TestWorkerPool_ResultsChannel(t *testing.T) {
	gs, cleanup := setupTestGraph(t)
	defer cleanup()

	wp := NewWorkerPool(2)
	wp.Start(gs)
	defer wp.Stop()

	// Verify Results() returns the same channel
	ch1 := wp.Results()
	ch2 := wp.Results()

	if ch1 != ch2 {
		t.Error("Results() should return the same channel")
	}

	// Submit and verify we can receive on the channel
	task := &mockTask{id: "test", result: "data"}
	wp.Submit(task)

	select {
	case result := <-ch1:
		if result.TaskID != "test" {
			t.Errorf("Expected task ID 'test', got '%s'", result.TaskID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

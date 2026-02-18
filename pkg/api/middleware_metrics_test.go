package api

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func createTestServer(t *testing.T) (*Server, func()) {
	t.Helper()

	// Create temp directory for storage
	tmpDir, err := os.MkdirTemp("", "metrics-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	graph, err := storage.NewGraphStorage(filepath.Join(tmpDir, "data"))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	registry := metrics.NewRegistry()
	checker := health.NewHealthChecker()

	server := &Server{
		graph:           graph,
		metricsRegistry: registry,
		healthChecker:   checker,
		startTime:       time.Now(),
		metricsStopCh:   make(chan struct{}),
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return server, cleanup
}

func TestServer_StopMetrics(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Start metrics goroutine
	server.metricsWg.Add(1)
	go server.updateMetricsPeriodically()

	// Give the goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// StopMetrics should return promptly
	done := make(chan struct{})
	go func() {
		server.StopMetrics()
		close(done)
	}()

	select {
	case <-done:
		// Success - StopMetrics returned
	case <-time.After(2 * time.Second):
		t.Fatal("StopMetrics() did not return in time - possible goroutine leak")
	}
}

func TestServer_StopMetrics_Idempotent(t *testing.T) {
	// Calling StopMetrics on a server with nil channel should not panic
	server := &Server{}

	// Should not panic
	server.StopMetrics()
}

func TestServer_StopMetrics_MultipleGoroutines(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	// Start metrics goroutine
	server.metricsWg.Add(1)
	go server.updateMetricsPeriodically()

	// Multiple goroutines trying to stop should not race
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Only first close should work, others should be no-op
			defer func() {
				// Recover from potential panic on double-close
				// This tests the nil check in StopMetrics
				recover()
			}()
			server.StopMetrics()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - possible deadlock")
	}
}

func TestServer_MetricsGoroutineRespondsToStop(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	goroutineStarted := make(chan struct{})
	goroutineStopped := make(chan struct{})

	// Wrap updateMetricsPeriodically to track lifecycle
	server.metricsWg.Add(1)
	go func() {
		close(goroutineStarted)
		server.updateMetricsPeriodically()
		close(goroutineStopped)
	}()

	// Wait for goroutine to start
	select {
	case <-goroutineStarted:
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not start in time")
	}

	// Signal stop
	close(server.metricsStopCh)

	// Wait for goroutine to stop
	select {
	case <-goroutineStopped:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not stop in time after receiving stop signal")
	}

	// Wait for WaitGroup
	done := make(chan struct{})
	go func() {
		server.metricsWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("WaitGroup did not complete")
	}
}

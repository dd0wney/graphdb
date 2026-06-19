package api

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestServer_Shutdown_StopsServing verifies that Shutdown(ctx) stops the
// HTTP server and causes Start() to return nil or http.ErrServerClosed (not
// a hard error). Uses port 0 so the OS picks an ephemeral port and the test
// never fights another process for a fixed port.
func TestServer_Shutdown_StopsServing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "api-shutdown-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create graph storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Port 0 asks the OS for a free ephemeral port, so this test never
	// races against another listener on a fixed port.
	server, err := NewServerWithDataDir(gs, 0, tmpDir)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start on ephemeral port in the background.
	errCh := make(chan error, 1)
	go func() { errCh <- server.Start() }()

	// Wait until httpServer is assigned (Start sets s.httpServer before
	// calling ListenAndServe, so this polls until the field is visible).
	deadline := time.Now().Add(2 * time.Second)
	for server.httpServer.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if server.httpServer.Load() == nil {
		t.Fatal("server never started (httpServer field never set)")
	}
	// Give ListenAndServe a moment to bind the socket after the field is set.
	time.Sleep(20 * time.Millisecond)

	// Shutdown must return nil and Start must return nil or ErrServerClosed.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case startErr := <-errCh:
		if startErr != nil && startErr != http.ErrServerClosed {
			t.Fatalf("Start returned %v, want nil or http.ErrServerClosed", startErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

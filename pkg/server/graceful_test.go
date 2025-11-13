package server

import (
	"net/http"
	"syscall"
	"testing"
	"time"
)

// TestGracefulServer_ConfigReload tests configuration reload via SIGHUP
func TestGracefulServer_ConfigReload(t *testing.T) {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	gs := NewGracefulServer(":0", handler) // Use :0 for random port

	// Start server in background
	go func() {
		if err := gs.Start(); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Send SIGHUP signal
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	if err != nil {
		t.Fatalf("Failed to send SIGHUP: %v", err)
	}

	// Wait for reload to be processed
	time.Sleep(200 * time.Millisecond)

	// Check that config reload was triggered
	// Note: Since we can't easily check the actual reload in a test,
	// we're mainly verifying the signal doesn't crash the server
	if gs.IsShuttingDown() {
		t.Error("Server should not be shutting down after SIGHUP")
	}

	// Clean up
	if err := gs.Shutdown(1 * time.Second); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
}

// TestGracefulServer_ReloadConfig tests the ReloadConfig method
func TestGracefulServer_ReloadConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	gs := NewGracefulServer(":0", handler)

	// Create a test config callback
	reloadCalled := false
	configReloadFn := func() error {
		reloadCalled = true
		return nil
	}

	gs.SetConfigReloadFunc(configReloadFn)

	// Trigger reload
	err := gs.ReloadConfig()
	if err != nil {
		t.Errorf("ReloadConfig() error = %v", err)
	}

	if !reloadCalled {
		t.Error("Config reload function was not called")
	}
}

// TestGracefulServer_ReloadConfigWithError tests error handling during reload
func TestGracefulServer_ReloadConfigWithError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	gs := NewGracefulServer(":0", handler)

	// Create a config callback that returns an error
	configReloadFn := func() error {
		return http.ErrServerClosed
	}

	gs.SetConfigReloadFunc(configReloadFn)

	// Trigger reload
	err := gs.ReloadConfig()
	if err == nil {
		t.Error("ReloadConfig() expected error, got nil")
	}

	if err != http.ErrServerClosed {
		t.Errorf("ReloadConfig() error = %v, want %v", err, http.ErrServerClosed)
	}
}

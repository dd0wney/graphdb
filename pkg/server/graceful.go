package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ConfigReloadFunc is a function that reloads configuration
type ConfigReloadFunc func() error

// GracefulServer wraps an HTTP server with graceful shutdown capabilities
type GracefulServer struct {
	server         *http.Server
	shutdownCh     chan struct{}
	shutdownOnce   sync.Once
	configReloadFn ConfigReloadFunc
	configMu       sync.RWMutex
}

// NewGracefulServer creates a new graceful HTTP server
func NewGracefulServer(addr string, handler http.Handler) *GracefulServer {
	return &GracefulServer{
		server: &http.Server{
			Addr:           addr,
			Handler:        handler,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			IdleTimeout:    120 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
		shutdownCh: make(chan struct{}),
	}
}

// Start starts the server and handles graceful shutdown signals
func (gs *GracefulServer) Start() error {
	// Handle shutdown signals
	go gs.handleSignals()

	log.Printf("Starting HTTP server on %s", gs.server.Addr)
	if err := gs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// Shutdown initiates a graceful shutdown
func (gs *GracefulServer) Shutdown(timeout time.Duration) error {
	var err error
	gs.shutdownOnce.Do(func() {
		close(gs.shutdownCh)

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		log.Printf("Initiating graceful shutdown (timeout: %v)", timeout)

		if shutdownErr := gs.server.Shutdown(ctx); shutdownErr != nil {
			err = shutdownErr
			log.Printf("Error during shutdown: %v", shutdownErr)
		} else {
			log.Printf("Server shutdown complete")
		}
	})
	return err
}

// handleSignals listens for OS signals and triggers graceful shutdown
func (gs *GracefulServer) handleSignals() {
	sigCh := make(chan os.Signal, 1)

	// Listen for signals
	signal.Notify(sigCh,
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGTERM, // Termination signal (systemd, docker, k8s)
		syscall.SIGHUP,  // Reload configuration
		syscall.SIGUSR1, // Custom: trigger rolling restart
	)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			log.Printf("Received %v signal, starting graceful shutdown...", sig)
			if err := gs.Shutdown(30 * time.Second); err != nil {
				log.Printf("Shutdown error: %v", err)
				os.Exit(1)
			}
			os.Exit(0)

		case syscall.SIGHUP:
			log.Printf("Received SIGHUP signal, triggering configuration reload...")
			if err := gs.ReloadConfig(); err != nil {
				log.Printf("Configuration reload error: %v", err)
			}

		case syscall.SIGUSR1:
			log.Printf("Received SIGUSR1 signal, preparing for rolling restart...")
			// Signal the upgrade system that this node is ready for restart
			// This allows the binary to be replaced while connections drain
			go func() {
				time.Sleep(5 * time.Second) // Brief delay to allow health checks to detect
				if err := gs.Shutdown(30 * time.Second); err != nil {
					log.Printf("Rolling restart shutdown error: %v", err)
				}
			}()
		}
	}
}

// IsShuttingDown returns true if shutdown has been initiated
func (gs *GracefulServer) IsShuttingDown() bool {
	select {
	case <-gs.shutdownCh:
		return true
	default:
		return false
	}
}

// ShutdownChannel returns a channel that closes when shutdown is initiated
func (gs *GracefulServer) ShutdownChannel() <-chan struct{} {
	return gs.shutdownCh
}

// SetConfigReloadFunc sets the function to call when configuration reload is triggered
func (gs *GracefulServer) SetConfigReloadFunc(fn ConfigReloadFunc) {
	gs.configMu.Lock()
	defer gs.configMu.Unlock()
	gs.configReloadFn = fn
}

// ReloadConfig triggers a configuration reload
func (gs *GracefulServer) ReloadConfig() error {
	gs.configMu.RLock()
	reloadFn := gs.configReloadFn
	gs.configMu.RUnlock()

	if reloadFn == nil {
		log.Printf("Configuration reload requested, but no reload function configured")
		return nil
	}

	log.Printf("Reloading configuration...")
	if err := reloadFn(); err != nil {
		log.Printf("Configuration reload failed: %v", err)
		return err
	}

	log.Printf("Configuration reload complete")
	return nil
}

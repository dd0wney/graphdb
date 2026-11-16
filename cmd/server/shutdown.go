package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type shutdowner interface{ Shutdown(context.Context) error }
type closer interface{ Close() error }

const shutdownTimeout = 30 * time.Second

// serve runs the server and owns the whole shutdown sequence, so that storage
// closes exactly once. The signal goroutine only drains HTTP. Start returns
// http.ErrServerClosed as soon as Shutdown begins, while Shutdown is still
// draining in-flight requests, so serve waits for drained before it closes
// storage. Returning from here lets the caller exit only after Close returns,
// which keeps the snapshot write from being cut short.
//
// It returns the process exit code. A Start failure returns 1 without closing
// storage, as before: the server never served, so no snapshot is owed.
func serve(start func() error, sig <-chan os.Signal, srv shutdowner, stopLicense func(), graph closer, logger *slog.Logger) int {
	drained := make(chan struct{})
	go func() {
		<-sig
		logger.Info("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
		close(drained)
	}()

	err := start()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		return 1
	}
	// Only ErrServerClosed proves the goroutine called Shutdown. Any other
	// return would leave drained open forever.
	if errors.Is(err, http.ErrServerClosed) {
		<-drained
	}

	stopLicense()
	// Close snapshots first, and refuses to write when a base record is damaged
	// (#521). Discarding that error hides the only report the operator gets: the
	// snapshot did not write, so the WAL will not truncate and will grow on every
	// restart until the disk fills.
	if err := graph.Close(); err != nil {
		logger.Error("graph storage did not close cleanly; the snapshot may not have been written", "error", err)
	}
	logger.Info("server exited")
	return 0
}

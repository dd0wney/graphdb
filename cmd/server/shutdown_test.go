package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer mimics http.Server: Shutdown makes Start return ErrServerClosed
// at once, then keeps draining for drain.
type fakeServer struct {
	stopped chan struct{}
	drain   time.Duration
}

func newFakeServer(drain time.Duration) *fakeServer {
	return &fakeServer{stopped: make(chan struct{}), drain: drain}
}

func (f *fakeServer) Start() error {
	<-f.stopped
	return http.ErrServerClosed
}

func (f *fakeServer) Shutdown(context.Context) error {
	close(f.stopped)
	time.Sleep(f.drain)
	return nil
}

type fakeGraph struct {
	calls    atomic.Int32
	finished atomic.Bool
	err      error
}

func (g *fakeGraph) Close() error {
	if g.calls.Add(1) > 1 {
		return errors.New("storage already closed")
	}
	time.Sleep(50 * time.Millisecond) // a snapshot write takes time
	g.finished.Store(true)
	return g.err
}

func runServe(t *testing.T, srv *fakeServer, g *fakeGraph, stops *atomic.Int32) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	code := serve(srv.Start, sig, srv, func() { stops.Add(1) }, g, logger)
	return code, buf.String()
}

func TestServe_ClosesStorageExactlyOnceBeforeReturning(t *testing.T) {
	g := &fakeGraph{}
	var stops atomic.Int32
	code, logs := runServe(t, newFakeServer(10*time.Millisecond), g, &stops)

	if n := g.calls.Load(); n != 1 {
		t.Errorf("Close called %d times, want 1", n)
	}
	if !g.finished.Load() {
		t.Error("serve returned before Close finished")
	}
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if stops.Load() != 1 {
		t.Errorf("licence stop called %d times, want 1", stops.Load())
	}
	if strings.Contains(logs, "did not close cleanly") {
		t.Errorf("false close error logged on a clean close:\n%s", logs)
	}
}

func TestServe_LogsRealCloseFailure(t *testing.T) {
	g := &fakeGraph{err: errors.New("snapshot refused")}
	var stops atomic.Int32
	_, logs := runServe(t, newFakeServer(0), g, &stops)

	if n := strings.Count(logs, "did not close cleanly"); n != 1 {
		t.Errorf("close failure logged %d times, want 1:\n%s", n, logs)
	}
	if !strings.Contains(logs, "snapshot refused") {
		t.Errorf("close error text missing:\n%s", logs)
	}
}

func TestServe_StartFailureExitsOneWithoutClose(t *testing.T) {
	g := &fakeGraph{}
	var stops atomic.Int32
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	code := serve(func() error { return errors.New("bind failed") }, make(chan os.Signal), newFakeServer(0), func() { stops.Add(1) }, g, logger)
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
}

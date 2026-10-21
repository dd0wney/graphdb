package licensing

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Stop used to cancel the context and return at once, while the report loop
// could still be inside a send. Holding the loop in getMetrics makes that
// window deterministic: Stop must not return until the loop has left it.
func TestTelemetryReporter_StopWaitsForReportLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, time.Hour)
	reporter.endpoint = server.URL

	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	getMetrics := func() (int64, int64) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return 1, 2
	}

	reporter.Start(&License{Key: "test-key"}, getMetrics)
	<-entered

	stopped := make(chan struct{})
	go func() {
		reporter.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		close(release)
		t.Fatal("Stop returned while the report loop was still building a report")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after the report loop was released")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("getMetrics calls = %d, want 1", got)
	}
}

// Stop on a reporter that never started, or one that is disabled, must not
// block, and a second Stop must not block either.
func TestTelemetryReporter_StopWithoutStartReturns(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		reporter := NewTelemetryReporter(enabled, time.Hour)
		if !enabled {
			reporter.Start(&License{Key: "k"}, func() (int64, int64) { return 0, 0 })
		}

		done := make(chan struct{})
		go func() {
			reporter.Stop()
			reporter.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("Stop blocked on a reporter that was not running (enabled=%v)", enabled)
		}
	}
}

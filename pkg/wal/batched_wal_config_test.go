package wal

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

// A non-positive flush interval used to reach time.NewTicker inside the
// background flusher goroutine, and a panic there kills the process: no
// caller can recover it. A negative batch size panicked in make. Both must
// be refused before the constructor opens a file or starts a goroutine.
func TestNewBatchedWAL_RefusesInvalidConfig(t *testing.T) {
	cases := []struct {
		name          string
		batchSize     int
		flushInterval time.Duration
		field         string
	}{
		{"zero flush interval", 10, 0, "FlushInterval"},
		{"negative flush interval", 10, -time.Millisecond, "FlushInterval"},
		{"negative batch size", -1, 10 * time.Millisecond, "BatchSize"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			before := runtime.NumGoroutine()

			bw, err := NewBatchedWAL(dir, tc.batchSize, tc.flushInterval)
			if err == nil {
				bw.Close()
				t.Fatalf("NewBatchedWAL(%d, %v) succeeded, want a refusal", tc.batchSize, tc.flushInterval)
			}
			var cfgErr *BatchConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("error %v (%T) is not a *BatchConfigError", err, err)
			}
			if cfgErr.Field != tc.field {
				t.Errorf("Field = %q, want %q", cfgErr.Field, tc.field)
			}

			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("refused constructor left %d entries in the data directory", len(entries))
			}
			if after := runtime.NumGoroutine(); after > before {
				t.Errorf("goroutines: %d before, %d after a refused constructor", before, after)
			}
		})
	}
}

// A zero batch size is accepted on purpose: every append reaches the batch
// threshold, so each write flushes alone. Callers that set EnableBatching
// without a BatchSize depend on that.
func TestNewBatchedWAL_AcceptsZeroBatchSize(t *testing.T) {
	bw, err := NewBatchedWAL(t.TempDir(), 0, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewBatchedWAL with batch size 0: %v", err)
	}
	defer bw.Close()

	if _, err := bw.Append(OpCreateNode, []byte("x")); err != nil {
		t.Fatalf("Append with batch size 0: %v", err)
	}
}

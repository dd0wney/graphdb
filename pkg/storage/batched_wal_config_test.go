package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// mapper-6b reported that an open with EnableBatching and no FlushInterval
// killed the process: the panic came from the WAL's flusher goroutine, which
// no caller can recover. The open must refuse instead and name the field.
func TestOpen_BatchedWALRefusesInvalidConfig(t *testing.T) {
	cases := []struct {
		name          string
		batchSize     int
		flushInterval time.Duration
		field         string
	}{
		{"zero flush interval", 100, 0, "FlushInterval"},
		{"negative flush interval", 100, -time.Second, "FlushInterval"},
		{"negative batch size", -1, 10 * time.Millisecond, "BatchSize"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs, err := NewGraphStorageWithConfig(StorageConfig{
				DataDir:        t.TempDir(),
				EnableBatching: true,
				BatchSize:      tc.batchSize,
				FlushInterval:  tc.flushInterval,
			})
			if err == nil {
				_ = gs.Close()
				t.Fatal("open succeeded, want a refusal")
			}
			var cfgErr *wal.BatchConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("error %v does not wrap a *wal.BatchConfigError", err)
			}
			if cfgErr.Field != tc.field {
				t.Errorf("Field = %q, want %q", cfgErr.Field, tc.field)
			}
		})
	}
}

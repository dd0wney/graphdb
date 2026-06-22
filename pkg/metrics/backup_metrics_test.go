package metrics

import (
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func counterValue(t *testing.T, c interface {
	Write(*dto.Metric) error
}) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.Counter.GetValue()
}

func TestRecordBackup(t *testing.T) {
	r := NewRegistry()

	r.RecordBackup("success", 2048, 12*time.Millisecond)
	r.RecordBackup("success", 4096, 8*time.Millisecond)
	r.RecordBackup("error", 0, 1*time.Millisecond)

	ok, _ := r.BackupsTotal.GetMetricWithLabelValues("success")
	if v := counterValue(t, ok); v != 2 {
		t.Errorf(`BackupsTotal{result="success"} = %v, want 2`, v)
	}
	bad, _ := r.BackupsTotal.GetMetricWithLabelValues("error")
	if v := counterValue(t, bad); v != 1 {
		t.Errorf(`BackupsTotal{result="error"} = %v, want 1`, v)
	}

	// The size + duration histograms should be registered and have observed
	// the backups; confirm via Gather.
	mfs, err := r.GetPrometheusRegistry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]uint64{}
	for _, mf := range mfs {
		name := mf.GetName()
		if strings.Contains(name, "backup") && !strings.HasPrefix(name, "graphdb_") {
			t.Errorf("backup metric %s lacks graphdb_ prefix", name)
		}
		for _, m := range mf.GetMetric() {
			if h := m.GetHistogram(); h != nil {
				names[name] += h.GetSampleCount()
			}
		}
	}
	if names["graphdb_backup_size_bytes"] != 3 {
		t.Errorf("graphdb_backup_size_bytes sample count = %d, want 3", names["graphdb_backup_size_bytes"])
	}
	if names["graphdb_backup_duration_seconds"] != 3 {
		t.Errorf("graphdb_backup_duration_seconds sample count = %d, want 3", names["graphdb_backup_duration_seconds"])
	}
}

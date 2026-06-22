package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initBackupMetrics() {
	r.BackupsTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_backup_total",
			Help: "Total number of hot-backup archives produced, by result",
		},
		[]string{"result"}, // "success" | "error"
	)

	r.BackupDuration = promauto.With(r.registry).NewHistogram(
		prometheus.HistogramOpts{
			Name:    "graphdb_backup_duration_seconds",
			Help:    "Time taken to stream a backup archive in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60},
		},
	)

	r.BackupSizeBytes = promauto.With(r.registry).NewHistogram(
		prometheus.HistogramOpts{
			Name:    "graphdb_backup_size_bytes",
			Help:    "Size of produced backup archives in bytes",
			Buckets: prometheus.ExponentialBuckets(1024, 4, 10), // 1KiB .. ~256MiB
		},
	)
}

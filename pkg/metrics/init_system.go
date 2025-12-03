package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initSystemMetrics() {
	r.UptimeSeconds = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_uptime_seconds",
			Help: "Time since the server started in seconds",
		},
	)

	r.GoRoutines = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_goroutines",
			Help: "Number of goroutines",
		},
	)

	r.MemoryAllocBytes = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_memory_alloc_bytes",
			Help: "Bytes of allocated heap objects",
		},
	)

	r.MemorySysBytes = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_memory_sys_bytes",
			Help: "Total bytes of memory obtained from the OS",
		},
	)
}

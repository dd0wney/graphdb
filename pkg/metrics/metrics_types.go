package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Registry holds all metrics for the application
type Registry struct {
	// HTTP Metrics
	HTTPRequestsTotal     *prometheus.CounterVec
	HTTPRequestDuration   *prometheus.HistogramVec
	HTTPRequestsInFlight  prometheus.Gauge
	HTTPResponseSizeBytes *prometheus.HistogramVec

	// Storage Metrics
	StorageNodesTotal        prometheus.Gauge
	StorageEdgesTotal        prometheus.Gauge
	StorageOperationsTotal   *prometheus.CounterVec
	StorageOperationDuration *prometheus.HistogramVec
	StorageDiskUsageBytes    prometheus.Gauge

	// Query Metrics
	QueriesTotal        *prometheus.CounterVec
	QueryDuration       *prometheus.HistogramVec
	QueryNodesScanned   *prometheus.HistogramVec
	QueryEdgesScanned   *prometheus.HistogramVec
	SlowQueries         *prometheus.CounterVec

	// Replication Metrics
	ReplicationLagLSN             prometheus.Gauge
	ReplicationLagSeconds         prometheus.Gauge
	ReplicationThroughputBytes    *prometheus.CounterVec
	ReplicationConnectedReplicas  prometheus.Gauge
	ReplicationWALEntriesTotal    *prometheus.CounterVec
	ReplicationHeartbeatsTotal    *prometheus.CounterVec

	// Cluster Metrics (HA)
	ClusterNodesTotal        prometheus.Gauge
	ClusterHealthyNodesTotal prometheus.Gauge
	ClusterHasQuorum         prometheus.Gauge
	ClusterElectionsTotal    *prometheus.CounterVec
	ClusterElectionDuration  prometheus.Histogram
	ClusterEpoch             prometheus.Gauge
	ClusterTerm              prometheus.Gauge
	ClusterRole              *prometheus.GaugeVec

	// Licensing Metrics
	LicenseValid            prometheus.Gauge
	LicenseExpiresAt        prometheus.Gauge
	LicenseValidationErrors prometheus.Counter

	// Security Metrics
	AuthFailuresTotal                prometheus.Counter
	SecurityEncryptionEnabled        prometheus.Gauge
	SecurityKeyLastRotationTimestamp prometheus.Gauge
	SecurityTLSEnabled               prometheus.Gauge
	SecurityTLSCertExpiryTimestamp   prometheus.Gauge
	SecurityAuditExportFailuresTotal prometheus.Counter
	SecuritySuspiciousEventsTotal    prometheus.Counter
	SecurityUnauthorizedAccessTotal  prometheus.Counter
	SecurityHealthStatus             prometheus.Gauge

	// System Metrics
	UptimeSeconds    prometheus.Gauge
	GoRoutines       prometheus.Gauge
	MemoryAllocBytes prometheus.Gauge
	MemorySysBytes   prometheus.Gauge

	registry *prometheus.Registry
	mu       sync.RWMutex
}

var (
	// Global registry instance
	defaultRegistry *Registry
	once            sync.Once
)

// DefaultRegistry returns the global metrics registry
func DefaultRegistry() *Registry {
	once.Do(func() {
		defaultRegistry = NewRegistry()
	})
	return defaultRegistry
}

// NewRegistry creates a new metrics registry with all metrics initialized
func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()

	r := &Registry{
		registry: reg,
	}

	// Initialize all metrics
	r.initHTTPMetrics()
	r.initStorageMetrics()
	r.initQueryMetrics()
	r.initReplicationMetrics()
	r.initClusterMetrics()
	r.initLicensingMetrics()
	r.initSecurityMetrics()
	r.initSystemMetrics()

	return r
}

// GetPrometheusRegistry returns the underlying Prometheus registry
func (r *Registry) GetPrometheusRegistry() *prometheus.Registry {
	return r.registry
}

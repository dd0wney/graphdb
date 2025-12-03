package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}

	// Verify all metrics are initialized
	if r.HTTPRequestsTotal == nil {
		t.Error("HTTPRequestsTotal not initialized")
	}
	if r.HTTPRequestDuration == nil {
		t.Error("HTTPRequestDuration not initialized")
	}
	if r.StorageNodesTotal == nil {
		t.Error("StorageNodesTotal not initialized")
	}
	if r.ClusterNodesTotal == nil {
		t.Error("ClusterNodesTotal not initialized")
	}
	if r.registry == nil {
		t.Error("Prometheus registry not initialized")
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Should return the same instance
	r1 := DefaultRegistry()
	r2 := DefaultRegistry()

	if r1 != r2 {
		t.Error("DefaultRegistry() should return the same instance")
	}
}

func TestRecordHTTPRequest(t *testing.T) {
	r := NewRegistry()

	// Record some requests
	r.RecordHTTPRequest("GET", "/api/nodes", "200", 100*time.Millisecond)
	r.RecordHTTPRequest("POST", "/api/nodes", "201", 200*time.Millisecond)
	r.RecordHTTPRequest("GET", "/api/nodes", "404", 50*time.Millisecond)

	// Verify counter was incremented
	counter, err := r.HTTPRequestsTotal.GetMetricWithLabelValues("GET", "/api/nodes", "200")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	var metric dto.Metric
	if err := counter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 1 {
		t.Errorf("Counter value = %v, want 1", metric.Counter.GetValue())
	}
}

func TestRecordStorageOperation(t *testing.T) {
	r := NewRegistry()

	// Record some operations
	r.RecordStorageOperation("create_node", "success", 10*time.Millisecond)
	r.RecordStorageOperation("create_node", "success", 20*time.Millisecond)
	r.RecordStorageOperation("create_node", "error", 5*time.Millisecond)

	// Verify success counter
	successCounter, err := r.StorageOperationsTotal.GetMetricWithLabelValues("create_node", "success")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	var metric dto.Metric
	if err := successCounter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 2 {
		t.Errorf("Success counter = %v, want 2", metric.Counter.GetValue())
	}

	// Verify error counter
	errorCounter, err := r.StorageOperationsTotal.GetMetricWithLabelValues("create_node", "error")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	if err := errorCounter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 1 {
		t.Errorf("Error counter = %v, want 1", metric.Counter.GetValue())
	}
}

func TestRecordQuery(t *testing.T) {
	r := NewRegistry()

	// Record successful query
	r.RecordQuery("match", "success", 50*time.Millisecond, 100, 200)

	counter, err := r.QueriesTotal.GetMetricWithLabelValues("match", "success")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	var metric dto.Metric
	if err := counter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 1 {
		t.Errorf("Query counter = %v, want 1", metric.Counter.GetValue())
	}
}

func TestSetClusterRole(t *testing.T) {
	r := NewRegistry()

	// Set role to primary
	r.SetClusterRole("primary")

	// Get metric
	gauge, err := r.ClusterRole.GetMetricWithLabelValues("primary")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	var metric dto.Metric
	if err := gauge.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("Primary role gauge = %v, want 1", metric.Gauge.GetValue())
	}

	// Check replica is 0
	replicaGauge, err := r.ClusterRole.GetMetricWithLabelValues("replica")
	if err != nil {
		t.Fatalf("Failed to get replica metric: %v", err)
	}

	if err := replicaGauge.Write(&metric); err != nil {
		t.Fatalf("Failed to write replica metric: %v", err)
	}

	if metric.Gauge.GetValue() != 0 {
		t.Errorf("Replica role gauge = %v, want 0", metric.Gauge.GetValue())
	}

	// Switch to replica
	r.SetClusterRole("replica")

	if err := replicaGauge.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("After switch, replica gauge = %v, want 1", metric.Gauge.GetValue())
	}
}

func TestGaugeMetrics(t *testing.T) {
	r := NewRegistry()

	// Test various gauge metrics
	r.StorageNodesTotal.Set(100)
	r.StorageEdgesTotal.Set(500)
	r.ClusterNodesTotal.Set(3)
	r.ClusterHealthyNodesTotal.Set(3)

	tests := []struct {
		name     string
		gauge    prometheus.Gauge
		expected float64
	}{
		{"StorageNodesTotal", r.StorageNodesTotal, 100},
		{"StorageEdgesTotal", r.StorageEdgesTotal, 500},
		{"ClusterNodesTotal", r.ClusterNodesTotal, 3},
		{"ClusterHealthyNodesTotal", r.ClusterHealthyNodesTotal, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var metric dto.Metric
			if err := tt.gauge.Write(&metric); err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}

			if metric.Gauge.GetValue() != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, metric.Gauge.GetValue(), tt.expected)
			}
		})
	}
}

func TestQuorumMetric(t *testing.T) {
	r := NewRegistry()

	// Set quorum to true (1)
	r.ClusterHasQuorum.Set(1)

	var metric dto.Metric
	if err := r.ClusterHasQuorum.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("HasQuorum = %v, want 1", metric.Gauge.GetValue())
	}

	// Set quorum to false (0)
	r.ClusterHasQuorum.Set(0)

	if err := r.ClusterHasQuorum.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 0 {
		t.Errorf("HasQuorum = %v, want 0", metric.Gauge.GetValue())
	}
}

func TestReplicationMetrics(t *testing.T) {
	r := NewRegistry()

	// Test replication lag
	r.ReplicationLagLSN.Set(100)

	var metric dto.Metric
	if err := r.ReplicationLagLSN.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 100 {
		t.Errorf("ReplicationLagLSN = %v, want 100", metric.Gauge.GetValue())
	}

	// Test connected replicas
	r.ReplicationConnectedReplicas.Set(2)

	if err := r.ReplicationConnectedReplicas.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 2 {
		t.Errorf("ConnectedReplicas = %v, want 2", metric.Gauge.GetValue())
	}

	// Test WAL entries counter
	r.ReplicationWALEntriesTotal.WithLabelValues("sent").Inc()
	r.ReplicationWALEntriesTotal.WithLabelValues("sent").Inc()
	r.ReplicationWALEntriesTotal.WithLabelValues("received").Inc()

	sentCounter, _ := r.ReplicationWALEntriesTotal.GetMetricWithLabelValues("sent")
	if err := sentCounter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 2 {
		t.Errorf("WAL sent counter = %v, want 2", metric.Counter.GetValue())
	}
}

func TestElectionMetrics(t *testing.T) {
	r := NewRegistry()

	// Record elections
	r.ClusterElectionsTotal.WithLabelValues("won").Inc()
	r.ClusterElectionsTotal.WithLabelValues("won").Inc()
	r.ClusterElectionsTotal.WithLabelValues("lost").Inc()

	// Check won counter
	wonCounter, _ := r.ClusterElectionsTotal.GetMetricWithLabelValues("won")
	var metric dto.Metric
	if err := wonCounter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 2 {
		t.Errorf("Elections won = %v, want 2", metric.Counter.GetValue())
	}

	// Check lost counter
	lostCounter, _ := r.ClusterElectionsTotal.GetMetricWithLabelValues("lost")
	if err := lostCounter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 1 {
		t.Errorf("Elections lost = %v, want 1", metric.Counter.GetValue())
	}

	// Test election duration
	r.ClusterElectionDuration.Observe(1.5)
	r.ClusterElectionDuration.Observe(2.3)

	if err := r.ClusterElectionDuration.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Histogram.GetSampleCount() != 2 {
		t.Errorf("Election duration sample count = %v, want 2", metric.Histogram.GetSampleCount())
	}
}

func TestSystemMetrics(t *testing.T) {
	r := NewRegistry()

	// Set system metrics
	r.UptimeSeconds.Set(3600)
	r.GoRoutines.Set(50)
	r.MemoryAllocBytes.Set(1024 * 1024 * 100) // 100 MB
	r.MemorySysBytes.Set(1024 * 1024 * 200)   // 200 MB

	tests := []struct {
		name     string
		gauge    prometheus.Gauge
		expected float64
	}{
		{"UptimeSeconds", r.UptimeSeconds, 3600},
		{"GoRoutines", r.GoRoutines, 50},
		{"MemoryAllocBytes", r.MemoryAllocBytes, 1024 * 1024 * 100},
		{"MemorySysBytes", r.MemorySysBytes, 1024 * 1024 * 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var metric dto.Metric
			if err := tt.gauge.Write(&metric); err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}

			if metric.Gauge.GetValue() != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, metric.Gauge.GetValue(), tt.expected)
			}
		})
	}
}

func TestLicenseMetrics(t *testing.T) {
	r := NewRegistry()

	// Test license status
	r.LicenseValid.Set(1)

	var metric dto.Metric
	if err := r.LicenseValid.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("LicenseValid = %v, want 1", metric.Gauge.GetValue())
	}

	// Test license expiry
	expiryTime := time.Now().Add(30 * 24 * time.Hour).Unix()
	r.LicenseExpiresAt.Set(float64(expiryTime))

	if err := r.LicenseExpiresAt.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != float64(expiryTime) {
		t.Errorf("LicenseExpiresAt = %v, want %v", metric.Gauge.GetValue(), expiryTime)
	}
}

func TestGetPrometheusRegistry(t *testing.T) {
	r := NewRegistry()
	promRegistry := r.GetPrometheusRegistry()

	if promRegistry == nil {
		t.Fatal("GetPrometheusRegistry() returned nil")
	}

	// Verify we can gather metrics
	metrics, err := promRegistry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("No metrics registered")
	}

	// Verify some expected metrics exist
	expectedMetrics := []string{
		"graphdb_storage_nodes_total",
		"graphdb_cluster_nodes_total",
		"graphdb_uptime_seconds",
	}

	metricNames := make(map[string]bool)
	for _, m := range metrics {
		metricNames[m.GetName()] = true
	}

	for _, expected := range expectedMetrics {
		if !metricNames[expected] {
			t.Errorf("Expected metric %s not found", expected)
		}
	}
}

func TestHistogramMetrics(t *testing.T) {
	r := NewRegistry()

	// Record HTTP request durations (method, path, status)
	r.HTTPRequestDuration.WithLabelValues("GET", "/api/nodes", "200").Observe(0.1)
	r.HTTPRequestDuration.WithLabelValues("GET", "/api/nodes", "200").Observe(0.2)
	r.HTTPRequestDuration.WithLabelValues("GET", "/api/nodes", "200").Observe(0.15)

	histogram, err := r.HTTPRequestDuration.GetMetricWithLabelValues("GET", "/api/nodes", "200")
	if err != nil {
		t.Fatalf("Failed to get histogram: %v", err)
	}

	var metric dto.Metric
	if err := histogram.(prometheus.Histogram).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Histogram.GetSampleCount() != 3 {
		t.Errorf("Sample count = %v, want 3", metric.Histogram.GetSampleCount())
	}

	// Sum should be approximately 0.45 (0.1 + 0.2 + 0.15)
	sum := metric.Histogram.GetSampleSum()
	if sum < 0.44 || sum > 0.46 {
		t.Errorf("Sample sum = %v, want ~0.45", sum)
	}
}

func TestConcurrentMetricUpdates(t *testing.T) {
	r := NewRegistry()

	// Simulate concurrent HTTP requests
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				r.RecordHTTPRequest("GET", "/test", "200", 10*time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify counter
	counter, err := r.HTTPRequestsTotal.GetMetricWithLabelValues("GET", "/test", "200")
	if err != nil {
		t.Fatalf("Failed to get metric: %v", err)
	}

	var metric dto.Metric
	if err := counter.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	// Should have 1000 total requests (10 goroutines * 100 requests)
	if metric.Counter.GetValue() != 1000 {
		t.Errorf("Counter = %v, want 1000", metric.Counter.GetValue())
	}
}

func TestMetricLabels(t *testing.T) {
	r := NewRegistry()

	// Test that metrics with different labels are tracked separately
	r.RecordHTTPRequest("GET", "/api/nodes", "200", 10*time.Millisecond)
	r.RecordHTTPRequest("POST", "/api/nodes", "201", 20*time.Millisecond)
	r.RecordHTTPRequest("GET", "/api/edges", "200", 15*time.Millisecond)

	// Each should have count of 1
	getNodes, _ := r.HTTPRequestsTotal.GetMetricWithLabelValues("GET", "/api/nodes", "200")
	postNodes, _ := r.HTTPRequestsTotal.GetMetricWithLabelValues("POST", "/api/nodes", "201")
	getEdges, _ := r.HTTPRequestsTotal.GetMetricWithLabelValues("GET", "/api/edges", "200")

	var metric dto.Metric

	getNodes.Write(&metric)
	if metric.Counter.GetValue() != 1 {
		t.Errorf("GET /api/nodes counter = %v, want 1", metric.Counter.GetValue())
	}

	postNodes.Write(&metric)
	if metric.Counter.GetValue() != 1 {
		t.Errorf("POST /api/nodes counter = %v, want 1", metric.Counter.GetValue())
	}

	getEdges.Write(&metric)
	if metric.Counter.GetValue() != 1 {
		t.Errorf("GET /api/edges counter = %v, want 1", metric.Counter.GetValue())
	}
}

func TestMetricNaming(t *testing.T) {
	r := NewRegistry()
	promRegistry := r.GetPrometheusRegistry()

	metrics, err := promRegistry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify all metrics have the graphdb_ prefix
	for _, m := range metrics {
		name := m.GetName()
		if !strings.HasPrefix(name, "graphdb_") {
			t.Errorf("Metric %s does not have graphdb_ prefix", name)
		}
	}
}

func BenchmarkRecordHTTPRequest(b *testing.B) {
	r := NewRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RecordHTTPRequest("GET", "/api/nodes", "200", 10*time.Millisecond)
	}
}

func BenchmarkRecordStorageOperation(b *testing.B) {
	r := NewRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RecordStorageOperation("create_node", "success", 5*time.Millisecond)
	}
}

func BenchmarkSetGauge(b *testing.B) {
	r := NewRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.StorageNodesTotal.Set(float64(i))
	}
}

package metrics

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// TestSecurityMetricsInitialization tests that all security metrics are initialized
func TestSecurityMetricsInitialization(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name   string
		metric any
	}{
		{"AuthFailuresTotal", r.AuthFailuresTotal},
		{"SecurityEncryptionEnabled", r.SecurityEncryptionEnabled},
		{"SecurityKeyLastRotationTimestamp", r.SecurityKeyLastRotationTimestamp},
		{"SecurityTLSEnabled", r.SecurityTLSEnabled},
		{"SecurityTLSCertExpiryTimestamp", r.SecurityTLSCertExpiryTimestamp},
		{"SecurityAuditExportFailuresTotal", r.SecurityAuditExportFailuresTotal},
		{"SecuritySuspiciousEventsTotal", r.SecuritySuspiciousEventsTotal},
		{"SecurityUnauthorizedAccessTotal", r.SecurityUnauthorizedAccessTotal},
		{"SecurityHealthStatus", r.SecurityHealthStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.metric == nil {
				t.Errorf("%s metric not initialized", tt.name)
			}
		})
	}

	t.Log("✓ All security metrics initialized correctly")
}

// TestAuthFailuresTotal tests the auth failures counter
func TestAuthFailuresTotal(t *testing.T) {
	r := NewRegistry()

	// Initially should be 0
	var metric dto.Metric
	if err := r.AuthFailuresTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 0 {
		t.Errorf("Initial auth failures = %v, want 0", metric.Counter.GetValue())
	}

	// Simulate authentication failures
	r.AuthFailuresTotal.Inc()
	r.AuthFailuresTotal.Inc()
	r.AuthFailuresTotal.Inc()

	if err := r.AuthFailuresTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 3 {
		t.Errorf("Auth failures = %v, want 3", metric.Counter.GetValue())
	}

	t.Log("✓ AuthFailuresTotal counter working correctly")
}

// TestSecurityEncryptionEnabled tests the encryption enabled gauge
func TestSecurityEncryptionEnabled(t *testing.T) {
	r := NewRegistry()

	// Test encryption disabled (0)
	r.SecurityEncryptionEnabled.Set(0)

	var metric dto.Metric
	if err := r.SecurityEncryptionEnabled.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 0 {
		t.Errorf("Encryption enabled = %v, want 0", metric.Gauge.GetValue())
	}

	// Test encryption enabled (1)
	r.SecurityEncryptionEnabled.Set(1)

	if err := r.SecurityEncryptionEnabled.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("Encryption enabled = %v, want 1", metric.Gauge.GetValue())
	}

	t.Log("✓ SecurityEncryptionEnabled gauge working correctly")
}

// TestSecurityKeyLastRotationTimestamp tests the key rotation timestamp
func TestSecurityKeyLastRotationTimestamp(t *testing.T) {
	r := NewRegistry()

	// Set to current time
	now := time.Now().Unix()
	r.SecurityKeyLastRotationTimestamp.Set(float64(now))

	var metric dto.Metric
	if err := r.SecurityKeyLastRotationTimestamp.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != float64(now) {
		t.Errorf("Key rotation timestamp = %v, want %v", metric.Gauge.GetValue(), now)
	}

	// Test updating to a new rotation time
	futureTime := now + 86400 // 1 day later
	r.SecurityKeyLastRotationTimestamp.Set(float64(futureTime))

	if err := r.SecurityKeyLastRotationTimestamp.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != float64(futureTime) {
		t.Errorf("Updated key rotation timestamp = %v, want %v", metric.Gauge.GetValue(), futureTime)
	}

	t.Log("✓ SecurityKeyLastRotationTimestamp gauge working correctly")
}

// TestSecurityTLSEnabled tests the TLS enabled gauge
func TestSecurityTLSEnabled(t *testing.T) {
	r := NewRegistry()

	// Test TLS disabled (0)
	r.SecurityTLSEnabled.Set(0)

	var metric dto.Metric
	if err := r.SecurityTLSEnabled.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 0 {
		t.Errorf("TLS enabled = %v, want 0", metric.Gauge.GetValue())
	}

	// Test TLS enabled (1)
	r.SecurityTLSEnabled.Set(1)

	if err := r.SecurityTLSEnabled.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("TLS enabled = %v, want 1", metric.Gauge.GetValue())
	}

	t.Log("✓ SecurityTLSEnabled gauge working correctly")
}

// TestSecurityTLSCertExpiryTimestamp tests the TLS cert expiry timestamp
func TestSecurityTLSCertExpiryTimestamp(t *testing.T) {
	r := NewRegistry()

	// Set cert expiry to 30 days from now
	expiryTime := time.Now().Add(30 * 24 * time.Hour).Unix()
	r.SecurityTLSCertExpiryTimestamp.Set(float64(expiryTime))

	var metric dto.Metric
	if err := r.SecurityTLSCertExpiryTimestamp.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != float64(expiryTime) {
		t.Errorf("TLS cert expiry = %v, want %v", metric.Gauge.GetValue(), expiryTime)
	}

	t.Log("✓ SecurityTLSCertExpiryTimestamp gauge working correctly")
}

// TestSecurityAuditExportFailuresTotal tests the audit export failures counter
func TestSecurityAuditExportFailuresTotal(t *testing.T) {
	r := NewRegistry()

	// Initially should be 0
	var metric dto.Metric
	if err := r.SecurityAuditExportFailuresTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 0 {
		t.Errorf("Initial audit export failures = %v, want 0", metric.Counter.GetValue())
	}

	// Simulate export failures
	r.SecurityAuditExportFailuresTotal.Inc()
	r.SecurityAuditExportFailuresTotal.Inc()

	if err := r.SecurityAuditExportFailuresTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 2 {
		t.Errorf("Audit export failures = %v, want 2", metric.Counter.GetValue())
	}

	t.Log("✓ SecurityAuditExportFailuresTotal counter working correctly")
}

// TestSecuritySuspiciousEventsTotal tests the suspicious events counter
func TestSecuritySuspiciousEventsTotal(t *testing.T) {
	r := NewRegistry()

	// Initially should be 0
	var metric dto.Metric
	if err := r.SecuritySuspiciousEventsTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 0 {
		t.Errorf("Initial suspicious events = %v, want 0", metric.Counter.GetValue())
	}

	// Simulate suspicious events
	for i := 0; i < 5; i++ {
		r.SecuritySuspiciousEventsTotal.Inc()
	}

	if err := r.SecuritySuspiciousEventsTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 5 {
		t.Errorf("Suspicious events = %v, want 5", metric.Counter.GetValue())
	}

	t.Log("✓ SecuritySuspiciousEventsTotal counter working correctly")
}

// TestSecurityUnauthorizedAccessTotal tests the unauthorized access counter
func TestSecurityUnauthorizedAccessTotal(t *testing.T) {
	r := NewRegistry()

	// Initially should be 0
	var metric dto.Metric
	if err := r.SecurityUnauthorizedAccessTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 0 {
		t.Errorf("Initial unauthorized access = %v, want 0", metric.Counter.GetValue())
	}

	// Simulate unauthorized access attempts
	for i := 0; i < 10; i++ {
		r.SecurityUnauthorizedAccessTotal.Inc()
	}

	if err := r.SecurityUnauthorizedAccessTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Counter.GetValue() != 10 {
		t.Errorf("Unauthorized access = %v, want 10", metric.Counter.GetValue())
	}

	t.Log("✓ SecurityUnauthorizedAccessTotal counter working correctly")
}

// TestSecurityHealthStatus tests the security health status gauge
func TestSecurityHealthStatus(t *testing.T) {
	r := NewRegistry()

	// Test healthy status (1)
	r.SecurityHealthStatus.Set(1)

	var metric dto.Metric
	if err := r.SecurityHealthStatus.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 1 {
		t.Errorf("Security health = %v, want 1", metric.Gauge.GetValue())
	}

	// Test unhealthy status (0)
	r.SecurityHealthStatus.Set(0)

	if err := r.SecurityHealthStatus.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	if metric.Gauge.GetValue() != 0 {
		t.Errorf("Security health = %v, want 0", metric.Gauge.GetValue())
	}

	t.Log("✓ SecurityHealthStatus gauge working correctly")
}

// TestSecurityMetricsRegistration tests that security metrics are registered with Prometheus
func TestSecurityMetricsRegistration(t *testing.T) {
	r := NewRegistry()
	promRegistry := r.GetPrometheusRegistry()

	// Gather all metrics
	metrics, err := promRegistry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Expected security metric names
	expectedMetrics := []string{
		"graphdb_auth_failures_total",
		"graphdb_security_encryption_enabled",
		"graphdb_security_key_last_rotation_timestamp_seconds",
		"graphdb_security_tls_enabled",
		"graphdb_security_tls_cert_expiry_timestamp_seconds",
		"graphdb_security_audit_export_failures_total",
		"graphdb_security_suspicious_events_total",
		"graphdb_security_unauthorized_access_total",
		"graphdb_security_health_status",
	}

	// Build map of registered metric names
	metricNames := make(map[string]bool)
	for _, m := range metrics {
		metricNames[m.GetName()] = true
	}

	// Verify all expected metrics are registered
	for _, expected := range expectedMetrics {
		if !metricNames[expected] {
			t.Errorf("Expected metric %s not registered", expected)
		}
	}

	t.Logf("✓ All %d security metrics registered with Prometheus", len(expectedMetrics))
}

// TestSecurityMetricsEmission tests that security metrics emit correct Prometheus format
func TestSecurityMetricsEmission(t *testing.T) {
	r := NewRegistry()

	// Set various security metrics
	r.AuthFailuresTotal.Inc()
	r.SecurityEncryptionEnabled.Set(1)
	r.SecurityKeyLastRotationTimestamp.Set(float64(time.Now().Unix()))
	r.SecurityTLSEnabled.Set(1)
	r.SecurityTLSCertExpiryTimestamp.Set(float64(time.Now().Add(90 * 24 * time.Hour).Unix()))
	r.SecurityHealthStatus.Set(1)

	// Gather metrics
	promRegistry := r.GetPrometheusRegistry()
	metrics, err := promRegistry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify we can emit metrics
	if len(metrics) == 0 {
		t.Fatal("No metrics emitted")
	}

	// Count security metrics in emission
	securityMetricCount := 0
	for _, m := range metrics {
		name := m.GetName()
		if name == "graphdb_auth_failures_total" ||
			name == "graphdb_security_encryption_enabled" ||
			name == "graphdb_security_key_last_rotation_timestamp_seconds" ||
			name == "graphdb_security_tls_enabled" ||
			name == "graphdb_security_tls_cert_expiry_timestamp_seconds" ||
			name == "graphdb_security_audit_export_failures_total" ||
			name == "graphdb_security_suspicious_events_total" ||
			name == "graphdb_security_unauthorized_access_total" ||
			name == "graphdb_security_health_status" {
			securityMetricCount++

			// Verify metric has description
			if m.GetHelp() == "" {
				t.Errorf("Metric %s has no help text", name)
			}

			// Verify metric has correct type (Counter or Gauge)
			metricType := m.GetType()
			if metricType != dto.MetricType_COUNTER && metricType != dto.MetricType_GAUGE {
				t.Errorf("Metric %s has unexpected type: %v", name, metricType)
			}
		}
	}

	if securityMetricCount != 9 {
		t.Errorf("Expected 9 security metrics in emission, got %d", securityMetricCount)
	}

	t.Logf("✓ Security metrics emitted correctly (found %d metrics)", securityMetricCount)
}

// TestConcurrentSecurityMetricUpdates tests thread-safe metric updates
func TestConcurrentSecurityMetricUpdates(t *testing.T) {
	r := NewRegistry()

	// Simulate concurrent auth failures
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				r.AuthFailuresTotal.Inc()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify counter
	var metric dto.Metric
	if err := r.AuthFailuresTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}

	// Should have 1000 total failures (10 goroutines * 100 increments)
	if metric.Counter.GetValue() != 1000 {
		t.Errorf("Concurrent auth failures = %v, want 1000", metric.Counter.GetValue())
	}

	t.Log("✓ Concurrent security metric updates working correctly")
}

// TestSecurityMetricsRealisticScenario tests a realistic security monitoring scenario
func TestSecurityMetricsRealisticScenario(t *testing.T) {
	r := NewRegistry()

	// Scenario: Server starts with encryption and TLS enabled
	r.SecurityEncryptionEnabled.Set(1)
	r.SecurityTLSEnabled.Set(1)
	r.SecurityHealthStatus.Set(1)

	// Set initial key rotation time (90 days ago - overdue)
	ninetyDaysAgo := time.Now().Add(-90 * 24 * time.Hour).Unix()
	r.SecurityKeyLastRotationTimestamp.Set(float64(ninetyDaysAgo))

	// Set TLS cert expiry (30 days from now - warning threshold)
	thirtyDaysFromNow := time.Now().Add(30 * 24 * time.Hour).Unix()
	r.SecurityTLSCertExpiryTimestamp.Set(float64(thirtyDaysFromNow))

	// Simulate attack scenario: multiple auth failures and unauthorized access
	for i := 0; i < 25; i++ {
		r.AuthFailuresTotal.Inc()
		r.SecurityUnauthorizedAccessTotal.Inc()
	}

	// Detect suspicious activity
	for i := 0; i < 15; i++ {
		r.SecuritySuspiciousEventsTotal.Inc()
	}

	// Simulate audit export failure
	r.SecurityAuditExportFailuresTotal.Inc()

	// Verify all metrics are set correctly
	var metric dto.Metric

	// Check auth failures
	if err := r.AuthFailuresTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to read auth failures: %v", err)
	}
	if metric.Counter.GetValue() != 25 {
		t.Errorf("Auth failures = %v, want 25", metric.Counter.GetValue())
	}

	// Check encryption enabled
	if err := r.SecurityEncryptionEnabled.Write(&metric); err != nil {
		t.Fatalf("Failed to read encryption status: %v", err)
	}
	if metric.Gauge.GetValue() != 1 {
		t.Errorf("Encryption enabled = %v, want 1", metric.Gauge.GetValue())
	}

	// Check key rotation timestamp
	if err := r.SecurityKeyLastRotationTimestamp.Write(&metric); err != nil {
		t.Fatalf("Failed to read key rotation: %v", err)
	}
	rotationAge := time.Now().Unix() - int64(metric.Gauge.GetValue())
	if rotationAge < 89*24*3600 || rotationAge > 91*24*3600 {
		t.Errorf("Key rotation age = %d seconds, expected ~90 days", rotationAge)
	}

	// Check suspicious events
	if err := r.SecuritySuspiciousEventsTotal.Write(&metric); err != nil {
		t.Fatalf("Failed to read suspicious events: %v", err)
	}
	if metric.Counter.GetValue() != 15 {
		t.Errorf("Suspicious events = %v, want 15", metric.Counter.GetValue())
	}

	t.Log("✓ Realistic security monitoring scenario completed successfully")
	t.Logf("  - Auth failures: 25 (CRITICAL: >20/sec threshold)")
	t.Logf("  - Suspicious events: 15 (WARNING threshold)")
	t.Logf("  - Key rotation: %d days overdue", rotationAge/(24*3600))
	t.Logf("  - TLS cert expires in 30 days (WARNING)")
}

// BenchmarkSecurityMetricIncrement benchmarks incrementing security counters
func BenchmarkSecurityMetricIncrement(b *testing.B) {
	r := NewRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.AuthFailuresTotal.Inc()
	}
}

// BenchmarkSecurityMetricGaugeSet benchmarks setting security gauges
func BenchmarkSecurityMetricGaugeSet(b *testing.B) {
	r := NewRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.SecurityEncryptionEnabled.Set(1)
	}
}

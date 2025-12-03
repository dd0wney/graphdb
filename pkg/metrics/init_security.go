package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initSecurityMetrics() {
	r.AuthFailuresTotal = promauto.With(r.registry).NewCounter(
		prometheus.CounterOpts{
			Name: "graphdb_auth_failures_total",
			Help: "Total number of authentication failures",
		},
	)

	r.SecurityEncryptionEnabled = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_security_encryption_enabled",
			Help: "Whether encryption is enabled (1=yes, 0=no)",
		},
	)

	r.SecurityKeyLastRotationTimestamp = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_security_key_last_rotation_timestamp_seconds",
			Help: "Timestamp of the last encryption key rotation as Unix timestamp",
		},
	)

	r.SecurityTLSEnabled = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_security_tls_enabled",
			Help: "Whether TLS is enabled (1=yes, 0=no)",
		},
	)

	r.SecurityTLSCertExpiryTimestamp = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_security_tls_cert_expiry_timestamp_seconds",
			Help: "TLS certificate expiration time as Unix timestamp",
		},
	)

	r.SecurityAuditExportFailuresTotal = promauto.With(r.registry).NewCounter(
		prometheus.CounterOpts{
			Name: "graphdb_security_audit_export_failures_total",
			Help: "Total number of audit log export failures",
		},
	)

	r.SecuritySuspiciousEventsTotal = promauto.With(r.registry).NewCounter(
		prometheus.CounterOpts{
			Name: "graphdb_security_suspicious_events_total",
			Help: "Total number of suspicious security events detected",
		},
	)

	r.SecurityUnauthorizedAccessTotal = promauto.With(r.registry).NewCounter(
		prometheus.CounterOpts{
			Name: "graphdb_security_unauthorized_access_total",
			Help: "Total number of unauthorized access attempts",
		},
	)

	r.SecurityHealthStatus = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_security_health_status",
			Help: "Security health status (1=healthy, 0=unhealthy)",
		},
	)
}

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initLicensingMetrics() {
	r.LicenseValid = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_license_valid",
			Help: "Whether the license is currently valid (1=yes, 0=no)",
		},
	)

	r.LicenseExpiresAt = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_license_expires_at_timestamp_seconds",
			Help: "License expiration time as Unix timestamp",
		},
	)

	r.LicenseValidationErrors = promauto.With(r.registry).NewCounter(
		prometheus.CounterOpts{
			Name: "graphdb_license_validation_errors_total",
			Help: "Total number of license validation errors",
		},
	)
}

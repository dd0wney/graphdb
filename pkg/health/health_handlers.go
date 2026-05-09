package health

import (
	"encoding/json"
	"log"
	"net/http"
)

// HTTPHandler returns an HTTP handler for the health check endpoint
func (hc *HealthChecker) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := hc.Check()

		w.Header().Set("Content-Type", "application/json")

		// Set appropriate HTTP status code
		switch response.Status {
		case StatusHealthy:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK) // 200 but degraded
		case StatusUnhealthy:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		// Encode error after WriteHeader is unrecoverable; log and move on.
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("health: encode response failed: %v", err)
		}
	}
}

// ReadinessHandler returns an HTTP handler for readiness checks
func (hc *HealthChecker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := hc.CheckReadiness()

		w.Header().Set("Content-Type", "application/json")

		// Readiness is binary - either ready or not
		if response.Status == StatusHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		// Encode error after WriteHeader is unrecoverable; log and move on.
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("health: encode response failed: %v", err)
		}
	}
}

// LivenessHandler returns an HTTP handler for liveness checks
func (hc *HealthChecker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := hc.CheckLiveness()

		w.Header().Set("Content-Type", "application/json")

		// Liveness is binary - either alive or not
		if response.Status == StatusHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		// Encode error after WriteHeader is unrecoverable; log and move on.
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("health: encode response failed: %v", err)
		}
	}
}

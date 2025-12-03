package health

import (
	"time"
)

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		checks:      make(map[string]CheckFunc),
		readyChecks: make(map[string]CheckFunc),
		liveChecks:  make(map[string]CheckFunc),
	}
}

// RegisterCheck registers a health check
func (hc *HealthChecker) RegisterCheck(name string, check CheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = check
}

// RegisterReadinessCheck registers a readiness check
func (hc *HealthChecker) RegisterReadinessCheck(name string, check CheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.readyChecks[name] = check
}

// RegisterLivenessCheck registers a liveness check
func (hc *HealthChecker) RegisterLivenessCheck(name string, check CheckFunc) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.liveChecks[name] = check
}

// Check performs all health checks
func (hc *HealthChecker) Check() Response {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.performChecks(hc.checks)
}

// CheckReadiness performs readiness checks
func (hc *HealthChecker) CheckReadiness() Response {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.performChecks(hc.readyChecks)
}

// CheckLiveness performs liveness checks
func (hc *HealthChecker) CheckLiveness() Response {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.performChecks(hc.liveChecks)
}

func (hc *HealthChecker) performChecks(checksMap map[string]CheckFunc) Response {
	response := Response{
		Status:    StatusHealthy,
		Timestamp: time.Now(),
		Checks:    make(map[string]Check),
	}

	for name, checkFunc := range checksMap {
		start := time.Now()
		check := checkFunc()
		check.Duration = time.Since(start)
		check.LastChecked = start

		response.Checks[name] = check

		// Determine overall status (worst status wins)
		if check.Status == StatusUnhealthy {
			response.Status = StatusUnhealthy
		} else if check.Status == StatusDegraded && response.Status != StatusUnhealthy {
			response.Status = StatusDegraded
		}
	}

	return response
}

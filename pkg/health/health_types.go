package health

import (
	"sync"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// Check represents a health check for a specific component
type Check struct {
	Name        string         `json:"name"`
	Status      Status         `json:"status"`
	Message     string         `json:"message,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	LastChecked time.Time      `json:"last_checked"`
	Duration    time.Duration  `json:"duration_ms"`
}

// CheckFunc is a function that performs a health check
type CheckFunc func() Check

// HealthChecker manages health checks for the application
type HealthChecker struct {
	checks      map[string]CheckFunc
	mu          sync.RWMutex
	readyChecks map[string]CheckFunc // Checks for readiness
	liveChecks  map[string]CheckFunc // Checks for liveness
}

// Response represents the overall health response
type Response struct {
	Status    Status           `json:"status"`
	Timestamp time.Time        `json:"timestamp"`
	Checks    map[string]Check `json:"checks"`
	Uptime    time.Duration    `json:"uptime_seconds"`
}

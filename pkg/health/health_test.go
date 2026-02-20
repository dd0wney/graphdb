package health

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHealthChecker(t *testing.T) {
	hc := NewHealthChecker()

	if hc == nil {
		t.Fatal("NewHealthChecker returned nil")
	}
	if hc.checks == nil {
		t.Error("checks map not initialized")
	}
	if hc.readyChecks == nil {
		t.Error("readyChecks map not initialized")
	}
	if hc.liveChecks == nil {
		t.Error("liveChecks map not initialized")
	}
}

func TestRegisterCheck(t *testing.T) {
	hc := NewHealthChecker()

	called := false
	hc.RegisterCheck("test", func() Check {
		called = true
		return Check{Status: StatusHealthy}
	})

	resp := hc.Check()
	if !called {
		t.Error("registered check was not called")
	}
	if _, exists := resp.Checks["test"]; !exists {
		t.Error("check result not in response")
	}
}

func TestRegisterReadinessCheck(t *testing.T) {
	hc := NewHealthChecker()

	called := false
	hc.RegisterReadinessCheck("ready-test", func() Check {
		called = true
		return Check{Status: StatusHealthy}
	})

	// Should not be called for regular Check()
	hc.Check()
	if called {
		t.Error("readiness check should not be called for Check()")
	}

	// Should be called for CheckReadiness()
	resp := hc.CheckReadiness()
	if !called {
		t.Error("readiness check was not called")
	}
	if _, exists := resp.Checks["ready-test"]; !exists {
		t.Error("readiness check result not in response")
	}
}

func TestRegisterLivenessCheck(t *testing.T) {
	hc := NewHealthChecker()

	called := false
	hc.RegisterLivenessCheck("live-test", func() Check {
		called = true
		return Check{Status: StatusHealthy}
	})

	// Should not be called for regular Check()
	hc.Check()
	if called {
		t.Error("liveness check should not be called for Check()")
	}

	// Should be called for CheckLiveness()
	resp := hc.CheckLiveness()
	if !called {
		t.Error("liveness check was not called")
	}
	if _, exists := resp.Checks["live-test"]; !exists {
		t.Error("liveness check result not in response")
	}
}

func TestCheckStatusAggregation(t *testing.T) {
	tests := []struct {
		name           string
		checkStatuses  []Status
		expectedStatus Status
	}{
		{
			name:           "all healthy",
			checkStatuses:  []Status{StatusHealthy, StatusHealthy, StatusHealthy},
			expectedStatus: StatusHealthy,
		},
		{
			name:           "one degraded",
			checkStatuses:  []Status{StatusHealthy, StatusDegraded, StatusHealthy},
			expectedStatus: StatusDegraded,
		},
		{
			name:           "one unhealthy",
			checkStatuses:  []Status{StatusHealthy, StatusUnhealthy, StatusHealthy},
			expectedStatus: StatusUnhealthy,
		},
		{
			name:           "degraded and unhealthy",
			checkStatuses:  []Status{StatusDegraded, StatusUnhealthy, StatusHealthy},
			expectedStatus: StatusUnhealthy,
		},
		{
			name:           "no checks",
			checkStatuses:  []Status{},
			expectedStatus: StatusHealthy,
		},
		{
			name:           "single healthy",
			checkStatuses:  []Status{StatusHealthy},
			expectedStatus: StatusHealthy,
		},
		{
			name:           "single unhealthy",
			checkStatuses:  []Status{StatusUnhealthy},
			expectedStatus: StatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHealthChecker()

			for i, status := range tt.checkStatuses {
				s := status // capture
				hc.RegisterCheck(string(rune('a'+i)), func() Check {
					return Check{Status: s}
				})
			}

			resp := hc.Check()
			if resp.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, resp.Status)
			}
		})
	}
}

func TestCheckTimestamp(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterCheck("test", func() Check {
		return Check{Status: StatusHealthy}
	})

	before := time.Now()
	resp := hc.Check()
	after := time.Now()

	if resp.Timestamp.Before(before) || resp.Timestamp.After(after) {
		t.Errorf("timestamp %v not between %v and %v", resp.Timestamp, before, after)
	}
}

func TestCheckDuration(t *testing.T) {
	hc := NewHealthChecker()

	sleepDuration := 10 * time.Millisecond
	hc.RegisterCheck("slow", func() Check {
		time.Sleep(sleepDuration)
		return Check{Status: StatusHealthy}
	})

	resp := hc.Check()
	check := resp.Checks["slow"]

	if check.Duration < sleepDuration {
		t.Errorf("duration %v less than sleep time %v", check.Duration, sleepDuration)
	}
}

func TestSimpleCheck(t *testing.T) {
	check := SimpleCheck("test-component")

	if check.Name != "test-component" {
		t.Errorf("expected name 'test-component', got %s", check.Name)
	}
	if check.Status != StatusHealthy {
		t.Errorf("expected status healthy, got %s", check.Status)
	}
	if check.LastChecked.IsZero() {
		t.Error("LastChecked not set")
	}
}

func TestDatabaseCheck(t *testing.T) {
	tests := []struct {
		name           string
		pingErr        error
		expectedStatus Status
		expectedMsg    string
	}{
		{
			name:           "connected",
			pingErr:        nil,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Connected",
		},
		{
			name:           "connection error",
			pingErr:        errors.New("connection refused"),
			expectedStatus: StatusUnhealthy,
			expectedMsg:    "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkFunc := DatabaseCheck(func() error {
				return tt.pingErr
			})

			check := checkFunc()

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, check.Status)
			}
			if check.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, check.Message)
			}
			if check.Name != "database" {
				t.Errorf("expected name 'database', got %s", check.Name)
			}
		})
	}
}

func TestReplicationCheck(t *testing.T) {
	tests := []struct {
		name           string
		connected      bool
		lag            int64
		replicas       int
		expectedStatus Status
		expectedMsg    string
	}{
		{
			name:           "standalone mode",
			connected:      false,
			lag:            0,
			replicas:       0,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Standalone mode",
		},
		{
			name:           "not connected to primary",
			connected:      false,
			lag:            0,
			replicas:       2,
			expectedStatus: StatusUnhealthy,
			expectedMsg:    "Not connected to primary",
		},
		{
			name:           "high replication lag",
			connected:      true,
			lag:            5000,
			replicas:       2,
			expectedStatus: StatusDegraded,
			expectedMsg:    "High replication lag",
		},
		{
			name:           "healthy replication",
			connected:      true,
			lag:            100,
			replicas:       2,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Replication healthy",
		},
		{
			name:           "lag at threshold",
			connected:      true,
			lag:            1000,
			replicas:       1,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Replication healthy",
		},
		{
			name:           "lag just over threshold",
			connected:      true,
			lag:            1001,
			replicas:       1,
			expectedStatus: StatusDegraded,
			expectedMsg:    "High replication lag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkFunc := ReplicationCheck(func() (bool, int64, int) {
				return tt.connected, tt.lag, tt.replicas
			})

			check := checkFunc()

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, check.Status)
			}
			if check.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, check.Message)
			}
			if check.Details["connected"] != tt.connected {
				t.Errorf("expected connected=%v in details", tt.connected)
			}
			if check.Details["lag_lsn"] != tt.lag {
				t.Errorf("expected lag_lsn=%d in details", tt.lag)
			}
		})
	}
}

func TestClusterCheck(t *testing.T) {
	tests := []struct {
		name           string
		hasQuorum      bool
		healthyNodes   int
		totalNodes     int
		expectedStatus Status
		expectedMsg    string
	}{
		{
			name:           "cluster disabled",
			hasQuorum:      false,
			healthyNodes:   0,
			totalNodes:     0,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Cluster mode disabled",
		},
		{
			name:           "no quorum",
			hasQuorum:      false,
			healthyNodes:   1,
			totalNodes:     3,
			expectedStatus: StatusUnhealthy,
			expectedMsg:    "No quorum",
		},
		{
			name:           "some nodes unhealthy",
			hasQuorum:      true,
			healthyNodes:   2,
			totalNodes:     3,
			expectedStatus: StatusDegraded,
			expectedMsg:    "Some nodes unhealthy",
		},
		{
			name:           "all healthy",
			hasQuorum:      true,
			healthyNodes:   3,
			totalNodes:     3,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Cluster healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkFunc := ClusterCheck(func() (bool, int, int) {
				return tt.hasQuorum, tt.healthyNodes, tt.totalNodes
			})

			check := checkFunc()

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, check.Status)
			}
			if check.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, check.Message)
			}
		})
	}
}

func TestDiskSpaceCheck(t *testing.T) {
	tests := []struct {
		name           string
		used           uint64
		total          uint64
		expectedStatus Status
		expectedMsg    string
	}{
		{
			name:           "sufficient space",
			used:           50,
			total:          100,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Sufficient disk space",
		},
		{
			name:           "low space (80%)",
			used:           80,
			total:          100,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Sufficient disk space",
		},
		{
			name:           "low space (81%)",
			used:           81,
			total:          100,
			expectedStatus: StatusDegraded,
			expectedMsg:    "Low disk space",
		},
		{
			name:           "critical space (95%)",
			used:           95,
			total:          100,
			expectedStatus: StatusDegraded,
			expectedMsg:    "Low disk space",
		},
		{
			name:           "critical space (96%)",
			used:           96,
			total:          100,
			expectedStatus: StatusUnhealthy,
			expectedMsg:    "Critical disk space",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkFunc := DiskSpaceCheck(func() (uint64, uint64) {
				return tt.used, tt.total
			})

			check := checkFunc()

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, check.Status)
			}
			if check.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, check.Message)
			}
		})
	}
}

func TestMemoryCheck(t *testing.T) {
	tests := []struct {
		name           string
		alloc          uint64
		sys            uint64
		expectedStatus Status
		expectedMsg    string
	}{
		{
			name:           "normal usage",
			alloc:          50,
			sys:            100,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Memory usage normal",
		},
		{
			name:           "high usage (90%)",
			alloc:          90,
			sys:            100,
			expectedStatus: StatusHealthy,
			expectedMsg:    "Memory usage normal",
		},
		{
			name:           "high usage (91%)",
			alloc:          91,
			sys:            100,
			expectedStatus: StatusDegraded,
			expectedMsg:    "High memory usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkFunc := MemoryCheck(func() (uint64, uint64) {
				return tt.alloc, tt.sys
			})

			check := checkFunc()

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, check.Status)
			}
			if check.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, check.Message)
			}
		})
	}
}

func TestHTTPHandler(t *testing.T) {
	tests := []struct {
		name           string
		checkStatus    Status
		expectedCode   int
	}{
		{
			name:           "healthy returns 200",
			checkStatus:    StatusHealthy,
			expectedCode:   http.StatusOK,
		},
		{
			name:           "degraded returns 200",
			checkStatus:    StatusDegraded,
			expectedCode:   http.StatusOK,
		},
		{
			name:           "unhealthy returns 503",
			checkStatus:    StatusUnhealthy,
			expectedCode:   http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHealthChecker()
			hc.RegisterCheck("test", func() Check {
				return Check{Status: tt.checkStatus}
			})

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()

			hc.HTTPHandler()(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("expected status code %d, got %d", tt.expectedCode, rec.Code)
			}

			if rec.Header().Get("Content-Type") != "application/json" {
				t.Error("expected Content-Type application/json")
			}

			var resp Response
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Status != tt.checkStatus {
				t.Errorf("expected response status %s, got %s", tt.checkStatus, resp.Status)
			}
		})
	}
}

func TestReadinessHandler(t *testing.T) {
	tests := []struct {
		name           string
		checkStatus    Status
		expectedCode   int
	}{
		{
			name:           "healthy returns 200",
			checkStatus:    StatusHealthy,
			expectedCode:   http.StatusOK,
		},
		{
			name:           "degraded returns 503",
			checkStatus:    StatusDegraded,
			expectedCode:   http.StatusServiceUnavailable,
		},
		{
			name:           "unhealthy returns 503",
			checkStatus:    StatusUnhealthy,
			expectedCode:   http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHealthChecker()
			hc.RegisterReadinessCheck("test", func() Check {
				return Check{Status: tt.checkStatus}
			})

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()

			hc.ReadinessHandler()(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("expected status code %d, got %d", tt.expectedCode, rec.Code)
			}
		})
	}
}

func TestLivenessHandler(t *testing.T) {
	tests := []struct {
		name           string
		checkStatus    Status
		expectedCode   int
	}{
		{
			name:           "healthy returns 200",
			checkStatus:    StatusHealthy,
			expectedCode:   http.StatusOK,
		},
		{
			name:           "degraded returns 503",
			checkStatus:    StatusDegraded,
			expectedCode:   http.StatusServiceUnavailable,
		},
		{
			name:           "unhealthy returns 503",
			checkStatus:    StatusUnhealthy,
			expectedCode:   http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHealthChecker()
			hc.RegisterLivenessCheck("test", func() Check {
				return Check{Status: tt.checkStatus}
			})

			req := httptest.NewRequest(http.MethodGet, "/live", nil)
			rec := httptest.NewRecorder()

			hc.LivenessHandler()(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("expected status code %d, got %d", tt.expectedCode, rec.Code)
			}
		})
	}
}

func TestConcurrentCheckRegistration(t *testing.T) {
	hc := NewHealthChecker()

	// Register checks concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			hc.RegisterCheck(string(rune('a'+id)), func() Check {
				return Check{Status: StatusHealthy}
			})
			done <- true
		}(i)
	}

	// Wait for all registrations
	for i := 0; i < 10; i++ {
		<-done
	}

	// Run checks concurrently
	for i := 0; i < 10; i++ {
		go func() {
			hc.Check()
			done <- true
		}()
	}

	// Wait for all checks
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all checks registered
	resp := hc.Check()
	if len(resp.Checks) != 10 {
		t.Errorf("expected 10 checks, got %d", len(resp.Checks))
	}
}

func TestResponseJSONSerialization(t *testing.T) {
	hc := NewHealthChecker()
	hc.RegisterCheck("test", func() Check {
		return Check{
			Status:  StatusHealthy,
			Message: "All good",
			Details: map[string]any{
				"version": "1.0.0",
				"count":   42,
			},
		}
	})

	resp := hc.Check()

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if decoded.Status != resp.Status {
		t.Errorf("status mismatch: expected %s, got %s", resp.Status, decoded.Status)
	}

	check, exists := decoded.Checks["test"]
	if !exists {
		t.Fatal("check 'test' not found in decoded response")
	}

	if check.Message != "All good" {
		t.Errorf("message mismatch: expected 'All good', got %s", check.Message)
	}
}

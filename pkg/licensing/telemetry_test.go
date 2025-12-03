package licensing

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestNewTelemetryReporter tests telemetry reporter construction
func TestNewTelemetryReporter(t *testing.T) {
	reporter := NewTelemetryReporter(true, 1*time.Minute)

	if reporter == nil {
		t.Fatal("Expected reporter, got nil")
	}

	if reporter.endpoint != TelemetryEndpoint {
		t.Errorf("Expected endpoint %s, got %s", TelemetryEndpoint, reporter.endpoint)
	}

	if reporter.interval != 1*time.Minute {
		t.Errorf("Expected interval 1m, got %v", reporter.interval)
	}

	if !reporter.enabled {
		t.Error("Expected reporter to be enabled")
	}

	if reporter.httpClient == nil {
		t.Error("Expected HTTP client to be initialized")
	}

	if reporter.httpClient.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", reporter.httpClient.Timeout)
	}

	if reporter.ctx == nil {
		t.Error("Expected context to be initialized")
	}

	if reporter.cancel == nil {
		t.Error("Expected cancel function to be initialized")
	}

	// Clean up
	reporter.Stop()
}

// TestTelemetryReporter_Stop tests reporter shutdown
func TestTelemetryReporter_Stop(t *testing.T) {
	reporter := NewTelemetryReporter(true, 1*time.Minute)

	// Check context is not done initially
	select {
	case <-reporter.ctx.Done():
		t.Error("Context should not be done initially")
	default:
	}

	// Stop the reporter
	reporter.Stop()

	// Check context is done after stop
	select {
	case <-reporter.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be done after Stop()")
	}

	// Multiple calls to Stop should be safe
	reporter.Stop()
	reporter.Stop()
}

// TestTelemetryReporter_Start_Disabled tests that disabled reporter doesn't send
func TestTelemetryReporter_Start_Disabled(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(false, 50*time.Millisecond)
	reporter.endpoint = server.URL
	defer reporter.Stop()

	license := &License{Key: "test-key"}
	getMetrics := func() (int64, int64) { return 10, 20 }

	reporter.Start(license, getMetrics)

	// Wait a bit to ensure no calls are made
	time.Sleep(200 * time.Millisecond)

	if serverCalled {
		t.Error("Disabled reporter should not send telemetry")
	}
}

// TestTelemetryReporter_Start_Success tests successful telemetry reporting
func TestTelemetryReporter_Start_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// Verify request method
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Expected Content-Type: application/json")
		}
		if r.Header.Get("User-Agent") != "GraphDB-Telemetry/1.0" {
			t.Error("Expected User-Agent: GraphDB-Telemetry/1.0")
		}

		// Read and verify body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}

		var data TelemetryData
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("Failed to unmarshal telemetry: %v", err)
		}

		// Verify data fields
		if data.LicenseHash == "" {
			t.Error("Expected non-empty license hash")
		}
		if data.GoVersion != runtime.Version() {
			t.Errorf("Expected GoVersion %s, got %s", runtime.Version(), data.GoVersion)
		}
		if data.OS != runtime.GOOS {
			t.Errorf("Expected OS %s, got %s", runtime.GOOS, data.OS)
		}
		if data.Arch != runtime.GOARCH {
			t.Errorf("Expected Arch %s, got %s", runtime.GOARCH, data.Arch)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, 100*time.Millisecond)
	reporter.endpoint = server.URL
	defer reporter.Stop()

	license := &License{Key: "test-key-12345"}
	getMetrics := func() (int64, int64) { return 100, 200 }

	reporter.Start(license, getMetrics)

	// Wait for at least 2 reports (initial + 1 periodic)
	time.Sleep(250 * time.Millisecond)

	if callCount < 2 {
		t.Errorf("Expected at least 2 calls, got %d", callCount)
	}
}

// TestTelemetryReporter_buildTelemetryData tests data construction
func TestTelemetryReporter_buildTelemetryData(t *testing.T) {
	reporter := NewTelemetryReporter(true, 1*time.Minute)
	defer reporter.Stop()

	license := &License{Key: "test-license-key"}
	getMetrics := func() (int64, int64) { return 50, 100 }

	data := reporter.buildTelemetryData(license, getMetrics, 3600)

	// Verify license hash (should be truncated SHA-256)
	if len(data.LicenseHash) != 16 {
		t.Errorf("Expected license hash length 16, got %d", len(data.LicenseHash))
	}
	if data.LicenseHash == "" {
		t.Error("Expected non-empty license hash")
	}

	// Verify system info
	if data.GoVersion != runtime.Version() {
		t.Errorf("Expected GoVersion %s, got %s", runtime.Version(), data.GoVersion)
	}
	if data.OS != runtime.GOOS {
		t.Errorf("Expected OS %s, got %s", runtime.GOOS, data.OS)
	}
	if data.Arch != runtime.GOARCH {
		t.Errorf("Expected Arch %s, got %s", runtime.GOARCH, data.Arch)
	}

	// Verify metrics
	if data.NodeCount != 50 {
		t.Errorf("Expected NodeCount 50, got %d", data.NodeCount)
	}
	if data.EdgeCount != 100 {
		t.Errorf("Expected EdgeCount 100, got %d", data.EdgeCount)
	}
	if data.Uptime != 3600 {
		t.Errorf("Expected Uptime 3600, got %d", data.Uptime)
	}

	// Verify deployment type is set
	validTypes := map[string]bool{"docker": true, "kubernetes": true, "binary": true}
	if !validTypes[data.DeploymentType] {
		t.Errorf("Invalid deployment type: %s", data.DeploymentType)
	}

	// Verify timestamp is recent
	if time.Since(data.Timestamp) > 5*time.Second {
		t.Error("Timestamp should be recent")
	}
}

// TestTelemetryReporter_buildTelemetryData_NilMetrics tests with nil getMetrics
func TestTelemetryReporter_buildTelemetryData_NilMetrics(t *testing.T) {
	reporter := NewTelemetryReporter(true, 1*time.Minute)
	defer reporter.Stop()

	license := &License{Key: "test-key"}

	data := reporter.buildTelemetryData(license, nil, 0)

	// Metrics should be zero when getMetrics is nil
	if data.NodeCount != 0 {
		t.Errorf("Expected NodeCount 0, got %d", data.NodeCount)
	}
	if data.EdgeCount != 0 {
		t.Errorf("Expected EdgeCount 0, got %d", data.EdgeCount)
	}
}

// TestTelemetryReporter_send_Success tests successful HTTP transmission
func TestTelemetryReporter_send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, 1*time.Minute)
	reporter.endpoint = server.URL
	defer reporter.Stop()

	data := &TelemetryData{
		LicenseHash: "abc123",
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Timestamp:   time.Now(),
	}

	err := reporter.send(data)
	if err != nil {
		t.Fatalf("send() failed: %v", err)
	}
}

// TestTelemetryReporter_send_Accepted tests 202 Accepted response
func TestTelemetryReporter_send_Accepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, 1*time.Minute)
	reporter.endpoint = server.URL
	defer reporter.Stop()

	data := &TelemetryData{
		LicenseHash: "abc123",
		Timestamp:   time.Now(),
	}

	err := reporter.send(data)
	if err != nil {
		t.Fatalf("send() should accept 202, got error: %v", err)
	}
}

// TestTelemetryReporter_send_ServerError tests server error handling
func TestTelemetryReporter_send_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, 1*time.Minute)
	reporter.endpoint = server.URL
	defer reporter.Stop()

	data := &TelemetryData{
		LicenseHash: "abc123",
		Timestamp:   time.Now(),
	}

	err := reporter.send(data)
	if err == nil {
		t.Error("Expected error for 500 status code, got nil")
	}
}

// TestTelemetryReporter_send_NetworkError tests network error handling
func TestTelemetryReporter_send_NetworkError(t *testing.T) {
	reporter := NewTelemetryReporter(true, 1*time.Minute)
	reporter.endpoint = "http://invalid-host-that-does-not-exist.local:99999"
	defer reporter.Stop()

	data := &TelemetryData{
		LicenseHash: "abc123",
		Timestamp:   time.Now(),
	}

	err := reporter.send(data)
	if err == nil {
		t.Error("Expected error for invalid endpoint, got nil")
	}
}

// TestTelemetryReporter_send_ContextCanceled tests context cancellation
func TestTelemetryReporter_send_ContextCanceled(t *testing.T) {
	// Create slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, 1*time.Minute)
	reporter.endpoint = server.URL

	// Cancel context immediately
	reporter.Stop()

	data := &TelemetryData{
		LicenseHash: "abc123",
		Timestamp:   time.Now(),
	}

	err := reporter.send(data)
	if err == nil {
		t.Error("Expected error for canceled context, got nil")
	}
}

// TestHashString tests SHA-256 hashing
func TestHashString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "test",
			expected: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		},
		{
			input:    "hello world",
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			input:    "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := hashString(tt.input)
			if result != tt.expected {
				t.Errorf("hashString(%q) = %s, expected %s", tt.input, result, tt.expected)
			}
		})
	}
}

// TestDetectDeploymentType tests deployment type detection
func TestDetectDeploymentType(t *testing.T) {
	deploymentType := detectDeploymentType()

	// Should return one of the valid types
	validTypes := map[string]bool{"docker": true, "kubernetes": true, "binary": true}
	if !validTypes[deploymentType] {
		t.Errorf("Invalid deployment type: %s", deploymentType)
	}
}

// TestIsRunningInDocker tests Docker detection
func TestIsRunningInDocker(t *testing.T) {
	// This test is environment-dependent
	// Just verify it returns a boolean without panicking
	result := isRunningInDocker()

	// Result can be true or false depending on environment
	_ = result

	// If /.dockerenv exists, should return true
	if _, err := os.Stat("/.dockerenv"); err == nil {
		if !result {
			t.Error("Expected true when /.dockerenv exists")
		}
	}
}

// TestIsRunningInKubernetes tests Kubernetes detection
func TestIsRunningInKubernetes(t *testing.T) {
	// Save original environment
	origEnv := os.Getenv("KUBERNETES_SERVICE_HOST")
	defer func() {
		if origEnv != "" {
			os.Setenv("KUBERNETES_SERVICE_HOST", origEnv)
		} else {
			os.Unsetenv("KUBERNETES_SERVICE_HOST")
		}
	}()

	// Test with env var unset
	os.Unsetenv("KUBERNETES_SERVICE_HOST")

	result := isRunningInKubernetes()

	// If service account token exists, should return true
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		if !result {
			t.Error("Expected true when K8s service account token exists")
		}
	}

	// Test with env var set
	os.Setenv("KUBERNETES_SERVICE_HOST", "kubernetes.default.svc")
	result = isRunningInKubernetes()
	if !result {
		t.Error("Expected true when KUBERNETES_SERVICE_HOST is set")
	}
}

// TestReportOnce tests one-time telemetry reporting
func TestReportOnce(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// Read and verify body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}

		var data TelemetryData
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("Failed to unmarshal telemetry: %v", err)
		}

		// Verify version was set
		if data.Version != "v1.0.0-test" {
			t.Errorf("Expected version v1.0.0-test, got %s", data.Version)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Override endpoint
	originalEndpoint := TelemetryEndpoint
	TelemetryEndpoint = server.URL
	defer func() { TelemetryEndpoint = originalEndpoint }()

	license := &License{Key: "test-key-once"}

	err := ReportOnce(license, "v1.0.0-test")
	if err != nil {
		t.Fatalf("ReportOnce failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected exactly 1 call, got %d", callCount)
	}
}

// TestReportOnce_Error tests ReportOnce error handling
func TestReportOnce_Error(t *testing.T) {
	// Override endpoint to invalid URL
	originalEndpoint := TelemetryEndpoint
	TelemetryEndpoint = "http://invalid-host.local:99999"
	defer func() { TelemetryEndpoint = originalEndpoint }()

	license := &License{Key: "test-key"}

	err := ReportOnce(license, "v1.0.0")
	if err == nil {
		t.Error("Expected error for invalid endpoint, got nil")
	}
}

// TestTelemetryReporter_reportLoop_Cancellation tests clean shutdown
func TestTelemetryReporter_reportLoop_Cancellation(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := NewTelemetryReporter(true, 50*time.Millisecond)
	reporter.endpoint = server.URL

	license := &License{Key: "test-key"}
	getMetrics := func() (int64, int64) { return 10, 20 }

	reporter.Start(license, getMetrics)

	// Let it run for a bit
	time.Sleep(150 * time.Millisecond)

	// Stop the reporter
	reporter.Stop()

	// Wait a bit more to ensure it stopped
	prevCount := callCount
	time.Sleep(150 * time.Millisecond)

	// Call count should not increase after Stop()
	if callCount > prevCount {
		t.Error("Reporter should not send after Stop()")
	}
}

// TestTelemetryData_JSONMarshaling tests JSON serialization
func TestTelemetryData_JSONMarshaling(t *testing.T) {
	timestamp := time.Date(2024, 11, 19, 10, 30, 0, 0, time.UTC)

	data := &TelemetryData{
		LicenseHash:    "abc123def456",
		Version:        "v1.0.0",
		GoVersion:      "go1.21.0",
		OS:             "linux",
		Arch:           "amd64",
		NodeCount:      1000,
		EdgeCount:      2000,
		Uptime:         3600,
		DeploymentType: "kubernetes",
		Timestamp:      timestamp,
		InstallationID: "install-123",
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var unmarshaled TelemetryData
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields
	if unmarshaled.LicenseHash != data.LicenseHash {
		t.Errorf("LicenseHash mismatch: %s != %s", unmarshaled.LicenseHash, data.LicenseHash)
	}
	if unmarshaled.Version != data.Version {
		t.Errorf("Version mismatch: %s != %s", unmarshaled.Version, data.Version)
	}
	if unmarshaled.NodeCount != data.NodeCount {
		t.Errorf("NodeCount mismatch: %d != %d", unmarshaled.NodeCount, data.NodeCount)
	}
}

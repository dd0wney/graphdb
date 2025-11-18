package licensing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"
)

// TelemetryEndpoint is the URL where telemetry data is sent
// Override this in production to point to your telemetry server
var TelemetryEndpoint = "https://telemetry.graphdb.io/v1/report"

// TelemetryData represents anonymous usage metrics
// All data is anonymized - no personal or identifying information is sent
type TelemetryData struct {
	// Anonymous license hash (SHA-256 of license key)
	LicenseHash string `json:"license_hash"`

	// Version information
	Version    string `json:"version"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`

	// Usage metrics (anonymous)
	NodeCount     int64  `json:"node_count,omitempty"`
	EdgeCount     int64  `json:"edge_count,omitempty"`
	Uptime        int64  `json:"uptime_seconds,omitempty"`

	// Deployment type
	DeploymentType string `json:"deployment_type,omitempty"` // docker, binary, kubernetes

	// Timestamp
	Timestamp time.Time `json:"timestamp"`

	// Installation ID (anonymous hash of hardware fingerprint)
	InstallationID string `json:"installation_id,omitempty"`
}

// TelemetryReporter handles periodic telemetry reporting
type TelemetryReporter struct {
	endpoint   string
	interval   time.Duration
	httpClient *http.Client
	enabled    bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewTelemetryReporter creates a new telemetry reporter
func NewTelemetryReporter(enabled bool, interval time.Duration) *TelemetryReporter {
	ctx, cancel := context.WithCancel(context.Background())

	return &TelemetryReporter{
		endpoint:   TelemetryEndpoint,
		interval:   interval,
		enabled:    enabled,
		ctx:        ctx,
		cancel:     cancel,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start begins periodic telemetry reporting
func (t *TelemetryReporter) Start(license *License, getMetrics func() (nodeCount, edgeCount int64)) {
	if !t.enabled {
		return
	}

	go t.reportLoop(license, getMetrics)
}

// Stop halts telemetry reporting
func (t *TelemetryReporter) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

// reportLoop sends telemetry data at regular intervals
func (t *TelemetryReporter) reportLoop(license *License, getMetrics func() (nodeCount, edgeCount int64)) {
	// Send initial report immediately
	t.sendReport(license, getMetrics, 0)

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			uptime := int64(time.Since(startTime).Seconds())
			t.sendReport(license, getMetrics, uptime)
		}
	}
}

// sendReport sends a single telemetry report
func (t *TelemetryReporter) sendReport(license *License, getMetrics func() (nodeCount, edgeCount int64), uptime int64) {
	data := t.buildTelemetryData(license, getMetrics, uptime)

	if err := t.send(data); err != nil {
		// Log error but don't fail - telemetry is best-effort
		// In production, you might want to log this to a file
		_ = err
	}
}

// buildTelemetryData constructs the telemetry payload
func (t *TelemetryReporter) buildTelemetryData(license *License, getMetrics func() (nodeCount, edgeCount int64), uptime int64) *TelemetryData {
	// Create anonymous hash of license key
	licenseHash := hashString(license.Key)

	// Get hardware fingerprint for installation ID
	installationID := ""
	if fp, err := GenerateFingerprint(); err == nil {
		installationID = fp.Hash[:16] // Use first 16 chars for brevity
	}

	// Get current metrics
	nodeCount, edgeCount := int64(0), int64(0)
	if getMetrics != nil {
		nodeCount, edgeCount = getMetrics()
	}

	// Detect deployment type
	deploymentType := detectDeploymentType()

	return &TelemetryData{
		LicenseHash:    licenseHash[:16], // Truncate for brevity
		Version:        "", // Set by caller
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		NodeCount:      nodeCount,
		EdgeCount:      edgeCount,
		Uptime:         uptime,
		DeploymentType: deploymentType,
		Timestamp:      time.Now().UTC(),
		InstallationID: installationID,
	}
}

// send transmits telemetry data to the endpoint
func (t *TelemetryReporter) send(data *TelemetryData) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal telemetry: %w", err)
	}

	req, err := http.NewRequestWithContext(t.ctx, "POST", t.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GraphDB-Telemetry/1.0")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telemetry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("telemetry server returned status: %d", resp.StatusCode)
	}

	return nil
}

// hashString creates a SHA-256 hash of a string
func hashString(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

// detectDeploymentType attempts to determine how GraphDB is deployed
func detectDeploymentType() string {
	// Check for Docker
	if isRunningInDocker() {
		return "docker"
	}

	// Check for Kubernetes
	if isRunningInKubernetes() {
		return "kubernetes"
	}

	return "binary"
}

// isRunningInDocker checks if we're running inside a Docker container
func isRunningInDocker() bool {
	// Check for .dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check cgroup (fallback)
	// This is a simple check - in production you might want more robust detection
	return false
}

// isRunningInKubernetes checks if we're running inside a Kubernetes pod
func isRunningInKubernetes() bool {
	// Check for Kubernetes service account token
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return true
	}

	// Check for KUBERNETES_SERVICE_HOST environment variable
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	return false
}

// ReportOnce sends a single telemetry report without starting the periodic reporter
// Useful for one-time events like startup or shutdown
func ReportOnce(license *License, version string) error {
	reporter := NewTelemetryReporter(true, 0)

	data := reporter.buildTelemetryData(license, nil, 0)
	data.Version = version

	return reporter.send(data)
}

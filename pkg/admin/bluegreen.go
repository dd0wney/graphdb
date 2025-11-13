package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// BlueGreenManager manages blue-green deployments
type BlueGreenManager struct {
	currentColor  string // "blue" or "green"
	activePort    int
	standbyPort   int
	mu            sync.RWMutex
	healthChecker *HealthChecker
}

// DeploymentColor represents a deployment environment
type DeploymentColor struct {
	Color       string    `json:"color"`
	Port        int       `json:"port"`
	Version     string    `json:"version"`
	Active      bool      `json:"active"`
	Healthy     bool      `json:"healthy"`
	NodeCount   uint64    `json:"node_count"`
	EdgeCount   uint64    `json:"edge_count"`
	LastChecked time.Time `json:"last_checked"`
}

// BlueGreenStatus represents the current blue-green deployment status
type BlueGreenStatus struct {
	CurrentActive string            `json:"current_active"`
	Blue          DeploymentColor   `json:"blue"`
	Green         DeploymentColor   `json:"green"`
	CanSwitch     bool              `json:"can_switch"`
	SwitchMessage string            `json:"switch_message,omitempty"`
}

// SwitchRequest represents a request to switch active deployment
type SwitchRequest struct {
	TargetColor string        `json:"target_color"` // "blue" or "green"
	Timeout     time.Duration `json:"timeout"`
	DrainTime   time.Duration `json:"drain_time"` // Time to drain existing connections
}

// SwitchResponse represents the result of a deployment switch
type SwitchResponse struct {
	Success       bool      `json:"success"`
	PreviousColor string    `json:"previous_color"`
	NewColor      string    `json:"new_color"`
	Message       string    `json:"message"`
	SwitchedAt    time.Time `json:"switched_at"`
}

// HealthChecker performs health checks on deployments
type HealthChecker struct {
	checkEndpoint string
	timeout       time.Duration
}

// NewBlueGreenManager creates a new blue-green deployment manager
func NewBlueGreenManager(initialColor string, bluePort, greenPort int) *BlueGreenManager {
	return &BlueGreenManager{
		currentColor: initialColor,
		activePort:   getPortForColor(initialColor, bluePort, greenPort),
		standbyPort:  getPortForColor(opposite(initialColor), bluePort, greenPort),
		healthChecker: &HealthChecker{
			checkEndpoint: "/health",
			timeout:       5 * time.Second,
		},
	}
}

// GetStatus returns the current blue-green deployment status
func (bgm *BlueGreenManager) GetStatus() BlueGreenStatus {
	bgm.mu.RLock()
	defer bgm.mu.RUnlock()

	status := BlueGreenStatus{
		CurrentActive: bgm.currentColor,
	}

	// Check both deployments
	status.Blue = bgm.checkDeployment("blue")
	status.Green = bgm.checkDeployment("green")

	// Determine if we can switch
	standbyColor := opposite(bgm.currentColor)
	standbyDeployment := status.Blue
	if standbyColor == "green" {
		standbyDeployment = status.Green
	}

	status.CanSwitch = standbyDeployment.Healthy
	if !status.CanSwitch {
		status.SwitchMessage = fmt.Sprintf("Standby deployment (%s) is not healthy", standbyColor)
	}

	return status
}

// Switch switches the active deployment from current to target color
func (bgm *BlueGreenManager) Switch(ctx context.Context, req SwitchRequest) (*SwitchResponse, error) {
	bgm.mu.Lock()
	defer bgm.mu.Unlock()

	response := &SwitchResponse{
		PreviousColor: bgm.currentColor,
		SwitchedAt:    time.Now(),
	}

	// Validate target color
	if req.TargetColor != "blue" && req.TargetColor != "green" {
		response.Success = false
		response.Message = "target_color must be 'blue' or 'green'"
		return response, fmt.Errorf("invalid target color: %s", req.TargetColor)
	}

	// Check if already on target
	if bgm.currentColor == req.TargetColor {
		response.Success = true
		response.NewColor = req.TargetColor
		response.Message = fmt.Sprintf("Already running on %s deployment", req.TargetColor)
		return response, nil
	}

	// Verify standby deployment is healthy
	standbyHealthy := bgm.healthChecker.Check(bgm.standbyPort)
	if !standbyHealthy {
		response.Success = false
		response.Message = fmt.Sprintf("Standby deployment (%s) failed health check", req.TargetColor)
		return response, fmt.Errorf("standby deployment unhealthy")
	}

	// Drain existing connections
	if req.DrainTime > 0 {
		log.Printf("Draining connections for %v...", req.DrainTime)
		time.Sleep(req.DrainTime)
	}

	// Perform the switch
	bgm.currentColor = req.TargetColor
	oldStandby := bgm.standbyPort
	bgm.standbyPort = bgm.activePort
	bgm.activePort = oldStandby

	response.Success = true
	response.NewColor = req.TargetColor
	response.Message = fmt.Sprintf("Successfully switched from %s to %s", response.PreviousColor, req.TargetColor)

	log.Printf("Blue-Green switch complete: %s -> %s (active port: %d)",
		response.PreviousColor, response.NewColor, bgm.activePort)

	return response, nil
}

func (bgm *BlueGreenManager) checkDeployment(color string) DeploymentColor {
	port := bgm.getPortForColorInternal(color)

	deployment := DeploymentColor{
		Color:       color,
		Port:        port,
		Active:      color == bgm.currentColor,
		LastChecked: time.Now(),
	}

	// Perform health check
	deployment.Healthy = bgm.healthChecker.Check(port)

	// Fetch version from health endpoint
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if version, err := bgm.healthChecker.FetchVersion(baseURL); err == nil {
		deployment.Version = version
	} else {
		deployment.Version = "unknown"
	}

	return deployment
}

func (bgm *BlueGreenManager) getPortForColorInternal(color string) int {
	if color == bgm.currentColor {
		return bgm.activePort
	}
	return bgm.standbyPort
}

func getPortForColor(color string, bluePort, greenPort int) int {
	if color == "blue" {
		return bluePort
	}
	return greenPort
}

func opposite(color string) string {
	if color == "blue" {
		return "green"
	}
	return "blue"
}

// Check performs a health check on the given port
func (hc *HealthChecker) Check(port int) bool {
	client := &http.Client{
		Timeout: hc.timeout,
	}

	url := fmt.Sprintf("http://localhost:%d%s", port, hc.checkEndpoint)
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// FetchVersion fetches the version from a deployment's health endpoint
func (hc *HealthChecker) FetchVersion(baseURL string) (string, error) {
	client := &http.Client{
		Timeout: hc.timeout,
	}

	url := baseURL + hc.checkEndpoint
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch health endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("health endpoint returned status %d", resp.StatusCode)
	}

	var healthResp struct {
		Version string `json:"version"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return "", fmt.Errorf("failed to decode health response: %w", err)
	}

	return healthResp.Version, nil
}

// RegisterHandlers registers blue-green HTTP handlers
func (bgm *BlueGreenManager) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/admin/bluegreen/status", bgm.handleStatus)
	mux.HandleFunc("/admin/bluegreen/switch", bgm.handleSwitch)
}

func (bgm *BlueGreenManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := bgm.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (bgm *BlueGreenManager) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SwitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Timeout == 0 {
		req.Timeout = 30 * time.Second
	}
	if req.DrainTime == 0 {
		req.DrainTime = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	response, err := bgm.Switch(ctx, req)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if response.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		log.Printf("Failed to encode switch response: %v", encErr)
	}
}

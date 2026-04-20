package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// suilClient buffers telemetry and flushes to the Súil backend.
// Disabled if SUIL_ENDPOINT and SUIL_API_KEY env vars are not set.
type suilClient struct {
	endpoint string
	apiKey   string
	project  string
	client   *http.Client

	mu      sync.Mutex
	logs    []suilLogEntry
	metrics []suilMetricEntry
}

type suilLogEntry struct {
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Timestamp string         `json:"timestamp"`
	Service   string         `json:"service,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type suilMetricEntry struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp string            `json:"timestamp,omitempty"`
}

type suilPayload struct {
	Project string            `json:"project"`
	Logs    []suilLogEntry    `json:"logs,omitempty"`
	Metrics []suilMetricEntry `json:"metrics,omitempty"`
}

// newSuilClient creates a client from environment variables.
// Returns nil if SUIL_ENDPOINT or SUIL_API_KEY are not set.
func newSuilClient() *suilClient {
	endpoint := os.Getenv("SUIL_ENDPOINT")
	apiKey := os.Getenv("SUIL_API_KEY")
	if endpoint == "" || apiKey == "" {
		return nil
	}

	log.Printf("Súil observability enabled → %s", endpoint)
	return &suilClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		project:  "graphdb",
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *suilClient) info(message string, fields map[string]any) {
	c.log("info", message, fields)
}

// logError is the error-level counterpart to info(). Kept for symmetry
// of the observability API surface; callers can escalate to this level
// when they introduce error-path instrumentation without re-implementing
// the wrapping.
//
//nolint:unused // observability API surface reserved for error-level callers
func (c *suilClient) logError(message string, fields map[string]any) {
	c.log("error", message, fields)
}

func (c *suilClient) log(level, message string, fields map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := suilLogEntry{
		Level:     level,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Extra:     fields,
	}
	if v, ok := fields["service"].(string); ok {
		entry.Service = v
	}
	if v, ok := fields["requestId"].(string); ok {
		entry.RequestID = v
	}
	c.logs = append(c.logs, entry)
}

func (c *suilClient) metric(name string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = append(c.metrics, suilMetricEntry{
		Name:      name,
		Value:     value,
		Labels:    labels,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (c *suilClient) flush() {
	c.mu.Lock()
	if len(c.logs) == 0 && len(c.metrics) == 0 {
		c.mu.Unlock()
		return
	}
	payload := suilPayload{
		Project: c.project,
		Logs:    c.logs,
		Metrics: c.metrics,
	}
	c.logs = nil
	c.metrics = nil
	c.mu.Unlock()

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint+"/api/v1/ingest", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// suilMiddleware instruments HTTP requests. No-op if client is nil.
func suilMiddleware(client *suilClient) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if client == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := r.Header.Get("X-Request-ID")

			client.info("request.start", map[string]any{
				"requestId": requestID,
				"method":    r.Method,
				"path":      r.URL.Path,
			})

			sw := &suilStatusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			duration := time.Since(start)
			client.info("request.end", map[string]any{
				"requestId": requestID,
				"status":    sw.status,
				"duration":  duration.Milliseconds(),
			})

			client.metric("http_request_duration_ms", float64(duration.Milliseconds()), map[string]string{
				"method": r.Method,
				"path":   r.URL.Path,
				"status": fmt.Sprintf("%d", sw.status),
			})

			go client.flush()
		})
	}
}

type suilStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *suilStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

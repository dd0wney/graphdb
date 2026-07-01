package graphdb

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiResult struct {
	data   json.RawMessage
	header http.Header
}

type transport struct {
	baseURL string
	http    *http.Client

	// static auth
	token  string
	apiKey string

	// login-based auth (Task 2 populates/uses these)
	username     string
	password     string
	refreshToken string

	maxRetries int
}

func (t *transport) authHeaders(h http.Header) {
	switch {
	case t.token != "":
		h.Set("Authorization", "Bearer "+t.token)
	case t.apiKey != "":
		h.Set("X-API-Key", t.apiKey)
	}
}

// request performs a JSON request with retry on retryable statuses. Auth-refresh
// on 401 is layered in by Task 2 (attemptWithRefresh).
func (t *transport) request(ctx context.Context, method, path string, body any, params url.Values) (*apiResult, error) {
	for attempt := 0; ; attempt++ {
		resp, err := t.attempt(ctx, method, path, body, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 && attempt < t.maxRetries && isRetryable(resp.StatusCode) {
			resp.Body.Close()
			time.Sleep(backoff(attempt))
			continue
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return nil, fromResponse(resp.StatusCode, data, method, path)
		}
		return &apiResult{data: data, header: resp.Header}, nil
	}
}

func (t *transport) attempt(ctx context.Context, method, path string, body any, params url.Values) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	u := strings.TrimRight(t.baseURL, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	t.authHeaders(req.Header)
	return t.http.Do(req)
}

func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func backoff(attempt int) time.Duration {
	d := time.Duration(100<<attempt) * time.Millisecond
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

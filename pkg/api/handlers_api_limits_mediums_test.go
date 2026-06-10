package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/audit"
)

// TestGraphQL_DepthLimitRejectsDeepQuery pins security audit finding M-3:
// /graphql validated complexity but never depth, so a deeply nested but
// narrow query (low complexity, high recursion) slipped through. The
// depth validator existed but was unwired.
//
// RED against pre-fix code: the deep query is not rejected for depth.
func TestGraphQL_DepthLimitRejectsDeepQuery(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Build a query nested well past maxGraphQLQueryDepth (10).
	const levels = 15
	deep := "{ " + strings.Repeat("a { ", levels) + "leaf " + strings.Repeat("} ", levels) + "}"

	body, _ := json.Marshal(map[string]string{"query": deep})
	r := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleGraphQL(rr, r)

	if !strings.Contains(strings.ToLower(rr.Body.String()), "depth") {
		t.Errorf("deep query (%d levels) not rejected for depth; body: %s", levels, rr.Body.String())
	}
}

// TestGraphQL_ShallowQueryNotDepthRejected pins that a normal-depth query
// is not rejected by the new depth guard (the control for M-3).
func TestGraphQL_ShallowQueryNotDepthRejected(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"query": "{ a { leaf } }"})
	r := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleGraphQL(rr, r)

	if strings.Contains(strings.ToLower(rr.Body.String()), "exceeds maximum allowed depth") {
		t.Errorf("shallow query wrongly rejected for depth; body: %s", rr.Body.String())
	}
}

// TestBodyLimitMiddleware_AuthPathTightCap pins security audit finding
// M-4: the /auth/* paths are skipped by inputValidationMiddleware, so
// before the fix they had no body bound at all — a large pre-auth POST
// to /auth/login was read whole. bodyLimitMiddleware caps every path,
// with a tight cap on /auth/*.
//
// RED against pre-fix code: there is no bodyLimitMiddleware in the chain,
// so an oversized /auth body is not rejected at the middleware layer.
func TestBodyLimitMiddleware_AuthPathTightCap(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	probe := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	wrapped := server.bodyLimitMiddleware(http.HandlerFunc(probe))

	cases := []struct {
		name string
		path string
		size int
		want int
	}{
		{"auth oversized", "/auth/login", maxAuthBodyBytes + 1, http.StatusRequestEntityTooLarge},
		{"auth within cap", "/auth/login", 256, http.StatusOK},
		{"general within general cap", "/nodes", maxAuthBodyBytes + 1, http.StatusOK},
		{"general oversized", "/nodes", maxGeneralBodyBytes + 1, http.StatusRequestEntityTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(make([]byte, tc.size)))
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, r)
			if rr.Code != tc.want {
				t.Errorf("%s (%d bytes): want %d, got %d", tc.path, tc.size, tc.want, rr.Code)
			}
		})
	}
}

// TestSecurityAuditLogs_LimitCapped pins security audit finding M-16: the
// caller-supplied ?limit was accepted unbounded, so one request could
// force serialization of the entire ring buffer. It is now capped at
// maxAuditLogLimit.
//
// RED against pre-fix code: limit=5000 returns all 1001 events.
func TestSecurityAuditLogs_LimitCapped(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	for i := 0; i < maxAuditLogLimit+1; i++ {
		_ = server.auditLogger.Log(&audit.Event{
			ID:           fmt.Sprintf("evt-%d", i),
			UserID:       "u",
			Action:       audit.ActionRead,
			ResourceType: audit.ResourceNode,
			Status:       audit.StatusSuccess,
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/logs?limit=5000", nil)
	rr := httptest.NewRecorder()
	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count > maxAuditLogLimit {
		t.Errorf("returned %d events, want <= %d (uncapped limit)", resp.Count, maxAuditLogLimit)
	}
}

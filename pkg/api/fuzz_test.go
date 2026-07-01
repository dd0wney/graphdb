package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/tenant"
)

// These fuzz targets assert a robustness invariant: HTTP handlers must NEVER
// panic on arbitrary input — malformed bodies, headers, and paths yield an
// error response, not a crash. Seed corpus runs on every `go test`;
// `go test -fuzz=FuzzXxx` drives coverage-guided search.
//
// They were disabled during the SRP refactor (the old NewServer(storage,
// &Config{}) + server.Handler() API no longer exist). Re-wired to the current
// API two ways:
//   - Body-JSON targets call the handler methods DIRECTLY with tenant context
//     wired the way withTenant does, so they exercise handler bodies past the
//     requireAuth wall (an unauthenticated mux request 401s before reaching
//     the handler — vacuous for body fuzzing).
//   - Path/header targets go through the RAW mux (registerRoutes, no panic-
//     recovery middleware) so a handler/middleware panic actually propagates
//     to the fuzzer instead of being masked as a 500.

const fuzzTenant = "fuzz-tenant"

// fuzzServer builds an isolated per-iteration test server (own temp dir, auto
// cleaned). Per-iteration (not shared) because Go runs fuzz iterations
// concurrently and a shared graph would race.
func fuzzServer(t *testing.T) *Server {
	t.Helper()
	s, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	return s
}

// rawReq carries a RAW (possibly malformed) body plus tenant context, for
// fuzzing handler bodies directly.
func rawReq(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(tenant.WithTenant(req.Context(), fuzzTenant))
}

// rawMux builds the route table without the middleware chain, so panics
// propagate to the fuzzer rather than being recovered.
func rawMux(s *Server) http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return mux
}

// FuzzNodeCreation fuzzes the node-creation handler body.
func FuzzNodeCreation(f *testing.F) {
	for _, s := range []string{
		`{"labels":["Person"],"properties":{"name":"Alice"}}`,
		`{"labels":[],"properties":{}}`,
		`{"labels":["A","B","C"],"properties":{"x":1}}`,
		`{}`, `{"labels":null}`, `{"properties":null}`,
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, jsonPayload string) {
		if len(jsonPayload) > 100000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleNodes panicked on %q: %v", jsonPayload, r)
			}
		}()
		s := fuzzServer(t)
		s.handleNodes(httptest.NewRecorder(), rawReq(http.MethodPost, "/nodes", jsonPayload))
	})
}

// FuzzEdgeCreation fuzzes the edge-creation handler body.
func FuzzEdgeCreation(f *testing.F) {
	for _, s := range []string{
		`{"from":1,"to":2,"label":"KNOWS","properties":{}}`,
		`{"from":0,"to":0,"label":"","properties":null}`,
		`{"from":-1,"to":-1,"label":"test"}`,
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, jsonPayload string) {
		if len(jsonPayload) > 100000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleEdges panicked on %q: %v", jsonPayload, r)
			}
		}()
		s := fuzzServer(t)
		s.handleEdges(httptest.NewRecorder(), rawReq(http.MethodPost, "/edges", jsonPayload))
	})
}

// FuzzQueryExecution fuzzes the query handler body.
func FuzzQueryExecution(f *testing.F) {
	for _, s := range []string{
		`{"query":"MATCH (n) RETURN n"}`, `{"query":""}`, `{"query":"MATCH"}`,
		`{}`, `{"query":null}`, `{"params":{"x":1}}`,
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, jsonPayload string) {
		if len(jsonPayload) > 100000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleQuery panicked on %q: %v", jsonPayload, r)
			}
		}()
		s := fuzzServer(t)
		s.handleQuery(httptest.NewRecorder(), rawReq(http.MethodPost, "/query", jsonPayload))
	})
}

// FuzzJSONPayloads throws the same (valid+malformed) JSON at the three main
// write handlers.
func FuzzJSONPayloads(f *testing.F) {
	for _, s := range []string{
		`{"valid": "json"}`, `{`, `}`, `{"unclosed": "string}`, `{"key": }`,
		`{"key": undefined}`, `[1, 2, 3]`, `null`, `true`, `123`, `"string"`,
		`{"nested": {"deep": {"object": {"value": 1}}}}`,
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, jsonData string) {
		if len(jsonData) > 100000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handler panicked on JSON %q: %v", jsonData, r)
			}
		}()
		s := fuzzServer(t)
		type route struct {
			path string
			h    http.HandlerFunc
		}
		for _, rt := range []route{
			{"/nodes", s.handleNodes},
			{"/edges", s.handleEdges},
			{"/query", s.handleQuery},
		} {
			rt.h(httptest.NewRecorder(), rawReq(http.MethodPost, rt.path, jsonData))
		}
	})
}

// FuzzPropertyInjection sends injection-style key/value pairs through the node
// handler; the property is embedded as a well-formed JSON value so the target
// exercises property handling rather than the JSON decoder.
func FuzzPropertyInjection(f *testing.F) {
	seeds := []struct{ k, v string }{
		{"name", "'; DROP TABLE nodes; --"},
		{"query", "MATCH (n) DELETE n"},
		{"script", "<script>alert('xss')</script>"},
		{"path", "../../etc/passwd"},
		{"command", "; rm -rf /"},
	}
	for _, s := range seeds {
		f.Add(s.k, s.v)
	}

	f.Fuzz(func(t *testing.T, key, maliciousValue string) {
		if key == "" || len(key) > 1000 || len(maliciousValue) > 10000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handleNodes panicked on injection %q=%q: %v", key, maliciousValue, r)
			}
		}()
		payload := map[string]any{
			"labels":     []string{"Test"},
			"properties": map[string]any{key: maliciousValue},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}
		s := fuzzServer(t)
		s.handleNodes(httptest.NewRecorder(), rawReq(http.MethodPost, "/nodes", string(body)))
	})
}

// FuzzHTTPHeaders fuzzes arbitrary request headers through the full route table.
func FuzzHTTPHeaders(f *testing.F) {
	seeds := []struct{ n, v string }{
		{"Content-Type", "application/json"}, {"Authorization", "Bearer token123"},
		{"X-Custom-Header", "value"}, {"User-Agent", "Mozilla/5.0"},
	}
	for _, s := range seeds {
		f.Add(s.n, s.v)
	}

	f.Fuzz(func(t *testing.T, headerName, headerValue string) {
		if len(headerName) > 1000 || len(headerValue) > 10000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("mux panicked on header %q:%q: %v", headerName, headerValue, r)
			}
		}()
		// http.NewRequest validates the target (returns error, never panics),
		// so a bad header name below is the only fuzzed variable.
		req, err := http.NewRequest(http.MethodGet, "/nodes", nil)
		if err != nil {
			return
		}
		if headerName != "" {
			// Header.Set canonicalises the name and never panics, so no guard is
			// needed here — and a recover() registered at this point would run
			// LIFO-before the outer one and silently swallow a real ServeHTTP
			// panic below, defeating the target.
			req.Header.Set(headerName, headerValue)
		}
		rawMux(fuzzServer(t)).ServeHTTP(httptest.NewRecorder(), req)
	})
}

// FuzzURLPaths fuzzes arbitrary request paths through the full route table.
func FuzzURLPaths(f *testing.F) {
	for _, s := range []string{
		"/nodes", "/nodes/123", "/edges", "/query", "/", "//",
		"/nodes/../../etc/passwd", "/nodes/%2e%2e",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, path string) {
		if len(path) > 10000 {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("mux panicked on path %q: %v", path, r)
			}
		}()
		// http.NewRequest returns an error (not a panic) on a malformed target,
		// so harness limitations aren't misreported as handler panics.
		req, err := http.NewRequest(http.MethodGet, path, nil)
		if err != nil {
			return
		}
		rawMux(fuzzServer(t)).ServeHTTP(httptest.NewRecorder(), req)
	})
}

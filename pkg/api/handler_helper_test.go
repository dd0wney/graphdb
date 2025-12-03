package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestRequestDecoder_DecodeJSON(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name      string
		body      string
		expectErr bool
	}{
		{
			name:      "valid JSON",
			body:      `{"labels": ["Test"], "properties": {"name": "test"}}`,
			expectErr: false,
		},
		{
			name:      "invalid JSON",
			body:      `{invalid json}`,
			expectErr: true,
		},
		{
			name:      "empty body",
			body:      ``,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(tt.body))
			rr := httptest.NewRecorder()

			var nodeReq NodeRequest
			decoder := server.NewRequestDecoder(rr, req)
			decoder.DecodeJSON(&nodeReq)

			if tt.expectErr && !decoder.HasError() {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && decoder.HasError() {
				t.Errorf("Expected no error but got: %v", decoder.Error())
			}
		})
	}
}

func TestRequestDecoder_ValidateNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name      string
		req       NodeRequest
		expectErr bool
	}{
		{
			name: "valid node request",
			req: NodeRequest{
				Labels:     []string{"Person"},
				Properties: map[string]any{"name": "Alice"},
			},
			expectErr: false,
		},
		{
			name: "empty labels",
			req: NodeRequest{
				Labels:     []string{},
				Properties: map[string]any{"name": "Alice"},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.req)
			req := httptest.NewRequest("POST", "/test", bytes.NewBuffer(body))
			rr := httptest.NewRecorder()

			var nodeReq NodeRequest
			decoder := server.NewRequestDecoder(rr, req)
			decoder.DecodeJSON(&nodeReq).ValidateNode(&nodeReq)

			if tt.expectErr && !decoder.HasError() {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && decoder.HasError() {
				t.Errorf("Expected no error but got: %v", decoder.Error())
			}
		})
	}
}

func TestRequestDecoder_RespondError(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(`{invalid}`))
	rr := httptest.NewRecorder()

	var nodeReq NodeRequest
	decoder := server.NewRequestDecoder(rr, req)
	decoder.DecodeJSON(&nodeReq)

	responded := decoder.RespondError()

	if !responded {
		t.Error("Expected RespondError to return true")
	}

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}

func TestPathIDExtractor_ExtractUint64(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name      string
		path      string
		prefix    string
		expectID  uint64
		expectOK  bool
	}{
		{
			name:     "valid ID",
			path:     "/nodes/123",
			prefix:   "/nodes/",
			expectID: 123,
			expectOK: true,
		},
		{
			name:     "valid ID with trailing slash",
			path:     "/nodes/456/",
			prefix:   "/nodes/",
			expectID: 456,
			expectOK: true,
		},
		{
			name:     "invalid ID",
			path:     "/nodes/abc",
			prefix:   "/nodes/",
			expectID: 0,
			expectOK: false,
		},
		{
			name:     "wrong prefix",
			path:     "/edges/123",
			prefix:   "/nodes/",
			expectID: 0,
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()

			extractor := server.NewPathExtractor(rr, req)
			id, ok := extractor.ExtractUint64(tt.prefix)

			if ok != tt.expectOK {
				t.Errorf("Expected ok=%v, got %v", tt.expectOK, ok)
			}
			if id != tt.expectID {
				t.Errorf("Expected id=%d, got %d", tt.expectID, id)
			}
		})
	}
}

func TestPropertyConverter_ConvertAndSanitize(t *testing.T) {
	converter := newPropertyConverter()

	props := map[string]any{
		"name":  "<script>alert('xss')</script>Test",
		"count": 42,
	}

	toValue := func(v any) storage.Value {
		switch val := v.(type) {
		case string:
			return storage.StringValue(val)
		default:
			return storage.StringValue(fmt.Sprintf("%v", val))
		}
	}

	result := converter.ConvertAndSanitize(props, toValue)

	if len(result) != 2 {
		t.Errorf("Expected 2 properties, got %d", len(result))
	}

	// Check that the script tag was sanitized
	nameVal, ok := result["name"]
	if !ok {
		t.Error("Expected 'name' property")
	}
	nameStr := string(nameVal.Data)
	if nameStr == "<script>alert('xss')</script>Test" {
		t.Error("XSS was not sanitized")
	}
}

func TestMethodRouter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		method         string
		expectHandled  string
	}{
		{"GET request", http.MethodGet, "get"},
		{"POST request", http.MethodPost, "post"},
		{"PUT request", http.MethodPut, "put"},
		{"DELETE request", http.MethodDelete, "delete"},
		{"PATCH request", http.MethodPatch, "notallowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			rr := httptest.NewRecorder()

			handled := ""

			router := server.NewMethodRouter(rr, req)
			router.
				Get(func() { handled = "get" }).
				Post(func() { handled = "post" }).
				Put(func() { handled = "put" }).
				Delete(func() { handled = "delete" }).
				NotAllowed()

			if tt.expectHandled == "notallowed" {
				if rr.Code != http.StatusMethodNotAllowed {
					t.Errorf("Expected 405, got %d", rr.Code)
				}
			} else if handled != tt.expectHandled {
				t.Errorf("Expected handled=%q, got %q", tt.expectHandled, handled)
			}
		})
	}
}

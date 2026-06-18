package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// vectorSearchPropertyFilter is a helper that POSTs req to /vector-search
// and returns the recorder. Used by the property_filter test family below.
func vectorSearchPropertyFilter(t *testing.T, server *Server, req VectorSearchRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, httpReq)
	return rr
}

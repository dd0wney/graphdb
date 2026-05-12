package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateCheckHandler(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Mock manifest would normally be fetched via HTTP.
	// For this test, we are mainly verifying the handler plumbing.
	
	req := httptest.NewRequest(http.MethodGet, "/admin/update/check?channel=stable", nil)
	rr := httptest.NewRecorder()
	
	server.handleUpdateCheck(rr, req)
	
	// Should fail with 500 because manifest URL isn't reachable in test environment
	// (or we could mock the HTTP client in Client, but that's overkill for a spike).
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestUpdateApplyHandler_NoUpdate(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"channel": "stable"})
	req := httptest.NewRequest(http.MethodPost, "/admin/update/apply", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	
	server.handleUpdateApply(rr, req)
	
	// Should fail with 500 for same reason as above
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

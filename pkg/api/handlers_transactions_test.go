package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
	"github.com/stretchr/testify/assert"
)

func TestTransactionAPI_E2E(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tenantID := "default"
	
	t.Run("Full Lifecycle", func(t *testing.T) {
		// 1. Begin
		req := httptest.NewRequest(http.MethodPost, "/v1/transactions", nil)
		req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
		rr := httptest.NewRecorder()
		server.handleTransactions(rr, req)
		
		assert.Equal(t, http.StatusCreated, rr.Code)
		var resp TransactionResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		txID := resp.ID
		assert.NotEmpty(t, txID)
		assert.Equal(t, "open", resp.Status)

		// 2. Query within Tx
		body, _ := json.Marshal(map[string]string{"query": "MATCH (n) RETURN n LIMIT 1"})
		qReq := httptest.NewRequest(http.MethodPost, "/v1/transactions/"+txID+"/query", bytes.NewReader(body))
		qReq = qReq.WithContext(tenant.WithTenant(qReq.Context(), tenantID))
		qRR := httptest.NewRecorder()
		server.handleTransactionEndpoint(qRR, qReq)
		
		assert.Equal(t, http.StatusOK, qRR.Code)

		// 3. Commit
		cReq := httptest.NewRequest(http.MethodPost, "/v1/transactions/"+txID+"/commit", nil)
		cReq = cReq.WithContext(tenant.WithTenant(cReq.Context(), tenantID))
		cRR := httptest.NewRecorder()
		server.handleTransactionEndpoint(cRR, cReq)
		
		assert.Equal(t, http.StatusOK, cRR.Code)
		var cResp TransactionResponse
		json.Unmarshal(cRR.Body.Bytes(), &cResp)
		assert.Equal(t, "committed", cResp.Status)
	})
}

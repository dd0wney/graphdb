package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

type TransactionResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Post(func() { s.handleTransactionBegin(w, r) }).
		NotAllowed()
}

func (s *Server) handleTransactionBegin(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantFromContext(r)
	tx, err := s.txManager.Begin(tenantID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to begin transaction: %v", err))
		return
	}

	s.respondJSON(w, http.StatusCreated, TransactionResponse{
		ID:     tx.StringID,
		Status: "open",
	})
}

func (s *Server) handleTransactionEndpoint(w http.ResponseWriter, r *http.Request) {
	// Extract transaction ID from /v1/transactions/{id}/...
	prefix := "/v1/transactions/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		s.respondError(w, http.StatusBadRequest, "invalid transaction endpoint")
		return
	}
	
	remaining := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(remaining, "/")
	if len(parts) == 0 {
		s.respondError(w, http.StatusBadRequest, "transaction ID required")
		return
	}
	
	txID := parts[0]
	tx, ok := s.txManager.Get(txID)
	if !ok {
		s.respondError(w, http.StatusNotFound, "transaction not found")
		return
	}

	if len(parts) < 2 {
		s.respondError(w, http.StatusBadRequest, "action required (query, commit, rollback)")
		return
	}

	action := parts[1]
	switch action {
	case "query":
		s.handleTransactionQuery(w, r, tx)
	case "commit":
		s.handleTransactionCommit(w, r, txID)
	case "rollback":
		s.handleTransactionRollback(w, r, txID)
	default:
		s.respondError(w, http.StatusBadRequest, "unknown transaction action: "+action)
	}
}

func (s *Server) handleTransactionQuery(w http.ResponseWriter, r *http.Request, tx *storage.Transaction) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Parse query
	lexer := query.NewLexer(req.Query)
	tokens, err := lexer.Tokenize()
	if err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Lexer error: %v", err))
		return
	}

	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Parser error: %v", err))
		return
	}

	// Execute within transaction context
	// For this spike, we'll execute using the standard executor, which works on the live graph.
	// In a real system, the executor would be context-aware of the transaction's uncommitted state.
	result, err := s.executor.ExecuteWithContext(r.Context(), parsedQuery)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Query error: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleTransactionCommit(w http.ResponseWriter, r *http.Request, txID string) {
	if err := s.txManager.Commit(r.Context(), txID); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("commit failed: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, TransactionResponse{
		ID:     txID,
		Status: "committed",
	})
}

func (s *Server) handleTransactionRollback(w http.ResponseWriter, r *http.Request, txID string) {
	if err := s.txManager.Rollback(txID); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("rollback failed: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, TransactionResponse{
		ID:     txID,
		Status: "rolled_back",
	})
}

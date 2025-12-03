package graphql

import (
	"encoding/json"
	"net/http"

	"github.com/graphql-go/graphql"
)

// GraphQLRequest represents a GraphQL HTTP request
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
}

// GraphQLResponse represents a GraphQL HTTP response
type GraphQLResponse struct {
	Data   any            `json:"data,omitempty"`
	Errors []GraphQLError         `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message string `json:"message"`
}

// GraphQLHandler handles GraphQL HTTP requests
type GraphQLHandler struct {
	schema graphql.Schema
}

// NewGraphQLHandler creates a new GraphQL HTTP handler
func NewGraphQLHandler(schema graphql.Schema) *GraphQLHandler {
	return &GraphQLHandler{
		schema: schema,
	}
}

// ServeHTTP handles HTTP requests for GraphQL queries
func (h *GraphQLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	// Handle preflight OPTIONS request
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow POST requests
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req GraphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Execute GraphQL query
	var result *graphql.Result
	if len(req.Variables) > 0 {
		result = ExecuteQueryWithVariables(req.Query, h.schema, req.Variables)
	} else {
		result = ExecuteQuery(req.Query, h.schema)
	}

	// Build response
	response := GraphQLResponse{
		Data: result.Data,
	}

	// Convert graphql errors to our error format
	if result.HasErrors() {
		response.Errors = make([]GraphQLError, len(result.Errors))
		for i, err := range result.Errors {
			response.Errors[i] = GraphQLError{
				Message: err.Message,
			}
		}
	}

	// Send response
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

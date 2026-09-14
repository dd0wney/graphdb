package graphql

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/graphql-go/graphql"
)

// GraphQLRequest represents a GraphQL HTTP request
type GraphQLRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

// GraphQLResponse represents a GraphQL HTTP response
type GraphQLResponse struct {
	Data   any            `json:"data,omitempty"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message string `json:"message"`
	// Extensions carries machine-readable error detail (for example
	// WAL_WRITE_FAILED's code/applied/durable/retry/id) when a resolver's
	// error implements gqlerrors.ExtendedError. Absent for a plain error.
	Extensions map[string]interface{} `json:"extensions,omitempty"`
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

	// Execute GraphQL query.
	//
	// Audit A6c-graphql-ctx (2026-05-08): pre-fix this dropped
	// r.Context() — resolvers ran with context.Background(), so
	// JWT-derived tenant scoping was invisible. Now r.Context() is
	// threaded into graphql.Params; the next PR migrates resolvers
	// to read tenantID via tenant.GetTenant(p.Context).
	var result *graphql.Result
	if len(req.Variables) > 0 {
		result = ExecuteQueryWithVariables(r.Context(), req.Query, h.schema, req.Variables)
	} else {
		result = ExecuteQuery(r.Context(), req.Query, h.schema)
	}

	// Build response
	response := GraphQLResponse{
		Data: result.Data,
	}

	// Convert graphql errors to our error format.
	//
	// Extensions is copied through from gqlerrors.FormattedError.Extensions.
	// graphql-go populates that field when a resolver's returned error
	// implements gqlerrors.ExtendedError (Extensions()
	// map[string]interface{}) — see gqlerrors/formatted.go's FormatError.
	// The chain that gets there: resolveField panics with the resolver's
	// raw returned error, handleFieldError wraps it via
	// NewLocatedErrorWithPath, and newLocatedError (located.go) sets
	// gqlerrors.Error.OriginalError to that same error value WITHOUT
	// changing its dynamic type — so FormatError's type-assertion
	// `origError.(ExtendedError)` still succeeds. This only works if the
	// resolver returns the extended error directly; fmt.Errorf's %w
	// produces a *fmt.wrapError with no Extensions() method, which would
	// lose it. See walWriteFailedError (wal_errors.go) for the type this
	// project defines, and TestQuery_OriginalErrorExtended in
	// github.com/graphql-go/graphql's executor_test.go for the same
	// mechanism exercised inside the library itself.
	if result.HasErrors() {
		response.Errors = make([]GraphQLError, len(result.Errors))
		for i, err := range result.Errors {
			response.Errors[i] = GraphQLError{
				Message:    err.Message,
				Extensions: err.Extensions,
			}
		}
	}

	// Send response. Headers are committed by WriteHeader; an encode
	// failure here cannot be recovered via respondError, so log.
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("graphql http: encode response failed: %v", err)
	}
}

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupTestServer creates a test server with sample data
func setupTestServer(t *testing.T) (*Server, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "api-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	server, err := NewServer(gs, 8080)
	if err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create server: %v", err)
	}

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tmpDir)
	}

	return server, cleanup
}

// setupTestServerWithData creates a test server with predefined test data
func setupTestServerWithData(t *testing.T) (*Server, func()) {
	t.Helper()

	server, cleanup := setupTestServer(t)
	gs := server.graph

	// Create test employees
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(80000),
		"age":        storage.IntValue(30),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(60000),
		"age":        storage.IntValue(25),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie"),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(70000),
		"age":        storage.IntValue(35),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Diana"),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(50000),
		"age":        storage.IntValue(28),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Eve"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(90000),
		"age":        storage.IntValue(40),
	})

	return server, cleanup
}

// makeQueryRequest makes a POST request to /query endpoint
func makeQueryRequest(t *testing.T, server *Server, queryStr string) *httptest.ResponseRecorder {
	t.Helper()

	reqBody := QueryRequest{
		Query: queryStr,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleQuery(rr, req)

	return rr
}

// TestAPI_Query_Aggregations tests aggregation functions via HTTP API
func TestAPI_Query_Aggregations(t *testing.T) {
	server, cleanup := setupTestServerWithData(t)
	defer cleanup()

	tests := []struct {
		name          string
		query         string
		expectedCount int
		verifyRow     func(t *testing.T, row map[string]any)
	}{
		{
			name:          "COUNT aggregation",
			query:         "MATCH (e:Employee) RETURN COUNT(e.name) AS total",
			expectedCount: 1,
			verifyRow: func(t *testing.T, row map[string]any) {
				if row["total"].(float64) != 5 {
					t.Errorf("Expected COUNT=5, got %v", row["total"])
				}
			},
		},
		{
			name:          "SUM aggregation",
			query:         "MATCH (e:Employee) RETURN SUM(e.salary) AS total_salary",
			expectedCount: 1,
			verifyRow: func(t *testing.T, row map[string]any) {
				expected := float64(80000 + 60000 + 70000 + 50000 + 90000)
				if row["total_salary"].(float64) != expected {
					t.Errorf("Expected SUM=%v, got %v", expected, row["total_salary"])
				}
			},
		},
		{
			name:          "AVG aggregation",
			query:         "MATCH (e:Employee) RETURN AVG(e.salary) AS avg_salary",
			expectedCount: 1,
			verifyRow: func(t *testing.T, row map[string]any) {
				expected := float64(350000) / 5.0
				if row["avg_salary"].(float64) != expected {
					t.Errorf("Expected AVG=%v, got %v", expected, row["avg_salary"])
				}
			},
		},
		{
			name:          "MIN aggregation",
			query:         "MATCH (e:Employee) RETURN MIN(e.salary) AS min_salary",
			expectedCount: 1,
			verifyRow: func(t *testing.T, row map[string]any) {
				if row["min_salary"].(float64) != 50000 {
					t.Errorf("Expected MIN=50000, got %v", row["min_salary"])
				}
			},
		},
		{
			name:          "MAX aggregation",
			query:         "MATCH (e:Employee) RETURN MAX(e.salary) AS max_salary",
			expectedCount: 1,
			verifyRow: func(t *testing.T, row map[string]any) {
				if row["max_salary"].(float64) != 90000 {
					t.Errorf("Expected MAX=90000, got %v", row["max_salary"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := makeQueryRequest(t, server, tt.query)

			if status := rr.Code; status != http.StatusOK {
				t.Fatalf("Handler returned wrong status code: got %v want %v, body: %s",
					status, http.StatusOK, rr.Body.String())
			}

			var resp QueryResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if resp.Count != tt.expectedCount {
				t.Errorf("Expected %d rows, got %d", tt.expectedCount, resp.Count)
			}

			if len(resp.Rows) > 0 {
				tt.verifyRow(t, resp.Rows[0])
			}
		})
	}
}

// TestAPI_Query_GroupBy tests GROUP BY functionality via HTTP API
func TestAPI_Query_GroupBy(t *testing.T) {
	server, cleanup := setupTestServerWithData(t)
	defer cleanup()

	// Query: Group by department and get average salary
	query := "MATCH (e:Employee) RETURN e.department, AVG(e.salary) AS avg_salary GROUP BY e.department"
	rr := makeQueryRequest(t, server, query)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code: got %v want %v, body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have 2 departments
	if resp.Count != 2 {
		t.Errorf("Expected 2 departments, got %d", resp.Count)
	}

	// Verify results contain both departments
	deptFound := make(map[string]bool)
	for _, row := range resp.Rows {
		dept := row["e.department"].(string)
		deptFound[dept] = true

		avgSalary := row["avg_salary"].(float64)

		if dept == "Engineering" {
			// Engineering: (80000 + 60000 + 90000) / 3 = 76666.67
			expected := float64(230000) / 3.0
			if avgSalary != expected {
				t.Errorf("Engineering avg salary: expected %v, got %v", expected, avgSalary)
			}
		} else if dept == "Sales" {
			// Sales: (70000 + 50000) / 2 = 60000
			expected := float64(60000)
			if avgSalary != expected {
				t.Errorf("Sales avg salary: expected %v, got %v", expected, avgSalary)
			}
		}
	}

	if !deptFound["Engineering"] || !deptFound["Sales"] {
		t.Error("Not all departments found in results")
	}
}

// TestAPI_Query_LIMIT_SKIP tests LIMIT and SKIP via HTTP API
func TestAPI_Query_LIMIT_SKIP(t *testing.T) {
	server, cleanup := setupTestServerWithData(t)
	defer cleanup()

	tests := []struct {
		name          string
		query         string
		expectedCount int
	}{
		{
			name:          "LIMIT only",
			query:         "MATCH (e:Employee) RETURN e.name LIMIT 3",
			expectedCount: 3,
		},
		{
			name:          "SKIP only",
			query:         "MATCH (e:Employee) RETURN e.name SKIP 2",
			expectedCount: 3, // 5 - 2 = 3
		},
		{
			name:          "LIMIT and SKIP",
			query:         "MATCH (e:Employee) RETURN e.name SKIP 1 LIMIT 2",
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := makeQueryRequest(t, server, tt.query)

			if status := rr.Code; status != http.StatusOK {
				t.Fatalf("Handler returned wrong status code: got %v want %v, body: %s",
					status, http.StatusOK, rr.Body.String())
			}

			var resp QueryResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if resp.Count != tt.expectedCount {
				t.Errorf("Expected %d rows, got %d", tt.expectedCount, resp.Count)
			}
		})
	}
}

// TestAPI_Query_WHERE_Filter tests WHERE clause filtering via HTTP API
func TestAPI_Query_WHERE_Filter(t *testing.T) {
	server, cleanup := setupTestServerWithData(t)
	defer cleanup()

	// Query: Find employees with salary > 60000
	query := "MATCH (e:Employee) WHERE e.salary > 60000 RETURN e.name, e.salary"
	rr := makeQueryRequest(t, server, query)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code: got %v want %v, body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should match: Alice (80k), Charlie (70k), Eve (90k) = 3 employees
	if resp.Count != 3 {
		t.Errorf("Expected 3 employees with salary > 60000, got %d", resp.Count)
	}

	// Verify all salaries are > 60000
	for _, row := range resp.Rows {
		salary := row["e.salary"].(float64)
		if salary <= 60000 {
			t.Errorf("Found employee with salary <= 60000: %v", salary)
		}
	}
}

// TestAPI_Query_Complex tests complex query with multiple features
func TestAPI_Query_Complex(t *testing.T) {
	server, cleanup := setupTestServerWithData(t)
	defer cleanup()

	// Complex query: WHERE + GROUP BY + Aggregations + LIMIT
	query := `MATCH (e:Employee)
	          WHERE e.age >= 30
	          RETURN e.department, COUNT(e.name) AS emp_count, AVG(e.salary) AS avg_salary
	          GROUP BY e.department
	          LIMIT 10`

	rr := makeQueryRequest(t, server, query)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("Handler returned wrong status code: got %v want %v, body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have 2 departments (both have employees age >= 30)
	if resp.Count != 2 {
		t.Errorf("Expected 2 departments, got %d", resp.Count)
	}

	// Verify the data structure
	for _, row := range resp.Rows {
		if _, ok := row["e.department"]; !ok {
			t.Error("Missing e.department column")
		}
		if _, ok := row["emp_count"]; !ok {
			t.Error("Missing emp_count column")
		}
		if _, ok := row["avg_salary"]; !ok {
			t.Error("Missing avg_salary column")
		}
	}
}

// TestAPI_Query_InvalidQuery tests error handling for invalid queries
func TestAPI_Query_InvalidQuery(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name        string
		query       string
		expectError bool
	}{
		{
			name:        "Invalid syntax",
			query:       "INVALID QUERY",
			expectError: true,
		},
		{
			name:        "Empty query",
			query:       "",
			expectError: false, // Empty query might just return empty results
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := makeQueryRequest(t, server, tt.query)

			if tt.expectError && rr.Code == http.StatusOK {
				t.Error("Expected error response, got 200 OK")
			}
		})
	}
}

// TestAPI_Query_MethodNotAllowed tests GET request is rejected
func TestAPI_Query_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/query", nil)
	rr := httptest.NewRecorder()

	server.handleQuery(rr, req)

	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %v, got %v", http.StatusMethodNotAllowed, status)
	}
}

// TestAPI_GraphQL_Integration tests the GraphQL endpoint integration
func TestAPI_GraphQL_Integration(t *testing.T) {
	// Create server with data first, then regenerate schema
	tmpDir, err := os.MkdirTemp("", "api-test-graphql-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}
	defer gs.Close()

	// Create test data BEFORE generating schema
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	// Now create server with schema that knows about Employee label
	server, err := NewServer(gs, 8080)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// GraphQL query request
	queryReq := map[string]any{
		"query": `{
			employees {
				id
				labels
			}
		}`,
	}

	body, err := json.Marshal(queryReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleGraphQL(rr, req)

	// Verify response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v, body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	// Parse GraphQL response
	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify data exists
	if response["data"] == nil {
		t.Error("Response missing 'data' field")
	}

	// Verify no errors
	if errors, ok := response["errors"]; ok && errors != nil {
		t.Errorf("Response has errors: %v", errors)
	}
}

// TestAPI_GraphQL_VariableSupport tests GraphQL queries with variables
func TestAPI_GraphQL_VariableSupport(t *testing.T) {
	// Create server with data first
	tmpDir, err := os.MkdirTemp("", "api-test-graphql-vars-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}
	defer gs.Close()

	// Create test data BEFORE generating schema
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	// Now create server
	server, err := NewServer(gs, 8080)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// GraphQL query with variables
	queryReq := map[string]any{
		"query": `query GetEmployee($id: ID!) {
			employee(id: $id) {
				id
				labels
			}
		}`,
		"variables": map[string]any{
			"id": "1",
		},
	}

	body, err := json.Marshal(queryReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleGraphQL(rr, req)

	// Verify response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify no errors
	if errors, ok := response["errors"]; ok && errors != nil {
		t.Errorf("Response has errors: %v", errors)
	}
}

// TestAPI_GraphQL_CORS tests CORS headers on GraphQL endpoint
func TestAPI_GraphQL_CORS(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	queryReq := map[string]any{
		"query": `{ health }`,
	}

	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleGraphQL(rr, req)

	// Verify CORS headers
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("CORS header 'Access-Control-Allow-Origin' not set")
	}
}

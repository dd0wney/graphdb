package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestConcurrent_NodeCreation tests concurrent node creation
func TestConcurrent_NodeCreation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	concurrency := 100
	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := NodeRequest{
				Labels: []string{"ConcurrentTest"},
				Properties: map[string]any{
					"id":   id,
					"name": fmt.Sprintf("Node%d", id),
				},
			}

			body, _ := json.Marshal(req)
			httpReq := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleNodes(rr, httpReq)

			if rr.Code == http.StatusCreated {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if successCount != int32(concurrency) {
		t.Errorf("Expected %d successful creations, got %d (errors: %d)",
			concurrency, successCount, errorCount)
	}

	t.Logf("✓ %d concurrent node creations succeeded", successCount)
}

// TestConcurrent_EdgeCreation tests concurrent edge creation
func TestConcurrent_EdgeCreation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create nodes first
	nodes := make([]uint64, 50)
	for i := 0; i < 50; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodes[i] = node.ID
	}

	concurrency := 100
	var wg sync.WaitGroup
	var successCount int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			fromIdx := idx % len(nodes)
			toIdx := (idx + 1) % len(nodes)

			req := EdgeRequest{
				FromNodeID: nodes[fromIdx],
				ToNodeID:   nodes[toIdx],
				Type:       "CONCURRENT_LINK",
				Weight:     float64(idx),
			}

			body, _ := json.Marshal(req)
			httpReq := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleEdges(rr, httpReq)

			if rr.Code == http.StatusCreated {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if successCount != int32(concurrency) {
		t.Errorf("Expected %d successful edge creations, got %d", concurrency, successCount)
	}

	t.Logf("✓ %d concurrent edge creations succeeded", successCount)
}

// TestConcurrent_ReadWrite tests concurrent reads and writes
func TestConcurrent_ReadWrite(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create initial nodes
	initialNodes := 10
	nodeIDs := make([]uint64, initialNodes)
	for i := 0; i < initialNodes; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	concurrency := 100
	var wg sync.WaitGroup
	var readCount int32
	var writeCount int32

	// Mix of reads and writes
	for i := 0; i < concurrency; i++ {
		wg.Add(1)

		if i%2 == 0 {
			// Read operation
			go func(idx int) {
				defer wg.Done()

				nodeID := nodeIDs[idx%len(nodeIDs)]
				req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", nodeID), nil)
				rr := httptest.NewRecorder()

				server.handleNode(rr, req)

				if rr.Code == http.StatusOK {
					atomic.AddInt32(&readCount, 1)
				}
			}(i)
		} else {
			// Write operation
			go func(idx int) {
				defer wg.Done()

				req := NodeRequest{
					Labels: []string{"ConcurrentWrite"},
					Properties: map[string]any{
						"id": idx,
					},
				}

				body, _ := json.Marshal(req)
				httpReq := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
				httpReq.Header.Set("Content-Type", "application/json")

				rr := httptest.NewRecorder()
				server.handleNodes(rr, httpReq)

				if rr.Code == http.StatusCreated {
					atomic.AddInt32(&writeCount, 1)
				}
			}(i)
		}
	}

	wg.Wait()

	expectedReads := int32(concurrency / 2)
	expectedWrites := int32(concurrency / 2)

	if readCount != expectedReads {
		t.Errorf("Expected %d reads, got %d", expectedReads, readCount)
	}

	if writeCount != expectedWrites {
		t.Errorf("Expected %d writes, got %d", expectedWrites, writeCount)
	}

	t.Logf("✓ Concurrent read/write: %d reads, %d writes", readCount, writeCount)
}

// TestConcurrent_BatchOperations tests concurrent batch operations
func TestConcurrent_BatchOperations(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	concurrency := 20
	batchSize := 10
	var wg sync.WaitGroup
	var totalCreated int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(batchID int) {
			defer wg.Done()

			nodes := make([]NodeRequest, batchSize)
			for j := 0; j < batchSize; j++ {
				nodes[j] = NodeRequest{
					Labels: []string{"BatchNode"},
					Properties: map[string]any{
						"batch_id": batchID,
						"node_id":  j,
					},
				}
			}

			req := BatchNodeRequest{Nodes: nodes}
			body, _ := json.Marshal(req)

			httpReq := httptest.NewRequest(http.MethodPost, "/batch/nodes", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleBatchNodes(rr, httpReq)

			if rr.Code == http.StatusCreated {
				var response BatchNodeResponse
				json.Unmarshal(rr.Body.Bytes(), &response)
				atomic.AddInt32(&totalCreated, int32(response.Created))
			}
		}(i)
	}

	wg.Wait()

	expected := int32(concurrency * batchSize)
	if totalCreated != expected {
		t.Errorf("Expected %d nodes created via batch, got %d", expected, totalCreated)
	}

	t.Logf("✓ %d concurrent batch operations created %d nodes", concurrency, totalCreated)
}

// TestConcurrent_AlgorithmExecution tests concurrent algorithm execution
func TestConcurrent_AlgorithmExecution(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create graph for algorithm tests
	nodeIDs := make([]uint64, 20)
	for i := 0; i < 20; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID

		if i > 0 {
			server.graph.CreateEdge(nodeIDs[i-1], nodeIDs[i], "LINK", map[string]storage.Value{}, 1.0)
		}
	}

	concurrency := 50
	var wg sync.WaitGroup
	var successCount int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := AlgorithmRequest{
				Algorithm: "pagerank",
				Parameters: map[string]any{
					"iterations": 10,
				},
			}

			body, _ := json.Marshal(req)
			httpReq := httptest.NewRequest(http.MethodPost, "/algorithms", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleAlgorithm(rr, httpReq)

			if rr.Code == http.StatusOK {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	if successCount != int32(concurrency) {
		t.Errorf("Expected %d successful algorithm executions, got %d", concurrency, successCount)
	}

	t.Logf("✓ %d concurrent PageRank executions succeeded", successCount)
}

// TestConcurrent_MixedOperations tests all operation types concurrently
func TestConcurrent_MixedOperations(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create initial data
	for i := 0; i < 10; i++ {
		server.graph.CreateNode([]string{"Initial"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
	}

	concurrency := 200
	var wg sync.WaitGroup
	var nodeCreates int32
	var edgeCreates int32
	var reads int32
	var health int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)

		switch i % 4 {
		case 0: // Create node
			go func(id int) {
				defer wg.Done()
				req := NodeRequest{
					Labels: []string{"Mixed"},
					Properties: map[string]any{"id": id},
				}
				body, _ := json.Marshal(req)
				httpReq := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
				httpReq.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleNodes(rr, httpReq)
				if rr.Code == http.StatusCreated {
					atomic.AddInt32(&nodeCreates, 1)
				}
			}(i)

		case 1: // Create edge
			go func() {
				defer wg.Done()
				req := EdgeRequest{
					FromNodeID: 1,
					ToNodeID:   2,
					Type:       "MIXED",
					Weight:     1.0,
				}
				body, _ := json.Marshal(req)
				httpReq := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
				httpReq.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleEdges(rr, httpReq)
				if rr.Code == http.StatusCreated {
					atomic.AddInt32(&edgeCreates, 1)
				}
			}()

		case 2: // Read node
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/nodes/1", nil)
				rr := httptest.NewRecorder()
				server.handleNode(rr, req)
				if rr.Code == http.StatusOK {
					atomic.AddInt32(&reads, 1)
				}
			}()

		case 3: // Health check
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/health", nil)
				rr := httptest.NewRecorder()
				server.handleHealth(rr, req)
				if rr.Code == http.StatusOK {
					atomic.AddInt32(&health, 1)
				}
			}()
		}
	}

	wg.Wait()

	total := nodeCreates + edgeCreates + reads + health

	if total != int32(concurrency) {
		t.Errorf("Expected %d total operations, got %d", concurrency, total)
	}

	t.Logf("✓ Mixed concurrent operations: nodes=%d, edges=%d, reads=%d, health=%d (total=%d)",
		nodeCreates, edgeCreates, reads, health, total)
}

// TestStress_RapidCreationDeletion tests rapid create/delete cycles
func TestStress_RapidCreationDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	server, cleanup := setupTestServer(t)
	defer cleanup()

	cycles := 10
	nodesPerCycle := 50
	var totalCreated int32
	var totalDeleted int32

	for cycle := 0; cycle < cycles; cycle++ {
		// Create nodes
		nodeIDs := make([]uint64, 0, nodesPerCycle)
		for i := 0; i < nodesPerCycle; i++ {
			req := NodeRequest{
				Labels: []string{"Stress"},
				Properties: map[string]any{
					"cycle": cycle,
					"id":    i,
				},
			}

			body, _ := json.Marshal(req)
			httpReq := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleNodes(rr, httpReq)

			if rr.Code == http.StatusCreated {
				var response NodeResponse
				json.Unmarshal(rr.Body.Bytes(), &response)
				nodeIDs = append(nodeIDs, response.ID)
				atomic.AddInt32(&totalCreated, 1)
			}
		}

		// Delete some nodes
		for i := 0; i < len(nodeIDs)/2; i++ {
			req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/nodes/%d", nodeIDs[i]), nil)
			rr := httptest.NewRecorder()
			server.handleNode(rr, req)

			if rr.Code == http.StatusOK {
				atomic.AddInt32(&totalDeleted, 1)
			}
		}
	}

	expectedCreated := int32(cycles * nodesPerCycle)
	expectedDeleted := int32(cycles * (nodesPerCycle / 2))

	if totalCreated != expectedCreated {
		t.Errorf("Expected %d nodes created, got %d", expectedCreated, totalCreated)
	}

	if totalDeleted != expectedDeleted {
		t.Errorf("Expected %d nodes deleted, got %d", expectedDeleted, totalDeleted)
	}

	t.Logf("✓ Stress test: %d cycles, %d created, %d deleted",
		cycles, totalCreated, totalDeleted)
}

// TestConcurrent_DataRace tests for data races under concurrent load
func TestConcurrent_DataRace(t *testing.T) {
	// This test is designed to catch data races when run with -race flag
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a node that will be read/updated concurrently
	node, _ := server.graph.CreateNode([]string{"RaceTest"}, map[string]storage.Value{
		"counter": storage.IntValue(0),
	})

	concurrency := 100
	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", node.ID), nil)
			rr := httptest.NewRecorder()
			server.handleNode(rr, req)
		}()
	}

	// Concurrent updates
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			req := NodeRequest{
				Properties: map[string]any{
					"counter": val,
				},
			}
			body, _ := json.Marshal(req)
			httpReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/nodes/%d", node.ID), bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			server.handleNode(rr, httpReq)
		}(i)
	}

	wg.Wait()

	t.Logf("✓ Data race test completed (%d concurrent operations)", concurrency*2)
}

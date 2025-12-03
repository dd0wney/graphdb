package query

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestNewResultStream tests result stream creation
func TestNewResultStream(t *testing.T) {
	stream := NewResultStream(10)

	if stream == nil {
		t.Fatal("NewResultStream returned nil")
	}

	if stream.ch == nil {
		t.Error("Expected channel to be initialized")
	}

	if stream.errCh == nil {
		t.Error("Expected error channel to be initialized")
	}

	if stream.ctx == nil {
		t.Error("Expected context to be initialized")
	}

	// Clean up
	stream.Close()
}

// TestResultStream_SendNext tests sending and receiving
func TestResultStream_SendNext(t *testing.T) {
	stream := NewResultStream(10)
	defer stream.Close()

	// Create test node
	node := &storage.Node{
		ID:     1,
		Labels: []string{"Person"},
		Properties: map[string]storage.Value{
			"name": storage.StringValue("Alice"),
		},
	}

	// Send node
	if !stream.Send(node) {
		t.Error("Send should succeed")
	}

	// Receive node
	received, err := stream.Next()
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}

	if received == nil {
		t.Fatal("Expected to receive node")
	}

	if received.ID != 1 {
		t.Errorf("Expected node ID 1, got %d", received.ID)
	}
}

// TestResultStream_Close tests stream closing
func TestResultStream_Close(t *testing.T) {
	stream := NewResultStream(10)

	// Close the stream
	stream.Close()

	// Try to receive - should get nil node (end of stream)
	// Error could be nil or context.Canceled due to select race
	node, _ := stream.Next()

	if node != nil {
		t.Error("Expected nil node after close")
	}

	// Double close should not panic
	stream.Close()
}

// TestResultStream_SendError tests error handling
func TestResultStream_SendError(t *testing.T) {
	stream := NewResultStream(10)

	testErr := errors.New("test error")
	stream.SendError(testErr)

	// Next should return an error (either the test error or context canceled)
	node, err := stream.Next()
	if err == nil {
		t.Error("Expected an error")
	}

	if node != nil {
		t.Error("Expected nil node when error occurs")
	}
}

// TestResultStream_EndOfStream tests end of stream behavior
func TestResultStream_EndOfStream(t *testing.T) {
	stream := NewResultStream(10)

	// Send one node
	node1 := &storage.Node{ID: 1}
	stream.Send(node1)

	// Close stream
	stream.Close()

	// First Next should return the node
	received, err := stream.Next()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if received == nil || received.ID != 1 {
		t.Error("Expected to receive node 1")
	}

	// Second Next should return nil node (end of stream)
	// Error could be nil or context.Canceled due to select race
	received, _ = stream.Next()
	if received != nil {
		t.Error("Expected nil node at end of stream")
	}
}

// TestNewStreamingQuery tests streaming query creation
func TestNewStreamingQuery(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	sq := NewStreamingQuery(graph)

	if sq == nil {
		t.Fatal("NewStreamingQuery returned nil")
	}

	if sq.graph != graph {
		t.Error("Expected graph to be set")
	}
}

// TestStreamingQuery_StreamNodes tests node streaming
func TestStreamingQuery_StreamNodes(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create test nodes
	_, err = graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	_, err = graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	sq := NewStreamingQuery(graph)

	// Stream all nodes
	stream := sq.StreamNodes(nil)
	defer stream.Close()

	count := 0
	for {
		node, err := stream.Next()
		// node == nil indicates end of stream (err might be nil or context.Canceled)
		if node == nil {
			break
		}
		// Only fail on error if we also got a non-nil node (shouldn't happen)
		if err != nil {
			t.Fatalf("Stream error with non-nil node: %v", err)
		}

		count++
	}

	if count != 2 {
		t.Errorf("Expected 2 nodes, got %d", count)
	}
}

// TestStreamingQuery_StreamNodesWithFilter tests filtered streaming
func TestStreamingQuery_StreamNodesWithFilter(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create test nodes
	_, err = graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	_, err = graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	sq := NewStreamingQuery(graph)

	// Stream nodes with filter - only nodes with age >= 30
	filter := func(n *storage.Node) bool {
		if val, ok := n.Properties["age"]; ok {
			age, err := val.AsInt()
			if err == nil && age >= 30 {
				return true
			}
		}
		return false
	}

	stream := sq.StreamNodes(filter)
	defer stream.Close()

	count := 0
	for {
		node, err := stream.Next()
		// node == nil indicates end of stream (err might be nil or context.Canceled)
		if node == nil {
			break
		}
		// Only fail on error if we also got a non-nil node (shouldn't happen)
		if err != nil {
			t.Fatalf("Stream error with non-nil node: %v", err)
		}

		count++
	}

	if count != 1 {
		t.Errorf("Expected 1 filtered node, got %d", count)
	}
}

// TestStreamingQuery_StreamTraversal tests traversal streaming
func TestStreamingQuery_StreamTraversal(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create a simple graph: 1 -> 2 -> 3
	node1, _ := graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"id": storage.IntValue(1),
	})

	node2, _ := graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"id": storage.IntValue(2),
	})

	node3, _ := graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"id": storage.IntValue(3),
	})

	// Create edges
	_, err = graph.CreateEdge(node1.ID, node2.ID, "CONNECTED", nil, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	_, err = graph.CreateEdge(node2.ID, node3.ID, "CONNECTED", nil, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	sq := NewStreamingQuery(graph)

	// Stream traversal from node 1 with max depth 2
	stream := sq.StreamTraversal(node1.ID, 2)
	defer stream.Close()

	nodeIDs := make(map[uint64]bool)
	for {
		node, err := stream.Next()
		// node == nil indicates end of stream (err might be nil or context.Canceled)
		if node == nil {
			break
		}
		// Only fail on error if we also got a non-nil node (shouldn't happen)
		if err != nil {
			t.Fatalf("Stream error with non-nil node: %v", err)
		}

		nodeIDs[node.ID] = true
	}

	// Should have visited nodes 1, 2, and 3
	if len(nodeIDs) != 3 {
		t.Errorf("Expected 3 nodes in traversal, got %d", len(nodeIDs))
	}
}

// TestNewBatchProcessor tests batch processor creation
func TestNewBatchProcessor(t *testing.T) {
	processor := func(batch []*storage.Node) error {
		return nil
	}

	bp := NewBatchProcessor(10, processor)

	if bp == nil {
		t.Fatal("NewBatchProcessor returned nil")
	}

	if bp.batchSize != 10 {
		t.Errorf("Expected batch size 10, got %d", bp.batchSize)
	}
}

// TestBatchProcessor_Process tests batch processing
func TestBatchProcessor_Process(t *testing.T) {
	stream := NewResultStream(10)

	// Send test nodes
	for i := 1; i <= 5; i++ {
		node := &storage.Node{
			ID:     uint64(i),
			Labels: []string{"Test"},
		}
		stream.Send(node)
	}
	stream.Close()

	// Process in batches of 2
	batches := make([][]*storage.Node, 0)
	processor := func(batch []*storage.Node) error {
		// Make a copy of the batch
		batchCopy := make([]*storage.Node, len(batch))
		copy(batchCopy, batch)
		batches = append(batches, batchCopy)
		return nil
	}

	bp := NewBatchProcessor(2, processor)
	err := bp.Process(stream)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should have 3 batches: [1,2], [3,4], [5]
	if len(batches) != 3 {
		t.Errorf("Expected 3 batches, got %d", len(batches))
	}

	// Verify batch sizes
	if len(batches[0]) != 2 {
		t.Errorf("Expected first batch size 2, got %d", len(batches[0]))
	}
	if len(batches[1]) != 2 {
		t.Errorf("Expected second batch size 2, got %d", len(batches[1]))
	}
	if len(batches[2]) != 1 {
		t.Errorf("Expected third batch size 1, got %d", len(batches[2]))
	}
}

// TestBatchProcessor_ProcessError tests error handling in batch processing
func TestBatchProcessor_ProcessError(t *testing.T) {
	stream := NewResultStream(10)

	// Send an error
	testErr := errors.New("stream error")
	stream.SendError(testErr)

	processor := func(batch []*storage.Node) error {
		t.Error("Processor should not be called when stream has error")
		return nil
	}

	bp := NewBatchProcessor(10, processor)
	err := bp.Process(stream)

	// Should get an error (either testErr or context.Canceled)
	if err == nil {
		t.Error("Expected an error from Process")
	}
}

// TestBatchProcessor_ProcessorError tests error from processor
func TestBatchProcessor_ProcessorError(t *testing.T) {
	stream := NewResultStream(10)

	// Send nodes
	for i := 1; i <= 3; i++ {
		stream.Send(&storage.Node{ID: uint64(i)})
	}
	stream.Close()

	procErr := errors.New("processor error")
	processor := func(batch []*storage.Node) error {
		return procErr
	}

	bp := NewBatchProcessor(2, processor)
	err := bp.Process(stream)

	if err != procErr {
		t.Errorf("Expected processor error %v, got %v", procErr, err)
	}
}

// TestResultStream_ConcurrentSend tests concurrent sends
func TestResultStream_ConcurrentSend(t *testing.T) {
	stream := NewResultStream(100)
	defer stream.Close()

	numSenders := 5
	nodesPerSender := 10

	var wg sync.WaitGroup

	// Start multiple senders
	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(senderID int) {
			defer wg.Done()
			for j := 0; j < nodesPerSender; j++ {
				node := &storage.Node{
					ID: uint64(senderID*100 + j),
				}
				stream.Send(node)
			}
		}(i)
	}

	// Wait for all senders to complete
	wg.Wait()

	// Close stream
	stream.Close()

	// Count received nodes
	count := 0
	for {
		node, err := stream.Next()
		// node == nil indicates end of stream (err might be nil or context.Canceled)
		if node == nil {
			break
		}
		// Only fail on error if we also got a non-nil node (shouldn't happen)
		if err != nil {
			t.Fatalf("Unexpected error with non-nil node: %v", err)
		}
		count++
	}

	expected := numSenders * nodesPerSender
	if count != expected {
		t.Errorf("Expected %d nodes, got %d", expected, count)
	}
}

// TestResultStream_ContextCancellation tests context cancellation
func TestResultStream_ContextCancellation(t *testing.T) {
	stream := NewResultStream(10)

	// Cancel the context
	stream.cancel()

	// Next should return context error (eventually, after any buffered items)
	_, err := stream.Next()
	if err == nil {
		t.Error("Expected context error")
	}

	// After context is cancelled, send will eventually fail
	// (may succeed a few times due to buffering and select race)
	node := &storage.Node{ID: 1}
	failedCount := 0
	for i := 0; i < 100; i++ {
		if !stream.Send(node) {
			failedCount++
		}
	}
	// Should have at least some failures since context is cancelled
	if failedCount == 0 {
		t.Error("Expected Send to fail after context cancellation")
	}
}

// TestResultStream_BufferFull tests buffer overflow handling
func TestResultStream_BufferFull(t *testing.T) {
	// Create stream with small buffer
	stream := NewResultStream(2)

	// Fill the buffer
	stream.Send(&storage.Node{ID: 1})
	stream.Send(&storage.Node{ID: 2})

	// Try to send one more (this will block unless buffer has space or stream is closed)
	// Start a goroutine to send
	sendResult := make(chan bool)
	go func() {
		result := stream.Send(&storage.Node{ID: 3})
		sendResult <- result
	}()

	// Give it a moment to block on send
	time.Sleep(50 * time.Millisecond)

	// Consume one item from buffer - this should unblock the sender
	node, _ := stream.Next()
	if node == nil || node.ID != 1 {
		t.Error("Expected to receive first node")
	}

	// Wait for send to complete
	select {
	case result := <-sendResult:
		if !result {
			t.Error("Send should succeed after buffer space available")
		}
	case <-time.After(1 * time.Second):
		t.Error("Send did not complete after buffer space available")
	}

	// Clean up
	stream.Close()
}

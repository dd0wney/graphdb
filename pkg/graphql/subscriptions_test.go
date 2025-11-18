package graphql

import (
	"context"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/pubsub"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestNodeCreatedSubscription tests subscribing to node creation events
func TestNodeCreatedSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: false,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	ps := pubsub.NewPubSub()
	defer ps.Shutdown()

	schema, err := GenerateSchemaWithSubscriptions(gs, ps)
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Subscribe to node created events
	ctx := context.Background()
	sub, err := SubscribeToNodeCreated(ps, ctx, "Person")
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	received := make(chan *storage.Node, 1)
	go func() {
		for msg := range sub.Channel() {
			if event, ok := msg.(*NodeEvent); ok {
				received <- event.Node
			}
		}
	}()

	// Create a node
	node, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	// Trigger the event
	PublishNodeCreated(ps, node)

	// Verify we received the event
	select {
	case receivedNode := <-received:
		if receivedNode.ID != node.ID {
			t.Errorf("Expected node ID %d, got %d", node.ID, receivedNode.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for node created event")
	}

	_ = schema // Use schema to avoid unused variable error
}

// TestNodeUpdatedSubscription tests subscribing to node update events
func TestNodeUpdatedSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: false,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	ps := pubsub.NewPubSub()
	defer ps.Shutdown()

	// Create initial node
	node, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})

	// Subscribe to updates
	ctx := context.Background()
	sub, err := SubscribeToNodeUpdated(ps, ctx, node.ID)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	received := make(chan *storage.Node, 1)
	go func() {
		for msg := range sub.Channel() {
			if event, ok := msg.(*NodeEvent); ok {
				received <- event.Node
			}
		}
	}()

	// Update the node
	_ = gs.UpdateNode(node.ID, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(26),
	})

	// Get updated node and trigger the event
	updatedNode, _ := gs.GetNode(node.ID)
	PublishNodeUpdated(ps, updatedNode)

	// Verify we received the event
	select {
	case receivedNode := <-received:
		if age, err := receivedNode.Properties["age"].AsInt(); err == nil {
			if age != 26 {
				t.Errorf("Expected age 26, got %d", age)
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for node updated event")
	}
}

// TestNodeDeletedSubscription tests subscribing to node deletion events
func TestNodeDeletedSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: false,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	ps := pubsub.NewPubSub()
	defer ps.Shutdown()

	// Create a node
	node, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
	})

	// Subscribe to deletion events
	ctx := context.Background()
	sub, err := SubscribeToNodeDeleted(ps, ctx, node.ID)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	received := make(chan uint64, 1)
	go func() {
		for msg := range sub.Channel() {
			if event, ok := msg.(*NodeEvent); ok {
				received <- event.Node.ID
			}
		}
	}()

	// Delete the node
	gs.DeleteNode(node.ID)

	// Trigger the event
	PublishNodeDeleted(ps, node)

	// Verify we received the event
	select {
	case receivedID := <-received:
		if receivedID != node.ID {
			t.Errorf("Expected node ID %d, got %d", node.ID, receivedID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for node deleted event")
	}
}

// TestEdgeCreatedSubscription tests subscribing to edge creation events
func TestEdgeCreatedSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: false,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	ps := pubsub.NewPubSub()
	defer ps.Shutdown()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	// Subscribe to edge created events
	ctx := context.Background()
	sub, err := SubscribeToEdgeCreated(ps, ctx, "KNOWS")
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	received := make(chan *storage.Edge, 1)
	go func() {
		for msg := range sub.Channel() {
			if event, ok := msg.(*EdgeEvent); ok {
				received <- event.Edge
			}
		}
	}()

	// Create an edge
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{}, 1.0)

	// Trigger the event
	PublishEdgeCreated(ps, edge)

	// Verify we received the event
	select {
	case receivedEdge := <-received:
		if receivedEdge.ID != edge.ID {
			t.Errorf("Expected edge ID %d, got %d", edge.ID, receivedEdge.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for edge created event")
	}
}

// TestMultipleSubscribers tests multiple clients subscribing to the same events
func TestMultipleSubscribers(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: false,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	ps := pubsub.NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()
	numSubscribers := 3
	subs := make([]*pubsub.Subscription, numSubscribers)
	received := make([]chan *storage.Node, numSubscribers)

	// Create multiple subscribers
	for i := 0; i < numSubscribers; i++ {
		sub, err := SubscribeToNodeCreated(ps, ctx, "Person")
		if err != nil {
			t.Fatalf("Failed to subscribe %d: %v", i, err)
		}
		subs[i] = sub
		defer sub.Unsubscribe()

		received[i] = make(chan *storage.Node, 1)
		go func(ch chan *storage.Node, subscription *pubsub.Subscription) {
			for msg := range subscription.Channel() {
				if event, ok := msg.(*NodeEvent); ok {
					ch <- event.Node
				}
			}
		}(received[i], sub)
	}

	// Create a node and trigger event
	node, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Diana"),
	})
	PublishNodeCreated(ps, node)

	// All subscribers should receive the event
	for i := 0; i < numSubscribers; i++ {
		select {
		case receivedNode := <-received[i]:
			if receivedNode.ID != node.ID {
				t.Errorf("Subscriber %d: expected node ID %d, got %d", i, node.ID, receivedNode.ID)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("Subscriber %d: timeout waiting for event", i)
		}
	}
}

// TestLabelSpecificSubscription tests subscribing to events for specific labels
func TestLabelSpecificSubscription(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: false,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	ps := pubsub.NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()

	// Subscribe to Person nodes only
	sub, _ := SubscribeToNodeCreated(ps, ctx, "Person")
	defer sub.Unsubscribe()

	received := make(chan string, 2)
	go func() {
		for msg := range sub.Channel() {
			if event, ok := msg.(*NodeEvent); ok {
				if len(event.Node.Labels) > 0 {
					received <- event.Node.Labels[0]
				}
			}
		}
	}()

	// Create a Person node
	personNode, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	PublishNodeCreated(ps, personNode)

	// Create a Company node (should not be received)
	companyNode, _ := gs.CreateNode([]string{"Company"}, map[string]storage.Value{
		"name": storage.StringValue("TechCorp"),
	})
	PublishNodeCreated(ps, companyNode)

	// Should only receive Person event
	select {
	case label := <-received:
		if label != "Person" {
			t.Errorf("Expected Person label, got %s", label)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for Person event")
	}

	// Should not receive Company event
	select {
	case label := <-received:
		t.Errorf("Unexpected event for label: %s", label)
	case <-time.After(200 * time.Millisecond):
		// Expected: no event
	}
}

package pubsub

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestBasicPubSub tests basic publish/subscribe functionality
func TestBasicPubSub(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	received := make(chan interface{}, 1)
	ctx := context.Background()

	// Subscribe to a topic
	sub, err := ps.Subscribe(ctx, "test-topic")
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Start listening
	go func() {
		msg := <-sub.Channel()
		received <- msg
	}()

	// Publish a message
	ps.Publish("test-topic", "Hello, World!")

	// Wait for message
	select {
	case msg := <-received:
		if msg != "Hello, World!" {
			t.Errorf("Expected 'Hello, World!', got %v", msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	sub.Unsubscribe()
}

// TestMultipleSubscribers tests multiple subscribers to the same topic
func TestMultipleSubscribers(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()
	numSubscribers := 5
	received := make([]chan interface{}, numSubscribers)

	// Create multiple subscribers
	for i := 0; i < numSubscribers; i++ {
		received[i] = make(chan interface{}, 1)
		sub, err := ps.Subscribe(ctx, "broadcast-topic")
		if err != nil {
			t.Fatalf("Failed to subscribe %d: %v", i, err)
		}
		defer sub.Unsubscribe()

		// Listen for messages
		go func(ch chan interface{}, subscription *Subscription) {
			msg := <-subscription.Channel()
			ch <- msg
		}(received[i], sub)
	}

	// Publish one message
	testMsg := "Broadcast message"
	ps.Publish("broadcast-topic", testMsg)

	// All subscribers should receive the message
	for i := 0; i < numSubscribers; i++ {
		select {
		case msg := <-received[i]:
			if msg != testMsg {
				t.Errorf("Subscriber %d: expected '%s', got %v", i, testMsg, msg)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("Subscriber %d: timeout waiting for message", i)
		}
	}
}

// TestTopicIsolation tests that messages are isolated by topic
func TestTopicIsolation(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()

	sub1, _ := ps.Subscribe(ctx, "topic-1")
	sub2, _ := ps.Subscribe(ctx, "topic-2")
	defer sub1.Unsubscribe()
	defer sub2.Unsubscribe()

	received1 := make(chan interface{}, 1)
	received2 := make(chan interface{}, 1)

	go func() {
		select {
		case msg := <-sub1.Channel():
			received1 <- msg
		case <-time.After(500 * time.Millisecond):
			received1 <- nil
		}
	}()

	go func() {
		select {
		case msg := <-sub2.Channel():
			received2 <- msg
		case <-time.After(500 * time.Millisecond):
			received2 <- nil
		}
	}()

	// Publish to topic-1 only
	ps.Publish("topic-1", "Message for topic 1")

	// topic-1 should receive, topic-2 should not
	msg1 := <-received1
	if msg1 != "Message for topic 1" {
		t.Errorf("Topic 1: expected message, got %v", msg1)
	}

	msg2 := <-received2
	if msg2 != nil {
		t.Errorf("Topic 2: expected no message, got %v", msg2)
	}
}

// TestUnsubscribe tests that unsubscribed clients don't receive messages
func TestUnsubscribe(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()
	sub, _ := ps.Subscribe(ctx, "test-topic")

	received := make(chan interface{}, 2)
	go func() {
		for msg := range sub.Channel() {
			received <- msg
		}
	}()

	// First message
	ps.Publish("test-topic", "Message 1")
	msg1 := <-received
	if msg1 != "Message 1" {
		t.Errorf("Expected 'Message 1', got %v", msg1)
	}

	// Unsubscribe
	sub.Unsubscribe()

	// Second message (should not be received)
	ps.Publish("test-topic", "Message 2")

	select {
	case msg := <-received:
		t.Errorf("Received message after unsubscribe: %v", msg)
	case <-time.After(200 * time.Millisecond):
		// Expected: no message received
	}
}

// TestContextCancellation tests that subscriptions respect context cancellation
func TestContextCancellation(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	sub, _ := ps.Subscribe(ctx, "test-topic")

	received := make(chan interface{}, 1)
	done := make(chan bool, 1)

	go func() {
		for msg := range sub.Channel() {
			received <- msg
		}
		done <- true
	}()

	// Cancel context
	cancel()

	// Wait for channel to close
	select {
	case <-done:
		// Expected: channel closed
	case <-time.After(1 * time.Second):
		t.Fatal("Subscription channel did not close on context cancellation")
	}
}

// TestConcurrentPublish tests concurrent publishing from multiple goroutines
func TestConcurrentPublish(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()
	sub, _ := ps.Subscribe(ctx, "concurrent-topic")
	defer sub.Unsubscribe()

	numMessages := 100
	received := make(map[int]bool)
	var mu sync.Mutex

	go func() {
		for msg := range sub.Channel() {
			if num, ok := msg.(int); ok {
				mu.Lock()
				received[num] = true
				mu.Unlock()
			}
		}
	}()

	// Publish concurrently
	var wg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ps.Publish("concurrent-topic", n)
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond) // Allow time for messages to be processed

	// Verify all messages received
	mu.Lock()
	defer mu.Unlock()
	if len(received) != numMessages {
		t.Errorf("Expected %d messages, received %d", numMessages, len(received))
	}
}

// TestBufferedSubscription tests that subscriptions can handle buffering
func TestBufferedSubscription(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()
	sub, _ := ps.Subscribe(ctx, "buffered-topic")
	defer sub.Unsubscribe()

	// Publish multiple messages before consuming
	for i := 0; i < 5; i++ {
		ps.Publish("buffered-topic", i)
	}

	// Consume messages
	for i := 0; i < 5; i++ {
		select {
		case msg := <-sub.Channel():
			if msg != i {
				t.Errorf("Expected %d, got %v", i, msg)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("Timeout waiting for message %d", i)
		}
	}
}

// TestGetSubscriberCount tests getting the number of subscribers for a topic
func TestGetSubscriberCount(t *testing.T) {
	ps := NewPubSub()
	defer ps.Shutdown()

	ctx := context.Background()

	// Initially no subscribers
	count := ps.GetSubscriberCount("test-topic")
	if count != 0 {
		t.Errorf("Expected 0 subscribers, got %d", count)
	}

	// Add subscribers
	sub1, _ := ps.Subscribe(ctx, "test-topic")
	sub2, _ := ps.Subscribe(ctx, "test-topic")
	sub3, _ := ps.Subscribe(ctx, "test-topic")

	count = ps.GetSubscriberCount("test-topic")
	if count != 3 {
		t.Errorf("Expected 3 subscribers, got %d", count)
	}

	// Remove one
	sub1.Unsubscribe()
	count = ps.GetSubscriberCount("test-topic")
	if count != 2 {
		t.Errorf("Expected 2 subscribers after unsubscribe, got %d", count)
	}

	sub2.Unsubscribe()
	sub3.Unsubscribe()
}

// TestShutdown tests graceful shutdown
func TestShutdown(t *testing.T) {
	ps := NewPubSub()

	ctx := context.Background()
	sub, _ := ps.Subscribe(ctx, "test-topic")

	done := make(chan bool, 1)
	go func() {
		for range sub.Channel() {
			// Consume messages
		}
		done <- true
	}()

	// Shutdown
	ps.Shutdown()

	// Verify channel closed
	select {
	case <-done:
		// Expected
	case <-time.After(1 * time.Second):
		t.Fatal("Subscription channel did not close on shutdown")
	}
}

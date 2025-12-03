package pubsub

import (
	"context"
	"sync"
)

// PubSub provides publish/subscribe functionality for real-time updates
type PubSub struct {
	subscribers map[string]map[*Subscription]bool
	mu          sync.RWMutex
	shutdown    chan struct{}
	shutdownMu  sync.Mutex
	isShutdown  bool
}

// Subscription represents a subscription to a topic
type Subscription struct {
	topic     string
	channel   chan any
	ps        *PubSub
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once // Ensures channel is only closed once
}

// NewPubSub creates a new PubSub instance
func NewPubSub() *PubSub {
	return &PubSub{
		subscribers: make(map[string]map[*Subscription]bool),
		shutdown:    make(chan struct{}),
	}
}

// Subscribe creates a new subscription to a topic
func (ps *PubSub) Subscribe(ctx context.Context, topic string) (*Subscription, error) {
	ps.shutdownMu.Lock()
	if ps.isShutdown {
		ps.shutdownMu.Unlock()
		return nil, nil
	}
	ps.shutdownMu.Unlock()

	// Create subscription with buffered channel
	subCtx, cancel := context.WithCancel(ctx)
	sub := &Subscription{
		topic:   topic,
		channel: make(chan any, 100), // Buffer for messages
		ps:      ps,
		ctx:     subCtx,
		cancel:  cancel,
	}

	// Add to subscribers
	ps.mu.Lock()
	if ps.subscribers[topic] == nil {
		ps.subscribers[topic] = make(map[*Subscription]bool)
	}
	ps.subscribers[topic][sub] = true
	ps.mu.Unlock()

	// Monitor context cancellation
	go func() {
		select {
		case <-subCtx.Done():
			sub.Unsubscribe()
		case <-ps.shutdown:
			sub.close()
		}
	}()

	return sub, nil
}

// Publish sends a message to all subscribers of a topic.
// Uses a snapshot copy to avoid holding lock during potentially slow channel sends.
func (ps *PubSub) Publish(topic string, message any) {
	ps.shutdownMu.Lock()
	if ps.isShutdown {
		ps.shutdownMu.Unlock()
		return
	}
	ps.shutdownMu.Unlock()

	// Take a snapshot of subscribers under lock to avoid race condition
	// during iteration (concurrent Unsubscribe could modify the map)
	ps.mu.RLock()
	topicSubs := ps.subscribers[topic]
	if len(topicSubs) == 0 {
		ps.mu.RUnlock()
		return
	}
	// Copy subscriber pointers to slice for safe iteration
	subs := make([]*Subscription, 0, len(topicSubs))
	for sub := range topicSubs {
		subs = append(subs, sub)
	}
	ps.mu.RUnlock()

	// Send message to all subscribers (outside lock to avoid blocking)
	for _, sub := range subs {
		select {
		case sub.channel <- message:
			// Message sent
		default:
			// Channel full, skip (non-blocking)
		}
	}
}

// GetSubscriberCount returns the number of subscribers for a topic
func (ps *PubSub) GetSubscriberCount(topic string) int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if ps.subscribers[topic] == nil {
		return 0
	}

	return len(ps.subscribers[topic])
}

// Shutdown closes all subscriptions and shuts down the PubSub
func (ps *PubSub) Shutdown() {
	ps.shutdownMu.Lock()
	if ps.isShutdown {
		ps.shutdownMu.Unlock()
		return
	}
	ps.isShutdown = true
	ps.shutdownMu.Unlock()

	close(ps.shutdown)

	// Close all subscription channels
	ps.mu.Lock()
	for topic := range ps.subscribers {
		for sub := range ps.subscribers[topic] {
			sub.close()
		}
		delete(ps.subscribers, topic)
	}
	ps.mu.Unlock()
}

// Channel returns the subscription's message channel
func (s *Subscription) Channel() <-chan any {
	return s.channel
}

// Unsubscribe removes the subscription
func (s *Subscription) Unsubscribe() {
	s.cancel()

	s.ps.mu.Lock()
	defer s.ps.mu.Unlock()

	if s.ps.subscribers[s.topic] != nil {
		delete(s.ps.subscribers[s.topic], s)
		if len(s.ps.subscribers[s.topic]) == 0 {
			delete(s.ps.subscribers, s.topic)
		}
	}

	s.close()
}

// close closes the subscription channel safely (idempotent)
func (s *Subscription) close() {
	s.closeOnce.Do(func() {
		close(s.channel)
	})
}

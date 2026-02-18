package oidc

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const (
	// StateLength is the byte length of CSRF state tokens
	StateLength = 32
	// DefaultStateTTL is how long state tokens are valid
	DefaultStateTTL = 10 * time.Minute
	// MaxStateEntries is the maximum number of pending state entries (DoS protection)
	MaxStateEntries = 10000
)

// StateStore manages CSRF state for OAuth2 flows
type StateStore struct {
	mu      sync.RWMutex
	states  map[string]*StateEntry
	ttl     time.Duration
	maxSize int
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewStateStore creates a new state store with default settings
func NewStateStore() *StateStore {
	store := &StateStore{
		states:  make(map[string]*StateEntry),
		ttl:     DefaultStateTTL,
		maxSize: MaxStateEntries,
		stopCh:  make(chan struct{}),
	}
	store.wg.Add(1)
	go store.cleanupLoop()
	return store
}

// NewStateStoreWithTTL creates a state store with custom TTL
func NewStateStoreWithTTL(ttl time.Duration) *StateStore {
	if ttl <= 0 {
		ttl = DefaultStateTTL
	}
	store := &StateStore{
		states:  make(map[string]*StateEntry),
		ttl:     ttl,
		maxSize: MaxStateEntries,
		stopCh:  make(chan struct{}),
	}
	store.wg.Add(1)
	go store.cleanupLoop()
	return store
}

// GenerateState creates a new CSRF state token
func (s *StateStore) GenerateState() (string, error) {
	// Generate random bytes
	bytes := make([]byte, StateLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	state := base64.RawURLEncoding.EncodeToString(bytes)

	// Generate nonce for additional security
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if we're at capacity (DoS protection)
	if len(s.states) >= s.maxSize {
		// Remove oldest entries
		s.evictOldest(s.maxSize / 10) // Remove 10% of entries
	}

	s.states[state] = &StateEntry{
		CreatedAt: time.Now(),
		Nonce:     nonce,
	}

	return state, nil
}

// ValidateAndConsume validates a state token and removes it (one-time use)
func (s *StateStore) ValidateAndConsume(state string) (*StateEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.states[state]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Since(entry.CreatedAt) > s.ttl {
		delete(s.states, state)
		return nil, false
	}

	// Consume (one-time use)
	delete(s.states, state)
	return entry, true
}

// evictOldest removes the oldest entries (must be called with lock held)
func (s *StateStore) evictOldest(count int) {
	if count <= 0 {
		return
	}

	// Find oldest entries
	type stateTime struct {
		state     string
		createdAt time.Time
	}
	entries := make([]stateTime, 0, len(s.states))
	for state, entry := range s.states {
		entries = append(entries, stateTime{state, entry.CreatedAt})
	}

	// Sort by creation time (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].createdAt.Before(entries[i].createdAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove oldest entries
	for i := 0; i < count && i < len(entries); i++ {
		delete(s.states, entries[i].state)
	}
}

// cleanupLoop periodically removes expired entries
func (s *StateStore) cleanupLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// cleanup removes expired entries
func (s *StateStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for state, entry := range s.states {
		if now.Sub(entry.CreatedAt) > s.ttl {
			delete(s.states, state)
		}
	}
}

// Len returns the number of pending state entries
func (s *StateStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.states)
}

// Close stops the cleanup goroutine and releases resources
func (s *StateStore) Close() {
	close(s.stopCh)
	s.wg.Wait()
}

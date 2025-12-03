package middleware

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitConfig configures rate limiting
type RateLimitConfig struct {
	RequestsPerSecond float64       // Rate of token replenishment
	BurstSize         int           // Maximum burst size (bucket capacity)
	CleanupInterval   time.Duration // How often to clean up expired buckets
	ClientExpiration  time.Duration // How long to keep inactive client buckets
	MaxClients        int           // Maximum number of tracked clients (prevents DoS via memory exhaustion)
}

// DefaultRateLimitConfig returns sensible defaults for rate limiting
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond: 100,              // 100 req/s sustained
		BurstSize:         200,              // Allow bursts up to 200
		CleanupInterval:   5 * time.Minute,  // Clean up every 5 minutes
		ClientExpiration:  10 * time.Minute, // Remove clients inactive for 10 minutes
		MaxClients:        100000,           // Limit tracked clients to prevent memory exhaustion
	}
}

// tokenBucket implements the token bucket rate limiting algorithm
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

// RateLimiter manages rate limiting for multiple clients
type RateLimiter struct {
	config   *RateLimitConfig
	clients  map[string]*tokenBucket
	mu       sync.RWMutex
	stopChan chan struct{}
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
	if config == nil {
		config = DefaultRateLimitConfig()
	}

	rl := &RateLimiter{
		config:   config,
		clients:  make(map[string]*tokenBucket),
		stopChan: make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given client should be allowed.
// Returns false if the client is rate limited or if max clients has been reached.
func (rl *RateLimiter) Allow(clientID string) bool {
	bucket := rl.getBucket(clientID)

	// If bucket is nil, max clients was reached - deny the request
	if bucket == nil {
		return false
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()

	// Refill tokens based on elapsed time
	bucket.tokens += elapsed * rl.config.RequestsPerSecond
	if bucket.tokens > float64(rl.config.BurstSize) {
		bucket.tokens = float64(rl.config.BurstSize)
	}
	bucket.lastRefill = now

	// Check if we have tokens available
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}

	return false
}

// getBucket gets or creates a token bucket for a client.
// Returns nil if the client limit has been reached and no existing bucket exists.
func (rl *RateLimiter) getBucket(clientID string) *tokenBucket {
	rl.mu.RLock()
	bucket, exists := rl.clients[clientID]
	clientCount := len(rl.clients)
	rl.mu.RUnlock()

	if exists {
		return bucket
	}

	// Check if we've reached the maximum number of clients
	// This prevents memory exhaustion attacks
	if rl.config.MaxClients > 0 && clientCount >= rl.config.MaxClients {
		log.Printf("Rate limiter: max clients (%d) reached, rejecting new client %s", rl.config.MaxClients, clientID)
		return nil
	}

	// Create new bucket
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if bucket, exists = rl.clients[clientID]; exists {
		return bucket
	}

	// Re-check client count under write lock
	if rl.config.MaxClients > 0 && len(rl.clients) >= rl.config.MaxClients {
		return nil
	}

	bucket = &tokenBucket{
		tokens:     float64(rl.config.BurstSize), // Start with full bucket
		lastRefill: time.Now(),
	}
	rl.clients[clientID] = bucket
	return bucket
}

// cleanupLoop periodically removes expired client buckets
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopChan:
			return
		}
	}
}

// cleanup removes inactive client buckets
// Uses a two-phase approach to minimize lock contention:
// 1. Collect candidates under read lock
// 2. Delete expired entries under write lock
func (rl *RateLimiter) cleanup() {
	now := time.Now()
	expiredClients := make([]string, 0)

	// Phase 1: Identify expired clients under read lock
	rl.mu.RLock()
	for clientID, bucket := range rl.clients {
		bucket.mu.Lock()
		isExpired := now.Sub(bucket.lastRefill) > rl.config.ClientExpiration
		bucket.mu.Unlock()
		if isExpired {
			expiredClients = append(expiredClients, clientID)
		}
	}
	rl.mu.RUnlock()

	if len(expiredClients) == 0 {
		return
	}

	// Phase 2: Delete expired clients under write lock
	rl.mu.Lock()
	for _, clientID := range expiredClients {
		// Re-verify expiration (bucket may have been refreshed)
		if bucket, exists := rl.clients[clientID]; exists {
			bucket.mu.Lock()
			if now.Sub(bucket.lastRefill) > rl.config.ClientExpiration {
				delete(rl.clients, clientID)
			}
			bucket.mu.Unlock()
		}
	}
	rl.mu.Unlock()

	log.Printf("Rate limiter cleanup: removed %d expired clients", len(expiredClients))
}

// Stop stops the rate limiter cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// GetStats returns current rate limiter statistics
func (rl *RateLimiter) GetStats() map[string]any {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]any{
		"active_clients":      len(rl.clients),
		"requests_per_second": rl.config.RequestsPerSecond,
		"burst_size":          rl.config.BurstSize,
	}
}

// GetConfig returns the rate limiter configuration (for header values)
func (rl *RateLimiter) GetConfig() *RateLimitConfig {
	return rl.config
}

// ClientIDFunc is a function that extracts a client identifier from a request
type ClientIDFunc func(*http.Request) string

// RateLimit creates middleware that applies rate limiting per client.
// The getClientID function extracts the client identifier from the request.
// The onLimited function is called when a request is rate limited (optional).
func RateLimit(limiter *RateLimiter, getClientID ClientIDFunc, onLimited func(w http.ResponseWriter, r *http.Request, clientID string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting if not configured
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Get client identifier
			clientID := getClientID(r)

			// Check rate limit
			if !limiter.Allow(clientID) {
				// Log rate limit hit
				log.Printf("Rate limit exceeded for client: %s (path: %s)", clientID, r.URL.Path)

				// Call custom handler if provided
				if onLimited != nil {
					onLimited(w, r, clientID)
				}

				// Return 429 Too Many Requests with Retry-After header
				w.Header().Set("Retry-After", "1")
				w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(limiter.config.RequestsPerSecond, 'f', 0, 64))
				http.Error(w, "Rate limit exceeded. Please retry after 1 second.", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

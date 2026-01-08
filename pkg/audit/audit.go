package audit

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Action types for audit events
type Action string

const (
	ActionCreate Action = "create"
	ActionRead   Action = "read"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
	ActionAuth   Action = "auth"
	ActionQuery  Action = "query"
)

// ResourceType represents the type of resource being accessed
type ResourceType string

const (
	ResourceNode  ResourceType = "node"
	ResourceEdge  ResourceType = "edge"
	ResourceQuery ResourceType = "query"
	ResourceAuth  ResourceType = "auth"
	ResourceUser  ResourceType = "user"
	ResourceKey   ResourceType = "apikey"
)

// Status represents the outcome of an action
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
)

// Event represents a single audit log entry
type Event struct {
	ID           string                 `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	TenantID     string                 `json:"tenant_id,omitempty"` // Multi-tenancy: empty defaults to "default"
	UserID       string                 `json:"user_id,omitempty"`
	Username     string                 `json:"username,omitempty"`
	Action       Action                 `json:"action"`
	ResourceType ResourceType           `json:"resource_type"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	Status       Status                 `json:"status"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	IPAddress    string                 `json:"ip_address,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Filter represents filtering criteria for audit events
type Filter struct {
	TenantID     string // Filter by tenant (empty = all tenants)
	UserID       string
	Username     string
	Action       Action
	ResourceType ResourceType
	ResourceID   string
	Status       Status
	StartTime    *time.Time
	EndTime      *time.Time
}

// Logger is the interface for audit logging implementations.
// Both in-memory AuditLogger and PersistentAuditLogger implement this interface.
type Logger interface {
	// Log records an audit event
	Log(event *Event) error

	// GetEventCount returns the number of events logged
	GetEventCount() int64
}

// AuditLogger manages audit log events with a circular buffer
type AuditLogger struct {
	events     []*Event
	bufferSize int
	index      int
	count      int
	mu         sync.RWMutex
}

// NewAuditLogger creates a new audit logger with specified buffer size
func NewAuditLogger(bufferSize int) *AuditLogger {
	return &AuditLogger{
		events:     make([]*Event, bufferSize),
		bufferSize: bufferSize,
		index:      0,
		count:      0,
	}
}

// Log records an audit event
func (l *AuditLogger) Log(event *Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Set timestamp and ID if not set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	// Store in circular buffer
	l.events[l.index] = event
	l.index = (l.index + 1) % l.bufferSize

	// Track total count (up to buffer size)
	if l.count < l.bufferSize {
		l.count++
	}

	return nil
}

// GetEvents retrieves audit events with optional filtering
func (l *AuditLogger) GetEvents(filter *Filter) []*Event {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*Event, 0, l.count)

	// Iterate through all stored events
	for i := 0; i < l.count; i++ {
		// Calculate the actual index in the circular buffer
		idx := (l.index - l.count + i + l.bufferSize) % l.bufferSize
		event := l.events[idx]

		if event == nil {
			continue
		}

		// Apply filters
		if filter != nil {
			if filter.TenantID != "" && event.TenantID != filter.TenantID {
				continue
			}
			if filter.UserID != "" && event.UserID != filter.UserID {
				continue
			}
			if filter.Username != "" && event.Username != filter.Username {
				continue
			}
			if filter.Action != "" && event.Action != filter.Action {
				continue
			}
			if filter.ResourceType != "" && event.ResourceType != filter.ResourceType {
				continue
			}
			if filter.ResourceID != "" && event.ResourceID != filter.ResourceID {
				continue
			}
			if filter.Status != "" && event.Status != filter.Status {
				continue
			}
			if filter.StartTime != nil && event.Timestamp.Before(*filter.StartTime) {
				continue
			}
			if filter.EndTime != nil && event.Timestamp.After(*filter.EndTime) {
				continue
			}
		}

		result = append(result, event)
	}

	return result
}

// GetRecentEvents returns the N most recent events
func (l *AuditLogger) GetRecentEvents(n int) []*Event {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if n > l.count {
		n = l.count
	}

	result := make([]*Event, 0, n)

	// Get the most recent N events
	for i := 0; i < n; i++ {
		idx := (l.index - 1 - i + l.bufferSize) % l.bufferSize
		if l.events[idx] != nil {
			result = append(result, l.events[idx])
		}
	}

	return result
}

// GetEventCount returns the total number of events currently stored
func (l *AuditLogger) GetEventCount() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return int64(l.count)
}

// Clear removes all events from the logger
func (l *AuditLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = make([]*Event, l.bufferSize)
	l.index = 0
	l.count = 0
}

// Helper function to create a standard event
func NewEvent(userID, username string, action Action, resourceType ResourceType, resourceID string, status Status) *Event {
	return &Event{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		UserID:       userID,
		Username:     username,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Status:       status,
	}
}

// Helper function to create a failed event with error message
func NewFailedEvent(userID, username string, action Action, resourceType ResourceType, errorMsg string) *Event {
	return &Event{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		UserID:       userID,
		Username:     username,
		Action:       action,
		ResourceType: resourceType,
		Status:       StatusFailure,
		ErrorMessage: errorMsg,
	}
}

// String returns a human-readable representation of an event
func (e *Event) String() string {
	tenantStr := e.TenantID
	if tenantStr == "" {
		tenantStr = "default"
	}
	return fmt.Sprintf("[%s] tenant=%s %s %s %s %s (user: %s, status: %s)",
		e.Timestamp.Format(time.RFC3339),
		tenantStr,
		e.Username,
		e.Action,
		e.ResourceType,
		e.ResourceID,
		e.UserID,
		e.Status,
	)
}

// NewEventWithTenant creates an event with tenant context
func NewEventWithTenant(tenantID, userID, username string, action Action, resourceType ResourceType, resourceID string, status Status) *Event {
	return &Event{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		TenantID:     tenantID,
		UserID:       userID,
		Username:     username,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Status:       status,
	}
}

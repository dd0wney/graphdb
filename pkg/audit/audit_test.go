package audit

import (
	"fmt"
	"testing"
	"time"
)

// TestAuditLogger_LogEvent tests basic event logging
func TestAuditLogger_LogEvent(t *testing.T) {
	logger := NewAuditLogger(100) // Buffer size 100

	tests := []struct {
		name      string
		event     *Event
		wantError bool
	}{
		{
			name: "Valid node creation event",
			event: &Event{
				UserID:       "user123",
				Username:     "alice",
				Action:       ActionCreate,
				ResourceType: ResourceNode,
				ResourceID:   "node456",
				Status:       StatusSuccess,
				IPAddress:    "192.168.1.1",
				UserAgent:    "curl/7.68.0",
			},
			wantError: false,
		},
		{
			name: "Valid query execution event",
			event: &Event{
				UserID:       "user123",
				Username:     "alice",
				Action:       ActionRead,
				ResourceType: ResourceQuery,
				Status:       StatusSuccess,
				Metadata: map[string]any{
					"query": "MATCH (n) RETURN n LIMIT 10",
				},
			},
			wantError: false,
		},
		{
			name: "Failed authentication event",
			event: &Event{
				Username:     "attacker",
				Action:       ActionAuth,
				ResourceType: ResourceAuth,
				Status:       StatusFailure,
				ErrorMessage: "Invalid credentials",
				IPAddress:    "10.0.0.1",
			},
			wantError: false,
		},
		{
			name: "Valid edge deletion event",
			event: &Event{
				UserID:       "user456",
				Username:     "bob",
				Action:       ActionDelete,
				ResourceType: ResourceEdge,
				ResourceID:   "edge789",
				Status:       StatusSuccess,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := logger.Log(tt.event)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				// Verify event has timestamp
				if tt.event.Timestamp.IsZero() {
					t.Error("Expected non-zero timestamp")
				}

				// Verify event has ID
				if tt.event.ID == "" {
					t.Error("Expected non-empty event ID")
				}
			}
		})
	}
}

// TestAuditLogger_GetEvents tests retrieving logged events
func TestAuditLogger_GetEvents(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log some events
	events := []*Event{
		{
			UserID:       "user123",
			Username:     "alice",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			Status:       StatusSuccess,
		},
		{
			UserID:       "user123",
			Username:     "alice",
			Action:       ActionRead,
			ResourceType: ResourceQuery,
			Status:       StatusSuccess,
		},
		{
			UserID:       "user456",
			Username:     "bob",
			Action:       ActionDelete,
			ResourceType: ResourceEdge,
			Status:       StatusSuccess,
		},
	}

	for _, e := range events {
		if err := logger.Log(e); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Get all events
	retrieved := logger.GetEvents(nil)
	if len(retrieved) != 3 {
		t.Errorf("Expected 3 events, got %d", len(retrieved))
	}
}

// TestAuditLogger_FilterByUser tests filtering events by user
func TestAuditLogger_FilterByUser(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log events for different users
	if err := logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user456",
		Username:     "bob",
		Action:       ActionDelete,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Filter by alice
	filter := &Filter{UserID: "user123"}
	events := logger.GetEvents(filter)

	if len(events) != 2 {
		t.Errorf("Expected 2 events for alice, got %d", len(events))
	}

	for _, e := range events {
		if e.UserID != "user123" {
			t.Errorf("Expected UserID user123, got %s", e.UserID)
		}
	}
}

// TestAuditLogger_FilterByAction tests filtering by action type
func TestAuditLogger_FilterByAction(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log various actions
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Filter by ActionCreate
	filter := &Filter{Action: ActionCreate}
	events := logger.GetEvents(filter)

	if len(events) != 2 {
		t.Errorf("Expected 2 create events, got %d", len(events))
	}

	for _, e := range events {
		if e.Action != ActionCreate {
			t.Errorf("Expected ActionCreate, got %s", e.Action)
		}
	}
}

// TestAuditLogger_FilterByResourceType tests filtering by resource
func TestAuditLogger_FilterByResourceType(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log events for different resources
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Filter by ResourceNode
	filter := &Filter{ResourceType: ResourceNode}
	events := logger.GetEvents(filter)

	if len(events) != 2 {
		t.Errorf("Expected 2 node events, got %d", len(events))
	}
}

// TestAuditLogger_FilterByStatus tests filtering by status
func TestAuditLogger_FilterByStatus(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log successful and failed events
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusFailure,
		ErrorMessage: "Validation error",
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Filter by StatusFailure
	filter := &Filter{Status: StatusFailure}
	events := logger.GetEvents(filter)

	if len(events) != 1 {
		t.Errorf("Expected 1 failed event, got %d", len(events))
	}

	if events[0].Status != StatusFailure {
		t.Errorf("Expected StatusFailure, got %s", events[0].Status)
	}
}

// TestAuditLogger_FilterByTimeRange tests time-based filtering
func TestAuditLogger_FilterByTimeRange(t *testing.T) {
	logger := NewAuditLogger(100)

	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	// Log events
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Small delay to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Filter by time range
	filter := &Filter{
		StartTime: &yesterday,
		EndTime:   &tomorrow,
	}
	events := logger.GetEvents(filter)

	if len(events) != 2 {
		t.Errorf("Expected 2 events in time range, got %d", len(events))
	}
}

// TestAuditLogger_BufferOverflow tests circular buffer behavior
func TestAuditLogger_BufferOverflow(t *testing.T) {
	bufferSize := 10
	logger := NewAuditLogger(bufferSize)

	// Log more events than buffer size
	for i := 0; i < 15; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			ResourceID:   fmt.Sprintf("node%d", i),
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Should only have the last 10 events
	events := logger.GetEvents(nil)
	if len(events) != bufferSize {
		t.Errorf("Expected %d events (buffer size), got %d", bufferSize, len(events))
	}

	// First event should be node5 (events 0-4 were discarded)
	if events[0].ResourceID != "node5" {
		t.Errorf("Expected first event to be node5, got %s", events[0].ResourceID)
	}
}

// TestAuditLogger_CombinedFilters tests multiple filters
func TestAuditLogger_CombinedFilters(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log various events
	if err := logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}
	if err := logger.Log(&Event{
		UserID:       "user456",
		Username:     "bob",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Filter by user AND resource type
	filter := &Filter{
		UserID:       "user123",
		ResourceType: ResourceNode,
	}
	events := logger.GetEvents(filter)

	if len(events) != 1 {
		t.Errorf("Expected 1 event matching combined filters, got %d", len(events))
	}

	if events[0].UserID != "user123" || events[0].ResourceType != ResourceNode {
		t.Error("Event does not match combined filter criteria")
	}
}

// TestAuditLogger_ThreadSafety tests concurrent logging
func TestAuditLogger_ThreadSafety(t *testing.T) {
	logger := NewAuditLogger(1000)

	// Launch multiple goroutines logging concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				if err := logger.Log(&Event{
					UserID:       fmt.Sprintf("user%d", id),
					Action:       ActionCreate,
					ResourceType: ResourceNode,
					Status:       StatusSuccess,
				}); err != nil {
					t.Errorf("Failed to log event: %v", err)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have logged 1000 events (10 goroutines * 100 events each)
	// But buffer is 1000, so we should have exactly 1000
	events := logger.GetEvents(nil)
	if len(events) != 1000 {
		t.Errorf("Expected 1000 events, got %d", len(events))
	}
}

// TestAuditLogger_GetRecentEvents tests retrieving N most recent events
func TestAuditLogger_GetRecentEvents(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log 10 events
	for i := 0; i < 10; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			ResourceID:   fmt.Sprintf("node%d", i),
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	}

	tests := []struct {
		name     string
		n        int
		expected int
	}{
		{"Get 5 recent events", 5, 5},
		{"Get 3 recent events", 3, 3},
		{"Get more than available", 20, 10}, // Should return all 10
		{"Get 0 events", 0, 0},
		{"Get 1 event", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := logger.GetRecentEvents(tt.n)
			if len(events) != tt.expected {
				t.Errorf("Expected %d events, got %d", tt.expected, len(events))
			}

			// Verify events are in reverse chronological order (most recent first)
			if len(events) > 1 {
				for i := 0; i < len(events)-1; i++ {
					if events[i].Timestamp.Before(events[i+1].Timestamp) {
						t.Error("Events are not in reverse chronological order")
					}
				}
			}
		})
	}
}

// TestAuditLogger_GetRecentEvents_MostRecent tests that most recent events are returned
func TestAuditLogger_GetRecentEvents_MostRecent(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log events with identifiable resource IDs
	for i := 0; i < 5; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			ResourceID:   fmt.Sprintf("node%d", i),
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Get 2 most recent
	events := logger.GetRecentEvents(2)

	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	// Most recent should be node4, then node3
	if events[0].ResourceID != "node4" {
		t.Errorf("Expected most recent to be node4, got %s", events[0].ResourceID)
	}
	if events[1].ResourceID != "node3" {
		t.Errorf("Expected second most recent to be node3, got %s", events[1].ResourceID)
	}
}

// TestAuditLogger_GetEventCount tests counting stored events
func TestAuditLogger_GetEventCount(t *testing.T) {
	logger := NewAuditLogger(100)

	// Initially empty
	if count := logger.GetEventCount(); count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	// Log some events
	for i := 0; i < 5; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	if count := logger.GetEventCount(); count != int64(5) {
		t.Errorf("Expected count 5, got %d", count)
	}

	// Log more events
	for i := 0; i < 3; i++ {
		if err := logger.Log(&Event{
			UserID:       "user456",
			Action:       ActionRead,
			ResourceType: ResourceNode,
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	if count := logger.GetEventCount(); count != int64(8) {
		t.Errorf("Expected count 8, got %d", count)
	}
}

// TestAuditLogger_GetEventCount_BufferLimit tests count with buffer overflow
func TestAuditLogger_GetEventCount_BufferLimit(t *testing.T) {
	bufferSize := 10
	logger := NewAuditLogger(bufferSize)

	// Log more than buffer size
	for i := 0; i < 15; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Count should be capped at buffer size
	if count := logger.GetEventCount(); count != int64(bufferSize) {
		t.Errorf("Expected count %d (buffer size), got %d", bufferSize, count)
	}
}

// TestAuditLogger_Clear tests clearing all events
func TestAuditLogger_Clear(t *testing.T) {
	logger := NewAuditLogger(100)

	// Log some events
	for i := 0; i < 10; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			Status:       StatusSuccess,
		}); err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}

	// Verify events exist
	if count := logger.GetEventCount(); count != int64(10) {
		t.Errorf("Expected 10 events before clear, got %d", count)
	}

	// Clear
	logger.Clear()

	// Verify all cleared
	if count := logger.GetEventCount(); count != int64(0) {
		t.Errorf("Expected 0 events after clear, got %d", count)
	}

	events := logger.GetEvents(nil)
	if len(events) != 0 {
		t.Errorf("Expected 0 events after clear, got %d", len(events))
	}
}

// TestAuditLogger_Clear_Idempotent tests clearing empty logger
func TestAuditLogger_Clear_Idempotent(t *testing.T) {
	logger := NewAuditLogger(100)

	// Clear when already empty (should not panic)
	logger.Clear()
	logger.Clear()

	if count := logger.GetEventCount(); count != int64(0) {
		t.Errorf("Expected count 0 after multiple clears, got %d", count)
	}

	// Can still log after clear
	if err := logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}); err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	if count := logger.GetEventCount(); count != int64(1) {
		t.Errorf("Expected count 1 after logging post-clear, got %d", count)
	}
}

// TestNewEvent tests creating a standard event
func TestNewEvent(t *testing.T) {
	event := NewEvent(
		"user123",
		"alice",
		ActionCreate,
		ResourceNode,
		"node456",
		StatusSuccess,
	)

	if event == nil {
		t.Fatal("NewEvent returned nil")
	}

	// Verify fields
	if event.UserID != "user123" {
		t.Errorf("Expected UserID user123, got %s", event.UserID)
	}
	if event.Username != "alice" {
		t.Errorf("Expected Username alice, got %s", event.Username)
	}
	if event.Action != ActionCreate {
		t.Errorf("Expected Action create, got %s", event.Action)
	}
	if event.ResourceType != ResourceNode {
		t.Errorf("Expected ResourceType node, got %s", event.ResourceType)
	}
	if event.ResourceID != "node456" {
		t.Errorf("Expected ResourceID node456, got %s", event.ResourceID)
	}
	if event.Status != StatusSuccess {
		t.Errorf("Expected Status success, got %s", event.Status)
	}

	// Verify auto-generated fields
	if event.ID == "" {
		t.Error("Expected non-empty ID")
	}
	if event.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
}

// TestNewEvent_AllActions tests creating events for all action types
func TestNewEvent_AllActions(t *testing.T) {
	actions := []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete, ActionAuth, ActionQuery}

	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			event := NewEvent("user123", "alice", action, ResourceNode, "node456", StatusSuccess)

			if event.Action != action {
				t.Errorf("Expected Action %s, got %s", action, event.Action)
			}
		})
	}
}

// TestNewFailedEvent tests creating a failed event with error message
func TestNewFailedEvent(t *testing.T) {
	errorMsg := "Invalid credentials"
	event := NewFailedEvent(
		"user123",
		"attacker",
		ActionAuth,
		ResourceAuth,
		errorMsg,
	)

	if event == nil {
		t.Fatal("NewFailedEvent returned nil")
	}

	// Verify fields
	if event.UserID != "user123" {
		t.Errorf("Expected UserID user123, got %s", event.UserID)
	}
	if event.Username != "attacker" {
		t.Errorf("Expected Username attacker, got %s", event.Username)
	}
	if event.Action != ActionAuth {
		t.Errorf("Expected Action auth, got %s", event.Action)
	}
	if event.ResourceType != ResourceAuth {
		t.Errorf("Expected ResourceType auth, got %s", event.ResourceType)
	}
	if event.Status != StatusFailure {
		t.Errorf("Expected Status failure, got %s", event.Status)
	}
	if event.ErrorMessage != errorMsg {
		t.Errorf("Expected ErrorMessage '%s', got '%s'", errorMsg, event.ErrorMessage)
	}

	// Verify auto-generated fields
	if event.ID == "" {
		t.Error("Expected non-empty ID")
	}
	if event.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	// ResourceID should be empty for failed events
	if event.ResourceID != "" {
		t.Errorf("Expected empty ResourceID for failed event, got %s", event.ResourceID)
	}
}

// TestNewFailedEvent_Various tests different failure scenarios
func TestNewFailedEvent_Various(t *testing.T) {
	tests := []struct {
		name         string
		action       Action
		resourceType ResourceType
		errorMsg     string
	}{
		{"Auth failure", ActionAuth, ResourceAuth, "Invalid credentials"},
		{"Create failure", ActionCreate, ResourceNode, "Validation error"},
		{"Delete failure", ActionDelete, ResourceEdge, "Permission denied"},
		{"Query failure", ActionQuery, ResourceQuery, "Syntax error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewFailedEvent("user123", "alice", tt.action, tt.resourceType, tt.errorMsg)

			if event.Status != StatusFailure {
				t.Error("Expected Status failure")
			}
			if event.ErrorMessage != tt.errorMsg {
				t.Errorf("Expected error message '%s', got '%s'", tt.errorMsg, event.ErrorMessage)
			}
		})
	}
}

// TestEvent_String tests human-readable event formatting
func TestEvent_String(t *testing.T) {
	event := &Event{
		ID:           "event-123",
		Timestamp:    time.Date(2024, 11, 19, 10, 30, 0, 0, time.UTC),
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		ResourceID:   "node456",
		Status:       StatusSuccess,
	}

	str := event.String()

	// Verify string contains key information
	if !containsAll(str, []string{"alice", "create", "node", "node456", "user123", "success"}) {
		t.Errorf("String representation missing key info: %s", str)
	}

	// Verify timestamp is formatted
	if !containsAll(str, []string{"2024"}) {
		t.Errorf("String representation missing timestamp: %s", str)
	}
}

// TestEvent_String_FailedEvent tests formatting of failed events
func TestEvent_String_FailedEvent(t *testing.T) {
	event := NewFailedEvent("user456", "bob", ActionAuth, ResourceAuth, "Invalid password")

	str := event.String()

	// Should contain failure status
	if !containsAll(str, []string{"bob", "auth", "failure"}) {
		t.Errorf("Failed event string missing key info: %s", str)
	}
}

// TestEvent_String_EmptyFields tests formatting with empty optional fields
func TestEvent_String_EmptyFields(t *testing.T) {
	event := &Event{
		ID:           "event-123",
		Timestamp:    time.Now(),
		Username:     "alice",
		Action:       ActionRead,
		ResourceType: ResourceQuery,
		Status:       StatusSuccess,
		// UserID and ResourceID are empty
	}

	str := event.String()

	// Should not panic with empty fields
	if str == "" {
		t.Error("String representation is empty")
	}
}

// Helper function to check if string contains all substrings
func containsAll(s string, substrs []string) bool {
	for _, substr := range substrs {
		if !contains(s, substr) {
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark audit operations
func BenchmarkAuditLogger_Log(b *testing.B) {
	logger := NewAuditLogger(10000)
	event := &Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := logger.Log(event); err != nil {
			b.Fatalf("Failed to log event: %v", err)
		}
	}
}

func BenchmarkAuditLogger_GetEvents(b *testing.B) {
	logger := NewAuditLogger(10000)
	for i := 0; i < 1000; i++ {
		if err := logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			Status:       StatusSuccess,
		}); err != nil {
			b.Fatalf("Failed to log event: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.GetEvents(nil)
	}
}

func BenchmarkNewEvent(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewEvent("user123", "alice", ActionCreate, ResourceNode, "node456", StatusSuccess)
	}
}

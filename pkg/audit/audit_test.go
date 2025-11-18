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
				Metadata: map[string]interface{}{
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
		logger.Log(e)
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
	logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user456",
		Username:     "bob",
		Action:       ActionDelete,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	})

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
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})

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
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	})

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
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusFailure,
		ErrorMessage: "Validation error",
	})
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})

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
	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})

	// Small delay to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	logger.Log(&Event{
		UserID:       "user123",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})

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
		logger.Log(&Event{
			UserID:       "user123",
			Action:       ActionCreate,
			ResourceType: ResourceNode,
			ResourceID:   fmt.Sprintf("node%d", i),
			Status:       StatusSuccess,
		})
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
	logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user123",
		Username:     "alice",
		Action:       ActionCreate,
		ResourceType: ResourceEdge,
		Status:       StatusSuccess,
	})
	logger.Log(&Event{
		UserID:       "user456",
		Username:     "bob",
		Action:       ActionCreate,
		ResourceType: ResourceNode,
		Status:       StatusSuccess,
	})

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
				logger.Log(&Event{
					UserID:       fmt.Sprintf("user%d", id),
					Action:       ActionCreate,
					ResourceType: ResourceNode,
					Status:       StatusSuccess,
				})
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

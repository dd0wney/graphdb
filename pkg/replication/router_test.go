package replication

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestMessageRouter_Handle(t *testing.T) {
	router := NewMessageRouter()

	called := false
	router.Handle(MsgHeartbeat, func(data []byte) error {
		called = true
		return nil
	})

	if !router.HasHandler(MsgHeartbeat) {
		t.Error("Expected handler to be registered")
	}

	msg, err := NewMessage(MsgHeartbeat, HeartbeatMessage{From: "test"})
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	if err := router.Dispatch(msg); err != nil {
		t.Errorf("Dispatch failed: %v", err)
	}

	if !called {
		t.Error("Handler was not called")
	}
}

func TestMessageRouter_ChainHandle(t *testing.T) {
	router := NewMessageRouter()

	router.
		Handle(MsgHeartbeat, func(data []byte) error { return nil }).
		Handle(MsgAck, func(data []byte) error { return nil }).
		Handle(MsgError, func(data []byte) error { return nil })

	if router.HandlerCount() != 3 {
		t.Errorf("Expected 3 handlers, got %d", router.HandlerCount())
	}
}

func TestMessageRouter_NoHandler(t *testing.T) {
	router := NewMessageRouter()

	msg, err := NewMessage(MsgHeartbeat, HeartbeatMessage{From: "test"})
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	err = router.Dispatch(msg)
	if err == nil {
		t.Error("Expected error when dispatching to unregistered handler")
	}
}

func TestMessageRouter_HandlerError(t *testing.T) {
	router := NewMessageRouter()

	expectedErr := errors.New("handler error")
	router.Handle(MsgHeartbeat, func(data []byte) error {
		return expectedErr
	})

	msg, err := NewMessage(MsgHeartbeat, HeartbeatMessage{From: "test"})
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	err = router.Dispatch(msg)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestMessageRouter_DispatchRaw(t *testing.T) {
	router := NewMessageRouter()

	var received HeartbeatMessage
	router.Handle(MsgHeartbeat, func(data []byte) error {
		return json.Unmarshal(data, &received)
	})

	msg, _ := NewMessage(MsgHeartbeat, HeartbeatMessage{From: "test-node", CurrentLSN: 42})
	rawData, _ := json.Marshal(msg)

	if err := router.DispatchRaw(rawData); err != nil {
		t.Errorf("DispatchRaw failed: %v", err)
	}

	if received.From != "test-node" {
		t.Errorf("Expected From='test-node', got %q", received.From)
	}
	if received.CurrentLSN != 42 {
		t.Errorf("Expected CurrentLSN=42, got %d", received.CurrentLSN)
	}
}

func TestMessageRouter_ConcurrentAccess(t *testing.T) {
	router := NewMessageRouter()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		// Concurrent writes
		go func(msgType MessageType) {
			defer wg.Done()
			router.Handle(msgType, func(data []byte) error { return nil })
		}(MessageType(i % 7))

		// Concurrent reads
		go func() {
			defer wg.Done()
			_ = router.HasHandler(MsgHeartbeat)
		}()
	}

	wg.Wait()
}

func TestHandleFunc_TypedHandler(t *testing.T) {
	router := NewMessageRouter()

	var received *HeartbeatMessage
	HandleFunc(router, MsgHeartbeat, func(hb *HeartbeatMessage) error {
		received = hb
		return nil
	})

	msg, _ := NewMessage(MsgHeartbeat, HeartbeatMessage{From: "typed-test", CurrentLSN: 100})
	if err := router.Dispatch(msg); err != nil {
		t.Errorf("Dispatch failed: %v", err)
	}

	if received == nil {
		t.Fatal("Handler was not called")
	}
	if received.From != "typed-test" {
		t.Errorf("Expected From='typed-test', got %q", received.From)
	}
}

func TestMessageTypeName(t *testing.T) {
	tests := []struct {
		msgType  MessageType
		expected string
	}{
		{MsgHandshake, "Handshake"},
		{MsgHeartbeat, "Heartbeat"},
		{MsgAck, "Ack"},
		{MsgSync, "Sync"},
		{MsgWALEntry, "WALEntry"},
		{MsgSnapshot, "Snapshot"},
		{MsgError, "Error"},
		{MessageType(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			name := MessageTypeName(tt.msgType)
			if name != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, name)
			}
		})
	}
}

func TestMessageBuilder(t *testing.T) {
	hb := HeartbeatMessage{
		From:       "builder-test",
		CurrentLSN: 200,
	}

	msg, err := Heartbeat().WithData(hb).Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if msg.Type != MsgHeartbeat {
		t.Errorf("Expected type MsgHeartbeat, got %d", msg.Type)
	}
	if msg.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}

	// Decode and verify
	var decoded HeartbeatMessage
	if err := msg.Decode(&decoded); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decoded.From != "builder-test" {
		t.Errorf("Expected From='builder-test', got %q", decoded.From)
	}
}

func TestMessageBuilder_MustBuild(t *testing.T) {
	// Test successful MustBuild
	msg := Heartbeat().WithData(HeartbeatMessage{From: "must-test"}).MustBuild()
	if msg.Type != MsgHeartbeat {
		t.Error("MustBuild failed to create correct message type")
	}
}

func TestMessageBuilderShortcuts(t *testing.T) {
	tests := []struct {
		name     string
		builder  *MessageBuilder
		expected MessageType
	}{
		{"Handshake", Handshake(), MsgHandshake},
		{"Heartbeat", Heartbeat(), MsgHeartbeat},
		{"Ack", Ack(), MsgAck},
		{"WALEntry", WALEntry(), MsgWALEntry},
		{"ErrorMsg", ErrorMsg(), MsgError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := tt.builder.WithData(struct{}{}).Build()
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}
			if msg.Type != tt.expected {
				t.Errorf("Expected type %d, got %d", tt.expected, msg.Type)
			}
		})
	}
}

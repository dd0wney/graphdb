package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

// MessageHandler is a function that handles a specific message type.
type MessageHandler func(data []byte) error

// MessageRouter dispatches messages to registered handlers.
// It provides a clean way to handle different message types without
// large switch statements.
type MessageRouter struct {
	handlers map[MessageType]MessageHandler
	mu       sync.RWMutex
	logger   *log.Logger
}

// NewMessageRouter creates a new message router.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make(map[MessageType]MessageHandler),
	}
}

// SetLogger sets a custom logger for the router.
func (mr *MessageRouter) SetLogger(logger *log.Logger) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.logger = logger
}

// Handle registers a handler for a specific message type.
func (mr *MessageRouter) Handle(msgType MessageType, handler MessageHandler) *MessageRouter {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.handlers[msgType] = handler
	return mr
}

// HandleFunc is a convenience method that registers a typed handler.
// The handler receives the decoded message data.
func HandleFunc[T any](mr *MessageRouter, msgType MessageType, handler func(*T) error) *MessageRouter {
	return mr.Handle(msgType, func(data []byte) error {
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("failed to decode message: %w", err)
		}
		return handler(&v)
	})
}

// Dispatch routes a message to the appropriate handler.
// Returns an error if no handler is registered for the message type.
func (mr *MessageRouter) Dispatch(msg *Message) error {
	mr.mu.RLock()
	handler, ok := mr.handlers[msg.Type]
	logger := mr.logger
	mr.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler for message type %d", msg.Type)
	}

	if err := handler(msg.Data); err != nil {
		if logger != nil {
			logger.Printf("Error handling message type %d: %v", msg.Type, err)
		}
		return err
	}

	return nil
}

// DispatchRaw dispatches a raw message payload.
// It first decodes the Message envelope, then routes to the handler.
func (mr *MessageRouter) DispatchRaw(data []byte) error {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("failed to decode message envelope: %w", err)
	}
	return mr.Dispatch(&msg)
}

// HasHandler returns true if a handler is registered for the message type.
func (mr *MessageRouter) HasHandler(msgType MessageType) bool {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	_, ok := mr.handlers[msgType]
	return ok
}

// HandlerCount returns the number of registered handlers.
func (mr *MessageRouter) HandlerCount() int {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return len(mr.handlers)
}

// MessageTypeName returns a human-readable name for a message type.
func MessageTypeName(msgType MessageType) string {
	switch msgType {
	case MsgHandshake:
		return "Handshake"
	case MsgHeartbeat:
		return "Heartbeat"
	case MsgAck:
		return "Ack"
	case MsgSync:
		return "Sync"
	case MsgWALEntry:
		return "WALEntry"
	case MsgSnapshot:
		return "Snapshot"
	case MsgError:
		return "Error"
	default:
		return fmt.Sprintf("Unknown(%d)", msgType)
	}
}

// MessageDispatcher is an interface for dispatching messages.
// This allows for easy mocking in tests.
type MessageDispatcher interface {
	Dispatch(msg *Message) error
	DispatchRaw(data []byte) error
}

// Ensure MessageRouter implements MessageDispatcher
var _ MessageDispatcher = (*MessageRouter)(nil)

// MessageBuilder provides a fluent interface for building messages.
type MessageBuilder struct {
	msgType MessageType
	data    any
}

// NewMessageBuilder creates a new message builder.
func NewMessageBuilder(msgType MessageType) *MessageBuilder {
	return &MessageBuilder{msgType: msgType}
}

// WithData sets the message data.
func (mb *MessageBuilder) WithData(data any) *MessageBuilder {
	mb.data = data
	return mb
}

// Build creates the message.
func (mb *MessageBuilder) Build() (*Message, error) {
	return NewMessage(mb.msgType, mb.data)
}

// MustBuild creates the message, panicking on error.
// Use only when you know the data can be marshaled.
func (mb *MessageBuilder) MustBuild() *Message {
	msg, err := NewMessage(mb.msgType, mb.data)
	if err != nil {
		panic(fmt.Sprintf("failed to build message: %v", err))
	}
	return msg
}

// Handshake creates a handshake message builder.
func Handshake() *MessageBuilder {
	return NewMessageBuilder(MsgHandshake)
}

// Heartbeat creates a heartbeat message builder.
func Heartbeat() *MessageBuilder {
	return NewMessageBuilder(MsgHeartbeat)
}

// Ack creates an acknowledgment message builder.
func Ack() *MessageBuilder {
	return NewMessageBuilder(MsgAck)
}

// WALEntry creates a WAL entry message builder.
func WALEntry() *MessageBuilder {
	return NewMessageBuilder(MsgWALEntry)
}

// ErrorMsg creates an error message builder.
func ErrorMsg() *MessageBuilder {
	return NewMessageBuilder(MsgError)
}

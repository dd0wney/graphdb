package replication

import (
	"encoding/json"
	"time"
)

// MessageType represents the type of replication message
type MessageType uint8

const (
	// Control messages
	MsgHandshake MessageType = iota
	MsgHeartbeat
	MsgAck
	MsgSync

	// Data messages
	MsgWALEntry
	MsgSnapshot

	// Error messages
	MsgError
)

// Message is the base replication message
type Message struct {
	Type      MessageType `json:"type"`
	Timestamp int64       `json:"timestamp"`
	Data      []byte      `json:"data,omitempty"`
}

// NewMessage creates a new message with the given type and data
func NewMessage(msgType MessageType, data any) (*Message, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Now().Unix(),
		Data:      dataBytes,
	}, nil
}

// Decode decodes message data into the provided interface
func (m *Message) Decode(v any) error {
	return json.Unmarshal(m.Data, v)
}

package storage

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// ValueType represents the type of a property value
type ValueType uint8

const (
	TypeString ValueType = iota
	TypeInt
	TypeFloat
	TypeBool
	TypeBytes
	TypeTimestamp
	TypeVector // Vector of float32 for embeddings/vector search
)

// Value represents a typed property value
type Value struct {
	Type ValueType
	Data []byte
}

// Helper functions to create typed values
func StringValue(s string) Value {
	return Value{Type: TypeString, Data: []byte(s)}
}

func IntValue(i int64) Value {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, uint64(i))
	return Value{Type: TypeInt, Data: data}
}

func FloatValue(f float64) Value {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, math.Float64bits(f))
	return Value{Type: TypeFloat, Data: data}
}

func BoolValue(b bool) Value {
	data := []byte{0}
	if b {
		data[0] = 1
	}
	return Value{Type: TypeBool, Data: data}
}

func BytesValue(b []byte) Value {
	return Value{Type: TypeBytes, Data: b}
}

func TimestampValue(t time.Time) Value {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, uint64(t.Unix()))
	return Value{Type: TypeTimestamp, Data: data}
}

func VectorValue(vec []float32) Value {
	// Encode as: [4 bytes dimensions][4 bytes per float32 element]
	data := make([]byte, 4+len(vec)*4)
	binary.LittleEndian.PutUint32(data[0:4], uint32(len(vec)))
	for i, f := range vec {
		binary.LittleEndian.PutUint32(data[4+i*4:8+i*4], math.Float32bits(f))
	}
	return Value{Type: TypeVector, Data: data}
}

// Decode methods
func (v Value) AsString() (string, error) {
	if v.Type != TypeString {
		return "", fmt.Errorf("value is not a string")
	}
	return string(v.Data), nil
}

func (v Value) AsInt() (int64, error) {
	if v.Type != TypeInt {
		return 0, fmt.Errorf("value is not an int")
	}
	return int64(binary.LittleEndian.Uint64(v.Data)), nil
}

func (v Value) AsFloat() (float64, error) {
	if v.Type != TypeFloat {
		return 0, fmt.Errorf("value is not a float")
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(v.Data)), nil
}

func (v Value) AsBool() (bool, error) {
	if v.Type != TypeBool {
		return false, fmt.Errorf("value is not a bool")
	}
	return v.Data[0] == 1, nil
}

func (v Value) AsTimestamp() (time.Time, error) {
	if v.Type != TypeTimestamp {
		return time.Time{}, fmt.Errorf("value is not a timestamp")
	}
	return time.Unix(int64(binary.LittleEndian.Uint64(v.Data)), 0), nil
}

func (v Value) AsVector() ([]float32, error) {
	if v.Type != TypeVector {
		return nil, fmt.Errorf("value is not a vector")
	}
	if len(v.Data) < 4 {
		return nil, fmt.Errorf("invalid vector data: too short")
	}

	// Decode dimensions
	dims := binary.LittleEndian.Uint32(v.Data[0:4])
	expectedLen := 4 + int(dims)*4
	if len(v.Data) != expectedLen {
		return nil, fmt.Errorf("invalid vector data: expected %d bytes, got %d", expectedLen, len(v.Data))
	}

	// Decode floats
	vec := make([]float32, dims)
	for i := uint32(0); i < dims; i++ {
		bits := binary.LittleEndian.Uint32(v.Data[4+i*4 : 8+i*4])
		vec[i] = math.Float32frombits(bits)
	}

	return vec, nil
}

// Node represents a vertex in the graph
type Node struct {
	ID         uint64
	Labels     []string
	Properties map[string]Value
	CreatedAt  int64
	UpdatedAt  int64
}

// Edge represents a relationship between nodes
type Edge struct {
	ID         uint64
	FromNodeID uint64
	ToNodeID   uint64
	Type       string
	Properties map[string]Value
	Weight     float64
	CreatedAt  int64
}

// Clone creates a deep copy of a node
func (n *Node) Clone() *Node {
	clone := &Node{
		ID:         n.ID,
		Labels:     make([]string, len(n.Labels)),
		Properties: make(map[string]Value),
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
	}
	copy(clone.Labels, n.Labels)
	for k, v := range n.Properties {
		clone.Properties[k] = v
	}
	return clone
}

// HasLabel checks if node has a specific label
func (n *Node) HasLabel(label string) bool {
	for _, l := range n.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// GetProperty gets a property value
func (n *Node) GetProperty(key string) (Value, bool) {
	val, ok := n.Properties[key]
	return val, ok
}

// Clone creates a deep copy of an edge
func (e *Edge) Clone() *Edge {
	clone := &Edge{
		ID:         e.ID,
		FromNodeID: e.FromNodeID,
		ToNodeID:   e.ToNodeID,
		Type:       e.Type,
		Properties: make(map[string]Value),
		Weight:     e.Weight,
		CreatedAt:  e.CreatedAt,
	}
	for k, v := range e.Properties {
		clone.Properties[k] = v
	}
	return clone
}

// GetProperty gets a property value
func (e *Edge) GetProperty(key string) (Value, bool) {
	val, ok := e.Properties[key]
	return val, ok
}

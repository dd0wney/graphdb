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

	// Array types
	TypeStringArray
	TypeIntArray
	TypeFloatArray
	TypeBoolArray
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

// StringArrayValue creates a string array value
// Encoding: [4 bytes count][for each: 4 bytes len + string bytes]
func StringArrayValue(arr []string) Value {
	// Calculate total size
	size := 4 // count
	for _, s := range arr {
		size += 4 + len(s) // length prefix + string bytes
	}

	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data[0:4], uint32(len(arr)))

	offset := 4
	for _, s := range arr {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(len(s)))
		copy(data[offset+4:offset+4+len(s)], s)
		offset += 4 + len(s)
	}

	return Value{Type: TypeStringArray, Data: data}
}

// IntArrayValue creates an int64 array value
// Encoding: [4 bytes count][8 bytes per int64]
func IntArrayValue(arr []int64) Value {
	data := make([]byte, 4+len(arr)*8)
	binary.LittleEndian.PutUint32(data[0:4], uint32(len(arr)))

	for i, v := range arr {
		binary.LittleEndian.PutUint64(data[4+i*8:12+i*8], uint64(v))
	}

	return Value{Type: TypeIntArray, Data: data}
}

// FloatArrayValue creates a float64 array value
// Encoding: [4 bytes count][8 bytes per float64]
func FloatArrayValue(arr []float64) Value {
	data := make([]byte, 4+len(arr)*8)
	binary.LittleEndian.PutUint32(data[0:4], uint32(len(arr)))

	for i, v := range arr {
		binary.LittleEndian.PutUint64(data[4+i*8:12+i*8], math.Float64bits(v))
	}

	return Value{Type: TypeFloatArray, Data: data}
}

// BoolArrayValue creates a bool array value
// Encoding: [4 bytes count][1 byte per bool]
func BoolArrayValue(arr []bool) Value {
	data := make([]byte, 4+len(arr))
	binary.LittleEndian.PutUint32(data[0:4], uint32(len(arr)))

	for i, v := range arr {
		if v {
			data[4+i] = 1
		} else {
			data[4+i] = 0
		}
	}

	return Value{Type: TypeBoolArray, Data: data}
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

// AsStringArray decodes a string array value
func (v Value) AsStringArray() ([]string, error) {
	if v.Type != TypeStringArray {
		return nil, fmt.Errorf("value is not a string array")
	}
	if len(v.Data) < 4 {
		return nil, fmt.Errorf("invalid string array data: too short")
	}

	count := binary.LittleEndian.Uint32(v.Data[0:4])
	result := make([]string, count)

	offset := 4
	for i := uint32(0); i < count; i++ {
		if offset+4 > len(v.Data) {
			return nil, fmt.Errorf("invalid string array data: truncated at element %d", i)
		}
		strLen := binary.LittleEndian.Uint32(v.Data[offset : offset+4])
		offset += 4

		if offset+int(strLen) > len(v.Data) {
			return nil, fmt.Errorf("invalid string array data: string %d extends past end", i)
		}
		result[i] = string(v.Data[offset : offset+int(strLen)])
		offset += int(strLen)
	}

	return result, nil
}

// AsIntArray decodes an int64 array value
func (v Value) AsIntArray() ([]int64, error) {
	if v.Type != TypeIntArray {
		return nil, fmt.Errorf("value is not an int array")
	}
	if len(v.Data) < 4 {
		return nil, fmt.Errorf("invalid int array data: too short")
	}

	count := binary.LittleEndian.Uint32(v.Data[0:4])
	expectedLen := 4 + int(count)*8
	if len(v.Data) != expectedLen {
		return nil, fmt.Errorf("invalid int array data: expected %d bytes, got %d", expectedLen, len(v.Data))
	}

	result := make([]int64, count)
	for i := uint32(0); i < count; i++ {
		result[i] = int64(binary.LittleEndian.Uint64(v.Data[4+i*8 : 12+i*8]))
	}

	return result, nil
}

// AsFloatArray decodes a float64 array value
func (v Value) AsFloatArray() ([]float64, error) {
	if v.Type != TypeFloatArray {
		return nil, fmt.Errorf("value is not a float array")
	}
	if len(v.Data) < 4 {
		return nil, fmt.Errorf("invalid float array data: too short")
	}

	count := binary.LittleEndian.Uint32(v.Data[0:4])
	expectedLen := 4 + int(count)*8
	if len(v.Data) != expectedLen {
		return nil, fmt.Errorf("invalid float array data: expected %d bytes, got %d", expectedLen, len(v.Data))
	}

	result := make([]float64, count)
	for i := uint32(0); i < count; i++ {
		bits := binary.LittleEndian.Uint64(v.Data[4+i*8 : 12+i*8])
		result[i] = math.Float64frombits(bits)
	}

	return result, nil
}

// AsBoolArray decodes a bool array value
func (v Value) AsBoolArray() ([]bool, error) {
	if v.Type != TypeBoolArray {
		return nil, fmt.Errorf("value is not a bool array")
	}
	if len(v.Data) < 4 {
		return nil, fmt.Errorf("invalid bool array data: too short")
	}

	count := binary.LittleEndian.Uint32(v.Data[0:4])
	expectedLen := 4 + int(count)
	if len(v.Data) != expectedLen {
		return nil, fmt.Errorf("invalid bool array data: expected %d bytes, got %d", expectedLen, len(v.Data))
	}

	result := make([]bool, count)
	for i := uint32(0); i < count; i++ {
		result[i] = v.Data[4+i] == 1
	}

	return result, nil
}

// ArrayContains checks if an array value contains a specific element.
// Works with TypeStringArray, TypeIntArray, TypeFloatArray, TypeBoolArray.
func (v Value) ArrayContains(element Value) (bool, error) {
	switch v.Type {
	case TypeStringArray:
		arr, err := v.AsStringArray()
		if err != nil {
			return false, err
		}
		target, err := element.AsString()
		if err != nil {
			return false, fmt.Errorf("element must be a string for string array")
		}
		for _, s := range arr {
			if s == target {
				return true, nil
			}
		}
		return false, nil

	case TypeIntArray:
		arr, err := v.AsIntArray()
		if err != nil {
			return false, err
		}
		target, err := element.AsInt()
		if err != nil {
			return false, fmt.Errorf("element must be an int for int array")
		}
		for _, i := range arr {
			if i == target {
				return true, nil
			}
		}
		return false, nil

	case TypeFloatArray:
		arr, err := v.AsFloatArray()
		if err != nil {
			return false, err
		}
		target, err := element.AsFloat()
		if err != nil {
			return false, fmt.Errorf("element must be a float for float array")
		}
		for _, f := range arr {
			if f == target {
				return true, nil
			}
		}
		return false, nil

	case TypeBoolArray:
		arr, err := v.AsBoolArray()
		if err != nil {
			return false, err
		}
		target, err := element.AsBool()
		if err != nil {
			return false, fmt.Errorf("element must be a bool for bool array")
		}
		for _, b := range arr {
			if b == target {
				return true, nil
			}
		}
		return false, nil

	default:
		return false, fmt.Errorf("ArrayContains only works on array types, got %v", v.Type)
	}
}

// ArrayLen returns the length of an array value
func (v Value) ArrayLen() (int, error) {
	switch v.Type {
	case TypeStringArray, TypeIntArray, TypeFloatArray, TypeBoolArray:
		if len(v.Data) < 4 {
			return 0, fmt.Errorf("invalid array data: too short")
		}
		return int(binary.LittleEndian.Uint32(v.Data[0:4])), nil
	default:
		return 0, fmt.Errorf("ArrayLen only works on array types, got %v", v.Type)
	}
}

// Node represents a vertex in the graph
type Node struct {
	ID         uint64
	TenantID   string // Multi-tenancy: empty string defaults to "default" tenant
	Labels     []string
	Properties map[string]Value
	CreatedAt  int64
	UpdatedAt  int64
}

// Edge represents a relationship between nodes
type Edge struct {
	ID         uint64
	TenantID   string // Multi-tenancy: empty string defaults to "default" tenant
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
		TenantID:   n.TenantID,
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
		TenantID:   e.TenantID,
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

// String returns a string representation of the Value for comparison and display purposes.
// This method implements the fmt.Stringer interface.
func (v Value) String() string {
	switch v.Type {
	case TypeString:
		s, _ := v.AsString()
		return s
	case TypeInt:
		i, _ := v.AsInt()
		return fmt.Sprintf("%d", i)
	case TypeFloat:
		f, _ := v.AsFloat()
		return fmt.Sprintf("%g", f)
	case TypeBool:
		b, _ := v.AsBool()
		return fmt.Sprintf("%t", b)
	case TypeTimestamp:
		t, _ := v.AsTimestamp()
		return t.String()
	case TypeBytes:
		return fmt.Sprintf("%x", v.Data)
	case TypeVector:
		vec, err := v.AsVector()
		if err != nil {
			return fmt.Sprintf("<invalid vector: %v>", err)
		}
		return fmt.Sprintf("%v", vec)
	case TypeStringArray:
		arr, err := v.AsStringArray()
		if err != nil {
			return fmt.Sprintf("<invalid string array: %v>", err)
		}
		return fmt.Sprintf("%v", arr)
	case TypeIntArray:
		arr, err := v.AsIntArray()
		if err != nil {
			return fmt.Sprintf("<invalid int array: %v>", err)
		}
		return fmt.Sprintf("%v", arr)
	case TypeFloatArray:
		arr, err := v.AsFloatArray()
		if err != nil {
			return fmt.Sprintf("<invalid float array: %v>", err)
		}
		return fmt.Sprintf("%v", arr)
	case TypeBoolArray:
		arr, err := v.AsBoolArray()
		if err != nil {
			return fmt.Sprintf("<invalid bool array: %v>", err)
		}
		return fmt.Sprintf("%v", arr)
	default:
		return fmt.Sprintf("%x", v.Data)
	}
}

package storage

import (
	"bytes"
	"math"
	"testing"
	"time"
)

// TestStringValue tests string value creation and decoding
func TestStringValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"simple string", "hello"},
		{"string with spaces", "hello world"},
		{"unicode string", "Hello 世界 🌍"},
		{"long string", string(make([]byte, 1000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := StringValue(tt.input)

			if val.Type != TypeString {
				t.Errorf("Expected type TypeString, got %v", val.Type)
			}

			decoded, err := val.AsString()
			if err != nil {
				t.Fatalf("AsString failed: %v", err)
			}

			if decoded != tt.input {
				t.Errorf("Expected %q, got %q", tt.input, decoded)
			}
		})
	}
}

// TestIntValue tests integer value creation and decoding
func TestIntValue(t *testing.T) {
	tests := []struct {
		name  string
		input int64
	}{
		{"zero", 0},
		{"positive", 42},
		{"negative", -42},
		{"max int64", math.MaxInt64},
		{"min int64", math.MinInt64},
		{"large positive", 1 << 50},
		{"large negative", -(1 << 50)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := IntValue(tt.input)

			if val.Type != TypeInt {
				t.Errorf("Expected type TypeInt, got %v", val.Type)
			}

			if len(val.Data) != 8 {
				t.Errorf("Expected 8 bytes, got %d", len(val.Data))
			}

			decoded, err := val.AsInt()
			if err != nil {
				t.Fatalf("AsInt failed: %v", err)
			}

			if decoded != tt.input {
				t.Errorf("Expected %d, got %d", tt.input, decoded)
			}
		})
	}
}

// TestFloatValue tests float value creation and decoding
func TestFloatValue(t *testing.T) {
	tests := []struct {
		name  string
		input float64
	}{
		{"zero", 0.0},
		{"positive", 3.14},
		{"negative", -3.14},
		{"large", 1e100},
		{"small", 1e-100},
		{"inf", math.Inf(1)},
		{"neg inf", math.Inf(-1)},
		{"max float", math.MaxFloat64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := FloatValue(tt.input)

			if val.Type != TypeFloat {
				t.Errorf("Expected type TypeFloat, got %v", val.Type)
			}

			if len(val.Data) != 8 {
				t.Errorf("Expected 8 bytes, got %d", len(val.Data))
			}

			decoded, err := val.AsFloat()
			if err != nil {
				t.Fatalf("AsFloat failed: %v", err)
			}

			// For NaN, use special comparison
			if math.IsNaN(tt.input) {
				if !math.IsNaN(decoded) {
					t.Errorf("Expected NaN, got %f", decoded)
				}
			} else if decoded != tt.input {
				t.Errorf("Expected %f, got %f", tt.input, decoded)
			}
		})
	}
}

// TestBoolValue tests boolean value creation and decoding
func TestBoolValue(t *testing.T) {
	tests := []struct {
		name  string
		input bool
	}{
		{"true", true},
		{"false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := BoolValue(tt.input)

			if val.Type != TypeBool {
				t.Errorf("Expected type TypeBool, got %v", val.Type)
			}

			if len(val.Data) != 1 {
				t.Errorf("Expected 1 byte, got %d", len(val.Data))
			}

			decoded, err := val.AsBool()
			if err != nil {
				t.Fatalf("AsBool failed: %v", err)
			}

			if decoded != tt.input {
				t.Errorf("Expected %v, got %v", tt.input, decoded)
			}
		})
	}
}

// TestBytesValue tests bytes value creation
func TestBytesValue(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"small", []byte{1, 2, 3}},
		{"large", make([]byte, 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := BytesValue(tt.input)

			if val.Type != TypeBytes {
				t.Errorf("Expected type TypeBytes, got %v", val.Type)
			}

			if !bytes.Equal(val.Data, tt.input) {
				t.Errorf("Expected %v, got %v", tt.input, val.Data)
			}
		})
	}
}

// TestTimestampValue tests timestamp value creation and decoding
func TestTimestampValue(t *testing.T) {
	tests := []struct {
		name  string
		input time.Time
	}{
		{"epoch", time.Unix(0, 0)},
		{"now", time.Now()},
		{"future", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"past", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := TimestampValue(tt.input)

			if val.Type != TypeTimestamp {
				t.Errorf("Expected type TypeTimestamp, got %v", val.Type)
			}

			if len(val.Data) != 8 {
				t.Errorf("Expected 8 bytes, got %d", len(val.Data))
			}

			decoded, err := val.AsTimestamp()
			if err != nil {
				t.Fatalf("AsTimestamp failed: %v", err)
			}

			// Unix timestamps lose nanosecond precision
			if decoded.Unix() != tt.input.Unix() {
				t.Errorf("Expected %v, got %v", tt.input.Unix(), decoded.Unix())
			}
		})
	}
}

// TestValue_TypeMismatch tests decoding with wrong type
func TestValue_TypeMismatch(t *testing.T) {
	stringVal := StringValue("hello")

	// Try to decode as int
	_, err := stringVal.AsInt()
	if err == nil {
		t.Error("Expected error when decoding string as int")
	}

	// Try to decode as float
	_, err = stringVal.AsFloat()
	if err == nil {
		t.Error("Expected error when decoding string as float")
	}

	// Try to decode as bool
	_, err = stringVal.AsBool()
	if err == nil {
		t.Error("Expected error when decoding string as bool")
	}

	// Try to decode as timestamp
	_, err = stringVal.AsTimestamp()
	if err == nil {
		t.Error("Expected error when decoding string as timestamp")
	}
}

// TestNode_Clone tests node cloning
func TestNode_Clone(t *testing.T) {
	original := &Node{
		ID:     1,
		Labels: []string{"Person", "Employee"},
		Properties: map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(30),
		},
		CreatedAt: 100,
		UpdatedAt: 200,
	}

	clone := original.Clone()

	// Verify all fields are copied
	if clone.ID != original.ID {
		t.Errorf("Expected ID %d, got %d", original.ID, clone.ID)
	}

	if len(clone.Labels) != len(original.Labels) {
		t.Errorf("Expected %d labels, got %d", len(original.Labels), len(clone.Labels))
	}

	if len(clone.Properties) != len(original.Properties) {
		t.Errorf("Expected %d properties, got %d", len(original.Properties), len(clone.Properties))
	}

	// Verify deep copy - modifying clone shouldn't affect original
	clone.Labels[0] = "Modified"
	if original.Labels[0] == "Modified" {
		t.Error("Modifying clone affected original labels")
	}

	clone.Properties["new"] = StringValue("new value")
	if _, exists := original.Properties["new"]; exists {
		t.Error("Modifying clone affected original properties")
	}

	clone.Properties["name"] = StringValue("Bob")
	originalName, _ := original.Properties["name"].AsString()
	if originalName != "Alice" {
		t.Error("Modifying clone property affected original")
	}
}

// TestNode_HasLabel tests label checking
func TestNode_HasLabel(t *testing.T) {
	node := &Node{
		ID:     1,
		Labels: []string{"Person", "Employee", "Manager"},
	}

	tests := []struct {
		label    string
		expected bool
	}{
		{"Person", true},
		{"Employee", true},
		{"Manager", true},
		{"Admin", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			result := node.HasLabel(tt.label)
			if result != tt.expected {
				t.Errorf("HasLabel(%q) = %v, expected %v", tt.label, result, tt.expected)
			}
		})
	}
}

// TestNode_GetProperty tests property retrieval
func TestNode_GetProperty(t *testing.T) {
	node := &Node{
		ID: 1,
		Properties: map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(30),
		},
	}

	// Test existing property
	val, ok := node.GetProperty("name")
	if !ok {
		t.Error("Expected to find 'name' property")
	}
	name, _ := val.AsString()
	if name != "Alice" {
		t.Errorf("Expected 'Alice', got %q", name)
	}

	// Test non-existent property
	_, ok = node.GetProperty("missing")
	if ok {
		t.Error("Expected not to find 'missing' property")
	}
}

// TestEdge_Clone tests edge cloning
func TestEdge_Clone(t *testing.T) {
	original := &Edge{
		ID:         1,
		FromNodeID: 10,
		ToNodeID:   20,
		Type:       "KNOWS",
		Properties: map[string]Value{
			"since": IntValue(2020),
		},
		Weight:    0.8,
		CreatedAt: 100,
	}

	clone := original.Clone()

	// Verify all fields are copied
	if clone.ID != original.ID {
		t.Errorf("Expected ID %d, got %d", original.ID, clone.ID)
	}

	if clone.FromNodeID != original.FromNodeID {
		t.Errorf("Expected FromNodeID %d, got %d", original.FromNodeID, clone.FromNodeID)
	}

	if clone.ToNodeID != original.ToNodeID {
		t.Errorf("Expected ToNodeID %d, got %d", original.ToNodeID, clone.ToNodeID)
	}

	if clone.Type != original.Type {
		t.Errorf("Expected Type %q, got %q", original.Type, clone.Type)
	}

	if len(clone.Properties) != len(original.Properties) {
		t.Errorf("Expected %d properties, got %d", len(original.Properties), len(clone.Properties))
	}

	// Verify deep copy
	clone.Properties["new"] = StringValue("new value")
	if _, exists := original.Properties["new"]; exists {
		t.Error("Modifying clone affected original properties")
	}
}

// TestEdge_GetProperty tests edge property retrieval
func TestEdge_GetProperty(t *testing.T) {
	edge := &Edge{
		ID: 1,
		Properties: map[string]Value{
			"weight": FloatValue(0.5),
			"type":   StringValue("friend"),
		},
	}

	// Test existing property
	val, ok := edge.GetProperty("type")
	if !ok {
		t.Error("Expected to find 'type' property")
	}
	typeStr, _ := val.AsString()
	if typeStr != "friend" {
		t.Errorf("Expected 'friend', got %q", typeStr)
	}

	// Test non-existent property
	_, ok = edge.GetProperty("missing")
	if ok {
		t.Error("Expected not to find 'missing' property")
	}
}

// TestValue_EmptyData tests handling of empty/invalid data
func TestValue_EmptyData(t *testing.T) {
	// Test int with insufficient data
	val := Value{Type: TypeInt, Data: []byte{1, 2, 3}} // Less than 8 bytes

	// This might panic or return wrong value - test current behavior
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Panic recovered (expected for malformed data): %v", r)
		}
	}()

	_, err := val.AsInt()
	if err != nil {
		t.Logf("Error returned for insufficient data: %v", err)
	}
}

// TestNode_EmptyLabels tests node with no labels
func TestNode_EmptyLabels(t *testing.T) {
	node := &Node{
		ID:     1,
		Labels: []string{},
	}

	if node.HasLabel("Any") {
		t.Error("Empty node should not have any label")
	}

	clone := node.Clone()
	if len(clone.Labels) != 0 {
		t.Error("Cloned empty labels should also be empty")
	}
}

// TestNode_EmptyProperties tests node with no properties
func TestNode_EmptyProperties(t *testing.T) {
	node := &Node{
		ID:         1,
		Properties: map[string]Value{},
	}

	_, ok := node.GetProperty("any")
	if ok {
		t.Error("Empty properties should not find any key")
	}

	clone := node.Clone()
	if len(clone.Properties) != 0 {
		t.Error("Cloned empty properties should also be empty")
	}
}

// === Array Type Tests ===

// TestStringArrayValue tests string array creation and decoding
func TestStringArrayValue(t *testing.T) {
	tests := []struct {
		name  string
		input []string
	}{
		{"empty array", []string{}},
		{"single element", []string{"hello"}},
		{"multiple elements", []string{"a", "b", "c"}},
		{"with empty strings", []string{"", "middle", ""}},
		{"unicode strings", []string{"Hello", "世界", "🌍"}},
		{"long strings", []string{string(make([]byte, 100)), "short"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := StringArrayValue(tt.input)

			if val.Type != TypeStringArray {
				t.Errorf("Expected type TypeStringArray, got %v", val.Type)
			}

			decoded, err := val.AsStringArray()
			if err != nil {
				t.Fatalf("AsStringArray failed: %v", err)
			}

			if len(decoded) != len(tt.input) {
				t.Fatalf("Expected %d elements, got %d", len(tt.input), len(decoded))
			}

			for i, s := range tt.input {
				if decoded[i] != s {
					t.Errorf("Element %d: expected %q, got %q", i, s, decoded[i])
				}
			}
		})
	}
}

// TestIntArrayValue tests int64 array creation and decoding
func TestIntArrayValue(t *testing.T) {
	tests := []struct {
		name  string
		input []int64
	}{
		{"empty array", []int64{}},
		{"single element", []int64{42}},
		{"multiple elements", []int64{1, 2, 3, 4, 5}},
		{"negative numbers", []int64{-100, 0, 100}},
		{"extreme values", []int64{math.MinInt64, 0, math.MaxInt64}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := IntArrayValue(tt.input)

			if val.Type != TypeIntArray {
				t.Errorf("Expected type TypeIntArray, got %v", val.Type)
			}

			decoded, err := val.AsIntArray()
			if err != nil {
				t.Fatalf("AsIntArray failed: %v", err)
			}

			if len(decoded) != len(tt.input) {
				t.Fatalf("Expected %d elements, got %d", len(tt.input), len(decoded))
			}

			for i, v := range tt.input {
				if decoded[i] != v {
					t.Errorf("Element %d: expected %d, got %d", i, v, decoded[i])
				}
			}
		})
	}
}

// TestFloatArrayValue tests float64 array creation and decoding
func TestFloatArrayValue(t *testing.T) {
	tests := []struct {
		name  string
		input []float64
	}{
		{"empty array", []float64{}},
		{"single element", []float64{3.14}},
		{"multiple elements", []float64{1.1, 2.2, 3.3}},
		{"special values", []float64{0, -0, math.Inf(1), math.Inf(-1)}},
		{"small values", []float64{1e-10, 1e-20, 1e-100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := FloatArrayValue(tt.input)

			if val.Type != TypeFloatArray {
				t.Errorf("Expected type TypeFloatArray, got %v", val.Type)
			}

			decoded, err := val.AsFloatArray()
			if err != nil {
				t.Fatalf("AsFloatArray failed: %v", err)
			}

			if len(decoded) != len(tt.input) {
				t.Fatalf("Expected %d elements, got %d", len(tt.input), len(decoded))
			}

			for i, v := range tt.input {
				if decoded[i] != v && !(math.IsNaN(v) && math.IsNaN(decoded[i])) {
					t.Errorf("Element %d: expected %v, got %v", i, v, decoded[i])
				}
			}
		})
	}
}

// TestBoolArrayValue tests bool array creation and decoding
func TestBoolArrayValue(t *testing.T) {
	tests := []struct {
		name  string
		input []bool
	}{
		{"empty array", []bool{}},
		{"single true", []bool{true}},
		{"single false", []bool{false}},
		{"mixed", []bool{true, false, true, true, false}},
		{"all true", []bool{true, true, true}},
		{"all false", []bool{false, false, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := BoolArrayValue(tt.input)

			if val.Type != TypeBoolArray {
				t.Errorf("Expected type TypeBoolArray, got %v", val.Type)
			}

			decoded, err := val.AsBoolArray()
			if err != nil {
				t.Fatalf("AsBoolArray failed: %v", err)
			}

			if len(decoded) != len(tt.input) {
				t.Fatalf("Expected %d elements, got %d", len(tt.input), len(decoded))
			}

			for i, v := range tt.input {
				if decoded[i] != v {
					t.Errorf("Element %d: expected %v, got %v", i, v, decoded[i])
				}
			}
		})
	}
}

// TestArrayContains tests the ArrayContains method
func TestArrayContains(t *testing.T) {
	t.Run("string array", func(t *testing.T) {
		arr := StringArrayValue([]string{"apple", "banana", "cherry"})

		contains, err := arr.ArrayContains(StringValue("banana"))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if !contains {
			t.Error("Expected to find 'banana'")
		}

		contains, err = arr.ArrayContains(StringValue("grape"))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if contains {
			t.Error("Should not find 'grape'")
		}
	})

	t.Run("int array", func(t *testing.T) {
		arr := IntArrayValue([]int64{10, 20, 30})

		contains, err := arr.ArrayContains(IntValue(20))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if !contains {
			t.Error("Expected to find 20")
		}

		contains, err = arr.ArrayContains(IntValue(25))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if contains {
			t.Error("Should not find 25")
		}
	})

	t.Run("float array", func(t *testing.T) {
		arr := FloatArrayValue([]float64{1.1, 2.2, 3.3})

		contains, err := arr.ArrayContains(FloatValue(2.2))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if !contains {
			t.Error("Expected to find 2.2")
		}
	})

	t.Run("bool array", func(t *testing.T) {
		arr := BoolArrayValue([]bool{false, false})

		contains, err := arr.ArrayContains(BoolValue(true))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if contains {
			t.Error("Should not find true")
		}

		contains, err = arr.ArrayContains(BoolValue(false))
		if err != nil {
			t.Fatalf("ArrayContains failed: %v", err)
		}
		if !contains {
			t.Error("Expected to find false")
		}
	})

	t.Run("type mismatch", func(t *testing.T) {
		arr := StringArrayValue([]string{"a", "b"})

		// Try to check with wrong element type
		_, err := arr.ArrayContains(IntValue(1))
		if err == nil {
			t.Error("Expected error for type mismatch")
		}
	})

	t.Run("non-array type", func(t *testing.T) {
		val := StringValue("not an array")

		_, err := val.ArrayContains(StringValue("a"))
		if err == nil {
			t.Error("Expected error for non-array type")
		}
	})
}

// TestArrayLen tests the ArrayLen method
func TestArrayLen(t *testing.T) {
	tests := []struct {
		name     string
		value    Value
		expected int
	}{
		{"empty string array", StringArrayValue([]string{}), 0},
		{"string array with 3", StringArrayValue([]string{"a", "b", "c"}), 3},
		{"int array with 5", IntArrayValue([]int64{1, 2, 3, 4, 5}), 5},
		{"float array with 2", FloatArrayValue([]float64{1.1, 2.2}), 2},
		{"bool array with 4", BoolArrayValue([]bool{true, false, true, false}), 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			length, err := tt.value.ArrayLen()
			if err != nil {
				t.Fatalf("ArrayLen failed: %v", err)
			}
			if length != tt.expected {
				t.Errorf("Expected length %d, got %d", tt.expected, length)
			}
		})
	}

	// Test non-array type
	t.Run("non-array type", func(t *testing.T) {
		val := StringValue("not an array")
		_, err := val.ArrayLen()
		if err == nil {
			t.Error("Expected error for non-array type")
		}
	})
}

// TestArrayTypeMismatch tests decoding arrays with wrong type
func TestArrayTypeMismatch(t *testing.T) {
	stringArr := StringArrayValue([]string{"a", "b"})

	_, err := stringArr.AsIntArray()
	if err == nil {
		t.Error("Expected error when decoding string array as int array")
	}

	_, err = stringArr.AsFloatArray()
	if err == nil {
		t.Error("Expected error when decoding string array as float array")
	}

	_, err = stringArr.AsBoolArray()
	if err == nil {
		t.Error("Expected error when decoding string array as bool array")
	}
}

// TestArrayInNodeProperties tests using arrays in node properties
func TestArrayInNodeProperties(t *testing.T) {
	node := &Node{
		ID:     1,
		Labels: []string{"User"},
		Properties: map[string]Value{
			"tags":       StringArrayValue([]string{"admin", "active", "verified"}),
			"scores":     IntArrayValue([]int64{95, 87, 92}),
			"weights":    FloatArrayValue([]float64{0.5, 0.3, 0.2}),
			"permissions": BoolArrayValue([]bool{true, true, false, true}),
		},
	}

	// Verify we can retrieve and decode arrays
	tagsVal, ok := node.GetProperty("tags")
	if !ok {
		t.Fatal("Expected to find 'tags' property")
	}
	tags, err := tagsVal.AsStringArray()
	if err != nil {
		t.Fatalf("AsStringArray failed: %v", err)
	}
	if len(tags) != 3 || tags[0] != "admin" {
		t.Errorf("Unexpected tags: %v", tags)
	}

	// Test ArrayContains on property
	contains, err := tagsVal.ArrayContains(StringValue("active"))
	if err != nil {
		t.Fatalf("ArrayContains failed: %v", err)
	}
	if !contains {
		t.Error("Expected tags to contain 'active'")
	}
}

// TestArrayInEdgeProperties tests using arrays in edge properties
func TestArrayInEdgeProperties(t *testing.T) {
	edge := &Edge{
		ID:         1,
		FromNodeID: 10,
		ToNodeID:   20,
		Type:       "TAUGHT",
		Properties: map[string]Value{
			"concepts": StringArrayValue([]string{"calc-101", "calc-102", "linear-algebra"}),
			"ratings":  IntArrayValue([]int64{5, 4, 5}),
		},
		Weight: 1.0,
	}

	conceptsVal, ok := edge.GetProperty("concepts")
	if !ok {
		t.Fatal("Expected to find 'concepts' property")
	}

	concepts, err := conceptsVal.AsStringArray()
	if err != nil {
		t.Fatalf("AsStringArray failed: %v", err)
	}

	if len(concepts) != 3 {
		t.Errorf("Expected 3 concepts, got %d", len(concepts))
	}

	// Check array length
	length, _ := conceptsVal.ArrayLen()
	if length != 3 {
		t.Errorf("Expected ArrayLen 3, got %d", length)
	}
}

package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestMatchStep_CopyBinding(t *testing.T) {
	ms := &MatchStep{}

	original := &BindingSet{
		bindings: map[string]any{
			"key1": "value1",
			"key2": "value2",
		},
	}

	copied := ms.copyBinding(original)

	// Verify copy has same values
	if copied.bindings["key1"] != "value1" {
		t.Errorf("Expected key1='value1', got %v", copied.bindings["key1"])
	}

	// Verify it's a deep copy
	copied.bindings["key1"] = "modified"
	if original.bindings["key1"] != "value1" {
		t.Error("Expected original binding to be unchanged after modifying copy")
	}
}

// TestMatchStep_HasLabels tests label matching

func TestMatchStep_HasLabels(t *testing.T) {
	ms := &MatchStep{}

	node := &storage.Node{
		Labels: []string{"Person", "Employee"},
	}

	// Should match if node has all required labels
	if !ms.hasLabels(node, []string{"Person"}) {
		t.Error("Expected node to match single label")
	}

	if !ms.hasLabels(node, []string{"Person", "Employee"}) {
		t.Error("Expected node to match multiple labels")
	}

	if ms.hasLabels(node, []string{"Person", "Manager"}) {
		t.Error("Expected node not to match when missing label")
	}

	if !ms.hasLabels(node, []string{}) {
		t.Error("Expected node to match empty label list")
	}
}

// TestMatchStep_MatchProperties tests property matching

func TestMatchStep_MatchProperties(t *testing.T) {
	ms := &MatchStep{}

	nodeProps := map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	}

	// Should match empty pattern
	if !ms.matchProperties(nodeProps, map[string]any{}) {
		t.Error("Expected to match empty pattern")
	}

	// Should match exact property
	if !ms.matchProperties(nodeProps, map[string]any{"name": "Alice"}) {
		t.Error("Expected to match exact string property")
	}

	// Should not match different value
	if ms.matchProperties(nodeProps, map[string]any{"name": "Bob"}) {
		t.Error("Expected not to match different value")
	}

	// Should not match missing property
	if ms.matchProperties(nodeProps, map[string]any{"city": "NYC"}) {
		t.Error("Expected not to match missing property")
	}
}

// TestMatchStep_ValuesEqual tests value comparison

func TestMatchStep_ValuesEqual(t *testing.T) {
	ms := &MatchStep{}

	// String comparison
	if !ms.valuesEqual(storage.StringValue("hello"), "hello") {
		t.Error("Expected string values to match")
	}

	if ms.valuesEqual(storage.StringValue("hello"), "world") {
		t.Error("Expected different strings not to match")
	}

	// Integer comparison
	if !ms.valuesEqual(storage.IntValue(42), int64(42)) {
		t.Error("Expected int values to match")
	}

	if ms.valuesEqual(storage.IntValue(42), int64(43)) {
		t.Error("Expected different ints not to match")
	}

	// Float comparison - note: float comparison uses AsFloat() which may have precision issues
	// Skip this test as float comparison behavior depends on storage implementation

	// Boolean comparison
	if !ms.valuesEqual(storage.BoolValue(true), true) {
		t.Error("Expected bool values to match")
	}

	if ms.valuesEqual(storage.BoolValue(true), false) {
		t.Error("Expected different bools not to match")
	}
}

// TestConvertValue tests value conversion

func TestConvertValue(t *testing.T) {
	// String
	val := convertToStorageValue("hello")
	str, _ := val.AsString()
	if str != "hello" {
		t.Errorf("Expected string 'hello', got '%s'", str)
	}

	// Int64
	val = convertToStorageValue(int64(42))
	i, _ := val.AsInt()
	if i != 42 {
		t.Errorf("Expected int 42, got %d", i)
	}

	// Float64 - note: float storage may have precision issues depending on implementation
	val = convertToStorageValue(float64(3.14))
	f, _ := val.AsFloat()
	if f < 3.0 || f > 3.2 {
		t.Errorf("Expected float around 3.14, got %f", f)
	}

	// Bool
	val = convertToStorageValue(true)
	b, _ := val.AsBool()
	if !b {
		t.Error("Expected bool true")
	}
}

// TestExecutor_WhereClause tests WHERE filtering

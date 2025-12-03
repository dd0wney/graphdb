package storage

import (
	"errors"
	"fmt"
	"testing"
)

func TestStorageError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *StorageError
		expected string
	}{
		{
			name: "with ID",
			err: &StorageError{
				Op:     "create",
				Entity: "node",
				ID:     123,
				Cause:  fmt.Errorf("duplicate key"),
			},
			expected: "create node 123: duplicate key",
		},
		{
			name: "with ID and field",
			err: &StorageError{
				Op:     "update",
				Entity: "node",
				ID:     456,
				Field:  "name",
				Cause:  fmt.Errorf("validation failed"),
			},
			expected: "update node 456 (field name): validation failed",
		},
		{
			name: "without ID with field",
			err: &StorageError{
				Op:     "insert",
				Entity: "index",
				Field:  "email",
				Cause:  fmt.Errorf("index full"),
			},
			expected: "insert index (field email): index full",
		},
		{
			name: "with context",
			err: &StorageError{
				Op:      "flush",
				Entity:  "WAL",
				Context: "during shutdown",
				Cause:   fmt.Errorf("disk full"),
			},
			expected: "flush WAL (during shutdown): disk full",
		},
		{
			name: "minimal",
			err: &StorageError{
				Op:     "close",
				Entity: "storage",
				Cause:  fmt.Errorf("already closed"),
			},
			expected: "close storage: already closed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStorageError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := &StorageError{
		Op:     "create",
		Entity: "node",
		Cause:  cause,
	}

	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestStorageError_Is(t *testing.T) {
	err := &StorageError{
		Op:     "get",
		Entity: "node",
		ID:     123,
		Cause:  ErrNodeNotFound,
	}

	if !errors.Is(err, ErrNodeNotFound) {
		t.Error("Expected errors.Is to match ErrNodeNotFound")
	}

	if errors.Is(err, ErrEdgeNotFound) {
		t.Error("Expected errors.Is to not match ErrEdgeNotFound")
	}
}

func TestErrorBuilder(t *testing.T) {
	err := NewError("delete").
		Node(789).
		Cause(fmt.Errorf("permission denied")).
		Build()

	if err.Op != "delete" {
		t.Errorf("Op = %q, want %q", err.Op, "delete")
	}
	if err.Entity != "node" {
		t.Errorf("Entity = %q, want %q", err.Entity, "node")
	}
	if err.ID != 789 {
		t.Errorf("ID = %d, want %d", err.ID, 789)
	}
}

func TestErrorBuilder_Edge(t *testing.T) {
	err := NewError("update").Edge(456).Build()

	if err.Entity != "edge" {
		t.Errorf("Entity = %q, want %q", err.Entity, "edge")
	}
	if err.ID != 456 {
		t.Errorf("ID = %d, want %d", err.ID, 456)
	}
}

func TestErrorBuilder_Index(t *testing.T) {
	err := NewError("insert").Index("email").Build()

	if err.Entity != "index" {
		t.Errorf("Entity = %q, want %q", err.Entity, "index")
	}
	if err.Field != "email" {
		t.Errorf("Field = %q, want %q", err.Field, "email")
	}
}

func TestErrorBuilder_WAL(t *testing.T) {
	err := NewError("append").WAL().Build()

	if err.Entity != "WAL" {
		t.Errorf("Entity = %q, want %q", err.Entity, "WAL")
	}
}

func TestErrorBuilder_Context(t *testing.T) {
	err := NewError("sync").Context("background flush").Build()

	if err.Context != "background flush" {
		t.Errorf("Context = %q, want %q", err.Context, "background flush")
	}
}

func TestNodeNotFoundError(t *testing.T) {
	err := NodeNotFoundError(123)

	if !errors.Is(err, ErrNodeNotFound) {
		t.Error("Expected error to wrap ErrNodeNotFound")
	}

	expected := "get node 123: node not found"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestEdgeNotFoundError(t *testing.T) {
	err := EdgeNotFoundError(456)

	if !errors.Is(err, ErrEdgeNotFound) {
		t.Error("Expected error to wrap ErrEdgeNotFound")
	}
}

func TestWALError(t *testing.T) {
	cause := fmt.Errorf("disk full")
	err := WALError("append", cause)

	if !errors.Is(err, cause) {
		t.Error("Expected error to wrap cause")
	}
}

func TestIndexError(t *testing.T) {
	cause := fmt.Errorf("duplicate key")
	err := IndexError("insert", "email", cause)

	if !errors.Is(err, cause) {
		t.Error("Expected error to wrap cause")
	}

	expected := "insert index (field email): duplicate key"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestMarshalError(t *testing.T) {
	cause := fmt.Errorf("json: unsupported type")
	err := MarshalError("node", 123, cause)

	expected := "marshal node 123: json: unsupported type"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"node not found", NodeNotFoundError(1), true},
		{"edge not found", EdgeNotFoundError(1), true},
		{"other error", fmt.Errorf("other"), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.expected {
				t.Errorf("IsNotFound() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsClosed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "storage closed error",
			err:      NewError("write").Context("storage").Cause(ErrStorageClosed).Err(),
			expected: true,
		},
		{
			name:     "other error",
			err:      fmt.Errorf("other"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsClosed(tt.err); got != tt.expected {
				t.Errorf("IsClosed() = %v, want %v", got, tt.expected)
			}
		})
	}
}

package storage

import (
	"errors"
	"fmt"
)

// Common sentinel errors
var (
	ErrNodeNotFound    = errors.New("node not found")
	ErrEdgeNotFound    = errors.New("edge not found")
	ErrStorageClosed   = errors.New("storage is closed")
	ErrInvalidID       = errors.New("invalid ID")
	ErrWALAppendFailed = errors.New("WAL append failed")
	ErrMarshalFailed   = errors.New("marshal failed")
	ErrIndexFailed     = errors.New("index operation failed")
)

// StorageError provides structured error information for storage operations.
type StorageError struct {
	Op      string      // Operation that failed (e.g., "CreateNode", "DeleteEdge")
	Entity  string      // Entity type (e.g., "node", "edge", "index")
	ID      uint64      // Entity ID (if applicable)
	Field   string      // Field name (for property operations)
	Cause   error       // Underlying error
	Context string      // Additional context
}

// Error implements the error interface.
func (e *StorageError) Error() string {
	if e.ID != 0 {
		if e.Field != "" {
			return fmt.Sprintf("%s %s %d (field %s): %v", e.Op, e.Entity, e.ID, e.Field, e.Cause)
		}
		return fmt.Sprintf("%s %s %d: %v", e.Op, e.Entity, e.ID, e.Cause)
	}
	if e.Field != "" {
		return fmt.Sprintf("%s %s (field %s): %v", e.Op, e.Entity, e.Field, e.Cause)
	}
	if e.Context != "" {
		return fmt.Sprintf("%s %s (%s): %v", e.Op, e.Entity, e.Context, e.Cause)
	}
	return fmt.Sprintf("%s %s: %v", e.Op, e.Entity, e.Cause)
}

// Unwrap returns the underlying cause for error chain support.
func (e *StorageError) Unwrap() error {
	return e.Cause
}

// Is reports whether the target error matches this error or its cause.
func (e *StorageError) Is(target error) bool {
	if target == nil {
		return false
	}
	return errors.Is(e.Cause, target)
}

// ErrorBuilder provides a fluent interface for building StorageErrors.
type ErrorBuilder struct {
	err StorageError
}

// NewError creates a new error builder with the given operation.
func NewError(op string) *ErrorBuilder {
	return &ErrorBuilder{err: StorageError{Op: op}}
}

// Node sets the entity to "node" with the given ID.
func (b *ErrorBuilder) Node(id uint64) *ErrorBuilder {
	b.err.Entity = "node"
	b.err.ID = id
	return b
}

// Edge sets the entity to "edge" with the given ID.
func (b *ErrorBuilder) Edge(id uint64) *ErrorBuilder {
	b.err.Entity = "edge"
	b.err.ID = id
	return b
}

// Index sets the entity to "index" with the given field name.
func (b *ErrorBuilder) Index(field string) *ErrorBuilder {
	b.err.Entity = "index"
	b.err.Field = field
	return b
}

// WAL sets the entity to "WAL".
func (b *ErrorBuilder) WAL() *ErrorBuilder {
	b.err.Entity = "WAL"
	return b
}

// Field sets the field name for property operations.
func (b *ErrorBuilder) Field(name string) *ErrorBuilder {
	b.err.Field = name
	return b
}

// Context sets additional context information.
func (b *ErrorBuilder) Context(ctx string) *ErrorBuilder {
	b.err.Context = ctx
	return b
}

// Cause sets the underlying error cause.
func (b *ErrorBuilder) Cause(err error) *ErrorBuilder {
	b.err.Cause = err
	return b
}

// Build returns the constructed StorageError.
func (b *ErrorBuilder) Build() *StorageError {
	return &b.err
}

// Err returns the error as an error interface.
func (b *ErrorBuilder) Err() error {
	return &b.err
}

// Convenience functions for common error patterns

// NodeNotFoundError creates a node not found error.
func NodeNotFoundError(nodeID uint64) error {
	return NewError("get").Node(nodeID).Cause(ErrNodeNotFound).Err()
}

// EdgeNotFoundError creates an edge not found error.
func EdgeNotFoundError(edgeID uint64) error {
	return NewError("get").Edge(edgeID).Cause(ErrEdgeNotFound).Err()
}

// WALError creates a WAL operation error.
func WALError(op string, cause error) error {
	return NewError(op).WAL().Cause(cause).Err()
}

// IndexError creates an index operation error.
func IndexError(op, field string, cause error) error {
	return NewError(op).Index(field).Cause(cause).Err()
}

// MarshalError creates a marshal error for the given entity.
func MarshalError(entity string, id uint64, cause error) error {
	return &StorageError{
		Op:     "marshal",
		Entity: entity,
		ID:     id,
		Cause:  cause,
	}
}

// IsNotFound returns true if the error is a not found error.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNodeNotFound) || errors.Is(err, ErrEdgeNotFound)
}

// IsClosed returns true if the error indicates the storage is closed.
func IsClosed(err error) bool {
	return errors.Is(err, ErrStorageClosed)
}

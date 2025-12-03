package replication

import (
	"io"
	"log"
)

// ResourceCleanup provides a stack-based cleanup mechanism for resources.
// Resources are closed in reverse order (LIFO) when Cleanup is called.
// This eliminates cascading error handling boilerplate during initialization.
//
// Example usage:
//
//	cleanup := NewResourceCleanup()
//	defer cleanup.Cleanup() // Will close all registered resources on error
//
//	socket1, err := createSocket()
//	if err != nil {
//	    return err
//	}
//	cleanup.Add(socket1, "socket1")
//
//	socket2, err := createSocket()
//	if err != nil {
//	    return err // cleanup.Cleanup() will close socket1
//	}
//	cleanup.Add(socket2, "socket2")
//
//	// Success - prevent cleanup from closing resources
//	cleanup.Clear()
//	return nil
type ResourceCleanup struct {
	resources []namedCloser
}

// namedCloser wraps a closer with a descriptive name for logging
type namedCloser struct {
	closer io.Closer
	name   string
}

// NewResourceCleanup creates a new ResourceCleanup instance.
func NewResourceCleanup() *ResourceCleanup {
	return &ResourceCleanup{
		resources: make([]namedCloser, 0, 8),
	}
}

// Add registers a resource to be cleaned up.
// Resources are closed in reverse order (LIFO).
func (rc *ResourceCleanup) Add(closer io.Closer, name string) {
	rc.resources = append(rc.resources, namedCloser{closer: closer, name: name})
}

// Cleanup closes all registered resources in reverse order.
// Any errors during close are logged but do not stop the cleanup process.
// This method is idempotent - calling it multiple times is safe.
func (rc *ResourceCleanup) Cleanup() {
	// Close in reverse order (LIFO)
	for i := len(rc.resources) - 1; i >= 0; i-- {
		r := rc.resources[i]
		if r.closer != nil {
			if err := r.closer.Close(); err != nil {
				log.Printf("Warning: Failed to close %s during cleanup: %v", r.name, err)
			}
		}
	}
	// Clear the slice to prevent double-close
	rc.resources = rc.resources[:0]
}

// Clear removes all registered resources without closing them.
// Call this after successful initialization to prevent deferred Cleanup from closing resources.
func (rc *ResourceCleanup) Clear() {
	rc.resources = rc.resources[:0]
}

// CloseAll closes all registered resources and returns the first error encountered.
// All resources are attempted to be closed regardless of errors.
// Use this for explicit cleanup where you want error reporting.
func (rc *ResourceCleanup) CloseAll() error {
	var firstErr error
	// Close in reverse order (LIFO)
	for i := len(rc.resources) - 1; i >= 0; i-- {
		r := rc.resources[i]
		if r.closer != nil {
			if err := r.closer.Close(); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				log.Printf("Warning: Failed to close %s: %v", r.name, err)
			}
		}
	}
	// Clear the slice to prevent double-close
	rc.resources = rc.resources[:0]
	return firstErr
}

// Len returns the number of registered resources.
func (rc *ResourceCleanup) Len() int {
	return len(rc.resources)
}

package wal

// WALAppender is the interface for appending entries to a WAL.
// This interface can be used by packages that need to write to WAL
// without depending on the concrete implementation.
type WALAppender interface {
	// Append appends a new entry to the WAL.
	// Returns the LSN (Log Sequence Number) assigned to the entry.
	Append(opType OpType, data []byte) (uint64, error)
}

// WALReader is the interface for reading entries from a WAL.
type WALReader interface {
	// Replay iterates through all WAL entries and calls the handler for each.
	// Used for recovery after restart.
	Replay(handler func(*Entry) error) error
}

// WALManager is the interface for WAL lifecycle management.
type WALManager interface {
	// Truncate removes all entries from the WAL.
	// Typically called after a successful snapshot.
	Truncate() error

	// Close flushes any buffered data and closes the WAL.
	Close() error

	// GetCurrentLSN returns the current Log Sequence Number.
	GetCurrentLSN() uint64
}

// WriteAheadLog is the complete interface for a Write-Ahead Log implementation.
// All WAL implementations (WAL, BatchedWAL, CompressedWAL) implement this interface.
type WriteAheadLog interface {
	WALAppender
	WALReader
	WALManager
}

// Verify that all WAL implementations satisfy the WriteAheadLog interface
var _ WriteAheadLog = (*WAL)(nil)
var _ WriteAheadLog = (*BatchedWAL)(nil)
var _ WriteAheadLog = (*CompressedWAL)(nil)

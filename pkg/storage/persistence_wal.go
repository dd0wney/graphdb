package storage

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// writeToWAL writes an operation to the write-ahead log for durability
// Handles both batched and non-batched WAL writes
// Note: This function logs errors rather than returning them to maintain
// backward compatibility with existing callers. Critical operations that
// require durability guarantees should use writeToWALWithError instead.
func (gs *GraphStorage) writeToWAL(operation wal.OpType, data any) {
	if err := gs.writeToWALWithError(operation, data); err != nil {
		// Log the error - callers that need error handling should use writeToWALWithError
		fmt.Fprintf(os.Stderr, "WAL write error (op=%d): %v\n", operation, err)
	}
}

// writeToWALWithError writes an operation to the WAL and returns any error
// Use this for operations that require durability guarantees
func (gs *GraphStorage) writeToWALWithError(operation wal.OpType, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL data: %w", err)
	}

	if gs.useBatching && gs.batchedWAL != nil {
		if _, err := gs.batchedWAL.Append(operation, encoded); err != nil {
			return fmt.Errorf("failed to append to batched WAL: %w", err)
		}
	} else if gs.wal != nil {
		if _, err := gs.wal.Append(operation, encoded); err != nil {
			return fmt.Errorf("failed to append to WAL: %w", err)
		}
	}
	// No WAL configured - this is valid for in-memory only mode
	return nil
}

// enqueueWAL records an operation for durability and returns a handle the
// caller must Wait() on before treating the write as durable.
//
// For the batched WAL it enqueues WITHOUT blocking and returns a non-nil
// *wal.Pending — the caller is expected to release gs.mu and THEN Wait(), so
// concurrent writers can fill the same batch (group commit, Track P item 1).
// The enqueue happens under the caller's gs.mu, so WAL order matches in-memory
// mutation order; only the durability wait moves outside the lock.
//
// For the synchronous path (plain WAL, or no WAL) the durable write happens
// inline here exactly as writeToWAL does today, and the returned handle is nil
// (nothing to wait for). This keeps the non-batched default byte-identical.
//
// Matches writeToWAL's fail-soft contract: marshal / synchronous-append errors
// are logged, not returned. The caller likewise logs (does not propagate) the
// deferred Wait() error.
func (gs *GraphStorage) enqueueWAL(operation wal.OpType, data any) *wal.Pending {
	encoded, err := json.Marshal(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WAL write error (op=%d): %v\n", operation, err)
		return nil
	}

	if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.Enqueue(operation, encoded)
	} else if gs.wal != nil {
		if _, err := gs.wal.Append(operation, encoded); err != nil {
			fmt.Fprintf(os.Stderr, "WAL write error (op=%d): %v\n", operation, err)
		}
	}
	// No WAL configured - valid for in-memory only mode.
	return nil
}

// waitWALPending blocks on a pending batched-WAL durability handle and logs
// (does not propagate) any flush error, preserving writeToWAL's fail-soft
// contract. nil handle (synchronous/no-op path) returns immediately.
func (gs *GraphStorage) waitWALPending(operation wal.OpType, pending *wal.Pending) {
	if pending == nil {
		return
	}
	if err := pending.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "WAL write error (op=%d): %v\n", operation, err)
	}
}

// GetCurrentLSN returns the current LSN (Log Sequence Number) from the WAL
// This is used by replication to track the latest position in the write-ahead log
func (gs *GraphStorage) GetCurrentLSN() uint64 {
	if gs.useCompression && gs.compressedWAL != nil {
		return gs.compressedWAL.GetCurrentLSN()
	} else if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.GetCurrentLSN()
	} else if gs.wal != nil {
		return gs.wal.GetCurrentLSN()
	}
	return 0
}

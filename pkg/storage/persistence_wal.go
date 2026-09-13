package storage

import (
	"encoding/json"
	"fmt"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// writeToWALWithError writes an operation to the WAL and returns any error
// Use this for operations that require durability guarantees
func (gs *GraphStorage) writeToWALWithError(operation wal.OpType, data any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return gs.noteWALWriteError(fmt.Errorf("failed to marshal WAL data: %w", err))
	}
	if encoded, err = gs.sealWALPayload(encoded); err != nil {
		return gs.noteWALWriteError(err)
	}

	if gs.useBatching && gs.batchedWAL != nil {
		if _, err := gs.batchedWAL.Append(operation, encoded); err != nil {
			return gs.noteWALWriteError(fmt.Errorf("failed to append to batched WAL: %w", err))
		}
	} else if gs.useCompression && gs.compressedWAL != nil {
		// This branch was MISSING until M-1's compact tests surfaced it:
		// with EnableCompression every single-op write silently skipped
		// the WAL (and replayWAL never read it back) — zero crash
		// durability on the compressed backend.
		if _, err := gs.compressedWAL.Append(operation, encoded); err != nil {
			return gs.noteWALWriteError(fmt.Errorf("failed to append to compressed WAL: %w", err))
		}
	} else if gs.wal != nil {
		if _, err := gs.wal.Append(operation, encoded); err != nil {
			return gs.noteWALWriteError(fmt.Errorf("failed to append to WAL: %w", err))
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
// inline here exactly as writeToWALWithError does, and the returned handle is
// nil on success (nothing to wait for).
//
// A marshal, seal, or synchronous-append failure returns a *wal.Pending whose
// Wait() returns the error at once (wal.FailedPending) — the caller always
// gets a handle it can Wait() on, with no special case for "the write never
// reached the batch buffer." The in-memory change the caller already applied
// stays applied: this is the fail-loud-but-keep-going contract described on
// ErrWALWriteFailed, not a rollback.
func (gs *GraphStorage) enqueueWAL(operation wal.OpType, data any) *wal.Pending {
	encoded, err := json.Marshal(data)
	if err != nil {
		return wal.FailedPending(gs.noteWALWriteError(fmt.Errorf("failed to marshal WAL data: %w", err)))
	}
	if encoded, err = gs.sealWALPayload(encoded); err != nil {
		return wal.FailedPending(gs.noteWALWriteError(err))
	}

	if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.Enqueue(operation, encoded)
	} else if gs.useCompression && gs.compressedWAL != nil {
		// Synchronous like the plain path. This branch was MISSING (see
		// writeToWALWithError) — single-op writes never reached the
		// compressed WAL.
		if _, err := gs.compressedWAL.Append(operation, encoded); err != nil {
			return wal.FailedPending(gs.noteWALWriteError(fmt.Errorf("failed to append to compressed WAL: %w", err)))
		}
	} else if gs.wal != nil {
		if _, err := gs.wal.Append(operation, encoded); err != nil {
			return wal.FailedPending(gs.noteWALWriteError(fmt.Errorf("failed to append to WAL: %w", err)))
		}
	}
	// No WAL configured - valid for in-memory only mode.
	return nil
}

// waitWALPending blocks on a pending WAL durability handle and returns any
// failure to become durable. A nil handle (the synchronous path already
// succeeded, or no WAL is configured) returns nil at once.
//
// A non-nil error wraps ErrWALWriteFailed and the operation type, and has
// already been recorded via gs.noteWALWriteError so the next Close rewrites
// the snapshot. The caller's in-memory change stays applied and its result
// value stays valid — see ErrWALWriteFailed's doc comment for the contract.
func (gs *GraphStorage) waitWALPending(operation wal.OpType, pending *wal.Pending) error {
	if pending == nil {
		return nil
	}
	if err := pending.Wait(); err != nil {
		gs.noteWALWriteError(err)
		return fmt.Errorf("%w: op %d: %w", ErrWALWriteFailed, operation, err)
	}
	return nil
}

// appendWALBatch durably writes a batch of WAL entries with a single fsync —
// all-or-none at the fsync boundary — and returns once durable. This is the
// atomic-commit primitive for Transaction.Commit: a crash before the fsync
// leaves none of the batch in the WAL, after leaves all of it (replay then
// restores the whole transaction via the existing per-op opcodes).
//
// Like the single-op enqueueWAL/waitWALPending path, this PROPAGATES the
// error: a transaction whose commit did not become durable must fail loudly
// so the caller knows.
//
// Atomicity holds on the batched and plain WAL (both back onto WAL.Append-
// BatchAtomic's single fsync). The compressed WAL has no batch primitive, so it
// falls back to sequential Append — durable but NOT atomic across the batch;
// compression is opt-in and uncommon, and this is documented rather than
// silently atomic.
func (gs *GraphStorage) appendWALBatch(entries []wal.BatchEntry) error {
	if len(entries) == 0 {
		return nil
	}
	// Seal each payload (H-3). The slice is rebuilt rather than mutated:
	// callers may retain their entries.
	if gs.encryptionEngine != nil {
		sealed := make([]wal.BatchEntry, len(entries))
		for i, e := range entries {
			data, err := gs.sealWALPayload(e.Data)
			if err != nil {
				return err
			}
			sealed[i] = wal.BatchEntry{OpType: e.OpType, Data: data}
		}
		entries = sealed
	}
	switch {
	case gs.useBatching && gs.batchedWAL != nil:
		return gs.batchedWAL.AppendBatchAtomic(entries)
	case gs.useCompression && gs.compressedWAL != nil:
		// Non-atomic fallback (see doc comment): each entry appended in order.
		for _, e := range entries {
			if _, err := gs.compressedWAL.Append(e.OpType, e.Data); err != nil {
				return err
			}
		}
		return nil
	case gs.wal != nil:
		return gs.wal.AppendBatchAtomic(entries)
	}
	return nil // No WAL configured (in-memory only).
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

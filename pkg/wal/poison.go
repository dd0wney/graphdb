package wal

import (
	"errors"
	"fmt"
)

// ErrWALPoisoned is returned by every append made after a Sync (fsync)
// failure. A fsync failure leaves the on-disk state of the flushed bytes
// unknown: the operating system may already have given up on the dirty
// pages the failed fsync could not write back, so a later, unrelated
// write's successful fsync gives no guarantee that the earlier bytes ever
// reached disk. A retry after a failed fsync is therefore not safe, so the
// WAL refuses every later append instead of pretending the failure was a
// one-off. The poison lasts until the process restarts.
var ErrWALPoisoned = errors.New("wal: poisoned by a prior sync failure, refusing further appends")

// wrapPoisoned builds the error an Append, AppendBatchAtomic or Enqueue call
// refuses with once a WAL is poisoned: ErrWALPoisoned wrapping the sync
// failure that poisoned it, so a caller can still see the original cause
// with errors.Is/errors.As.
func wrapPoisoned(cause error) error {
	return fmt.Errorf("%w: last sync failure: %w", ErrWALPoisoned, cause)
}

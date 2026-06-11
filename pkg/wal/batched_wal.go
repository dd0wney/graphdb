package wal

import (
	"hash/crc32"
	"sync"
	"time"
)

// BatchedWAL wraps WAL with batching capabilities for better write performance
type BatchedWAL struct {
	wal           *WAL
	buffer        []*pendingEntry
	batchSize     int
	flushInterval time.Duration
	mu            sync.Mutex
	// flushMu serializes whole flush bodies (buffer handoff + AppendBatch
	// + notify). bw.mu alone only guards the handoff: without flushMu a
	// CheckpointLSN could observe an empty buffer while a concurrent
	// flush's entries are still awaiting LSN assignment in AppendBatch,
	// and return a boundary that misses them.
	flushMu   sync.Mutex
	stopCh    chan struct{}
	flushCh   chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// pendingEntry represents an entry waiting to be flushed
type pendingEntry struct {
	opType OpType
	data   []byte
	doneCh chan error
}

// NewBatchedWAL creates a new batched WAL
func NewBatchedWAL(dataDir string, batchSize int, flushInterval time.Duration) (*BatchedWAL, error) {
	wal, err := NewWAL(dataDir)
	if err != nil {
		return nil, err
	}

	bw := &BatchedWAL{
		wal:           wal,
		buffer:        make([]*pendingEntry, 0, batchSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
		flushCh:       make(chan struct{}, 1),
	}

	// Start background flusher
	bw.wg.Add(1)
	go bw.backgroundFlusher()

	return bw, nil
}

// Pending represents an entry that has been enqueued into the batch buffer but
// is not yet durable. Wait blocks until the entry's batch has been flushed and
// fsynced.
//
// Splitting enqueue from wait lets a caller release a higher-level lock (e.g.
// storage's gs.mu) AFTER the entry is enqueued but BEFORE blocking on the
// fsync. Concurrent writers can then enqueue into the same batch — the
// group-commit path. Holding the higher-level lock across the wait (as a plain
// Append does) makes the batch unable to fill beyond one entry, defeating
// batching entirely. See Track P item (1).
type Pending struct {
	doneCh chan error
}

// Wait blocks until the enqueued entry's batch has been flushed and fsynced,
// returning the flush error (if any).
func (p *Pending) Wait() error {
	return <-p.doneCh
}

// Enqueue appends an entry to the batch buffer and returns a Pending handle
// WITHOUT waiting for durability. The caller MUST call Wait() on the returned
// handle before treating the write as durable. Entries become durable in
// enqueue order (batches flush FIFO), so a caller that enqueues under a lock
// preserves WAL order even after releasing that lock before Wait().
func (bw *BatchedWAL) Enqueue(opType OpType, data []byte) *Pending {
	doneCh := make(chan error, 1)

	entry := &pendingEntry{
		opType: opType,
		data:   data,
		doneCh: doneCh,
	}

	bw.mu.Lock()
	bw.buffer = append(bw.buffer, entry)
	shouldFlush := len(bw.buffer) >= bw.batchSize
	bw.mu.Unlock()

	// Trigger immediate flush if batch is full
	if shouldFlush {
		select {
		case bw.flushCh <- struct{}{}:
		default:
		}
	}

	return &Pending{doneCh: doneCh}
}

// Append enqueues an entry and blocks until it is durable. Equivalent to
// Enqueue followed by Wait; retained for callers that don't need to release a
// lock between the two.
func (bw *BatchedWAL) Append(opType OpType, data []byte) (uint64, error) {
	if err := bw.Enqueue(opType, data).Wait(); err != nil {
		return 0, err
	}

	// Return the LSN (we could track this more precisely, but for now return current LSN)
	return bw.wal.GetCurrentLSN(), nil
}

// backgroundFlusher periodically flushes buffered entries
func (bw *BatchedWAL) backgroundFlusher() {
	defer bw.wg.Done()

	ticker := time.NewTicker(bw.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bw.stopCh:
			// Final flush on shutdown
			bw.flush()
			return

		case <-ticker.C:
			bw.flush()

		case <-bw.flushCh:
			bw.flush()
		}
	}
}

// flush writes all buffered entries to WAL with a single fsync
func (bw *BatchedWAL) flush() {
	bw.flushMu.Lock()
	defer bw.flushMu.Unlock()
	bw.flushLocked()
}

// flushLocked is flush's body; callers must hold bw.flushMu.
func (bw *BatchedWAL) flushLocked() {
	bw.mu.Lock()
	if len(bw.buffer) == 0 {
		bw.mu.Unlock()
		return
	}

	// Take ownership of current buffer
	entries := bw.buffer
	bw.buffer = make([]*pendingEntry, 0, bw.batchSize)
	bw.mu.Unlock()

	// Write all entries using the low-level batch write
	err := bw.wal.AppendBatch(entries)

	// Notify all waiting goroutines
	for _, entry := range entries {
		entry.doneCh <- err
		close(entry.doneCh)
	}
}

// BatchEntry is one (opType, data) pair for an atomic batch write. It is the
// exported input to AppendBatchAtomic — the storage layer's transaction-commit
// path builds these directly (the internal pendingEntry carries a done-channel
// the synchronous atomic path does not need).
type BatchEntry struct {
	OpType OpType
	Data   []byte
}

// AppendBatch writes multiple buffered entries with a single fsync. Used by
// BatchedWAL.flush; delegates to AppendBatchAtomic so the write/flush/fsync
// logic lives in one place.
func (w *WAL) AppendBatch(entries []*pendingEntry) error {
	if len(entries) == 0 {
		return nil
	}
	batch := make([]BatchEntry, len(entries))
	for i, e := range entries {
		batch[i] = BatchEntry{OpType: e.opType, Data: e.data}
	}
	return w.AppendBatchAtomic(batch)
}

// AppendBatchAtomic writes all entries then a SINGLE flush + fsync, so the
// whole batch is durable all-or-none at the fsync boundary — the primitive a
// transaction commit needs for atomic durability. Synchronous: returns once
// durable (no done-channel). On a mid-batch write error it rolls back the LSN
// counter and returns the error.
func (w *WAL) AppendBatchAtomic(entries []BatchEntry) error {
	if len(entries) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Write all entries to buffer
	for _, entry := range entries {
		w.currentLSN++

		walEntry := Entry{
			LSN:       w.currentLSN,
			OpType:    entry.OpType,
			Data:      entry.Data,
			Checksum:  0, // Will be calculated in writeEntry
			Timestamp: time.Now().Unix(),
		}

		// Calculate checksum
		walEntry.Checksum = walEntry.calculateChecksum()

		if err := w.writeEntry(&walEntry); err != nil {
			// On error, rollback all LSNs
			w.currentLSN -= uint64(len(entries))
			return err
		}
	}

	// Single flush for all entries
	if err := w.writer.Flush(); err != nil {
		return err
	}

	// Single fsync for all entries (this is the key optimization)
	if err := w.file.Sync(); err != nil {
		return err
	}

	return nil
}

// AppendBatchAtomic writes a batch of entries to the underlying WAL with a
// single fsync (all-or-none at the fsync boundary), bypassing the background-
// flush queue. The underlying WAL serializes this against the background
// flusher via its own mutex, so it is safe to call concurrently with ticker
// flushes. Used by Transaction.Commit for atomic commit durability.
func (bw *BatchedWAL) AppendBatchAtomic(entries []BatchEntry) error {
	return bw.wal.AppendBatchAtomic(entries)
}

// calculateChecksum calculates CRC32 checksum for entry data
func (e *Entry) calculateChecksum() uint32 {
	return crc32.ChecksumIEEE(e.Data)
}

// Replay replays WAL entries
func (bw *BatchedWAL) Replay(handler func(*Entry) error) error {
	return bw.wal.Replay(handler)
}

// Truncate truncates the WAL
func (bw *BatchedWAL) Truncate() error {
	// Ensure all buffered entries are flushed first
	bw.flush()
	return bw.wal.Truncate()
}

// Close closes the batched WAL
func (bw *BatchedWAL) Close() error {
	var closeErr error
	bw.closeOnce.Do(func() {
		// Signal background flusher to stop
		close(bw.stopCh)

		// Wait for background flusher to complete (including final flush)
		bw.wg.Wait()

		// Close underlying WAL
		closeErr = bw.wal.Close()
	})
	return closeErr
}

// GetCurrentLSN returns the current LSN
func (bw *BatchedWAL) GetCurrentLSN() uint64 {
	return bw.wal.GetCurrentLSN()
}

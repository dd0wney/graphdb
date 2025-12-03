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
	stopCh        chan struct{}
	flushCh       chan struct{}
	wg            sync.WaitGroup
	closeOnce     sync.Once
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

// Append appends an entry to the batch buffer
func (bw *BatchedWAL) Append(opType OpType, data []byte) (uint64, error) {
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

	// Wait for flush to complete
	err := <-doneCh
	if err != nil {
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

// AppendBatch is a helper method on WAL to write multiple entries with single fsync
func (w *WAL) AppendBatch(entries []*pendingEntry) error {
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
			OpType:    entry.opType,
			Data:      entry.data,
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

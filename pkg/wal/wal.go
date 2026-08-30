package wal

import (
	"bufio"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/dd0wney/graphdb/pkg/faultsim"
	"github.com/dd0wney/graphdb/pkg/vfs"
)

// walFile is the file surface the WAL depends on. *os.File satisfies it, and
// production code always holds one.
//
// The indirection exists so tests can inject I/O faults: a disk that fails a
// write, an fsync that reports an error, a close that fails. Those paths carry
// the durability contract, and without a seam they were unreachable from a
// test. See wal_io_fault_test.go. This mirrors, in miniature, the substitutable
// VFS that SQLite uses for the same purpose.
type walFile interface {
	io.Reader
	io.Writer
	io.Seeker
	Sync() error
	Close() error
}

// WAL is a Write-Ahead Log for durability
type WAL struct {
	// fs is the filesystem driver. It is vfs.Default() unless a caller chose
	// another through NewWALWithFS, which is how an I/O fault or a simulated
	// power cut reaches this code — through the same path production takes,
	// not through a substituted test object.
	fs         vfs.FileSystem
	file       walFile
	writer     *bufio.Writer
	currentLSN uint64
	dataDir    string
	mu         sync.Mutex
}

// NewWAL creates a new Write-Ahead Log on the default filesystem driver.
func NewWAL(dataDir string) (*WAL, error) {
	return NewWALWithFS(dataDir, vfs.Default())
}

// NewWALWithFS creates a Write-Ahead Log on a caller-supplied filesystem
// driver.
//
// Intended for testing: pass a driver from pkg/vfs/vfstest to make the disk
// fail, or to simulate a power cut. It is exported rather than test-only on
// purpose — see pkg/vfs's package comment and docs/adr/0002.
func NewWALWithFS(dataDir string, fs vfs.FileSystem) (*WAL, error) {
	if fs == nil {
		fs = vfs.Default()
	}
	if err := fs.MkdirAll(dataDir, walDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	walPath := filepath.Join(dataDir, "wal.log")

	// Open or create WAL file
	file, err := fs.Open(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, walFilePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	wal := &WAL{
		fs:      fs,
		file:    file,
		writer:  bufio.NewWriter(file),
		dataDir: dataDir,
	}

	// Read existing entries to set currentLSN
	if err := wal.recoverLSN(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to recover LSN: %w", err)
	}

	return wal, nil
}

// Append appends a new entry to the WAL
func (w *WAL) Append(opType OpType, data []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check for LSN overflow (CRITICAL: prevents wraparound).
	//
	// faultsim makes this branch reachable. Honestly reaching it needs 2^64
	// appends, so before this the guard was code nobody had ever executed —
	// which is the state SQLite's sqlite3FaultSim exists to fix.
	if w.currentLSN == ^uint64(0) || faultsim.Fail(faultsim.WALLSNExhausted) { // MaxUint64
		return 0, fmt.Errorf("WAL LSN space exhausted - require WAL rotation")
	}

	w.currentLSN++
	lsn := w.currentLSN

	entry := Entry{
		LSN:       lsn,
		OpType:    opType,
		Data:      data,
		Checksum:  crc32.ChecksumIEEE(data),
		Timestamp: 0, // Will be set during write
	}

	if err := w.writeEntry(&entry); err != nil {
		w.currentLSN-- // Rollback LSN on error
		return 0, err
	}

	// Flush to disk for durability
	if err := w.writer.Flush(); err != nil {
		return 0, fmt.Errorf("failed to flush WAL: %w", err)
	}

	if err := w.file.Sync(); err != nil {
		return 0, fmt.Errorf("failed to sync WAL: %w", err)
	}

	return lsn, nil
}

// ReadAll reads all entries from the WAL
// Returns all valid entries read before any corruption is detected.
// Corruption is logged but does not return an error to allow partial recovery.
func (w *WAL) ReadAll() ([]*Entry, error) {
	// Seek to beginning
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, err
	}

	// Note: bufio.NewReader does not return an error - it always succeeds
	reader := bufio.NewReader(w.file)
	entries := make([]*Entry, 0)
	var entriesRead int
	var lastLSN uint64

	for {
		entry, err := w.readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Two very different reasons to stop, and conflating them costs
			// data.
			//
			// The DATA is bad or ended: a torn tail, a bad length, a record
			// the writer never finished. Nothing beyond that point was ever
			// durable, so the last good entry is the true end of the log and
			// recovery is complete. This is the common case after a crash.
			//
			// We COULD NOT READ: an allocation was refused, or the device
			// returned an error. Valid records may well follow the point we
			// gave up at, so the last entry we saw is NOT the end of the log.
			// Deriving currentLSN from it makes the next Append reuse an LSN
			// that already exists on disk, and after the non-advancing-LSN
			// guard above, that new record is silently dropped by the NEXT
			// recovery. Found by the out-of-memory sweep: refusing allocation
			// 3 of a 5-entry WAL left currentLSN=2 and the next append took
			// LSN 3.
			if isResourceError(err) {
				log.Printf("WARNING: WAL recovery could not read after %d entries: %v", entriesRead, err)
				return entries, fmt.Errorf("WAL recovery incomplete after %d entries: %w", entriesRead, err)
			}
			// Log corruption details for debugging
			log.Printf("WARNING: WAL corruption detected after %d entries: read error: %v", entriesRead, err)
			log.Printf("WARNING: WAL recovery stopped, %d entries recovered successfully", entriesRead)
			break
		}

		// A run of zero bytes decodes as a structurally valid record: LSN 0,
		// OpType 0, zero-length data, checksum 0 — and crc32 of empty input IS
		// 0, so the checksum below agrees with it. Filesystems zero-fill, so
		// this is the shape a partially written page most often takes.
		//
		// Two rules reject it. LSNs start at 1, because Append increments
		// before use, so LSN 0 never appears in a record this code wrote. And
		// LSNs only ever increase, so a record that does not advance is not
		// one of ours either.
		//
		// Without this, recovery walks past the damage, and recoverLSN takes
		// its LSN from the LAST entry — so a phantom resets the counter and
		// every later Append reuses LSNs that already exist on disk.
		if entry.LSN == 0 || (entriesRead > 0 && entry.LSN <= lastLSN) {
			log.Printf("WARNING: WAL entry %d has non-advancing LSN %d (previous %d): treating as corruption",
				entriesRead, entry.LSN, lastLSN)
			log.Printf("WARNING: WAL recovery stopped, %d entries recovered successfully", entriesRead)
			break
		}

		// Verify checksum
		expectedChecksum := crc32.ChecksumIEEE(entry.Data)
		if expectedChecksum != entry.Checksum {
			// Log checksum mismatch with details
			log.Printf("WARNING: WAL checksum mismatch at LSN %d (entry %d): expected %08x, got %08x",
				entry.LSN, entriesRead, expectedChecksum, entry.Checksum)
			log.Printf("WARNING: WAL recovery stopped, %d entries recovered successfully", entriesRead)
			break
		}

		entries = append(entries, entry)
		lastLSN = entry.LSN
		entriesRead++
	}

	// Seek back to end for appending
	if _, err := w.file.Seek(0, 2); err != nil {
		return nil, err
	}

	return entries, nil
}

// walDataErrors is the closed list of errors that mean "the record is bad or
// the log ended". Every one of them is produced by this package's own read
// path, so the list is knowable and short:
//
//   - io.EOF — the log ended on a record boundary. ReadAll breaks on this
//     before it asks isResourceError, so the row exists to keep the
//     classification total rather than because the loop needs it.
//   - io.ErrUnexpectedEOF — a short read inside a record. The writer never
//     finished it, so nothing beyond that point was ever durable.
//   - errWALRecordTooLarge — the header claims a length this package never
//     writes. readEntry rejects it before it allocates.
//
// An error that arrives from the vfs driver never belongs here. A driver
// reports that it could not read the bytes, which says nothing about whether
// the bytes exist.
//
// wal_device_error_test.go holds the table that pins every row of this list and
// of the classification below.
var walDataErrors = []error{io.EOF, io.ErrUnexpectedEOF, errWALRecordTooLarge}

// isResourceError reports whether a read stopped because the reader could not
// obtain something it needed, rather than because the data was bad or ended.
//
// A resource failure says nothing about the records that follow it, so
// recovery cannot treat the last entry it read as the end of the log.
//
// The question is asked in this direction on purpose. The list of data errors
// is closed, because this package produces all of them. The set of resource
// errors is open, because vfs.FileSystem is a published interface and a driver
// outside this repository returns whatever error it likes. So an unrecognised
// error is a resource failure, and a new driver cannot reopen this gap by
// choosing an error shape nobody here anticipated.
//
// The previous version asked the opposite question: it named *os.PathError as
// the device failure and read everything else as data. That held only for the
// os driver. pkg/vfs/vfstest's RoleFS reports a failed read as
// fmt.Errorf("read %s: %w", name, ErrInjected), which is not an *os.PathError,
// so a device error looked like a torn tail. ReadAll stopped and returned no
// error, recoverLSN took currentLSN from the last entry it managed to read, and
// the next Append reused an LSN that a later record on disk already held — a
// record the NEXT recovery then dropped at the non-advancing-LSN guard above.
// Injecting a read fault into a 20-record log recovered 7 entries and set
// currentLSN to 7.
//
// The two ways to be wrong cost very different amounts. Calling data a resource
// failure costs a refused open that names the real error, and an operator can
// see it. Calling a resource failure data costs a record, silently. So the
// default is the safe one.
func isResourceError(err error) bool {
	if err == nil {
		return false
	}
	for _, dataErr := range walDataErrors {
		if errors.Is(err, dataErr) {
			return false
		}
	}
	// alloc.ErrNoMemory reaches this code through a data-shaped path — readEntry
	// sizes the allocation from the record header — and is deliberately absent
	// from walDataErrors. The size is data. The refusal is not.
	return true
}

// recoverLSN recovers the current LSN from existing WAL entries
func (w *WAL) recoverLSN() error {
	entries, err := w.ReadAll()
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		w.currentLSN = entries[len(entries)-1].LSN
	}

	return nil
}

// Truncate truncates the WAL (used after snapshot)
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	walPath := filepath.Join(w.dataDir, "wal.log")

	// Flush any pending writes before truncating
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL before truncate: %w", err)
	}

	// Create the new file BEFORE closing the old one to ensure we have a valid handle
	newFile, err := w.fs.Open(walPath+".new", os.O_RDWR|os.O_CREATE|os.O_TRUNC, walFilePerm)
	if err != nil {
		return fmt.Errorf("failed to create new WAL file: %w", err)
	}

	// Close current file
	closeErr := w.file.Close()

	// Rename new file to replace old file (atomic on POSIX).
	//
	// faultsim reaches the recovery branch below, which otherwise runs only
	// when a rename fails after the replacement file was already created.
	renameErr := w.fs.Rename(walPath+".new", walPath)
	if faultsim.Fail(faultsim.WALRotateReopen) && renameErr == nil {
		renameErr = fmt.Errorf("simulated rename failure")
	}
	if err := renameErr; err != nil {
		// Failed to rename - close new file and return error
		newFile.Close()
		// Try to reopen old file to maintain consistent state
		if oldFile, reopenErr := w.fs.Open(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, walFilePerm); reopenErr == nil {
			w.file = oldFile
			w.writer = bufio.NewWriter(oldFile)
		}
		return fmt.Errorf("failed to rename WAL file: %w (close error: %v)", err, closeErr)
	}

	// Update state with new file
	w.file = newFile
	w.writer = bufio.NewWriter(newFile)
	w.currentLSN = 0

	// Return close error if rename succeeded but close failed (non-fatal but worth logging)
	if closeErr != nil {
		// Log but don't fail - we successfully truncated
		fmt.Printf("WARNING: failed to close old WAL file during truncate: %v\n", closeErr)
	}

	// Publish the new name. The rename is atomic, and the entry it creates in
	// the data directory is not durable until that directory is itself
	// synced. This runs after the handles are repointed so a failure leaves a
	// usable WAL and still reports that the rotation is not on stable storage.
	if err := vfs.SyncParentDir(w.fs, walPath); err != nil {
		return fmt.Errorf("failed to sync the WAL directory after rotation: %w", err)
	}

	return nil
}

// GetCurrentLSN returns the current LSN
func (w *WAL) GetCurrentLSN() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLSN
}

// Close closes the WAL.
//
// Every step runs even when an earlier one fails, and all faults are reported
// together. Returning early on a flush or fsync error used to leak the file
// descriptor, which is worst in the case that provokes it: a disk fault makes a
// caller drop this WAL and open another, so the leak compounds until the
// process hits EMFILE far away from the disk error that caused it.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	flushErr := w.writer.Flush()
	syncErr := w.file.Sync()
	closeErr := w.file.Close()

	return errors.Join(flushErr, syncErr, closeErr)
}

// Replay replays WAL entries to reconstruct state
func (w *WAL) Replay(handler func(*Entry) error) error {
	entries, err := w.ReadAll()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := handler(entry); err != nil {
			return fmt.Errorf("failed to replay entry LSN=%d: %w", entry.LSN, err)
		}
	}

	return nil
}

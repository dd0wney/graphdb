package audit

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PersistentAuditLogger writes audit logs to disk with tamper detection
type PersistentAuditLogger struct {
	logDir        string
	currentFile   *os.File
	writer        *bufio.Writer
	gzipWriter    *gzip.Writer
	lastHash      string
	eventCount    int64
	bytesWritten  int64
	rotationSize  int64 // Rotate when file exceeds this size
	rotationTime  time.Duration
	lastRotation  time.Time
	compress      bool
	retentionDays int
	mu            sync.Mutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewPersistentAuditLogger creates a new persistent audit logger
func NewPersistentAuditLogger(config *PersistentAuditConfig) (*PersistentAuditLogger, error) {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	logger := &PersistentAuditLogger{
		logDir:        config.LogDir,
		rotationSize:  config.RotationSize,
		rotationTime:  config.RotationTime,
		compress:      config.Compress,
		retentionDays: config.RetentionDays,
		lastRotation:  time.Now(),
		stopCh:        make(chan struct{}),
	}

	// Open current log file
	if err := logger.openLogFile(); err != nil {
		return nil, err
	}

	// Load last hash from previous log entries
	if err := logger.loadLastHash(); err != nil {
		// Not fatal, just means this is the first run
		logger.lastHash = ""
	}

	// Start background rotation and cleanup workers
	logger.wg.Add(2)
	go logger.rotationWorker()
	go logger.cleanupWorker()

	return logger, nil
}

// LogPersistent writes a persistent audit event to disk
func (l *PersistentAuditLogger) LogPersistent(event *Event, severity Severity) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create persistent event with hash chain
	persistentEvent := &PersistentEvent{
		Event:        event,
		Severity:     severity,
		PreviousHash: l.lastHash,
	}

	// Calculate event hash
	eventData, err := json.Marshal(persistentEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	hash := sha256.Sum256(eventData)
	persistentEvent.EventHash = hex.EncodeToString(hash[:])
	l.lastHash = persistentEvent.EventHash

	// Re-marshal with hash
	eventData, err = json.Marshal(persistentEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal event with hash: %w", err)
	}

	// Write to file (one event per line, JSONL format)
	eventLine := append(eventData, '\n')
	n, err := l.writer.Write(eventLine)
	if err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	// Flush to ensure it's written to OS buffer
	if err := l.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush event: %w", err)
	}

	// CRITICAL: Sync to disk for durability guarantee
	// This ensures audit entries are persisted before returning success
	// Required for compliance: SOC2, HIPAA, PCI-DSS, GDPR
	if err := l.currentFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync audit log to disk: %w", err)
	}

	l.eventCount++
	l.bytesWritten += int64(n)

	// Check if rotation is needed
	if l.shouldRotate() {
		return l.rotate()
	}

	return nil
}

// Log writes an event with Info severity (compatible with AuditLogger interface)
func (l *PersistentAuditLogger) Log(event *Event) error {
	return l.LogPersistent(event, SeverityInfo)
}

// LogCritical writes a critical severity event
func (l *PersistentAuditLogger) LogCritical(event *Event) error {
	return l.LogPersistent(event, SeverityCritical)
}

// LogWarning writes a warning severity event
func (l *PersistentAuditLogger) LogWarning(event *Event) error {
	return l.LogPersistent(event, SeverityWarning)
}

// openLogFile opens the current audit log file
func (l *PersistentAuditLogger) openLogFile() error {
	filename := filepath.Join(l.logDir, l.getCurrentLogFilename())

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Get current file size (do this before assigning to l.currentFile)
	stat, err := file.Stat()
	if err != nil {
		file.Close() // CRITICAL: Close file on error path to prevent leak
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	l.currentFile = file
	// Note: bufio.NewWriter does not return an error - it always succeeds
	l.writer = bufio.NewWriter(file)
	l.bytesWritten = stat.Size()

	return nil
}

// getCurrentLogFilename returns the filename for the current log file
func (l *PersistentAuditLogger) getCurrentLogFilename() string {
	return fmt.Sprintf("audit-%s.jsonl", time.Now().Format("2006-01-02"))
}

// GetEventCount returns the total number of events logged in the current file
func (l *PersistentAuditLogger) GetEventCount() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.eventCount
}

// Close closes the audit logger
func (l *PersistentAuditLogger) Close() error {
	// Stop background workers
	close(l.stopCh)
	l.wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()

	var flushErr error
	if l.writer != nil {
		flushErr = l.writer.Flush()
	}

	var closeErr error
	if l.currentFile != nil {
		closeErr = l.currentFile.Close() // CRITICAL: Always close file, even if flush failed
	}

	// Return the first error we encountered
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

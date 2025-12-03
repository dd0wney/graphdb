package audit

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// shouldRotate checks if log rotation is needed
func (l *PersistentAuditLogger) shouldRotate() bool {
	// Rotate if file size exceeds limit
	if l.rotationSize > 0 && l.bytesWritten >= l.rotationSize {
		return true
	}

	// Rotate if time has elapsed
	if l.rotationTime > 0 && time.Since(l.lastRotation) >= l.rotationTime {
		return true
	}

	return false
}

// rotate closes the current log file and opens a new one
func (l *PersistentAuditLogger) rotate() error {
	// Flush current file (but don't return early on error)
	var flushErr error
	if l.writer != nil {
		flushErr = l.writer.Flush()
	}

	// CRITICAL: Always close file, even if flush failed
	if l.currentFile != nil {
		oldFilename := l.currentFile.Name()
		if err := l.currentFile.Close(); err != nil {
			// If we also had a flush error, prefer that one
			if flushErr != nil {
				return fmt.Errorf("failed to flush before rotation: %w (also failed to close: %v)", flushErr, err)
			}
			return fmt.Errorf("failed to close log file: %w", err)
		}

		// Compress old file if enabled
		if l.compress {
			go func() {
				if err := l.compressFile(oldFilename); err != nil {
					fmt.Fprintf(os.Stderr, "failed to compress audit log file %s: %v\n", oldFilename, err)
				}
			}()
		}
	}

	// If flush failed, return that error now
	if flushErr != nil {
		return fmt.Errorf("failed to flush before rotation: %w", flushErr)
	}

	// Reset counters
	l.bytesWritten = 0
	l.eventCount = 0
	l.lastRotation = time.Now()

	// Open new log file
	return l.openLogFile()
}

// compressFile compresses a log file using gzip
func (l *PersistentAuditLogger) compressFile(filename string) (retErr error) {
	// Only compress if file doesn't already end with .gz
	if strings.HasSuffix(filename, ".gz") {
		return nil
	}

	gzFilename := filename + ".gz"

	// Open source file
	src, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open source file for compression: %w", err)
	}
	defer func() {
		if closeErr := src.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close source file: %w", closeErr)
		}
	}()

	// Create gzip file
	dst, err := os.Create(gzFilename)
	if err != nil {
		return fmt.Errorf("failed to create gzip file: %w", err)
	}
	defer func() {
		if closeErr := dst.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close gzip file: %w", closeErr)
		}
	}()

	// Create gzip writer
	gzWriter := gzip.NewWriter(dst)
	defer func() {
		// CRITICAL: gzWriter.Close() flushes buffered data - must check error
		if closeErr := gzWriter.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close gzip writer: %w", closeErr)
		}
	}()

	// Copy data
	if _, err := io.Copy(gzWriter, src); err != nil {
		if removeErr := os.Remove(gzFilename); removeErr != nil {
			return fmt.Errorf("failed to copy data: %w (also failed to remove partial file: %v)", err, removeErr)
		}
		return fmt.Errorf("failed to copy data: %w", err)
	}

	// Remove original file only if everything succeeded
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("failed to remove original file after compression: %w", err)
	}

	return nil
}

// loadLastHash loads the last event hash from the most recent log file
func (l *PersistentAuditLogger) loadLastHash() (retErr error) {
	// Find most recent log file
	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return err
	}

	// Sort by modification time (newest first)
	var logFiles []string
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "audit-") && (strings.HasSuffix(file.Name(), ".jsonl") || strings.HasSuffix(file.Name(), ".jsonl.gz")) {
			logFiles = append(logFiles, file.Name())
		}
	}

	if len(logFiles) == 0 {
		return fmt.Errorf("no log files found")
	}

	sort.Strings(logFiles)
	mostRecent := filepath.Join(l.logDir, logFiles[len(logFiles)-1])

	// Open file
	var reader io.Reader
	file, err := os.Open(mostRecent)
	if err != nil {
		return err
	}
	defer func() {
		// CRITICAL: Ensure file is properly closed
		if closeErr := file.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close audit log file: %w", closeErr)
		}
	}()

	// Handle gzip
	if strings.HasSuffix(mostRecent, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer func() {
			// CRITICAL: Ensure gzip reader is properly closed
			if closeErr := gzReader.Close(); closeErr != nil && retErr == nil {
				retErr = fmt.Errorf("failed to close gzip reader: %w", closeErr)
			}
		}()
		reader = gzReader
	} else {
		reader = file
	}

	// Read last line
	scanner := bufio.NewScanner(reader)
	var lastLine string
	for scanner.Scan() {
		lastLine = scanner.Text()
	}

	if scanner.Err() != nil {
		return scanner.Err()
	}

	// Parse last event
	var event PersistentEvent
	if err := json.Unmarshal([]byte(lastLine), &event); err != nil {
		return err
	}

	l.lastHash = event.EventHash
	return nil
}

// rotationWorker periodically checks if rotation is needed
func (l *PersistentAuditLogger) rotationWorker() {
	defer l.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.mu.Lock()
			if l.shouldRotate() {
				if err := l.rotate(); err != nil {
					fmt.Fprintf(os.Stderr, "failed to rotate audit log: %v\n", err)
				}
			}
			l.mu.Unlock()
		}
	}
}

// cleanupWorker periodically removes old log files
func (l *PersistentAuditLogger) cleanupWorker() {
	defer l.wg.Done()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

// cleanup removes log files older than retention period
func (l *PersistentAuditLogger) cleanup() error {
	if l.retentionDays <= 0 {
		return nil // Retention disabled
	}

	cutoffTime := time.Now().AddDate(0, 0, -l.retentionDays)

	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "audit-") {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoffTime) {
			filePath := filepath.Join(l.logDir, file.Name())
			if err := os.Remove(filePath); err != nil {
				fmt.Fprintf(os.Stderr, "failed to remove old audit log file %s: %v\n", filePath, err)
			}
		}
	}

	return nil
}

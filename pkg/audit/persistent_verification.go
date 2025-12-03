package audit

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifyIntegrity verifies the integrity of audit logs using hash chain
func (l *PersistentAuditLogger) VerifyIntegrity(filename string) (_ bool, retErr error) {
	file, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer func() {
		// CRITICAL: Ensure file is properly closed
		if closeErr := file.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close audit log file: %w", closeErr)
		}
	}()

	var reader io.Reader = file

	// Handle gzip
	if strings.HasSuffix(filename, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return false, err
		}
		defer func() {
			// CRITICAL: Ensure gzip reader is properly closed
			if closeErr := gzReader.Close(); closeErr != nil && retErr == nil {
				retErr = fmt.Errorf("failed to close gzip reader: %w", closeErr)
			}
		}()
		reader = gzReader
	}

	scanner := bufio.NewScanner(reader)
	var previousHash string

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		var event PersistentEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return false, fmt.Errorf("line %d: failed to parse event: %w", lineNum, err)
		}

		// Verify hash chain
		if event.PreviousHash != previousHash {
			return false, fmt.Errorf("line %d: hash chain broken (expected previous hash: %s, got: %s)",
				lineNum, previousHash, event.PreviousHash)
		}

		// Verify event hash
		eventCopy := event
		eventCopy.EventHash = "" // Clear hash for recalculation
		eventData, _ := json.Marshal(eventCopy)
		hash := sha256.Sum256(eventData)
		calculatedHash := hex.EncodeToString(hash[:])

		if calculatedHash != event.EventHash {
			return false, fmt.Errorf("line %d: event hash mismatch (expected: %s, got: %s)",
				lineNum, calculatedHash, event.EventHash)
		}

		previousHash = event.EventHash
	}

	if scanner.Err() != nil {
		return false, scanner.Err()
	}

	return true, nil
}

// GetStatistics returns statistics about the audit logger
func (l *PersistentAuditLogger) GetStatistics() AuditStatistics {
	l.mu.Lock()
	defer l.mu.Unlock()

	stats := AuditStatistics{
		TotalEvents:   l.eventCount,
		BytesWritten:  l.bytesWritten,
		CurrentFile:   l.getCurrentLogFilename(),
		LastRotation:  l.lastRotation,
		RetentionDays: l.retentionDays,
	}

	// Count total files
	files, err := os.ReadDir(l.logDir)
	if err == nil {
		for _, file := range files {
			if strings.HasPrefix(file.Name(), "audit-") {
				stats.TotalFiles++
				if info, err := file.Info(); err == nil {
					stats.TotalSize += info.Size()
				}
			}
		}
	}

	return stats
}

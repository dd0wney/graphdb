package audit

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Export exports audit logs to the specified writer
func (e *Exporter) Export(writer io.Writer, options *ExportOptions) error {
	// Read and filter events
	events, err := e.readEvents(options)
	if err != nil {
		return fmt.Errorf("failed to read events: %w", err)
	}

	// Apply limit
	if options.Limit > 0 && len(events) > options.Limit {
		events = events[:options.Limit]
	}

	// Export in specified format
	switch options.Format {
	case FormatJSON:
		return e.exportJSON(writer, events, options.Pretty)
	case FormatJSONL:
		return e.exportJSONL(writer, events)
	case FormatCSV:
		return e.exportCSV(writer, events)
	case FormatSyslog:
		return e.exportSyslog(writer, events)
	default:
		return fmt.Errorf("unsupported export format: %s", options.Format)
	}
}

// ExportToFile exports audit logs to a file
func (e *Exporter) ExportToFile(filename string, options *ExportOptions) (retErr error) {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create export file: %w", err)
	}
	defer func() {
		// CRITICAL: Always close file and check for errors
		if closeErr := file.Close(); closeErr != nil {
			// If export succeeded but close failed, return the close error
			if retErr == nil {
				retErr = fmt.Errorf("failed to close export file: %w", closeErr)
			}
		}
	}()

	retErr = e.Export(file, options)
	return retErr
}

// readEvents reads and filters events from audit log files
func (e *Exporter) readEvents(options *ExportOptions) ([]*PersistentEvent, error) {
	files, err := e.getLogFiles()
	if err != nil {
		return nil, err
	}

	var events []*PersistentEvent

	for _, filename := range files {
		fileEvents, err := e.readEventsFromFile(filename, options)
		if err != nil {
			// Log error but continue with other files
			continue
		}
		events = append(events, fileEvents...)
	}

	return events, nil
}

// getLogFiles returns a sorted list of audit log files
func (e *Exporter) getLogFiles() ([]string, error) {
	files, err := os.ReadDir(e.logDir)
	if err != nil {
		return nil, err
	}

	var logFiles []string
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "audit-") {
			logFiles = append(logFiles, filepath.Join(e.logDir, file.Name()))
		}
	}

	return logFiles, nil
}

// readEventsFromFile reads events from a single log file
func (e *Exporter) readEventsFromFile(filename string, options *ExportOptions) ([]*PersistentEvent, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close audit log file in readEventsFromFile: %v\n", closeErr)
		}
	}()

	var reader io.Reader = file

	// Handle gzip
	if strings.HasSuffix(filename, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closeErr := gzReader.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "failed to close gzip reader in readEventsFromFile: %v\n", closeErr)
			}
		}()
		reader = gzReader
	}

	var events []*PersistentEvent
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		var event PersistentEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			// Skip malformed events
			continue
		}

		// Apply filters
		if !e.matchesFilters(&event, options) {
			continue
		}

		events = append(events, &event)
	}

	return events, scanner.Err()
}

// matchesFilters checks if an event matches the export filters
func (e *Exporter) matchesFilters(event *PersistentEvent, options *ExportOptions) bool {
	if options == nil {
		return true
	}

	// Time filters
	if options.StartTime != nil && event.Timestamp.Before(*options.StartTime) {
		return false
	}
	if options.EndTime != nil && event.Timestamp.After(*options.EndTime) {
		return false
	}

	// Severity filter
	if options.Severity != "" && event.Severity != options.Severity {
		return false
	}

	// Action filter
	if options.Action != "" && event.Action != options.Action {
		return false
	}

	// Username filter
	if options.Username != "" && event.Username != options.Username {
		return false
	}

	// Resource type filter
	if options.ResourceType != "" && event.ResourceType != options.ResourceType {
		return false
	}

	return true
}

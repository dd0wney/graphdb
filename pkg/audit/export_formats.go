package audit

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// exportJSON exports events as JSON array
func (e *Exporter) exportJSON(writer io.Writer, events []*PersistentEvent, pretty bool) error {
	// Note: json.NewEncoder does not return an error - it always succeeds
	encoder := json.NewEncoder(writer)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(events)
}

// exportJSONL exports events as JSON Lines (one JSON object per line)
func (e *Exporter) exportJSONL(writer io.Writer, events []*PersistentEvent) error {
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// exportCSV exports events as CSV
func (e *Exporter) exportCSV(writer io.Writer, events []*PersistentEvent) (retErr error) {
	csvWriter := csv.NewWriter(writer)
	defer func() {
		// CRITICAL: Always flush CSV writer to ensure buffered data is written
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			// If we don't have an error yet, return the flush error
			if retErr == nil {
				retErr = fmt.Errorf("CSV writer flush error: %w", err)
			}
		}
	}()

	// Write header
	header := []string{
		"ID",
		"Timestamp",
		"Severity",
		"Username",
		"UserID",
		"Action",
		"ResourceType",
		"ResourceID",
		"Status",
		"ErrorMessage",
		"IPAddress",
		"UserAgent",
	}
	if err := csvWriter.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write events
	for _, event := range events {
		record := []string{
			event.ID,
			event.Timestamp.Format(time.RFC3339),
			string(event.Severity),
			event.Username,
			event.UserID,
			string(event.Action),
			string(event.ResourceType),
			event.ResourceID,
			string(event.Status),
			event.ErrorMessage,
			event.IPAddress,
			event.UserAgent,
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	return nil
}

// exportSyslog exports events in syslog format (RFC 5424)
func (e *Exporter) exportSyslog(writer io.Writer, events []*PersistentEvent) error {
	for _, event := range events {
		// Determine syslog severity (0-7 scale)
		var syslogSeverity int
		switch event.Severity {
		case SeverityCritical:
			syslogSeverity = 2 // Critical
		case SeverityWarning:
			syslogSeverity = 4 // Warning
		default:
			syslogSeverity = 6 // Informational
		}

		// Determine syslog facility (16 = local use 0)
		facility := 16
		priority := facility*8 + syslogSeverity

		// Format: <priority>version timestamp hostname app-name procid msgid structured-data msg
		syslogMsg := fmt.Sprintf("<%d>1 %s graphdb audit - - [event@graphdb id=\"%s\" user=\"%s\" action=\"%s\" resource=\"%s\" status=\"%s\"] %s %s %s %s\n",
			priority,
			event.Timestamp.Format(time.RFC3339),
			event.ID,
			event.Username,
			event.Action,
			event.ResourceType,
			event.Status,
			event.Username,
			string(event.Action),
			string(event.ResourceType),
			event.ResourceID,
		)

		if _, err := writer.Write([]byte(syslogMsg)); err != nil {
			return err
		}
	}

	return nil
}

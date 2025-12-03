package audit

import (
	"time"
)

// Severity levels for audit events
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Enhanced event with additional security fields
type PersistentEvent struct {
	*Event
	Severity     Severity `json:"severity"`
	PreviousHash string   `json:"previous_hash,omitempty"` // For tamper detection
	EventHash    string   `json:"event_hash"`              // Hash of this event
}

// PersistentAuditConfig holds configuration for persistent audit logging
type PersistentAuditConfig struct {
	LogDir        string        // Directory to store audit logs
	RotationSize  int64         // Rotate log file when it exceeds this size (bytes)
	RotationTime  time.Duration // Rotate log file after this duration
	Compress      bool          // Compress rotated log files
	RetentionDays int           // Delete logs older than this many days
}

// DefaultPersistentConfig returns default configuration
func DefaultPersistentConfig() *PersistentAuditConfig {
	return &PersistentAuditConfig{
		LogDir:        "./data/audit",
		RotationSize:  100 * 1024 * 1024, // 100MB
		RotationTime:  24 * time.Hour,    // Daily
		Compress:      true,
		RetentionDays: 365, // 1 year
	}
}

// AuditStatistics holds statistics about the audit logger
type AuditStatistics struct {
	TotalEvents   int64     `json:"total_events"`
	TotalFiles    int       `json:"total_files"`
	TotalSize     int64     `json:"total_size_bytes"`
	BytesWritten  int64     `json:"bytes_written"`
	CurrentFile   string    `json:"current_file"`
	LastRotation  time.Time `json:"last_rotation"`
	RetentionDays int       `json:"retention_days"`
}

package audit

import "time"

// ExportFormat represents the format for exporting audit logs
type ExportFormat string

const (
	FormatJSON   ExportFormat = "json"
	FormatCSV    ExportFormat = "csv"
	FormatJSONL  ExportFormat = "jsonl" // JSON Lines (one JSON object per line)
	FormatSyslog ExportFormat = "syslog"
)

// ExportOptions holds options for exporting audit logs
type ExportOptions struct {
	Format       ExportFormat
	StartTime    *time.Time
	EndTime      *time.Time
	Severity     Severity
	Action       Action
	Username     string
	ResourceType ResourceType
	Limit        int  // Maximum number of events to export (0 = unlimited)
	Pretty       bool // Pretty-print JSON output
}

// Exporter handles exporting audit logs to various formats
type Exporter struct {
	logDir string
}

// NewExporter creates a new audit log exporter
func NewExporter(logDir string) *Exporter {
	return &Exporter{
		logDir: logDir,
	}
}

// ReportStatistics holds statistical data for reports
type ReportStatistics struct {
	TotalEvents  int
	BySeverity   map[Severity]int
	ByAction     map[Action]int
	ByStatus     map[Status]int
	TopUsers     []UserStat
	TopResources []ResourceStat
}

// UserStat holds statistics for a user
type UserStat struct {
	Username string
	Count    int
}

// ResourceStat holds statistics for a resource
type ResourceStat struct {
	ResourceType ResourceType
	ResourceID   string
	Count        int
}

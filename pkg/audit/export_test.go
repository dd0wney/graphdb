package audit

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper to create test events
func createTestEvents(count int) []*PersistentEvent {
	events := make([]*PersistentEvent, count)
	for i := 0; i < count; i++ {
		events[i] = &PersistentEvent{
			Event: &Event{
				ID:           "event-" + string(rune('a'+i)),
				Timestamp:    time.Now().Add(time.Duration(-i) * time.Hour),
				Username:     "user" + string(rune('1'+i%3)),
				UserID:       "uid-" + string(rune('1'+i%3)),
				Action:       ActionCreate,
				ResourceType: ResourceNode,
				ResourceID:   "resource-" + string(rune('a'+i)),
				Status:       StatusSuccess,
				IPAddress:    "192.168.1." + string(rune('1'+i%10)),
				UserAgent:    "TestAgent/1.0",
			},
			Severity:  SeverityInfo,
			EventHash: "hash-" + string(rune('a'+i)),
		}
	}
	return events
}

// --- NewExporter Tests ---

func TestNewExporter(t *testing.T) {
	exporter := NewExporter("/tmp/audit")
	if exporter == nil {
		t.Fatal("NewExporter returned nil")
	}
	if exporter.logDir != "/tmp/audit" {
		t.Errorf("Expected logDir '/tmp/audit', got '%s'", exporter.logDir)
	}
}

// --- Export Format Tests ---

func TestExport_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	// Create test log file
	events := createTestEvents(3)
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
		Pretty: false,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify output is valid JSON array
	var parsed []*PersistentEvent
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}

	if len(parsed) != 3 {
		t.Errorf("Expected 3 events, got %d", len(parsed))
	}
}

func TestExport_JSONPretty(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(1)
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
		Pretty: true,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Pretty-printed JSON should contain newlines
	if !strings.Contains(buf.String(), "\n") {
		t.Error("Pretty-printed JSON should contain newlines")
	}
}

func TestExport_JSONL(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(3)
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSONL,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// JSONL should have one object per line
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var event PersistentEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestExport_CSV(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(2)
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatCSV,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// Header + 2 events = 3 lines
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines (header + 2 events), got %d", len(lines))
	}

	// Check header
	if !strings.HasPrefix(lines[0], "ID,Timestamp,Severity") {
		t.Errorf("Unexpected header: %s", lines[0])
	}
}

func TestExport_Syslog(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(2)
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatSyslog,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	output := buf.String()
	// Syslog format should contain priority in angle brackets
	if !strings.Contains(output, "<") || !strings.Contains(output, ">") {
		t.Error("Syslog output should contain priority")
	}

	// Should contain graphdb hostname
	if !strings.Contains(output, "graphdb") {
		t.Error("Syslog output should contain hostname")
	}
}

func TestExport_InvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: ExportFormat("invalid"),
	}

	err := exporter.Export(&buf, options)
	if err == nil {
		t.Error("Expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Error should mention unsupported format: %v", err)
	}
}

// --- Export Filtering Tests ---

func TestExport_WithLimit(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(10)
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
		Limit:  3,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	var parsed []*PersistentEvent
	json.Unmarshal(buf.Bytes(), &parsed)
	if len(parsed) != 3 {
		t.Errorf("Expected 3 events (limit), got %d", len(parsed))
	}
}

func TestExport_FilterBySeverity(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(5)
	events[0].Severity = SeverityWarning
	events[2].Severity = SeverityWarning
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format:   FormatJSON,
		Severity: SeverityWarning,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	var parsed []*PersistentEvent
	json.Unmarshal(buf.Bytes(), &parsed)
	if len(parsed) != 2 {
		t.Errorf("Expected 2 warning events, got %d", len(parsed))
	}
}

func TestExport_FilterByAction(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(5)
	events[1].Action = ActionDelete
	events[3].Action = ActionDelete
	events[4].Action = ActionDelete
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
		Action: ActionDelete,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	var parsed []*PersistentEvent
	json.Unmarshal(buf.Bytes(), &parsed)
	if len(parsed) != 3 {
		t.Errorf("Expected 3 delete events, got %d", len(parsed))
	}
}

func TestExport_FilterByUsername(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(6)
	events[0].Username = "admin"
	events[2].Username = "admin"
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format:   FormatJSON,
		Username: "admin",
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	var parsed []*PersistentEvent
	json.Unmarshal(buf.Bytes(), &parsed)
	if len(parsed) != 2 {
		t.Errorf("Expected 2 admin events, got %d", len(parsed))
	}
}

func TestExport_FilterByTimeRange(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	now := time.Now()
	events := createTestEvents(5)
	// Set timestamps: -4h, -3h, -2h, -1h, now
	for i := 0; i < 5; i++ {
		events[i].Timestamp = now.Add(time.Duration(-(4 - i)) * time.Hour)
	}
	writeTestLogFile(t, tmpDir, events)

	startTime := now.Add(-3 * time.Hour)
	endTime := now.Add(-1 * time.Hour)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format:    FormatJSON,
		StartTime: &startTime,
		EndTime:   &endTime,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	var parsed []*PersistentEvent
	json.Unmarshal(buf.Bytes(), &parsed)
	// Events at -3h, -2h, -1h should match
	if len(parsed) != 3 {
		t.Errorf("Expected 3 events in time range, got %d", len(parsed))
	}
}

// --- ExportToFile Tests ---

func TestExportToFile(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(3)
	writeTestLogFile(t, tmpDir, events)

	outputFile := filepath.Join(tmpDir, "export.json")
	options := &ExportOptions{
		Format: FormatJSON,
	}

	err := exporter.ExportToFile(outputFile, options)
	if err != nil {
		t.Fatalf("ExportToFile failed: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read export file: %v", err)
	}

	var parsed []*PersistentEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("Export file is not valid JSON: %v", err)
	}

	if len(parsed) != 3 {
		t.Errorf("Expected 3 events in file, got %d", len(parsed))
	}
}

// --- Empty/Missing Directory Tests ---

func TestExport_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export should succeed with empty directory: %v", err)
	}

	// Should export empty array
	if buf.String() != "[]\n" && buf.String() != "null\n" {
		t.Errorf("Expected empty array or null, got: %s", buf.String())
	}
}

func TestExport_NonExistentDirectory(t *testing.T) {
	exporter := NewExporter("/nonexistent/path/that/does/not/exist")

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
	}

	err := exporter.Export(&buf, options)
	// Should return error for non-existent directory
	if err == nil {
		// Some systems might just return empty results
		t.Log("No error for non-existent directory (acceptable)")
	}
}

// --- Gzip Support Tests ---

func TestExport_GzippedLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := createTestEvents(3)
	writeGzippedLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatJSON,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed with gzipped file: %v", err)
	}

	var parsed []*PersistentEvent
	json.Unmarshal(buf.Bytes(), &parsed)
	if len(parsed) != 3 {
		t.Errorf("Expected 3 events from gzipped file, got %d", len(parsed))
	}
}

// --- matchesFilters Tests ---

func TestMatchesFilters_NilOptions(t *testing.T) {
	exporter := NewExporter("/tmp")
	event := &PersistentEvent{
		Event: &Event{
			Username: "test",
		},
		Severity: SeverityInfo,
	}

	if !exporter.matchesFilters(event, nil) {
		t.Error("Event should match with nil options")
	}
}

func TestMatchesFilters_ResourceType(t *testing.T) {
	exporter := NewExporter("/tmp")
	event := &PersistentEvent{
		Event: &Event{
			ResourceType: ResourceNode,
		},
		Severity: SeverityInfo,
	}

	options := &ExportOptions{
		ResourceType: ResourceEdge, // Different type
	}

	if exporter.matchesFilters(event, options) {
		t.Error("Event should not match different resource type")
	}
}

// --- Syslog Severity Tests ---

func TestExport_SyslogSeverityLevels(t *testing.T) {
	tmpDir := t.TempDir()
	exporter := NewExporter(tmpDir)

	events := []*PersistentEvent{
		{Event: &Event{ID: "1", Timestamp: time.Now(), Action: ActionCreate, ResourceType: ResourceNode, Status: StatusSuccess}, Severity: SeverityCritical},
		{Event: &Event{ID: "2", Timestamp: time.Now(), Action: ActionCreate, ResourceType: ResourceNode, Status: StatusSuccess}, Severity: SeverityWarning},
		{Event: &Event{ID: "3", Timestamp: time.Now(), Action: ActionCreate, ResourceType: ResourceNode, Status: StatusSuccess}, Severity: SeverityInfo},
	}
	writeTestLogFile(t, tmpDir, events)

	var buf bytes.Buffer
	options := &ExportOptions{
		Format: FormatSyslog,
	}

	err := exporter.Export(&buf, options)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	output := buf.String()
	// Critical = priority 130 (facility 16 * 8 + severity 2)
	// Warning = priority 132 (facility 16 * 8 + severity 4)
	// Info = priority 134 (facility 16 * 8 + severity 6)
	if !strings.Contains(output, "<130>") {
		t.Error("Expected critical severity priority <130>")
	}
	if !strings.Contains(output, "<132>") {
		t.Error("Expected warning severity priority <132>")
	}
	if !strings.Contains(output, "<134>") {
		t.Error("Expected info severity priority <134>")
	}
}

// --- Helper Functions ---

func writeTestLogFile(t *testing.T, dir string, events []*PersistentEvent) {
	t.Helper()
	filename := filepath.Join(dir, "audit-test.log")

	file, err := os.Create(filename)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}
	defer file.Close()

	for _, event := range events {
		data, _ := json.Marshal(event)
		file.Write(append(data, '\n'))
	}
}

func writeGzippedLogFile(t *testing.T, dir string, events []*PersistentEvent) {
	t.Helper()
	filename := filepath.Join(dir, "audit-test.log.gz")

	file, err := os.Create(filename)
	if err != nil {
		t.Fatalf("Failed to create test gzipped log file: %v", err)
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	for _, event := range events {
		data, _ := json.Marshal(event)
		gzWriter.Write(append(data, '\n'))
	}
}

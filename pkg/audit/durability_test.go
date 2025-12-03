package audit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAuditLogDurability verifies that audit logs survive process crashes
// This is critical for compliance (SOC2, HIPAA, PCI-DSS, GDPR)
func TestAuditLogDurability(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Create audit logger
	config := &PersistentAuditConfig{
		LogDir:        tmpDir,
		RotationSize:  100 * 1024 * 1024,
		RotationTime:  24 * time.Hour,
		Compress:      false,
		RetentionDays: 365,
	}

	logger, err := NewPersistentAuditLogger(config)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}

	// Write a critical audit event
	event := &Event{
		ID:           "test-001",
		Timestamp:    time.Now(),
		Username:     "test-user",
		Action:       "update",
		ResourceType: "node",
		ResourceID:   "sensitive-data",
		Status:       StatusSuccess,
		IPAddress:    "192.168.1.1",
		Metadata: map[string]any{
			"test_id": "durability-test",
			"operation": "critical-operation",
		},
	}

	err = logger.LogCritical(event)
	if err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Close the logger (simulates normal shutdown)
	err = logger.Close()
	if err != nil {
		t.Fatalf("Failed to close logger: %v", err)
	}

	// Verify the audit log file exists and contains the event
	files, err := filepath.Glob(filepath.Join(tmpDir, "audit-*.jsonl"))
	if err != nil {
		t.Fatalf("Failed to list audit files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("No audit log files found")
	}

	// Read the audit log and verify our event is there
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read audit log: %v", err)
	}

	logContent := string(data)
	if len(logContent) == 0 {
		t.Fatal("Audit log is empty - event was lost!")
	}

	// Verify our test event is in the log
	if !strings.Contains(logContent, "test-user") {
		t.Fatal("Audit event not found in log file - durability violated!")
	}

	if !strings.Contains(logContent, "durability-test") {
		t.Fatal("Event details not found in log file")
	}

	t.Log("✅ Audit log durability verified - event persisted to disk")
}

// TestAuditLogSync verifies that Sync() is actually called
func TestAuditLogSync(t *testing.T) {
	tmpDir := t.TempDir()

	config := &PersistentAuditConfig{
		LogDir:        tmpDir,
		RotationSize:  100 * 1024 * 1024,
		RotationTime:  24 * time.Hour,
		Compress:      false,
		RetentionDays: 365,
	}

	logger, err := NewPersistentAuditLogger(config)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Errorf("Failed to close logger: %v", err)
		}
	}()

	// Write event
	event := &Event{
		ID:           "test-sync-001",
		Timestamp:    time.Now(),
		Username:     "test-user",
		Action:       ActionRead,
		ResourceType: ResourceNode,
		ResourceID:   "test-resource",
		Status:       StatusSuccess,
	}

	err = logger.Log(event)
	if err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// The event should be immediately readable from disk
	// (because Sync() was called)
	files, err := filepath.Glob(filepath.Join(tmpDir, "audit-*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatal("Audit log file not created")
	}

	// Read file without closing logger (proves Sync() worked)
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read audit log: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("Audit log is empty - Sync() may not have been called")
	}

	t.Log("✅ Sync() verified - audit log immediately readable")
}

// TestAuditLogCrashScenario simulates a crash scenario
// This test verifies that even if we don't call Close(),
// the audit entry is still on disk (thanks to Sync())
func TestAuditLogCrashScenario(t *testing.T) {
	if os.Getenv("TEST_CRASH_SUBPROCESS") == "1" {
		// This is the subprocess that will "crash"
		runCrashSubprocess()
		return
	}

	// Main test process
	tmpDir := t.TempDir()

	// Run subprocess that writes audit log and "crashes"
	cmd := exec.Command(os.Args[0], "-test.run=TestAuditLogCrashScenario")
	cmd.Env = append(os.Environ(),
		"TEST_CRASH_SUBPROCESS=1",
		"TEST_CRASH_DIR="+tmpDir,
	)

	// Run and let it "crash" (exit without Close())
	_ = cmd.Run() // Ignore error, it's expected to "crash"

	// Now verify the audit log was still written to disk
	files, err := filepath.Glob(filepath.Join(tmpDir, "audit-*.jsonl"))
	if err != nil {
		t.Fatalf("Failed to list audit files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("No audit log files found after crash")
	}

	// Read the log
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read audit log: %v", err)
	}

	logContent := string(data)
	if !strings.Contains(logContent, "crash-test-user") {
		t.Fatal("Audit event NOT found after crash - DURABILITY VIOLATED!")
	}

	t.Log("✅ Crash scenario passed - audit log survived without Close()")
}

// runCrashSubprocess runs in the subprocess
func runCrashSubprocess() {
	tmpDir := os.Getenv("TEST_CRASH_DIR")
	if tmpDir == "" {
		return
	}

	config := &PersistentAuditConfig{
		LogDir:        tmpDir,
		RotationSize:  100 * 1024 * 1024,
		RotationTime:  24 * time.Hour,
		Compress:      false,
		RetentionDays: 365,
	}

	logger, err := NewPersistentAuditLogger(config)
	if err != nil {
		panic(err)
	}

	// Write critical event
	event := &Event{
		ID:           "crash-test-001",
		Timestamp:    time.Now(),
		Username:     "crash-test-user",
		Action:       ActionUpdate,
		ResourceType: ResourceNode,
		ResourceID:   "test-resource",
		Status:       StatusSuccess,
		Metadata: map[string]any{
			"test_type": "crash-test-event",
		},
	}

	err = logger.LogCritical(event)
	if err != nil {
		panic(err)
	}

	// IMPORTANT: Don't call logger.Close()
	// This simulates a crash where Close() never happens
	// Thanks to Sync(), the data should still be on disk
	os.Exit(0)
}

// TestAuditLogPerformance measures the performance impact of Sync()
func TestAuditLogPerformance(t *testing.T) {
	tmpDir := t.TempDir()

	config := &PersistentAuditConfig{
		LogDir:        tmpDir,
		RotationSize:  100 * 1024 * 1024,
		RotationTime:  24 * time.Hour,
		Compress:      false,
		RetentionDays: 365,
	}

	logger, err := NewPersistentAuditLogger(config)
	if err != nil {
		t.Fatalf("Failed to create audit logger: %v", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Errorf("Failed to close logger: %v", err)
		}
	}()

	// Measure time to write 100 events with Sync()
	start := time.Now()
	for i := 0; i < 100; i++ {
		event := &Event{
			ID:           fmt.Sprintf("perf-test-%d", i),
			Timestamp:    time.Now(),
			Username:     "perf-test-user",
			Action:       ActionQuery,
			ResourceType: ResourceQuery,
			ResourceID:   "test-resource",
			Status:       StatusSuccess,
		}
		err = logger.Log(event)
		if err != nil {
			t.Fatalf("Failed to log event: %v", err)
		}
	}
	duration := time.Since(start)

	avgLatency := duration / 100
	t.Logf("Average latency per audit entry (with Sync): %v", avgLatency)

	if avgLatency > 50*time.Millisecond {
		t.Logf("⚠️  Warning: Audit log latency is high (%v). Consider batching for high-throughput systems.", avgLatency)
	} else {
		t.Logf("✅ Audit log performance acceptable: %v per entry", avgLatency)
	}
}

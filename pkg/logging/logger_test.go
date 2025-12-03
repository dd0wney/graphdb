package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{DebugLevel, "DEBUG"},
		{InfoLevel, "INFO"},
		{WarnLevel, "WARN"},
		{ErrorLevel, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("Level.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"DEBUG", DebugLevel},
		{"debug", DebugLevel},
		{"INFO", InfoLevel},
		{"info", InfoLevel},
		{"WARN", WarnLevel},
		{"warn", WarnLevel},
		{"WARNING", WarnLevel},
		{"warning", WarnLevel},
		{"ERROR", ErrorLevel},
		{"error", ErrorLevel},
		{"invalid", InfoLevel}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseLevel(tt.input); got != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFieldConstructors(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		f := String("key", "value")
		if f.Key != "key" || f.Value != "value" {
			t.Errorf("String() = %+v, want {Key:key Value:value}", f)
		}
	})

	t.Run("Int", func(t *testing.T) {
		f := Int("count", 42)
		if f.Key != "count" || f.Value != 42 {
			t.Errorf("Int() = %+v, want {Key:count Value:42}", f)
		}
	})

	t.Run("Int64", func(t *testing.T) {
		f := Int64("id", 1234567890)
		if f.Key != "id" || f.Value != int64(1234567890) {
			t.Errorf("Int64() = %+v", f)
		}
	})

	t.Run("Uint64", func(t *testing.T) {
		f := Uint64("id", 9876543210)
		if f.Key != "id" || f.Value != uint64(9876543210) {
			t.Errorf("Uint64() = %+v", f)
		}
	})

	t.Run("Float64", func(t *testing.T) {
		f := Float64("ratio", 3.14)
		if f.Key != "ratio" || f.Value != 3.14 {
			t.Errorf("Float64() = %+v", f)
		}
	})

	t.Run("Bool", func(t *testing.T) {
		f := Bool("enabled", true)
		if f.Key != "enabled" || f.Value != true {
			t.Errorf("Bool() = %+v", f)
		}
	})

	t.Run("Duration", func(t *testing.T) {
		d := 5 * time.Second
		f := Duration("timeout", d)
		if f.Key != "timeout" || f.Value != "5s" {
			t.Errorf("Duration() = %+v", f)
		}
	})

	t.Run("Error", func(t *testing.T) {
		err := errors.New("test error")
		f := Error(err)
		if f.Key != "error" || f.Value != "test error" {
			t.Errorf("Error() = %+v", f)
		}
	})

	t.Run("Error_nil", func(t *testing.T) {
		f := Error(nil)
		if f.Key != "error" || f.Value != nil {
			t.Errorf("Error(nil) = %+v", f)
		}
	})

	t.Run("Any", func(t *testing.T) {
		data := map[string]int{"a": 1, "b": 2}
		f := Any("data", data)
		if f.Key != "data" {
			t.Errorf("Any() key = %v, want data", f.Key)
		}
	})
}

func TestJSONLogger_BasicLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, DebugLevel)

	logger.Info("test message", String("key", "value"))

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to unmarshal log entry: %v", err)
	}

	if entry.Level != "INFO" {
		t.Errorf("Level = %v, want INFO", entry.Level)
	}
	if entry.Message != "test message" {
		t.Errorf("Message = %v, want 'test message'", entry.Message)
	}
	if entry.Fields["key"] != "value" {
		t.Errorf("Fields[key] = %v, want 'value'", entry.Fields["key"])
	}
	if entry.Time == "" {
		t.Error("Time field is empty")
	}
}

func TestJSONLogger_LogLevels(t *testing.T) {
	tests := []struct {
		name     string
		logLevel Level
		logFunc  func(Logger)
		expected string
	}{
		{
			name:     "Debug",
			logLevel: DebugLevel,
			logFunc:  func(l Logger) { l.Debug("debug msg") },
			expected: "DEBUG",
		},
		{
			name:     "Info",
			logLevel: InfoLevel,
			logFunc:  func(l Logger) { l.Info("info msg") },
			expected: "INFO",
		},
		{
			name:     "Warn",
			logLevel: WarnLevel,
			logFunc:  func(l Logger) { l.Warn("warn msg") },
			expected: "WARN",
		},
		{
			name:     "Error",
			logLevel: ErrorLevel,
			logFunc:  func(l Logger) { l.Error("error msg") },
			expected: "ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewJSONLogger(&buf, DebugLevel)

			tt.logFunc(logger)

			var entry LogEntry
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if entry.Level != tt.expected {
				t.Errorf("Level = %v, want %v", entry.Level, tt.expected)
			}
		})
	}
}

func TestJSONLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, WarnLevel)

	// These should not be logged
	logger.Debug("debug message")
	logger.Info("info message")

	// These should be logged
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should only have 2 log entries (WARN and ERROR)
	if len(lines) != 2 {
		t.Errorf("Expected 2 log entries, got %d", len(lines))
	}

	// Verify WARN entry
	var warnEntry LogEntry
	if err := json.Unmarshal([]byte(lines[0]), &warnEntry); err != nil {
		t.Fatalf("Failed to unmarshal WARN entry: %v", err)
	}
	if warnEntry.Level != "WARN" {
		t.Errorf("First entry level = %v, want WARN", warnEntry.Level)
	}

	// Verify ERROR entry
	var errorEntry LogEntry
	if err := json.Unmarshal([]byte(lines[1]), &errorEntry); err != nil {
		t.Fatalf("Failed to unmarshal ERROR entry: %v", err)
	}
	if errorEntry.Level != "ERROR" {
		t.Errorf("Second entry level = %v, want ERROR", errorEntry.Level)
	}
}

func TestJSONLogger_MultipleFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, InfoLevel)

	logger.Info("test",
		String("str", "hello"),
		Int("num", 42),
		Bool("flag", true),
	)

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if entry.Fields["str"] != "hello" {
		t.Errorf("str field = %v, want hello", entry.Fields["str"])
	}
	if entry.Fields["num"] != float64(42) { // JSON unmarshals numbers as float64
		t.Errorf("num field = %v, want 42", entry.Fields["num"])
	}
	if entry.Fields["flag"] != true {
		t.Errorf("flag field = %v, want true", entry.Fields["flag"])
	}
}

func TestJSONLogger_With(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, InfoLevel)

	// Create child logger with preset fields
	childLogger := logger.With(
		String("component", "storage"),
		String("version", "1.0"),
	)

	// Log with additional fields
	childLogger.Info("test message", String("action", "create"))

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Check preset fields
	if entry.Fields["component"] != "storage" {
		t.Errorf("component field = %v, want storage", entry.Fields["component"])
	}
	if entry.Fields["version"] != "1.0" {
		t.Errorf("version field = %v, want 1.0", entry.Fields["version"])
	}

	// Check additional field
	if entry.Fields["action"] != "create" {
		t.Errorf("action field = %v, want create", entry.Fields["action"])
	}
}

func TestJSONLogger_SetLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, InfoLevel)

	if logger.GetLevel() != InfoLevel {
		t.Errorf("Initial level = %v, want InfoLevel", logger.GetLevel())
	}

	logger.SetLevel(ErrorLevel)

	if logger.GetLevel() != ErrorLevel {
		t.Errorf("After SetLevel, level = %v, want ErrorLevel", logger.GetLevel())
	}

	// Debug and Info should not be logged
	logger.Debug("debug")
	logger.Info("info")

	if buf.Len() != 0 {
		t.Error("Expected no output for Debug/Info at ErrorLevel")
	}

	// Error should be logged
	logger.Error("error")

	if buf.Len() == 0 {
		t.Error("Expected output for Error at ErrorLevel")
	}
}

func TestDefaultLogger(t *testing.T) {
	// Just ensure it doesn't panic and returns a non-nil logger
	logger := DefaultLogger()
	if logger == nil {
		t.Fatal("DefaultLogger() returned nil")
	}

	// Verify it works
	logger.Info("test message")
}

func TestGlobalHelperFunctions(t *testing.T) {
	// Create a custom default logger for testing
	var buf bytes.Buffer
	SetDefaultLogger(NewJSONLogger(&buf, DebugLevel))

	// Test global functions
	Debug("debug msg")
	Info("info msg")
	Warn("warn msg")
	ErrorLog("error msg")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 4 {
		t.Errorf("Expected 4 log entries, got %d", len(lines))
	}

	// Verify each level
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, expectedLevel := range levels {
		var entry LogEntry
		if err := json.Unmarshal([]byte(lines[i]), &entry); err != nil {
			t.Fatalf("Failed to unmarshal entry %d: %v", i, err)
		}
		if entry.Level != expectedLevel {
			t.Errorf("Entry %d level = %v, want %v", i, entry.Level, expectedLevel)
		}
	}
}

func TestGlobalWith(t *testing.T) {
	var buf bytes.Buffer
	SetDefaultLogger(NewJSONLogger(&buf, InfoLevel))

	childLogger := With(String("service", "graphdb"))
	childLogger.Info("test")

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if entry.Fields["service"] != "graphdb" {
		t.Errorf("service field = %v, want graphdb", entry.Fields["service"])
	}
}

func TestJSONLogger_NoFieldsOmitted(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, InfoLevel)

	logger.Info("message without fields")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// When no fields are present, the fields key should be omitted
	if _, exists := entry["fields"]; exists {
		t.Error("Expected fields key to be omitted when empty")
	}
}

func BenchmarkJSONLogger_Info(b *testing.B) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, InfoLevel)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message",
			String("key1", "value1"),
			Int("key2", 42),
		)
	}
}

func BenchmarkJSONLogger_InfoFiltered(b *testing.B) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, ErrorLevel)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This should be filtered out (not logged)
		logger.Info("benchmark message",
			String("key1", "value1"),
			Int("key2", 42),
		)
	}
}

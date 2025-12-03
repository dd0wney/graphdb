package logging

import (
	"io"
	"sync"
	"time"
)

// Level represents a log level
type Level int

const (
	// DebugLevel logs are typically voluminous, and are usually disabled in production
	DebugLevel Level = iota
	// InfoLevel is the default logging priority
	InfoLevel
	// WarnLevel logs are more important than Info, but don't need individual human review
	WarnLevel
	// ErrorLevel logs are high-priority. If an application is running smoothly, it shouldn't generate any error-level logs
	ErrorLevel
)

// String returns the string representation of a log level
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel converts a string to a Level
func ParseLevel(s string) Level {
	switch s {
	case "DEBUG", "debug":
		return DebugLevel
	case "INFO", "info":
		return InfoLevel
	case "WARN", "warn", "WARNING", "warning":
		return WarnLevel
	case "ERROR", "error":
		return ErrorLevel
	default:
		return InfoLevel
	}
}

// Field represents a key-value pair for structured logging
type Field struct {
	Key   string
	Value any
}

// Logger is the interface for structured logging
type Logger interface {
	// Debug logs a debug-level message
	Debug(msg string, fields ...Field)
	// Info logs an info-level message
	Info(msg string, fields ...Field)
	// Warn logs a warning-level message
	Warn(msg string, fields ...Field)
	// Error logs an error-level message
	Error(msg string, fields ...Field)
	// With creates a child logger with the given fields pre-set
	With(fields ...Field) Logger
	// SetLevel sets the minimum log level
	SetLevel(level Level)
	// GetLevel returns the current log level
	GetLevel() Level
}

// JSONLogger implements Logger with JSON output
type JSONLogger struct {
	writer io.Writer
	level  Level
	fields []Field
	mu     sync.Mutex
}

// LogEntry represents a single log entry in JSON format
type LogEntry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// NopLogger is a logger that does nothing (useful for testing)
type NopLogger struct{}

func (NopLogger) Debug(msg string, fields ...Field) {}
func (NopLogger) Info(msg string, fields ...Field)  {}
func (NopLogger) Warn(msg string, fields ...Field)  {}
func (NopLogger) Error(msg string, fields ...Field) {}
func (n NopLogger) With(fields ...Field) Logger     { return n }
func (NopLogger) SetLevel(level Level)              {}
func (NopLogger) GetLevel() Level                   { return InfoLevel }

// NewNopLogger creates a logger that discards all output
func NewNopLogger() Logger {
	return NopLogger{}
}

// TimedOperation helps measure operation duration
type TimedOperation struct {
	logger Logger
	msg    string
	start  time.Time
	fields []Field
}

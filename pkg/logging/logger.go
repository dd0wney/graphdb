package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// NewJSONLogger creates a new JSON logger
func NewJSONLogger(writer io.Writer, level Level) *JSONLogger {
	return &JSONLogger{
		writer: writer,
		level:  level,
		fields: make([]Field, 0),
	}
}

// NewDefaultLogger creates a logger that writes to stdout at INFO level
func NewDefaultLogger() *JSONLogger {
	return NewJSONLogger(os.Stdout, InfoLevel)
}

// log is the internal logging method
func (l *JSONLogger) log(level Level, msg string, fields ...Field) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Build field map
	fieldMap := make(map[string]any)

	// Add pre-set fields
	for _, f := range l.fields {
		fieldMap[f.Key] = f.Value
	}

	// Add new fields
	for _, f := range fields {
		fieldMap[f.Key] = f.Value
	}

	entry := LogEntry{
		Time:    time.Now().Format(time.RFC3339Nano),
		Level:   level.String(),
		Message: msg,
	}

	// Only include fields if there are any
	if len(fieldMap) > 0 {
		entry.Fields = fieldMap
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple text logging if JSON marshal fails
		fmt.Fprintf(l.writer, "[ERROR] Failed to marshal log entry: %v\n", err)
		return
	}

	l.writer.Write(data)
	l.writer.Write([]byte("\n"))
}

// Debug logs a debug-level message
func (l *JSONLogger) Debug(msg string, fields ...Field) {
	l.log(DebugLevel, msg, fields...)
}

// Info logs an info-level message
func (l *JSONLogger) Info(msg string, fields ...Field) {
	l.log(InfoLevel, msg, fields...)
}

// Warn logs a warning-level message
func (l *JSONLogger) Warn(msg string, fields ...Field) {
	l.log(WarnLevel, msg, fields...)
}

// Error logs an error-level message
func (l *JSONLogger) Error(msg string, fields ...Field) {
	l.log(ErrorLevel, msg, fields...)
}

// With creates a child logger with the given fields pre-set
func (l *JSONLogger) With(fields ...Field) Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create a copy of existing fields
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)

	return &JSONLogger{
		writer: l.writer,
		level:  l.level,
		fields: newFields,
	}
}

// SetLevel sets the minimum log level
func (l *JSONLogger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *JSONLogger) GetLevel() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

// Global default logger
var (
	defaultLogger Logger
	once          sync.Once
)

// DefaultLogger returns the global default logger
func DefaultLogger() Logger {
	once.Do(func() {
		level := InfoLevel
		if levelStr := os.Getenv("LOG_LEVEL"); levelStr != "" {
			level = ParseLevel(levelStr)
		}
		defaultLogger = NewJSONLogger(os.Stdout, level)
	})
	return defaultLogger
}

// SetDefaultLogger sets the global default logger
func SetDefaultLogger(logger Logger) {
	defaultLogger = logger
}

// Helper functions that use the default logger

// Debug logs a debug-level message using the default logger
func Debug(msg string, fields ...Field) {
	DefaultLogger().Debug(msg, fields...)
}

// Info logs an info-level message using the default logger
func Info(msg string, fields ...Field) {
	DefaultLogger().Info(msg, fields...)
}

// Warn logs a warning-level message using the default logger
func Warn(msg string, fields ...Field) {
	DefaultLogger().Warn(msg, fields...)
}

// ErrorLog logs an error-level message using the default logger
// Named ErrorLog to avoid conflict with Error field constructor
func ErrorLog(msg string, fields ...Field) {
	DefaultLogger().Error(msg, fields...)
}

// With creates a child logger with the given fields pre-set using the default logger
func With(fields ...Field) Logger {
	return DefaultLogger().With(fields...)
}

// StartTimer begins timing an operation
func StartTimer(logger Logger, msg string, fields ...Field) *TimedOperation {
	return &TimedOperation{
		logger: logger,
		msg:    msg,
		start:  time.Now(),
		fields: fields,
	}
}

// End logs the operation with its duration
func (t *TimedOperation) End() {
	elapsed := time.Since(t.start)
	t.logger.Info(t.msg, append(t.fields, Latency(elapsed))...)
}

// EndWithLevel logs the operation at the specified level with its duration
func (t *TimedOperation) EndWithLevel(level Level, msg string) {
	elapsed := time.Since(t.start)
	fields := append(t.fields, Latency(elapsed))
	switch level {
	case DebugLevel:
		t.logger.Debug(msg, fields...)
	case InfoLevel:
		t.logger.Info(msg, fields...)
	case WarnLevel:
		t.logger.Warn(msg, fields...)
	case ErrorLevel:
		t.logger.Error(msg, fields...)
	}
}

// EndError logs the operation as an error with its duration
func (t *TimedOperation) EndError(err error) {
	elapsed := time.Since(t.start)
	t.logger.Error(t.msg, append(t.fields, Latency(elapsed), Error(err))...)
}

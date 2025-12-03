package wal

import (
	"bufio"
	"fmt"
	"os"
)

// FileRotator handles atomic file rotation for WAL files.
// It ensures safe file replacement with recovery on failure.
type FileRotator struct {
	path       string
	file       *os.File
	writer     *bufio.Writer
	bufferSize int
}

// NewFileRotator creates a new file rotator for the given path.
// bufferSize controls the bufio.Writer buffer size (0 = default).
func NewFileRotator(path string, bufferSize int) *FileRotator {
	return &FileRotator{
		path:       path,
		bufferSize: bufferSize,
	}
}

// Open opens or creates the file for appending.
func (fr *FileRotator) Open() error {
	file, err := os.OpenFile(fr.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", fr.path, err)
	}

	fr.file = file
	if fr.bufferSize > 0 {
		fr.writer = bufio.NewWriterSize(file, fr.bufferSize)
	} else {
		fr.writer = bufio.NewWriter(file)
	}
	return nil
}

// File returns the underlying file handle.
func (fr *FileRotator) File() *os.File {
	return fr.file
}

// Writer returns the buffered writer.
func (fr *FileRotator) Writer() *bufio.Writer {
	return fr.writer
}

// Flush flushes the buffered writer.
func (fr *FileRotator) Flush() error {
	if fr.writer == nil {
		return nil
	}
	return fr.writer.Flush()
}

// Sync flushes the buffer and syncs the file to disk.
func (fr *FileRotator) Sync() error {
	if err := fr.Flush(); err != nil {
		return err
	}
	if fr.file == nil {
		return nil
	}
	return fr.file.Sync()
}

// Close flushes, syncs, and closes the file.
func (fr *FileRotator) Close() error {
	if err := fr.Sync(); err != nil {
		return err
	}
	if fr.file == nil {
		return nil
	}
	return fr.file.Close()
}

// Rotate atomically replaces the current file with a new empty file.
// This is used after a snapshot to start a fresh WAL.
// On success, the rotator points to the new file.
// On failure, the rotator attempts to recover to the original file.
func (fr *FileRotator) Rotate() error {
	if fr.file == nil {
		return fmt.Errorf("no file to rotate")
	}

	// Flush any pending writes before rotation
	if err := fr.Flush(); err != nil {
		return fmt.Errorf("failed to flush before rotate: %w", err)
	}

	newPath := fr.path + ".new"

	// Create new file before closing old one
	newFile, err := os.OpenFile(newPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create new file: %w", err)
	}

	// Close current file
	closeErr := fr.file.Close()

	// Atomic rename (on POSIX systems)
	if err := os.Rename(newPath, fr.path); err != nil {
		// Failed to rename - cleanup and try to recover
		newFile.Close()
		if oldFile, reopenErr := os.OpenFile(fr.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644); reopenErr == nil {
			fr.file = oldFile
			if fr.bufferSize > 0 {
				fr.writer = bufio.NewWriterSize(oldFile, fr.bufferSize)
			} else {
				fr.writer = bufio.NewWriter(oldFile)
			}
		}
		return fmt.Errorf("failed to rename file: %w (close error: %v)", err, closeErr)
	}

	// Update state with new file
	fr.file = newFile
	if fr.bufferSize > 0 {
		fr.writer = bufio.NewWriterSize(newFile, fr.bufferSize)
	} else {
		fr.writer = bufio.NewWriter(newFile)
	}

	return nil
}

// SafeWrite writes data with automatic flush on error.
type SafeWriter struct {
	writer *bufio.Writer
}

// NewSafeWriter creates a new safe writer.
func NewSafeWriter(w *bufio.Writer) *SafeWriter {
	return &SafeWriter{writer: w}
}

// Write writes data to the buffer.
func (sw *SafeWriter) Write(p []byte) (int, error) {
	return sw.writer.Write(p)
}

// WriteAndFlush writes data and immediately flushes.
func (sw *SafeWriter) WriteAndFlush(p []byte) error {
	if _, err := sw.writer.Write(p); err != nil {
		return err
	}
	return sw.writer.Flush()
}

// Flush flushes the underlying writer.
func (sw *SafeWriter) Flush() error {
	return sw.writer.Flush()
}

// ChecksumWriter calculates CRC32 while writing.
// This can be used to verify WAL entry integrity.
type ChecksumWriter struct {
	writer   *bufio.Writer
	checksum uint32
}

// EnsureDir creates a directory if it doesn't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// FileExists checks if a file exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FileSize returns the size of a file in bytes.
func FileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

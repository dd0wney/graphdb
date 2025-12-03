package wal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileRotator_Open(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fr := NewFileRotator(path, 0)
	if err := fr.Open(); err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	defer fr.Close()

	if fr.File() == nil {
		t.Error("File() returned nil after Open()")
	}
	if fr.Writer() == nil {
		t.Error("Writer() returned nil after Open()")
	}
}

func TestFileRotator_WriteAndFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fr := NewFileRotator(path, 0)
	if err := fr.Open(); err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	defer fr.Close()

	data := []byte("test data")
	if _, err := fr.Writer().Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := fr.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify data was written
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != "test data" {
		t.Errorf("Content = %q, want %q", string(content), "test data")
	}
}

func TestFileRotator_Rotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fr := NewFileRotator(path, 0)
	if err := fr.Open(); err != nil {
		t.Fatalf("Open() failed: %v", err)
	}

	// Write some data before rotation
	if _, err := fr.Writer().Write([]byte("before rotation")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := fr.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify data exists before rotation
	beforeSize, _ := FileSize(path)
	if beforeSize == 0 {
		t.Error("Expected non-zero file size before rotation")
	}

	// Rotate
	if err := fr.Rotate(); err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}

	// Verify file is now empty
	afterSize, _ := FileSize(path)
	if afterSize != 0 {
		t.Errorf("Expected file size 0 after rotation, got %d", afterSize)
	}

	// Write new data after rotation
	if _, err := fr.Writer().Write([]byte("after rotation")); err != nil {
		t.Fatalf("Write after rotation failed: %v", err)
	}
	if err := fr.Flush(); err != nil {
		t.Fatalf("Flush after rotation failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after rotation failed: %v", err)
	}
	if string(content) != "after rotation" {
		t.Errorf("Content = %q, want %q", string(content), "after rotation")
	}

	fr.Close()
}

func TestFileRotator_WithBufferSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	fr := NewFileRotator(path, 1024)
	if err := fr.Open(); err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	defer fr.Close()

	// Write data smaller than buffer - should not appear on disk yet
	smallData := []byte("small")
	if _, err := fr.Writer().Write(smallData); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Check file size before flush (may be 0 due to buffering)
	preFlushSize, _ := FileSize(path)

	// Flush and verify
	if err := fr.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	postFlushSize, _ := FileSize(path)
	if postFlushSize <= preFlushSize {
		t.Error("Expected file size to increase after flush")
	}
}

func TestSafeWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	file, _ := os.Create(path)
	defer file.Close()

	fr := NewFileRotator(path, 0)
	if err := fr.Open(); err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	defer fr.Close()

	sw := NewSafeWriter(fr.Writer())

	// Test WriteAndFlush
	if err := sw.WriteAndFlush([]byte("safe write")); err != nil {
		t.Fatalf("WriteAndFlush failed: %v", err)
	}

	content, _ := os.ReadFile(path)
	if string(content) != "safe write" {
		t.Errorf("Content = %q, want %q", string(content), "safe write")
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	newDir := filepath.Join(dir, "nested", "path")

	if err := EnsureDir(newDir); err != nil {
		t.Fatalf("EnsureDir() failed: %v", err)
	}

	if !FileExists(newDir) {
		t.Error("Directory should exist after EnsureDir()")
	}

	// Calling again should not error
	if err := EnsureDir(newDir); err != nil {
		t.Fatalf("EnsureDir() failed on existing dir: %v", err)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()

	// Non-existent file
	if FileExists(filepath.Join(dir, "nonexistent")) {
		t.Error("FileExists should return false for non-existent file")
	}

	// Create a file
	path := filepath.Join(dir, "exists.txt")
	os.WriteFile(path, []byte("test"), 0644)

	if !FileExists(path) {
		t.Error("FileExists should return true for existing file")
	}
}

func TestFileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sized.txt")

	// Write known data
	data := []byte("1234567890")
	os.WriteFile(path, data, 0644)

	size, err := FileSize(path)
	if err != nil {
		t.Fatalf("FileSize() failed: %v", err)
	}
	if size != int64(len(data)) {
		t.Errorf("FileSize() = %d, want %d", size, len(data))
	}
}

func TestFileSize_NonExistent(t *testing.T) {
	_, err := FileSize("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("FileSize should return error for non-existent file")
	}
}

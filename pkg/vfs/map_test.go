package vfs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The OS driver maps a file, and the bytes are the file's bytes.
func TestMapFile_OSDriverReturnsFileContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.bin")
	want := []byte("GMNP\x04\x00\x00\x00payload")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, release, err := MapFile(OS(), path)
	if err != nil {
		t.Fatalf("MapFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("mapped %q, want %q", got, want)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// mapperFS is a driver that implements Mapper and serves its own bytes. This is
// the case the whole capability exists for: a fault driver hands the reader a
// deliberately corrupt snapshot without writing one to disk.
type mapperFS struct {
	FileSystem
	payload  []byte
	released bool
}

func (m *mapperFS) Map(string) ([]byte, func() error, error) {
	return m.payload, func() error { m.released = true; return nil }, nil
}

func TestMapFile_PrefersTheDriversMapper(t *testing.T) {
	corrupt := []byte("not a snapshot at all")
	fs := &mapperFS{FileSystem: OS(), payload: corrupt}

	got, release, err := MapFile(fs, "/nonexistent/path/it/never/opens")
	if err != nil {
		t.Fatalf("MapFile: %v", err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatalf("mapped %q, want the driver's payload %q", got, corrupt)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !fs.released {
		t.Fatal("release did not reach the driver's own release function")
	}
}

// plainFS implements FileSystem and NOT Mapper. A driver written before this
// capability existed must keep working.
type plainFS struct{ FileSystem }

func (plainFS) Name() string { return "plain" }

func TestMapFile_FallsBackWhenTheDriverCannotMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.bin")
	want := []byte("read through Open, not mapped")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, release, err := MapFile(plainFS{FileSystem: OS()}, path)
	if err != nil {
		t.Fatalf("MapFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("read %q, want %q", got, want)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// A missing file is an error from either path, not an empty slice.
func TestMapFile_MissingFileIsAnError(t *testing.T) {
	_, _, err := MapFile(OS(), filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("MapFile on a missing file returned no error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error does not wrap os.ErrNotExist: %v", err)
	}
}

package lsm

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func buildSSTable(t *testing.T, dir string, n int) *SSTable {
	t.Helper()
	entries := make([]*Entry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, &Entry{
			Key:       []byte(fmt.Sprintf("key%05d", i)),
			Value:     []byte("value"),
			Timestamp: time.Now().UnixNano(),
		})
	}
	sst, err := NewSSTable(filepath.Join(dir, "L0-000001.sst"), entries)
	if err != nil {
		t.Fatalf("NewSSTable: %v", err)
	}
	t.Cleanup(func() { _ = sst.Close() })
	return sst
}

// allocatedBy reports the bytes allocated while f runs. TotalAlloc is
// cumulative, so this is a total rather than a live-heap reading and does not
// depend on when the collector runs.
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A scan must stop at the end of the entry section.
//
// The header records IndexOffset, and everything after it is the sparse index
// and the bloom filter. ScanEntries read to end of file instead, so it fed the
// index and filter bytes to readEntry as if they were entries. readEntry took
// whatever four bytes it found next as a length and allocated that many, so a
// fifty-entry scan allocated 1.8 GB. Nothing stopped a byte sequence that
// happens to parse from being returned as an entry either.
func TestScanStopsAtTheEntrySection(t *testing.T) {
	const n = 50
	sst := buildSSTable(t, t.TempDir(), n)

	var got []*Entry
	var err error
	allocated := allocatedBy(func() {
		got, err = sst.ScanEntries([]byte(""), []byte("\xff"))
	})
	if err != nil {
		t.Fatalf("ScanEntries: %v", err)
	}

	if len(got) != n {
		t.Errorf("scan returned %d entries for a table holding %d", len(got), n)
	}
	// The real data is about 1.5 kB. Four megabytes is a wide margin over any
	// reasonable buffering and three orders below the defect.
	if allocated > 4<<20 {
		t.Errorf("a %d-entry scan allocated %d bytes", n, allocated)
	}
}

// A corrupt length prefix must be refused, not allocated.
//
// readEntry took a uint32 from the file and passed it straight to make(), so a
// corrupt or truncated SSTable could ask for up to 4 GiB per field. This is
// the defect #477 fixed for the mmap snapshot decoders; the SSTable decoder
// has it too and was not covered.
func TestReadEntryRefusesAnAbsurdLength(t *testing.T) {
	dir := t.TempDir()
	sst := buildSSTable(t, dir, 8)
	path := sst.path
	if err := sst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Overwrite the first entry's key-length prefix with 128 MiB: far above any
	// legitimate field, and small enough that the unfixed code survives to be
	// measured rather than exhausting the machine.
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	headerSize := int64(binary.Size(SSTableHeader{}))
	if err := binary.Write(&offsetWriter{f: f, off: headerSize}, binary.LittleEndian, uint32(128<<20)); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := OpenSSTable(path)
	if err != nil {
		// Refusing to open a corrupt table is an acceptable outcome.
		return
	}
	defer reopened.Close()

	allocated := allocatedBy(func() {
		_, _ = reopened.ScanEntries([]byte(""), []byte("\xff"))
	})
	if allocated > 4<<20 {
		t.Errorf("a corrupt length prefix caused %d bytes to be allocated", allocated)
	}
}

// offsetWriter writes at a fixed offset, so the test can corrupt one field
// without rewriting the file.
type offsetWriter struct {
	f   *os.File
	off int64
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	return n, err
}

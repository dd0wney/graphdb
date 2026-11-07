package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// The compressed WAL snappy-encodes each record, and snappy turns a run of 4
// or more bytes it has already seen into a back-reference. A marker whose
// first bytes repeat earlier in the same record is then not on disk as one
// run, so a raw byte search cannot see it. That made the plaintext control of
// TestEncryptedStore_WritesNoPlaintext fail in about 3% of runs, with every
// missing marker present in the decompressed record, and it hides a leak from
// the encrypted-store search in the same way.
//
// The record built here forces the split, so the search must look inside the
// decompressed records to pass.
func TestFindNoPlaintextMarkers_SeesThroughWALCompression(t *testing.T) {
	dir := t.TempDir()
	marker := "m3f9a1c07d2e84b6590aa17c3e5d2b8f"

	cw, err := wal.NewCompressedWAL(filepath.Join(dir, "wal"))
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"first":"` + marker[:12] + `","second":"` + marker + `"}`)
	if _, err := cw.Append(wal.OpCreateNode, record); err != nil {
		t.Fatal(err)
	}
	if err := cw.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "wal", "wal_compressed.log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(marker)) {
		t.Fatal("precondition: snappy kept the marker as one run, so this record does not test the split")
	}

	hits := findNoPlaintextMarkers(t, dir, noPlaintextMarkers{"k": marker})
	if len(hits) == 0 {
		t.Fatal("the search cannot see a marker that is in a compressed WAL record but split by snappy")
	}
}

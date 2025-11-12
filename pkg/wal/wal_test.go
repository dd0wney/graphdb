package wal

import (
	"testing"
)

func TestWAL_AppendAndRead(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	// Append entries
	data1 := []byte("test data 1")
	lsn1, err := w.Append(OpCreateNode, data1)
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	if lsn1 != 1 {
		t.Errorf("Expected LSN 1, got %d", lsn1)
	}

	data2 := []byte("test data 2")
	lsn2, err := w.Append(OpCreateEdge, data2)
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	if lsn2 != 2 {
		t.Errorf("Expected LSN 2, got %d", lsn2)
	}

	// Read all entries
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read entries: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Verify first entry
	if string(entries[0].Data) != "test data 1" {
		t.Errorf("Expected 'test data 1', got '%s'", string(entries[0].Data))
	}

	if entries[0].OpType != OpCreateNode {
		t.Errorf("Expected OpCreateNode, got %d", entries[0].OpType)
	}

	// Verify second entry
	if string(entries[1].Data) != "test data 2" {
		t.Errorf("Expected 'test data 2', got '%s'", string(entries[1].Data))
	}

	if entries[1].OpType != OpCreateEdge {
		t.Errorf("Expected OpCreateEdge, got %d", entries[1].OpType)
	}
}

func TestWAL_Replay(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Append some entries
	w.Append(OpCreateNode, []byte("node1"))
	w.Append(OpCreateEdge, []byte("edge1"))
	w.Append(OpCreateNode, []byte("node2"))

	// Replay entries
	replayed := make([]string, 0)
	err = w.Replay(func(entry *Entry) error {
		replayed = append(replayed, string(entry.Data))
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if len(replayed) != 3 {
		t.Fatalf("Expected 3 replayed entries, got %d", len(replayed))
	}

	expected := []string{"node1", "edge1", "node2"}
	for i, exp := range expected {
		if replayed[i] != exp {
			t.Errorf("Entry %d: expected '%s', got '%s'", i, exp, replayed[i])
		}
	}

	w.Close()
}

func TestWAL_Truncate(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Append entries
	w.Append(OpCreateNode, []byte("node1"))
	w.Append(OpCreateEdge, []byte("edge1"))

	// Truncate
	err = w.Truncate()
	if err != nil {
		t.Fatalf("Failed to truncate: %v", err)
	}

	// Verify WAL is empty
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read after truncate: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Expected 0 entries after truncate, got %d", len(entries))
	}

	// Verify LSN reset
	if w.GetCurrentLSN() != 0 {
		t.Errorf("Expected LSN 0 after truncate, got %d", w.GetCurrentLSN())
	}

	w.Close()
}

func TestWAL_Persistence(t *testing.T) {
	dataDir := t.TempDir()

	// Create WAL and append entries
	w1, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	w1.Append(OpCreateNode, []byte("persisted node"))
	w1.Append(OpCreateEdge, []byte("persisted edge"))
	w1.Close()

	// Reopen WAL
	w2, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	// Verify entries persisted
	entries, err := w2.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read entries: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 persisted entries, got %d", len(entries))
	}

	if string(entries[0].Data) != "persisted node" {
		t.Errorf("Entry 0 not persisted correctly")
	}

	// Verify LSN recovered
	if w2.GetCurrentLSN() != 2 {
		t.Errorf("Expected LSN 2 after recovery, got %d", w2.GetCurrentLSN())
	}
}

func BenchmarkWAL_Append(b *testing.B) {
	dataDir := b.TempDir()
	w, _ := NewWAL(dataDir)
	defer w.Close()

	data := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Append(OpCreateNode, data)
	}
}

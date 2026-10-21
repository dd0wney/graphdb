package storage

import "testing"

// Snapshot cost on a populated store, one benchmark per snapshot format. The
// run time covers the capture under gs.mu.RLock and the encode and write after
// it, so a change that moves work into the lock shows as a change here only
// together with the write. Set TMPDIR to a disk-backed directory: b.TempDir
// on tmpfs measures RAM, not the fsync the snapshot does.
const (
	snapshotBenchNodes = 50_000
	snapshotBenchEdges = 100_000
)

func benchSnapshot(b *testing.B, cfg StorageConfig) {
	// No WAL, only to make the load fast: 150k per-record fsyncs take
	// minutes. The snapshot encode and write do not touch the WAL; the
	// boundary read under the lock is a constant-time counter read.
	cfg.BulkImportMode = true
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		b.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer gs.Close()

	batch := gs.BeginBatch()
	ids := make([]uint64, snapshotBenchNodes)
	for i := range ids {
		id, err := batch.AddNode([]string{"B"}, map[string]Value{"i": IntValue(int64(i))})
		if err != nil {
			b.Fatalf("AddNode: %v", err)
		}
		ids[i] = id
	}
	for i := 0; i < snapshotBenchEdges; i++ {
		from, to := ids[i%snapshotBenchNodes], ids[(i*7+1)%snapshotBenchNodes]
		if _, err := batch.AddEdge(from, to, "E", nil, 1.0); err != nil {
			b.Fatalf("AddEdge: %v", err)
		}
	}
	if err := batch.Commit(); err != nil {
		b.Fatalf("Commit: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := gs.Snapshot(); err != nil {
			b.Fatalf("Snapshot: %v", err)
		}
	}
}

func BenchmarkSnapshot_JSON(b *testing.B) {
	benchSnapshot(b, jsonConfig(b.TempDir()))
}

func BenchmarkSnapshot_Mmap(b *testing.B) {
	benchSnapshot(b, mmapConfig(b.TempDir()))
}

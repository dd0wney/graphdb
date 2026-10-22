package query

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/storage/storagetest"
)

// drainStream reads s to its end and returns the IDs it streamed, in order,
// and the error it ended with (nil at a clean end).
func drainStream(t *testing.T, s *ResultStream) ([]uint64, error) {
	t.Helper()
	var ids []uint64
	for {
		node, err := s.Next()
		if err != nil {
			return ids, err
		}
		if node == nil {
			return ids, nil
		}
		ids = append(ids, node.ID)
	}
}

// StreamNodes scanned IDs 1..NodeCount, so a delete that left a gap below the
// highest ID hid the nodes above the live count.
func TestStreamNodes_AfterDeleteStreamsEveryLiveNodeInIDOrder(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })

	var ids []uint64
	for i := 0; i < 3; i++ {
		n, err := graph.CreateNode([]string{"Person"}, nil)
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		ids = append(ids, n.ID)
	}
	if err := graph.DeleteNode(ids[0]); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}

	got, err := drainStream(t, NewStreamingQuery(graph).StreamNodes(nil))
	if err != nil {
		t.Fatalf("stream ended with %v", err)
	}
	want := ids[1:]
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("streamed %v, want %v", got, want)
	}
}

// A record that does not decode must end the stream with the error. Skipping
// it streamed a short result that looked complete.
func TestStreamNodes_UnreadableRecordEndsTheStreamWithTheError(t *testing.T) {
	dir := t.TempDir()
	cfg := storage.DefaultStorageConfig(dir)
	cfg.UseMmapSnapshot = true // the JSON path has no record to damage

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var ids []uint64
	for i := 0; i < 3; i++ {
		n, err := gs.CreateNode([]string{"Thing"}, map[string]storage.Value{"i": storage.IntValue(int64(i))})
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		ids = append(ids, n.ID)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	damaged := ids[1]
	storagetest.DamageNodeRecord(t, dir, damaged)

	reopened, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	// Positive control: the damage took, so a clean stream below is a defect
	// and not a fault injector that stopped working.
	if _, err := reopened.GetNode(damaged); !errors.Is(err, storage.ErrRecordUnreadable) {
		t.Fatalf("GetNode(%d) = %v, want ErrRecordUnreadable: the damage did not take", damaged, err)
	}

	got, err := drainStream(t, NewStreamingQuery(reopened).StreamNodes(nil))
	if !errors.Is(err, storage.ErrRecordUnreadable) {
		t.Fatalf("stream streamed %v and ended with %v, want ErrRecordUnreadable", got, err)
	}
}

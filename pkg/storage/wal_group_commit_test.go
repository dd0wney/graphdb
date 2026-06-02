package storage

import (
	"fmt"
	"testing"
	"time"
)

// Track P item (1) — the batched WAL must release gs.mu during the durability
// wait so concurrent writers can fill one batch (group commit).
//
// Before the fix, CreateNodeWithTenant holds gs.mu for the whole of
// createNodeLocked, including BatchedWAL.Append parking on its done-channel. A
// second writer therefore cannot acquire gs.mu to enqueue, the batch never
// reaches batchSize, and both writes serialize behind the flush ticker.
//
// This is a deterministic STRUCTURAL test (the functional WAL behaviour is
// already correct on current code). batchSize=2 with a long flushInterval: two
// concurrent creates can only complete quickly if the batch fills — which can
// only happen if the first writer releases gs.mu before waiting on durability.
// On the pre-fix code the batch cannot fill and the writes block until the
// 10s ticker fires, so the 3s deadline trips.
func TestBatchedWAL_ConcurrentWritesFillBatchWithoutHoldingGlobalLock(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:        t.TempDir(),
		EnableBatching: true,
		BatchSize:      2,
		FlushInterval:  10 * time.Second, // long: the ticker must NOT be what unblocks us
	})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer gs.Close()

	const writers = 2 // == batchSize: the batch fills iff both enqueue before either blocks
	done := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func(n int) {
			_, cerr := gs.CreateNodeWithTenant("t0", []string{"Doc"},
				map[string]Value{"name": StringValue(fmt.Sprintf("n%d", n))})
			done <- cerr
		}(i)
	}

	// After the fix the batch fills and flushes immediately (~ms). Before the
	// fix the writes serialize behind the 10s ticker. 3s discriminates cleanly.
	deadline := time.After(3 * time.Second)
	for got := 0; got < writers; got++ {
		select {
		case cerr := <-done:
			if cerr != nil {
				t.Fatalf("CreateNodeWithTenant: %v", cerr)
			}
		case <-deadline:
			t.Fatalf("only %d/%d concurrent batched writes completed in 3s — "+
				"writers are serialized under gs.mu (the batch cannot fill)", got, writers)
		}
	}
}

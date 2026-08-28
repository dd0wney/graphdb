package wal

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
	"github.com/dd0wney/graphdb/pkg/alloc/alloctest"
)

// SQLite runs two out-of-memory loops, not one:
//
//	Rig the allocator to fail once on the N-th allocation for N=1,2,3,....
//	Repeat until no memory allocations fail.
//
//	Rig the allocator to fail all allocations beginning with the N-th, for
//	N=1,2,3,.... Repeat until no memory allocations fail.
//
// The second is not a variation on the first. Fail-once finds a handler that
// copes with one refusal and continues. Fail-all-from finds the handler that
// only works because a LATER allocation succeeded — a cleanup path that
// allocates in order to clean up is the classic example, and it passes the
// first loop while being broken.
//
// What is gated here is graphdb's own length-driven buffer in readEntry, sized
// from the record header. Go's implicit allocations are untouched; see
// pkg/alloc for what this can and cannot claim.

// seedWAL writes n entries with the real allocator and returns the directory.
func seedWAL(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()

	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	for i := 0; i < n; i++ {
		if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

// recoveryInvariant is the contract under either loop. A refused allocation is
// allowed to cut recovery short — it is a resource failure, not corruption —
// but what it returns must still be a contiguous prefix, and it must never
// invent an entry or panic.
func recoveryInvariant(t *testing.T, dir string, n, written int, mode alloctest.Mode) {
	t.Helper()

	w, err := NewWAL(dir)
	if err != nil {
		return // refusing to open under memory pressure is acceptable
	}
	entries, err := w.ReadAll()
	_ = w.Close()
	if err != nil {
		return
	}

	if len(entries) > written {
		t.Fatalf("%s N=%d: recovery returned %d entries, more than the %d written",
			mode, n, len(entries), written)
	}
	for i, e := range entries {
		if e.LSN != uint64(i+1) {
			t.Fatalf("%s N=%d: entry %d has LSN %d, want %d — not a prefix",
				mode, n, i, e.LSN, i+1)
		}
	}
}

func TestWAL_OOMSweep_FailOnce(t *testing.T) {
	const written = 4
	dir := seedWAL(t, written)

	var reopens int
	run := func() error {
		w, err := NewWAL(dir)
		if err != nil {
			return err
		}
		reopens++
		_, err = w.ReadAll()
		_ = w.Close()
		return err
	}

	alloctest.Sweep(t, alloctest.FailOnce, 128, run, func(t *testing.T, n int, runErr error) {
		recoveryInvariant(t, dir, n, written, alloctest.FailOnce)
	})

	if reopens == 0 {
		t.Error("the scenario never reopened the WAL; the sweep proved nothing")
	}
	t.Logf("fail-once sweep completed over %d reopen attempts", reopens)
}

// TestWAL_OOMSweep_FailAllFrom is the loop that finds handlers depending on a
// later allocation succeeding.
func TestWAL_OOMSweep_FailAllFrom(t *testing.T) {
	const written = 4
	dir := seedWAL(t, written)

	run := func() error {
		w, err := NewWAL(dir)
		if err != nil {
			return err
		}
		_, err = w.ReadAll()
		_ = w.Close()
		return err
	}

	alloctest.Sweep(t, alloctest.FailAllFrom, 128, run, func(t *testing.T, n int, runErr error) {
		recoveryInvariant(t, dir, n, written, alloctest.FailAllFrom)
	})
}

// TestWAL_OOMDuringOpenMustNotReuseLSNs is the regression test for the defect
// the fail-once sweep found.
//
// Refusing allocation 3 while opening a 5-entry WAL used to leave
// currentLSN=2. The next Append then took LSN 3 — an LSN already on disk — and
// after the non-advancing-LSN guard added in #478, that new record is silently
// dropped by the NEXT recovery. An allocation failure at startup therefore
// caused silent data loss later.
//
// The cause was conflating two reasons to stop reading. A torn tail means the
// data ended and the last good entry IS the end of the log. A refused
// allocation means we could not read, and says nothing about what follows. Now
// only the first is treated as a clean end; the second fails the open.
func TestWAL_OOMDuringOpenMustNotReuseLSNs(t *testing.T) {
	const written = 5
	dir := seedWAL(t, written)

	alloc.Install(alloctest.New(alloctest.FailOnce, 3))
	w, err := NewWAL(dir)
	refused := alloc.Refused()
	allocs := alloc.Allocs()
	alloc.Reset()

	// Negative controls: the allocator must have been reached and must have
	// refused, or this test is about nothing.
	if allocs == 0 {
		t.Fatal("no gated allocation was attempted during open")
	}
	if refused == 0 {
		t.Fatal("the allocator refused nothing; the failure point was never reached")
	}

	if err != nil {
		// The correct outcome: recovery could not complete, so the WAL does
		// not open rather than opening with a truncated LSN.
		return
	}

	// If it did open, it must at least not reuse an LSN.
	defer func() { _ = w.Close() }()
	lsn, aerr := w.Append(OpCreateNode, []byte("after"))
	if aerr != nil {
		t.Fatalf("Append: %v", aerr)
	}
	if lsn <= uint64(written) {
		t.Errorf("LSN reuse: append got %d with %d entries already on disk", lsn, written)
	}
}

package alloctest

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
)

// The allocator is an instrument. Each test here checks that it refuses when it
// should and, just as importantly, that it does not when it should not.

func TestDefault_IsTransparent(t *testing.T) {
	alloc.Reset()
	buf, err := alloc.Bytes(16)
	if err != nil {
		t.Fatalf("default allocator refused: %v", err)
	}
	if len(buf) != 16 {
		t.Errorf("len = %d, want 16", len(buf))
	}
	if alloc.Allocs() != 0 {
		t.Errorf("Allocs = %d, want 0 — the production path must not count", alloc.Allocs())
	}
}

func TestFailOnce_RefusesExactlyOne(t *testing.T) {
	alloc.Install(New(FailOnce, 2))
	defer alloc.Reset()

	if _, err := alloc.Bytes(8); err != nil {
		t.Fatalf("allocation 1 refused: %v", err)
	}
	if _, err := alloc.Bytes(8); !errors.Is(err, alloc.ErrNoMemory) {
		t.Fatalf("allocation 2 err = %v, want ErrNoMemory", err)
	}
	if _, err := alloc.Bytes(8); err != nil {
		t.Fatalf("allocation 3 refused, but FailOnce should have disarmed: %v", err)
	}
	if alloc.Refused() != 1 {
		t.Errorf("Refused = %d, want 1", alloc.Refused())
	}
	if alloc.Allocs() != 3 {
		t.Errorf("Allocs = %d, want 3", alloc.Allocs())
	}
}

// TestFailAllFrom_RefusesEverythingAfter pins the difference between the two
// loops. If this behaved like FailOnce the second SQLite loop would be a
// duplicate of the first, and the handlers it exists to catch — the ones that
// need a LATER allocation to succeed — would go untested.
func TestFailAllFrom_RefusesEverythingAfter(t *testing.T) {
	alloc.Install(New(FailAllFrom, 2))
	defer alloc.Reset()

	if _, err := alloc.Bytes(8); err != nil {
		t.Fatalf("allocation 1 refused: %v", err)
	}
	for i := 2; i <= 4; i++ {
		if _, err := alloc.Bytes(8); !errors.Is(err, alloc.ErrNoMemory) {
			t.Fatalf("allocation %d err = %v, want ErrNoMemory", i, err)
		}
	}
	if alloc.Refused() != 3 {
		t.Errorf("Refused = %d, want 3", alloc.Refused())
	}
}

func TestResetRestoresTheDefault(t *testing.T) {
	alloc.Install(New(FailAllFrom, 1))
	if _, err := alloc.Bytes(8); err == nil {
		t.Fatal("the installed allocator did not refuse")
	}
	alloc.Reset()
	if _, err := alloc.Bytes(8); err != nil {
		t.Fatalf("allocation refused after Reset: %v", err)
	}
}

// TestSweep_TerminatesAndCoversEveryAllocation checks the loop itself: it must
// walk every allocation the scenario makes and stop once past the end.
func TestSweep_TerminatesAndCoversEveryAllocation(t *testing.T) {
	const allocations = 5

	var deepest, runs int
	run := func() error {
		for i := 0; i < allocations; i++ {
			if _, err := alloc.Bytes(4); err != nil {
				return err
			}
		}
		return nil
	}

	Sweep(t, FailOnce, 64, run, func(t *testing.T, n int, runErr error) {
		runs++
		if n > deepest {
			deepest = n
		}
	})

	// The sweep must reach one past the last allocation: that is the run where
	// nothing is refused and the loop ends.
	if deepest != allocations+1 {
		t.Errorf("sweep reached N=%d, want %d (one past the %d allocations)",
			deepest, allocations+1, allocations)
	}
	if runs != allocations+1 {
		t.Errorf("sweep made %d runs, want %d", runs, allocations+1)
	}
}

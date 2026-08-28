package alloc_test

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
)

// refusing fails every request past a size. It is deliberately not
// alloctest's: this file is about pkg/alloc's own entry point, and a test that
// went through the fault package would be testing that instead.
type refusing struct{ limit int }

func (r refusing) Bytes(n int) ([]byte, error) {
	if n > r.limit {
		return nil, alloc.ErrNoMemory
	}
	return make([]byte, n), nil
}
func (r refusing) Name() string { return "refusing" }

// The production path is a plain make, and it must stay one.
//
// pkg/alloc had no test at all until the coupling-coverage measure reported
// Bytes at 0 of 20 statements, joint-last with pkg/vfs.Resolve. Both are D6,
// the row whose evidence column read "the drivers' own tests" — the drivers
// were tested and the entry points they are installed into were not.
func TestBytesWithNoAllocatorInstalled(t *testing.T) {
	alloc.Reset()

	buf, err := alloc.Bytes(64)
	if err != nil {
		t.Fatalf("Bytes with no allocator installed gave an error: %v", err)
	}
	if len(buf) != 64 {
		t.Errorf("Bytes(64) returned %d bytes", len(buf))
	}
	// The package promises callers pay nothing for a feature they do not use.
	// Counting is the observable part of that.
	if got := alloc.Allocs(); got != 0 {
		t.Errorf("the production path counted %d allocations", got)
	}
}

func TestBytesUsesTheInstalledAllocator(t *testing.T) {
	alloc.Install(refusing{limit: 1024})
	t.Cleanup(alloc.Reset)

	buf, err := alloc.Bytes(16)
	if err != nil {
		t.Fatalf("Bytes(16) under a limit of 1024: %v", err)
	}
	if len(buf) != 16 {
		t.Errorf("Bytes(16) returned %d bytes", len(buf))
	}
	if got := alloc.Allocs(); got != 1 {
		t.Errorf("Allocs() = %d, want 1", got)
	}
	if got := alloc.Refused(); got != 0 {
		t.Errorf("Refused() = %d for a request within the limit", got)
	}
}

// The error path, with the negative control the package's own documentation
// asks for: seeing the expected error proves nothing unless an allocation was
// actually attempted, because the same error can arrive from elsewhere.
func TestBytesReturnsTheAllocatorsRefusal(t *testing.T) {
	alloc.Install(refusing{limit: 8})
	t.Cleanup(alloc.Reset)

	buf, err := alloc.Bytes(4096)
	if !errors.Is(err, alloc.ErrNoMemory) {
		t.Fatalf("Bytes(4096) over a limit of 8 gave %v, want ErrNoMemory", err)
	}
	if buf != nil {
		t.Errorf("Bytes returned %d bytes alongside its error", len(buf))
	}
	if got := alloc.Allocs(); got != 1 {
		t.Errorf("Allocs() = %d: the error did not come from an attempted allocation", got)
	}
	if got := alloc.Refused(); got != 1 {
		t.Errorf("Refused() = %d, want 1", got)
	}
}

// Install(nil) is the documented way back, and Reset is its name. A test that
// installs an allocator and does not undo it changes every later test in the
// same binary.
func TestResetRestoresTheProductionPath(t *testing.T) {
	alloc.Install(refusing{limit: 0})
	if _, err := alloc.Bytes(1); err == nil {
		t.Fatal("the refusing allocator was not installed, so this proves nothing")
	}

	alloc.Reset()

	if _, err := alloc.Bytes(1 << 20); err != nil {
		t.Errorf("Bytes after Reset: %v", err)
	}
	if got := alloc.Allocs(); got != 0 {
		t.Errorf("Reset left the counter at %d", got)
	}
}

// Install replaces rather than stacks, and it clears the counters as it goes.
func TestInstallReplacesAndClearsTheCounters(t *testing.T) {
	alloc.Install(refusing{limit: 0})
	t.Cleanup(alloc.Reset)
	_, _ = alloc.Bytes(1)
	if alloc.Refused() != 1 {
		t.Fatalf("Refused() = %d after one refusal", alloc.Refused())
	}

	alloc.Install(refusing{limit: 4096})
	if got := alloc.Allocs(); got != 0 {
		t.Errorf("Install left Allocs at %d", got)
	}
	if got := alloc.Refused(); got != 0 {
		t.Errorf("Install left Refused at %d", got)
	}
	if _, err := alloc.Bytes(16); err != nil {
		t.Errorf("the replacement allocator refused a request inside its limit: %v", err)
	}
}

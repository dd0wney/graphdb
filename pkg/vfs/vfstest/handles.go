package vfstest

import (
	"os"
	"sort"
	"sync"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Handles records which file handles a driver has handed out and not taken
// back, by name.
//
// Wrap it around any other driver, including a FaultFS, to ask "did the code
// under test release everything it opened".
//
// # Why a tracker and not a count of operations
//
// Counting "open" and "close" in a trace is the obvious instrument and it is
// wrong, in the direction that invents defects. An Open that FAILS is a traced
// operation that produces no handle, and therefore never produces a Close, so
// any code that probes for an absent file reads as a leak. That instrument
// reported an imbalance against already-correct code in pkg/storage before this
// type existed.
//
// # Why names and not a count
//
// A count can fail a sweep and cannot help fix one. "Four handles leaked" sends
// the reader back to the code to guess which; "wal.log, four times" names the
// defect. The github.com/dd0wney/fault session reached the same conclusion in
// its own library on the same day, after hand-writing the name tracking twice.
type Handles struct {
	vfs.FileSystem

	mu     sync.Mutex
	open   map[*trackedFile]string
	opened int
}

type trackedFile struct {
	vfs.File
	owner *Handles
}

// NewHandles wraps base with handle tracking.
func NewHandles(base vfs.FileSystem) *Handles {
	return &Handles{FileSystem: base, open: make(map[*trackedFile]string)}
}

func (h *Handles) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f, err := h.FileSystem.Open(name, flag, perm)
	if err != nil {
		// No handle exists, so there is nothing to track and nothing to leak.
		return nil, err
	}
	tf := &trackedFile{File: f, owner: h}
	h.mu.Lock()
	h.open[tf] = name
	h.opened++
	h.mu.Unlock()
	return tf, nil
}

// Close stops tracking the handle even when the underlying Close reports an
// error. The descriptor is released either way, which is the same reason
// CrashFS releases it: a driver that held one back would make every test using
// it leak, and the leak would be blamed on the code under test.
func (f *trackedFile) Close() error {
	f.owner.mu.Lock()
	delete(f.owner.open, f)
	f.owner.mu.Unlock()
	return f.File.Close()
}

// Outstanding names every handle opened and not closed, sorted. One entry per
// handle, so the same path appears as many times as it is held.
func (h *Handles) Outstanding() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	names := make([]string, 0, len(h.open))
	for _, n := range h.open {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Opened reports how many handles were ever handed out.
//
// This is the control for every assertion on Outstanding. An empty Outstanding
// means nothing when Opened is zero: the driver may simply never have been
// consulted, and that reads exactly like a clean result.
func (h *Handles) Opened() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.opened
}

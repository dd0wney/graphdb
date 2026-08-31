package vfstest

import (
	"os"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Fired must report a per-method fault, not only the sweep's N-th-op fault.
//
// Its doc calls it "the negative control for any single fault test". FailOpen,
// FailWrite, FailSync and FailClose are the single-fault modes, and for all
// four it returned false however many times the fault fired. A test that took
// the doc at its word was permanently red, or — worse, if it asserted the
// other way — silently unguarded.
//
// Found while writing pkg/search's LSA load gate (PR #538), which had to route
// around it by matching ErrInjected instead.
func TestFired_ReportsAPerMethodFault(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(*FaultFS)
		run  func(vfs.FileSystem, string) error
	}{
		{
			name: "open",
			arm:  func(f *FaultFS) { f.FailOpen(Always) },
			run: func(fs vfs.FileSystem, path string) error {
				_, err := fs.Open(path, os.O_RDONLY, 0)
				return err
			},
		},
		{
			name: "sync",
			arm:  func(f *FaultFS) { f.FailSync(Always) },
			run: func(fs vfs.FileSystem, path string) error {
				f, err := fs.Open(path, os.O_WRONLY|os.O_CREATE, 0o600)
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				return f.Sync()
			},
		},
		{
			name: "close",
			arm:  func(f *FaultFS) { f.FailClose(Always) },
			run: func(fs vfs.FileSystem, path string) error {
				f, err := fs.Open(path, os.O_WRONLY|os.O_CREATE, 0o600)
				if err != nil {
					return err
				}
				return f.Close()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := NewFaults(vfs.OS(), "fired-"+tc.name)
			path := t.TempDir() + "/f"
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatalf("seed the file: %v", err)
			}

			tc.arm(fs)

			// The positive control for this test: the fault must actually have
			// fired, or Fired() reporting false would be correct and the
			// assertion below would prove nothing.
			if err := tc.run(fs, path); err == nil {
				t.Fatalf("the armed fault did not fire, so this case tests nothing")
			}
			if !fs.Fired() {
				t.Errorf("a %s fault fired and Fired() reported false, so it cannot serve "+
					"as the negative control its own doc promises", tc.name)
			}
		})
	}
}

// The sweep's meaning must survive. Sweep arms FailNthOp and stops when Fired
// goes false, which is how it knows N ran off the end of the sequence.
func TestFired_StaysFalseWhenTheNthOpFaultRunsOffTheEnd(t *testing.T) {
	fs := NewFaults(vfs.OS(), "fired-nth-past-end")
	fs.FailNthOp(1000)

	path := t.TempDir() + "/f"
	f, err := fs.Open(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if fs.Fired() {
		t.Errorf("no fault fired, and Fired() reported true, so a sweep would never stop")
	}
}

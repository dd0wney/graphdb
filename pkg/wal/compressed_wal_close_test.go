package wal

// CompressedWAL.Close returned early on a Flush or a Sync error and never
// closed the file.
//
//	if err := w.writer.Flush(); err != nil { return err }   // handle still open
//	if err := w.file.Sync(); err != nil { return err }      // handle still open
//	return w.file.Close()
//
// WAL.Close, one file over, does all three unconditionally and joins the
// errors. The two implementations of one idea had come apart, and only the
// compressed one was wrong.
//
// Measured by the github.com/dd0wney/fault session: 9 of 24 injected failure
// points leaked, every one with the store reporting a successful open and a
// Close that was actually called. The nine are the points where the fault lands
// on the Flush or the Sync.
//
// This compounds with the pkg/storage half. Close's switch had no case for the
// compressed WAL at all, so the handle leaked on every ordinary shutdown.
// Adding that case is necessary and not sufficient: once Close is called, a
// failing flush or sync leaks it again.
//
// persistence.go already states the rule this restores: "The Close runs either
// way. Skipping it on a truncate failure was the same leak in a second place."

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

func TestCompressedWALCloseReleasesTheFileWhenSyncFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(*vfstest.FaultFS)
	}{
		{name: "sync fails", arm: func(f *vfstest.FaultFS) { f.FailSync(vfstest.Always) }},
		{name: "no fault", arm: func(*vfstest.FaultFS) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handles := vfstest.NewHandles(vfs.OS())
			faults := vfstest.NewFaults(handles, "compressed-wal-close")

			w, err := NewCompressedWALWithFS(t.TempDir(), faults)
			if err != nil {
				t.Fatalf("open the compressed WAL: %v", err)
			}
			if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
				t.Fatalf("append: %v", err)
			}

			tc.arm(faults)
			closeErr := w.Close()

			// The control. An empty Outstanding proves nothing if the driver
			// never handed out a handle.
			if handles.Opened() == 0 {
				t.Fatalf("the driver handed out no handle, so it is not installed and a " +
					"clean result here says nothing")
			}
			if left := handles.Outstanding(); len(left) > 0 {
				t.Errorf("Close returned %v and left %d of %d handles open: %v\n"+
					"an early return on Flush or Sync skips file.Close entirely",
					closeErr, len(left), handles.Opened(), left)
			}
			// A failing sync must still be reported, not swallowed for the sake
			// of closing. WAL.Close joins all three for exactly this reason.
			if tc.name == "sync fails" && !errors.Is(closeErr, vfstest.ErrInjected) {
				t.Errorf("the sync failure was not reported: %v", closeErr)
			}
		})
	}
}

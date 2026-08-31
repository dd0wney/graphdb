package search

// Gate for the LSA snapshot LOAD path.
//
// SaveToFileWithFS threads pkg/vfs. LoadLSAFromFile did not: it called os.Open
// directly, so no driver observed the reload at all.
//
// That asymmetry is the defect, and it is worse than an untested path. A crash
// sweep writes a scenario's on-disk states through its driver. To check a state
// it must reopen through the same driver. A check that reached for
// LoadLSAFromFile instead read the real directory the scenario had written,
// found every state intact, and reported a pass. The check was not weak. It was
// measuring a different file than the one under test, and a pass from it reads
// exactly like a pass that means something.
//
// Reported by the github.com/dd0wney/fault session on 2026-08-31. It worked
// around the gap by decoding with ReadLSASnapshot(io.Reader), which tests the
// bytes and not the production reload.
//
// RED-FIRST. These tests were run against a LoadLSAFromFileWithFS whose body
// was `os.Open(path)` — an implementation that compiles, satisfies the
// signature, and ignores its first argument. What they printed is in the pull
// request body. The signature alone is not the fix, so the signature alone must
// not be enough to pass.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const roleLSALoad = vfstest.Role("lsaload")

// One role, so the whole reload lands in a single trace and the order of its
// operations is visible. The publish gate next door takes the same approach for
// the same reason.
func lsaLoadClassifier() vfstest.Classifier {
	return func(vfstest.Op, string, int) vfstest.Role { return roleLSALoad }
}

// writeTinyLSASnapshot publishes a snapshot through fsys and returns its path.
func writeTinyLSASnapshot(t *testing.T, fsys vfs.FileSystem, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "tenant.lsa")
	if err := buildTinyLSA(t).SaveToFileWithFS(fsys, path); err != nil {
		t.Fatalf("SaveToFileWithFS: %v", err)
	}
	return path
}

// The reload must reach the driver it is given.
//
// The assertion is the trace, not the returned index. An implementation that
// calls the os package returns a correct index too — it reads the same bytes
// off the same disk. What it does not do is let the driver see the read, and
// that is the whole capability this function exists to provide.
func TestLoadLSAFromFileWithFS_ReadsThroughTheDriver(t *testing.T) {
	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "lsa-load-driver", lsaLoadClassifier())

	path := writeTinyLSASnapshot(t, fs, dir)
	before := len(fs.Trace(roleLSALoad))

	if _, err := LoadLSAFromFileWithFS(fs, path); err != nil {
		t.Fatalf("LoadLSAFromFileWithFS: %v", err)
	}

	// Scoped to the operations the load added. An unscoped search matches the
	// publish's own open and read, which are already in this trace.
	reload := fs.Trace(roleLSALoad)[before:]
	joined := strings.Join(reload, ",")
	if len(reload) == 0 {
		t.Fatalf("the driver observed no operation during the reload, so the load " +
			"bypassed it and a sweep cannot reopen through it")
	}
	if reload[0] != "open" {
		t.Errorf("the reload did not open through the driver\ntrace: %s", joined)
	}
	sawRead := false
	for _, op := range reload {
		if op == "read" {
			sawRead = true
			break
		}
	}
	if !sawRead {
		t.Errorf("the driver observed no read, so the snapshot bytes did not come "+
			"through it\ntrace: %s", joined)
	}
}

// A driver that refuses the open must fail the load.
//
// This is the strong form of the test above. The file is present and valid on
// the real disk, so an implementation that calls the os package succeeds here
// and returns a usable index. Only an implementation that goes through fsys can
// fail.
func TestLoadLSAFromFileWithFS_ADriverThatRefusesTheOpenFailsTheLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeTinyLSASnapshot(t, vfs.OS(), dir)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the snapshot is not on the real disk, so this test proves nothing: %v", err)
	}

	fs := vfstest.NewFaults(vfs.OS(), "lsa-load-refuses-open")
	fs.FailOpen(vfstest.Always)

	idx, err := LoadLSAFromFileWithFS(fs, path)
	if err == nil {
		t.Fatalf("the load succeeded while the driver refused every open, so it read " +
			"past the driver and straight off the disk")
	}
	if idx != nil {
		t.Errorf("a failed load returned a non-nil index, which a caller may store")
	}
	// The negative control. Without it this test passes when the load fails for
	// any other reason — a decode error, a path typo — and says nothing about
	// whether the driver was consulted. Matching the injected error proves the
	// refusal is what the load reported.
	//
	// FaultFS.Fired() is the obvious candidate and it does not work here: only
	// FailNthOp sets that flag, so it stays false for FailOpen, FailWrite,
	// FailSync and FailClose. See the note in the pull request body.
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Errorf("the load failed for some reason other than the driver's refusal, "+
			"so this test does not show the open went through fsys\ngot: %v", err)
	}
}

// The absent contract, which LoadAll depends on.
//
// LoadAll treats a load failure as a per-tenant error and carries on. The doc
// comment promises that an absent snapshot is distinguishable, so a caller can
// fall through to the build path instead of reporting a fault.
func TestLoadLSAFromFileWithFS_AbsentSnapshotSatisfiesErrNotExist(t *testing.T) {
	fs := vfstest.NewRoles(vfs.OS(), "lsa-load-absent", lsaLoadClassifier())
	path := filepath.Join(t.TempDir(), "no-such-tenant.lsa")

	_, err := LoadLSAFromFileWithFS(fs, path)
	if err == nil {
		t.Fatalf("an absent snapshot returned no error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an absent snapshot must satisfy errors.Is(err, os.ErrNotExist), so a "+
			"caller can tell it from a fault and fall through to the build path\ngot: %v", err)
	}
}

// LoadLSAFromFile keeps its behaviour, on the default driver.
func TestLoadLSAFromFile_StillLoadsThroughTheDefaultDriver(t *testing.T) {
	dir := t.TempDir()
	path := writeTinyLSASnapshot(t, vfs.OS(), dir)

	if _, err := LoadLSAFromFile(path); err != nil {
		t.Fatalf("LoadLSAFromFile: %v", err)
	}
}

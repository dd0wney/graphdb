package storage

// Durability gate for the JSON snapshot publish.
//
// FOUND BY A CRASH SWEEP, not by reading. The github.com/dd0wney/fault session
// drove this path through a recorder and generated the on-disk states a power
// cut can leave. Measured on main at 8a0e5e1, 20 of 38 broke, in two modes:
//
//	11 states   the store reopened holding 1 node where 2 or 3 were published
//	 9 states   the store did NOT reopen at all:
//	            "failed to unmarshal snapshot: unexpected end of JSON input"
//
// The same harness against this fix: 16 states, 0 failures, both modes gone.
//
// The first figure reported was 28, and it was 8 too high. That harness applied
// a flat "never 1 node" rule at every crash point, but a crash inside the FIRST
// publish legitimately leaves 1 node because nothing had been acknowledged yet.
// The rule is now scoped to the crash point — the same shape as the scoping
// this test needed below.
//
// The second is the severe one and it is the one reading did not predict.
// snapshot.json exists and is EMPTY, so this is not a revert to an older
// snapshot — it is a store that will not open.
//
// The sharpest state was after=snapshot.json.tmp:rename1 with
// lost=snapshot.json.tmp:write1: the process died just after the rename while
// the write into the temporary file had not reached the platter, so the rename
// published a name pointing at nothing.
//
// The cause is two missing syncs on one path:
//
//   - writeFileWithFS (vfs_helpers.go) opened, wrote and closed. POSIX does not
//     make close(2) flush, so the temporary file's CONTENTS were not durable.
//   - the caller renamed without syncing the parent directory, so the NAME was
//     not durable either.
//
// Both are needed. A durable name over unsynced bytes is not durable data, and
// synced bytes under a name that never landed are unreachable. PR #530 fixed
// the mmap path; this path kept both gaps.
//
// This test drives a real publish through a vfstest.RoleFS with EVERY operation
// in one role, so the order across the temp file and the directory is visible
// in a single trace.

import (
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// oneRole puts every operation into a single role, so the trace is the whole
// publish in order. Two roles would show the file sync and the rename in
// different traces and the ordering between them would be unobservable.
const roleWholePublish = vfstest.Role("wholepublish")

func wholePublishClassifier() vfstest.Classifier {
	return func(vfstest.Op, string, int) vfstest.Role { return roleWholePublish }
}

// The JSON publish must sync the temporary file BEFORE the rename, and the
// parent directory AFTER it.
//
// The order carries the whole assertion. A file sync after the rename is too
// late — the name already points at bytes that may not exist. A directory sync
// before the rename publishes an entry the rename has not yet created.
func TestJSONSnapshot_SyncsTheFileBeforeRenameAndTheDirectoryAfter(t *testing.T) {
	if !vfs.DirSyncSupported {
		t.Skip("this platform cannot sync a directory handle")
	}

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "json-publish-durability", wholePublishClassifier())

	cfg := jsonConfig(dir)
	cfg.FS = fs
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	for i := 0; i < 3; i++ {
		if _, err := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("n")}); err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	trace := fs.Trace(roleWholePublish)
	joined := strings.Join(trace, ",")

	last := -1
	for i, op := range trace {
		if op == "rename" {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("no rename in the trace, so the snapshot did not publish: %s", joined)
	}

	// The temporary file's contents must be durable BEFORE the name appears.
	//
	// The assertion is scoped to the PUBLISH, not to the whole trace. An
	// earlier version looked for any sync before the rename and passed against
	// the unfixed code, because the WAL syncs on every CreateNode and those
	// syncs sit in the same trace. It was measuring the WAL and reporting on
	// the snapshot.
	//
	// The publish is the last open before the rename, through to the rename.
	// Only a sync inside that window is the temporary file's.
	openAt := -1
	for i := last - 1; i >= 0; i-- {
		if trace[i] == "open" {
			openAt = i
			break
		}
	}
	if openAt < 0 {
		t.Fatalf("no open before the rename, so the publish is not in this trace: %s", joined)
	}
	syncedBeforeRename := false
	for _, op := range trace[openAt:last] {
		if op == "sync" {
			syncedBeforeRename = true
			break
		}
	}
	if !syncedBeforeRename {
		t.Errorf("the temporary file is not synced before the rename, so the rename can "+
			"publish a name over bytes that never reached the platter. This is the state "+
			"that leaves snapshot.json EMPTY and the store unable to open.\ntrace: %s", joined)
	}

	// The name itself must be durable AFTER the rename.
	if len(trace) < last+3 || trace[last+1] != "open" || trace[last+2] != "sync" {
		t.Errorf("the data directory is not opened and synced after the rename, so the "+
			"rename itself is not durable and the publish can be lost whole.\ntrace: %s", joined)
	}
}

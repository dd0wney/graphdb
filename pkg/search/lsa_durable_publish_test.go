package search

// Durability gate for the LSA snapshot publish.
//
// SaveToFile writes a temporary file and renames it. Until PR #535's sibling
// fix it synced NEITHER the file nor the parent directory, which is the same
// defect a crash sweep measured on pkg/storage's JSON publish: 20 of 38 states
// broken, 9 of them leaving a file that exists and does not decode.
//
// This path was invisible to that sweep. SaveToFile called the os package
// directly, so no driver observed any of its operations. The sweep ran over
// graphdb, reported, and never saw this function. That is the sharper half of
// the lesson: a clean sweep says nothing about code outside the seam, and
// "found nothing" and "never looked" render identically.
//
// The fix threads pkg/vfs through so the path is both testable and sweepable.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const roleLSAPublish = vfstest.Role("lsapublish")

// Every operation lands in one role, so the order across the temporary file
// and the parent directory is visible in a single trace. Two roles would put
// the file sync and the rename in different traces and the ordering between
// them would be unobservable.
func lsaPublishClassifier() vfstest.Classifier {
	return func(vfstest.Op, string, int) vfstest.Role { return roleLSAPublish }
}

// The LSA publish must sync the temporary file BEFORE the rename, and the
// parent directory AFTER it.
//
// The order is the assertion. A file sync after the rename is too late — the
// name already points at bytes that may not exist. A directory sync before the
// rename publishes an entry the rename has not yet created.
func TestLSASnapshot_SyncsTheFileBeforeRenameAndTheDirectoryAfter(t *testing.T) {
	if !vfs.DirSyncSupported {
		t.Skip("this platform cannot sync a directory handle")
	}

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "lsa-publish-durability", lsaPublishClassifier())

	idx := buildTinyLSA(t)

	path := filepath.Join(dir, "tenant.lsa")
	if err := idx.SaveToFileWithFS(fs, path); err != nil {
		t.Fatalf("SaveToFileWithFS: %v", err)
	}

	trace := fs.Trace(roleLSAPublish)
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

	// Scoped to the publish window — the last open before the rename through to
	// the rename. An unscoped search for "any sync before the rename" is how the
	// pkg/storage version of this test passed against unfixed code: it matched
	// the WAL's syncs, which sat in the same trace.
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
	synced := false
	for _, op := range trace[openAt:last] {
		if op == "sync" {
			synced = true
			break
		}
	}
	if !synced {
		t.Errorf("the temporary file is not synced before the rename, so the rename can "+
			"publish a name over bytes that never reached the platter\ntrace: %s", joined)
	}

	if len(trace) < last+3 || trace[last+1] != "open" || trace[last+2] != "sync" {
		t.Errorf("the parent directory is not opened and synced after the rename, so the "+
			"rename itself is not durable and the publish can be lost whole\ntrace: %s", joined)
	}
}

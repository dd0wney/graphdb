package storage

// Tests for the persisted uniqueness-rules registry itself (ADR 0001, commit
// 1): construction, persistence, load-time refusals and validation. The
// enforcement path (CreateNodeWithUniquenessRulesForTenant) is covered
// separately in uniqueness_rules_enforce_test.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// rulesEqual compares two already-sorted UniquenessRule slices field by
// field. UniquenessRule holds only strings, so == is safe per element.
func rulesEqual(a, b []UniquenessRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestUniquenessRules_PersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}

	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "b_rule", Label: "Beta", PropertyKey: "key_b"}); err != nil {
		t.Fatalf("RegisterUniquenessRule b-rule: %v", err)
	}
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "a_rule", Label: "Alpha", PropertyKey: "key_a"}); err != nil {
		t.Fatalf("RegisterUniquenessRule a-rule: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	got := reopened.UniquenessRules()
	want := []UniquenessRule{
		{Name: "a_rule", Label: "Alpha", PropertyKey: "key_a"},
		{Name: "b_rule", Label: "Beta", PropertyKey: "key_b"},
	}
	if !rulesEqual(got, want) {
		t.Fatalf("UniquenessRules() after reopen = %+v, want %+v", got, want)
	}
}

func TestUniquenessRules_MissingFileIsEmpty(t *testing.T) {
	gs := newTestGraphStorage(t)
	if got := gs.UniquenessRules(); len(got) != 0 {
		t.Errorf("UniquenessRules() on a fresh store = %+v, want empty", got)
	}
}

func TestUniquenessRules_CorruptFileRefusesOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed corrupt rules.json: %v", err)
	}

	_, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err == nil {
		t.Fatalf("NewGraphStorageWithConfig: want an error for a corrupt rules.json, got nil")
	}
	if !strings.Contains(err.Error(), "rules.json") {
		t.Errorf("error %q does not name rules.json", err.Error())
	}
}

func TestUniquenessRules_OversizedFileRefusesOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	oversized := make([]byte, maxUniquenessRulesFileSize+1)
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatalf("seed oversized rules.json: %v", err)
	}

	_, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err == nil {
		t.Fatalf("NewGraphStorageWithConfig: want an error for an oversized rules.json, got nil")
	}
	if !strings.Contains(err.Error(), "rules.json") {
		t.Errorf("error %q does not name rules.json", err.Error())
	}
}

func TestUniquenessRules_CountCap(t *testing.T) {
	gs := newTestGraphStorage(t)

	for i := 0; i < maxUniquenessRules; i++ {
		name := fmt.Sprintf("rule_%03d", i)
		if err := gs.RegisterUniquenessRule(UniquenessRule{Name: name, Label: "L", PropertyKey: "k"}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	if got := len(gs.UniquenessRules()); got != maxUniquenessRules {
		t.Fatalf("registered rule count = %d, want %d", got, maxUniquenessRules)
	}

	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "rule_256", Label: "L", PropertyKey: "k"}); err == nil {
		t.Errorf("a 257th distinct rule name should have been refused")
	}

	// An upsert of an EXISTING name at the cap must still succeed.
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "rule_000", Label: "L2", PropertyKey: "k2"}); err != nil {
		t.Errorf("upsert of an existing name at the cap should succeed: %v", err)
	}
}

func TestUniquenessRules_RegisterValidatesFields(t *testing.T) {
	gs := newTestGraphStorage(t)

	overlongNameOrKey := strings.Repeat("a", 101)
	overlongLabel := strings.Repeat("b", 51)

	tests := []struct {
		name string
		rule UniquenessRule
	}{
		{"empty name", UniquenessRule{Name: "", Label: "L", PropertyKey: "k"}},
		{"empty label", UniquenessRule{Name: "n", Label: "", PropertyKey: "k"}},
		{"empty propertyKey", UniquenessRule{Name: "n", Label: "L", PropertyKey: ""}},
		{"overlong name", UniquenessRule{Name: overlongNameOrKey, Label: "L", PropertyKey: "k"}},
		{"overlong label", UniquenessRule{Name: "n", Label: overlongLabel, PropertyKey: "k"}},
		{"overlong propertyKey", UniquenessRule{Name: "n", Label: "L", PropertyKey: overlongNameOrKey}},
		{"bad charset name (leading digit)", UniquenessRule{Name: "1bad", Label: "L", PropertyKey: "k"}},
		{"bad charset label (hyphen)", UniquenessRule{Name: "n", Label: "L-bad", PropertyKey: "k"}},
		{"bad charset propertyKey (leading digit)", UniquenessRule{Name: "n", Label: "L", PropertyKey: "1bad"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := gs.RegisterUniquenessRule(tt.rule); err == nil {
				t.Errorf("RegisterUniquenessRule(%+v) = nil, want an error", tt.rule)
			}
		})
	}
}

// TestUniquenessRules_DurableWriteSurvivesCrash targets the exact window
// CLAUDE.md's format-stability rule cares about: the rename that publishes
// rules.json is not durable until the parent directory is itself synced
// (see vfs_helpers.go's writeFileWithFS doc comment and PR #530). This test
// swaps the driver to a vfstest.CrashFS only for the SECOND persist, so
// construction and the first register run on the plain OS driver and only
// the write under test is subject to the simulated power cut.
func TestUniquenessRules_DurableWriteSurvivesCrash(t *testing.T) {
	if !vfs.DirSyncSupported {
		t.Skip("this platform cannot sync a directory handle")
	}

	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "old_rule", Label: "Old", PropertyKey: "k"}); err != nil {
		t.Fatalf("seed old-rule: %v", err)
	}

	crash := vfstest.NewCrash(vfs.OS(), "uniqueness-rules-crash", 1)
	gs.fs = crash

	// persistUniquenessRulesLocked's operation sequence on this driver:
	// open(.new) [1], write [2], sync [3], close (not gated), rename (not
	// gated — CrashFS.Rename forwards straight to the base filesystem),
	// open(dir) [4], sync(dir) [5]. Cutting at 4 fires while the
	// parent-directory sync is starting: the rename has already reached the
	// base filesystem and the new file's bytes were already fsynced before
	// it, which is the cut the plan calls "between rename and parent fsync".
	crash.CrashAt(4)
	persistErr := gs.RegisterUniquenessRule(UniquenessRule{Name: "new_rule", Label: "New", PropertyKey: "k2"})

	if persistErr == nil {
		t.Fatalf("expected the second register to fail under the simulated crash, got nil")
	}
	if !crash.Crashed() {
		t.Fatalf("expected the crash to fire during the second persist; ops=%d", crash.Ops())
	}
	// gs is deliberately abandoned here, not Closed: a real process would be
	// gone at this point, and Close would run through the crashed driver.

	reopened, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	got := reopened.UniquenessRules()
	oldOnly := []UniquenessRule{{Name: "old_rule", Label: "Old", PropertyKey: "k"}}
	both := []UniquenessRule{
		{Name: "new_rule", Label: "New", PropertyKey: "k2"},
		{Name: "old_rule", Label: "Old", PropertyKey: "k"},
	}
	if !rulesEqual(got, oldOnly) && !rulesEqual(got, both) {
		t.Fatalf("after the crash, UniquenessRules() = %+v, want either the old set %+v or the new set %+v",
			got, oldOnly, both)
	}
}

package backup_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// writeCoveredClaim attempts the one write this file's tests all use to
// probe the registry: a :Claim node with a fixed for_task value, in tenant
// "t1". Factored out so the pre-restore and post-restore attempts are
// provably the same call.
func writeCoveredClaim(gs *storage.GraphStorage) error {
	_, err := gs.CreateNodeWithUniquenessRulesForTenant(
		"t1", []string{"Claim"}, map[string]storage.Value{"for_task": storage.StringValue("x")},
	)
	return err
}

// TestWriteArchive_CarriesUniquenessRules pins that rules.json (ADR 0001, a
// third on-disk artefact alongside the two snapshot formats — CLAUDE.md's
// "Snapshot format stability" section) travels through backup/restore.
//
// Before this, a restored store lost every registered rule: with a
// deployment-required rule configured on both ends and nothing surviving
// the round trip, the restored store refused the covered write exactly as
// if the operator had never registered anything.
//
// Fix round 1 (Missing item) strengthened this to the same two-direction
// shape stage 1's own TestCreateNodeWithUniquenessRules_TwoDirection uses
// for the registry itself: refused before, accepted once the state is
// right, refused again on the duplicate. Here "the state is right" is
// specifically RESTORING the archive — the same required pair, at an
// otherwise-empty directory, refuses beforehand and accepts afterward.
func TestWriteArchive_CarriesUniquenessRules(t *testing.T) {
	srcDir := t.TempDir()
	required := []storage.RequiredUniquenessRule{{Name: "claim_for_task", Label: "Claim"}}

	cfg := storage.DefaultStorageConfig(srcDir)
	cfg.RequiredUniquenessRules = required
	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	if err := gs.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var buf bytes.Buffer
	if err := backup.WriteArchive(&buf, srcDir, "test-version"); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	// BEFORE restoring: the destination directory is empty, so a store
	// opened there with the same required pair has nothing registered —
	// the covered write must be refused with the required-rule-missing
	// class (REST's 503; storage's own sentinel here, since this package
	// has no HTTP layer to check a status code against).
	destDir := t.TempDir()
	preCfg := storage.DefaultStorageConfig(destDir)
	preCfg.RequiredUniquenessRules = required
	pre, err := storage.NewGraphStorageWithConfig(preCfg)
	if err != nil {
		t.Fatalf("open empty destination before restore: %v", err)
	}
	if err := writeCoveredClaim(pre); !errors.Is(err, storage.ErrRequiredUniquenessRuleMissing) {
		t.Fatalf("write before restore: got %v, want ErrRequiredUniquenessRuleMissing", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-restore store: %v", err)
	}

	if err := backup.Extract(bytes.NewReader(buf.Bytes()), destDir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	restoredCfg := storage.DefaultStorageConfig(destDir)
	restoredCfg.RequiredUniquenessRules = required
	restored, err := storage.NewGraphStorageWithConfig(restoredCfg)
	if err != nil {
		t.Fatalf("reopen restored store: %v", err)
	}
	defer func() { _ = restored.Close() }()

	rules := restored.UniquenessRules()
	if len(rules) != 1 || rules[0].Name != "claim_for_task" {
		t.Fatalf("restored registry = %+v, want the one registered rule", rules)
	}

	// AFTER restoring, the same write the pre-restore store refused now
	// succeeds — restoring the archive is what flipped this, not anything
	// else the test did to the destination directory.
	if err := writeCoveredClaim(restored); err != nil {
		t.Fatalf("write after restore should succeed with the rule restored: %v", err)
	}
	// The duplicate is refused on the OTHER ground: the rule now exists
	// and is doing its job.
	if err := writeCoveredClaim(restored); !errors.Is(err, storage.ErrUniqueConstraintViolation) {
		t.Fatalf("duplicate after restore: got %v, want ErrUniqueConstraintViolation", err)
	}
}

// TestWriteArchive_MissingRulesFileIsNotAnError pins that a store which
// never registered a rule backs up and restores cleanly with no rules.json
// member and an empty registry — the file is optional, exactly like the
// two snapshot formats.
func TestWriteArchive_MissingRulesFileIsNotAnError(t *testing.T) {
	srcDir := t.TempDir()
	gs, err := storage.NewGraphStorage(srcDir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var buf bytes.Buffer
	if err := backup.WriteArchive(&buf, srcDir, "test-version"); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	destDir := t.TempDir()
	if err := backup.Extract(bytes.NewReader(buf.Bytes()), destDir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	restored, err := storage.NewGraphStorage(destDir)
	if err != nil {
		t.Fatalf("reopen restored store: %v", err)
	}
	defer func() { _ = restored.Close() }()
	if rules := restored.UniquenessRules(); len(rules) != 0 {
		t.Errorf("restored registry = %+v, want empty", rules)
	}
}

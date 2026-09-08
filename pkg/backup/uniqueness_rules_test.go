package backup_test

import (
	"bytes"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestWriteArchive_CarriesUniquenessRules pins that rules.json (ADR 0001, a
// third on-disk artefact alongside the two snapshot formats — CLAUDE.md's
// "Snapshot format stability" section) travels through backup/restore.
//
// Before this, a restored store lost every registered rule: with a
// deployment-required rule configured on both ends and nothing surviving
// the round trip, the restored store refused the covered write exactly as
// if the operator had never registered anything.
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

	destDir := t.TempDir()
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

	if _, err := restored.CreateNodeWithUniquenessRulesForTenant(
		"t1", []string{"Claim"}, map[string]storage.Value{"for_task": storage.StringValue("x")},
	); err != nil {
		t.Fatalf("first covered write should succeed with the rule restored: %v", err)
	}
	if _, err := restored.CreateNodeWithUniquenessRulesForTenant(
		"t1", []string{"Claim"}, map[string]storage.Value{"for_task": storage.StringValue("x")},
	); err == nil {
		t.Fatal("duplicate should be refused; the restored rule was not enforced")
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

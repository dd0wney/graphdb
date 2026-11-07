package storage

// Red-first tests for the three defects of the first sealed-rules.json commit:
// a wrong key sealing the registry, SetEncryption leaving rules.json under the
// old engine, and a data race on the engine field.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/encryption"
)

func otherEncryptionEngine(t *testing.T, fill string) *encryption.Engine {
	t.Helper()
	engine, err := encryption.NewEngine(bytes.Repeat([]byte(fill), 32))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

// writeVersion1Rules writes a version 1 rules.json into an otherwise empty
// directory: an encrypted store whose process stopped before Close, so no
// snapshot and no WAL entry can prove the key.
func writeVersion1Rules(t *testing.T, dir string) []byte {
	t.Helper()
	legacy, err := json.Marshal(uniquenessRulesDocument{Version: uniquenessRulesFileVersion, Rules: []UniquenessRule{sealedTestRule}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), legacy, 0o600); err != nil {
		t.Fatalf("write version 1 rules.json: %v", err)
	}
	return legacy
}

// Fix 1: with no snapshot and no WAL entry, nothing proves the key, so an
// open must keep version 1. The next open that proves the key seals it.
func TestUniquenessRules_UnprovenKeyKeepsVersion1ThenSealsOnProvenOpen(t *testing.T) {
	dir := t.TempDir()
	legacy := writeVersion1Rules(t, dir)
	cfg := encryptedRulesConfig(dir, testEncryptionEngine(t))

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := readRulesFile(t, dir); !bytes.Equal(got, legacy) {
		t.Errorf("an open that proved no key changed rules.json:\n got %s\nwant %s", got, legacy)
	}
	if err := gs.Close(); err != nil { // writes an encrypted snapshot: the key is now provable
		t.Fatalf("close: %v", err)
	}

	gs, err = NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = gs.Close() }()
	if v := rulesFileKeys(t, readRulesFile(t, dir))["version"]; string(v) != "2" {
		t.Errorf("version after an open that decrypted the snapshot = %s, want 2", v)
	}
}

// Fix 1, registration path: an unproven engine must not seal either.
func TestUniquenessRules_RegisterWritesVersion1WhileKeyUnproven(t *testing.T) {
	dir := t.TempDir()
	writeVersion1Rules(t, dir)
	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, testEncryptionEngine(t)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()
	extra := UniquenessRule{Name: "extra", Label: "ExtraLabel", PropertyKey: "extra_key"}
	if err := gs.RegisterUniquenessRule(extra); err != nil {
		t.Fatalf("register: %v", err)
	}
	if v := rulesFileKeys(t, readRulesFile(t, dir))["version"]; string(v) != "1" {
		t.Errorf("version after Register on an unproven key = %s, want 1", v)
	}
}

// Fix 1, positive control: a fresh encrypted store has nothing a key could
// contradict, so its first registration is sealed.
func TestUniquenessRules_FreshEncryptedStoreSealsFirstRegistration(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, testEncryptionEngine(t)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()
	if err := gs.RegisterUniquenessRule(sealedTestRule); err != nil {
		t.Fatalf("register: %v", err)
	}
	if v := rulesFileKeys(t, readRulesFile(t, dir))["version"]; string(v) != "2" {
		t.Errorf("version = %s, want 2", v)
	}
}

// Fix 2: after SetEncryption(newEngine), rules.json is sealed under the new
// key at once, and an open with the new key reads the rules.
func TestUniquenessRules_SetEncryptionRotatesRulesFile(t *testing.T) {
	dir := t.TempDir()
	oldKey := testEncryptionEngine(t)
	newKey := otherEncryptionEngine(t, "n")
	registerAndClose(t, encryptedRulesConfig(dir, oldKey), sealedTestRule)

	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, oldKey))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := gs.SetEncryption(newKey, &mockKeyProvider{}); err != nil {
		t.Fatalf("SetEncryption: %v", err)
	}
	if _, err := newKey.Decrypt(sealedBytes(t, readRulesFile(t, dir))); err != nil {
		t.Errorf("rules.json is not sealed under the new key right after SetEncryption: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	assertRulePresent(t, encryptedRulesConfig(dir, newKey), sealedTestRule)
}

// Fix 2: SetEncryption(nil) writes version 1, so an open with no key reads
// the rules.
func TestUniquenessRules_SetEncryptionNilWritesVersion1(t *testing.T) {
	dir := t.TempDir()
	oldKey := testEncryptionEngine(t)
	registerAndClose(t, encryptedRulesConfig(dir, oldKey), sealedTestRule)

	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, oldKey))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := gs.SetEncryption(nil, nil); err != nil {
		t.Fatalf("SetEncryption: %v", err)
	}
	if v := rulesFileKeys(t, readRulesFile(t, dir))["version"]; string(v) != "1" {
		t.Errorf("version right after SetEncryption(nil) = %s, want 1", v)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	assertRulePresent(t, jsonConfig(dir), sealedTestRule)
}

// Fix 3: Register and SetEncryption must not race on the engine field.
// Run with -race.
func TestUniquenessRules_RegisterAndSetEncryptionDoNotRace(t *testing.T) {
	dir := t.TempDir()
	a := testEncryptionEngine(t)
	b := otherEncryptionEngine(t, "b")
	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, a))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			r := UniquenessRule{Name: fmt.Sprintf("r%d", i), Label: fmt.Sprintf("L%d", i), PropertyKey: "k"}
			if err := gs.RegisterUniquenessRule(r); err != nil {
				t.Errorf("register: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			engine := a
			if i%2 == 0 {
				engine = b
			}
			if err := gs.SetEncryption(engine, &mockKeyProvider{}); err != nil {
				t.Errorf("SetEncryption: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

// Nit: invalid base64 in "sealed" must say base64, not "JSON".
func TestUniquenessRules_InvalidBase64SealedNamesBase64(t *testing.T) {
	dir := t.TempDir()
	bad := `{"version":2,"sealed":"!!!not base64!!!"}`
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, testEncryptionEngine(t)))
	if err == nil {
		t.Fatal("open: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "base64") || strings.Contains(err.Error(), "does not parse as JSON") {
		t.Errorf("error %q should name base64 and not say JSON", err.Error())
	}
}

func sealedBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var doc uniquenessRulesSealedDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("rules.json is not a sealed document: %v (%s)", err, data)
	}
	return doc.Sealed
}

// Fix 2: a failed rules.json write makes SetEncryption return the error and
// change nothing, so the old engine still opens the store.
func TestUniquenessRules_SetEncryptionReturnsRulesWriteError(t *testing.T) {
	dir := t.TempDir()
	oldKey := testEncryptionEngine(t)
	newKey := otherEncryptionEngine(t, "n")
	registerAndClose(t, encryptedRulesConfig(dir, oldKey), sealedTestRule)

	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, oldKey))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()
	// A directory at the temp path makes the write fail before the rename.
	if err := os.Mkdir(filepath.Join(dir, "rules.json.new"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := gs.SetEncryption(newKey, &mockKeyProvider{}); err == nil {
		t.Fatal("SetEncryption with a failing rules.json write: want an error, got nil")
	}
	if _, err := oldKey.Decrypt(sealedBytes(t, readRulesFile(t, dir))); err != nil {
		t.Errorf("rules.json changed although SetEncryption failed: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "rules.json.new")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// The engine did not change: a registration still seals under the old key.
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "after", Label: "AfterLabel", PropertyKey: "after_key"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := oldKey.Decrypt(sealedBytes(t, readRulesFile(t, dir))); err != nil {
		t.Errorf("registration after the failed SetEncryption is not sealed under the old key: %v", err)
	}
}

// PROBE, not a fix. Sequence: an encrypted store with key K1 holds rules and
// no snapshot and no WAL entry. An open with a wrong key K2 succeeds, and its
// Close writes a snapshot under K2. An open with K1 then fails.
//
// The rules file here is version 1. A version 2 file would refuse the K2 open
// at rules.json, so the sequence needs a file that nothing sealed: a registry
// written before this branch, or kept as version 1 while the key was unproven.
func TestProbe_WrongKeyOpenThenCloseLocksOutRightKey(t *testing.T) {
	dir := t.TempDir()
	writeVersion1Rules(t, dir)
	k1 := testEncryptionEngine(t)
	k2 := otherEncryptionEngine(t, "z")

	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, k2))
	if err != nil {
		t.Skipf("step 1 stops the sequence: the open with the wrong key fails: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Skipf("step 2 stops the sequence: Close under the wrong key fails: %v", err)
	}
	gs, err = NewGraphStorageWithConfig(encryptedRulesConfig(dir, k1))
	if err == nil {
		_ = gs.Close()
		t.Skip("step 3 stops the sequence: the open with the right key succeeds")
	}
	t.Skipf("DEFECT (not fixed here, needs a design): wrong-key open then Close wrote a K2 snapshot, and the K1 open now fails: %v", err)
}

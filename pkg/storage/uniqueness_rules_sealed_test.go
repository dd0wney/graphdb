package storage

// Tests for the sealed form of rules.json (version 2): an encrypted store
// seals the registry with its engine, an unencrypted store still writes
// version 1, and a version 1 file is upgraded when an engine opens it.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/encryption"
)

func encryptedRulesConfig(dir string, engine *encryption.Engine) StorageConfig {
	cfg := jsonConfig(dir)
	cfg.EncryptionEngine = engine
	cfg.KeyManager = &mockKeyProvider{}
	return cfg
}

func readRulesFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "rules.json"))
	if err != nil {
		t.Fatalf("read rules.json: %v", err)
	}
	return data
}

func rulesFileKeys(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("rules.json does not parse: %v (%s)", err, data)
	}
	return m
}

var sealedTestRule = UniquenessRule{Name: "sealed_rule", Label: "SealedLabel", PropertyKey: "sealed_key"}

func registerAndClose(t *testing.T, cfg StorageConfig, r UniquenessRule) {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := gs.RegisterUniquenessRule(r); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func assertRulePresent(t *testing.T, cfg StorageConfig, want UniquenessRule) {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs.Close() }()
	if got := gs.UniquenessRules(); !rulesEqual(got, []UniquenessRule{want}) {
		t.Fatalf("rules after reopen = %+v, want [%+v]", got, want)
	}
}

func TestUniquenessRules_EncryptedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	engine := testEncryptionEngine(t)
	cfg := encryptedRulesConfig(dir, engine)
	registerAndClose(t, cfg, sealedTestRule)
	assertRulePresent(t, cfg, sealedTestRule)
}

func TestUniquenessRules_EncryptedFileIsVersion2WithoutRules(t *testing.T) {
	dir := t.TempDir()
	registerAndClose(t, encryptedRulesConfig(dir, testEncryptionEngine(t)), sealedTestRule)

	data := readRulesFile(t, dir)
	keys := rulesFileKeys(t, data)
	if string(keys["version"]) != "2" {
		t.Errorf("version = %s, want 2", keys["version"])
	}
	if _, ok := keys["rules"]; ok {
		t.Errorf("encrypted rules.json has a rules key: %s", data)
	}
	if _, ok := keys["sealed"]; !ok {
		t.Errorf("encrypted rules.json has no sealed key: %s", data)
	}
	if bytes.Contains(data, []byte(sealedTestRule.Name)) {
		t.Errorf("encrypted rules.json holds the rule name in plaintext: %s", data)
	}
}

func TestUniquenessRules_UnencryptedFileStaysVersion1(t *testing.T) {
	dir := t.TempDir()
	registerAndClose(t, jsonConfig(dir), sealedTestRule)

	want := `{"version":1,"rules":[{"name":"sealed_rule","label":"SealedLabel","propertyKey":"sealed_key"}]}`
	if got := string(readRulesFile(t, dir)); got != want {
		t.Errorf("rules.json = %s, want %s", got, want)
	}
}

// sealedRulesDir returns the data directory of an encrypted store that
// registered sealedTestRule and closed. The registry loads before the
// snapshot, so an open of this directory meets rules.json first.
func sealedRulesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	registerAndClose(t, encryptedRulesConfig(dir, testEncryptionEngine(t)), sealedTestRule)
	return dir
}

func TestUniquenessRules_SealedFileWithoutEngineRefusesOpen(t *testing.T) {
	dir := sealedRulesDir(t)

	_, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err == nil {
		t.Fatal("open of a sealed rules.json with no engine: want an error, got nil")
	}
	for _, want := range []string{"rules.json", "encrypted", "no encryption engine"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestUniquenessRules_SealedFileWithWrongKeyRefusesOpen(t *testing.T) {
	dir := sealedRulesDir(t)

	other, err := encryption.NewEngine(bytes.Repeat([]byte("z"), 32))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_, err = NewGraphStorageWithConfig(encryptedRulesConfig(dir, other))
	if err == nil {
		t.Fatal("open with a different key: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "rules.json") {
		t.Errorf("error %q does not name rules.json", err.Error())
	}
}

// A version 1 file beside a plaintext snapshot is not sealed by the first open
// with an engine: the plaintext snapshot does not prove the key. That open's
// Close writes an encrypted snapshot, and the next open, which decrypts it,
// seals the file.
func TestUniquenessRules_Version1UpgradedOnTheOpenThatProvesTheKey(t *testing.T) {
	dir := t.TempDir()
	registerAndClose(t, jsonConfig(dir), sealedTestRule)
	if v := rulesFileKeys(t, readRulesFile(t, dir))["version"]; string(v) != "1" {
		t.Fatalf("setup: version = %s, want 1", v)
	}

	cfg := encryptedRulesConfig(dir, testEncryptionEngine(t))
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open with engine: %v", err)
	}
	if v := rulesFileKeys(t, readRulesFile(t, dir))["version"]; string(v) != "1" {
		t.Errorf("version after an open that proved no key = %s, want 1", v)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	gs, err = NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	// Read before any registration: the upgrade must not wait for a write.
	data := readRulesFile(t, dir)
	if v := rulesFileKeys(t, data)["version"]; string(v) != "2" {
		t.Errorf("version after the open that decrypted the snapshot = %s, want 2: %s", v, data)
	}
	if bytes.Contains(data, []byte(sealedTestRule.Name)) {
		t.Errorf("plaintext rule name still on disk after open: %s", data)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	assertRulePresent(t, cfg, sealedTestRule)
}

// A store encrypted before rules.json could be sealed holds an encrypted
// snapshot beside a version 1 registry. Opened by mistake with the wrong key,
// the open must fail without sealing the registry under that key: otherwise
// the next open with the right key cannot decrypt rules.json, and a refused
// open has cost the operator their registry.
func TestUniquenessRules_WrongKeyOpenDoesNotResealVersion1(t *testing.T) {
	dir := t.TempDir()
	right := testEncryptionEngine(t)
	gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, right))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := gs.CreateNode([]string{"Sealed"}, nil); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	legacy, err := json.Marshal(uniquenessRulesDocument{Version: uniquenessRulesFileVersion, Rules: []UniquenessRule{sealedTestRule}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), legacy, 0o600); err != nil {
		t.Fatalf("write version 1 rules.json: %v", err)
	}

	wrong, err := encryption.NewEngine(bytes.Repeat([]byte("z"), 32))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if gs, err := NewGraphStorageWithConfig(encryptedRulesConfig(dir, wrong)); err == nil {
		_ = gs.Close()
		t.Fatal("open with the wrong key: want an error, got nil")
	}
	if got := readRulesFile(t, dir); !bytes.Equal(got, legacy) {
		t.Fatalf("a refused open rewrote rules.json:\n got %s\nwant %s", got, legacy)
	}

	assertRulePresent(t, encryptedRulesConfig(dir, right), sealedTestRule)
}

package storage

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/encryption"
)

// M-14 (AUDIT_security_2026-06-10): snapshot.json gets a magic-header +
// version envelope (mirroring the LSA snapshot's GLSA pattern) instead of
// the first-byte encrypted-vs-plaintext heuristic. Legacy headerless
// snapshots (plaintext and encrypted) must keep loading.

func testEncryptionEngine(t *testing.T) *encryption.Engine {
	t.Helper()
	engine, err := encryption.NewEngine(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

// writeSnapshotToDir creates a storage in dir with one marker node and
// snapshots it, returning the raw snapshot file bytes.
func writeSnapshotToDir(t *testing.T, dir string, engine *encryption.Engine) []byte {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	if engine != nil {
		gs.SetEncryption(engine, &mockKeyProvider{})
	}
	if _, err := gs.CreateNode([]string{"EnvelopeMarker"}, map[string]Value{
		"name": StringValue("envelope-roundtrip"),
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}
	return data
}

// reopenAndAssertMarker opens a storage over dir and asserts the marker
// node written by writeSnapshotToDir survived the round-trip.
func reopenAndAssertMarker(t *testing.T, dir string, engine *encryption.Engine) {
	t.Helper()
	cfg := jsonConfig(dir)
	if engine != nil { // typed-nil *Engine in the interface field would defeat != nil checks
		cfg.EncryptionEngine = engine
	}
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs.Close() }()
	var count int
	for _, node := range gs.GetAllNodesAcrossTenants() {
		for _, label := range node.Labels {
			if label == "EnvelopeMarker" {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 EnvelopeMarker node after reload, got %d", count)
	}
}

func TestSnapshot_WritesVersionedEnvelope(t *testing.T) {
	data := writeSnapshotToDir(t, t.TempDir(), nil)

	if len(data) < snapshotHeaderSize {
		t.Fatalf("snapshot file shorter than envelope header: %d bytes", len(data))
	}
	if !bytes.Equal(data[:4], snapshotMagic[:]) {
		t.Fatalf("expected magic %q at offset 0, got %q", snapshotMagic, data[:4])
	}
	if v := binary.BigEndian.Uint32(data[4:8]); v != snapshotFormatVersion {
		t.Fatalf("expected version %d, got %d", snapshotFormatVersion, v)
	}
	if flags := data[8]; flags&snapshotFlagEncrypted != 0 {
		t.Fatalf("plaintext snapshot has encrypted flag set (flags=%#x)", flags)
	}
	if payload := data[snapshotHeaderSize:]; len(payload) == 0 || payload[0] != '{' {
		t.Fatalf("plaintext payload should be raw JSON starting with '{'")
	}
}

func TestSnapshot_EncryptedEnvelopeFlagAndOpaquePayload(t *testing.T) {
	data := writeSnapshotToDir(t, t.TempDir(), testEncryptionEngine(t))

	if !bytes.Equal(data[:4], snapshotMagic[:]) {
		t.Fatalf("expected magic header on encrypted snapshot, got %q", data[:4])
	}
	if flags := data[8]; flags&snapshotFlagEncrypted == 0 {
		t.Fatalf("encrypted snapshot missing encrypted flag (flags=%#x)", flags)
	}
	if bytes.Contains(data, []byte("envelope-roundtrip")) {
		t.Fatalf("encrypted snapshot leaks plaintext property value")
	}
}

func TestSnapshotLoad_EnvelopeRoundTrip_Plaintext(t *testing.T) {
	dir := t.TempDir()
	writeSnapshotToDir(t, dir, nil)
	reopenAndAssertMarker(t, dir, nil)
}

func TestSnapshotLoad_EnvelopeRoundTrip_Encrypted(t *testing.T) {
	dir := t.TempDir()
	engine := testEncryptionEngine(t)
	writeSnapshotToDir(t, dir, engine)
	reopenAndAssertMarker(t, dir, engine)
}

// Legacy formats: produced by stripping the envelope off a new-format file
// (the payload bytes are identical to what pre-M-14 binaries wrote).

func TestSnapshotLoad_LegacyHeaderlessPlaintext(t *testing.T) {
	data := writeSnapshotToDir(t, t.TempDir(), nil)
	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, "snapshot.json"), data[snapshotHeaderSize:], 0o600); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}
	reopenAndAssertMarker(t, legacyDir, nil)
}

func TestSnapshotLoad_LegacyHeaderlessEncrypted(t *testing.T) {
	engine := testEncryptionEngine(t)
	data := writeSnapshotToDir(t, t.TempDir(), nil) // plaintext, then encrypt by hand
	encrypted, err := engine.Encrypt(data[snapshotHeaderSize:])
	if err != nil {
		t.Fatalf("encrypt legacy payload: %v", err)
	}
	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, "snapshot.json"), encrypted, 0o600); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}
	reopenAndAssertMarker(t, legacyDir, engine)
}

// Regression: loading a legacy headerless encrypted snapshot must not depend
// on the ciphertext's first byte. The stored payload begins with the random
// GCM nonce, so ~0.8% of the time its first byte is '{' or '[' — which the old
// first-byte heuristic (persistence.go) mis-read as plaintext JSON, skipping
// decryption and failing json.Unmarshal on ciphertext. That surfaced as a CI
// flake (~1/128 loads). Here we synthesize that exact adversarial input
// deterministically so the regression cannot hide behind randomness.
func TestSnapshotLoad_LegacyHeaderlessEncrypted_CiphertextStartsLikeJSON(t *testing.T) {
	engine := testEncryptionEngine(t)
	plain := writeSnapshotToDir(t, t.TempDir(), nil)[snapshotHeaderSize:]

	// Encrypt until the ciphertext's first byte collides with a JSON opener.
	// Expected ~128 tries (2/256); the cap is a safety valve, not a real bound.
	var encrypted []byte
	for i := 0; i < 100000; i++ {
		enc, err := engine.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt legacy payload: %v", err)
		}
		if enc[0] == '{' || enc[0] == '[' {
			encrypted = enc
			break
		}
	}
	if encrypted == nil {
		t.Fatal("could not synthesize ciphertext starting with '{' or '[' — encryption format changed?")
	}

	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, "snapshot.json"), encrypted, 0o600); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}
	reopenAndAssertMarker(t, legacyDir, engine)
}

func TestSnapshotLoad_EncryptedEnvelopeWithoutEngineFails(t *testing.T) {
	dir := t.TempDir()
	writeSnapshotToDir(t, dir, testEncryptionEngine(t))

	_, err := NewGraphStorageWithConfig(jsonConfig(dir)) // no engine configured
	if err == nil {
		t.Fatalf("expected load of encrypted snapshot without engine to fail")
	}
	if !strings.Contains(err.Error(), "encrypt") {
		t.Fatalf("error should be operator-actionable about encryption, got: %v", err)
	}
}

func TestSnapshotLoad_UnknownVersionFails(t *testing.T) {
	dir := t.TempDir()
	data := writeSnapshotToDir(t, dir, nil)
	binary.BigEndian.PutUint32(data[4:8], snapshotFormatVersion+1)
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), data, 0o600); err != nil {
		t.Fatalf("rewrite snapshot: %v", err)
	}

	_, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err == nil {
		t.Fatalf("expected unknown snapshot version to fail loudly")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("error should name the unsupported version, got: %v", err)
	}
}

package storage

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// H-3 (AUDIT_security_2026-06-10): SetEncryption used to cover only
// snapshot.json — every WAL entry (everything since the last snapshot)
// was raw JSON, silent partial coverage for an operator who enabled
// encryption. WAL entry payloads are now sealed through the same engine,
// marked with the walEncMagic prefix; legacy plaintext entries replay
// unchanged and are purged by a one-time CompactWAL at startup.

const h3Marker = "wal-encryption-PII-marker-5d8a2f0c4e7b1963"

var h3WALMarker = base64.StdEncoding.EncodeToString([]byte(h3Marker))

func encryptedStorageConfig(t *testing.T, dir string, batched bool) StorageConfig {
	t.Helper()
	cfg := DefaultStorageConfig(dir)
	cfg.EncryptionEngine = testEncryptionEngine(t)
	cfg.KeyManager = &mockKeyProvider{}
	if batched {
		cfg.EnableBatching = true
		cfg.BatchSize = 8
	}
	return cfg
}

func readPlainWALEntries(t *testing.T, dir string) []*wal.Entry {
	t.Helper()
	w, err := wal.NewWAL(filepath.Join(dir, "wal"))
	if err != nil {
		t.Fatalf("open WAL for inspection: %v", err)
	}
	defer w.Close()
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return entries
}

func createH3Node(t *testing.T, gs *GraphStorage) *Node {
	t.Helper()
	node, err := gs.CreateNodeWithTenant("h3", []string{"Doc"}, map[string]Value{
		"secret": StringValue(h3Marker),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	return node
}

func TestWALEncryption_EntriesAreCiphertext(t *testing.T) {
	for _, batched := range []bool{false, true} {
		name := "plain"
		if batched {
			name = "batched"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			gs, err := NewGraphStorageWithConfig(encryptedStorageConfig(t, dir, batched))
			if err != nil {
				t.Fatalf("NewGraphStorageWithConfig: %v", err)
			}
			defer func() { _ = gs.Close() }()
			createH3Node(t, gs)

			entries := readPlainWALEntries(t, dir)
			if len(entries) == 0 {
				t.Fatalf("expected WAL entries")
			}
			for _, e := range entries {
				if strings.Contains(string(e.Data), h3WALMarker) {
					t.Fatalf("WAL entry LSN=%d carries plaintext payload despite encryption", e.LSN)
				}
				if !bytes.HasPrefix(e.Data, walEncMagic[:]) {
					t.Fatalf("WAL entry LSN=%d missing encrypted-payload marker", e.LSN)
				}
			}
		})
	}
}

func TestWALEncryption_CrashRecoveryRoundTrip(t *testing.T) {
	for _, batched := range []bool{false, true} {
		name := "plain"
		if batched {
			name = "batched"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := encryptedStorageConfig(t, dir, batched)
			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("NewGraphStorageWithConfig: %v", err)
			}
			node := createH3Node(t, gs)

			// Crash-sim: no Close — recovery must decrypt the WAL.
			gs2, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("crash-recovery reopen: %v", err)
			}
			defer func() { _ = gs2.Close() }()
			recovered, err := gs2.GetNodeForTenant(node.ID, "h3")
			if err != nil {
				t.Fatalf("node lost across encrypted-WAL recovery: %v", err)
			}
			if got := string(recovered.Properties["secret"].Data); got != h3Marker {
				t.Fatalf("recovered property mismatch: %q", got)
			}
		})
	}
}

// Transaction commits go through appendWALBatch — a separate append site
// that must seal too.
func TestWALEncryption_TransactionCommitSealed(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(encryptedStorageConfig(t, dir, false))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()
	tx, err := gs.BeginTransactionForTenant("h3")
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.CreateNode([]string{"T"}, map[string]Value{"secret": StringValue(h3Marker)}); err != nil {
		t.Fatalf("tx create: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx commit: %v", err)
	}

	for _, e := range readPlainWALEntries(t, dir) {
		if strings.Contains(string(e.Data), h3WALMarker) {
			t.Fatalf("transaction WAL entry LSN=%d is plaintext despite encryption", e.LSN)
		}
	}
}

// Enabling encryption on an existing database: legacy plaintext entries
// must still replay, and the startup must purge them (CompactWAL) so the
// old plaintext does not linger next to new ciphertext (the audit's
// toggle-orphan concern, gated on M-1).
func TestWALEncryption_LegacyPlaintextReplaysAndIsPurged(t *testing.T) {
	dir := t.TempDir()
	plainCfg := DefaultStorageConfig(dir)
	gs, err := NewGraphStorageWithConfig(plainCfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	node := createH3Node(t, gs)
	// Crash-sim: leave the plaintext entry in the WAL (no Close).

	encCfg := encryptedStorageConfig(t, dir, false)
	gs2, err := NewGraphStorageWithConfig(encCfg)
	if err != nil {
		t.Fatalf("reopen with encryption enabled: %v", err)
	}
	defer func() { _ = gs2.Close() }()
	if _, err := gs2.GetNodeForTenant(node.ID, "h3"); err != nil {
		t.Fatalf("legacy plaintext entry lost when enabling encryption: %v", err)
	}

	// The plaintext payload must be gone from disk after the toggle.
	for _, e := range readPlainWALEntries(t, dir) {
		if strings.Contains(string(e.Data), h3WALMarker) {
			t.Fatalf("legacy plaintext entry survived the encryption toggle (LSN=%d)", e.LSN)
		}
	}
}

func TestWALEncryption_CiphertextWithoutEngineFailsLoud(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(encryptedStorageConfig(t, dir, false))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	createH3Node(t, gs)
	// Crash-sim, then reopen WITHOUT an engine: replaying ciphertext
	// silently as garbage would be corruption — must fail loud.
	_, err = NewGraphStorageWithConfig(DefaultStorageConfig(dir))
	if err == nil {
		t.Fatalf("expected reopen without engine to fail on encrypted WAL entries")
	}
	if !strings.Contains(err.Error(), "encrypt") {
		t.Fatalf("error should be operator-actionable about encryption, got: %v", err)
	}
}

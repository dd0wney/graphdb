# Encryption Key Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An encrypted store refuses a wrong key at open, before it writes anything, by checking a sealed `keycheck.json` in its data directory.

**Architecture:** A small codec (`pkg/storage/keycheck.go`) seals and verifies a known value with the store's own `encryption.EncryptDecrypter`. The constructor verifies the file right after the data-directory lock and writes it as its last step; `SetEncryption` keeps it in step with `rules.json`; the backup archive carries it.

**Tech Stack:** Go 1.27, `crypto/rand`, `encoding/json`, `encoding/base64`; existing `pkg/encryption`, `pkg/vfs`, `pkg/vfs/vfstest`.

**Spec:** `docs/internals/design/SPEC_encryption_key_check_2026-11-07.md` (approved 2026-11-07). Code facts read at `34dc8d0`.

## Global Constraints

- File name `keycheck.json` in the data directory; temp file `keycheck.json.new`.
- Format `{"version": 1, "sealed": "<std base64 of engine.Encrypt(plaintext)>"}`.
- `plaintext` = the 20 bytes `graphdb-keycheck-v1\n` followed by 32 bytes from `crypto/rand`.
- A check passes only when `Decrypt` returns no error **and** the result starts with that prefix.
- No change to `snapshot.json`, `snapshot.mmap`, the WAL, `rules.json` or the backup manifest schema.
- Never rewrite or delete a `keycheck.json` that fails to parse.
- Every new test is seen red first; the PR records each failure line (repo `CLAUDE.md`, "Red-first").
- Gates: `gofmt -s -l ./pkg ./cmd` empty; `go vet ./pkg/storage/ ./pkg/backup/`; `go test ./pkg/storage/ -short -timeout 300s -count=1`; `go test -race ./pkg/storage/ -count=3 -timeout 600s`; `go test ./pkg/backup/ ./cmd/graphdb-admin/ -count=1`; `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./pkg/... ./cmd/...` reports 0 issues.

## Review Focus

1. A wrong-key open in a **different WAL mode** from the last open (for example `EnableCompression` on a store that used the plain WAL) creates no `wal_compressed.log` and no other file. Test in Task 2.
2. A leftover `keycheck.json.new` from a crash is ignored at open and replaced by the next write. Test in Task 1.
3. A legacy headerless snapshot opened with a wrong key fails to open and leaves no `keycheck.json`. Test in Task 2.
4. A directory that holds only `LOCK` and `keycheck.json` (data wiped by hand) opens with the right key and refuses a wrong one. Test in Task 2.
5. `RegisterUniquenessRule` racing `SetEncryption` stays race-free with the extra write. The existing `RegisterAndSetEncryptionDoNotRace` test covers it; Task 3 runs it under `-race`.

---

### Task 1: The key-check codec and its file

**Files:**
- Create: `pkg/storage/keycheck.go`
- Modify: `pkg/storage/errors.go` (add three sentinels beside `ErrDataDirLocked`)
- Test: `pkg/storage/keycheck_test.go`

**Interfaces:**
- Produces:
  - `var ErrEncryptionKeyMismatch = errors.New("the encryption key does not match this data directory")`
  - `var ErrKeyCheckDamaged = errors.New("keycheck.json is damaged")`
  - `var ErrEncryptionRequired = errors.New("the data directory is encrypted but no encryption engine is configured")`
  - `func keyCheckPath(dataDir string) string`
  - `func sealKeyCheck(engine encryption.EncryptDecrypter) ([]byte, error)` — the file bytes.
  - `func verifyKeyCheck(data []byte, engine encryption.EncryptDecrypter) error` — nil, or wraps one of the first two sentinels.
  - `func (gs *GraphStorage) writeKeyCheck(engine encryption.EncryptDecrypter) error` — crash-safe write through `gs.fs`; a nil engine removes the file (a missing file is not an error) and syncs the parent directory.

- [ ] **Step 1: Write the failing tests** in `keycheck_test.go`:
  - `TestKeyCheck_RoundTrip`: `sealKeyCheck(e1)` then `verifyKeyCheck(data, e1)` returns nil; two seals of one engine give different bytes.
  - `TestKeyCheck_WrongKey`: `verifyKeyCheck(sealed by e1, e2)` is `errors.Is(..., ErrEncryptionKeyMismatch)`. `e2` is `encryption.NewEngine(bytes.Repeat([]byte("w"), 32))`; `e1` is `testEncryptionEngine(t)`.
  - `TestKeyCheck_PrefixChecked`: with an engine whose `Decrypt` returns its input unchanged (a test type), a `sealed` value without the prefix gives `ErrEncryptionKeyMismatch`.
  - `TestKeyCheck_Damaged`: each of `{`, `{"version":2,"sealed":"AA=="}`, `{"version":1,"sealed":"!!"}` and an empty file gives `ErrKeyCheckDamaged`.
  - `TestWriteKeyCheck_CrashLeavesOldFile`: with `vfstest.NewFaults` failing the rename (or the sync of `keycheck.json.new`), `writeKeyCheck` returns an error and the old `keycheck.json` bytes are unchanged.
  - `TestWriteKeyCheck_NilEngineRemoves`: after a write with `e1`, `writeKeyCheck(nil)` removes the file; a second `writeKeyCheck(nil)` returns nil.
  - `TestWriteKeyCheck_ReplacesLeftoverTemp`: a stale `keycheck.json.new` with junk bytes does not stop `writeKeyCheck(e1)`, and the result verifies.
- [ ] **Step 2: Run** `go test ./pkg/storage/ -run 'TestKeyCheck_|TestWriteKeyCheck_' -count=1`. Expected: FAIL (undefined: `sealKeyCheck` and the rest). Record the output.
- [ ] **Step 3: Implement** the Interfaces above. Mirror `writeUniquenessRulesLocked` (`pkg/storage/uniqueness_rules.go:377`) for the write sequence: open `keycheck.json.new` with `O_TRUNC`, write, `Sync`, `Close`, `Rename`, `vfs.SyncParentDir`. An error never carries the file bytes.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat(storage): add a sealed key-check file codec`.

### Task 2: Check at open, write at the end of a successful open

**Files:**
- Modify: `pkg/storage/storage.go` (check between the `Ephemeral` guard and the WAL constructors, at `storage.go:148-155` at `34dc8d0`; write just before `opened = true`, `storage.go:376`)
- Modify: `pkg/storage/storage_types.go` (add `keyCheckExisted bool` beside `rulesFileExisted`)
- Modify: `pkg/storage/uniqueness_rules_seal_fix_test.go` (the existing probe)
- Test: `pkg/storage/keycheck_open_test.go`

**Interfaces:**
- Consumes: Task 1's `keyCheckPath`, `verifyKeyCheck`, `writeKeyCheck` and the three sentinels.
- Produces: `func (gs *GraphStorage) checkKeyAtOpen() error` — reads the file through `gs.fs`; absent: nil; present with an engine: `verifyKeyCheck`, and on success sets `gs.encryptionKeyProven = true` and `gs.keyCheckExisted = true`; present without an engine: `ErrEncryptionRequired` wrapped with the path. Skipped for an `Ephemeral` store.

Write rule (the plan's call, from spec §3.2 row 4): after every load and after `sealPlaintextRulesAfterOpen`, when `gs.encryptionEngine != nil && !gs.ephemeral && !gs.keyCheckExisted`, call `gs.writeKeyCheck(gs.encryptionEngine)`; an error fails the open. A successful open has either proved the key or found no ciphertext, so no further condition is needed.

- [ ] **Step 1: Write the failing tests** in `keycheck_open_test.go` (helper `snapshotFiles(t, dir) map[string][]byte` reads every regular file except `LOCK`):
  - `TestKeyCheck_WrongKeyRefusedBeforeAnyWrite`: create with `e1` (one node), `Close`. Record `snapshotFiles`. Open with `e2`: `errors.Is(err, ErrEncryptionKeyMismatch)`; `snapshotFiles` equal before and after. Then open with `e1`: succeeds and the node is there.
  - `TestKeyCheck_WrongKeyOtherWALModeCreatesNothing`: create with `e1` on `jsonConfig` (plain WAL), `Close`. Open with `e2` and `EnableCompression: true`: refused, and `wal/wal_compressed.log` does not exist. (Review Focus 1.)
  - `TestKeyCheck_NoEngineRefused`: create with `e1`, `Close`; open with no engine: `errors.Is(err, ErrEncryptionRequired)`.
  - `TestKeyCheck_DamagedRefusedAndKept`: create with `e1`, `Close`, overwrite `keycheck.json` with `{`; open with `e1`: `ErrKeyCheckDamaged`, file bytes still `{`.
  - `TestKeyCheck_UpgradeWritesOnRightKey`: create an encrypted store with a snapshot, `Close`, delete `keycheck.json` (simulates a pre-change store). Open with `e2` first: refused (snapshot decrypt), and `keycheck.json` still absent. Open with `e1`: succeeds, and `keycheck.json` verifies with `e1`.
  - `TestKeyCheck_LegacySnapshotWrongKeyWritesNothing`: write a headerless encrypted legacy snapshot (as `snapshot_envelope_test.go` builds one) with `e1`; open with `e2`: fails, and no `keycheck.json`. (Review Focus 3.)
  - `TestKeyCheck_OnlyKeyCheckLeft`: create with `e1`, `Close`, remove every file except `LOCK` and `keycheck.json`; open with `e1` succeeds; open with `e2` refused. (Review Focus 4.)
  - `TestKeyCheck_FailedWriteFailsOpen`: a fresh encrypted open on a `vfstest` FS that fails the `keycheck.json.new` write returns an error.
- [ ] **Step 2: Change the existing probe** `TestProbe_WrongKeyOpenThenCloseLocksOutRightKey`: remove `t.Skip`, rename it `TestKeyCheck_ResidualFirstKeyIsTrusted`, and assert the spec §7 residual: with a hand-made version 1 `rules.json` and no other file, the K2 open succeeds and writes a `keycheck.json` that verifies with K2.
- [ ] **Step 3: Run** `go test ./pkg/storage/ -run 'TestKeyCheck_' -count=1`. Expected: FAIL (`TestKeyCheck_WrongKeyRefusedBeforeAnyWrite` opens with `e2`; the other new tests fail on the missing behaviour). Record the output.
- [ ] **Step 4: Implement** `checkKeyAtOpen` and the write rule.
- [ ] **Step 5: Run** the same command, then `TestEncryptedStore_WritesNoPlaintext` (it must stay green: `keycheck.json` holds no marker). Expected: PASS.
- [ ] **Step 6: Commit** `feat(storage): refuse a wrong encryption key at open`.

### Task 3: Keep `keycheck.json` in step in `SetEncryption`

**Files:**
- Modify: `pkg/storage/persistence.go` (`SetEncryption`, `persistence.go:521`)
- Test: `pkg/storage/keycheck_setencryption_test.go`

**Interfaces:**
- Consumes: Task 1's `writeKeyCheck`; the existing `rewriteUniquenessRulesForEngine` (`uniqueness_rules.go`).

Order (the plan's call): read the original bytes of `rules.json` (or note its absence) and the current `rulesFileWasPlaintext`; rewrite `rules.json`; write `keycheck.json`; swap the engine. If the `keycheck.json` write fails, restore `rules.json` to its original bytes (or remove it if it was absent) with the same crash-safe write, restore `rulesFileWasPlaintext`, leave the engine unchanged, and return the error. Never re-seal under the old engine. If the restore also fails, the error names both files.

- [ ] **Step 1: Write the failing tests:**
  - `TestSetEncryption_RewritesKeyCheck`: open with `e1`, register a rule, `SetEncryption(e2)`; `keycheck.json` verifies with `e2` and not with `e1`.
  - `TestSetEncryption_NilRemovesKeyCheck`: after `SetEncryption(nil)`, `keycheck.json` is absent, and a reopen without an engine succeeds.
  - `TestSetEncryption_KeyCheckFailureRestoresRules`: with a `vfstest` FS that fails the `keycheck.json.new` write, `SetEncryption(e2)` returns an error; `rules.json` bytes equal the bytes before the call; a reopen with `e1` succeeds.
- [ ] **Step 2: Run** `go test ./pkg/storage/ -run 'TestSetEncryption_' -count=1`. Expected: FAIL. Record the output.
- [ ] **Step 3: Implement** the order above in `SetEncryption`. Keep the lock order `rulesMu`, then `gs.mu`.
- [ ] **Step 4: Run** the same command and `go test -race ./pkg/storage/ -run 'RegisterAndSetEncryptionDoNotRace|TestSetEncryption_' -count=5`. Expected: PASS, no race. (Review Focus 5.)
- [ ] **Step 5: Commit** `feat(storage): keep keycheck.json in step in SetEncryption`.

### Task 4: Backup, documents and the changelog

**Files:**
- Modify: `pkg/backup/archive.go` (`entriesWithFS`, beside the `rules.json` entry)
- Test: `pkg/backup/keycheck_test.go`
- Modify: `docs/internals/design/ENCRYPTION_ARCHITECTURE.md` (a "Key check" section: the file, the open order, the residual of spec §7)
- Modify: `CLAUDE.md` § "Snapshot format stability" (one sentence: `keycheck.json` is a fourth artefact with its own version)
- Modify: the `graphdb-admin backup restore` documentation (find it with `grep -rln "backup restore" docs README.md`): a `restore --force` over a directory with a `keycheck.json` from another key leaves that file; remove it first.
- Modify: `CHANGELOG.md` under `[Unreleased]` → `### Security`

- [ ] **Step 1: Write the failing test** `TestArchive_IncludesKeyCheck`: an archive of a data directory that holds `keycheck.json` lists it in the manifest, and an extract restores it byte for byte. Then open the extracted directory with `storage`: `e1` succeeds and `e2` gives `ErrEncryptionKeyMismatch` (spec §5 test 9). If a `pkg/backup` test cannot import `pkg/storage` without a cycle, put this second half in `pkg/storage` as a test that calls the backup archive and extract functions.
- [ ] **Step 2: Run** `go test ./pkg/backup/ -run TestArchive_IncludesKeyCheck -count=1`. Expected: FAIL. Record the output.
- [ ] **Step 3: Implement** the `entriesWithFS` change ("if present", as `rules.json`).
- [ ] **Step 4: Run** `go test ./pkg/backup/ ./cmd/graphdb-admin/ -count=1`. Expected: PASS.
- [ ] **Step 5: Write the documents** listed above.
- [ ] **Step 6: Run every gate** in Global Constraints. Expected: all pass.
- [ ] **Step 7: Commit** `feat(backup): carry keycheck.json, and document the key check`.

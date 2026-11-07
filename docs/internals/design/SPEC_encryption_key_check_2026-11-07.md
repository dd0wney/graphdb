# SPEC: an encryption key check for the data directory

**Date**: 2026-11-07 · **Status**: draft for the user's approval (revised after a builder check) · **Coord task**:
`graphdb:v1.4-encryption-key-check-artefact` · **Track**: E (encryption at rest), item 5 in
`docs/NEXT_STEPS_2026-10-08.md`. Code facts below were read at `0ff4483`; re-read before acting.

## 1. The problem

An encrypted store proves its key only by decrypting something it already holds: an enveloped
`snapshot.json` (`loadFromDisk`, `pkg/storage/persistence.go`), a sealed WAL entry
(`replayEntry`), or a version 2 `rules.json` (#721). A data directory with **no ciphertext** —
a fresh directory, one created by a binary from before encryption was switched on, or a store
whose process stopped before its first snapshot — cannot tell a right key from a wrong one.

The failure that follows is a lockout, not a refusal. `TestProbe_WrongKeyOpenThenCloseLocksOutRightKey`
(#721, skipped) shows it:

1. An encrypted store with key K1 has rules (`rules.json` version 1), no snapshot and no WAL.
2. An open with a wrong key K2 succeeds: the missing snapshot reads as a fresh database.
3. Its `Close` writes `snapshot.json` under K2.
4. The next open with K1 fails with `failed to decrypt snapshot: authentication failed`.

The operator now holds the right key and cannot open the store, and nothing told them at step 2.

## 2. Goals, non-goals, anti-goals

**Goals**

- An open with a wrong key fails **before it writes anything**, whenever the directory was last
  opened by a binary that has this change.
- The refusal is loud and names the cause: a distinct error, with the file it checked.
- No change to `snapshot.json`, the WAL, `rules.json` or the backup manifest schema.

**Non-goals**

- Proving the key of a directory that holds no ciphertext **and** no key check, that is, one
  last written by an older binary. Nothing on disk records the old key, so no design can. §7
  states this residual.
- Key management: where keys live, how they rotate inside a `KeyProvider`.
- Zeroing keys in memory (`v1.4-encryption-key-zeroing`).

**Anti-goals** (the design has failed if it does these, even if every test passes)

- It silently regenerates a damaged or unreadable key check. That turns the check into a way to
  accept any key.
- It makes an existing encrypted store unopenable after an upgrade, with its right key.
- It writes the key or a plaintext hash of the key to disk.

## 3. Design

### 3.1 The artefact

A fourth file in the data directory, `keycheck.json`, beside `snapshot.*`, `wal/` and
`rules.json`, with its own version field, independent of every other format:

```json
{"version": 1, "sealed": "<base64 of engine.Encrypt(plaintext)>"}
```

`plaintext` is the 20-byte prefix `graphdb-keycheck-v1\n` followed by 32 random bytes from
`crypto/rand`. The check passes when `engine.Decrypt(sealed)` returns no error **and** the result
starts with that prefix. AES-GCM's tag already authenticates the decrypt; the prefix guards a
custom `EncryptDecrypter` that does not authenticate. The random part means no two stores share
a sealed value.

Why the engine and not a hash of the key: `EncryptDecrypter` has no way to expose its key (by
design: `pkg/encryption/interfaces.go`), engines may be KMS-backed, and a sealed value reveals
nothing an attacker cannot already test against the sealed snapshot.

It is written with the same crash-safe sequence as `rules.json` (`writeUniquenessRulesLocked`:
temp file, `Sync`, `Rename`, `vfs.SyncParentDir`), so a crash leaves the old file or the new one.

### 3.2 At open

| Engine configured | `keycheck.json` | Behaviour |
|---|---|---|
| yes | present, decrypts with the prefix | Key proven (`encryptionKeyProven = true`) before any load. Open continues. |
| yes | present, decrypt fails or prefix wrong | **Refuse** with `ErrEncryptionKeyMismatch`, before any load and any write. |
| yes | present, not parseable or unknown version | **Refuse** with `ErrKeyCheckDamaged`. Never rewrite it. |
| yes | absent | Open as today. As the **last step of a successful constructor**, write `keycheck.json` if the key is proven (a ciphertext artefact decrypted) or no ciphertext artefact exists. |
| no | present | **Refuse**: the store is encrypted and no engine is configured (the same shape as the existing `snapshot is encrypted but encryption is not enabled` error). |
| no | absent | As today. |

**Where the check runs.** After `lockDataDir` and before the WAL constructors
(`pkg/storage/storage.go:150-157` at `0ff4483`). "Before `loadFromDisk`" is too late: the WAL
constructors already create `wal/` and the WAL file (`wal/wal.go:68,74`,
`wal/compressed_wal.go:30`), and the edge store creates `edgestore/` (`lsm/lsm.go:16`), so a
wrong-key open in a different WAL mode would create a file. The two effects that precede the
check, `MkdirAll` of the data directory and the `LOCK` file, already exist after any earlier
open of that directory.

**Where the write runs.** As the last step of the constructor, after every load, after the WAL
purge (`CompactWAL` after a plaintext replay) and after `sealPlaintextRulesAfterOpen`. A legacy
headerless snapshot that fails to decrypt is read as plaintext (`persistence.go`, the `legacy`
branch), so "no ciphertext" is only known once the loads have succeeded. A failed write fails
the open, as a failed `sealPlaintextRulesAfterOpen` already does (*author's call*: a store that
cannot record its key must not run as if it had).

**"No ciphertext artefact exists"** means `!encryptionKeyProven` after a successful load: a
successful open either decrypts each ciphertext it meets or fails. Two notes for the builder:
`edgestore/`, `auth/` and `lsa/` are never encrypted, so "no ciphertext" does not mean "no
data"; and WAL entries at or below the snapshot's `WALBoundaryLSN` are skipped before decryption
(`persistence_replay.go:44`), so superseded sealed entries can exist while the flag stays false.
They are superseded, so this is harmless.

A passing check also lets `sealPlaintextRulesAfterOpen` seal a version 1 `rules.json` on that
open (#721 waits for a proven key), which closes the window in which `rules.json` stays
plaintext.

An `Ephemeral` store writes and checks nothing: it has no files. `BTreeGraphStorage` is out of
scope (its `SetEncryption` is a no-op today, `btree_storage.go:689`).

### 3.3 At `SetEncryption`

`SetEncryption` already rewrites `rules.json` under the new engine before it swaps the engine
(#721). It must also rewrite `keycheck.json` (or remove it for a nil engine); otherwise the key
check itself would refuse the new key after a correct change. The invariant: when
`SetEncryption` returns, `keycheck.json` and `rules.json` are sealed under the engine it returns
with, whether it returned an error or not. If the second write fails, it restores the **original
bytes** of the first file (and `rulesFileWasPlaintext`) and returns the error. It must not
re-seal under the old engine: that would seal a file under a key the open never proved, the
#721 hazard. If the restore also fails, the error names both files and the key each is under.

Only tests call `storage.GraphStorage.SetEncryption` today; the server configures encryption
through `StorageConfig` at open (`api.Server.SetEncryption` is a different method). So this path
is library-only.

**Out of scope, found while writing this spec: `SetEncryption` on a store that holds data is
unsafe** (decision 1 in §9). The builder check confirmed three defects, none caused by the key
check:

- WAL entries written after the call are sealed under the new key (`sealWALPayload` uses the
  engine at write time), while earlier entries stay under the old one. A crash before the next
  snapshot leaves a WAL that neither key replays. This is older than #721.
- The snapshot stays under the old key until the next snapshot or `Close`, so the same crash also
  fails on `snapshot.json` (K2) or `rules.json` (K1).
- On an mmap store (the default), no mmap code reads `encryptionEngine`, so `SetEncryption(K)`
  then `Close` writes a **plaintext** `snapshot.mmap`; the next open with K refuses the directory.

A fix needs `SetEncryption` to hold writers out across a snapshot rewrite, a WAL boundary, both
small files and the swap, and to refuse or convert an mmap store. That is its own design.

### 3.4 Backup and restore

`pkg/backup/archive.go` copies an explicit list: the snapshot, `rules.json` if present, and the
`wal`, `auth`, `lsa` and `edgestore` directories. `keycheck.json` joins that list on the same
"if present" rule as `rules.json`. The manifest lists each file with its size and SHA-256, and
`Verify` checks an archive only against its own manifest (`backup/verify.go:73-90`), so the new
member is additive for an older `graphdb-admin`. A restore of an older backup without it is
safe: the restored snapshot proves the key and the first open writes the file.

One stale-file case, to document: `restore --force` writes over a non-empty target and removes
no extra file (`cmd/graphdb-admin/backup.go:125`). Restoring an archive without `keycheck.json`
over a directory that has one from another key leaves the stale file, which then refuses the
right key. That is a loud refusal, not data loss; the restore documentation says to remove it.

### 3.5 Upgrade and downgrade

- **Upgrade** (an existing encrypted store, no `keycheck.json`): the first open with the right
  key proves it through the snapshot or the WAL and writes the file. An open with a wrong key
  fails on the snapshot as it does today, and writes nothing.
- **Downgrade**: an older binary does not read `keycheck.json` and ignores it. It loses the new
  protection but opens the store as before.

## 4. Errors

Two new sentinel errors in `pkg/storage/errors.go`, each wrapped with the file path:
`ErrEncryptionKeyMismatch` and `ErrKeyCheckDamaged`. The no-engine refusal can reuse a wrapped
`ErrEncryptionKeyMismatch` or get its own sentinel (builder's call). No error leaves the
key-check bytes in its text.

## 5. Tests (each seen red first)

1. **The probe**: a new test creates the store with K1 through the new binary (so `keycheck.json`
   exists), then opens with K2: refused, no file changed, and K1 opens afterwards. The existing
   `TestProbe_WrongKeyOpenThenCloseLocksOutRightKey` builds `rules.json` by hand with no K1 open,
   which is the §7 residual; it stays, loses its `t.Skip`, and asserts the residual behaviour (K2
   accepted) so that a change to it is visible.
2. A wrong-key open with `keycheck.json` present changes **no** file in the directory (compare
   names, sizes and contents before and after).
3. A damaged `keycheck.json` (truncated JSON, unknown version, bad base64) refuses with
   `ErrKeyCheckDamaged` and is left byte for byte as it was.
4. An open with no engine and `keycheck.json` present refuses.
5. Upgrade: an encrypted store with a snapshot and no `keycheck.json` gains one on the first
   right-key open; a wrong-key open first writes nothing.
6. `SetEncryption` to a new engine, then nil: after each, the files agree with the engine,
   including after an injected failure of the second write (FaultFS, as the #721 tests do).
7. Crash safety: a fault between the temp write and the rename leaves the old file intact.
8. `TestEncryptedStore_WritesNoPlaintext` keeps passing: `keycheck.json` holds no marker.
9. Backup round trip: an archive of an encrypted store contains `keycheck.json`, and a restore
   opens with the right key and refuses a wrong one.

## 6. Documents to update

- ADR 0001 is not the right home; this is a new artefact. Add a short ADR
  (`docs/adr/0002-encryption-key-check.md` or the next free number) or a section in
  `docs/internals/design/ENCRYPTION_ARCHITECTURE.md`, as the builder judges.
- `CLAUDE.md` § "Snapshot format stability" lists the on-disk artefacts; it gains the fourth.
- `CHANGELOG.md` under `[Unreleased]`.

## 7. Residual risk

A directory last written by a binary from before this change, holding no ciphertext, still
accepts the first key it is opened with: that open writes `keycheck.json` under whatever key it
was given. Nothing on disk records the earlier key, so this case cannot be closed after the
fact. It shrinks to nothing as stores are opened once by a binary with this change.

## 8. Alternatives considered

| Option | Pros | Cons |
|---|---|---|
| **A. Sealed key-check file (this spec)** | Small, independent of every data format; works with any engine; detects a wrong key before any write | A fourth artefact to back up and document; the pre-upgrade residual in §7 |
| B. A plaintext hash or HMAC of the key | Simple to verify | Needs access to the raw key, which `EncryptDecrypter` does not give; a hash of a weak key is an offline guessing target |
| C. Always write an encrypted snapshot at open, so the store is self-proving | No new file | A snapshot write on every open of a store without one; couples key proof to the snapshot format and to mmap (plaintext-only) |
| D. Refuse to open an encrypted store with no ciphertext unless an explicit "initialise" option is set | No new file; no trust on first use | Breaks every existing fresh-store open; the option becomes a habit and stops protecting anyone |

**Recommendation: A.** B cannot be built on the engine interface, C couples two concerns and
costs every open, and D moves the burden to every caller without removing the risk.

## 9. Decisions for the user

1. **`SetEncryption` on a store that holds data (§3.3).** Recommended: **out of scope**, as a new
   coord task, because its fix is a separate design (writers held out across a snapshot rewrite
   and a WAL boundary, and an mmap refusal). This spec only keeps `keycheck.json` in step with
   it. Only tests call it today. Alternative: put the full fix in this change, which roughly
   doubles its size and risk.
2. **File name.** Recommended: `keycheck.json`, beside `rules.json`.
3. **Release.** Recommended: a minor release (v1.13.0), because it adds an on-disk artefact and
   two error values, with no format bump of the existing files.

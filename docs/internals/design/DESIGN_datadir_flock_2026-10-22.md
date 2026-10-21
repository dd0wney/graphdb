# Design: an inter-process lock on the data directory — 2026-10-22

**Status**: approved for build. The user chose "proceed with recommendations" (2026-10-22), so every
decision marked **(recommended)** is the author's call, not the user's; §7 lists them for review.
**Coord task**: `graphdb:v1.4-datadir-flock`.
**Asked by**: `mapper-6b` (`oit-cyber/mapper`): a server and an embedded mapper can share a laptop,
and both default to opening a directory by path.
**Read at**: `ba0ef25`. Line numbers below are claims about that revision; re-read before you
navigate by them.

## 1. Problem

Nothing stops two processes opening one data directory. A case-sensitive search for `flock`,
`Flock`, `LOCK_EX` and `fcntl` in `pkg/` and `cmd/` finds no lock code (positive control: the same
search finds `func NewGraphStorage`). Two `GraphStorage` instances on one directory each replay the
WAL into their own memory, each append to `wal/wal.log`, and each write `snapshot.*` on Close. The
second Close's snapshot replaces the first's, and its WAL truncate empties what the first wrote
since. Writes acknowledged to one process are lost with no error anywhere.

Openers that can meet a running server on the same path (`cmd/server` defaults to `./data/server`):
`cmd/cli`, `cmd/tui`, `cmd/import-icij`, `cmd/import-dimacs`, the benchmarks, and any embedder.

## 2. Mechanism

`flock(fd, LOCK_EX|LOCK_NB)` on `<DataDir>/LOCK`, taken at open and held until Close.
**(recommended)**

| Option | Why not |
|---|---|
| `fcntl` (POSIX record) locks | Owned by the process, not the descriptor: closing ANY descriptor on the file in the same process drops the lock. `pkg/backup` and tests open files in the data directory by path. |
| PID file / `O_EXCL` create | Stale after a crash, and a crash is exactly when the next open must succeed. Needs liveness heuristics (PID reuse, containers sharing a PID namespace). |
| `File.Fd()` on `vfs.File` | `pkg/vfs/map.go` already rejects it: a fault driver has no descriptor, so the seam lets the OS driver through and locks every other driver out. |
| `flock` | Owned by the open file description, released by the kernel when the process dies. Two opens in ONE process also conflict, which catches an embedder that opens twice. |

`flock` on NFS is emulated with `fcntl` by Linux; that is out of scope (the store is not supported on
NFS today, and nothing changes that).

### 2.1 Through `pkg/vfs`, as an optional capability

`vfs.Locker` is an optional interface beside `vfs.Mapper`, for the same reason `Mapper` is optional:
`FileSystem` is a published interface, and a new method breaks every driver outside the repository.
**(recommended)**

```go
// Locker is an OPTIONAL capability: an exclusive, non-blocking lock on a file
// that the operating system releases when the holder dies.
type Locker interface {
	Lock(name string) (release func() error, err error)
}
var ErrLocked = errors.New("vfs: file is locked by another holder")
var ErrLockUnsupported = errors.New("vfs: driver cannot lock files")
```

- `osFS.Lock` on `unix` (`lock_unix.go`): `os.OpenFile(name, O_RDWR|O_CREATE, 0600)`, then
  `syscall.Flock(fd, LOCK_EX|LOCK_NB)`. `EWOULDBLOCK` maps to `ErrLocked`. `release` closes the
  descriptor, which drops the lock. No new module dependency (`syscall`, as `map_unix.go` uses).
- `osFS.Lock` on `!unix` (`lock_other.go`): `ErrLockUnsupported`. No release or CI job builds a
  non-unix target (`.goreleaser.yml`: linux, darwin; no workflow names windows).
- A driver without `Locker` (the fault drivers, `extfs`) gets no lock. Such a driver has no second
  process to exclude. `ErrLockUnsupported` is treated the same way. **(recommended)** The
  alternative, refusing to open, would fail every test driver for no protection gained.

### 2.2 The lock file

- `<DataDir>/LOCK`, mode `0600`, empty.
- Never deleted. Unlinking a lock file races: a waiter can hold the old inode while a third process
  creates a new file and locks that, and both believe they hold the directory.
- Not in a backup. `pkg/backup.entriesWithFS` archives a fixed list (`snapshot.*`, `rules.json`,
  `wal/`, `auth/`, `lsa/`, `edgestore/`), so `LOCK` is excluded without a change.

## 3. Placement in `pkg/storage`

- **Open** (`NewGraphStorageWithConfig`): take the lock directly after `MkdirAll` and before the WAL
  opens. Nothing that reads or writes the directory runs before it.
- **Failed open**: `releaseAfterFailedOpen` releases the lock last, after the WALs close. The
  `opened` defer already covers all twelve error returns after `MkdirAll`, so one line there covers
  every path.
- **Close**: release the lock as the last step, after the WAL closes. `Close` has no early return
  after its first-call check, so the release is always reached on the first Close; a second Close
  returns "storage already closed" and releases nothing, which is correct because nothing is held.

## 4. The refusal

```go
var ErrDataDirLocked = errors.New("data directory is locked by another open store")
type DataDirLockedError struct{ Dir string } // errors.Is(err, ErrDataDirLocked)
```

The message names the directory and says what to do: close the other process, or point this one at
a different `-data`. `cmd/server` and the other binaries already print the constructor's error and
exit non-zero, so no binary changes.

## 5. Crash-simulation tests

28 test files in `pkg/storage` simulate a crash with `testCrashableStorage` (56 call sites): a test
calls it once to write, leaves that store open, and calls it again on the same directory to recover.
(`testCrashRecovery` in the same file has no callers.) Hand-written variants exist in at least
`crash_recovery_test.go` and `failure_recovery_test.go`. In one process, `flock` makes the second
open refuse.

**(recommended)** An unexported `StorageConfig` field, `skipDataDirLock`, that only code in package
`storage` can set. `testCrashableStorage` sets it, and so does each hand-written variant. The
production lock cannot be turned off from outside the package.

| Option | Why not |
|---|---|
| An exported opt-out | Any embedder can disable the one check that stops silent data loss. |
| Release the abandoned store's lock in the helper | Most faithful (a dead process's lock is released by the kernel), but the helper takes only the directory, so 56 call sites change to pass the old store. Worth it only if a test needs the lock on the reopened store. |

The sweep is driven by the suite, not by grep: a test that opens a held directory now fails with
`ErrDataDirLocked` naming the directory, so each one is found by running `go test ./...` (the grep
for such tests gave a lower bound, 39 files by comment search).

## 6. Tests, red first

1. Second open of a held directory in the same process refuses: `errors.Is(err, ErrDataDirLocked)`,
   message names the directory.
2. Reopen after Close succeeds.
3. A failed open releases the lock: a corrupt `rules.json` fails the open; after repair the next open
   succeeds.
4. **Cross-process**: the test binary re-executes itself as a child that opens the directory and
   reports it holds it. The parent's open refuses. Then the parent kills the child with `SIGKILL`
   and the next open succeeds — the crash case a PID file gets wrong.
5. **The `fcntl` trap**: open `<DataDir>/LOCK` with `os.Open` and close it in the same process; a
   second store open still refuses.
6. A driver without `Locker` (`vfstest` fault driver) opens one directory twice without error.
7. `pkg/vfs`: `osFS.Lock` twice on one file gives `ErrLocked`; after release, it locks again.

## 7. Decisions for review

| # | Decision | Alternative |
|---|---|---|
| D1 | `flock` on `<DataDir>/LOCK` | `fcntl`; PID file |
| D2 | Optional `vfs.Locker`, beside `Mapper` | `syscall` directly in `pkg/storage` (bypasses the driver every other file operation uses) |
| D3 | No lock for a driver without `Locker`, or on `!unix` | Refuse to open |
| D4 | `LOCK` never deleted, empty | Write the PID for diagnostics (stale after a crash; `fuser`/`lsof` answer the question) |
| D5 | Unexported `skipDataDirLock` for crash tests | Exported opt-out; helper releases the abandoned lock |

## 8. Out of scope (follow-ups)

- `graphdb-admin backup restore --into` writes into a directory a running server can hold. It does
  not open a store, so it takes no lock. It could take the same lock for the length of the extract.
- `NewLSMGraphStorage` and `NewBTreeGraphStorage` take no lock. Their only non-test caller is
  `cmd/benchmark-graph-storage`.

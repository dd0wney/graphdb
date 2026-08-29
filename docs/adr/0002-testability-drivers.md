# 2. Testability drivers (VFS, fault injection, test control)

Date: 2026-08-28
Status: proposed

## Context

graphdb gained real fault-injection tests this week: an I/O fault seam in
`pkg/wal` (#466), crash and power-loss tests (#478), malformed-snapshot fuzzing
(#477). Each found a real defect. All of them share a flaw.

Every seam is test-only. `walFile` is unexported and only ever swapped inside a
`_test.go` file. `faultyFile` and `crashFile` are test types. The production
binary contains no way to make a disk fail, so the fault tests run against a
specially-constructed object, not against the artifact that ships.

SQLite draws exactly this distinction, and it is the reason TH3 exists at all.
Its TCL suite requires special compile-time options, so (in D. R. Hipp's words)
it "is really a source code test, not an object code test, because it's not
testing exactly the same code that you're delivering". TH3 was written to test
the delivered object code, and the mechanism that makes that possible is
published API present in every production build:

- `sqlite3_vfs_register` — the OS interface is a table of function pointers a
  caller can replace, so I/O errors, crashes and power loss are injected into
  the shipped library.
- `sqlite3_config(SQLITE_CONFIG_MALLOC, ...)` — the allocator is substitutable,
  which is how out-of-memory paths get tested.
- `sqlite3_test_control(...)` — documented "for testing purposes only", but
  compiled in, because omitting it would mean not testing what flies.

graphdb has none of these. The consequence is not hypothetical: `pkg/lsm` and
`pkg/btree` were repeatedly recorded this session as "blocked on no seam",
which is only true while a seam is conceived as a test-only hack rather than a
product feature.

Measured surface to migrate: 259 `os.*` call sites — `pkg/storage` 179,
`pkg/wal` 67, `pkg/lsm` 12, `pkg/btree` 1.

## Decision

Introduce driver interfaces as **production code**, and inject faults through
them rather than through test-only substitution.

### Driver 1 — `pkg/vfs`, the filesystem

```go
type FileSystem interface {
    Open(name string, flag int, perm os.FileMode) (File, error)
    Remove(name string) error
    Rename(oldpath, newpath string) error
    Stat(name string) (os.FileInfo, error)
    MkdirAll(path string, perm os.FileMode) error
    Name() string
}

type File interface {
    io.Reader; io.Writer; io.Seeker; io.Closer
    ReadAt(p []byte, off int64) (int, error)
    WriteAt(p []byte, off int64) (int, error)
    Sync() error
    Truncate(size int64) error
    Name() string
}
```

The OS-backed implementation is the default and is what ships. A registry
(`Register`, `Get`, `Default`) mirrors `sqlite3_vfs_register`, and
`StorageConfig` gains a field to select one.

### Driver 2 — `pkg/vfs/vfstest`, the wonky implementations

Fault, crash and torn-write filesystems live in their own package, not in
`_test.go` files. They are reachable by any package's tests **and by a
downstream consumer testing its own graphdb usage**, and they drive the
production code path because they are installed through the published registry.

This mirrors SQLite exactly: the *mechanism* ships, the wonky implementation is
a testing artifact that uses only published API.

### Driver 3 — allocation limits

Go cannot substitute `malloc`, so SQLite's OOM loop does not port directly. The
honest analogue is narrower: route graphdb's own large buffers (snapshot
assembly, LSM blocks, mmap staging) through an allocator interface that a test
can make fail. This covers the allocations that matter at graphdb's scale and
makes no claim about runtime-level exhaustion.

### Driver 4 — `never` / `always` / test control

Go equivalents of SQLite's `NEVER()`/`ALWAYS()` macros: no-ops in production,
panics under a debug build tag, and a published control surface for the
"disabled optimization" runs SQLite does (`SQLITE_TESTCTRL_OPTIMIZATIONS`).
graphdb already owns two differential oracles of this shape — JSON versus mmap
enumeration, and SIMD versus scalar — but has no general switch.

### Staging

1. `pkg/vfs` + `pkg/vfs/vfstest`, no callers migrated.
2. `pkg/wal` (67 sites). It carries the durability contract and already has
   `walFile` to replace.
3. `pkg/lsm` (12) and `pkg/btree` (1).
4. `pkg/storage` (179), the snapshot and mmap paths.
5. Drivers 3 and 4.

Each stage ships on its own and leaves the tree green.

## Alternatives Considered

| Option | Pros | Cons |
|---|---|---|
| **Production drivers (chosen)** | Fault tests exercise the shipped path; `pkg/lsm`/`pkg/btree` become testable; consumers can test their own usage | 259 call sites to migrate; a published interface to keep stable; an indirection on every file operation |
| Keep test-only seams (status quo) | No migration, no new public surface | Tests do not exercise what ships. `pkg/lsm` and `pkg/btree` stay untestable for I/O faults, which is where they are today |
| Per-package unexported seams (what #466 did) | Small, local | Repeats the same work per package, and still tests a substituted object rather than the delivered one |
| Build-tag-gated seams | No production indirection | Creates a second build configuration that CI must cover; an uncovered configuration is where rot grows (the reasoning that also rejected a build tag in #468) |

## Consequences

### Positive

- Fault injection runs against the production path, so a passing fault test is
  evidence about the shipped artifact.
- `pkg/lsm` and `pkg/btree` become fault-testable — they are not today.
- Crash simulation can model write reordering, which #478 explicitly does not:
  a VFS can hold unsynced writes and replay them out of order.
- Downstream consumers can inject faults into their own graphdb usage.

### Negative

- A public interface that must stay stable, on a package whose on-disk formats
  are already covered by a stability rule.
- An interface call replaces a direct `os` call on every file operation. Go
  devirtualises some of this; the WAL append path should be measured, not
  assumed.
- 259 call sites, and the mmap path uses `syscall.Mmap` directly, which does not
  fit the `File` interface and needs its own treatment.

  > **Correction, 2026-08-29 (stage 4).** The call-site counts in this document
  > are inflated by roughly 20x, and they mis-sized the work. They count every
  > `os.*` token — including `os.O_*` flag constants, `os.File` type references,
  > `os.Stderr` and `os.Getenv` — across test files as well as production code.
  >
  > Measured on the production surface: `pkg/storage` has **8** filesystem
  > operations, not 179. Of its 24 non-test `os.*` tokens, 11 are `os.Stderr`
  > in `fmt.Fprintf`, 2 are `os.Getenv` and 2 are the `os.IsNotExist`
  > predicate. `pkg/wal`'s "67" includes 39 `os.O_*` flags, which stay exactly
  > as they are because `vfs.Open` takes the same flag argument.
  >
  > The risk this section names was the real one, and the volume was not:
  > stage 4 was one function needing a file descriptor, plus seven mechanical
  > substitutions. `vfs.Mapper` resolves it — see below.

### Risks

- **A wrong abstraction is worse than none**: if `File` cannot express what the
  mmap reader needs, the migration stalls half-done. Mitigation: stage 1 ships
  the interface with `pkg/wal` migrated as the proof, before `pkg/storage` is
  touched.

  > **Resolved, 2026-08-29.** `File` could not express it: the reader needs a
  > descriptor for `syscall.Mmap`, and `File` has no `Fd`. It should not gain
  > one — a fault driver has no descriptor to return, so an `Fd`-shaped seam
  > admits the OS driver and excludes every other, which is the test-only seam
  > this ADR exists to remove. `vfs.Mapper` exposes the operation the reader
  > actually needs instead, as an OPTIONAL interface so `FileSystem` stays
  > stable. `syscall.Mmap` has now left `pkg/storage` entirely.
- **Performance regression on the hot path**: mitigation is the existing
  benchmark suite, run before and after stage 2, with the numbers in the PR.
- **A published API used in production by mistake**: mitigation is SQLite's —
  document it as testing-only and name it so.

## Amendment, 2026-08-28

This ADR plans DRIVERS. It does not cover TECHNIQUES, and two shipped without
appearing anywhere in it: the N-sweep (walk the failure point through every I/O
operation) and `pkg/faultsim` (SQLite's `sqlite3FaultSim`), both in #481.
faultsim is roughly this ADR's Driver 4, but Driver 4 as written describes
`never`/`always` and a test-control surface, and those are still unbuilt.

Techniques are tracked in `docs/internals/design/SQLITE_TESTING_SCORECARD.md`.
Stage progress for the drivers stays here. Stage 1 and the `pkg/wal` half of
stage 2 landed in #479.

## References

- sqlite.org/testing.html — techniques 5, 4, 6, 9
- D. R. Hipp, "SQLite: Past, Present, and Future" — the source-code versus
  object-code test distinction, and why TH3 exists
- #466 (WAL I/O seam), #477 (malformed snapshot), #478 (crash and power loss) —
  the test-only seams this ADR supersedes
- `docs/adr/0001-uniqueness-rules-registry.md` — the other open design

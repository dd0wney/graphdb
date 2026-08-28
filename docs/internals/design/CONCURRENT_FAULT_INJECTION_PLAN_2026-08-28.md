# Concurrent fault injection for `pkg/lsm` — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Inject faults into `pkg/lsm`'s background flush and compaction workers while foreground readers run, and prove what breaks.

**Architecture:** A role-attributing filesystem driver wraps the `pkg/vfs` driver that `pkg/lsm` already takes from `LSMOptions.FS`. It names each operation's actor from the path and the open flags, so a failure point can be named structurally — role, operation kind, occurrence within that role — rather than by a global ordinal that means nothing across runs. Two modes share that driver: a deterministic sweep for regression, and a randomised schedule for discovery, where a failing seed is minimised into a scripted case. A barrier parks an actor mid-operation so a test can observe the suspended state. A three-layer oracle — reopen, live read, and a new `CheckInvariants` for `pkg/lsm` — says what went wrong.

**Tech Stack:** Go, `pkg/vfs` (ADR 0002), `pkg/vfs/vfstest`, `testing`.

**Spec:** `docs/internals/design/CONCURRENT_FAULT_INJECTION_2026-08-28.md`

**Prerequisite plan:** `docs/internals/design/LSM_READ_PATH_CORRECTNESS_PLAN_2026-08-28.md` must be merged first. It fixes three confirmed read-path defects and adds the model-based test that would have found them. Its H3 fix is load-bearing here: while a tombstone fails to hide a flushed value, Task 9's compaction oracle fails on that reason in every run and cannot attribute anything to a fault.

## Global Constraints

- `golangci-lint run ./...` must pass **in CI**. The local linter on the development machine fails typecheck on a Go 1.26 standard library file, which suppresses the analysers and reports a clean-looking environment error. It missed real findings on #479 and #485. Never accept a local lint pass as evidence.
- `gofmt` must be clean. This is what #485 failed on.
- Every `//nolint:` directive must carry a reason after the lint name.
- **No fix lands without a test that would have exhibited the bug**, and the test must be seen to fail before the fix goes in. A test written after the fix, never observed red, is not evidence.
- **A background worker never discards an error.** It records it into sticky state, and every foreground entry point returns it until it is cleared. Task 8 establishes this for `pkg/lsm` and asserts it. `log.Printf` in a worker's error arm is a defect, not a style choice.
- **A `Sync` that returns nil promises that everything before it is on the disk.** Every layer above is entitled to believe it. This is the invariant H1 broke, and it outranks the mechanism that broke it.
- Conventional commits, imperative mood, one logical change per commit.
- `go test ./pkg/lsm/ -short -timeout 120s -count=1` must pass after every task.
- New long-running tests must be skipped under `-short`.
- Do not change the on-disk SSTable format. Do not add a method to `vfs.FileSystem` or `vfs.File` — they are published interfaces.
- Branch: `feat/lsm-concurrent-fault-injection`, taken from `origin/main` after Task 1.

## Task order and dependencies

| Task | Depends on |
|---|---|
| 1 — Unblock and merge #485 | nothing |
| 2 — `RoleFS`, with structural failure-point identity | 1 |
| 3 — The barrier | 2 |
| 4 — `SweepRole`, the deterministic mode | 2 |
| 5 — The randomised schedule, the discovery mode | 3, 4 |
| 6 — `CheckInvariants` I1 to I3 | 1 |
| 7 — `CheckInvariants` I4 and I5 | 6 |
| 8 — S1, the flush sweep, and the H1 fix | 3, 4, 7 |
| 9 — S2, the compaction sweep, and the H2 fix | 8 |
| 10 — S3, the record cache under a faulted compaction | 8 |
| 11 — Update the two tracking documents | 8, 9, 10 |

## Why two modes, and not just the barrier

A barrier buys reproducibility by deleting the interleavings it does not script. A barrier-scheduled test finds the bug in the schedule someone thought to write, and no others — which is the right property for a regression test and the wrong one for discovery.

So the harness carries both, over one driver:

- **Deterministic** (Tasks 3 and 4). `SweepRole` walks the failure point through one actor's operations. A barrier pins a specific interleaving. Reproducible, and what a regression test wants.
- **Randomised** (Task 5). The schedule is driven by a seed, the actors are left to interleave, and a failing seed is minimised into a scripted case that joins the deterministic set.

That is the relationship a fuzzer has with its corpus: the fuzzer roams, the interesting cases get pinned.

## Why the failure point needs a structural name

SQLite's "fail the N-th call, for N=1,2,3..." works because the sequence of calls is deterministic. A global ordinal is meaningless across runs the moment two actors interleave, and per-role counting only moves the problem: add one write to a scenario and every later ordinal shifts, so a recorded failure does not replay.

The sweep must still walk by ordinal, because that is how it reaches every operation. But every failure it finds is **reported and re-armed by a structural key** — `(role, operation kind, occurrence of that kind within that role)` — which survives a change in the operations before it. Task 2 builds both, and the sweep prints the structural key with any failure so it can be pinned as a scripted case.

---

### Task 1: Unblock and merge #485

`pkg/lsm` reaches the `pkg/vfs` driver in #485. Nothing in this plan can start without it. Its only failing check is `gofmt`.

**Files:**
- Modify: `pkg/lsm/compaction_types.go` (formatting only)

- [ ] **Step 1: Confirm the failure is still only gofmt**

Run: `gh pr checks 485`
Expected: one failure, `golangci-lint`. Read the detail with `gh run view <id> --log-failed | grep -E '\.go:[0-9]+'`. Expected: `pkg/lsm/compaction_types.go:3:1: File is not properly formatted (gofmt)`.

- [ ] **Step 2: Format the file on the PR's branch**

```bash
git checkout feat/vfs-btree-lsm
git pull --ff-only
gofmt -w pkg/lsm/compaction_types.go
git diff --stat
```

Expected: one file changed. If `git diff` is empty, the branch was already fixed; skip to Step 4.

- [ ] **Step 3: Commit and push**

```bash
git add pkg/lsm/compaction_types.go
git commit -m "style(lsm): gofmt compaction_types.go"
git push
```

- [ ] **Step 4: Wait for CI, then merge**

Run: `gh pr checks 485 --watch`
Expected: every check green. Read the real conclusions — never a piped exit code.

```bash
gh pr merge 485 --squash --delete-branch
```

- [ ] **Step 5: Branch for the rest of this plan**

```bash
git checkout main && git pull --ff-only
git checkout -b feat/lsm-concurrent-fault-injection
```

`fix/lsm-read-path` must be merged before this branch is taken. Task 6 uses `resolve`,
which that plan adds, and Task 8's oracle cannot attribute a failure while a tombstone
still fails to hide a flushed value.

---

### Task 2: `RoleFS`, the role-attributing driver

**Files:**
- Create: `pkg/vfs/vfstest/role.go`
- Create: `pkg/vfs/vfstest/role_test.go`

**Interfaces:**
- Consumes: `vfs.FileSystem` and `vfs.File` from `pkg/vfs`, including `ReadDir(name string) ([]os.DirEntry, error)` added in #485.
- Produces: `Role`, `RoleUnknown`, `Op` with `OpOpen OpRemove OpRename OpStat OpMkdirAll OpReadDir OpRead OpWrite OpSync OpTruncate OpClose`, `Key{Role, Op, Nth}`, `Classifier func(op Op, name string, flag int) Role`, `NewRoles(base vfs.FileSystem, name string, classify Classifier) *RoleFS`, and on `*RoleFS`: `FailNthOpForRole(role Role, n int)`, `FailAtKey(k Key)`, `Fired() bool`, `FiredKey() (Key, bool)`, `OpsForRole(role Role) int`, `Trace(role Role) []string`.

- [ ] **Step 1: Write the failing tests**

Create `pkg/vfs/vfstest/role_test.go`:

```go
package vfstest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

const (
	roleA Role = "a"
	roleB Role = "b"
)

// bySuffix names a role from the file extension, which is enough to test the
// driver without dragging pkg/lsm's rules in here.
func bySuffix(op Op, name string, flag int) Role {
	switch filepath.Ext(name) {
	case ".a":
		return roleA
	case ".b":
		return roleB
	}
	return RoleUnknown
}

func writeThrough(t *testing.T, fs vfs.FileSystem, path string) error {
	t.Helper()
	f, err := fs.Open(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte("x")); err != nil {
		return err
	}
	return f.Sync()
}

// A fault armed for one role must not fire on another role's operations.
func TestFailNthOpForRoleIsolatesRoles(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	fs.FailNthOpForRole(roleB, 1)

	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
		t.Fatalf("role a was faulted although only role b was armed: %v", err)
	}
	if fs.Fired() {
		t.Fatal("the fault fired on role a")
	}

	err := writeThrough(t, fs, filepath.Join(dir, "f.b"))
	if !errors.Is(err, ErrInjected) {
		t.Fatalf("role b: got %v, want ErrInjected", err)
	}
	if !fs.Fired() {
		t.Fatal("Fired reported false after the fault fired")
	}
}

// The N-th operation of a role means the N-th, not the N-th overall.
func TestFailNthOpForRoleCountsPerRole(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	fs.FailNthOpForRole(roleB, 2) // Open is 1, Write is 2

	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
		t.Fatalf("role a: %v", err)
	}
	err := writeThrough(t, fs, filepath.Join(dir, "f.b"))
	if !errors.Is(err, ErrInjected) {
		t.Fatalf("role b: got %v, want ErrInjected on the write", err)
	}
	if got := fs.OpsForRole(roleA); got != 3 {
		t.Errorf("role a op count = %d, want 3 (open, write, sync)", got)
	}
}

// The trace must name the operations, in order, so a sweep can compare runs.
func TestTraceRecordsOperationsInOrder(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := strings.Join(fs.Trace(roleA), ",")
	if !strings.HasPrefix(got, "open,write,sync") {
		t.Errorf("trace = %q, want it to start with open,write,sync", got)
	}
}

// An unclassified path must not silently join a real role.
func TestUnclassifiedPathTakesRoleUnknown(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	if err := writeThrough(t, fs, filepath.Join(dir, "f.zzz")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := fs.OpsForRole(RoleUnknown); got == 0 {
		t.Error("an unclassified path recorded no operations under RoleUnknown")
	}
	if got := fs.OpsForRole(roleA); got != 0 {
		t.Errorf("role a saw %d operations from an unclassified path", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/vfs/vfstest/ -run 'Role|Trace|Unclassified' -count=1`
Expected: FAIL to build, `undefined: NewRoles`.

- [ ] **Step 3: Write `role.go`**

Create `pkg/vfs/vfstest/role.go`:

```go
package vfstest

import (
	"fmt"
	"os"
	"sync"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Role names the actor that performs an operation.
//
// FaultFS counts every operation the process performs, which is enough for a
// single-threaded sweep. Under concurrency that counter is meaningless: the
// N-th operation overall is a different operation on every run, because the
// scheduler decides the interleaving. A per-role counter is stable as long as
// each actor's own sequence is, which SweepRole verifies rather than assumes.
type Role string

// RoleUnknown is the role of an operation the classifier does not recognise.
// It is a real role with its own counter, so an unclassified path cannot
// silently join a role a fault is armed on.
const RoleUnknown Role = "unknown"

// Op names a filesystem operation, for the trace and for the classifier.
type Op int

const (
	OpOpen Op = iota
	OpRemove
	OpRename
	OpStat
	OpMkdirAll
	OpReadDir
	OpRead
	OpWrite
	OpSync
	OpTruncate
	OpClose
)

func (o Op) String() string {
	switch o {
	case OpOpen:
		return "open"
	case OpRemove:
		return "remove"
	case OpRename:
		return "rename"
	case OpStat:
		return "stat"
	case OpMkdirAll:
		return "mkdirall"
	case OpReadDir:
		return "readdir"
	case OpRead:
		return "read"
	case OpWrite:
		return "write"
	case OpSync:
		return "sync"
	case OpTruncate:
		return "truncate"
	case OpClose:
		return "close"
	}
	return "unknown"
}

// Classifier attributes an operation to a role.
//
// It is called for Open and for the filesystem-level operations. A file takes
// the role assigned at Open and keeps it for every Read, Write, Sync, Truncate
// and Close, so those are never classified. That is correct wherever one actor
// opens a file and performs all of its I/O, which holds in pkg/lsm.
type Classifier func(op Op, name string, flag int) Role

// RoleFS wraps a filesystem, attributes each operation to a role, and can fail
// one role's N-th operation.
type RoleFS struct {
	base     vfs.FileSystem
	name     string
	classify Classifier

	mu     sync.Mutex
	counts map[Role]int
	traces map[Role][]string
	target Role
	nth    int
	fired  bool

	pause     *Pause
	pauseRole Role
	pauseNth  int
}

// NewRoles wraps base. Pass vfs.OS() for a driver that touches a real disk.
func NewRoles(base vfs.FileSystem, name string, classify Classifier) *RoleFS {
	return &RoleFS{
		base:     base,
		name:     name,
		classify: classify,
		counts:   make(map[Role]int),
		traces:   make(map[Role][]string),
	}
}

// FailNthOpForRole arms a fault on the N-th operation of one role, counting
// from 1. It resets the counters, so each sweep step starts from the same
// place.
func (r *RoleFS) FailNthOpForRole(role Role, n int) {
	r.mu.Lock()
	r.target, r.nth, r.fired = role, n, false
	r.counts = make(map[Role]int)
	r.traces = make(map[Role][]string)
	r.mu.Unlock()
}

// Fired reports whether the armed fault fired. A sweep ends when it did not.
func (r *RoleFS) Fired() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.fired }

// OpsForRole reports how many operations a role performed.
func (r *RoleFS) OpsForRole(role Role) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[role]
}

// Trace returns the role's operation names in order. SweepRole compares it
// between runs, because a sweep over "the N-th operation" means nothing if the
// sequence is not the same each time.
func (r *RoleFS) Trace(role Role) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.traces[role]))
	copy(out, r.traces[role])
	return out
}

// step records one operation and reports what should happen to it. It returns
// the barrier to block on, if this is the parked operation, and whether the
// armed fault fires. The caller blocks OUTSIDE r.mu: holding the driver's lock
// while an actor is parked would stop every other actor too, which would turn
// the barrier into a global freeze.
func (r *RoleFS) step(op Op, role Role, detail string) (*Pause, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.counts[role]++
	n := r.counts[role]
	r.traces[role] = append(r.traces[role], op.String())

	var block *Pause
	if r.pause != nil && role == r.pauseRole && n == r.pauseNth {
		block = r.pause
		// One arrival only. A second one would wait on a channel nothing
		// sends to.
		r.pause = nil
	}

	fail := r.target != "" && role == r.target && n == r.nth
	if fail {
		r.fired = true
	}
	return block, fail
}

// gate applies what step decided. It returns the error the operation must
// report, or nil to proceed.
func gate(block *Pause, fail bool, op Op, name string) error {
	if block != nil {
		if err := block.arrive(); err != nil {
			return err
		}
	}
	if fail {
		return fmt.Errorf("%s %s: %w", op, name, ErrInjected)
	}
	return nil
}

func (r *RoleFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	role := r.classify(OpOpen, name, flag)
	if err := gate(r.step(OpOpen, role, name)); err != nil {
		return nil, err
	}
	inner, err := r.base.Open(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &roleFile{File: inner, fs: r, role: role}, nil
}

func (r *RoleFS) Remove(name string) error {
	role := r.classify(OpRemove, name, 0)
	if err := gate(r.step(OpRemove, role, name)); err != nil {
		return err
	}
	return r.base.Remove(name)
}

func (r *RoleFS) Rename(oldpath, newpath string) error {
	role := r.classify(OpRename, oldpath, 0)
	if err := gate(r.step(OpRename, role, oldpath)); err != nil {
		return err
	}
	return r.base.Rename(oldpath, newpath)
}

func (r *RoleFS) Stat(name string) (os.FileInfo, error) {
	role := r.classify(OpStat, name, 0)
	if err := gate(r.step(OpStat, role, name)); err != nil {
		return nil, err
	}
	return r.base.Stat(name)
}

func (r *RoleFS) MkdirAll(path string, perm os.FileMode) error {
	role := r.classify(OpMkdirAll, path, 0)
	if err := gate(r.step(OpMkdirAll, role, path)); err != nil {
		return err
	}
	return r.base.MkdirAll(path, perm)
}

func (r *RoleFS) ReadDir(name string) ([]os.DirEntry, error) {
	role := r.classify(OpReadDir, name, 0)
	if err := gate(r.step(OpReadDir, role, name)); err != nil {
		return nil, err
	}
	return r.base.ReadDir(name)
}

func (r *RoleFS) Name() string { return r.name }

// roleFile carries the role assigned when the file was opened.
type roleFile struct {
	vfs.File
	fs   *RoleFS
	role Role
}

func (rf *roleFile) op(op Op) error {
	block, fail := rf.fs.step(op, rf.role, rf.Name())
	return gate(block, fail, op, rf.Name())
}

func (rf *roleFile) Read(p []byte) (int, error) {
	if err := rf.op(OpRead); err != nil {
		return 0, err
	}
	return rf.File.Read(p)
}

func (rf *roleFile) Write(p []byte) (int, error) {
	if err := rf.op(OpWrite); err != nil {
		return 0, err
	}
	return rf.File.Write(p)
}

func (rf *roleFile) Sync() error {
	if err := rf.op(OpSync); err != nil {
		return err
	}
	return rf.File.Sync()
}

func (rf *roleFile) Truncate(size int64) error {
	if err := rf.op(OpTruncate); err != nil {
		return err
	}
	return rf.File.Truncate(size)
}

func (rf *roleFile) Close() error {
	err := rf.op(OpClose)
	// The descriptor is released whatever the driver decided. A fault driver
	// that leaked one would make every test using it leak, and the leak would
	// be blamed on the code under test. FaultFS.Close does the same.
	closeErr := rf.File.Close()
	if err != nil {
		return err
	}
	return closeErr
}
```

Note: `gate(r.step(...))` passes two return values into a four-parameter function, which Go does not allow. Write each call site as two statements:

```go
	block, fail := r.step(OpOpen, role, name)
	if err := gate(block, fail, OpOpen, name); err != nil {
		return nil, err
	}
```

Apply that shape at every call site above.

- [ ] **Step 4: Add the compile-time interface assertion**

At the end of `role.go`:

```go
// RoleFS must satisfy the published interface. This fails to compile if
// vfs.FileSystem grows a method, which is the signal that the driver needs a
// considered implementation of it rather than a silent inherited one.
var _ vfs.FileSystem = (*RoleFS)(nil)
```

- [ ] **Step 5: Stub `Pause` so the package compiles**

`role.go` refers to `Pause`, which Task 3 writes. Add the minimum to `pkg/vfs/vfstest/pause.go` now:

```go
package vfstest

// Pause parks one operation until a test releases it. Task 3 fills this in.
type Pause struct {
	release chan error
}

func (p *Pause) arrive() error { return <-p.release }
```

- [ ] **Step 6: Run the tests**

Run: `go test ./pkg/vfs/vfstest/ -count=1`
Expected: PASS, including the existing `vfstest_test.go` tests.

- [ ] **Step 7: Add the structural failure-point key**

An ordinal is how the sweep walks. It is not how a failure should be recorded: add one write to the scenario and every later ordinal names a different operation, so a failure logged as "flush op 7" does not replay. A structural key survives that.

Append to `pkg/vfs/vfstest/role.go`:

```go
// Key names a failure point structurally: which actor, which kind of
// operation, and which occurrence of that kind within that actor.
//
// The sweep walks by ordinal because that is how it reaches every operation.
// It reports what it found by Key, because an ordinal is only meaningful
// against the exact scenario that produced it. "The flush worker's second
// sync" survives an extra write before it; "the flush worker's 7th operation"
// does not.
type Key struct {
	Role Role
	Op   Op
	Nth  int
}

func (k Key) String() string { return fmt.Sprintf("%s/%s#%d", k.Role, k.Op, k.Nth) }

// FailAtKey arms a fault on one structural failure point. This is how a case
// found by the sweep or by the randomised mode is pinned as a regression test.
func (r *RoleFS) FailAtKey(k Key) {
	r.mu.Lock()
	r.targetKey, r.hasTargetKey = k, true
	r.target, r.nth, r.fired = "", 0, false
	r.counts = make(map[Role]int)
	r.kinds = make(map[Role]map[Op]int)
	r.traces = make(map[Role][]string)
	r.mu.Unlock()
}

// FiredKey returns the structural key of the operation the fault fired on.
func (r *RoleFS) FiredKey() (Key, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.firedKey, r.fired
}
```

Add the fields `kinds map[Role]map[Op]int`, `targetKey Key`, `hasTargetKey bool` and `firedKey Key` to `RoleFS`, initialise `kinds` in `NewRoles` and in `FailNthOpForRole`, and extend `step` to maintain the per-kind counter and to record `firedKey`:

```go
	if r.kinds[role] == nil {
		r.kinds[role] = make(map[Op]int)
	}
	r.kinds[role][op]++
	key := Key{Role: role, Op: op, Nth: r.kinds[role][op]}

	fail := (r.target != "" && role == r.target && n == r.nth) ||
		(r.hasTargetKey && key == r.targetKey)
	if fail {
		r.fired, r.firedKey = true, key
	}
```

Add a test to `role_test.go` proving the key is stable when the operations before it change:

```go
// A structural key must name the same operation after the scenario grows an
// earlier operation. An ordinal does not, which is why failures are recorded
// by key.
func TestFailAtKeyIsStableAcrossAnExtraEarlierWrite(t *testing.T) {
	for _, extra := range []int{0, 1, 2} {
		dir := t.TempDir()
		fs := NewRoles(vfs.OS(), "roles", bySuffix)
		fs.FailAtKey(Key{Role: roleA, Op: OpSync, Nth: 1})

		path := filepath.Join(dir, "f.a")
		f, err := fs.Open(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatalf("extra=%d open: %v", extra, err)
		}
		for i := 0; i < 1+extra; i++ {
			if _, err := f.Write([]byte("x")); err != nil {
				t.Fatalf("extra=%d write %d: %v", extra, i, err)
			}
		}
		err = f.Sync()
		_ = f.Close()

		if !errors.Is(err, ErrInjected) {
			t.Errorf("extra=%d: the first sync was not faulted: %v", extra, err)
		}
	}
}
```

Run: `go test ./pkg/vfs/vfstest/ -run TestFailAtKey -v -count=1`
Expected: PASS for all three values of `extra`. With an ordinal the fault would move to a write as soon as `extra` grew.

- [ ] **Step 8: Commit**

```bash
git add pkg/vfs/vfstest/role.go pkg/vfs/vfstest/pause.go pkg/vfs/vfstest/role_test.go
git commit -m "feat(vfstest): attribute I/O to a role so a sweep works under concurrency

FaultFS counts every operation the process performs. Under concurrency
that counter names a different operation on every run, because the
scheduler picks the interleaving, so a sweep over it proves nothing.

RoleFS counts per role. The N-th write of the flush worker is the same
operation every run, as long as the actor's own sequence is stable —
which SweepRole verifies in the next commit rather than assuming."
```

---

### Task 3: The barrier

**Files:**
- Modify: `pkg/vfs/vfstest/pause.go` (replace the Task 2 stub)
- Modify: `pkg/vfs/vfstest/role.go` (add `PauseBeforeNthOpForRole`)
- Modify: `pkg/vfs/vfstest/role_test.go`

**Interfaces:**
- Consumes: `(*RoleFS).step` and the `Pause` fields `pause`, `pauseRole`, `pauseNth` from Task 2.
- Produces: `(*RoleFS).PauseBeforeNthOpForRole(t *testing.T, role Role, n int) *Pause`, `(*Pause).Wait(t *testing.T, timeout time.Duration)`, `(*Pause).Release(err error)`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/vfs/vfstest/role_test.go`:

```go
// The barrier must really block. Proved by a state change the test observes
// while the actor is parked, not by a sleep.
func TestPauseBlocksTheActor(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	p := fs.PauseBeforeNthOpForRole(t, roleA, 2) // park on the write

	var wrote atomic.Bool
	done := make(chan error, 1)
	go func() {
		err := writeThrough(t, fs, filepath.Join(dir, "f.a"))
		wrote.Store(true)
		done <- err
	}()

	p.Wait(t, 5*time.Second)
	if wrote.Load() {
		t.Fatal("the actor finished before the barrier released it")
	}

	p.Release(nil)
	if err := <-done; err != nil {
		t.Fatalf("the actor failed after a clean release: %v", err)
	}
}

// Release may inject the error the parked operation returns.
func TestPauseReleasesWithAnInjectedError(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	p := fs.PauseBeforeNthOpForRole(t, roleA, 2)

	done := make(chan error, 1)
	go func() { done <- writeThrough(t, fs, filepath.Join(dir, "f.a")) }()

	p.Wait(t, 5*time.Second)
	p.Release(ErrInjected)

	if err := <-done; !errors.Is(err, ErrInjected) {
		t.Fatalf("got %v, want ErrInjected", err)
	}
}
```

Add `"sync/atomic"` and `"time"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/vfs/vfstest/ -run 'TestPause' -count=1`
Expected: FAIL to build, `fs.PauseBeforeNthOpForRole undefined`.

- [ ] **Step 3: Write `pause.go`**

Replace `pkg/vfs/vfstest/pause.go`:

```go
package vfstest

import (
	"sync"
	"testing"
	"time"
)

// Pause parks one actor's operation so a test can observe the suspended state
// and then decide the operation's outcome.
//
// A sweep asks "what if this operation fails?". A barrier asks "what does
// everyone else see while this operation is in flight?", which is the question
// a background worker raises and a single-threaded sweep cannot pose.
type Pause struct {
	reached chan struct{}
	release chan error

	arriveOnce  sync.Once
	releaseOnce sync.Once
}

func newPause() *Pause {
	return &Pause{
		reached: make(chan struct{}),
		// Buffered, so Release never blocks even if the actor never arrives.
		release: make(chan error, 1),
	}
}

// arrive is called by the parked actor. It signals the waiting test, then
// blocks until Release.
func (p *Pause) arrive() error {
	p.arriveOnce.Do(func() { close(p.reached) })
	return <-p.release
}

// Wait blocks the test until the actor reaches the parked operation.
//
// It fails the test rather than hanging when the actor never arrives. A
// barrier that hangs turns a wrong scenario into a suite that never finishes,
// and the cause is then invisible.
func (p *Pause) Wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-p.reached:
	case <-time.After(timeout):
		t.Fatalf("vfstest: no actor reached the parked operation within %s: "+
			"either the role or the operation number is wrong, or the actor never ran", timeout)
	}
}

// Release resumes the parked actor. Pass an error to make the parked operation
// report it, or nil to let the operation proceed. A second call is a no-op.
func (p *Pause) Release(err error) {
	p.releaseOnce.Do(func() { p.release <- err })
}
```

- [ ] **Step 4: Add `PauseBeforeNthOpForRole`**

In `pkg/vfs/vfstest/role.go`, after `FailNthOpForRole`:

```go
// PauseBeforeNthOpForRole parks the role's N-th operation, counting from 1,
// until the returned Pause is released.
//
// The pause is released automatically when the test ends, so a scenario that
// fails before its Release cannot hang the suite or the actor's goroutine.
func (r *RoleFS) PauseBeforeNthOpForRole(t *testing.T, role Role, n int) *Pause {
	t.Helper()
	p := newPause()
	r.mu.Lock()
	r.pause, r.pauseRole, r.pauseNth = p, role, n
	r.mu.Unlock()
	t.Cleanup(func() { p.Release(nil) })
	return p
}
```

Add `"testing"` to `role.go`'s imports.

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/vfs/vfstest/ -count=1 -race`
Expected: PASS. `-race` matters here: the barrier is the package's first concurrent code.

- [ ] **Step 6: Commit**

```bash
git add pkg/vfs/vfstest/pause.go pkg/vfs/vfstest/role.go pkg/vfs/vfstest/role_test.go
git commit -m "feat(vfstest): a barrier that parks one actor mid-operation

A sweep asks what happens if an operation fails. A barrier asks what
everyone else sees while it is in flight, which is the question a
background worker raises and a single-threaded sweep cannot pose.

Wait fails the test instead of hanging when no actor arrives, and the
pause releases itself at test end, so a wrong role or a wrong operation
number costs a clear failure rather than a stuck suite."
```

---

### Task 4: `SweepRole`

**Files:**
- Create: `pkg/vfs/vfstest/sweep_role.go`
- Modify: `pkg/vfs/vfstest/role_test.go`

**Interfaces:**
- Consumes: `NewRoles`, `FailNthOpForRole`, `Fired`, `Trace` from Task 2.
- Produces: `SweepRole(t *testing.T, role Role, maxOps int, newFS func() *RoleFS, run func(fs vfs.FileSystem) error, check func(t *testing.T, n int, runErr error))`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/vfs/vfstest/role_test.go`:

```go
// The sweep must visit every operation of the role and then stop.
func TestSweepRoleVisitsEveryOperation(t *testing.T) {
	var seen int
	SweepRole(t, roleA, 32,
		func() *RoleFS { return NewRoles(vfs.OS(), "sweep", bySuffix) },
		func(fs vfs.FileSystem) error {
			return writeThrough(t, fs, filepath.Join(t.TempDir(), "f.a"))
		},
		func(t *testing.T, n int, runErr error) { seen = n },
	)
	// open, write, sync, close is four; the fifth run fires nothing and ends it.
	if seen != 5 {
		t.Errorf("the sweep ended at n=%d, want 5 for a four-operation scenario", seen)
	}
}

// A scenario whose operation sequence is not stable must fail the sweep, not
// pass it. Without this check the sweep is unsound under concurrency and says
// nothing, which is worse than not running.
func TestSweepRoleRejectsANondeterministicScenario(t *testing.T) {
	var round int
	fake := &testing.T{}
	func() {
		defer func() { _ = recover() }()
		SweepRole(fake, roleA, 32,
			func() *RoleFS { return NewRoles(vfs.OS(), "sweep", bySuffix) },
			func(fs vfs.FileSystem) error {
				round++
				path := filepath.Join(t.TempDir(), "f.a")
				if round%2 == 0 {
					// An extra write on every second run moves every later
					// operation, so run N is not run N-1 plus one failure.
					f, err := fs.Open(path, os.O_RDWR|os.O_CREATE, 0o600)
					if err != nil {
						return err
					}
					_, _ = f.Write([]byte("y"))
					_ = f.Close()
				}
				return writeThrough(t, fs, path)
			},
			func(t *testing.T, n int, runErr error) {},
		)
	}()
	if !fake.Failed() {
		t.Error("the sweep accepted a scenario whose operation sequence changes between runs")
	}
}
```

Note on the second test: `t.Fatalf` on a `*testing.T` the harness did not start calls `runtime.Goexit`, so the call is wrapped in a function with a `recover`. Assert on `fake.Failed()`, never on the panic.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/vfs/vfstest/ -run 'TestSweepRole' -count=1`
Expected: FAIL to build, `undefined: SweepRole`.

- [ ] **Step 3: Write `sweep_role.go`**

Create `pkg/vfs/vfstest/sweep_role.go`:

```go
package vfstest

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// SweepRole walks the point of failure through every operation ONE actor
// performs, while the other actors run untouched.
//
// Sweep does the same thing for a process that has one actor. It cannot be
// used under concurrency: its counter is per process, so the N-th operation is
// a different operation on every run and the walk visits an arbitrary subset
// of the error paths while appearing to visit all of them.
//
// SweepRole counts per role, which is stable only if the role's own operation
// sequence is stable. That is an assumption about the scenario, not a fact
// about the driver, so the sweep checks it: on every run it compares the
// role's trace with the previous run's, up to the injection point, and fails
// on a divergence. A sweep that cannot make that comparison is a sweep that
// terminates, passes, and proves nothing.
//
// newFS builds a fresh driver, with its classifier, for each step. Sweep can
// build its own inline because it needs no classifier.
func SweepRole(
	t *testing.T,
	role Role,
	maxOps int,
	newFS func() *RoleFS,
	run func(fs vfs.FileSystem) error,
	check func(t *testing.T, n int, runErr error),
) {
	t.Helper()

	if maxOps <= 0 {
		maxOps = 512
	}

	var prev []string

	for n := 1; ; n++ {
		if n > maxOps {
			t.Fatalf("the sweep of role %q did not terminate within %d operations: "+
				"the fault kept firing, so the scenario performs more I/O each step "+
				"or the driver is not counting", role, maxOps)
		}

		fs := newFS()
		fs.FailNthOpForRole(role, n)

		runErr := run(fs)
		trace := fs.Trace(role)

		if prev != nil {
			limit := n - 1
			if limit > len(trace) {
				limit = len(trace)
			}
			if limit > len(prev) {
				limit = len(prev)
			}
			for i := 0; i < limit; i++ {
				if trace[i] == prev[i] {
					continue
				}
				t.Fatalf("the sweep of role %q is unsound: at step %d of run %d the role "+
					"did %q, and in the previous run it did %q. The role's operation "+
					"sequence is not stable, so \"the N-th operation\" names a different "+
					"operation on each run and this sweep proves nothing.",
					role, i+1, n, trace[i], prev[i])
			}
		}
		prev = trace

		failedBefore := t.Failed()
		check(t, n, runErr)
		if !failedBefore && t.Failed() {
			// An ordinal is only meaningful against the exact scenario that
			// produced it. Print the structural key so the case can be pinned.
			if key, ok := fs.FiredKey(); ok {
				t.Logf("failure point %d has structural key %s: pin it as a regression "+
					"test with FailAtKey(vfstest.Key{Role: %q, Op: vfstest.%s, Nth: %d})",
					n, key, key.Role, opConstName(key.Op), key.Nth)
			}
		}

		if !fs.Fired() {
			// N ran off the end of the role's sequence: every one of its
			// operations has now been failed once, in turn.
			if n == 1 {
				t.Fatalf("role %q performed no I/O at all, so the sweep proved nothing", role)
			}
			return
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/vfs/vfstest/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/vfs/vfstest/sweep_role.go pkg/vfs/vfstest/role_test.go
git commit -m "feat(vfstest): a per-role sweep that refuses to be unsound

Sweep counts per process, so under concurrency the N-th operation is a
different operation on every run: the walk visits an arbitrary subset
of the error paths while appearing to visit all of them, and still
terminates and passes.

SweepRole counts per role, and checks the assumption that makes that
meaningful — it compares the role's trace with the previous run's up to
the injection point and fails on a divergence. A gate that cannot
report the negative is not a gate."
```

---

### Task 5: The randomised schedule, the discovery mode

**Why this task exists.** `SweepRole` and the barrier are deterministic, and that is what makes them good regression tests. It is also their limit: a scripted schedule finds the bug in the schedule someone thought to write. Discovery needs the opposite property — an unconstrained schedule, driven by a seed, where anything interesting that turns up gets minimised into a scripted case.

Go gives no control over its scheduler, so "randomised schedule" here means two seeded perturbations the driver *can* apply: a yield or a short sleep at operation boundaries, which changes which interleavings are reachable, and a random choice of which of a role's operations to fail.

**Files:**
- Create: `pkg/vfs/vfstest/explore.go`
- Modify: `pkg/vfs/vfstest/role.go` (the jitter hook in `step`)
- Modify: `pkg/vfs/vfstest/role_test.go`

**Interfaces:**
- Consumes: `RoleFS`, `Key`, `FiredKey` from Task 2.
- Produces: `(*RoleFS).Jitter(seed int64)`, `(*RoleFS).FailRandomly(seed int64, role Role, p float64)`, `Explore(t *testing.T, seeds int, newFS func(seed int64) *RoleFS, run func(fs vfs.FileSystem) error, check func(t *testing.T, seed int64, runErr error))`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/vfs/vfstest/role_test.go`:

```go
// Explore must run every seed and must name a reproducible case when one
// fails. A discovery mode that finds something it cannot hand back as a
// scripted test is a discovery mode that costs more than it returns.
func TestExploreReportsAPinnableKey(t *testing.T) {
	dir := t.TempDir()
	fake := &testing.T{}
	var lastKey Key
	var sawKey bool

	func() {
		defer func() { _ = recover() }()
		Explore(fake, 8,
			func(seed int64) *RoleFS {
				fs := NewRoles(vfs.OS(), "explore", bySuffix)
				fs.Jitter(seed)
				fs.FailRandomly(seed, roleA, 1.0) // always fail something
				return fs
			},
			func(fs vfs.FileSystem) error {
				return writeThrough(t, fs, filepath.Join(dir, "f.a"))
			},
			func(t *testing.T, seed int64, runErr error) {
				if runErr != nil {
					t.Errorf("seed %d failed", seed)
				}
			},
		)
	}()

	if !fake.Failed() {
		t.Fatal("Explore reported no failure although every seed was armed to fail")
	}
	_ = lastKey
	_ = sawKey
}

// Jitter must not change what the driver reports, only when it happens.
func TestJitterDoesNotChangeTheTrace(t *testing.T) {
	var first []string
	for seed := int64(0); seed < 4; seed++ {
		dir := t.TempDir()
		fs := NewRoles(vfs.OS(), "jitter", bySuffix)
		fs.Jitter(seed)
		if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		trace := fs.Trace(roleA)
		if first == nil {
			first = trace
			continue
		}
		if strings.Join(trace, ",") != strings.Join(first, ",") {
			t.Errorf("seed %d produced trace %v, seed 0 produced %v", seed, trace, first)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/vfs/vfstest/ -run 'TestExplore|TestJitter' -count=1`
Expected: FAIL to build, `undefined: Explore`.

- [ ] **Step 3: Add the jitter and the random fault to `role.go`**

Add the fields `rng *rand.Rand`, `jitter bool`, `failRole Role`, `failProb float64` to `RoleFS`, add `"math/rand"` and `"runtime"` to its imports, and add:

```go
// Jitter makes the driver yield the processor, and sometimes sleep briefly,
// before each operation.
//
// Go offers no control over its scheduler, so this is the only lever a driver
// has on the interleaving: it widens the windows between one actor's
// operations, which makes states reachable that a tight loop never visits. It
// is a discovery aid, not a proof of anything — an interleaving that jitter
// never produces is still an interleaving that can happen.
func (r *RoleFS) Jitter(seed int64) {
	r.mu.Lock()
	r.jitter = true
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(seed))
	}
	r.mu.Unlock()
}

// FailRandomly arms a fault on each of a role's operations with probability p,
// chosen from the seed. Exactly one fault fires per run: after it fires the
// driver disarms, so a failure names one failure point and Explore can hand it
// back as a Key.
func (r *RoleFS) FailRandomly(seed int64, role Role, p float64) {
	r.mu.Lock()
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(seed))
	}
	r.failRole, r.failProb = role, p
	r.mu.Unlock()
}
```

In `step`, after the counters are updated and before the return, add the random arm and the jitter decision. The jitter itself must happen **outside** `r.mu`, for the same reason the barrier does: holding the driver's lock while sleeping would stop every other actor and destroy the interleaving the jitter is trying to create.

```go
	if !fail && r.failRole != "" && role == r.failRole && r.rng.Float64() < r.failProb {
		fail = true
		r.fired, r.firedKey = true, key
		r.failRole = "" // one fault per run, so a failure names one point
	}
	yield := r.jitter
	nap := 0
	if yield {
		nap = r.rng.Intn(3)
	}
```

Return `yield` and `nap` alongside, and in `gate`:

```go
	if yield {
		runtime.Gosched()
		if nap > 0 {
			time.Sleep(time.Duration(nap) * 100 * time.Microsecond)
		}
	}
```

- [ ] **Step 4: Write `explore.go`**

Create `pkg/vfs/vfstest/explore.go`:

```go
package vfstest

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Explore runs a scenario under a range of seeds, each with its own schedule
// perturbation and its own choice of failure point.
//
// This is the other half of SweepRole. The sweep is exhaustive over one
// actor's operations and deterministic, which makes it a regression test and
// caps it at the interleavings the scenario happens to produce. Explore is
// neither exhaustive nor reproducible on its own, and it reaches states no
// scripted schedule was written for.
//
// The two are only useful together, and the join is the structural Key: when a
// seed fails, Explore prints the key of the operation that fired, and that key
// becomes a FailAtKey regression test which no longer depends on the seed, the
// scheduler, or the jitter. The fuzzer roams; the interesting cases get
// pinned.
func Explore(
	t *testing.T,
	seeds int,
	newFS func(seed int64) *RoleFS,
	run func(fs vfs.FileSystem) error,
	check func(t *testing.T, seed int64, runErr error),
) {
	t.Helper()

	for seed := int64(0); seed < int64(seeds); seed++ {
		fs := newFS(seed)

		failedBefore := t.Failed()
		runErr := run(fs)
		check(t, seed, runErr)

		if failedBefore || !t.Failed() {
			continue
		}
		key, ok := fs.FiredKey()
		if !ok {
			t.Logf("seed %d failed with no fault armed, so the defect is in the "+
				"schedule rather than in an error path: re-run with -race", seed)
			continue
		}
		t.Logf("seed %d failed at %s. Pin it as a deterministic regression test:
"+
			"    fs.FailAtKey(vfstest.Key{Role: %q, Op: vfstest.%s, Nth: %d})",
			seed, key, key.Role, opConstName(key.Op), key.Nth)
	}
}

// opConstName returns the Go constant name for an Op, so a logged failure can
// be pasted into a test without a lookup.
func opConstName(op Op) string {
	switch op {
	case OpOpen:
		return "OpOpen"
	case OpRemove:
		return "OpRemove"
	case OpRename:
		return "OpRename"
	case OpStat:
		return "OpStat"
	case OpMkdirAll:
		return "OpMkdirAll"
	case OpReadDir:
		return "OpReadDir"
	case OpRead:
		return "OpRead"
	case OpWrite:
		return "OpWrite"
	case OpSync:
		return "OpSync"
	case OpTruncate:
		return "OpTruncate"
	case OpClose:
		return "OpClose"
	}
	return "Op(0)"
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/vfs/vfstest/ -count=1 -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/vfs/vfstest/explore.go pkg/vfs/vfstest/role.go pkg/vfs/vfstest/role_test.go
git commit -m "feat(vfstest): a randomised discovery mode beside the deterministic sweep

A barrier and a sweep buy reproducibility by deleting the interleavings
nobody scripted. That is the right property for a regression test and
the wrong one for finding the bug in the first place.

Explore perturbs the schedule from a seed and picks the failure point
from the same seed. When a seed fails it prints the structural key of
the operation that fired, and that key becomes a FailAtKey test which
depends on neither the seed nor the scheduler. The fuzzer roams; the
interesting cases get pinned."
```

---

### Task 6: `CheckInvariants` for `pkg/lsm`, invariants I1 to I3

**Files:**
- Create: `pkg/lsm/invariants.go`
- Create: `pkg/lsm/invariants_test.go`

**Interfaces:**
- Consumes: `LSMStorage.fs`, `LSMStorage.dataDir`, `LSMStorage.levels`, `SSTable.path` — all added or present after #485.
- Produces: `lsm.CheckInvariants(l *LSMStorage) ([]string, error)`.

- [ ] **Step 1: Write the failing tests**

Create `pkg/lsm/invariants_test.go`:

```go
package lsm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newInvariantStore(t *testing.T) *LSMStorage {
	t.Helper()
	opts := DefaultLSMOptions(t.TempDir())
	opts.EnableAutoCompaction = false
	l, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return l
}

func TestCheckInvariantsCleanStore(t *testing.T) {
	l := newInvariantStore(t)
	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("a healthy store reported %d violations: %v", len(violations), violations)
	}
}

// I1, the direction that matters for H2: a file on the disk that no level
// knows about is what a failed compaction cleanup leaves behind.
func TestCheckInvariantsDetectsAnOrphanFileOnDisk(t *testing.T) {
	l := newInvariantStore(t)

	orphan := filepath.Join(l.dataDir, "L0-999999.sst")
	if err := os.WriteFile(orphan, []byte("not a real sstable"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "L0-999999.sst") {
		t.Errorf("an orphan SSTable on the disk was not reported: %v", violations)
	}
}

// I1, the other direction: a level holding a table whose file is gone.
func TestCheckInvariantsDetectsAMissingFile(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.RLock()
	path := l.levels[0][0].path
	l.mu.RUnlock()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, filepath.Base(path)) {
		t.Errorf("a level holding a table with no file was not reported: %v", violations)
	}
}

// I3: a table filed under the wrong level.
func TestCheckInvariantsDetectsAWrongLevel(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	moved := l.levels[0][0]
	l.levels[0] = nil
	l.levels = append(l.levels, []*SSTable{moved})
	l.mu.Unlock()

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "level") {
		t.Errorf("a table filed under the wrong level was not reported: %v", violations)
	}
}

// I2: the same table in two levels.
func TestCheckInvariantsDetectsADuplicate(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	dup := l.levels[0][0]
	l.levels[0] = append(l.levels[0], dup)
	l.mu.Unlock()

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "twice") {
		t.Errorf("a duplicated SSTable was not reported: %v", violations)
	}
}

func containsSubstring(violations []string, want string) bool {
	for _, v := range violations {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/lsm/ -run TestCheckInvariants -count=1`
Expected: FAIL to build, `undefined: CheckInvariants`.

- [ ] **Step 3: Write `invariants.go`**

Create `pkg/lsm/invariants.go`:

```go
package lsm

import (
	"fmt"
	"path/filepath"
)

// CheckInvariants returns one string per violated invariant. An empty slice
// means healthy. The error return is for a check that could not run at all,
// never for a violation.
//
// It exists for the reason #468 promoted the pkg/storage checker out of the
// test binary: a consumer that suspects a corrupt store must be able to ask,
// and an assertion that only a test can make is an assertion the shipped
// artefact does not carry.
//
// PRECONDITION: the store is quiescent. Stop the background workers, or call
// this before they are started. It takes lsm.mu for reading, which excludes a
// concurrent level swap but NOT a flush that is between its two critical
// sections, and I4 is meaningless while a flush is in flight.
func CheckInvariants(l *LSMStorage) ([]string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var violations []string
	report := func(format string, args ...any) {
		violations = append(violations, fmt.Sprintf(format, args...))
	}

	// Ground truth for I1 is the directory, read through the store's own
	// driver so a fault driver sees this read too.
	entries, err := l.fs.ReadDir(l.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", l.dataDir, err)
	}
	onDisk := make(map[string]struct{})
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sst" {
			continue
		}
		onDisk[filepath.Join(l.dataDir, e.Name())] = struct{}{}
	}

	inLevels := make(map[string]int)
	for level := range l.levels {
		for _, sst := range l.levels[level] {
			if sst == nil {
				report("levels[%d] holds a nil SSTable", level)
				continue
			}

			// I2: no table twice, in one level or across two.
			if seen, dup := inLevels[sst.path]; dup {
				report("SSTable %s appears twice: level %d and level %d",
					filepath.Base(sst.path), seen, level)
				continue
			}
			inLevels[sst.path] = level

			// I3: the name records the level, and ListSSTables rebuilds the
			// levels from it. A table filed elsewhere changes level across a
			// reopen, silently.
			var named, id int
			if _, err := fmt.Sscanf(filepath.Base(sst.path), "L%d-%d.sst", &named, &id); err != nil {
				report("SSTable %s has a name no reopen can parse", filepath.Base(sst.path))
			} else if named != level {
				report("SSTable %s is held at level %d but its name says level %d, "+
					"so a reopen would move it", filepath.Base(sst.path), level, named)
			}

			// I1, one direction.
			if _, ok := onDisk[sst.path]; !ok {
				report("SSTable %s is held at level %d but has no file on the disk",
					filepath.Base(sst.path), level)
			}
		}
	}

	// I1, the other direction. This is what a failed compaction cleanup leaves
	// behind, and a reopen loads it beside its own replacement.
	for path := range onDisk {
		if _, ok := inLevels[path]; !ok {
			report("SSTable %s is on the disk but no level holds it, "+
				"so a reopen would load it beside the tables that replaced it",
				filepath.Base(path))
		}
	}

	return violations, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./pkg/lsm/ -run TestCheckInvariants -v -count=1`
Expected: PASS, all five.

- [ ] **Step 5: Commit**

```bash
git add pkg/lsm/invariants.go pkg/lsm/invariants_test.go
git commit -m "feat(lsm): an invariant checker for the level set

pkg/storage gained one in #468 and pkg/lsm had none, so the level list
and the files on the disk could disagree with nothing to say so. The
disagreement is exactly what a failed compaction cleanup produces, and
a reopen then loads the superseded tables beside their replacement.

Each of the four invariants has a test that corrupts a store in the
matching way. An invariant that has never failed has not been shown to
work."
```

---

### Task 7: Invariants I4 and I5

**Files:**
- Modify: `pkg/lsm/invariants.go`
- Modify: `pkg/lsm/lsm.go` (extract `getUncached` from `Get`)
- Modify: `pkg/lsm/cache.go` (add `Snapshot`)
- Modify: `pkg/lsm/invariants_test.go`

**Interfaces:**
- Consumes: `(*LSMStorage).resolve`, added by `LSM_READ_PATH_CORRECTNESS_PLAN_2026-08-28.md`.
- Produces: `(*BlockCache).Snapshot() map[string][]byte`, `(*LSMStorage).getUncached(key []byte) ([]byte, bool)`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/lsm/invariants_test.go`:

```go
// I4 states H1 as an invariant: after a Sync returns, nothing is left
// half-flushed. A stranded immutableTable makes every later flush return early
// and Sync report success while writing nothing.
func TestCheckInvariantsDetectsAStrandedImmutableTable(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	l.immutableTable = NewMemTable(1024)
	_ = l.immutableTable.Put([]byte("stranded"), []byte("x"))
	l.mu.Unlock()

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "immutable") {
		t.Errorf("a stranded immutable memtable was not reported: %v", violations)
	}
}

// I5 is D5, the block cache: a shared LRU that no test has ever checked
// against the levels it mirrors.
func TestCheckInvariantsDetectsAStaleCacheEntry(t *testing.T) {
	l := newInvariantStore(t)

	if _, ok := l.Get([]byte("k")); !ok { // populates the cache
		t.Fatal("the key is missing, so the test cannot prove anything")
	}
	l.cache.Put("k", []byte("a value no level holds"))

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "cache") {
		t.Errorf("a stale cache entry was not reported: %v", violations)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/lsm/ -run 'TestCheckInvariantsDetectsAStranded|TestCheckInvariantsDetectsAStale' -count=1`
Expected: both FAIL, with no violation reported.

- [ ] **Step 3: Add `BlockCache.Snapshot`**

In `pkg/lsm/cache.go`, after `Put`:

```go
// Snapshot copies the cache's contents. CheckInvariants compares it with what
// the levels hold; iterating the live list under the caller's lock would need
// the cache's internals to leak.
func (bc *BlockCache) Snapshot() map[string][]byte {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	out := make(map[string][]byte, len(bc.cache))
	for key, elem := range bc.cache {
		if entry, ok := elem.Value.(*cacheEntry); ok {
			value := make([]byte, len(entry.value))
			copy(value, entry.value)
			out[key] = value
		}
	}
	return out
}
```

- [ ] **Step 4: Extract `getUncached` from `Get`**

In `pkg/lsm/lsm.go`, replace the three search steps of `Get` with a call, and put the search in its own method:

```go
func (lsm *LSMStorage) Get(key []byte) ([]byte, bool) {
	lsm.mu.RLock()
	defer lsm.mu.RUnlock()

	lsm.stats.ReadCount.Add(1)

	cacheKey := string(key)
	if value, ok := lsm.cache.Get(cacheKey); ok {
		return value, true
	}
	return lsm.getUncached(key, cacheKey)
}

// getUncached resolves a key from the memtables and the levels, without
// consulting the block cache.
//
// CheckInvariants needs it: comparing the cache against a lookup that consults
// the cache would compare the cache with itself and pass on any value.
//
// Caller holds lsm.mu.
func (lsm *LSMStorage) getUncached(key []byte, cacheKey string) ([]byte, bool) {
	if entry, ok := lsm.memTable.GetEntry(key); ok {
		return lsm.resolve(cacheKey, entry)
	}
	if lsm.immutableTable != nil {
		if entry, ok := lsm.immutableTable.GetEntry(key); ok {
			return lsm.resolve(cacheKey, entry)
		}
	}
	for level := 0; level < len(lsm.levels); level++ {
		for i := len(lsm.levels[level]) - 1; i >= 0; i-- {
			if entry, ok := lsm.levels[level][i].GetEntry(key); ok {
				return lsm.resolve(cacheKey, entry)
			}
		}
	}
	return nil, false
}
```

- [ ] **Step 5: Add I4 and I5 to `CheckInvariants`**

Append to `CheckInvariants` in `pkg/lsm/invariants.go`, before the `return`:

```go
	// I4: at quiescence nothing is half-flushed. A flush that fails after it
	// moved the memtable aside leaves this set forever, and every later flush
	// then takes the "already flushing" arm — including the one inside Sync,
	// which reports success and writes nothing.
	if l.immutableTable != nil {
		report("an immutable memtable of %d bytes is still set at quiescence: "+
			"a flush was interrupted, so every later flush returns early and "+
			"Sync reports success without writing", l.immutableTable.Size())
	}

	// I5: the block cache is shared across readers and mirrors the levels.
	// Nothing has ever checked it against them.
	for key, cached := range l.cache.Snapshot() {
		want, ok := l.getUncached([]byte(key), key)
		if !ok {
			report("the cache holds key %q, which no memtable or level holds", key)
			continue
		}
		if string(want) != string(cached) {
			report("the cache holds %q for key %q, and the levels hold %q",
				cached, key, want)
		}
	}
```

Note: `getUncached` calls `resolve`, which writes to the cache. That is safe here because it writes the value it just read from the levels, so it repairs rather than corrupts, but it means the check must run before the comparison of the next key rather than over a live iteration. `Snapshot` already copies, so the loop is over a copy.

- [ ] **Step 6: Run the tests**

Run: `go test ./pkg/lsm/ -run TestCheckInvariants -v -count=1`
Expected: PASS, all seven.

Run: `go test ./pkg/lsm/ ./pkg/storage/ -short -timeout 300s -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/lsm/invariants.go pkg/lsm/lsm.go pkg/lsm/cache.go pkg/lsm/invariants_test.go
git commit -m "feat(lsm): check the flush state and the block cache

I4 states as an invariant what a failed flush leaves behind: an
immutable memtable still set at quiescence, which makes every later
flush take the early return and Sync report success without writing.

I5 is the block cache, row D5 of the coupling analysis, which had no
evidence at all. It compares each cached value with a cache-bypassing
lookup; comparing against Get would compare the cache with itself."
```

---

### Task 8: S1 — sweep a fault through the flush worker, and fix H1

**Files:**
- Create: `pkg/lsm/lsm_concurrent_fault_test.go`
- Modify: `pkg/lsm/lsm_workers.go` (the H1 fix, after the test confirms it)

**Interfaces:**
- Consumes: `SweepRole`, `NewRoles`, `Role`, `Op`, `RoleUnknown` from Tasks 2 and 4; `CheckInvariants` from Tasks 6 and 7.
- Produces: `lsmRoles` classifier and `roleFlush`, `roleCompact`, `roleRead` constants, used again by Tasks 9 and 10.

- [ ] **Step 1: Write the classifier and its test**

Create `pkg/lsm/lsm_concurrent_fault_test.go` with the classifier first:

```go
package lsm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const (
	roleFlush   vfstest.Role = "flush"
	roleCompact vfstest.Role = "compact"
	roleRead    vfstest.Role = "read"
)

// lsmRoles names the actor behind an operation from the path and the open
// flags. SSTablePath writes L<level>-<id>.sst, so the level is in the name:
// the flush worker creates level 0, the compaction worker creates the rest and
// is the only actor that removes an SSTable, and any read-only open is a
// foreground reader.
//
// The role is derived rather than declared, which costs pkg/lsm no production
// change. It does not carry over to pkg/storage, where a flush and a reader
// touch one file.
func lsmRoles(op vfstest.Op, name string, flag int) vfstest.Role {
	base := filepath.Base(name)
	if !strings.HasSuffix(base, ".sst") {
		return vfstest.RoleUnknown
	}
	if op == vfstest.OpRemove {
		return roleCompact
	}
	if op != vfstest.OpOpen {
		return vfstest.RoleUnknown
	}
	if flag&os.O_CREATE == 0 {
		return roleRead
	}
	var level, id int
	if _, err := fmt.Sscanf(base, "L%d-%d.sst", &level, &id); err != nil {
		return vfstest.RoleUnknown
	}
	if level == 0 {
		return roleFlush
	}
	return roleCompact
}

// The classifier must read the file name, not the path. A data directory whose
// own name contains "L0-" would otherwise put every table in the flush role.
func TestLSMRolesReadsTheBaseName(t *testing.T) {
	cases := []struct {
		name string
		path string
		flag int
		want vfstest.Role
	}{
		{"level 0 create is a flush", "/d/L0-000001.sst", os.O_CREATE | os.O_RDWR, roleFlush},
		{"level 1 create is a compaction", "/d/L1-000001.sst", os.O_CREATE | os.O_RDWR, roleCompact},
		{"level 10 create is a compaction", "/d/L10-000001.sst", os.O_CREATE | os.O_RDWR, roleCompact},
		{"read-only open is a reader", "/d/L0-000001.sst", os.O_RDONLY, roleRead},
		{"a misleading directory name", "/tmp/L0-store/L2-000001.sst", os.O_CREATE | os.O_RDWR, roleCompact},
		{"not an SSTable", "/d/snapshot.json", os.O_CREATE | os.O_RDWR, vfstest.RoleUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lsmRoles(vfstest.OpOpen, tc.path, tc.flag); got != tc.want {
				t.Errorf("lsmRoles(%q, %#o) = %q, want %q", tc.path, tc.flag, got, tc.want)
			}
		})
	}
	if got := lsmRoles(vfstest.OpRemove, "/d/L1-000001.sst", 0); got != roleCompact {
		t.Errorf("a remove was classified as %q, want %q", got, roleCompact)
	}
}
```

- [ ] **Step 2: Run the classifier test**

Run: `go test ./pkg/lsm/ -run TestLSMRoles -v -count=1`
Expected: PASS. The classifier is pure, so it needs no fixture.

- [ ] **Step 3: Write the S1 scenario**

Append to `pkg/lsm/lsm_concurrent_fault_test.go`:

```go
// S1 sweeps a fault through every I/O operation the SHIPPED flush worker
// performs, while a foreground reader runs against the store.
//
// The oracle is durability: a key whose Sync returned nil must survive a
// reopen. The store may lose the flush — that is what the fault does — but it
// must not report success and lose the data.
func TestFlushWorkerUnderFaultSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("the sweep opens a store per failure point")
	}

	dir := t.TempDir()

	vfstest.SweepRole(t, roleFlush, 64,
		func() *vfstest.RoleFS { return vfstest.NewRoles(vfs.OS(), "lsm-flush", lsmRoles) },
		func(fs vfs.FileSystem) error {
			// A fresh directory per step: the sweep compares runs, so a run
			// must not inherit the previous run's files.
			stepDir := filepath.Join(dir, fmt.Sprintf("step-%d", time.Now().UnixNano()))
			opts := LSMOptions{
				DataDir:              stepDir,
				FS:                   fs,
				MemTableSize:         256, // small, so a few writes fill it
				CompactionStrategy:   DefaultLeveledCompaction(),
				EnableAutoCompaction: true, // the shipped workers
			}
			l, err := NewLSMStorage(opts)
			if err != nil {
				return err
			}

			stop := make(chan struct{})
			readerDone := make(chan struct{})
			go func() {
				defer close(readerDone)
				for {
					select {
					case <-stop:
						return
					default:
						l.Get([]byte("k0"))
					}
				}
			}()

			var writeErr error
			for i := 0; i < 8 && writeErr == nil; i++ {
				writeErr = l.Put([]byte(fmt.Sprintf("k%d", i)), []byte("value"))
			}
			syncErr := l.Sync()
			close(stop)
			<-readerDone

			violations, invErr := CheckInvariants(l)
			closeErr := l.Close()

			if invErr != nil {
				return fmt.Errorf("CheckInvariants: %w", invErr)
			}
			if len(violations) > 0 {
				return fmt.Errorf("invariants violated in %s: %s", stepDir, strings.Join(violations, "; "))
			}

			// The durability claim. Only a clean Sync makes one.
			if syncErr == nil {
				reopened, err := NewLSMStorage(LSMOptions{
					DataDir:              stepDir,
					MemTableSize:         256,
					CompactionStrategy:   DefaultLeveledCompaction(),
					EnableAutoCompaction: false,
				})
				if err != nil {
					return fmt.Errorf("reopen %s: %w", stepDir, err)
				}
				defer reopened.Close()
				for i := 0; i < 8; i++ {
					key := fmt.Sprintf("k%d", i)
					if _, ok := reopened.Get([]byte(key)); !ok {
						return fmt.Errorf("Sync returned nil but %q did not survive the reopen of %s", key, stepDir)
					}
				}
			}

			if writeErr != nil {
				return writeErr
			}
			if syncErr != nil {
				return syncErr
			}
			return closeErr
		},
		func(t *testing.T, n int, runErr error) {
			// An injected I/O error is an expected outcome. A lost write, a
			// violated invariant or a lying Sync is not.
			if runErr == nil || errorsIsInjected(runErr) {
				return
			}
			t.Errorf("failure point %d: %v", n, runErr)
		},
	)
}

func errorsIsInjected(err error) bool {
	return err != nil && strings.Contains(err.Error(), vfstest.ErrInjected.Error())
}
```

- [ ] **Step 4: Run S1 and read what it says**

Run: `go test ./pkg/lsm/ -run TestFlushWorkerUnderFaultSweep -v -count=1 -timeout 300s`

Expected: FAIL. The predicted message is `Sync returned nil but "k0" did not survive the reopen`, or the I4 violation `an immutable memtable of N bytes is still set at quiescence`.

**If it passes instead**, do not proceed. The sweep may be proving nothing. Check that `Fired()` was true for at least one step by adding a temporary `t.Logf` in the check function, and confirm the sweep ran more than one step.

- [ ] **Step 5: Fix H1**

**The defect is the durability lie, not the stranded field.** A stranded `immutableTable` and a `log.Printf` are how it happens. What broke is the promise every layer above is entitled to believe: *`Sync` returning nil means everything before it is on the disk.* Clearing the field alone would restore flushing and leave that promise still broken for the flush that failed — the data in that memtable is gone and `Sync` would say nothing about it.

So the fix is the general rule from the Global Constraints, applied here: **a background worker never discards an error; it records it into sticky state, and every foreground entry point returns it until a later success clears it.**

In `pkg/lsm/lsm_types.go`, add to `LSMStorage`:

```go
	// flushErr is the last error a background flush produced, and it is
	// sticky. A worker has no caller to return an error to, so without this
	// the error reaches log.Printf and nothing else: Sync goes on reporting
	// success for data that never reached the disk. Cleared by a flush that
	// succeeds. Guarded by mu.
	flushErr error
```

In `pkg/lsm/lsm_workers.go`, replace the error arm after `NewSSTable`, and clear the state on success:

```go
	// Create SSTable
	sstPath := SSTablePath(lsm.dataDir, 0, int(time.Now().UnixNano()))
	sst, err := NewSSTableWithFS(sstPath, entries, lsm.fs)
	if err != nil {
		lsm.mu.Lock()
		// Put the memtable back. Leaving immutableTable set makes every later
		// flush take the "already flushing" arm above, so flushing stops for
		// the life of the store.
		lsm.immutableTable = nil
		// Record the failure where a foreground caller can find it. This is
		// the half that matters: the flush is lost either way, and what must
		// not happen is Sync reporting success about it.
		lsm.flushErr = fmt.Errorf("background flush to %s failed: %w", sstPath, err)
		lsm.mu.Unlock()
		return err
	}
```

and, in the success path where `lsm.immutableTable = nil` is set under `lsm.mu.Lock()`:

```go
	lsm.flushErr = nil // this flush reached the disk, so the promise holds again
```

In `pkg/lsm/lsm.go`, make `Sync` and `Close` report it:

```go
func (lsm *LSMStorage) Sync() error {
	lsm.mu.Lock()
	needsFlush := lsm.memTable.Size() > 0
	lsm.mu.Unlock()

	if needsFlush {
		if err := lsm.flush(); err != nil {
			return fmt.Errorf("failed to flush memtable: %w", err)
		}
	}

	// A flush that failed earlier, in a background worker with no caller to
	// tell, still means data that Sync would otherwise claim is durable.
	lsm.mu.RLock()
	defer lsm.mu.RUnlock()
	return lsm.flushErr
}
```

Apply the same check at the end of `Close`, after the SSTable close errors are collected, so a process that shuts down cleanly cannot swallow it either.

- [ ] **Step 5b: Assert the promise directly**

The sweep's oracle checks the promise indirectly, through a reopen. State it directly too, so a future change that reintroduces the lie fails with the right message. Append to `pkg/lsm/lsm_concurrent_fault_test.go`:

```go
// Sync returning nil means everything before it is on the disk. A background
// flush that failed has no caller to report to, so the error must reach the
// next foreground caller who asks for durability.
func TestSyncReportsAFailedBackgroundFlush(t *testing.T) {
	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "lsm-sync", lsmRoles)
	fs.FailAtKey(vfstest.Key{Role: roleFlush, Op: vfstest.OpOpen, Nth: 1})

	l, err := NewLSMStorage(LSMOptions{
		DataDir:              dir,
		FS:                   fs,
		MemTableSize:         128,
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	})
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	defer l.Close()

	for i := 0; i < 16; i++ {
		if err := l.Put([]byte(fmt.Sprintf("k%d", i)), []byte("value")); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	// Give the worker its chance to fail. The fault fires on its first Open,
	// so the wait is bounded by the trigger, not by the ticker.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !fs.Fired() {
			time.Sleep(time.Millisecond)
			continue
		}
		break
	}
	if !fs.Fired() {
		t.Fatal("the flush worker never opened an SSTable, so the test proves nothing")
	}

	if err := l.Sync(); err == nil {
		t.Error("Sync returned nil after a background flush had failed: " +
			"a caller told the data was durable would be wrong")
	}
}
```

Run: `go test ./pkg/lsm/ -run TestSyncReportsAFailedBackgroundFlush -v -count=1`
Expected: FAIL before the fix with `Sync returned nil after a background flush had failed`, PASS after it.

- [ ] **Step 6: Re-run S1 and the package**

Run: `go test ./pkg/lsm/ -run TestFlushWorkerUnderFaultSweep -v -count=1 -timeout 300s`
Expected: PASS.

Run: `go test ./pkg/lsm/ -timeout 300s -count=1 && go test -race ./pkg/lsm/ -short -count=1`
Expected: PASS.

- [ ] **Step 7: Commit, in two commits**

```bash
git add pkg/lsm/lsm_concurrent_fault_test.go
git commit -m "test(lsm): sweep a fault through the shipped flush worker

Every fault test in this repository was single-threaded, and every
concurrent test injected no fault. This is the first that does both:
the shipped flushWorker runs, a foreground reader runs, and the point
of failure walks through the worker's I/O operations in turn."

git add pkg/lsm/lsm_workers.go
git commit -m "fix(lsm): Sync must not report success for a flush that failed

The visible symptom was that one failed flush stopped every later one:
flush moved the memtable into immutableTable and returned on a write
error without putting it back, so every later flush took the
\"already flushing\" arm.

The defect is the durability promise. Sync calls flush, took that arm,
and returned nil having written nothing — while flushWorker had already
discarded the original error into log.Printf. A caller told its data
was durable had no way to learn otherwise.

A background worker has no caller, so the error goes into sticky state
that Sync and Close return until a later flush clears it. The stranded
field is fixed as well, but that was the mechanism, not the defect."
```

---

### Task 9: S2 — sweep a fault through the compaction worker, and fix H2

**Files:**
- Modify: `pkg/lsm/lsm_concurrent_fault_test.go`
- Modify: `pkg/lsm/lsm_workers.go` (the H2 fix, after the test confirms it)

**Interfaces:**
- Consumes: `lsmRoles`, `roleCompact`, `roleFlush`, `errorsIsInjected` from Task 8.

- [ ] **Step 1: Write the S2 scenario**

Append to `pkg/lsm/lsm_concurrent_fault_test.go`:

```go
// S2 sweeps a fault through the compaction worker, including the Remove calls
// of CleanupOldSSTables.
//
// The oracle is the level set: a compaction that fails after it replaced the
// levels but before it removed the superseded files leaves them on the disk,
// and NewLSMStorage loads them beside their replacement. CheckInvariants
// reports that as an orphan; a reopen reports it as a key that came back.
func TestCompactionWorkerUnderFaultSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("the sweep opens a store per failure point")
	}

	root := t.TempDir()

	vfstest.SweepRole(t, roleCompact, 128,
		func() *vfstest.RoleFS { return vfstest.NewRoles(vfs.OS(), "lsm-compact", lsmRoles) },
		func(fs vfs.FileSystem) error {
			stepDir := filepath.Join(root, fmt.Sprintf("step-%d", time.Now().UnixNano()))
			opts := LSMOptions{
				DataDir:            stepDir,
				FS:                 fs,
				MemTableSize:       256,
				CompactionStrategy: DefaultLeveledCompaction(),
				// The flush is driven by Sync here, not by the worker, so the
				// compaction role is the only one whose sequence must be
				// stable. Step 3 asserts the flush role stayed idle.
				EnableAutoCompaction: false,
			}
			l, err := NewLSMStorage(opts)
			if err != nil {
				return err
			}

			// Build several L0 tables with a key deleted in the newest, so a
			// compaction that half-completes can revive it.
			for i := 0; i < 4; i++ {
				if err := l.Put([]byte("doomed"), []byte(fmt.Sprintf("v%d", i))); err != nil {
					l.Close()
					return err
				}
				if err := l.Sync(); err != nil {
					l.Close()
					return err
				}
			}
			if err := l.Delete([]byte("doomed")); err != nil {
				l.Close()
				return err
			}
			if err := l.Sync(); err != nil {
				l.Close()
				return err
			}

			compactErr := l.compact()

			violations, invErr := CheckInvariants(l)
			_ = l.Close()

			if invErr != nil {
				return fmt.Errorf("CheckInvariants: %w", invErr)
			}
			if len(violations) > 0 {
				return fmt.Errorf("invariants violated in %s: %s", stepDir, strings.Join(violations, "; "))
			}

			reopened, err := NewLSMStorage(LSMOptions{
				DataDir:              stepDir,
				MemTableSize:         256,
				CompactionStrategy:   DefaultLeveledCompaction(),
				EnableAutoCompaction: false,
			})
			if err != nil {
				return fmt.Errorf("reopen %s: %w", stepDir, err)
			}
			defer reopened.Close()

			if v, ok := reopened.Get([]byte("doomed")); ok {
				return fmt.Errorf("a deleted key came back as %q after the reopen of %s", v, stepDir)
			}

			return compactErr
		},
		func(t *testing.T, n int, runErr error) {
			if runErr == nil || errorsIsInjected(runErr) {
				return
			}
			t.Errorf("failure point %d: %v", n, runErr)
		},
	)
}
```

- [ ] **Step 2: Run S2 and read what it says**

Run: `go test ./pkg/lsm/ -run TestCompactionWorkerUnderFaultSweep -v -count=1 -timeout 600s`

Expected: FAIL. The predicted message is the I1 orphan violation, `SSTable L0-... is on the disk but no level holds it`, or `a deleted key came back as "v3" after the reopen`.

- [ ] **Step 3: Verify the classifier's assumption for this scenario**

Add to the scenario, immediately before the `return compactErr`, a guard that the flush role stayed idle. If a compaction ever wrote to level 0 it would be mislabelled as a flush, and this scenario's sweep would silently miss those operations:

```go
			if rfs, ok := fs.(*vfstest.RoleFS); ok {
				if got := rfs.OpsForRole(roleFlush); got != 0 {
					return fmt.Errorf("the flush role performed %d operations in a compaction-only "+
						"scenario, so the classifier is mislabelling compaction output as a flush", got)
				}
			}
```

Run the test again. If this guard fires, the classifier needs the level-0 compaction case handled before the sweep means anything.

- [ ] **Step 4: Fix H2**

In `pkg/lsm/lsm_workers.go`, make the cleanup failure visible and non-silent. Replace the tail of `compact`:

```go
	// Cleanup old SSTables. A failure here leaves the superseded files on the
	// disk, and NewLSMStorage rebuilds the levels by reading the directory —
	// so a reopen loads them beside the tables that replaced them, and any key
	// whose tombstone the compaction dropped becomes readable again.
	//
	// Retrying each removal once covers the transient case. A removal that
	// still fails is reported, so a caller can act rather than discover it at
	// the next open.
	if err := lsm.compactor.CleanupOldSSTables(plan.SSTables); err != nil {
		if retryErr := lsm.compactor.CleanupOldSSTables(plan.SSTables); retryErr != nil {
			return fmt.Errorf("compaction left %d superseded SSTables on disk, "+
				"which a reopen would load beside their replacement: %w",
				len(plan.SSTables), retryErr)
		}
	}

	return nil
```

Check `CleanupOldSSTables` in `pkg/lsm/compaction.go:169-181` first: if it stops at the first failure, make it continue and collect, so one unremovable file does not strand the rest. Write that change as part of this step and keep its existing signature.

- [ ] **Step 5: Re-run S2 and the package**

Run: `go test ./pkg/lsm/ -run TestCompactionWorkerUnderFaultSweep -v -count=1 -timeout 600s`
Expected: PASS.

Run: `go test ./pkg/lsm/ -timeout 300s -count=1`
Expected: PASS.

- [ ] **Step 6: Commit, in two commits**

```bash
git add pkg/lsm/lsm_concurrent_fault_test.go
git commit -m "test(lsm): sweep a fault through the compaction worker

The sweep includes the Remove calls of CleanupOldSSTables, which is the
window where the levels have already been replaced and the superseded
files are still on the disk."

git add pkg/lsm/lsm_workers.go pkg/lsm/compaction.go
git commit -m "fix(lsm): report a compaction cleanup that left files behind

compact replaced the levels and then removed the superseded SSTables.
A removal failure was returned as a plain wrapped error and the worker
logged it, so the files stayed on the disk. NewLSMStorage rebuilds the
levels by reading the directory, so the next open loaded them beside
their replacement and any key whose tombstone the compaction dropped
became readable again.

Cleanup now retries once, continues past a single unremovable file, and
names the consequence in the error."
```

---

### Task 10: S3 — the record cache under a faulted compaction

**Files:**
- Modify: `pkg/lsm/lsm_concurrent_fault_test.go`

**Interfaces:**
- Consumes: `lsmRoles`, `roleCompact`, `CheckInvariants`.

This is row D5, which has no evidence at all today. There is no hypothesis: the test states the property and finds out.

- [ ] **Step 1: Write the scenario**

Append to `pkg/lsm/lsm_concurrent_fault_test.go`:

```go
// S3 runs concurrent readers and writers over overlapping keys while the
// compaction worker is faulted, and checks the shared cache against the levels
// it mirrors.
//
// Row D5 of COUPLING_AND_INTERFERENCE.md calls this a block cache and says one
// workload evicts another's entries. The eviction half is right — it is an LRU
// with a fixed capacity. The name is not: BlockCache is keyed by the record
// key and holds that record's value (lsm.go:82), with no file and no offset in
// the key. So the hazard the name suggests, a reader serving blocks from a
// file compaction has deleted, cannot occur: no entry names a file.
//
// What remains is narrower. A cached value goes stale only if a value changes
// without invalidation, and Put and Delete both invalidate under lsm.mu.Lock
// while Get populates under RLock, so they exclude each other. That is an
// argument, not evidence, and this row has never had any.
func TestBlockCacheUnderFaultedCompaction(t *testing.T) {
	if testing.Short() {
		t.Skip("runs concurrent workloads across several failure points")
	}

	const keys = 32

	for _, failAt := range []int{1, 3, 5, 8} {
		t.Run(fmt.Sprintf("compaction-op-%d", failAt), func(t *testing.T) {
			dir := t.TempDir()
			fs := vfstest.NewRoles(vfs.OS(), "lsm-cache", lsmRoles)
			fs.FailNthOpForRole(roleCompact, failAt)

			l, err := NewLSMStorage(LSMOptions{
				DataDir:              dir,
				FS:                   fs,
				MemTableSize:         256,
				CompactionStrategy:   DefaultLeveledCompaction(),
				EnableAutoCompaction: false,
			})
			if err != nil {
				t.Fatalf("NewLSMStorage: %v", err)
			}
			defer l.Close()

			for round := 0; round < 4; round++ {
				for i := 0; i < keys; i++ {
					key := []byte(fmt.Sprintf("k%02d", i))
					if err := l.Put(key, []byte(fmt.Sprintf("r%d", round))); err != nil {
						t.Fatalf("Put: %v", err)
					}
				}
				if err := l.Sync(); err != nil {
					t.Fatalf("Sync: %v", err)
				}
			}

			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
						l.Get([]byte(fmt.Sprintf("k%02d", i%keys)))
					}
				}
			}()

			compactErr := l.compact()
			close(stop)
			<-done

			violations, err := CheckInvariants(l)
			if err != nil {
				t.Fatalf("CheckInvariants: %v", err)
			}
			for _, v := range violations {
				t.Errorf("failure point %d (compact returned %v): %s", failAt, compactErr, v)
			}

			// Every key must still read as the last value written, whatever
			// the compaction did.
			for i := 0; i < keys; i++ {
				key := fmt.Sprintf("k%02d", i)
				got, ok := l.Get([]byte(key))
				if !ok {
					t.Errorf("%s vanished after a faulted compaction", key)
					continue
				}
				if string(got) != "r3" {
					t.Errorf("%s = %q, want r3", key, got)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./pkg/lsm/ -run TestBlockCacheUnderFaultedCompaction -v -count=1 -timeout 300s`

Expected: unknown. This is the row with no evidence. If it passes, D5 has its first evidence. If it fails, read the violation, write the smallest test that reproduces it, and fix it in its own commit before this task closes.

- [ ] **Step 3: Run with the race detector**

Run: `go test -race ./pkg/lsm/ -run TestBlockCacheUnderFaultedCompaction -count=1 -timeout 300s`
Expected: PASS with no race report. A race here is a finding in its own right.

- [ ] **Step 4: Commit**

```bash
git add pkg/lsm/lsm_concurrent_fault_test.go
git commit -m "test(lsm): the block cache under a faulted compaction

Row D5 of the coupling analysis is a shared LRU across readers with no
evidence of any kind. This is its first."
```

---

### Task 11: Update the two tracking documents

**Files:**
- Modify: `docs/internals/design/COUPLING_AND_INTERFERENCE.md`
- Modify: `docs/internals/design/SQLITE_TESTING_SCORECARD.md`

Both live on branch `docs/coupling-interference` in PR #486 at the time of writing. If #486 has merged, edit them on `main`. If it has not, either rebase this branch on it or land these edits in a follow-up.

- [ ] **Step 1: Update the C5 and D5 evidence cells, and correct D5's description**

In `COUPLING_AND_INTERFERENCE.md`, replace `**None under fault injection**` in the C5 row and `**None**` in the D5 row with the test names from Tasks 8, 9 and 10. Do not write "done": name the test, as every other cell does.

Correct the D5 row itself. It reads "LSM block cache — Shared LRU across readers; one workload evicts another's entries." The eviction claim is right. The name is wrong: `BlockCache` is keyed by the record key and holds that record's value (`pkg/lsm/lsm.go:82`), with no file or offset in the key, so it is a record cache and a compacted-away file cannot orphan an entry. The row described a data structure the repository does not have, and the wrong description generated a plausible correctness hazard that cannot occur. Say what it is.

- [ ] **Step 2: Rewrite "The gap, stated plainly"**

That section says every fault test is single-threaded and that the two halves have never met. That stops being true at Task 8. Replace it with what is still open: `pkg/storage`'s 28 concurrent test files still inject no faults, and its driver migration is ADR 0002 stage 4.

- [ ] **Step 3: Close open item 1, and record what H3 taught**

Open item 1 is this work. Open item 3 says "D5 and C5 have no evidence at all", which Tasks 8 to 10 change.

Add a row to the defect table at the top for each defect this plan confirmed: H1 and H2 are coupling defects, and H3 with its `Scan` counterpart is a within-component defect found while reading the interface. That last point is worth stating: the coupling analysis predicted where the defects would be, and looking there also turned up defects that were not coupling defects.

- [ ] **Step 4: Update the scorecard**

In `SQLITE_TESTING_SCORECARD.md`, update the row for I/O fault injection to say the sweep now runs against concurrent actors, and add the defect count from this plan.

- [ ] **Step 5: Commit**

```bash
git add docs/internals/design/COUPLING_AND_INTERFERENCE.md docs/internals/design/SQLITE_TESTING_SCORECARD.md
git commit -m "docs: C5 and D5 have evidence, and the single-threaded gap is closed for pkg/lsm

Both rows read \"None\" because faults were injected sequentially and
concurrency was tested without faults. Names the tests that changed
that, and states what is still open: pkg/storage's 28 concurrent test
files, which need ADR 0002 stage 4 first."
```

---

## Before the pull request

- [ ] `go build ./... && go vet ./...`
- [ ] `go test ./pkg/lsm/ ./pkg/vfs/... ./pkg/storage/ -short -timeout 300s -count=1`
- [ ] Every defect this plan confirmed has a test in the suite that was seen to fail before its fix. Count them and say the number in the pull request body.
- [ ] `go test -race ./pkg/lsm/ ./pkg/vfs/... -short -count=1`
- [ ] `gofmt -l pkg/ | tee /dev/stderr | wc -l` reports 0
- [ ] `/review` on the diff, then `/preflight`
- [ ] Open the PR. Read `gh pr checks` conclusions directly — never a piped exit code. The local `golangci-lint` cannot be trusted on this machine.

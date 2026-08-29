# Absent versus refused — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a refused allocation and a damaged record distinguishable from a missing record, from the mmap decoder up to the public error boundary.

**Architecture:** `recordCursor` gains an `err` field set at the first failure. The decoders, `mmapSnapshot.getNode`/`getEdge`, and the six loader helpers move from `(T, bool)` to `(T, error)`, where absence is `ErrNodeNotFound`/`ErrEdgeNotFound` and any other error means the record exists but could not be produced. PR A does the whole move while every public boundary collapses errors to today's answer, so it is inert. PR B stops collapsing on single-record reads. PR C does the same for enumerations, which requires a shared-interface change.

**Tech Stack:** Go 1.26.0, `pkg/alloc` allocator seam, `pkg/vfs` filesystem driver, standard `testing`.

**Spec:** `docs/superpowers/specs/2026-08-29-absent-versus-refused-design.md`

## Global Constraints

- Go 1.26.0 (`go.mod`); toolchain pinned to 1.26.4. Multiple `%w` verbs in one `fmt.Errorf` are supported and used by this plan.
- `gofmt -s -l ./pkg ./cmd` MUST be empty. Plain `gofmt` is a weaker gate and will pass code that CI rejects.
- `golangci-lint` locally cannot typecheck `pkg/storage` (it imports `math/rand/v2`). Use `make lint-local`, which runs nine of CI's eleven linters. An exit code of 2 means the package did not load, which is not a result about the code.
- Every `//nolint:` directive MUST carry a reason after the lint name. Plain `//nolint` is rejected.
- `pkg/storage` tests need `-timeout 300s`. The suite runs about 120-170 seconds.
- Do not change either on-disk snapshot format. This work changes no bytes on disk.
- Cross-tenant and missing MUST keep returning the same error (`ErrNodeNotFound`). The new error class is the one documented exception, and only when the record cannot be decoded.
- Commit messages use conventional-commit prefixes and imperative mood.
- `$SCRATCH` below means this session's scratchpad directory. Export it once
  before starting: `export SCRATCH=/tmp/claude-1000/-mnt-ssd2-Workspace-github-com-graphdb/4474d5b1-0313-4e31-8e85-7af525dbfaeb/scratchpad`.
  Nothing under it is committed.
- Each commit ends with the two trailer lines this repo uses (`Co-Authored-By:` and `Claude-Session:`).

---

## File Structure

**Modified in PR A**

| File | Responsibility after the change |
|---|---|
| `pkg/storage/errors.go` | Adds `ErrRecordUnreadable` (exported) and `errRecordDamaged` (unexported cause) |
| `pkg/storage/mmap_snapshot_format.go` | `recordCursor` carries a reason. `readProps`, `decodeNodeRecordAt`, `decodeEdgeRecordAt` return `error` |
| `pkg/storage/mmap_snapshot_reader.go` | `getNode`/`getEdge` return `error` |
| `pkg/storage/mmap_snapshot_loader.go` | The six resolve/materialize helpers return `error` |
| 11 call-site files | Adapt to the new returns, collapsing at every public boundary |

**Created in PR A**

| File | Responsibility |
|---|---|
| `pkg/storage/record_reason_test.go` | Unit tests that the decoder reports damage and refusal as distinct reasons |

**Created in PR B**

| File | Responsibility |
|---|---|
| `pkg/storage/damaged_record_test.go` | The red-first gate: a damaged record must not be reported as missing |

---

# PR A — the mechanism

## Task 1: Record the red-first evidence against `main`

This task writes no production code. It produces the failure output that PR B's
pull-request body must quote. The test compiles against today's `main` because it
names no new symbol.

**Files:**
- Create: `/tmp/claude-1000/-mnt-ssd2-Workspace-github-com-graphdb/4474d5b1-0313-4e31-8e85-7af525dbfaeb/scratchpad/redfirst_probe_test.go` (throwaway; copied into `pkg/storage` to run, then removed)

**Interfaces:**
- Consumes: nothing.
- Produces: a recorded failure message, pasted into PR B's body and into Task 8.

- [ ] **Step 1: Write the probe**

Write this to the scratchpad, then copy it to `pkg/storage/zz_redfirst_probe_test.go`:

```go
package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

// Probe: on main, a record damaged in place reads as a missing node.
// Not committed. Its only product is the failure message.
func TestProbeDamagedRecordIsNotReportedAsMissing(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	n, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	target := n.ID
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)

	// Find the record's true offset through the reader's own directory, so the
	// corruption lands on a record body and leaves the CRC valid.
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(target)
	if !ok {
		t.Fatalf("node %d has no directory entry", target)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Byte 8 of a node record is the tenant-length prefix. 0xFFFF asks for more
	// bytes than the file holds, so the bounds check refuses the record.
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption itself. If the record still decodes,
	// this test proves nothing, and it must say so rather than pass.
	if _, stillOK := decodeNodeRecordAt(raw, off); stillOK {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	_, err = gs2.GetNodeForTenant(target, "")
	if err == nil {
		t.Fatalf("want an error for a damaged record, got none")
	}
	if errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a damaged record was reported as missing: %v", err)
	}
}
```

- [ ] **Step 2: Run it on `main` and record what it prints**

```bash
git stash list   # confirm a clean tree first
cp "$SCRATCH/redfirst_probe_test.go" pkg/storage/zz_redfirst_probe_test.go
go test ./pkg/storage/ -run TestProbeDamagedRecordIsNotReportedAsMissing -v -count=1 -timeout 300s 2>&1 | tee "$SCRATCH/redfirst-main.txt"
```

Expected: FAIL, with a line of the form
`a damaged record was reported as missing: node not found`.

If instead it prints `corruption did not take`, the snapshot is larger than
65535 bytes past the record, so the length prefix was reachable. Corrupt the
property-count prefix instead, or shrink the fixture to one node. **Do not
proceed until the test fails for the intended reason.**

- [ ] **Step 3: Remove the probe from the package**

```bash
rm pkg/storage/zz_redfirst_probe_test.go
git status --short    # must be clean
```

- [ ] **Step 4: No commit**

This task commits nothing. Carry `$SCRATCH/redfirst-main.txt` forward.

---

## Task 2: The decoder reports a reason

**Files:**
- Modify: `pkg/storage/errors.go:9-24` (the sentinel block)
- Modify: `pkg/storage/mmap_snapshot_format.go:239-253` (`recordCursor`, `newRecordCursor`), `:255-262` (`has`), `:318-341` (`blob`), `:518-535` (`readProps`), `:553-583` (`decodeNodeRecordAt`), `:621-643` (`decodeEdgeRecordAt`), `:585-586` and `:646` (two stale doc comments)
- Modify: `pkg/storage/mmap_snapshot_reader.go:188-204` (collapse to bool for now)
- Modify: `pkg/storage/storage_helpers.go:312` (adapt)
- Modify: `pkg/storage/mmap_snapshot_persist.go:38` (adapt)
- Test: `pkg/storage/record_reason_test.go` (create)

**Interfaces:**
- Consumes: `alloc.ErrNoMemory` from `pkg/alloc`.
- Produces:
  - `ErrRecordUnreadable error` — exported sentinel.
  - `errRecordDamaged error` — unexported cause.
  - `func (c *recordCursor) fail(err error)`
  - `func readProps(buf []byte, p int) (map[string]Value, int, error)`
  - `func decodeNodeRecordAt(buf []byte, off int64) (*Node, error)`
  - `func decodeEdgeRecordAt(buf []byte, off int64) (*Edge, error)`

- [ ] **Step 1: Write the failing test**

Create `pkg/storage/record_reason_test.go`:

```go
package storage

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
	"github.com/dd0wney/graphdb/pkg/alloc/alloctest"
)

// A decoder that refuses to say WHY it failed turns a damaged record and a
// refused allocation into a missing one. These two tests pin the difference at
// the lowest layer that knows it.

func TestDecodeNodeRecordAt_DamageReportsUnreadable(t *testing.T) {
	buf := encodeNodeRecord(&Node{
		ID: 7, TenantID: "default", Labels: []string{"Thing"},
		Properties: map[string]Value{"name": StringValue("alpha")},
	})
	// Positive control: the record decodes before it is damaged.
	if _, err := decodeNodeRecordAt(buf, 0); err != nil {
		t.Fatalf("undamaged record must decode: %v", err)
	}

	binary.LittleEndian.PutUint16(buf[8:], 0xFFFF) // tenant length past the buffer

	_, err := decodeNodeRecordAt(buf, 0)
	if err == nil {
		t.Fatal("a damaged record must not decode")
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}
	if errors.Is(err, alloc.ErrNoMemory) {
		t.Fatalf("damage must not be reported as a refusal: %v", err)
	}
}

func TestDecodeNodeRecordAt_RefusalReportsNoMemory(t *testing.T) {
	buf := encodeNodeRecord(&Node{
		ID: 7, TenantID: "default", Labels: []string{"Thing"},
		Properties: map[string]Value{"name": StringValue("alpha")},
	})

	alloc.Install(alloctest.FailFrom(1))
	defer alloc.Reset()

	_, err := decodeNodeRecordAt(buf, 0)
	if err == nil {
		t.Fatal("a refused allocation must not decode")
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}
	if !errors.Is(err, alloc.ErrNoMemory) {
		t.Fatalf("want the alloc cause to survive, got %v", err)
	}
	// Negative control: without this the test can pass on an error from
	// somewhere else entirely.
	if alloc.Allocs() == 0 {
		t.Fatal("no allocation was attempted, so this proves nothing")
	}
}
```

**Before running:** confirm the helper name. `alloctest` supplies the modes used
by `mmap_oom_test.go` (`alloctest.FailOnce`, `alloctest.FailAllFrom`). Open
`pkg/alloc/alloctest/alloctest.go` and use whatever constructor builds an
`alloc.Allocator` that refuses from the first request. If no such constructor
exists, write one in the test file:

```go
type failFromAllocator struct{ n, seen int }

func (a *failFromAllocator) Name() string { return "failFrom" }
func (a *failFromAllocator) Bytes(n int) ([]byte, error) {
	a.seen++
	if a.seen >= a.n {
		return nil, alloc.ErrNoMemory
	}
	return make([]byte, n), nil
}
```

and install `&failFromAllocator{n: 1}`.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./pkg/storage/ -run 'TestDecodeNodeRecordAt_' -v -count=1 -timeout 300s
```

Expected: it does not compile — `decodeNodeRecordAt` returns `(*Node, bool)`, and
`ErrRecordUnreadable` is undefined. A compile failure is the correct red here.

- [ ] **Step 3: Add the sentinels**

In `pkg/storage/errors.go`, inside the existing `var (...)` block, after
`ErrInvalidEdgeWeight`:

```go
	// ErrRecordUnreadable means a record exists but could not be produced.
	// It is never a statement about existence: the ID IS in the snapshot
	// directory. Two causes reach it, and both wrap into it:
	//
	//   errRecordDamaged — the bytes did not decode. The snapshot CRC covers
	//   the header, the directories and the metadata blob, NOT record bodies,
	//   so bit rot or a partial write survives open and arrives here.
	//
	//   alloc.ErrNoMemory — a length-driven buffer was refused.
	//
	// errors.Is(err, ErrRecordUnreadable) catches the class.
	// errors.Is(err, alloc.ErrNoMemory) still separates refusal from damage.
	ErrRecordUnreadable = errors.New("record unreadable")

	// errRecordDamaged is the cause wrapped into ErrRecordUnreadable when the
	// record bytes themselves are wrong. Unexported: callers discriminate on
	// ErrRecordUnreadable, or on alloc.ErrNoMemory for the other cause.
	errRecordDamaged = errors.New("record did not decode")
```

- [ ] **Step 4: Make the cursor carry the reason**

In `pkg/storage/mmap_snapshot_format.go`, replace the `recordCursor` type,
`newRecordCursor` and `has`:

```go
type recordCursor struct {
	buf []byte
	p   int
	ok  bool
	// err holds the FIRST failure's cause. The cursor is poisoned after it,
	// so later reads neither overwrite the reason nor add a second one.
	err error
}

// fail poisons the cursor and records why. The first cause wins: a bounds
// failure that follows a refused allocation is a consequence, not a new fact.
func (c *recordCursor) fail(err error) {
	if c.ok {
		c.ok = false
		c.err = err
	}
}

// reason returns the decode failure wrapped for a caller, or nil while the
// cursor is still good. what names the record for the message.
func (c *recordCursor) reason(what string, off int64) error {
	if c.ok {
		return nil
	}
	return fmt.Errorf("%s at offset %d: %w: %w", what, off, ErrRecordUnreadable, c.err)
}

func newRecordCursor(buf []byte, off int64) *recordCursor {
	c := &recordCursor{buf: buf, ok: true}
	if off < 0 || off > int64(len(buf)) {
		c.fail(errRecordDamaged)
		return c
	}
	c.p = int(off)
	return c
}

func (c *recordCursor) has(n int) bool {
	if !c.ok {
		return false
	}
	if n < 0 || c.p+n > len(c.buf) {
		c.fail(errRecordDamaged)
		return false
	}
	return true
}
```

In `blob`, replace `c.ok = false` with the cause:

```go
func (c *recordCursor) blob(n int) []byte {
	if !c.has(n) {
		return nil
	}
	v, err := alloc.Bytes(n)
	if err != nil {
		c.fail(err)
		return nil
	}
	copy(v, c.buf[c.p:c.p+n])
	c.p += n
	return v
}
```

- [ ] **Step 5: Move `readProps` and the two decoders to `error`**

```go
func readProps(buf []byte, p int) (map[string]Value, int, error) {
	c := newRecordCursor(buf, int64(p))
	n := c.u16()
	if !c.ok {
		return nil, p, c.reason("property bag", int64(p))
	}
	props := make(map[string]Value, min(n, 64)) // cap the hint: n is untrusted
	for i := 0; i < n; i++ {
		key := c.str(c.u16())
		vt := ValueType(c.u8())
		data := c.blob(c.u32()) // copy-on-read: do not alias the mapping
		if !c.ok {
			return nil, p, c.reason("property bag", int64(p))
		}
		props[key] = Value{Type: vt, Data: data}
	}
	return props, c.p, nil
}

func decodeNodeRecordAt(buf []byte, off int64) (*Node, error) {
	c := newRecordCursor(buf, off)
	n := &Node{}
	n.ID = c.u64()
	n.TenantID = c.str(c.u16())
	if nl := c.u16(); nl > 0 {
		if !c.has(nl * 2) { // cheapest possible floor: 2 bytes per label prefix
			return nil, c.reason("node record", off)
		}
		n.Labels = make([]string, nl)
		for i := 0; i < nl; i++ {
			n.Labels[i] = c.str(c.u16())
		}
	}
	if !c.ok {
		return nil, c.reason("node record", off)
	}
	props, p, err := readProps(buf, c.p)
	if err != nil {
		return nil, err
	}
	n.Properties = props
	c.p = p
	n.CreatedAt = int64(c.u64())
	n.UpdatedAt = int64(c.u64())
	if !c.ok {
		return nil, c.reason("node record", off)
	}
	return n, nil
}

func decodeEdgeRecordAt(buf []byte, off int64) (*Edge, error) {
	c := newRecordCursor(buf, off)
	e := &Edge{}
	e.ID = c.u64()
	e.TenantID = c.str(c.u16()) // str copies — no alias into the mmap region
	e.FromNodeID = c.u64()
	e.ToNodeID = c.u64()
	e.Type = c.str(c.u16())
	if !c.ok {
		return nil, c.reason("edge record", off)
	}
	props, p, err := readProps(buf, c.p)
	if err != nil {
		return nil, err
	}
	e.Properties = props
	c.p = p
	e.Weight = math.Float64frombits(c.u64())
	e.CreatedAt = int64(c.u64())
	if !c.ok {
		return nil, c.reason("edge record", off)
	}
	return e, nil
}
```

- [ ] **Step 6: Adapt the three direct callers, collapsing for now**

`pkg/storage/mmap_snapshot_reader.go:188-204` — keep the bool at this boundary
until Task 3:

```go
func (m *mmapSnapshot) getNode(id uint64) (*Node, bool) {
	off, ok := m.nodeOffset(id)
	if !ok {
		return nil, false
	}
	// A record the CRC does not cover can be damaged. A damaged record reads
	// as absent rather than crashing the process. Task 3 replaces this bool
	// with the reason the decoder now carries.
	n, err := decodeNodeRecordAt(m.data, off)
	return n, err == nil
}

func (m *mmapSnapshot) getEdge(id uint64) (*Edge, bool) {
	off, ok := m.edgeOffset(id)
	if !ok {
		return nil, false
	}
	e, err := decodeEdgeRecordAt(m.data, off)
	return e, err == nil
}
```

`pkg/storage/storage_helpers.go:312` — the surrounding closure keeps its skip:

```go
		n, err := decodeNodeRecordAt(gs.mmapSnap.data, off)
		if err != nil {
			// A damaged record is skipped rather than crashing the walk. The
			// invariant checker sees it as a missing record, which is the
			// signal we want. PR B revisits this: the checker should report
			// the damage instead of inferring it.
			return
		}
```

`pkg/storage/mmap_snapshot_persist.go:38` — this site already refuses rather than
drops, so only the shape changes:

```go
			e, err := decodeEdgeRecordAt(gs.mmapSnap.data, off)
			if err != nil {
				// Refuse rather than drop. Skipping the record here would turn
				// a damaged byte into permanent data loss at the next
				// snapshot, and the operator would never see it happen.
				damaged = id
				return
			}
```

- [ ] **Step 7: Correct the two stale doc comments**

`mmap_snapshot_format.go:585-586`:

```go
// scanNodeFields reads only the indexed prefix (id, tenant, labels) without
// allocating the property bag.
//
// NO PRODUCTION CALLER as of 2026-08-29 — only mmap_snapshot_format_test.go
// reaches it. The comment that used to say "used by the loader to build the
// in-memory indexes cheaply" described an intent, not the code. Deleting these
// two functions is a separate decision; while they stand they are the only
// bool-returning decoders left in this file.
```

and the matching line above `scanEdgeFields` at `:646`:

```go
// scanEdgeFields reads only the indexed prefix (id, tenant, from, to, type).
// No production caller — see scanNodeFields.
```

- [ ] **Step 8: Run the test to verify it passes**

```bash
go build ./... && go vet ./...
go test ./pkg/storage/ -run 'TestDecodeNodeRecordAt_' -v -count=1 -timeout 300s
```

Expected: PASS, both subtests.

- [ ] **Step 9: Run the whole storage suite — it must be unchanged**

```bash
go test ./pkg/storage/ -short -count=1 -timeout 300s
```

Expected: PASS. Any new failure here is a behaviour change, which this task must
not contain.

- [ ] **Step 10: Commit**

```bash
gofmt -s -l ./pkg ./cmd    # must print nothing
git add pkg/storage/errors.go pkg/storage/mmap_snapshot_format.go \
        pkg/storage/mmap_snapshot_reader.go pkg/storage/storage_helpers.go \
        pkg/storage/mmap_snapshot_persist.go pkg/storage/record_reason_test.go
git commit -m "refactor(storage): the record decoder reports why it failed

recordCursor carries the first failure's cause. readProps and the two
record decoders return an error that wraps ErrRecordUnreadable over
errRecordDamaged or alloc.ErrNoMemory.

The three direct callers collapse the error back to a bool, so nothing
above this layer changes yet.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 3: `mmapSnapshot.getNode`/`getEdge` return the reason

**Files:**
- Modify: `pkg/storage/mmap_snapshot_reader.go:188-204`
- Modify: `pkg/storage/mmap_snapshot_loader.go:168`, `:178`, `:196`, `:210`, `:226`, `:239`
- Modify: `pkg/storage/invariants.go:511` and its edge counterpart
- Modify: `pkg/storage/mmap_corruption_fuzz_test.go:82,88` (the `exerciseSnapshot` discards)

**Interfaces:**
- Consumes: `decodeNodeRecordAt`, `decodeEdgeRecordAt` from Task 2.
- Produces:
  - `func (m *mmapSnapshot) getNode(id uint64) (*Node, error)` — `ErrNodeNotFound` when the directory has no entry.
  - `func (m *mmapSnapshot) getEdge(id uint64) (*Edge, error)` — `ErrEdgeNotFound` likewise.

- [ ] **Step 1: Write the failing test**

Append to `pkg/storage/record_reason_test.go`:

```go
func TestMmapSnapshotGetNode_AbsentIsNotUnreadable(t *testing.T) {
	path := snapshotOnDisk(t) // helper in mmap_oom_test.go
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = snap.close() }()

	// Positive control: a node the directory lists must come back.
	var present uint64
	snap.forEachNodeID(func(id uint64, _ int64) {
		if present == 0 {
			present = id
		}
	})
	if present == 0 {
		t.Fatal("fixture has no nodes, so this test proves nothing")
	}
	if _, err := snap.getNode(present); err != nil {
		t.Fatalf("node %d must decode: %v", present, err)
	}

	_, maxID := snap.nodeIDRange()
	_, err = snap.getNode(maxID + 1000)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("an ID outside the directory must be ErrNodeNotFound, got %v", err)
	}
	if errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("absence must never be reported as unreadable: %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./pkg/storage/ -run TestMmapSnapshotGetNode_AbsentIsNotUnreadable -v -count=1 -timeout 300s
```

Expected: compile failure — `snap.getNode` returns `(*Node, bool)`, so
`errors.Is(err, ...)` does not type-check.

- [ ] **Step 3: Move the two methods**

`pkg/storage/mmap_snapshot_reader.go`:

```go
// getNode returns the node's record, or ErrNodeNotFound when the directory has
// no entry for the ID. Any OTHER error means the entry exists and the record
// could not be produced — see ErrRecordUnreadable. The two must never be
// confused: one is a fact about the graph, the other about this file.
func (m *mmapSnapshot) getNode(id uint64) (*Node, error) {
	off, ok := m.nodeOffset(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	return decodeNodeRecordAt(m.data, off)
}

func (m *mmapSnapshot) getEdge(id uint64) (*Edge, error) {
	off, ok := m.edgeOffset(id)
	if !ok {
		return nil, ErrEdgeNotFound
	}
	return decodeEdgeRecordAt(m.data, off)
}
```

- [ ] **Step 4: Adapt the six loader sites, still collapsing**

In `pkg/storage/mmap_snapshot_loader.go`, each of the six helpers keeps its bool
return in this task. Replace each `gs.mmapSnap.getNode(id)` tail:

```go
	// resolveNodeRefLocked, line ~168
	n, err := gs.mmapSnap.getNode(id)
	return n, err == nil
```

```go
	// resolveEdgeRefLocked, line ~178
	e, err := gs.mmapSnap.getEdge(id)
	return e, err == nil
```

```go
	// resolveNodeRefOwnedLocked, line ~196
	fresh, err := gs.mmapSnap.getNode(id) // fresh decode — already owned
	return fresh, err == nil, err == nil
```

```go
	// resolveEdgeRefOwnedLocked, line ~210
	fresh, err := gs.mmapSnap.getEdge(id)
	return fresh, err == nil, err == nil
```

```go
	// materializeNodeLocked, line ~226
	n, err := gs.mmapSnap.getNode(id)
	if err != nil {
		return nil, false
	}
	gs.storeNodeInShard(n) // promote into the overlay
	return n, true
```

```go
	// materializeEdgeLocked, line ~239
	e, err := gs.mmapSnap.getEdge(id)
	if err != nil {
		return nil, false
	}
	gs.storeEdgeInShard(e)
	return e, true
```

- [ ] **Step 5: Adapt `invariants.go`**

At `pkg/storage/invariants.go:511` and the edge block below it:

```go
		if n, err := gs.mmapSnap.getNode(id); err == nil {
			liveNodes[id] = n
		}
```

Apply the same shape to the edge loop. Leave the semantics alone: PR B changes
what the checker does with a damaged record.

- [ ] **Step 6: Adapt the fuzz driver**

`pkg/storage/mmap_corruption_fuzz_test.go:82,88` already discards both returns
(`_, _ = snap.getNode(id)`). The blank identifiers still compile against an
`error`, so no edit is needed. Confirm by building the test binary rather than
assuming:

```bash
go vet ./pkg/storage/
```

- [ ] **Step 7: Run the test to verify it passes**

```bash
go test ./pkg/storage/ -run 'TestMmapSnapshotGetNode_|TestDecodeNodeRecordAt_' -v -count=1 -timeout 300s
```

Expected: PASS.

- [ ] **Step 8: Run the whole suite**

```bash
go test ./pkg/storage/ -short -count=1 -timeout 300s
```

Expected: PASS, unchanged.

- [ ] **Step 9: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/storage/mmap_snapshot_reader.go pkg/storage/mmap_snapshot_loader.go \
        pkg/storage/invariants.go pkg/storage/record_reason_test.go
git commit -m "refactor(storage): mmapSnapshot.getNode returns a reason, not a bool

An ID outside the directory is ErrNodeNotFound. A record that exists and
does not decode is ErrRecordUnreadable. The loader helpers still collapse
both to a bool, so no behaviour moves in this commit.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 4: The six loader helpers return the reason

This is the bulk of PR A: one atomic signature change plus 43 call sites. It must
be inert. Any behaviour change here is a defect.

**Files:**
- Modify: `pkg/storage/mmap_snapshot_loader.go:161-243`
- Modify: `pkg/storage/node_operations.go:152`, `:331`, `:360`, `:418`, `:463`, `:543`, `:710`
- Modify: `pkg/storage/edge_operations.go:177`, `:204`, `:243`, `:274`, `:327`, `:486`, `:514`, `:600`, `:650`
- Modify: `pkg/storage/batch_executor.go:169`, `:240`, `:281`, `:294`, `:341`
- Modify: `pkg/storage/node_adjacency.go:65`, `:95`
- Modify: `pkg/storage/storage_helpers.go:82`, `:100`
- Modify: `pkg/storage/index_operations.go:128`
- Modify: `pkg/storage/transaction_commit.go:125`
- Modify: `pkg/storage/persistence_replay.go:79`, `:127`, `:160`, `:177`, `:218`, `:258`
- Modify: `pkg/storage/query_operations.go:8`, `:23`
- Modify: `pkg/storage/tenant_operations.go:154`, `:188`, `:222`, `:256`
- Modify: `pkg/storage/pagination.go:78`, `:110`, `:136`, `:167`

**Interfaces:**
- Consumes: `getNode`/`getEdge` from Task 3.
- Produces:
  - `func (gs *GraphStorage) resolveNodeRefLocked(id uint64) (*Node, error)`
  - `func (gs *GraphStorage) resolveEdgeRefLocked(id uint64) (*Edge, error)`
  - `func (gs *GraphStorage) resolveNodeRefOwnedLocked(id uint64) (n *Node, owned bool, err error)`
  - `func (gs *GraphStorage) resolveEdgeRefOwnedLocked(id uint64) (e *Edge, owned bool, err error)`
  - `func (gs *GraphStorage) materializeNodeLocked(id uint64) (*Node, error)`
  - `func (gs *GraphStorage) materializeEdgeLocked(id uint64) (*Edge, error)`

  In all six, absence is `ErrNodeNotFound` or `ErrEdgeNotFound`. `owned` keeps its
  meaning and is only valid when `err == nil`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/storage/record_reason_test.go`:

```go
func TestResolveNodeRefLocked_AbsentIsNotFound(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Positive control: a node that exists resolves with no error.
	if _, err := gs.resolveNodeRefLocked(n.ID); err != nil {
		t.Fatalf("existing node must resolve: %v", err)
	}
	// Absence is ErrNodeNotFound, never a bare nil or an unreadable.
	_, err = gs.resolveNodeRefLocked(n.ID + 9999)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("want ErrNodeNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./pkg/storage/ -run TestResolveNodeRefLocked_AbsentIsNotFound -v -count=1 -timeout 300s
```

Expected: compile failure — the helper returns `(*Node, bool)`.

- [ ] **Step 3: Move the six helpers**

Replace `pkg/storage/mmap_snapshot_loader.go:157-243` with:

```go
// resolveNodeRefLocked returns the current node for id: the shard overlay if
// present, else the lazily-materialized mmap base (a fresh copy), respecting
// tombstones. When mmapSnap == nil this is exactly lookupNodeShard. Caller holds
// the appropriate read lock (rlockShard for the hot path, or gs.mu).
//
// Absence is ErrNodeNotFound. Any other error means the ID IS in the snapshot
// and its record could not be produced — see ErrRecordUnreadable. A caller that
// treats every error as absence reintroduces the defect this signature exists
// to prevent.
func (gs *GraphStorage) resolveNodeRefLocked(id uint64) (*Node, error) {
	if n, ok := gs.lookupNodeShard(id); ok {
		return n, nil
	}
	if gs.mmapSnap == nil || gs.isNodeDeletedLocked(id) {
		return nil, ErrNodeNotFound
	}
	return gs.mmapSnap.getNode(id)
}

func (gs *GraphStorage) resolveEdgeRefLocked(id uint64) (*Edge, error) {
	if e, ok := gs.lookupEdgeShard(id); ok {
		return e, nil
	}
	if gs.mmapSnap == nil || gs.isEdgeDeletedLocked(id) {
		return nil, ErrEdgeNotFound
	}
	return gs.mmapSnap.getEdge(id)
}

// resolveNodeRefOwnedLocked is resolveNodeRefLocked with an ownership flag:
// owned==true means the returned *Node is a fresh, caller-owned copy
// (materialized from the mmap base) that may be returned directly without
// cloning; owned==false means it is a live overlay shard pointer that the
// caller MUST Clone before handing out to external callers. owned is only
// meaningful when err == nil.
//
// When mmapSnap==nil only the overlay branch is reachable, so owned is always
// false and the behaviour is identical to resolveNodeRefLocked.
func (gs *GraphStorage) resolveNodeRefOwnedLocked(id uint64) (n *Node, owned bool, err error) {
	if sh, hit := gs.lookupNodeShard(id); hit {
		return sh, false, nil // live overlay pointer — caller must Clone
	}
	if gs.mmapSnap == nil || gs.isNodeDeletedLocked(id) {
		return nil, false, ErrNodeNotFound
	}
	fresh, err := gs.mmapSnap.getNode(id) // fresh decode — already owned
	if err != nil {
		return nil, false, err
	}
	return fresh, true, nil
}

// resolveEdgeRefOwnedLocked is resolveEdgeRefLocked plus an `owned` flag: owned==true
// means the returned *Edge is a fresh, caller-owned copy (mmap-base decode) returnable
// directly; owned==false means a live overlay pointer the caller MUST Clone. owned is
// always false when mmapSnap == nil, and only meaningful when err == nil.
func (gs *GraphStorage) resolveEdgeRefOwnedLocked(id uint64) (e *Edge, owned bool, err error) {
	if sh, hit := gs.lookupEdgeShard(id); hit {
		return sh, false, nil
	}
	if gs.mmapSnap == nil || gs.isEdgeDeletedLocked(id) {
		return nil, false, ErrEdgeNotFound
	}
	fresh, err := gs.mmapSnap.getEdge(id)
	if err != nil {
		return nil, false, err
	}
	return fresh, true, nil
}

// materializeNodeLocked returns the node's shard-resident pointer, promoting it
// from the mmap base into the shard overlay first if needed (copy-on-write).
// Used by the write path before in-place mutation. Caller holds gs.mu.Lock AND
// lockShard(id). When mmapSnap == nil this is exactly lookupNodeShard.
func (gs *GraphStorage) materializeNodeLocked(id uint64) (*Node, error) {
	if n, ok := gs.lookupNodeShard(id); ok {
		return n, nil
	}
	if gs.mmapSnap == nil || gs.isNodeDeletedLocked(id) {
		return nil, ErrNodeNotFound
	}
	n, err := gs.mmapSnap.getNode(id)
	if err != nil {
		return nil, err
	}
	gs.storeNodeInShard(n) // promote into the overlay
	return n, nil
}

func (gs *GraphStorage) materializeEdgeLocked(id uint64) (*Edge, error) {
	if e, ok := gs.lookupEdgeShard(id); ok {
		return e, nil
	}
	if gs.mmapSnap == nil || gs.isEdgeDeletedLocked(id) {
		return nil, ErrEdgeNotFound
	}
	e, err := gs.mmapSnap.getEdge(id)
	if err != nil {
		return nil, err
	}
	gs.storeEdgeInShard(e)
	return e, nil
}
```

- [ ] **Step 4: Adapt the call sites that already return an error — pass it through**

These sites shrink. In every case the existing behaviour is preserved exactly,
because the helper now returns the very sentinel the site was constructing.

`node_operations.go:331` (`GetNode`):

```go
	node, owned, err := gs.resolveNodeRefOwnedLocked(nodeID)
	if err != nil {
		gs.recordOperation("get_node", "error", start)
		return nil, ErrNodeNotFound // PR B: return err
	}
```

`node_operations.go:360` (`GetNodeForTenant`):

```go
	node, owned, err := gs.resolveNodeRefOwnedLocked(nodeID)
	if err != nil {
		return nil, ErrNodeNotFound // PR B: return err
	}
```

`node_operations.go:418` (`getNodeRefForTenant`):

```go
	node, err := gs.resolveNodeRefLocked(nodeID)
	if err != nil {
		return nil, ErrNodeNotFound // PR B: return err
	}
```

`node_operations.go:463` and `:543` (`materializeNodeLocked` on the write path):

```go
	node, err := gs.materializeNodeLocked(nodeID)
	gs.unlockShard(nodeID)
	if err != nil {
		gs.mu.Unlock()
		return ErrNodeNotFound // PR B: return err
	}
```

`node_operations.go:710` (`DeleteNode`):

```go
	node, err := gs.resolveNodeRefLocked(nodeID)
	if err != nil {
		gs.mu.Unlock()
		return ErrNodeNotFound // PR B: return err
	}
```

`edge_operations.go:177`, `:243`, `:274`, `:327` — the same shape with
`ErrEdgeNotFound`. For `:243` and `:274` the helper is
`resolveEdgeRefOwnedLocked`, so the middle return is `owned`:

```go
	edge, owned, err := gs.resolveEdgeRefOwnedLocked(edgeID)
	if err != nil {
		return nil, ErrEdgeNotFound // PR B: return err
	}
```

`edge_operations.go:204` (a `fmt.Errorf` site — keep the message):

```go
	edge, err := gs.resolveEdgeRefLocked(edgeID)
	if err != nil {
		gs.unlockShard(edgeID)
		return fmt.Errorf("edge %d not found", edgeID) // PR B: wrap err
	}
```

`storage_helpers.go:82` (`verifyNodeExists`):

```go
	if _, err := gs.resolveNodeRefLocked(nodeID); err != nil {
		return fmt.Errorf("%s node %d not found", nodeType, nodeID) // PR B: wrap err
	}
```

`storage_helpers.go:100` (`verifyNodeExistsForTenant`):

```go
	node, err := gs.resolveNodeRefLocked(nodeID)
	if err != nil {
		return fmt.Errorf("%s node %d not found: %w", nodeType, nodeID, ErrNodeNotFound) // PR B: wrap err
	}
```

`batch_executor.go:169`:

```go
	node, err := b.graph.materializeNodeLocked(op.nodeID)
	b.graph.unlockShard(op.nodeID)
	if err != nil {
		return fmt.Errorf("node %d not found", op.nodeID) // PR B: wrap err
	}
```

`transaction_commit.go:125`:

```go
		node, err := tx.gs.materializeNodeLocked(nodeID)
		tx.gs.unlockShard(nodeID)
		if err != nil {
			// validateLocked guaranteed existence + ownership; defensive only.
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: update target %d vanished", nodeID) // PR B: wrap err
		}
```

- [ ] **Step 5: Adapt the call sites that skip or return nil — keep skipping**

Each of these currently treats `!exists` as "carry on". PR A keeps that exactly.
Every one gets a `// PR B:` marker so the follow-up work has a checklist.

`node_operations.go:152` (unique-constraint scan):

```go
		existing, err := gs.resolveNodeRefLocked(existingID)
		if err != nil {
			continue // PR B: an unreadable record must not be read as "no conflict"
		}
```

`batch_executor.go:240` and `:341`:

```go
	node, err := b.graph.resolveNodeRefLocked(op.nodeID)
	if err != nil {
		return nil // Skip non-existent nodes. PR B: distinguish unreadable
	}
```

```go
	edge, err := b.graph.resolveEdgeRefLocked(op.edgeID)
	if err != nil {
		return nil // Skip non-existent edges. PR B: distinguish unreadable
	}
```

`batch_executor.go:281` and `:294`:

```go
		if edge, err := b.graph.resolveEdgeRefLocked(edgeID); err == nil {
```

`node_adjacency.go:65` and `:95`:

```go
	edge, err := gs.resolveEdgeRefLocked(edgeID)
	if err != nil {
		gs.unlockShard(edgeID)
		return nil // PR B: an unreadable edge must not silently skip its cascade
	}
```

`edge_operations.go:486`, `:514`, `:650`:

```go
		edge, err := gs.resolveEdgeRefLocked(edgeID)
		if err != nil {
			continue // PR B: distinguish unreadable
		}
```

For `:650` the `gs.runlockShard(edgeID)` call stays between the resolve and the
check, exactly as now.

`edge_operations.go:600` — this site already discards the second return:

```go
		edge, _ := gs.materializeEdgeLocked(existing.ID) // mmap mode: promote base edge
```

The blank identifier compiles unchanged against an `error`. **Leave it, and do
not add a marker**: it is a pre-existing nil-dereference risk that PR B must fix,
so record it in Task 5's notes rather than papering over it here.

`index_operations.go:128`:

```go
		node, err := gs.resolveNodeRefLocked(nodeID)
		if err != nil {
			continue // PR B: distinguish unreadable
		}
```

`persistence_replay.go:79`, `:177` (skip when already present):

```go
	if _, err := gs.resolveNodeRefLocked(node.ID); err == nil {
		return nil
	}
```

```go
	if _, err := gs.resolveEdgeRefLocked(edge.ID); err == nil {
		return nil
	}
```

`persistence_replay.go:127`, `:160` (skip when absent):

```go
	node, err := gs.materializeNodeLocked(updateInfo.NodeID)
	if err != nil {
		return nil // PR B: distinguish unreadable
	}
```

```go
	existing, err := gs.materializeEdgeLocked(edge.ID)
	if err != nil {
		return nil // PR B: distinguish unreadable
	}
```

`persistence_replay.go:218`, `:258` (skip when absent, inverted test):

```go
	if _, err := gs.resolveEdgeRefLocked(edge.ID); err != nil {
		return nil // PR B: distinguish unreadable
	}
```

```go
	if _, err := gs.resolveNodeRefLocked(node.ID); err != nil {
		return nil // PR B: distinguish unreadable
	}
```

`query_operations.go:8`, `:23`:

```go
		if node, owned, err := gs.resolveNodeRefOwnedLocked(nodeID); err == nil {
```

```go
		if edge, owned, err := gs.resolveEdgeRefOwnedLocked(edgeID); err == nil {
```

`tenant_operations.go:154`, `:188`:

```go
		if node, owned, err := gs.resolveNodeRefOwnedLocked(id); err == nil {
```

```go
		if edge, owned, err := gs.resolveEdgeRefOwnedLocked(id); err == nil {
```

`tenant_operations.go:222` and `:256` — the `exists` name is used twice each, so
convert it to a local bool to keep the shape:

```go
		node, owned, err := gs.resolveNodeRefOwnedLocked(id)
		exists := err == nil
		// owned (mmap-base) nodes are already heap-safe — no Clone and safe to keep after unlock.
		if exists && !owned {
			node = node.Clone()
		}
		gs.runlockShard(id)
		if exists {
```

```go
		edge, owned, err := gs.resolveEdgeRefOwnedLocked(id)
		exists := err == nil
		// owned (mmap-base) edges are already heap-safe — no Clone and safe to keep after unlock.
		if exists && !owned {
			edge = edge.Clone()
		}
		gs.runlockShard(id)
		if exists {
```

`pagination.go:78`, `:110`, `:136`, `:167` — the four `cloneAt` closures keep
their `(T, bool)` shape, because `pageFromSortedIDs` is generic over it and PR C
changes that:

```go
	cloneAt := func(id uint64) (*Node, bool) {
		gs.rlockShard(id)
		n, owned, err := gs.resolveNodeRefOwnedLocked(id)
		ok := err == nil
		if ok && !owned {
			n = n.Clone()
		}
		gs.runlockShard(id)
		return n, ok
	}
```

and the two edge closures with `*Edge` and `e`.

- [ ] **Step 6: Run the new test to verify it passes**

```bash
go build ./... && go vet ./...
go test ./pkg/storage/ -run 'TestResolveNodeRefLocked_|TestMmapSnapshotGetNode_|TestDecodeNodeRecordAt_' -v -count=1 -timeout 300s
```

Expected: PASS.

- [ ] **Step 7: Prove the change is inert**

```bash
go test ./pkg/storage/ -short -count=1 -timeout 300s
go test ./pkg/query/ ./pkg/graphql/ ./pkg/api/ ./pkg/search/ ./pkg/algorithms/ -short -count=1 -timeout 300s
```

Expected: PASS everywhere, with no test edited in this task. A test that needed
editing is a behaviour change, which this task must not contain. If one does,
stop and report it rather than editing the test.

- [ ] **Step 8: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/storage/
git commit -m "refactor(storage): the resolve and materialize helpers return a reason

Six helpers move from (T, bool) to (T, error). Absence is ErrNodeNotFound
or ErrEdgeNotFound; any other error means the record exists and could not
be produced.

All 43 call sites collapse the error back to today's answer, so behaviour
does not move. Each site that will need a decision in PR B carries a
'// PR B:' marker.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 5: PR A gates, benchmark, and the pull request

**Files:**
- No source change unless a gate fails.

**Interfaces:**
- Consumes: everything from Tasks 2-4.
- Produces: PR A, ready for review.

- [ ] **Step 1: Run every gate**

```bash
go build ./... && go vet ./...
gofmt -s -l ./pkg ./cmd                                  # must print nothing
make lint-local
make lint-local-selftest                                 # prove the linter can report
go test ./pkg/storage/ -count=1 -timeout 300s
go test -race ./pkg/storage/ -run 'Mmap|Snapshot|Concurrent' -count=3 -timeout 300s
make contract-guard
make dccc-cover && make dccc
scripts/contract-guard-selftest.sh
scripts/dccc-selftest.sh
```

Record each verdict with the artefact it read. A PASS about a different artefact
than the one edited is not evidence about this change.

- [ ] **Step 2: Measure the hot-path cost**

`errors.Is` replaced a bool test on the single-node read path. Measure it, and
change one thing when attributing the result.

`git stash` cannot produce this baseline: the work is committed, so there is
nothing to stash and both runs would measure the same tree. Use a second
worktree at `origin/main` instead, so one variable changes.

```bash
git worktree add "$SCRATCH/bench-base" origin/main
( cd "$SCRATCH/bench-base" && go test ./pkg/storage/ -run XXX \
    -bench 'BenchmarkGetNode|BenchmarkGetEdge' -benchtime 3s -count=5 ) > "$SCRATCH/bench-before.txt"
go test ./pkg/storage/ -run XXX \
    -bench 'BenchmarkGetNode|BenchmarkGetEdge' -benchtime 3s -count=5 > "$SCRATCH/bench-after.txt"
benchstat "$SCRATCH/bench-before.txt" "$SCRATCH/bench-after.txt"
git worktree remove "$SCRATCH/bench-base"
```

Confirm the benchmark names exist before running: `grep -n 'func Benchmark' pkg/storage/bench_concurrent_read_test.go`.
A `-bench` pattern that matches nothing prints no benchmark lines and exits 0,
which reads exactly like a run with no difference.

If `benchstat` is not installed, quote the raw ns/op ranges from both files
instead of installing a tool. Note the machine and that the numbers are local,
not CI's.

- [ ] **Step 3: Confirm the red-first probe is still red**

Re-run Task 1's probe against this branch. It must **still fail**, because PR A
changes no public behaviour. That is PR A's inertness evidence on the axis that
matters.

```bash
cp "$SCRATCH/redfirst_probe_test.go" pkg/storage/zz_redfirst_probe_test.go
go test ./pkg/storage/ -run TestProbeDamagedRecordIsNotReportedAsMissing -v -count=1 -timeout 300s 2>&1 | tee "$SCRATCH/redfirst-prA.txt"
rm pkg/storage/zz_redfirst_probe_test.go
```

Expected: FAIL with the same message as on `main`. If it now passes, PR A is not
inert and something leaked.

- [ ] **Step 4: Open the pull request**

```bash
git push -u origin fix/storage-absent-versus-refused
gh pr create --title "refactor(storage): a reason code for the mmap read path (PR A of 3)" --body "$(cat <<'EOF'
## What

`pkg/storage`'s mmap read path returns one bool for three causes: no directory
entry, a record that did not decode, and a refused allocation. This moves the
decoder, `mmapSnapshot.getNode`/`getEdge` and the six resolve/materialize helpers
to `(T, error)`, where absence is `ErrNodeNotFound`.

**This PR changes no behaviour.** Every public boundary collapses the error back
to today's answer. It exists so that the contract change in PR B is a small diff
against a converted tree.

## Why the planning doc's framing was too narrow

Item 4 scopes this to memory pressure. `alloc.Bytes` cannot fail in production —
with no allocator installed it is a plain `make`. The production defect is the
second cause: the snapshot CRC covers the header, the directories and the
metadata blob, **not record bodies**, so a record damaged by bit rot or a partial
write survives open and reads as an absent node today.

## Evidence

- **Inert**: the full `pkg/storage` suite passes with no test edited. `pkg/query`,
  `pkg/graphql`, `pkg/api`, `pkg/search` and `pkg/algorithms` likewise.
- **Red-first probe still red**: [paste the failure line from redfirst-prA.txt].
  It failed identically on `main`: [paste from redfirst-main.txt]. PR B turns it
  green.
- **New unit tests**: damage and refusal now report distinct reasons, each with a
  positive control and, for the refusal case, an `alloc.Allocs() > 0` negative
  control.
- **Benchmarks**: [paste the ns/op comparison].
- **Gates**: [paste each verdict, naming the artefact it read].

## Incidental finding

`scanNodeFields` and `scanEdgeFields` have no production caller, contrary to
their doc comment. This PR corrects the comment only.

## Next

PR B stops collapsing on single-record reads. PR C does the enumerations, which
needs a `pkg/storage/interface.go` change and touches 26 sites in five packages.

Spec: `docs/superpowers/specs/2026-08-29-absent-versus-refused-design.md`
Plan: `docs/superpowers/plans/2026-08-29-absent-versus-refused.md`

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb
EOF
)"
```

- [ ] **Step 5: Verify CI is actually green**

```bash
gh pr view --json statusCheckRollup --jq '.statusCheckRollup[] | "\(.name) \(.conclusion)"'
```

Do not read a piped exit code. Read the conclusions.

---

# PR B — the single-record contract

Branch from PR A's tip: `git checkout -b fix/storage-unreadable-single-record`.

## Task 6: Land the red-first gate

**Files:**
- Create: `pkg/storage/damaged_record_test.go`

**Interfaces:**
- Consumes: `ErrRecordUnreadable`, `mmapConfig`, `mmapSnapshotPath`, `openMmapSnapshot`, `decodeNodeRecordAt` from PR A.
- Produces: `TestDamagedRecordIsNotReportedAsMissing`, the gate every later task in PR B must keep green.

- [ ] **Step 1: Write the test**

`pkg/storage/damaged_record_test.go` — this is Task 1's probe, strengthened from
"not ErrNodeNotFound" to "is ErrRecordUnreadable", plus an undamaged control:

```go
package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

// The snapshot CRC covers the header, the directories and the metadata blob —
// NOT record bodies (mmap_snapshot_format.go, "bounds-checked record reading").
// So a record damaged by bit rot, a partial write or a truncated copy survives
// open and reaches a decoder. Until this test, that record read as a MISSING
// node, and a query returned an incomplete result with nothing to signal it.
//
// SQLite's equivalent cannot happen: sqlite3_malloc failure returns
// SQLITE_NOMEM and a malformed page returns SQLITE_CORRUPT, neither of which
// can be mistaken for an empty result (sqlite.org/malloc.html).
func TestDamagedRecordIsNotReportedAsMissing(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	survivor, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("beta")})
	if err != nil {
		t.Fatalf("create survivor: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(target.ID)
	if !ok {
		t.Fatalf("node %d has no directory entry", target.ID)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Byte 8 of a node record is the tenant-length prefix. 0xFFFF asks for more
	// bytes than the file holds, so the bounds check refuses the record. The
	// CRC does not cover this byte, so the file still opens.
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption. Without it, a test that stopped
	// corrupting anything would still pass.
	if _, decErr := decodeNodeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	_, err = gs2.GetNodeForTenant(target.ID, "")
	if err == nil {
		t.Fatal("a damaged record must not read as a successful lookup")
	}
	if errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a damaged record was reported as missing: %v", err)
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}

	// NEGATIVE CONTROL: the undamaged neighbour still reads. Without this the
	// test would pass just as well if reopen had failed entirely.
	if _, err := gs2.GetNodeForTenant(survivor.ID, ""); err != nil {
		t.Fatalf("the undamaged node must still read: %v", err)
	}

	// An ID that was never in the directory is still plain absence.
	if _, err := gs2.GetNodeForTenant(survivor.ID+9999, ""); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a genuinely missing node must stay ErrNodeNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./pkg/storage/ -run TestDamagedRecordIsNotReportedAsMissing -v -count=1 -timeout 300s 2>&1 | tee "$SCRATCH/redfirst-prB.txt"
```

Expected: FAIL with `a damaged record was reported as missing: node not found`.
Record the exact line. It goes in PR B's body.

- [ ] **Step 3: Commit the red test on its own**

Committing the test before the fix makes the red state a reviewable artefact.

```bash
git add pkg/storage/damaged_record_test.go
git commit -m "test(storage): a damaged record must not read as a missing node

Red against PR A's tree. GetNodeForTenant returns ErrNodeNotFound for a
record that exists and does not decode.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 7: Stop collapsing on single-record reads

**Files:**
- Modify: `pkg/storage/node_operations.go:331`, `:360`, `:418`, `:463`, `:543`, `:710`
- Modify: `pkg/storage/edge_operations.go:177`, `:204`, `:243`, `:274`, `:327`, `:600`
- Modify: `pkg/storage/storage_helpers.go:82`, `:100`
- Modify: `pkg/storage/batch_executor.go:169`
- Modify: `pkg/storage/transaction_commit.go:125`

**Interfaces:**
- Consumes: the six helpers' `error` returns from PR A.
- Produces: `GetNode`, `GetNodeForTenant`, `getNodeRefForTenant`, `WithNodeRefForTenant`, `GetEdge`, `GetEdgeForTenant`, `getEdgeRefForTenant`, `UpdateNodeProperties`, `RemoveNodeProperty`, `DeleteNode`, `UpdateEdgeProperties`, `verifyNodeExists`, `verifyNodeExistsForTenant` all propagate `ErrRecordUnreadable` instead of `ErrNodeNotFound`/`ErrEdgeNotFound`.

- [ ] **Step 1: Replace each collapse with a pass-through**

At every site PR A marked `// PR B: return err`, delete the marker and return the
error:

```go
	node, owned, err := gs.resolveNodeRefOwnedLocked(nodeID)
	if err != nil {
		gs.recordOperation("get_node", "error", start)
		return nil, err
	}
```

The tenant check below it is unchanged and still returns the unified
`ErrNodeNotFound`:

```go
	if node.TenantID != effectiveTenantID(tenantID).String() {
		// Cross-tenant: same error as missing to avoid existence-leak side channel.
		//
		// An UNREADABLE record is the one documented exception to this rule.
		// Its tenant string lives inside the record, so the failure necessarily
		// precedes this check, and the error therefore reveals that the node
		// directory holds an entry at this ID. The alternative — consulting the
		// per-tenant membership section first — costs a lookup (~11ms at 937k
		// nodes) on the single-node read path, which is the cost cheap reopen
		// exists to avoid. See the spec, "The tenant side-channel".
		return nil, ErrNodeNotFound
	}
```

At the `fmt.Errorf` sites marked `// PR B: wrap err`, wrap rather than replace,
so the existing message survives for callers that match on text:

```go
	// edge_operations.go:204
	edge, err := gs.resolveEdgeRefLocked(edgeID)
	if err != nil {
		gs.unlockShard(edgeID)
		return fmt.Errorf("edge %d not found: %w", edgeID, err)
	}
```

```go
	// storage_helpers.go:82
	if _, err := gs.resolveNodeRefLocked(nodeID); err != nil {
		return fmt.Errorf("%s node %d not found: %w", nodeType, nodeID, err)
	}
```

```go
	// storage_helpers.go:100 — an unreadable record must NOT become the
	// unified ErrNodeNotFound, so branch before the existing wrap.
	node, err := gs.resolveNodeRefLocked(nodeID)
	if err != nil {
		if errors.Is(err, ErrRecordUnreadable) {
			return fmt.Errorf("%s node %d: %w", nodeType, nodeID, err)
		}
		return fmt.Errorf("%s node %d not found: %w", nodeType, nodeID, ErrNodeNotFound)
	}
```

```go
	// batch_executor.go:169
	node, err := b.graph.materializeNodeLocked(op.nodeID)
	b.graph.unlockShard(op.nodeID)
	if err != nil {
		return fmt.Errorf("node %d not found: %w", op.nodeID, err)
	}
```

```go
	// transaction_commit.go:125
	node, err := tx.gs.materializeNodeLocked(nodeID)
	tx.gs.unlockShard(nodeID)
	if err != nil {
		// validateLocked guaranteed existence + ownership; defensive only.
		tx.gs.mu.Unlock()
		return fmt.Errorf("commit: update target %d vanished: %w", nodeID, err)
	}
```

- [ ] **Step 2: Fix the discarded error at `edge_operations.go:600`**

PR A left this alone deliberately. It discards the second return and then
dereferences the result, so a base edge that fails to decode is a nil
dereference today.

```go
		// concurrent GetEdge readers. A4-edges.
		gs.lockShard(existing.ID)
		edge, err := gs.materializeEdgeLocked(existing.ID) // mmap mode: promote base edge
		if err != nil {
			gs.unlockShard(existing.ID)
			gs.mu.Unlock()
			return nil, fmt.Errorf("upsert edge %d: %w", existing.ID, err)
		}
```

**Check the surrounding lock and return shape before pasting this.** Read
`edge_operations.go:570-620` and match the function's own unlock discipline and
return arity. If the function returns `(*Edge, error)`, the above is right. If it
returns only `error`, drop the `nil`.

- [ ] **Step 3: Run the gate**

```bash
go build ./... && go vet ./...
go test ./pkg/storage/ -run TestDamagedRecordIsNotReportedAsMissing -v -count=1 -timeout 300s
```

Expected: PASS. This is the moment the defect closes.

- [ ] **Step 4: Run the full suite and the dependent packages**

```bash
go test ./pkg/storage/ -count=1 -timeout 300s
go test ./pkg/query/ ./pkg/graphql/ ./pkg/api/ ./pkg/search/ ./pkg/algorithms/ -count=1 -timeout 300s
```

Expected: PASS. `pkg/api` needs no source edit: its handlers already branch
`errors.Is(err, storage.ErrNodeNotFound)` → 404 and default → 500, so an
unreadable record becomes a 500. If a `pkg/api` test now fails, read it before
editing it — it may be asserting the defect.

- [ ] **Step 5: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/storage/
git commit -m "fix(storage): a single-record read reports an unreadable record

GetNode, GetNodeForTenant, GetEdge, GetEdgeForTenant and the update,
delete and verify paths return ErrRecordUnreadable instead of collapsing
it to ErrNodeNotFound.

Also fixes a nil dereference at edge_operations.go:600, where UpsertEdge
discarded materializeEdgeLocked's failure and then wrote through the nil
pointer.

pkg/api needs no change: it already routes a non-ErrNodeNotFound error to
500.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 8: The invariant checker reports damage

**Files:**
- Modify: `pkg/storage/storage_helpers.go:295-322` (`forEachNodeUnlocked`)
- Modify: `pkg/storage/invariants.go:505-515` and the edge block
- Test: `pkg/storage/damaged_record_test.go` (append)

**Interfaces:**
- Consumes: `decodeNodeRecordAt`'s error, `gs.mmapSnap.getNode`'s error.
- Produces: `CheckInvariants` returns a violation naming the damaged record's ID.

- [ ] **Step 1: Write the failing test**

Append to `pkg/storage/damaged_record_test.go`:

```go
func TestCheckInvariantsReportsADamagedRecord(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(target.ID)
	if !ok {
		t.Fatalf("node %d has no directory entry", target.ID)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	err = gs2.CheckInvariants()
	if err == nil {
		t.Fatal("a damaged record must be an invariant violation, not a missing node")
	}
	if !strings.Contains(err.Error(), strconv.FormatUint(target.ID, 10)) {
		t.Fatalf("the violation must name the damaged record: %v", err)
	}
}
```

Add `"strconv"` and `"strings"` to the file's imports.

**Check `CheckInvariants`' real signature first** — read
`pkg/storage/invariants.go` and match it. If it returns `([]string, error)` or
takes an argument, adjust the call and the assertion accordingly. Do not guess.

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./pkg/storage/ -run TestCheckInvariantsReportsADamagedRecord -v -count=1 -timeout 300s
```

Expected: FAIL — the checker treats the damaged record as missing, so it reports
no violation.

- [ ] **Step 3: Make `forEachNodeUnlocked` stop hiding damage**

`pkg/storage/storage_helpers.go:295-322` — the walk cannot return an error
(callers pass a `func(*Node) bool`), so record the damage on the store for the
checker to read:

```go
	stopped := false
	gs.mmapSnap.forEachNodeID(func(id uint64, off int64) {
		if stopped {
			return
		}
		if _, shadowed := gs.lookupNodeShard(id); shadowed || gs.isNodeDeletedLocked(id) {
			return
		}
		n, err := decodeNodeRecordAt(gs.mmapSnap.data, off)
		if err != nil {
			// The walk cannot fail, so it records instead. This used to
			// skip silently, and the invariant checker inferred a missing
			// record from the gap — an inference indistinguishable from a
			// genuine deletion.
			gs.recordDamagedNode(id, err)
			return
		}
		if !fn(n) {
			stopped = true
		}
	})
```

Add the recorder beside the other `GraphStorage` helpers. Keep it simple and
lock-free-safe: the walk already holds `gs.mu` at every caller, so a plain map
guarded by the same lock is correct. Verify that claim by reading each caller of
`forEachNodeUnlocked` before choosing the guard.

```go
// recordDamagedNode notes a record the walk could not decode, so CheckInvariants
// can report it rather than infer a missing node from the gap. Caller holds gs.mu.
func (gs *GraphStorage) recordDamagedNode(id uint64, cause error) {
	if gs.damagedNodes == nil {
		gs.damagedNodes = make(map[uint64]error)
	}
	gs.damagedNodes[id] = cause
}
```

Declare `damagedNodes map[uint64]error` on the `GraphStorage` struct, beside the
other mmap-overlay fields (`deletedNodes`, `deletedEdges`).

- [ ] **Step 4: Make `CheckInvariants` report it**

In `pkg/storage/invariants.go`, in the ground-truth block at `:505-515`:

```go
	gs.mmapSnap.forEachNodeID(func(id uint64, _ int64) {
		if gs.isNodeDeletedLocked(id) {
			return
		}
		n, err := gs.mmapSnap.getNode(id)
		if err != nil {
			report("node %d: the snapshot directory lists it and its record does not decode: %v", id, err)
			return
		}
		liveNodes[id] = n
	})
```

Apply the same shape to the edge loop below it. Also drain `gs.damagedNodes` into
`report` so damage seen by a walk outside the checker is not lost:

```go
	for id, cause := range gs.damagedNodes {
		report("node %d was skipped by an earlier walk: %v", id, cause)
	}
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
go test ./pkg/storage/ -run 'TestCheckInvariantsReportsADamagedRecord|TestDamagedRecordIsNotReportedAsMissing' -v -count=1 -timeout 300s
```

Expected: PASS, both.

- [ ] **Step 6: Run the full suite**

```bash
go test ./pkg/storage/ -count=1 -timeout 300s
go test -race ./pkg/storage/ -run 'Mmap|Snapshot|Invariant' -count=3 -timeout 300s
```

Expected: PASS. The `-race` run matters here because `damagedNodes` is new shared
state.

- [ ] **Step 7: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/storage/
git commit -m "fix(storage): CheckInvariants reports a damaged record

The mmap walk used to skip a record that would not decode, and the
checker inferred a missing node from the gap. That inference is
indistinguishable from a genuine deletion.

The walk now records the ID and the cause, and the checker reports both
its own decode failures and the recorded ones.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 9: The OOM sweep asserts completeness

**Files:**
- Modify: `pkg/storage/mmap_oom_test.go:37-53` (`readAll`), `:56-115` (the sweep)

**Interfaces:**
- Consumes: `mmapSnapshot.getNode`'s error from PR A.
- Produces: `TestSnapshotReadPathUnderAllocationFailure` asserts that no listed ID vanishes silently.

- [ ] **Step 1: Move `readAll` to the error return**

```go
// readAll opens the snapshot and returns every node it could decode, keyed by
// ID, plus the IDs the directory listed and the decoder refused, with why.
func readAll(path string) (map[uint64]*Node, map[uint64]error, error) {
	snap, err := openMmapSnapshot(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = snap.close() }()

	got := make(map[uint64]*Node)
	refused := make(map[uint64]error)
	snap.forEachNodeID(func(id uint64, _ int64) {
		n, err := snap.getNode(id)
		if err != nil {
			refused[id] = err
			return
		}
		got[id] = n
	})
	return got, refused, nil
}
```

- [ ] **Step 2: Promote the log line to an assertion**

In the sweep body, after the existing safety checks:

```go
					// COMPLETENESS. The safety property above says a returned
					// node is right. This says a node that did not come back
					// said so. Before this assertion the sweep logged
					// "worst case %d of %d nodes unreadable" and passed.
					for id, cause := range missing {
						if !errors.Is(cause, ErrRecordUnreadable) {
							return errSilentlyAbsent(id, cause)
						}
					}
					// Every listed ID is accounted for: decoded, or refused
					// with a reason.
					if len(got)+len(missing) != len(want) {
						return errUnaccounted(len(got), len(missing), len(want))
					}
```

with the two error constructors beside the existing ones:

```go
func errSilentlyAbsent(id uint64, cause error) error {
	return nodeErr{msg: "node " + strconv.FormatUint(id, 10) +
		" was absent for a reason that is not ErrRecordUnreadable: " + cause.Error()}
}
func errUnaccounted(decoded, refused, want int) error {
	return nodeErr{msg: "the directory listed " + strconv.Itoa(want) +
		" nodes; " + strconv.Itoa(decoded) + " decoded and " +
		strconv.Itoa(refused) + " were refused — the rest vanished"}
}
```

Update the reference read and the two `missing`/`worstAbsent` uses to the new map
shape. Add `"errors"` to the imports.

- [ ] **Step 3: Run it**

```bash
go test ./pkg/storage/ -run TestSnapshotReadPathUnderAllocationFailure -v -count=1 -timeout 300s
```

Expected: PASS, with the log line now reporting refusals rather than vanishings.

- [ ] **Step 4: Prove the assertion is a gate**

Per `CLAUDE.md` § "Red-first", a test nobody has watched go red is the artefact
most likely to be lying.

The usual mechanic — `git checkout <pre-fix-sha> -- <source files>` — cannot work
here, because the signature changed and the reverted file will not compile. A
compile failure is not evidence about the assertion.

Prove it by mutation instead. Edit `readAll` so a refusal is dropped rather than
recorded:

```go
		n, err := snap.getNode(id)
		if err != nil {
			return // MUTATION: do not record the refusal
		}
```

Then run the sweep and confirm the `errUnaccounted` branch fires:

```bash
go test ./pkg/storage/ -run TestSnapshotReadPathUnderAllocationFailure -v -count=1 -timeout 300s 2>&1 | tee "$SCRATCH/mutation-red.txt"
```

Expected: FAIL with `the directory listed N nodes; X decoded and 0 were refused —
the rest vanished`. Record the line, then revert the mutation with
`git checkout -- pkg/storage/mmap_oom_test.go` and re-run to confirm green.

- [ ] **Step 5: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/storage/mmap_oom_test.go
git commit -m "test(storage): the OOM sweep asserts completeness, not only safety

The sweep proved that a returned node was correct and LOGGED how many
vanished. It now requires that every ID the directory lists either
decodes or comes back with ErrRecordUnreadable.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 10: PR B docs, gates and the pull request

**Files:**
- Modify: `docs/CONSUMER_CONTRACTS.md` (append one row)
- Modify: `docs/internals/design/SQLITE_TESTING_SCORECARD.md` (row 4)
- Modify: `docs/NEXT_STEPS_2026-06-18.md` (item 4)
- Modify: `CHANGELOG.md` if the repo keeps one — check before assuming

**Interfaces:**
- Consumes: Tasks 6-9.
- Produces: PR B, ready for review.

- [ ] **Step 1: Add the consumer-contract row**

Match the existing column order exactly: `| id | Invariant | Consumer(s) | Guarding test(s) | Origin |`.

```markdown
| CC<N>-unreadable-not-missing | A record the snapshot directory lists but cannot decode returns `ErrRecordUnreadable`, never `ErrNodeNotFound` | all readers | `pkg/storage` `TestDamagedRecordIsNotReportedAsMissing`, `TestCheckInvariantsReportsADamagedRecord` | #<PR B> |
```

Read the last row's id to pick `<N>`. Then:

```bash
make contract-guard
scripts/contract-guard-selftest.sh
```

The guard checks the registry against the tests that enforce it, so a row with a
wrong test name fails here rather than in review.

- [ ] **Step 2: Update scorecard row 4**

Row 4 covers OOM gating of the read path. Add that the read path now reports its
refusals, and that LSM blocks and query results remain ungated. Do not mark the
row ✓ — the spec's out-of-scope list keeps two SQLite behaviours deliberately
absent.

- [ ] **Step 3: Close planning-doc item 4 partially**

`docs/NEXT_STEPS_2026-06-18.md` item 4: mark the single-record half done with the
PR number, and leave the enumeration half open, pointing at PR C. Correct the
item's framing while there: the production cause is a damaged record body, not
memory pressure.

- [ ] **Step 4: Run every gate**

```bash
go build ./... && go vet ./...
gofmt -s -l ./pkg ./cmd
make lint-local && make lint-local-selftest
go test ./pkg/storage/ -count=1 -timeout 300s
go test -race ./pkg/storage/ -run 'Mmap|Snapshot|Invariant|Concurrent' -count=3 -timeout 300s
go test ./pkg/query/ ./pkg/graphql/ ./pkg/api/ ./pkg/search/ ./pkg/algorithms/ -count=1 -timeout 300s
make contract-guard && scripts/contract-guard-selftest.sh
make dccc-cover && make dccc && scripts/dccc-selftest.sh
```

- [ ] **Step 5: Open the pull request**

```bash
git push -u origin fix/storage-unreadable-single-record
gh pr create --base fix/storage-absent-versus-refused \
  --title "fix(storage): a single-record read stops lying about existence (PR B of 3)" \
  --body "$(cat "$SCRATCH/prB-body.md")"
```

Write `$SCRATCH/prB-body.md` first. It must carry, at minimum:

- The red-first line from `$SCRATCH/redfirst-prB.txt`, quoted verbatim.
- The statement that `pkg/api` needed no edit, and why.
- The tenant side-channel decision and its cost, from the spec.
- The availability note: a deployment already serving a damaged snapshot starts
  failing single-record reads that used to return `ErrNodeNotFound`.
- Each gate verdict with the artefact it read.

**Base this PR on PR A's branch, not on `main`.** Then, before merging PR A,
retarget this one: `gh pr edit <B> --base main`. Merging PR A with
`--delete-branch` while B is still based on it auto-CLOSES B, and GitHub refuses
to reopen it.

---

# PR C — the enumeration contract

Not planned in detail here. It changes `pkg/storage/interface.go`, which the
user's global `CLAUDE.md` says to propose rather than implement unilaterally.

**Scope, measured 2026-08-29:**

| Item | Count |
|---|---|
| Public methods gaining an `error` | 7 (4 page methods + 3 enumerators) |
| Interface declarations to change | 3 (`interface.go:43,44,50`) |
| Second implementation to follow | `BTreeGraphStorage` (`btree_storage.go:124,150,262`) |
| Production call sites outside `pkg/storage` | 26 across `pkg/graphql` (14), `pkg/query` (6), `pkg/api` (4), `pkg/search` (1), `pkg/algorithms` (1) |
| Internal call sites | 5 |
| Total references including tests | 179 |

**Before starting PR C:** put the interface change to the user, with the count
above and the abort-versus-skip decision from the spec restated. `pageFromSortedIDs`'s
`cloneAt func(uint64) (*T, bool)` becomes `func(uint64) (*T, error)` at the same
time.

---

## Self-Review

**Spec coverage.**

| Spec section | Task |
|---|---|
| Error taxonomy | Task 2, Step 3 |
| Internal helper shape | Tasks 2, 3, 4 |
| Multi-record read policy: abort | PR C (deferred, with the reason stated) |
| Tenant side-channel: accept and document | Task 7, Step 1 |
| Blast radius | Task 7, Step 4 (`pkg/api` needs no edit); PR C table |
| Track structure | PR A / PR B / PR C headings |
| Red-first evidence, gate 1 | Tasks 1 and 6 |
| Red-first evidence, gate 2 | Task 9 |
| Red-first evidence, negative control | Task 6, Step 1 |
| Out of scope | PR C section; nothing here builds `SkipUnreadable` |
| Risk 1: behaviour regression | Task 10, Step 5 |
| Risk 2: hot-path cost | Task 5, Step 2 |
| Risk 3: a large diff hides a semantic change | Task 4, Step 7 |
| Risk 4: `storage_helpers.go:312` | Task 8 |
| Risk 5: dead `scan*Fields` | Task 2, Step 7 |

**Gap accepted deliberately:** the spec's abort-everywhere policy for
multi-record reads lands in PR C, not PR B. PR B leaves `buildNodeListFromIDs`
and the enumerators skipping, with `// PR B:` markers converted to PR C's
checklist. This is stated in Task 4, Step 5 and in the PR C section rather than
left implicit.

**Type consistency.** `ErrRecordUnreadable` and `errRecordDamaged` are spelled
identically in Tasks 2, 3, 6, 7, 8 and 9. The six helper signatures in Task 4's
Interfaces block match their bodies in Step 3 and their call sites in Steps 4-5.
`readAll` returns `(map[uint64]*Node, map[uint64]error, error)` in Task 9 Step 1
and is consumed with that arity in Step 2.

**Two places the executor must read the code before pasting**, flagged inline
rather than guessed: `CheckInvariants`' real signature (Task 8, Step 1) and
`UpsertEdge`'s lock and return shape (Task 7, Step 2).

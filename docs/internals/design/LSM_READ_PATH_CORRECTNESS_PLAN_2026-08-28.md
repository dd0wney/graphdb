# LSM read path correctness — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three confirmed defects in `pkg/lsm`'s read path, and add the model-based test that would have found all three.

**Architecture:** `MemTable.Get` and `SSTable.Get` return `(nil, false)` both for a tombstone and for an absent key. A caller that searches several levels in turn cannot stop at a delete, so it falls through to an older level holding the pre-delete value. New `GetEntry` and `ScanEntries` methods carry the third state. `lsm.Scan` is rewritten as a newest-first merge, which also settles a disagreement with `Get` about which SSTable is newer.

**Tech Stack:** Go, `testing`, `math/rand`.

**Spec:** `docs/internals/design/CONCURRENT_FAULT_INJECTION_2026-08-28.md`, section "The hypotheses this design must confirm or refute". This plan implements H3 and H4 from that document. It ships **ahead of** the concurrency work in `CONCURRENT_FAULT_INJECTION_PLAN_2026-08-28.md`, and that plan depends on it.

## Why this is its own deliverable

The concurrency spec framed H3 as a precondition — it hides H2 from the compaction oracle, so it must be fixed before that scenario can attribute a failure. That framing undersells it.

These are not preconditions. They are correctness defects in an exported API, and two of them are the kind a database is not allowed to have:

- **H3: `Delete` has no effect on any key that has been flushed.** Confirmed by probe.
- **H3b: the same, through `Scan`.** Confirmed by probe.
- **H4: `Get` and `Scan` return different values for the same key.** Confirmed by probe: `Get` returned `v2`, `Scan` returned `v1`. The answer to a query depends on which access path the caller took.

**Reachability, checked rather than assumed.** `pkg/storage/lsm_storage_nodes.go:81,139,146,152` and `lsm_storage_edges.go:86,89,94` delete nodes, edges, labels and property-index entries through `lsm.Delete`. They belong to `LSMGraphStorage`, whose only non-test constructor caller is `cmd/benchmark-graph-storage/main.go:160`. The shipped server builds `GraphStorage`, which never touches `pkg/lsm`. `EdgeStore`, which `UseDiskBackedEdges` does wire into the production path, has **no delete method at all** — `node_adjacency.go:30` rewrites the whole adjacency list through `StoreOutgoingEdges`. So the defect is live for a library consumer of `pkg/storage` and latent for `cmd/server`.

## The uncomfortable part

All three are pure single-threaded properties. All three were found by reading the code and writing a six-line probe by hand, during the design of a concurrency harness. None of them needed the harness, a fault driver, a sweep, or a fuzzer.

The repository had, on the day they were found, fifteen tracked testing techniques and twenty-two merged pull requests of fault injection. It had no model-based test of the LSM read path — no random operation sequence checked against a reference `map[string][]byte`, and no assertion that `Get` and `Scan` agree. Either would have caught these in its first hundred operations.

Task 3 closes that, and it comes with the fixes rather than after them. A fault-injection programme that runs ahead of the correctness programme finds sophisticated failures in code whose ordinary behaviour is wrong.

## Global Constraints

- `golangci-lint run ./...` must pass **in CI**. The local linter on the development machine fails typecheck on a Go 1.26 standard library file, which suppresses the analysers and reports a clean-looking environment error. It missed real findings on #479 and #485. Never accept a local lint pass as evidence.
- `gofmt` must be clean.
- Every `//nolint:` directive must carry a reason after the lint name.
- **No fix lands without a test that would have exhibited the bug.** Thirteen defects are worth thirteen tests only if the tests stay in the suite.
- Conventional commits, imperative mood, one logical change per commit.
- `go test ./pkg/lsm/ ./pkg/storage/ -short -timeout 300s -count=1` must pass after every task.
- Do not change the on-disk SSTable format. No task here needs to.
- Branch: `fix/lsm-read-path`, taken from `origin/main`. This is a separate pull request from the concurrency work.

## Task order

| Task | Depends on |
|---|---|
| 1 — `Get` must stop at a tombstone | nothing |
| 2 — `Scan` must merge newest-first and stop at a tombstone | nothing |
| 3 — A model-based test of the read path | 1, 2 |

---

### Task 1: `Get` must stop at a tombstone

**Why this is first.** `MemTable.Get` returns `(nil, false)` for a tombstone, which `lsm.Get` cannot tell from "absent". It therefore continues to the SSTable scan and returns the pre-delete value. `SSTable.Get` has the same shape. A `Delete` has no effect on any key that has been flushed. Confirmed by probe on 2026-08-28.

**The fix is additive, not a change of meaning.** `Get` and `Scan` on `MemTable` and `SSTable` filter tombstones today, and about 25 existing test assertions in `pkg/lsm` depend on that. New `GetEntry` and `ScanEntries` methods carry the third state, and the old methods stay as filtering wrappers.

**Files:**
- Modify: `pkg/lsm/memtable.go` (add `GetEntry` after `Get`, line 72-83)
- Modify: `pkg/lsm/sstable.go` (split `Get`, lines 10-63)
- Modify: `pkg/lsm/lsm.go` (`Get`, lines 75-115)
- Create: `pkg/lsm/tombstone_test.go`

**Interfaces:**
- Produces: `(*MemTable).GetEntry(key []byte) (*Entry, bool)`, `(*SSTable).GetEntry(key []byte) (*Entry, bool)`, `(*LSMStorage).resolve(cacheKey string, entry *Entry) ([]byte, bool)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/lsm/tombstone_test.go`:

```go
package lsm

import "testing"

// newTombstoneStore returns a store with the background workers off, so a
// flush happens only where the test asks for one.
func newTombstoneStore(t *testing.T) *LSMStorage {
	t.Helper()
	opts := DefaultLSMOptions(t.TempDir())
	opts.EnableAutoCompaction = false
	l, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// A tombstone in the memtable must hide a value that has already been flushed
// into an SSTable. Before this test, Delete had no effect on a flushed key:
// MemTable.Get returns (nil, false) for a tombstone, which lsm.Get cannot tell
// from "absent", so it fell through to the SSTable and returned the old value.
func TestGetTombstoneMasksFlushedValue(t *testing.T) {
	l := newTombstoneStore(t)

	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil { // forces the memtable into an SSTable
		t.Fatalf("Sync: %v", err)
	}
	if _, ok := l.Get([]byte("k")); !ok {
		t.Fatal("the key vanished across the flush, so the test cannot prove anything")
	}
	if err := l.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if v, ok := l.Get([]byte("k")); ok {
		t.Errorf("Get returned %q for a deleted key, want absent", v)
	}
}

// The same, with the tombstone itself flushed into a newer SSTable.
func TestGetFlushedTombstoneMasksOlderValue(t *testing.T) {
	l := newTombstoneStore(t)

	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := l.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := l.Sync(); err != nil { // the tombstone reaches its own SSTable
		t.Fatalf("Sync: %v", err)
	}

	if v, ok := l.Get([]byte("k")); ok {
		t.Errorf("Get returned %q for a deleted key, want absent", v)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/lsm/ -run 'TestGet.*Tombstone|TestGetFlushed' -v -count=1`

Expected: both FAIL, with `Get returned "v" for a deleted key, want absent`.

- [ ] **Step 3: Add `MemTable.GetEntry`**

In `pkg/lsm/memtable.go`, immediately after `Get`:

```go
// GetEntry returns the entry for a key, tombstone included.
//
// Get cannot express "deleted here": it returns (nil, false) both for a
// tombstone and for an absent key, so a caller that searches several levels in
// turn cannot stop at a delete and falls through to an older level holding the
// pre-delete value. GetEntry carries that third state. lsm.Get uses it, and
// Get stays as the filtering form its own callers expect.
func (mt *MemTable) GetEntry(key []byte) (*Entry, bool) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	entry, exists := mt.data[string(key)]
	if !exists {
		return nil, false
	}
	return entry, true
}
```

- [ ] **Step 4: Split `SSTable.Get`**

In `pkg/lsm/sstable.go`, rename `Get` to `GetEntry`, change its tombstone arm, and add a filtering `Get`. The only edit inside the body is the `entry.Deleted` arm:

```go
// GetEntry retrieves the entry for a key, tombstone included. See
// MemTable.GetEntry for why the third state matters.
func (sst *SSTable) GetEntry(key []byte) (*Entry, bool) {
	// ... body unchanged, except this arm ...
		if cmp == string(key) {
			return entry, true
		}
	// ...
}

// Get retrieves a live value by key. A tombstone reads as absent.
func (sst *SSTable) Get(key []byte) (*Entry, bool) {
	entry, ok := sst.GetEntry(key)
	if !ok || entry.Deleted {
		return nil, false
	}
	return entry, true
}
```

- [ ] **Step 5: Make `lsm.Get` stop at the newest entry**

Replace the body of `Get` in `pkg/lsm/lsm.go` from the memtable check to the return:

```go
	// 1. Check active MemTable
	if entry, ok := lsm.memTable.GetEntry(key); ok {
		return lsm.resolve(cacheKey, entry)
	}

	// 2. Check immutable MemTable
	if lsm.immutableTable != nil {
		if entry, ok := lsm.immutableTable.GetEntry(key); ok {
			return lsm.resolve(cacheKey, entry)
		}
	}

	// 3. Check SSTables from newest to oldest
	for level := 0; level < len(lsm.levels); level++ {
		for i := len(lsm.levels[level]) - 1; i >= 0; i-- {
			if entry, ok := lsm.levels[level][i].GetEntry(key); ok {
				return lsm.resolve(cacheKey, entry)
			}
		}
	}

	return nil, false
}

// resolve turns the newest entry found for a key into a Get result.
//
// The first entry found wins, because the search runs newest to oldest. A
// tombstone therefore ends the search rather than being skipped, which is the
// whole point: skipping it revives the value in an older level.
//
// A tombstone is not cached. The cache holds live values only, and Delete
// removes the key from it. Caller holds lsm.mu.
func (lsm *LSMStorage) resolve(cacheKey string, entry *Entry) ([]byte, bool) {
	if entry.Deleted {
		return nil, false
	}
	lsm.cache.Put(cacheKey, entry.Value)
	return entry.Value, true
}
```

- [ ] **Step 6: Run the new tests and then the package**

Run: `go test ./pkg/lsm/ -run 'TestGet.*Tombstone|TestGetFlushed' -v -count=1`
Expected: PASS.

Run: `go test ./pkg/lsm/ ./pkg/storage/ -short -timeout 300s -count=1`
Expected: PASS. `pkg/storage` is included because `lsm_storage_nodes.go` and `lsm_storage_edges.go` delete through this path.

- [ ] **Step 7: Commit**

```bash
git add pkg/lsm/memtable.go pkg/lsm/sstable.go pkg/lsm/lsm.go pkg/lsm/tombstone_test.go
git commit -m "fix(lsm): a tombstone must hide a flushed value in Get

MemTable.Get returns (nil, false) for a tombstone, which lsm.Get could
not tell from \"absent\". It fell through to the SSTable scan and
returned the pre-delete value, so Delete had no effect on any key that
had reached an SSTable.

GetEntry carries the third state. Get and Scan keep their filtering
meaning, which about 25 existing assertions in this package rely on."
```

---

### Task 2: `Scan` must merge newest-first and stop at a tombstone

**Two defects, one fix.** `lsm.Scan` walks each level's SSTables in ascending index order and keeps the first value it sees, so for a key written twice into L0 it returns the **older** value. `Get` walks the same slice in reverse and returns the newer one. Separately, `MemTable.Scan` and `SSTable.Scan` drop tombstones, so a deleted key reappears from an older table. Both confirmed by probe on 2026-08-28: `Get` returned `v2` while `Scan` returned `v1`, and a deleted key appeared in the scan result.

**Files:**
- Modify: `pkg/lsm/memtable.go` (add `ScanEntries` beside `Scan`, lines 141-168)
- Modify: `pkg/lsm/sstable_read.go` (add `ScanEntries` beside `Scan`, lines 93-130)
- Modify: `pkg/lsm/lsm.go` (`Scan`, lines 131-169)
- Modify: `pkg/lsm/tombstone_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `(*MemTable).ScanEntries(start, end []byte) []*Entry`, `(*SSTable).ScanEntries(start, end []byte) ([]*Entry, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/lsm/tombstone_test.go`:

```go
// Scan must not revive a key whose tombstone lives in a newer level.
func TestScanTombstoneMasksFlushedValue(t *testing.T) {
	l := newTombstoneStore(t)

	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := l.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := l.Scan([]byte("a"), []byte("z"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if v, ok := got["k"]; ok {
		t.Errorf("Scan returned %q for a deleted key, want absent", v)
	}
}

// Scan must agree with Get about which of two L0 tables is newer.
func TestScanPrefersNewestSSTable(t *testing.T) {
	l := newTombstoneStore(t)

	for _, v := range []string{"v1", "v2"} {
		if err := l.Put([]byte("k"), []byte(v)); err != nil {
			t.Fatalf("Put %s: %v", v, err)
		}
		if err := l.Sync(); err != nil {
			t.Fatalf("Sync %s: %v", v, err)
		}
	}

	if v, ok := l.Get([]byte("k")); !ok || string(v) != "v2" {
		t.Fatalf("Get returned %q ok=%v, want v2: the test cannot prove anything", v, ok)
	}

	got, err := l.Scan([]byte("a"), []byte("z"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if string(got["k"]) != "v2" {
		t.Errorf("Scan returned %q, want v2: Scan and Get disagree about which SSTable is newer", got["k"])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/lsm/ -run 'TestScanTombstone|TestScanPrefersNewest' -v -count=1`

Expected: both FAIL. The first with `Scan returned "v" for a deleted key`, the second with `Scan returned "v1", want v2`.

- [ ] **Step 3: Add `MemTable.ScanEntries`**

In `pkg/lsm/memtable.go`, immediately before `Scan`:

```go
// ScanEntries returns entries in range [start, end), tombstones included.
//
// Scan drops tombstones, which is right for a caller looking at one table and
// wrong for a caller merging several: the dropped tombstone lets an older
// table revive the key. lsm.Scan merges, so it uses this.
func (mt *MemTable) ScanEntries(start, end []byte) []*Entry {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if !mt.sorted {
		sort.Strings(mt.keys)
		mt.sorted = true
	}

	startStr := string(start)
	endStr := string(end)

	results := make([]*Entry, 0)
	for _, key := range mt.keys {
		if key >= endStr {
			break
		}
		if key >= startStr {
			results = append(results, mt.data[key])
		}
	}

	return results
}
```

- [ ] **Step 4: Add `SSTable.ScanEntries`**

In `pkg/lsm/sstable_read.go`, rename `Scan` to `ScanEntries`, delete the `if !entry.Deleted` guard so every entry is appended, and add a filtering `Scan` beneath it:

```go
// ScanEntries returns entries in range [start, end), tombstones included. See
// MemTable.ScanEntries for why a merging caller needs them.
func (sst *SSTable) ScanEntries(start, end []byte) ([]*Entry, error) {
	// ... body unchanged, except the append arm ...
		keyStr := string(entry.Key)
		if keyStr >= string(start) && keyStr < string(end) {
			results = append(results, entry)
		}
	// ...
}

// Scan returns live entries in range [start, end). A tombstone is omitted.
func (sst *SSTable) Scan(start, end []byte) ([]*Entry, error) {
	entries, err := sst.ScanEntries(start, end)
	if err != nil {
		return nil, err
	}
	live := make([]*Entry, 0, len(entries))
	for _, entry := range entries {
		if !entry.Deleted {
			live = append(live, entry)
		}
	}
	return live, nil
}
```

- [ ] **Step 5: Rewrite `lsm.Scan` as a newest-first merge**

Replace the body of `Scan` in `pkg/lsm/lsm.go` after the `RLock`:

```go
	// seen records every key already resolved, tombstones included. Without it
	// an older level revives a key that a newer level deleted or replaced.
	seen := make(map[string]struct{})
	results := make(map[string][]byte)

	take := func(entries []*Entry) {
		for _, entry := range entries {
			k := string(entry.Key)
			if _, done := seen[k]; done {
				continue
			}
			seen[k] = struct{}{}
			if !entry.Deleted {
				results[k] = entry.Value
			}
		}
	}

	// Newest to oldest, the same order Get uses. Within a level the last
	// SSTable is the most recently written one.
	take(lsm.memTable.ScanEntries(start, end))
	if lsm.immutableTable != nil {
		take(lsm.immutableTable.ScanEntries(start, end))
	}
	for level := 0; level < len(lsm.levels); level++ {
		for i := len(lsm.levels[level]) - 1; i >= 0; i-- {
			entries, err := lsm.levels[level][i].ScanEntries(start, end)
			if err != nil {
				continue
			}
			take(entries)
		}
	}

	return results, nil
```

- [ ] **Step 6: Run the tests**

Run: `go test ./pkg/lsm/ -run 'TestScan' -v -count=1`
Expected: PASS.

Run: `go test ./pkg/lsm/ ./pkg/storage/ -short -timeout 300s -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/lsm/memtable.go pkg/lsm/sstable_read.go pkg/lsm/lsm.go pkg/lsm/tombstone_test.go
git commit -m "fix(lsm): Scan must merge newest-first and honour tombstones

Scan walked each level's SSTables in ascending order and kept the first
value found, so a key written twice into L0 read back as the older
value while Get returned the newer one. It also dropped tombstones
before the merge, so an older table revived a deleted key.

ScanEntries carries tombstones to the merge. Scan on MemTable and
SSTable keep their filtering meaning."
```

---


---

### Task 3: A model-based test of the read path

**Why this task exists.** Tasks 1 and 2 fix three defects that a random operation sequence would have found in its first hundred operations. The repository had none. This is the test that makes the next one of these cheap to find, and it is deliberately the last task rather than the first, because it must be shown to fail on the unfixed code before it is trusted.

Two properties, and they need different amounts of machinery:

- **Reference equivalence.** An LSM store must behave like a `map[string][]byte`. Random `Put`, `Delete`, `Get` and `Scan`, compared with the map after every operation, again after a flush, and again after a reopen.
- **Self-consistency.** `Get(k)` must agree with `Scan`'s entry for `k`. This needs **no reference model at all** and would have caught H4 on its own. It is the cheaper property and it goes in first.

**Files:**
- Create: `pkg/lsm/model_test.go`

**Interfaces:**
- Consumes: `(*LSMStorage).Put`, `.Delete`, `.Get`, `.Scan`, `.Sync`, `.Close` and `NewLSMStorage` — no new production API.

- [ ] **Step 1: Write the self-consistency property first**

Create `pkg/lsm/model_test.go`:

```go
package lsm

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// opKind is one operation in a generated sequence.
type opKind int

const (
	opPut opKind = iota
	opDelete
	opSync
	opReopen
)

// modelOp is one generated operation. It is a value rather than a closure so a
// failing sequence can be printed and replayed by hand.
type modelOp struct {
	kind  opKind
	key   string
	value string
}

func (o modelOp) String() string {
	switch o.kind {
	case opPut:
		return fmt.Sprintf("Put(%q, %q)", o.key, o.value)
	case opDelete:
		return fmt.Sprintf("Delete(%q)", o.key)
	case opSync:
		return "Sync()"
	case opReopen:
		return "Reopen()"
	}
	return "?"
}

// generate builds a sequence biased towards collisions: a small key space, so
// the same key is written, deleted and rewritten across several flushes. A
// uniform key space over a large alphabet almost never revisits a key, and a
// tombstone that is never followed by a read of the same key proves nothing.
func generate(rng *rand.Rand, n int) []modelOp {
	const keySpace = 8
	ops := make([]modelOp, 0, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k%d", rng.Intn(keySpace))
		switch roll := rng.Intn(100); {
		case roll < 45:
			ops = append(ops, modelOp{kind: opPut, key: key, value: fmt.Sprintf("v%d", i)})
		case roll < 70:
			ops = append(ops, modelOp{kind: opDelete, key: key})
		case roll < 92:
			ops = append(ops, modelOp{kind: opSync})
		default:
			ops = append(ops, modelOp{kind: opReopen})
		}
	}
	return ops
}

// Get and Scan must answer the same question the same way. This needs no
// reference model: it is a property of the store against itself, and it is the
// cheapest test that would have caught the newest-SSTable disagreement.
func TestGetAndScanAgree(t *testing.T) {
	const seeds = 32

	for seed := int64(0); seed < seeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			ops := generate(rng, 120)

			dir := t.TempDir()
			l := openModelStore(t, dir)

			for i, op := range ops {
				l = applyModelOp(t, l, dir, op)

				scanned, err := l.Scan([]byte("k0"), []byte("l"))
				if err != nil {
					t.Fatalf("after %d ops (%v): Scan: %v", i+1, op, err)
				}
				for k := 0; k < 8; k++ {
					key := fmt.Sprintf("k%d", k)
					got, present := l.Get([]byte(key))
					want, inScan := scanned[key]

					if present != inScan {
						t.Fatalf("after %d ops (last: %v): Get(%q) present=%v but Scan present=%v\nsequence: %v",
							i+1, op, key, present, inScan, ops[:i+1])
					}
					if present && string(got) != string(want) {
						t.Fatalf("after %d ops (last: %v): Get(%q)=%q but Scan gives %q\nsequence: %v",
							i+1, op, key, got, want, ops[:i+1])
					}
				}
			}
			_ = l.Close()
		})
	}
}

// The store must behave like a map. This is the property that catches a
// tombstone which fails to hide an older value, whichever access path finds it.
func TestReadPathMatchesReferenceMap(t *testing.T) {
	const seeds = 32

	for seed := int64(0); seed < seeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed + 1000))
			ops := generate(rng, 120)

			dir := t.TempDir()
			l := openModelStore(t, dir)
			model := make(map[string]string)

			for i, op := range ops {
				switch op.kind {
				case opPut:
					model[op.key] = op.value
				case opDelete:
					delete(model, op.key)
				}
				l = applyModelOp(t, l, dir, op)

				for k := 0; k < 8; k++ {
					key := fmt.Sprintf("k%d", k)
					got, present := l.Get([]byte(key))
					want, inModel := model[key]

					if present != inModel {
						t.Fatalf("after %d ops (last: %v): Get(%q) present=%v, the model says %v\nsequence: %v",
							i+1, op, key, present, inModel, ops[:i+1])
					}
					if present && string(got) != want {
						t.Fatalf("after %d ops (last: %v): Get(%q)=%q, the model says %q\nsequence: %v",
							i+1, op, key, got, want, ops[:i+1])
					}
				}

				scanned, err := l.Scan([]byte("k0"), []byte("l"))
				if err != nil {
					t.Fatalf("after %d ops (%v): Scan: %v", i+1, op, err)
				}
				if len(scanned) != len(model) {
					t.Fatalf("after %d ops (last: %v): Scan returned %d keys, the model has %d\nscan: %v\nmodel: %v\nsequence: %v",
						i+1, op, len(scanned), len(model), sortedKeys(scanned), sortedModelKeys(model), ops[:i+1])
				}
			}
			_ = l.Close()
		})
	}
}

func openModelStore(t *testing.T, dir string) *LSMStorage {
	t.Helper()
	l, err := NewLSMStorage(LSMOptions{
		DataDir:      dir,
		MemTableSize: 128, // small, so the sequence crosses many flushes
		// The workers are off: this test is about the read path, not about
		// timing. A background flush would make a failure hard to replay.
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: false,
	})
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	return l
}

// applyModelOp performs one operation and returns the store to keep using,
// which differs from the one passed in only for a reopen.
func applyModelOp(t *testing.T, l *LSMStorage, dir string, op modelOp) *LSMStorage {
	t.Helper()
	switch op.kind {
	case opPut:
		if err := l.Put([]byte(op.key), []byte(op.value)); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	case opDelete:
		if err := l.Delete([]byte(op.key)); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	case opSync:
		if err := l.Sync(); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	case opReopen:
		if err := l.Close(); err != nil {
			t.Fatalf("close before reopen: %v", err)
		}
		return openModelStore(t, dir)
	}
	return l
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedModelKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 2: Prove the tests fail on the unfixed code**

This is the step that makes the tests worth keeping. A model-based test written after the fix, and never seen to fail, is a test whose generator may not reach the defect at all.

```bash
git stash            # sets the read path back to its unfixed state
go test ./pkg/lsm/ -run 'TestGetAndScanAgree|TestReadPathMatchesReferenceMap' -count=1 2>&1 | head -30
git stash pop
```

Expected: both FAIL, and within the first few seeds. Record in the commit message which seed failed first and after how many operations. If either test PASSES on the unfixed code, the generator does not reach the defect: raise the operation count, shrink the key space, or raise the `Delete` and `Sync` weights until it does. **Do not proceed until it has been seen to fail.**

- [ ] **Step 3: Run them against the fixed code**

Run: `go test ./pkg/lsm/ -run 'TestGetAndScanAgree|TestReadPathMatchesReferenceMap' -v -count=1`
Expected: PASS, all 64 subtests.

- [ ] **Step 4: Run the whole package and its consumer**

Run: `go test ./pkg/lsm/ ./pkg/storage/ -short -timeout 300s -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/lsm/model_test.go
git commit -m "test(lsm): a model-based test of the read path

Three defects fixed in the two previous commits were pure
single-threaded properties, and all three were found by reading the
code and writing a probe by hand. A random operation sequence compared
with a map[string][]byte finds every one of them in its first hundred
operations, and this repository had no such test.

Two properties. Get and Scan agreeing about the same key needs no
reference model and catches the newest-SSTable disagreement on its
own. Reference equivalence across flushes and reopens catches the
tombstones.

Both were run against the unfixed code first and seen to fail. A
generator that has never reached the defect is not evidence that the
defect is gone."
```

---

## Before the pull request

- [ ] `go build ./... && go vet ./...`
- [ ] `go test ./pkg/lsm/ ./pkg/storage/ -short -timeout 300s -count=1`
- [ ] `go test -race ./pkg/lsm/ -short -count=1`
- [ ] `gofmt -l pkg/ | wc -l` reports 0
- [ ] `/review` on the diff, then `/preflight`
- [ ] Open the pull request. Read `gh pr checks` conclusions directly — never a piped exit code.
- [ ] Say in the pull request body which seed and operation count first exhibited each defect. That number is the evidence the test is a gate rather than a decoration.

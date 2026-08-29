# Cross-Principal Observable Equivalence Sweep — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Assert mechanically that a caller who does not own a resource cannot distinguish it from a resource that never existed, at the HTTP boundary, across every endpoint that names a caller-supplied resource ID — including when the record is damaged on disk.

**Architecture:** One table of seven operations. For each, issue the same request as the owning tenant and as a stranger, against a resource that exists and against an ID that never existed, and compare the stranger's two observables byte for byte. Phase 1 runs against an intact store. Phase 2 repeats every row with the record damaged, which is the composition that catches the class no single-sided test sees.

**Tech Stack:** Go 1.26.0, `net/http/httptest`, `pkg/tenant` context injection, the existing `setupTestServer` harness in `pkg/api`.

**Spec:** `docs/superpowers/specs/2026-08-29-cross-principal-equivalence-design.md`

## Global Constraints

- Go 1.26.0 (`go.mod`); toolchain pinned to 1.26.4.
- `gofmt -s -l ./pkg ./cmd` MUST be empty. Plain `gofmt` is a weaker gate and passes what CI rejects.
- `golangci-lint run ./...` cannot typecheck `pkg/storage` here. Use `make lint-local`; exit 2 means a package did not load, which is not a result about the code.
- Every `//nolint:` directive MUST carry a reason after the lint name.
- `pkg/api` tests are slow — the package takes about 71 seconds. Use `-timeout 300s`.
- The observable is the pair (HTTP status code, response body), compared byte for byte. It is measured at the HTTP boundary and nowhere else.
- Response time is NOT in the observable. The spec records that as a stated limit.
- `$SCRATCH` means this session's scratchpad directory. Export it once:
  `export SCRATCH=/tmp/claude-1000/-mnt-ssd2-Workspace-github-com-graphdb/4474d5b1-0313-4e31-8e85-7af525dbfaeb/scratchpad`
- Commit messages use conventional-commit prefixes and imperative mood, and end with the two trailer lines this repository uses (`Co-Authored-By:` and `Claude-Session:`).

---

## File Structure

**Created**

| File | Responsibility |
|---|---|
| `pkg/api/equivalence_sweep_test.go` | The observable type, the request driver, the operation table, and both phases. One file, because the table and the driver change together and are meaningless apart. |

**Not modified.** This plan adds no production code. If a row fails, the fix belongs in the handler or in `pkg/storage`, in its own change.

---

## Task 1: The observable and the driver

**Files:**
- Create: `pkg/api/equivalence_sweep_test.go`

**Interfaces:**
- Consumes: `setupTestServer(t) (*Server, func())` from `pkg/api/server_test.go:16`; `tenant.WithTenant(ctx, tenantID) context.Context` from `pkg/tenant/context.go:14`.
- Produces:
  - `type observable struct { status int; body string }`
  - `func (o observable) String() string`
  - `func probe(t *testing.T, s *Server, tenantID, method, path, body string) observable`

- [ ] **Step 1: Write the failing test**

Create `pkg/api/equivalence_sweep_test.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// A cross-principal equivalence sweep.
//
// graphdb's tenant rule is that a cross-tenant lookup returns the same error as
// a missing one, so a stranger cannot learn that an ID exists. Six
// *TenantIsolation tests assert that a stranger cannot READ another tenant's
// data. None asserts that a stranger cannot DISTINGUISH it from data that never
// existed. Those are different properties and the second is the one that leaks.
//
// On 2026-08-29 a change made a damaged record return a different error class,
// so a stranger got 500 where a stranger asking for a nonexistent ID got 404 —
// an existence oracle. No test caught it. A security review did, and proving the
// fix complete took a reviewer enumerating every entry point by hand.
//
// This file replaces that enumeration with a table.

// observable is what a caller can see. The comparison is byte for byte.
//
// Response time is deliberately NOT here. A cross-tenant lookup that returns
// faster than a genuine miss is a leak this sweep cannot see, and the spec says
// so rather than leaving it to be discovered.
type observable struct {
	status int
	body   string
}

func (o observable) String() string {
	return fmt.Sprintf("%d %q", o.status, o.body)
}

// probe issues one request as one tenant and returns what the caller sees.
//
// The tenant goes into the request context directly rather than through a token,
// because this sweep tests the error-to-status mapping and not authentication.
// Driving the auth stack would add a second failure mode to every row.
func probe(t *testing.T, s *Server, tenantID, method, path, body string) observable {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r = r.WithContext(tenant.WithTenant(context.Background(), tenantID))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return observable{status: w.Code, body: strings.TrimSpace(w.Body.String())}
}

// TestProbeSeesTheTenant is a control on the driver itself. A probe that
// silently ignored its tenantID would make every later row pass by returning
// identical observables for the wrong reason.
func TestProbeSeesTheTenant(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	owned, err := s.graph.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	path := fmt.Sprintf("/nodes/%d", owned.ID)

	asOwner := probe(t, s, "owner", http.MethodGet, path, "")
	asStranger := probe(t, s, "stranger", http.MethodGet, path, "")

	if asOwner.status != http.StatusOK {
		t.Fatalf("the owner must be able to read its own node, got %s", asOwner)
	}
	if asOwner == asStranger {
		t.Fatalf("the probe ignores its tenant: owner and stranger both got %s", asOwner)
	}
}
```

- [ ] **Step 2: Run the control to verify it passes**

```bash
go test ./pkg/api/ -run TestProbeSeesTheTenant -v -count=1 -timeout 300s
```

Expected: PASS.

If it fails with the owner getting something other than 200, the tenant is not reaching the handler through the context. Read `pkg/api/middleware_tenant.go:113` — `getTenantFromContext` falls back to `tenant.DefaultTenantID` when the context carries nothing, so a broken injection makes BOTH probes the default tenant and the second assertion fires. That second assertion is why the control exists.

If `s.ServeHTTP` does not exist, find how the server exposes its mux (`grep -n "func (s \*Server) ServeHTTP\|s.mux\|http.Handler" pkg/api/server.go`) and drive that instead. Report which you used.

- [ ] **Step 3: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/api/equivalence_sweep_test.go
git commit -m "test(api): an observable, and a control that the probe sees its tenant

The sweep that follows compares what two principals can see. A probe that
ignored its tenant would make every row pass for the wrong reason, so the
driver lands with a control before any row uses it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 2: The table, and the clean sweep

**Files:**
- Modify: `pkg/api/equivalence_sweep_test.go` (append)

**Interfaces:**
- Consumes: `observable`, `probe` from Task 1.
- Produces:
  - `type sweepRow struct { name, method, pathFmt, bodyFmt string }`
  - `var equivalenceRows []sweepRow`
  - `func (r sweepRow) request(id uint64) (path, body string)`

- [ ] **Step 1: Write the failing test**

Append:

```go
// sweepRow is one operation that names a caller-supplied resource ID.
//
// pathFmt and bodyFmt each take the ID exactly once, through %d. A row puts the
// ID in one or the other, never both.
type sweepRow struct {
	name    string
	method  string
	pathFmt string
	bodyFmt string
}

func (r sweepRow) request(id uint64) (path, body string) {
	path = r.pathFmt
	if strings.Contains(path, "%d") {
		path = fmt.Sprintf(path, id)
	}
	if r.bodyFmt != "" {
		body = fmt.Sprintf(r.bodyFmt, id)
	}
	return path, body
}

// Every endpoint that names a resource the caller may not own.
//
// Rows 7 and 8 are POST /edges in each endpoint position. They are separate
// because CreateEdgeWithTenant verifies the source and the target in separate
// calls (edge_operations.go:61 and :64), and a guard on one is not a guard on
// the other. Row 7 is also the row a hand-written suite forgets, and it is the
// one that leaked on 2026-08-29.
var equivalenceRows = []sweepRow{
	{"GET node", http.MethodGet, "/nodes/%d", ""},
	{"PUT node", http.MethodPut, "/nodes/%d", `{"properties":{"x":"y"}}`},
	{"DELETE node", http.MethodDelete, "/nodes/%d", ""},
	{"GET edge", http.MethodGet, "/edges/%d", ""},
	{"PUT edge", http.MethodPut, "/edges/%d", `{"properties":{"x":"y"}}`},
	{"DELETE edge", http.MethodDelete, "/edges/%d", ""},
	{"POST edge, source position", http.MethodPost, "/edges",
		`{"from_node_id":%d,"to_node_id":1,"type":"LINKS"}`},
	{"POST edge, target position", http.MethodPost, "/edges",
		`{"from_node_id":1,"to_node_id":%d,"type":"LINKS"}`},
}

// TestCrossPrincipalEquivalence_IntactStore asserts that for every row, a
// stranger asking about a resource owned by another tenant sees exactly what a
// stranger asking about an ID that never existed sees.
func TestCrossPrincipalEquivalence_IntactStore(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	ownedNode, err := s.graph.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("alpha")})
	if err != nil {
		t.Fatalf("create owner node: %v", err)
	}
	otherNode, err := s.graph.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("beta")})
	if err != nil {
		t.Fatalf("create second owner node: %v", err)
	}
	ownedEdge, err := s.graph.CreateEdgeWithTenant("owner", ownedNode.ID, otherNode.ID,
		"LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("create owner edge: %v", err)
	}

	// An ID far beyond anything created. Both probes must agree that it is
	// absent, which is the reference the comparison is made against.
	const neverExisted uint64 = 999999

	for _, row := range equivalenceRows {
		t.Run(row.name, func(t *testing.T) {
			existing := ownedNode.ID
			if strings.Contains(row.pathFmt, "/edges/") {
				existing = ownedEdge.ID
			}

			ownedPath, ownedBody := row.request(existing)
			missingPath, missingBody := row.request(neverExisted)

			got := probe(t, s, "stranger", row.method, ownedPath, ownedBody)
			want := probe(t, s, "stranger", row.method, missingPath, missingBody)

			if got != want {
				t.Errorf("existence leak: a resource owned by another tenant is "+
					"distinguishable from one that never existed\n"+
					"  owned by \"owner\": %s\n"+
					"  never existed:    %s", got, want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it against the current tree**

```bash
go test ./pkg/api/ -run TestCrossPrincipalEquivalence_IntactStore -v -count=1 -timeout 300s
```

Expected: PASS on the current tree. The tenant checks work today.

**A test that has never failed is not evidence.** Step 3 supplies the red.

- [ ] **Step 3: Prove the sweep has teeth against a real historical defect**

Commit `9a5feb0` is post-Task-7 and pre-Task-7b of the absent-versus-refused
track: single-record reads returned `ErrRecordUnreadable`, and the tenant guard
that stops it leaking did not exist yet. That is a genuine defect that shipped in
this repository's history for one commit.

The sweep's intact-store phase does not reach it, because it needs a damaged
record. So this step proves the teeth a different way, by mutation:

```bash
export SCRATCH=/tmp/claude-1000/-mnt-ssd2-Workspace-github-com-graphdb/4474d5b1-0313-4e31-8e85-7af525dbfaeb/scratchpad
```

Temporarily edit `pkg/api/handlers_nodes.go`'s `deleteNode` so a cross-tenant
delete answers 403 instead of 404:

```go
		if errors.Is(err, storage.ErrNodeNotFound) {
			s.respondError(w, http.StatusForbidden, "Node not found") // MUTATION
			return
		}
```

Then:

```bash
go test ./pkg/api/ -run TestCrossPrincipalEquivalence_IntactStore -v -count=1 -timeout 300s 2>&1 | tee "$SCRATCH/equiv-mutation-red.txt"
git checkout -- pkg/api/handlers_nodes.go
```

Expected: the `DELETE node` row FAILS with the two observables printed, and the
other seven rows pass. Record the message. A mutation that fails every row would
mean the comparison is degenerate.

- [ ] **Step 4: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/api/equivalence_sweep_test.go
git commit -m "test(api): a cross-principal equivalence sweep over the intact store

Eight rows, one per endpoint that names a caller-supplied resource ID.
Each asserts that a stranger's view of another tenant's resource is
byte-identical to a stranger's view of an ID that never existed.

Proven to have teeth by mutation: making a cross-tenant DELETE answer 403
fails that row alone.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 3: The sweep under fault

This is the phase that catches the class. The 2026-08-29 leak was invisible to a
cross-tenant test on an intact store and invisible to a fault test with one
principal. It needed both.

**Files:**
- Modify: `pkg/api/equivalence_sweep_test.go` (append)

**Interfaces:**
- Consumes: `observable`, `probe`, `sweepRow`, `equivalenceRows` from Tasks 1 and 2.
- Produces: `func damageNodeRecord(t *testing.T, dir string, id uint64)`

- [ ] **Step 1: Write the failing test**

The store must be in mmap mode and closed before its snapshot can be damaged, so
this phase builds its own server rather than reusing `setupTestServer`. Read
`pkg/storage/damaged_record_test.go` first — it carries the corruption helper
this mirrors, and the byte it targets.

Append:

```go
// TestCrossPrincipalEquivalence_UnderFault repeats every row with the target
// record damaged on disk so it will not decode.
//
// This is the composition that matters. On 2026-08-29 a damaged record returned
// a different error class from a missing one, so a stranger saw 500 where a
// stranger asking for a nonexistent ID saw 404. A cross-tenant test on an intact
// store cannot see it, because the error paths do not diverge. A fault test with
// one principal cannot see it, because it never compares.
func TestCrossPrincipalEquivalence_UnderFault(t *testing.T) {
	// Build a store in mmap mode, close it so the snapshot is written, damage
	// one node record, reopen, then serve.
	//
	// IMPLEMENTER: mirror the fixture in pkg/storage/damaged_record_test.go.
	// It creates the store with mmapConfig(dir), closes it, finds the record's
	// true offset through openMmapSnapshot + nodeOffset, sets byte 8 (the
	// tenant-length uint16 prefix) to 0xFFFF, and asserts with a POSITIVE
	// CONTROL that the record then fails to decode. Those helpers are
	// unexported in pkg/storage, so this test needs its own copy of the
	// corruption step operating on the file directly.
	//
	// Carry the positive control across. Without it a test that stopped
	// corrupting anything would pass every row.
	t.Skip("IMPLEMENTER: see the note above — this skip must be removed")
}
```

**The `t.Skip` above is a placeholder you must remove.** It is written explicitly
so that a half-finished task cannot be mistaken for a passing one. If you commit
this task with the skip in place, the task is not done.

- [ ] **Step 2: Build the fixture and remove the skip**

Read `pkg/storage/damaged_record_test.go` and mirror its corruption step. The
record offset must come from the snapshot's own directory, not from a guessed
constant, so that the corruption lands on a record body and leaves the CRC valid.

For each row, the comparison is the same as Task 2's, with one addition: assert
that the OWNING tenant still sees a difference between the damaged record and a
nonexistent ID. Without that assertion, a handler that answered 404 for
everything would pass the sweep while destroying the diagnostic the
absent-versus-refused track exists to provide.

- [ ] **Step 3: Run it**

```bash
go test ./pkg/api/ -run TestCrossPrincipalEquivalence_UnderFault -v -count=1 -timeout 300s
```

Expected: PASS on the current tree, because PR B's guard closed this.

- [ ] **Step 4: Prove the teeth against the real historical defect**

This phase DOES reach `9a5feb0`. Build a worktree at that commit, lift only this
test file in, and run it:

```bash
git worktree add "$SCRATCH/equiv-red" 9a5feb0 --detach
git show HEAD:pkg/api/equivalence_sweep_test.go > "$SCRATCH/equiv-red/pkg/api/equivalence_sweep_test.go"
(cd "$SCRATCH/equiv-red" && go test ./pkg/api/ -run TestCrossPrincipalEquivalence_UnderFault -v -count=1 -timeout 300s) > "$SCRATCH/equiv-red-9a5feb0.txt" 2>&1
git worktree remove --force "$SCRATCH/equiv-red"
head -30 "$SCRATCH/equiv-red-9a5feb0.txt"
```

Expected: FAIL, with the stranger seeing 500 for the damaged record and 404 for
the nonexistent ID.

**If it passes, stop and report.** A sweep that cannot detect a defect this
repository actually shipped is not a gate, and the reason must be understood
before the task is called done.

- [ ] **Step 5: Commit**

```bash
gofmt -s -l ./pkg ./cmd
git add pkg/api/equivalence_sweep_test.go
git commit -m "test(api): repeat the equivalence sweep with the record damaged

The composition is the point. A cross-tenant test on an intact store cannot
see the class, because the error paths do not diverge. A fault test with one
principal cannot see it, because it never compares.

Proven against 9a5feb0, the one commit in this repository's history where
the leak was real.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018YsWwE1Ya4AfC149RXw5Kb"
```

---

## Task 4: Register the contract and close out

**Files:**
- Modify: `docs/CONSUMER_CONTRACTS.md` (append one row)
- Modify: `docs/internals/design/SQLITE_TESTING_SCORECARD.md` (the boundary-values row)

**Interfaces:**
- Consumes: the tests from Tasks 1 to 3.
- Produces: a registry row that `make contract-guard` enforces.

- [ ] **Step 1: Add the contract row**

Match the existing column order exactly: `| id | Invariant | Consumer(s) | Guarding test(s) | Origin |`. Read the last row to pick the next id.

```markdown
| CC<N>-existence-indistinguishable | A caller who does not own a resource sees an observable identical to one for an ID that never existed, intact or damaged | all REST consumers | `pkg/api` `TestCrossPrincipalEquivalence_IntactStore`, `TestCrossPrincipalEquivalence_UnderFault` | #<PR> |
```

Then:

```bash
make contract-guard
scripts/contract-guard-selftest.sh
```

The guard checks the registry against the tests that enforce it, so a wrong test
name fails here rather than in review.

- [ ] **Step 2: Run every gate**

```bash
go build ./pkg/... ./cmd/... && go vet ./pkg/...
gofmt -s -l ./pkg ./cmd
make lint-local && make lint-local-selftest
go test ./pkg/api/ -count=1 -timeout 300s
make contract-guard && scripts/contract-guard-selftest.sh
```

Record each verdict with the artefact it read.

- [ ] **Step 3: Commit and open the pull request**

The body must carry, verbatim in fenced blocks: the mutation red from Task 2
step 3, and the historical red from Task 3 step 4 against `9a5feb0`. Put both
above the summary. A reviewer who sees the failures the tests produce does not
have to take "this is a gate" on trust.

State the four limits from the spec plainly rather than burying them: timing is
not covered, the table is not a proof, conformance is not correctness, and rows
1 and 4 are weak because `GET /nodes/{id}` answers 404 for every error.

---

## Self-Review

**Spec coverage.**

| Spec section | Task |
|---|---|
| The invariant, stated | Task 2, the table and its assertion |
| The observable, and where it is measured | Task 1, `observable` and `probe` |
| `sanitizeError` makes the body constant | Spec only; the body still enters the comparison, per Task 1 |
| The operation table, 7 rows | Task 2 — **8 rows**, because `POST /edges` appears twice |
| Phase 1, the clean sweep | Task 2 |
| Phase 2, the sweep under fault | Task 3 |
| Not an authorisation test | Stated in the file's opening comment, Task 1 |
| Limit: timing | Task 1, on the `observable` type |
| Limits: table not proof, conformance not correctness, weak rows | Task 4 step 3, in the pull request body |
| Relationship to `dd0wney/fault` | Spec only; nothing here depends on it |

**A divergence found and closed during this review:** the spec's table listed
seven rows while its prose said the last one appears twice. The plan sweeps
eight. The spec's table was the wrong half and has been corrected in place, so
the two now agree at eight.

**Placeholder scan.** One `t.Skip` in Task 3 step 1 is deliberate and is marked
as a placeholder the implementer MUST remove, with the failure mode named. Task 3
step 2 directs the implementer to read an existing fixture rather than carrying a
copy of it, because the corruption helpers are unexported in `pkg/storage` and a
verbatim copy here would go stale. Everything else is complete code.

**Type consistency.** `observable` is a struct with `status int` and `body
string` in Task 1 and is compared with `==` in Tasks 2 and 3, which is valid
because both fields are comparable. `sweepRow.request(id uint64) (path, body
string)` is defined in Task 2 and called with that arity in Tasks 2 and 3.
`probe`'s six parameters match at every call site.

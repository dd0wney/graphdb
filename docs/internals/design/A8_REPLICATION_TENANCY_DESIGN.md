# A8 Design Spike — Replication-Tenancy Isolation

**Status:** spike output, not implementation. Date: 2026-05-09.
**Predecessors:** Audit Track A (PRs #17–#26) plus A9 (PRs #36–#39) closed cross-tenant data and metadata leakage on the *single-node* HTTP path. A8 is the parallel-track item flagged at audit (`audit_regression_test.go:46` "open follow-ups") because the replication path was not yet tenant-aware. Today the standalone primary/replica binaries and `pkg/replication` types pre-date the tenancy work entirely.
**Goal:** map the implementation surface for closing the replication-tenancy gap so the next PR can ship without scope-blowup.

## 1. What's broken today

### 1.1 The wire format drops tenant

`pkg/replication/transport.go:100` (and the parallel definition in `pkg/replication/zmq_primary_types.go:61`):

```go
type WriteOperation struct {
    Type       string                 `json:"type"`
    Labels     []string               `json:"labels,omitempty"`
    Properties map[string]interface{} `json:"properties,omitempty"`
    FromNodeID uint64                 `json:"from_node_id,omitempty"`
    ToNodeID   uint64                 `json:"to_node_id,omitempty"`
    EdgeType   string                 `json:"edge_type,omitempty"`
    Weight     float64                `json:"weight,omitempty"`
}
```

**No `TenantID` field.** `WriteOperation` is the JSON payload sent by a replica's `WriteForwarder` (see `pkg/replication/write_forwarder.go:74`) over a PUSH socket to the primary's `writeReceiver`. When the primary parses the JSON and applies the operation, it has no way to know which tenant the originating client was operating as.

### 1.2 The replica→primary apply path is tenant-blind

`pkg/replication/nng_primary_handlers.go:217` (and the parallel ZMQ handler in `pkg/replication/zmq_primary_handlers.go:152`):

```go
func (nm *NNGReplicationManager) executeWriteOperation(op *WriteOperation) {
    props := convertProperties(op.Properties)
    switch op.Type {
    case "create_node":
        if _, err := nm.storage.CreateNode(op.Labels, props); err != nil { ... }
    case "create_edge":
        if _, err := nm.storage.CreateEdge(op.FromNodeID, op.ToNodeID, op.EdgeType, props, op.Weight); err != nil { ... }
    }
}
```

`nm.storage` satisfies the `Storage` interface (`pkg/replication/transport.go:88-97`):

```go
type StorageWriter interface {
    CreateNode(labels []string, properties map[string]interface{}) (interface{}, error)
    CreateEdge(from, to uint64, edgeType string, properties map[string]interface{}, weight float64) (interface{}, error)
}
```

These are **tenant-blind** signatures from the pre-A3a era. Audit A3a (PR #17) added `*ForTenant` variants on the storage layer but `pkg/replication`'s interface still talks to the legacy methods. End result: any forwarded write lands in the default tenant on the primary, regardless of the originating tenant.

### 1.3 The standalone binaries are auth- and tenant-blind

`cmd/graphdb-primary/main.go:84-103`:

```go
http.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodPost {
        ...
        node, err := graph.CreateNode(req.Labels, req.Properties)  // tenant-blind
        ...
    }
})
```

No auth middleware. No `withTenant`. Direct `CreateNode` call.

`cmd/graphdb-replica/main.go:91-104`:

```go
http.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" { ... return }
    nodes := graph.GetAllNodesAcrossTenants()  // intentional, but no auth
    json.NewEncoder(w).Encode(nodes)
})
```

The cross-tenant read on the replica is intentional (the existing comment says so — replicas legitimately need full-state visibility for replication). But it's served on an HTTP endpoint with **no authentication** — any caller hitting `/nodes` on a replica gets a full data dump across every tenant.

### 1.4 What's NOT broken: the WAL replication path

`pkg/wal/wal_types.go:18` defines `Entry { LSN, OpType, Data, Checksum, Timestamp }`. The `Data` field is opaque bytes — for `OpCreateNode` it's a JSON-marshaled `storage.Node`, which (per `pkg/storage/types.go:393-400`) **does include `TenantID`**:

```go
type Node struct {
    ID         uint64
    TenantID   string
    Labels     []string
    Properties map[string]Value
    CreatedAt  int64
    UpdatedAt  int64
}
```

The replay path (`pkg/storage/persistence_replay.go:46`) deserializes the full `Node` struct, preserving `TenantID`. So **WAL-based primary→replica catch-up is tenant-safe end-to-end** as long as the originating write was correctly tenant-stamped on the primary.

### 1.5 Risk model — the leak is latent, not active

The Dockerfile entrypoint (`./cmd/server`) does **not** import `pkg/replication`. There are no production callers of `WriteOperation`, `WriteForwarder.Forward`, or `executeWriteOperation`. Same for the standalone primary/replica binaries — they're not in the production build/deploy path.

So today's exposure is:

- **Latent code that future deployment work could activate.** If anyone wires `pkg/replication` into `cmd/server`, all writes land in default tenant. The bug is silent — no error, no signal — corrupted data.
- **The standalone binaries (`cmd/graphdb-{primary,replica}`, `cmd/graphdb-{nng,zmq}-{primary,replica}`) ship in the repo and are buildable.** A user who reads the README and runs them gets a tenant-blind, auth-blind deployment.

This is "fix before we ship replication" rather than "stop the bleeding now," but the patch is small and prevents a Class-A regression hazard once HA work resumes.

## 2. Design decisions

### Q1: Add `TenantID` to `WriteOperation` (wire format)

**Decision: add `TenantID string` to both `WriteOperation` definitions, JSON-tagged `tenant_id` (snake_case to match this package's convention). Do NOT use `omitempty` — see Q3 for why.**

```go
type WriteOperation struct {
    TenantID   string                 `json:"tenant_id"`
    Type       string                 `json:"type"`
    ...
}
```

Alternatives considered and rejected:

- **Don't add — pass tenant via socket-level metadata.** ZMQ/NNG support multi-frame messages, so we could ship tenant in a separate frame. Rejected: it's strictly more complex and bypasses Go's type system. JSON struct field is universally readable and future-proof.
- **Encode tenant inside the existing `Properties` map.** Rejected: properties are user-controlled. Mixing security-critical metadata with user data is asking for a confused-deputy attack down the line.
- **Use `omitempty` for "rolling-upgrade compat".** Rejected: §1.5 established there are no production senders, so there's no rolling-upgrade case to preserve. Worse, `omitempty` plus fail-closed (Q3) makes "missing field on wire" and "explicit empty-string field" indistinguishable on the receive side, which masks the diagnostic signal a fail-closed handler is supposed to produce.

### Q2: Tenant-aware `StorageWriter` interface

**Decision: change `StorageWriter` to use the `*ForTenant` storage methods, signature taking `tenantID string`.**

```go
type StorageWriter interface {
    CreateNodeForTenant(tenantID string, labels []string, properties map[string]interface{}) (interface{}, error)
    CreateEdgeForTenant(tenantID string, from, to uint64, edgeType string, properties map[string]interface{}, weight float64) (interface{}, error)
}
```

Alternatives considered and rejected:

- **Keep the tenant-blind interface, route through a tenant-stamping wrapper.** Rejected: a wrapper-layer fix means every call site has to get the wrapper right. Pushing the tenant requirement *into the interface* makes "forgot to pass tenant" a compile error rather than a runtime missing-stamp.
- **Add `*ForTenant` variants alongside the existing methods.** Same additive pattern as A3a. Rejected here because `pkg/replication` is the *only* caller of `StorageWriter` — there's no third-party implementer to break, and the tenant-blind methods on `pkg/replication`'s interface have no reason to exist post-fix. Cleaner to migrate cleanly than carry two parallel surfaces.

Trade-offs:

- **`*storage.GraphStorage`** (the concrete type satisfying the interface in `pkg/replication`'s production wiring) already exposes both `CreateNodeForTenant` and `CreateEdgeForTenant` from A3a. No new storage code.
- **Tests in `pkg/replication`** (`transport_test.go:25` and similar) construct a mock `StorageWriter`. They'll need to be updated to the new signature — small, mechanical change.

### Q3: Migrate the apply path — fail closed on empty `TenantID`

**Decision: `executeWriteOperation` rejects (logs + drops) any `WriteOperation` with empty `TenantID`. No silent default. Calls `*ForTenant` methods with the explicit tenant.**

```go
func (nm *NNGReplicationManager) executeWriteOperation(op *WriteOperation) {
    if op.TenantID == "" {
        // Audit A8: fail closed. An empty tenant on the wire is
        // either a buggy/unmigrated sender or a malicious payload
        // attempting to land in the default tenant. Refuse rather
        // than silently default — same fail-closed shape as the
        // JWT_SECRET fix in server_init.go.
        log.Printf("replication: refusing %q with empty tenant_id; check sender migration", op.Type)
        return
    }
    props := convertProperties(op.Properties)
    switch op.Type {
    case "create_node":
        if _, err := nm.storage.CreateNodeForTenant(op.TenantID, op.Labels, props); err != nil { ... }
    case "create_edge":
        if _, err := nm.storage.CreateEdgeForTenant(op.TenantID, op.FromNodeID, op.ToNodeID, op.EdgeType, props, op.Weight); err != nil { ... }
    }
}
```

Same pattern repeated in `zmq_primary_handlers.go:executeWriteOperation` and `write_receiver.go:executeWrite`.

Rationale (this is the load-bearing security decision; audit-precedent matters):

- **The rest of the codebase chose fail-closed for this same shape of decision.** `pkg/api/server_init.go:74-77` (the JWT_SECRET fix from this audit cycle): "The previous behaviour (silently generating a random secret unless `GRAPHDB_ENV=='production'`) was a security finding from the 2026-05-06 audit." A8 is the *same* class of bug — silent default vs explicit refusal — and should match.
- **`matchesTenant`'s "empty == default" convention is for legacy on-disk data**, not runtime requests. Read the comment at `pkg/api/middleware_tenant.go:97-103` carefully: "An empty `nodeTenantID` is treated as the default tenant (pre-multi-tenant data that was written before tenants existed still belongs conceptually to 'default')." That's a backfill rule for data already on disk before the tenancy concept existed. Applying it to a runtime wire format would re-introduce the silent-default class A8 exists to close.
- **§1.5 establishes no production senders exist today**, so the "compat with old senders" argument has no force. An old-sender path means future rolling upgrades; we'll cross that bridge when there are senders to upgrade. For now, an empty `TenantID` on the wire can only come from a buggy migration or a malicious actor — both of which deserve a refusal.

**Future-compat escape hatch (deliberate, gated):** if a future deployment scenario genuinely needs to accept empty-tenant writes (e.g., a one-shot legacy-replica drain), gate behind an explicit env flag — same shape as the audit's JWT pattern:

```go
if op.TenantID == "" {
    if os.Getenv("REPLICATION_ALLOW_EMPTY_TENANT") != "1" {
        log.Printf("replication: refusing %q with empty tenant_id ...", op.Type)
        return
    }
    op.TenantID = tenant.DefaultTenantID  // explicit opt-in to the old behavior
}
```

Off by default. Documented as a sharp-edged migration tool, not a steady-state config.

### Q4: Migrate the forwarder path

**Decision: `WriteForwarder.Forward` is unchanged in signature; callers populate `WriteOperation.TenantID` before passing in. The forwarder is already a dumb wire-encoder, and that's correct.**

The interesting question is: where is the tenant set?

There are no production callers of `WriteForwarder.Forward` today (§1.5). So the migration is forward-looking: when HA work resumes and replicas accept writes, their HTTP handlers must extract `getTenantFromContext(r)` and populate `op.TenantID`.

Document this in a `pkg/replication/doc.go` (or top-of-file comment in `transport.go`) so the next person to wire forward-write is forced to confront the tenant requirement.

### Q5: Standalone binaries — fail-closed startup gate, not just a comment

**Decision: gate startup of the standalone primary/replica binaries (`cmd/graphdb-{primary,replica}`, plus the `nng`/`zmq` variants) behind an explicit env flag (`GRAPHDB_LEGACY_BINARY=1`). Default: `log.Fatalf` with a pointer to `cmd/server` and the audit context. Don't fix the underlying tenancy holes in this audit scope.**

Rationale:

- They're not in the deploy path (Dockerfile builds `cmd/server`).
- Fixing them properly means wiring auth + `withTenant` middleware + per-tenant routing — that's the same scope as building a real production replica daemon, which is HA work, not audit work.
- A drive-by partial fix (e.g., just calling `*ForTenant` with default tenant) would create the *appearance* of tenancy without the substance, and is worse than a clearly-marked legacy binary.
- A leading comment is too easy to ignore — the binaries ship and build today. A `log.Fatalf` requiring an explicit opt-in flag costs ~3 lines and *prevents* the misuse the comment only warns about. Same pattern as the JWT_SECRET fix and the Q3 escape hatch.

Sketch:

```go
func main() {
    if os.Getenv("GRAPHDB_LEGACY_BINARY") != "1" {
        log.Fatalf("graphdb-primary: this binary pre-dates the multi-tenant work " +
            "(audit A8) and is not safe for production. Use cmd/server. To run " +
            "anyway for development/testing, set GRAPHDB_LEGACY_BINARY=1.")
    }
    ...
}
```

Companion task: track "deprecate or rebuild standalone replication binaries on top of `cmd/server` infrastructure" as a separate audit follow-up.

### Q6: Tests

**Decision: three test additions, scoped tightly.**

1. **`pkg/replication/transport_test.go`** — round-trip a `WriteOperation{TenantID: "tenant-A", ...}` through JSON marshal/unmarshal; assert `TenantID` survives. Catches the "forgot to add the tag" or "renamed the field" regression.

2. **`pkg/replication/apply_test.go` (new)** — table-driven test that constructs a fake `StorageWriter` recording calls, drives `executeWriteOperation` (NNG and ZMQ variants) with a `WriteOperation` carrying `TenantID: "tenant-A"`, asserts the recorded `CreateNodeForTenant` call received `tenant-A`. The test is the gate that says "the tenant flowed all the way through."

3. **`pkg/api/audit_regression_test.go`** — add a row `A8/replication-write-preserves-tenant`. Construct a `WriteOperation` for tenant-A, run it through the executor against a real `*storage.GraphStorage`, query back via `GetNodesByLabelForTenant("tenant-A", ...)` and `GetNodesByLabelForTenant("default", ...)`, assert tenant-A sees the node and default does not.

Coverage gap intentionally left out of this PR (calls out in the PR body as future work):

- **End-to-end socket round-trip test.** Would need a fake replica + primary running on a loopback socket. Higher-value once HA work begins; over-investment for the audit scope. The unit-test layering above plus the WAL-replay safety (§1.4) catches the same regression class with less infrastructure.

### Q7: Implementation order — four commits, stacked

To keep PRs reviewable and to give us a clean revert point if a layer turns out wrong:

1. **`feat(replication): add TenantID to WriteOperation wire format`** — schema change only. Add the field (no `omitempty`, per Q1). Round-trip JSON test in `transport_test.go`. Old senders that don't yet populate the field will be rejected by commit 2; that's acceptable because §1.5 establishes there are no production senders.

2. **`refactor(replication): fail-closed apply path + StorageWriter *ForTenant signatures`** — interface change + behavior change. Update `executeWriteOperation` (NNG + ZMQ + `write_receiver`) to refuse empty `TenantID` and call `*ForTenant` methods. Update mock `StorageWriter` in `transport_test.go`. New `apply_test.go` table-driven test pinning fail-closed and tenant-flow-through. Document the `REPLICATION_ALLOW_EMPTY_TENANT` escape hatch in `pkg/replication/doc.go` (or top-of-file in `transport.go`).

3. **`test(api): A8 audit regression row`** — adds the `A8/replication-write-preserves-tenant` row to `audit_regression_test.go`. Reference map updated, A8 moves from "Open follow-ups" to active. This commit depends on commits 1+2 having landed; it's the umbrella gate.

4. **`feat(cmd): gate standalone replication binaries behind GRAPHDB_LEGACY_BINARY`** — independent of commits 1–3. Adds the `log.Fatalf` startup gate to `cmd/graphdb-{primary,replica}` and the `nng`/`zmq` variants. README note pointing users at `cmd/server`. Companion audit-follow-up task created (deprecate/rebuild on `cmd/server`).

Each commit ships as its own PR, stacked. Commit 4 is independent and can ship first or in parallel — its only dependency is the audit context (the doc this references). Same pattern as A9.

## 3. Out of scope (deliberate, called out for the next iteration)

- **End-to-end socket replication test.** §6.
- **Standalone-binary rewrite.** §5.
- **Replica's `/nodes` GET unauth'd cross-tenant dump.** Not the A8 finding (which is the write path); it's a separate auth-on-replica-binary issue. Track as its own item in the audit follow-ups.
- **Tenant-aware ACL on replication forwarding.** Today any client hitting a replica with a write request is implicitly trusted to claim its own tenant. Once auth is on the replica, the forwarder will need to validate the claimed tenant against the auth context — same shape as `withTenant`'s job on `cmd/server`. Future HA work.

## 4. Acceptance — when is A8 done

- `WriteOperation` carries `TenantID` (no `omitempty`); round-trip test pins the wire format.
- `StorageWriter` interface uses `*ForTenant` methods.
- All `executeWriteOperation` / `executeWrite` paths (NNG, ZMQ, generic) **refuse empty `TenantID`** and call `*ForTenant` for non-empty.
- `apply_test.go` pins both halves: a populated `TenantID` flows through to the right storage call; an empty `TenantID` is dropped — the assertion is **mock storage recorded zero calls**, not "didn't error." A future bug where rejection-then-call slips in must fail this test.
- `audit_regression_test.go` has a row pinning that a write through the replication apply path lands only in the originating tenant.
- Reference map in `audit_regression_test.go` updated (A8 moves from "Open follow-ups" up).
- Standalone primary/replica binaries refuse to start without `GRAPHDB_LEGACY_BINARY=1`; message points at `cmd/server`.
- All tests green; gofmt/vet/lint clean.

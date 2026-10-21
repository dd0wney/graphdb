# Spec: node upsert (storage, REST, Go client) — 2026-10-21

**Status**: draft for approval. No code lands before the user approves this document.
**Coord tasks**: `graphdb:v1.4-storage-upsert-node` → `graphdb:v1.4-rest-upsert-node` →
`graphdb:v1.4-go-client-upsert-node`.
**Asked by**: `mapper-6b` (`oit-cyber/mapper`). mapper embeds `pkg/storage` and also has a team mode
over REST, where two assessors ingest the same host at the same time.
**Read at**: `d08c106`. A builder check (opus reviewer) read this document against the code on 2026-10-21; its corrections are in. Line numbers below are claims about that revision; re-read before you
navigate by them.

The user chose to skip the brainstorm questions ("proceed with recommendations", 2026-10-21). Every
decision below marked **(recommended)** is the author's call, not the user's; §9 lists them so the
review can check each one.

---

## 1. Intent

A consumer that ingests facts about an entity from more than one source today does
create → 409 → lookup → update. That is three round trips, and the gap between the lookup and the
update is a race: a second writer can change the node in between, and nothing tells either writer.
mapper's team mode hits this when two assessors report the same host.

An upsert makes "this node, keyed by this property, should exist and carry at least these
properties" one atomic operation, and tells the caller whether it created the node.

## 2. Goals, non-goals, anti-goals

**Goals**

- G1. One storage call that finds by key and creates or merges under one `gs.mu` hold, so
  concurrent upserts of one key make exactly one node.
- G2. The REST surface for single and batch writes, keyed by the registered uniqueness rule.
- G3. The Go client methods, returning the node and a `created` flag.
- G4. No on-disk format change and no new WAL operation type.

**Non-goals**

- Edge upsert over REST (`v1.4-rest-edge-upsert`, separate).
- Label mutation on a match (`v1.4-node-label-mutation`, separate). See D3.
- An atomic batch. The batch keeps its partial-success shape (CC7, CC20);
  `v1.4-storage-batch-atomic-commit` is separate.
- GraphQL and Cypher. Cypher `MERGE` is Cypher's upsert and has its own registry gap
  (`v1.4-cypher-create-uniqueness-rules`).
- Faster key lookup. The upsert reuses the label-bucket scan; `v1.4-unique-create-property-index`
  improves both create and upsert later.

**Anti-goals** — the upsert has failed, even if it "works", if it:

- AG1. Picks one node silently when more than one node holds the key.
- AG2. Drops a part of the request silently (a label the stored node lacks, for example).
- AG3. Lets an upsert and a plain create of the same key make two nodes.
- AG4. Names a uniqueness rule, or another tenant's data, in a response (ADR 0001, R4).

## 3. Storage API

```go
// UpsertNodeForTenant finds the node of tenantID that has keyLabel and whose
// keyProperty equals properties[keyProperty]. If none exists it creates a node
// with labels and properties (created == true). If one exists it merges
// properties into it (created == false).
func (gs *GraphStorage) UpsertNodeForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
	keyLabel, keyProperty string,
) (node *Node, created bool, err error)

// UpsertNodeWithUniquenessRulesForTenant selects the key from the uniqueness-rules
// registry (ADR 0001), as CreateNodeWithUniquenessRulesForTenant does, and calls
// UpsertNodeForTenant.
func (gs *GraphStorage) UpsertNodeWithUniquenessRulesForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
) (node *Node, created bool, err error)
```

Both are added to the storage interface beside `CreateNodeWithUniquePropertyForTenant`
(`interface.go:110`) and `UpsertEdgeWithTenant` (`interface.go:119`).

### Argument checks (before the lock), same as the unique create

- `keyLabel` and `keyProperty` are not empty, `keyLabel` is in `labels`, and `properties` has
  `keyProperty`. Each failure is a plain error, as `CreateNodeWithUniquePropertyForTenant`
  returns today.

### Under `gs.mu` (one hold)

1. `checkClosed`.
2. The same scan as `createNodeUniqueUnderLock`: every node of the tenant in the `keyLabel` bucket,
   compared with `valuesEqual`. An unreadable record aborts with `ErrRecordUnreadable` (wrapped),
   never "no match".
3. Count the matches. **Do not stop at the first one** — the unique create stops at the first,
   which is right for a create and wrong here (AG1).
   - 0 matches → `createNodeLocked`. `created = true`.
   - 1 match → D3 check, then the D6 check, then the locked half of the patch path with
     `set = properties` and `remove = nil`. `created = false`.
   - 2 or more → `*UpsertAmbiguousError{Label, PropertyKey, NodeIDs}`, which unwraps to
     `ErrUpsertAmbiguous`. Nothing is written. Two nodes can hold one key because a rule was
     registered after the duplicates were written, or because a surface bypassed the registry
     (`POST /nodes/batch` before #674, Cypher today).

The lock section is a method with `defer gs.mu.Unlock()` and `panicPoint("UpsertNodeForTenant")`,
as #665 requires.

### Off the lock

Exactly what the chosen path already does after its lock section:

- create: vector inserts, the WAL wait, the created-node observer notification;
- match: vector removals and inserts, the WAL wait (`wal.OpUpdateNode`), the updated-node
  observer notification.

A WAL wait error wraps `ErrWALWriteFailed` and is returned **with** the node and the correct
`created` value. The create path already returns its node with that error. The match path does not:
`PatchNodeForTenant` and `patchNode` return only `error`, and `applyNodePatch` clones the node only
when observers are registered (`newNode`). The upsert's lock section therefore always takes its own
`node.Clone()` after the merge, for the return value.

### Implementation note

`applyNodePatch` takes the lock itself. Split it into a lock-taking wrapper and an
`applyNodePatchLocked(node *Node, set, remove)` body that the upsert's lock section calls, the
same split `createNodeLocked` already has. `patchNode`'s off-lock half becomes a function both
callers share. No behaviour of `PatchNodeForTenant` changes.

### Rules wrapper

`UpsertNodeWithUniquenessRulesForTenant` reuses `planUniquenessEnforcement`
(`uniqueness_rules.go:358`):

| Registry state for the labels | Result |
|---|---|
| A required pair is not satisfied | `*RequiredRuleMissingError` (as the create) |
| 0 rules match | `ErrNoUniquenessRule` (**new**). An upsert needs a key; there is no fallback to a plain create. |
| 1 rule matches, property absent | `ErrUniquenessRulePropertyMissing` (as the create) |
| 1 rule matches | `UpsertNodeForTenant(…, rule.Label, rule.PropertyKey)` |
| 2 or more rules match | `ErrMultipleUniquenessRules` (as the create) |

## 4. Decisions

### D1. The key — explicit in storage, registry-only over REST (recommended)

| Option | Pro | Con |
|---|---|---|
| A. Explicit key only | Flexible for embedded callers | Over REST, a caller could upsert on a key nothing enforces, and a plain create of the same key makes a duplicate (AG3) |
| B. Registry only | The key is always an enforced uniqueness key | An embedded caller must register a rule first |
| **C. Both: explicit in storage, registry over REST** | Embedded callers (mapper) choose; REST cannot violate AG3 | Two entry points to keep consistent |

C, with one refusal. `UpsertNodeForTenant` reads the registry before it takes `gs.mu`, as the
create wrapper does:

- No registered rule covers the request's labels: the explicit key is used. The caller gets G1
  against other upserts of that key, but **not** against plain creates of it; the doc comment says
  so.
- Every registered rule that covers the labels has the explicit key as its `(Label, PropertyKey)`:
  the explicit key is used, and AG3 holds.
- Any registered rule covers the labels with a different key: refused with
  `*UpsertKeyNotRegisteredError{KeyLabel, KeyProperty}`, unwrapping to `ErrUpsertKeyNotRegistered`,
  and nothing is written. Without this, mapper upserting `Host.hostname` while a rule enforces
  `Host.ip` could create a second node holding an `ip` value another node already holds (AG3).
  The error names the caller's own key only, never the rule (AG4).
  "Every", not "one of": the create branch checks only the key, so a second covering rule
  would go unchecked there. With two distinct covering rules the upsert is therefore always
  refused, the same answer the create gives (`ErrMultipleUniquenessRules`). (A first build
  accepted a key that matched any one covering rule; a red test caught it.)

REST uses the registry only.

### D2. Property merge on a match (recommended)

| Option | Pro | Con |
|---|---|---|
| **Merge: request properties override, absent ones stay** | Two sources that each know part of an entity both survive; same as `UpsertEdgeWithTenant` and `PATCH` | A caller cannot remove a property through an upsert |
| Replace the whole map | One call states the full node | The second assessor erases the first one's facts — the race the upsert exists to close |

Merge. Removal stays with `PUT /nodes/{id}` and `Content-Type: application/merge-patch+json`
(there is no HTTP `PATCH` route; `handleNode` routes GET, PUT and DELETE). A JSON `null` in an
upsert body stores null, as `POST /nodes` does (CC24); it does not remove.

### D3. Labels on a match — refuse a difference (recommended)

Storage has no label mutation (`v1.4-node-label-mutation`). On a match, the request's labels are
compared with the stored node's labels.

| Option | Pro | Con |
|---|---|---|
| Ignore the request's labels | Simple; most callers send the same labels | Silent drop of part of the request (AG2) |
| Union the labels | What a caller most likely wants | Needs label mutation: label index, membership, WAL record — a separate task |
| **Refuse unless the request's labels are a subset of the stored labels** | Loud; nothing is dropped; can relax to union later without breaking anyone | A caller that adds a label must wait for label mutation |

Refuse: `*UpsertLabelsDifferError{NodeID, Missing []string}`, unwrapping to
`ErrUpsertLabelsDiffer`, and nothing is written. A subset (fewer labels than stored) is accepted,
because no label is dropped. Relaxing a refusal later is additive; tightening a silent drop later
would break callers.

### D4. A match that changes nothing writes nothing (recommended)

If every request property is already stored with an equal value, the upsert returns the node with
`created = false` and writes no WAL entry and sends no observer notification. Repeated idempotent
ingestion (mapper re-scanning a host) then does not grow the WAL. Cost: one `valuesEqual` per
request property, under the lock that is already held.

### D6. A merge must not break a rule on the stored node's other labels (recommended)

D3 accepts request labels that are a subset of the stored labels, so the stored node can carry a
label the request did not name, with its own rule. Example: rules `Host.ip` and `Asset.serial`; the
request names `Host` only; the stored node is `Host` and `Asset`; the request sets `serial` to a
value another `Asset` node holds.

Before the merge, under the same `gs.mu` hold, the upsert evaluates every registered rule whose
`Label` is on the **stored** node and whose `PropertyKey` is in the request's properties with a
value different from the stored one. For each, it runs the same scan as the unique create, skipping
the matched node itself. A holder refuses the whole upsert with the existing
`*UniqueConstraintError`, and nothing is written.

The registry is read under `rulesMu` before `gs.mu` is taken (a snapshot of the rules, as the
create wrapper takes one); a rule registered between that read and the lock is not seen, which is
the same window the create has today.

`PatchNodeForTenant` (REST `PUT /nodes/{id}`) has this gap today for every update: it never
consults the registry. That is a separate defect with its own task
(`v1.4-patch-node-uniqueness-rules`, to be seeded); the upsert does not wait for it and does not fix
it.

### D5. REST flag — `on_conflict` query parameter (recommended)

| Option | Pro | Con |
|---|---|---|
| **`POST /nodes?on_conflict=merge`** | Same route and body; a client adds one parameter | `POST` answers 200 on a match |
| A body field | Self-describing body | Mixes control with data in a body other surfaces echo |
| A new route (`POST /nodes/upsert`) | Clear in logs | A second route to secure, rate-limit and document |

`on_conflict` values: absent or `error` (today's behaviour, 409 on a conflict) and `merge`. Any
other value is a 400 that names the accepted values. The same parameter on `POST /nodes/batch`.

## 5. REST

### `POST /nodes?on_conflict=merge`

Body: `NodeRequest`, unchanged. Response body: the `NodeResponse` fields plus
`"created": true|false`.

| Case | Status | Body |
|---|---|---|
| Created | 201 | node + `"created": true` |
| Matched (changed or not) | 200 | node + `"created": false` |
| WAL append failed | 202 | the existing `NodeNotDurableResponse` (`types.go:163`, as `createNode` sends through `respondNodeWALWriteFailed`), plus `"created"` |
| Required rule missing | 503 | the existing fixed body (R4) |
| No rule for the labels | 400 | `"no uniqueness rule covers these labels; on_conflict=merge needs one"` |
| Rule property absent | 400 | as the create |
| More than one rule | 500 | `create node failed`, as the create (no rule names) |
| More than one node holds the key | 409 | `ErrorResponse` + `"conflicting_node_ids": [...]` |
| Labels differ | 409 | `ErrorResponse` + `"conflicting_node_id"` + `"missing_labels": [...]` |

`conflicting_node_id` uses the field name that `v1.4-rest-conflict-node-id` adds to the create's
409, so a client reads one field for both.

### `POST /nodes/batch?on_conflict=merge`

Each item is one `UpsertNodeWithUniquenessRulesForTenant` call, in request order. Not atomic, the
same as the batch create. Two items with one key: the first creates, the second matches — the
result is deterministic because the order is the request order.

Response: the existing `BatchNodeResponse` shape, status 201 as today (CC20), with:

- each element of `nodes` carrying `"created": true|false`;
- `created` = the number of items that created a node (its meaning does not change);
- a new `matched` = the number of items that matched;
- `failed` and `errors[]` as today; an item error text follows the single-item mapping above,
  through `batchCreateItemError` (#674), so no item names a rule.
- A WAL failure on an item is reported as the batch create reports it today: an `errors[]` entry
  (with the fixed `walWriteFailedMessage` text), counted in `failed`, and absent from `nodes` —
  although the write applied in memory. This is a known limit of the batch create, kept identical
  here rather than solved for one route; a per-item not-durable status is future work.

## 6. Go client

```go
func (n *Nodes) Upsert(ctx context.Context, labels []string, props map[string]any) (*Node, bool, error)

type BatchUpsertResult struct {
	Nodes   []UpsertedNode // Node plus Created
	Created int
	Matched int
	Errors  []BatchItemError
}
func (n *Nodes) BatchUpsert(ctx context.Context, nodes []NodeInput) (*BatchUpsertResult, error)
```

- A 202 returns the node, the `created` flag and a `*NotDurableError`, as `Create` does.
- A 409 is an `*Error` with `ErrConflict`; `ConflictingNodeID` (from `v1.4-rest-conflict-node-id`)
  is filled when the body has it.
- `BatchUpsert` decodes `failed` and `errors`. (`BatchCreate` drops them today, and its
  `return out.Nodes, json.Unmarshal(…, &out)` depends on an evaluation order the Go spec does not
  fix — it works under gc. Not changed here; worth its own small fix.)

## 7. Tests (each seen red first)

Storage:

- 32 goroutines upsert one key: one node exists, exactly one call returns `created = true`.
- Upsert vs plain create of a rule-covered key, concurrently: one node (AG3).
- Two stored nodes with one key (written before the rule): `ErrUpsertAmbiguous`, nothing written.
- A matched upsert merges: an absent property stays, a sent one overrides.
- D3: a label the stored node lacks is refused; a subset is accepted.
- D4: a no-change match writes no WAL entry (count WAL entries before and after).
- D1: with a rule on `Host.ip`, an explicit upsert keyed on `Host.hostname` is refused with
  `ErrUpsertKeyNotRegistered` and writes nothing; keyed on `Host.ip` it proceeds.
- D6: rules `Host.ip` and `Asset.serial`; a request labelled `Host` that matches a `Host`+`Asset`
  node and sets a `serial` another `Asset` node holds is refused with `*UniqueConstraintError`,
  and the stored node is unchanged.
- Reopen after an upsert, in JSON and in mmap mode, gives the same node (the JSON↔mmap oracle).
- Cross-tenant: one key in two tenants gives two nodes.
- `panicPoint("UpsertNodeForTenant")` through `assertPanicReleasesLock`.

REST (contract tests decode through literal JSON keys, each with a catalogue row):

- 201 then 200 for one key; `created` true then false.
- Two concurrent requests for one key: one node, exactly one 201.
- 400 with no rule; 409 with `conflicting_node_ids`; 409 with `missing_labels`.
- Batch: a duplicate inside the batch gives `created = 1, matched = 1`.
- An unknown `on_conflict` value is a 400.

Go client: created true, created false, no-rule 400, 409 with the conflicting ID, a batch with one
item error.

## 8. Delivery

Three PRs, in order, each with its coord task: storage, REST, Go client. The REST PR depends on
#674 (batch through the registry, open at the time of writing) and on #675
(`conflicting_node_id`, open at the time of writing). A CHANGELOG entry in each.
None of them changes an on-disk format.

### Left to the builder

- Interface placement. `BTreeGraphStorage` implements `Storage` (`btree_storage.go:718`); if the two
  methods go into `StorageWriter`, it gets stubs returning `errBTreeBackendUnsupported`.

## 9. Calls made without the user (check these)

1. D1 — explicit key in storage, registry only over REST.
2. D2 — merge, not replace; a JSON null stores null.
3. D3 — refuse a label difference rather than ignore it or union it.
4. D4 — a no-change match writes nothing.
5. D5 — `on_conflict=merge` as a query parameter, on both routes.
6. More than one node holding a key is a 409 that lists them, not a 500.
7. `ErrNoUniquenessRule` — an upsert never falls back to a plain create.
8. The batch keeps status 201 and adds `matched`.
9. An explicit storage key that differs from a registered rule for the labels is refused
   (`ErrUpsertKeyNotRegistered`) — from the builder check.
10. D6 — a merge checks the rules of the stored node's labels, under the lock — from the builder
    check. The same gap in `PUT /nodes/{id}` becomes its own task.
11. A batch item WAL failure keeps the batch create's current reporting — from the builder check.

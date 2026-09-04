# V1-spike — deep-path traversal for the variable-length pattern path

**Date**: 2026-09-04
**Status**: Design spike. No implementation is approved.
**Revision read**: `main` at `f4dcfd4` (`chore(go): raise the toolchain floor to 1.27.0 (#555)`).
**Tracks**: `docs/NEXT_STEPS_2026-06-18.md` §D, "V1-spike — Deep-path traversal for the
variable-length pattern path".

Every `file:line` below is read off `f4dcfd4`. Two PRs landed after the planning-doc item
was written, so the line numbers in that item are stale and these supersede them:

- **#546** (`0c45ec0`, "the variable-length path stops honestly") rewrote 93 lines of
  `pkg/query/match_path.go` and added `ErrTraversalTruncated` to
  `pkg/query/traversal_types.go` and `noteTruncation` to `pkg/query/executor_plan.go`.
- **#551** (`28c04c2`, "bare-star variable-length path means one-or-more hops") changed
  `pkg/query/parser_patterns.go` only. It did not touch `match_path.go`, but it changed
  what reaches it: a bare star now arrives as `MaxHops == -1`.

---

## 1. Problem and consumer

`oit-cyber/interrogate` imports graphdb as a Go library in one process. It answers "which
employees reach this path" over a POSIX directory tree joined with LDAP group data. A
directory is reachable through many group memberships, so the same employee node sits at
the end of dozens of distinct routes. graphdb's variable-length pattern path returns one
binding per route, so interrogate gets dozens of rows for one answer, and it pays for
every route it did not ask about. It therefore computes its own reachability closure
outside graphdb and uses the engine only as a store.

Two capabilities would move that closure back into the engine. **(a)** A node-visited
traversal mode, in which a node reachable by twenty routes produces one binding, and cost
grows with the number of nodes rather than the number of routes. **(b)** An
expansion-time filter, which rejects an intermediate node *before* the traversal expands
it, so the rejected subtree is never read. Capability (a) removes the row explosion.
Capability (b) removes the work: interrogate's real predicate consults LDAP and POSIX
state that no query string can express, and today the only place to apply it is after the
rows come back, by which time the engine has already walked the subtree.

---

## 2. Current behaviour

### 2.1 Dispatch

`MatchStep.Execute` (`pkg/query/match_executor.go:19`) calls `matchPattern`
(`match_executor.go:48`), which routes a pattern with relationships to `matchPath`
(`match_executor.go:58`). `matchPath` (`pkg/query/match_path.go:10`) resolves the start
nodes and calls `traversePath` (`match_path.go:31`).

`traversePath` (`match_path.go:46`) decides variable-length at `match_path.go:57`:

```go
isVariableLength := (rel.MinHops != 1 || rel.MaxHops != 1) && (rel.MinHops != 0 || rel.MaxHops != 0)
```

A true result dispatches to `traverseVariablePath` (`match_path.go:59`); a false result
goes to `traverseFixedPath` (`match_path.go:62`), which is the single-hop path and is out
of scope here.

`OptionalMatchStep.Execute` builds its own `MatchStep` (`pkg/query/executor_steps.go:484`)
and reaches the same code, so `OPTIONAL MATCH` inherits every behaviour below.

### 2.2 The per-path visited clone

`bfsEntry` (`match_path.go:102-108`) carries a `visited map[uint64]bool` per queue entry,
described in place as a "per-path visited set" (`match_path.go:107`). The function's own
doc comment states the consequence (`match_path.go:111-112`):

> Cycle detection is per-path (a single path can't revisit a node, but different
> paths can reach the same node).

The clone happens on every admitted neighbour (`match_path.go:219-223`):

```go
newVisited := make(map[uint64]bool, len(entry.visited)+1)
for k, v := range entry.visited {
    newVisited[k] = v
}
newVisited[neighborID] = true
```

The edge list is copied alongside it (`match_path.go:225-227`). So each queue entry costs
`O(depth)` memory for the map plus `O(depth)` for the edge slice, and the queue holds one
entry per distinct simple path prefix.

### 2.3 The BFS shape

`traverseVariablePath` (`match_path.go:113`) seeds a one-element queue with the start node
and a visited set containing only the start node (`match_path.go:143-144`). The loop
(`match_path.go:146`) does four things in order:

1. Checks cancellation and returns `results, nil` on a cancelled context
   (`match_path.go:148-150`). Note that a cancelled traversal returns a **nil** error here.
2. Dequeues, nulling the slot to release references (`match_path.go:152-154`).
3. Emits, if the entry depth is inside `[rel.MinHops, maxHops]` (`match_path.go:157`) and
   the node matches the target node pattern (`match_path.go:158`). Emission copies the
   binding, binds the relationship variable to `entry.edges` as a `[]*storage.Edge`
   (`match_path.go:160-162`), binds the target variable to the node
   (`match_path.go:163-165`), and recurses into the rest of the pattern
   (`match_path.go:167`).
4. Expands, reading the frontier node's edges through `getEdges` (`match_path.go:188`) and
   admitting each unvisited neighbour (`match_path.go:205-235`).

`nodeMatchesPattern` (`match_path.go:296-301`) runs at emit time only
(`match_path.go:158`). Labels and properties in the target node pattern therefore do
**not** prune expansion: the traversal walks through non-matching intermediate nodes and
discards them at the end. That is Cypher-correct, and it is exactly why capability (b) is
not already available by writing a label into the pattern.

`getEdges` (`match_path.go:249-277`) reads `rel.Direction` in a switch
(`match_path.go:253-264`) and then filters on `rel.Type` (`match_path.go:266-276`). It
takes no predicate parameter, so a branch cannot be pruned before the traversal enters it.
`rel.Properties` exists on the AST (`pkg/query/ast.go:54`) and the parser fills it, but no
code on the match path reads it — the only readers of `rel.Properties` in `pkg/query` are
the parser and the parameter injector (`pkg/query/executor.go:281-285`). An edge property
written into a pattern is silently ignored on this path.

Each `getEdges` call reaches `GetOutgoingEdgesForTenant` / `GetIncomingEdgesForTenant`
(`match_path.go:255-260`), which call the tenant-blind readers and allocate a fresh
filtered slice (`pkg/storage/query_operations.go:70-83`). Those readers take the global
`gs.mu.RLock` and increment `Statistics.TotalQueries`
(`pkg/storage/query_operations.go:40,90`, `pkg/storage/statistics.go:23`). Re-expanding the
same node on a second route therefore costs a real lock acquisition and a real allocation,
not a cache hit.

### 2.4 Where `MaxHops == -1` is handled

The parser encodes "unbounded" as `-1`. `*1..` sets it at
`pkg/query/parser_patterns.go:155`; the bare star `[:TYPE*]` sets `MinHops = 1` and
`MaxHops = -1` at `parser_patterns.go:169-170` (PR #551).

`traverseVariablePath` handles the encoding at `match_path.go:135-139`:

```go
engineCapped := rel.MaxHops == -1
maxHops := rel.MaxHops
if engineCapped {
    maxHops = MaxAllowedTraversalDepth
}
```

`MaxAllowedTraversalDepth` is 100 (`pkg/query/traversal_types.go:15`). An *explicit*
`MaxHops` above the cap is refused outright with a wrapped `ErrInvalidTraversalDepth`
(`match_path.go:122-124`, sentinel at `traversal_types.go:37`). The distinction is
deliberate: a caller who names a bound above the cap asked for something the engine will
not do, and a caller who names no bound gets the cap plus a signal.

### 2.5 Where truncation is signalled

`ErrTraversalTruncated` (`traversal_types.go:34`) travels beside the results, never instead
of them. `traverseVariablePath` raises it in two places, both through
`ctx.noteTruncation` (`pkg/query/executor_plan.go:49`), which keeps the *first* limit hit:

- **Result cap** (`match_path.go:180-184`): when `len(results) >= MaxAllowedResults`
  (1,000,000, `traversal_types.go:22`). The comment at `match_path.go:173-179` names the
  route explosion as the reason this cap exists at all.
- **Depth cap** (`match_path.go:190-203`): only when `engineCapped` is true, and only when
  a frontier node has at least one neighbour that this path has not visited
  (`match_path.go:194-201`). A frontier whose neighbours are all already on the path costs
  the caller nothing, so it is not reported.

The signal leaves the executor as the error return of `executePlanWithContext`
(`executor_plan.go:221` and `executor_plan.go:229`), beside a fully built `*ResultSet`.

**Two existing holes in that path, both pre-existing and both relevant here:**

- `executeWithProfiling` returns `result, nil` (`pkg/query/executor_explain.go:111`). A
  `PROFILE` query never reports truncation.
- `executeWithChain` returns only the next segment's result
  (`pkg/query/executor.go:200`), discarding the `ExecutionContext` it built at
  `executor.go:130`. Truncation in the first segment of a `WITH` chain is dropped.
- The REST query handler treats any non-nil error as a failure and answers HTTP 500,
  discarding the rows it was handed (`pkg/api/server_handlers.go:165-174`). No file outside
  `pkg/query` references `ErrTraversalTruncated` at all. So over REST a truncated query
  today loses both its rows and its meaning.

None of the three is caused by this design. All three change what "opt-in mode" is worth,
so they appear as decisions in §7.

### 2.6 What already exists elsewhere, and why it does not solve this

- `Traverser.BFS` (`pkg/query/traversal_bfs.go:12`) already keeps **one** visited set
  (`traversal_bfs.go:24,40-43`) and already takes `Predicate func(*storage.Node) bool` and
  `EdgePredicate func(*storage.Edge) bool` (`pkg/query/traversal_types.go:113-114`). It is
  not usable here: it reads through the **tenant-blind** `GetNode` and `GetOutgoingEdges`
  (`traversal_bfs.go:46,149,168`), it returns nodes rather than pattern bindings, and its
  predicate runs *after* the node is dequeued and counted (`traversal_bfs.go:59-61`), which
  filters but does not prune.
- `algorithms.KHopNeighboursForTenant` (`pkg/algorithms/khop.go:51`) is a tenant-scoped,
  node-visited BFS with distances (`khop.go:70-138`), reachable from Cypher as
  `CALL algo.kHop(...)` (`pkg/query/procedures.go:87`). It returns node **IDs** only
  (`khop.go:20-22`), has no `MinHops`, applies no node pattern, cannot bind a relationship
  variable, and cuts off at `MaxResults` with a **nil** error (`khop.go:128-135`).

The capability gap is specific: the pattern path has no node-visited mode and no
expansion-time predicate. Two sibling surfaces have parts of each.

---

## 3. Capability (a) — a node-visited traversal mode

### 3.1 What "one binding per node" means for a row

Today a target node reachable by twenty routes produces twenty `*BindingSet` values, each
with a different `[]*storage.Edge` bound to the relationship variable
(`match_path.go:160-162`). In node-visited mode it must produce one. The row still needs a
value for the relationship variable if the pattern names one, so the mode has to choose a
path to report.

**Recommendation: report the BFS discovery path — a shortest path in edge count.** A
breadth-first traversal that admits each node once admits it at its minimum depth, so the
discovery path is a shortest path by construction. Two consequences must be written into
the doc comment and the release note:

- It is *a* shortest path, not *the* shortest path. When several paths tie on length, the
  winner is decided by adjacency iteration order, and `pkg/storage` does not specify that
  order. Callers must not depend on which one they get.
- The path is a real path in the graph, so `length(r)` and the endpoint edges stay
  meaningful. That is the property the "bind nothing" alternative gives up.

The two alternatives to reporting a path are worth naming and rejecting. Binding the
relationship variable to `nil` makes every downstream expression over `r` a silent zero,
which is the failure mode this repository keeps refusing (a limit that acts and does not
tell the caller). Refusing patterns that name a relationship variable in this mode is
honest, but it blocks `MATCH (a)-[r:X*]->(b) RETURN b, length(r)`, which is a reasonable
thing for the consumer to write.

Depth is worth exposing too: the mode knows the discovery depth of every node, and
interrogate wants it ("how far is this employee from that directory"). The cheapest
surface is the existing relationship binding — `length(r)` already yields it — so no new
column is needed in v1.

### 3.2 Interaction with `MinHops`

This is the sharp edge. With one shared visited set, a node is admitted at its **minimum**
depth and never re-enters the queue. If `MinHops >= 2`, a node whose minimum depth is 1 is
admitted at depth 1, is not emitted (it is outside the window, `match_path.go:157`), and
never reappears at depth 2 — even when a genuine simple path of length 2 exists. The mode
would silently drop rows that today's mode returns.

**Recommendation: refuse the combination.** In node-visited mode, accept `MinHops` of 0 or
1 and return a wrapped sentinel error for `MinHops >= 2`. With `MinHops <= 1` no row can be
lost, because the start node sits at depth 0 and every other admitted node sits at depth
1 or deeper. The consumer uses the bare star, which is `MinHops == 1`
(`parser_patterns.go:169`), so the restriction costs it nothing. A loud refusal also
matches the project objective of preferring refusal over silent degradation.

The alternative is to accept `MinHops >= 2` and define the mode as "a node is reported if
and only if its **shortest** distance falls inside `[MinHops, maxHops]`". That is coherent,
it costs no code, and it is a different question from the one the pattern asks. If the user
prefers it, the cost is documentation rather than implementation — see decision **D2**.

### 3.3 Interaction with truncation

- The result cap (`match_path.go:180-184`) stays as it is. In node-visited mode the result
  count is bounded by the node count of the tenant subgraph, so the cap moves from "fires
  on a wide graph" to "effectively never fires". It stays correct and stays a memory bound.
- The depth cap (`match_path.go:190-203`) needs one substitution: the unexplored-neighbour
  test at `match_path.go:196` must consult the **shared** visited set instead of
  `entry.visited`. That makes it strictly more accurate. A neighbour already visited
  globally is already in the answer, so it is not lost work and must not raise the signal.
- No new truncation source appears.
- The cancellation return at `match_path.go:148-150` returns a nil error, which claims
  completeness for an incomplete answer. It is pre-existing and out of scope, and it is
  named in §7 as **D7** because a consumer that relies on the node-visited mode for a
  security answer will meet it.

### 3.4 Cost model

Let `N` be the nodes reachable within the depth window, `E` the edges among them, `d` the
depth cap, and `P` the number of distinct simple paths within the window.

| | today (all simple paths) | node-visited mode |
|---|---|---|
| queue entries | `O(P)` | `O(N)` |
| memory per entry | `O(d)` visited clone + `O(d)` edge slice (`match_path.go:219-227`) | `O(1)` parent pointer |
| peak memory | `O(P · d)` | `O(N)` |
| `getEdges` calls | `O(P)` | `O(N)` |
| storage adjacency reads | `O(P)` — each one takes `gs.mu.RLock` and allocates (`query_operations.go:40,70-83`) | `O(N)` |
| bindings emitted | `O(P)` | `O(N)` |
| total | `O(P · d)` | `O(N + E)` |

`P` is exponential in `d` for a branching graph: a tree of branching factor `b` gives
`P = O(b^d)`. That is the whole complaint. `N + E` is linear in the subgraph.

### 3.5 Alternative designs

**A1 — one shared visited set in BFS order, plus a per-node parent pointer.
(Recommended.)**
Replace `bfsEntry.visited` (`match_path.go:107`) with a single `map[uint64]bool` owned by
the call, and replace the cloned `entry.edges` with a discovery record per node
(`map[uint64]struct{ edge *storage.Edge; parent uint64 }`). Rebuild the edge list only at
emit time, walking the parent chain, which is `O(depth)` for the nodes that actually
produce a row rather than for every prefix.

- *Pros*: meets the acceptance criterion exactly; deletes the two clones that dominate cost
  today; keeps the existing loop shape, the emit gate (`match_path.go:157-158`), the
  recursion into the rest of the pattern (`match_path.go:167`) and both truncation checks;
  gives a shortest path for free; is the same shape as `Traverser.BFS`
  (`traversal_bfs.go:24-43`) and `kHopNeighboursView` (`khop.go:70-138`), so the repository
  gains no third idea.
- *Cons*: the emitted path is a shortest one and not the caller's choice of one; the
  `MinHops >= 2` interaction needs the refusal in §3.2; the parent chain has to be
  reconstructed at emit time, which is new code the per-path version did not need.

**A2 — a per-node best-depth map (`map[uint64]int`), re-admitting a node at a strictly
smaller depth.**

- *Pros*: keeps the door open for a future weighted or DFS traversal, where a later path can
  genuinely be shorter; makes the reported depth explicit rather than implied.
- *Cons*: in a pure BFS, the first admission is already the minimum, so the re-admission
  branch is dead code. It buys a larger map and a comparison per edge for no behaviour
  change. Reject for v1.

**A3 — a reachability closure pass before binding, over
`algorithms.KHopNeighboursForTenant` (`khop.go:51`).**
Run kHop, then build one binding per returned ID that matches the target node pattern.

- *Pros*: reuses tested tenant-scoped code; leaves `match_path.go` untouched; distances come
  back for free (`khop.go:21`).
- *Cons*: kHop returns IDs (`khop.go:20`), so every node must be re-loaded through
  `GetNodeForTenant`; it has no `MinHops`; it cannot bind a relationship variable, so
  `[r:X*]` becomes unsupported in the new mode; it truncates at `MaxResults` with a nil
  error (`khop.go:128-135`), which contradicts the truncation contract #546 just
  established; and it forks the depth-cap and truncation rules into a second implementation
  of the same idea, which is the defect `traversal_limit_honesty_test.go:9-17` was written
  about. Reject.

**A4 — keep today's traversal and deduplicate the bindings by target node ID afterwards.**

- *Pros*: a ten-line diff; produces exactly the right rows.
- *Cons*: fails the acceptance criterion. Cost still grows with routes, because every route
  is still walked, cloned and allocated before the duplicate is dropped. It fixes the
  symptom the consumer can already fix itself and leaves the one it cannot. Reject — but
  name it in review, because it is the tempting shortcut.

**Recommendation: A1.**

---

## 4. Capability (b) — the expansion-time filter

### 4.1 The Go API

```go
// PathSemantics selects how a variable-length pattern segment enumerates.
type PathSemantics int

const (
    // AllSimplePaths is today's behaviour and the zero value: every distinct
    // simple path within [MinHops, MaxHops] produces a binding.
    AllSimplePaths PathSemantics = iota

    // DistinctNodes visits each node at most once. A node reachable by twenty
    // routes produces one binding, and the relationship variable is bound to
    // the BFS discovery path, which is a shortest path in edge count.
    DistinctNodes
)

// Expansion describes one candidate step, offered to an ExpandFilter before
// the traversal enters it.
//
// It is a struct rather than a parameter list so a later field does not break
// existing callers.
type Expansion struct {
    From  *storage.Node // the frontier node being expanded
    Edge  *storage.Edge // the edge under consideration
    To    *storage.Node // the candidate node at the far end
    Depth int           // the depth To would occupy (From's depth + 1)
}

// ExpandFilter reports whether the traversal may enter To.
//
// A false return prunes the candidate: To is not bound, is not queued, and is
// never expanded, so To's own neighbours are never read from storage. To IS
// read once, because the filter receives the loaded node.
//
// The filter runs once per edge examined, inside the traversal. It must not
// call back into the executor. A panic is recovered by ExecuteWithContext
// (executor.go:75-82) and returned as a query error.
type ExpandFilter func(Expansion) bool

// PathOptions carries per-call traversal policy for variable-length patterns.
// The zero value is today's behaviour exactly.
type PathOptions struct {
    Semantics PathSemantics
    Expand    ExpandFilter
}
```

The two capabilities are orthogonal. `Expand` applies in both semantics, and `Semantics`
works with a nil filter.

### 4.2 What the predicate receives

All four fields, and each earns its place:

- `To` is the decision subject. interrogate's rule is about the candidate node.
- `Edge` lets the rule depend on how the node was reached, which `rel.Type` cannot express
  because `getEdges` filters on one type only (`match_path.go:267`), and which no pattern
  can express because `rel.Properties` is never read on this path (§2.3).
- `From` lets the rule compare the two ends, which a POSIX ownership rule needs.
- `Depth` lets the rule tighten with distance, which is the cheap way to bound a wide
  search without lowering `MaxHops` globally.

Passing a struct rather than four parameters is deliberate: adding a field later is a
compatible change, and adding a parameter is not. This diverges from the existing
`Predicate func(*storage.Node) bool` / `EdgePredicate func(*storage.Edge) bool` shapes
(`traversal_types.go:113-114`) on purpose — those two cannot see depth, and a node-only
predicate cannot see which edge brought it.

### 4.3 Before or after the visited check

**After the visited check, after the node load, before admission.** In the expansion loop
(`match_path.go:205-235`) the order becomes:

1. `neighborID := ms.targetNodeID(edge, rel, entry.node)` (`match_path.go:206`)
2. visited check (`match_path.go:209` today; the shared set in `DistinctNodes` mode)
3. `GetNodeForTenant` (`match_path.go:213`)
4. **new**: `if opts.Expand != nil && !opts.Expand(Expansion{...}) { continue }`
5. mark visited, build the discovery record, enqueue (`match_path.go:219-234`)

Reasons, in order of weight:

- Step 4 must precede step 5, or the acceptance criterion fails. A rejected node that
  reaches the queue gets expanded, and its subtree gets read.
- Step 4 must follow step 3, because the filter receives the loaded node. An edge-only
  filter could run before the load and would be cheaper; see **D5**.
- Step 4 should follow step 2 so the engine does not spend caller CPU deciding about a node
  it has already excluded.
- **A rejection is not memoised.** The filter may read `Depth` and `Edge`, so "rejected here"
  does not mean "rejected everywhere", and marking a rejected node visited would be wrong.
  The filter therefore runs once per examined edge, which stays inside the `O(N + E)`
  budget of §3.4.

"Never visits the rejected subtree" needs one precise word. The rejected node itself is
**read** once (step 3) and never **expanded**. Its neighbours are never read. The test in
§6 asserts the second, which is the property that matters.

### 4.4 Alternatives

**B1 — Cypher syntax, as Memgraph's `(r, n | condition)`.**

- *Pros*: reaches every consumer including REST and GraphQL; declarative, so the optimiser
  and the plan cache can see it; portable phrasing that other engines already use.
- *Cons*: lexer, parser, AST, evaluator and tests, for a consumer that is in-process Go; and
  an expression language cannot call interrogate's LDAP and POSIX code, which *is* the
  predicate. It would ship a syntax that does not answer the asking consumer's question.
  Reject for v1. It is the right follow-up the day a REST consumer asks.

**B2 — a declarative label and property filter on the options
(`AllowLabels []string`, `RequireProps map[string]any`).**

- *Pros*: serialisable, so REST could carry it later without a parser; no closure crossing
  into the engine; no panic surface.
- *Cons*: cannot express the consumer's rule at all; and it duplicates `nodeMatchesPattern`
  (`match_path.go:296-301`), which already evaluates exactly that shape at emit time. Two
  implementations of one idea is the pattern this repository has been paying down. Reject
  as the primary. It is the natural REST shape if **D4** is ever answered "yes".

**B3 — reuse `TraversalOptions.Predicate` and `EdgePredicate`
(`traversal_types.go:113-114`).**

- *Pros*: one vocabulary across the BFS/DFS surface and the pattern surface.
- *Cons*: two functions where one decision is being made; neither carries depth; the node
  predicate cannot see the edge. Copying the shape would lock in the limitation. Reject,
  and say so in the doc comment so the divergence reads as a choice.

**Recommendation: the single struct-argument `ExpandFilter` of §4.1.**

---

## 5. Where the options plug in

### 5.1 The exact call path

```
caller
  └─ Executor.ExecuteWithContext          executor.go:73
       ├─ buildExecutionPlan              executor_plan.go:117   (MatchStep at :124)
       ├─ Optimizer.Optimize              executor.go:105
       └─ one of
            ├─ executeWithProfiling       executor.go:116  → executor_explain.go:64
            ├─ executeWithChain           executor.go:121  → executor.go:129
            └─ executePlanWithContext     executor.go:125  → executor_plan.go:189
                 └─ newExecutionContext   executor_plan.go:190 (definition :58)
                      └─ MatchStep.Execute      match_executor.go:19
                           └─ matchPattern      match_executor.go:48
                                └─ matchPath    match_executor.go:58 → match_path.go:10
                                     └─ traversePath        match_path.go:31 → :46
                                          └─ traverseVariablePath  match_path.go:59 → :113
```

`OptionalMatchStep.Execute` (`executor_steps.go:483`) joins at `matchPattern`
(`executor_steps.go:491`).

There are exactly three `newExecutionContext` call sites: `executor.go:130`,
`executor_explain.go:65` and `executor_plan.go:190`. All three have the `*Query` and the
`*Executor` in scope. `traverseVariablePath` already receives `*ExecutionContext`
(`match_path.go:113`), so `ExecutionContext` is the natural carrier and no signature below
the executor changes.

### 5.2 The smallest change that does not alter existing callers

Add one field to `ExecutionContext` (`executor_plan.go:31-45`):

```go
pathOpts PathOptions // zero value == today's behaviour
```

Read it at `match_path.go:113` and nowhere else. Because `PathOptions{}` means
`AllSimplePaths` with a nil filter, every caller that does not opt in keeps its behaviour
by construction rather than by a conditional — the negative-control test in §6 then proves
the construction.

Three ways to get the value into `newExecutionContext`:

**P1 — a field on `Executor` plus a setter.** Two lines.
*Pros*: smallest possible diff; matches the existing `SetQueryTimeout` shape
(`executor.go:58`).
*Cons*: process-wide. `pkg/api` builds exactly one shared executor
(`pkg/api/server_init.go:273`), so one caller's opt-in would change every tenant's rows.
The field is unsynchronised, so a setter called while a query runs is a data race. Reject.

**P2 — a field on `*Query`.** Four lines: one field, three call sites.
*Pros*: the smallest correct diff; `*Query` is already the per-call carrier and already
mutated in place by `injectParams` (`executor.go:228`).
*Cons*: puts a Go closure on a parsed-syntax struct. `*Query` is what the parser produces
and what `ExecuteWithText` (`executor.go:470`) pairs with a cached plan; a reader will
reasonably expect it to describe the query text and nothing else.

**P3 — a new public entry point plus one extra parameter on the three internal executors.
(Recommended.)**

```go
func (e *Executor) ExecuteWithOptions(ctx context.Context, q *Query, opts PathOptions) (*ResultSet, error)
```

`ExecuteWithContext` (`executor.go:73`) becomes a call to it with `PathOptions{}`, and
`executePlanWithContext`, `executeWithChain` and `executeWithProfiling` each take the value
and pass it to `newExecutionContext`.
*Pros*: per-call, so a shared server executor is safe; no shared mutable state and no race;
the AST stays syntax; the option is visible in the type system where a reader looks for it;
existing public signatures are untouched.
*Cons*: about eight lines of signature churn across three files, and `executeWithChain`
must pass the options to the recursive `ExecuteWithContext` at `executor.go:200` or the
second segment of a `WITH` chain silently reverts to the default.

**P4 — a `context.Context` value.** Rejected. The option changes which rows come back.
Hiding that in a context value makes it invisible at the call site and untypeable at the
boundary.

**Plan cache.** `ExecuteWithText` (`executor.go:470`) caches the *optimised plan* keyed by
query text (`executor.go:472,485`). P3 keeps the options off both the plan and the AST, so
the cache stays correct with no key change. Under P2 it also stays correct, because the
cache stores the plan while the `*Query` still arrives from the caller. Under P1 it is
correct and process-wide, which is the objection to P1 rather than a cache problem. Say
this in the PR, because it is the first question a reviewer will ask.

---

## 6. Test plan

Every test below must be seen to fail before the fix exists, and the PR must record what it
printed. The helpers already exist: `chainGraph`
(`pkg/query/traversal_limit_honesty_test.go:36`), `runQuery` (`:64`) and
`setupExecutorTestGraph`. The control convention to copy is
`pkg/query/bare_star_variable_length_test.go:50-95`, which pairs each assertion with a
control that proves the change did not over-reach.

A new fixture is needed: `fanGraph(t, routes)` builds one `:Root`, `routes` distinct `:Mid`
nodes, and one `:Sink`, with `Root -[:LINK]-> Mid_i -[:LINK]-> Sink` for every `i`. The
sink is then reachable by exactly `routes` distinct simple paths of length 2.

### Capability (a)

| id | Test | Assertion | How it goes red first |
|---|---|---|---|
| A-1 | `TestDistinctNodesModeReturnsOneRowPerNode` | `fanGraph(t, 20)`, `MATCH (a:Root)-[:LINK*1..2]->(b:Sink) RETURN b.name` in `DistinctNodes` returns **1** row | Before the mode exists the option does not compile. After it exists, revert the mode selection and watch it print 20. |
| A-2 | `TestAllSimplePathsModeIsUnchanged` (**negative control**) | The same query with no options returns **20** rows | Must pass before and after. If it ever passes only after, the default changed and the constraint is broken. |
| A-3 | `TestSingleHopPatternUnaffectedByPathOptions` (**control**) | `MATCH (a:Root)-[:LINK]->(b)` returns the same rows in both modes | Mirrors `bare_star_variable_length_test.go:114-140`; proves the mode did not leak into `traverseFixedPath` (`match_path.go:66`). |
| A-4 | `TestDistinctNodesCostGrowsWithNodesNotRoutes` | Take `gs.GetStatistics().TotalQueries` (`pkg/storage/statistics.go:10`) before and after. On `fanGraph(t, 20)` the delta in `DistinctNodes` is bounded by a small multiple of the node count and is strictly below the `AllSimplePaths` delta | Run it against `AllSimplePaths` and confirm the delta is route-shaped. **Caveat**: `TotalQueries` counts every adjacency read in the process (`query_operations.go:40,90`), so the test must take a tight delta around one query and must run without `t.Parallel()`. |
| A-5 | `TestDistinctNodesRefusesMinHopsAboveOne` | `*2..3` in `DistinctNodes` returns an error wrapping the new sentinel; **control**: `*1..3` is accepted | Assert `errors.Is`, in the shape of `TestVariablePathRefusesAnExplicitDepthAboveTheCap` (`traversal_limit_honesty_test.go:79`). Depends on decision **D2**. |
| A-6 | `TestDistinctNodesStillReportsTruncation` | A chain longer than `MaxAllowedTraversalDepth`, bare star, `DistinctNodes`: rows come back **and** the error wraps `ErrTraversalTruncated` | Mirrors `TestUnboundedVariablePathReportsThatItStoppedAtTheCap` (`traversal_limit_honesty_test.go:98`). Red by deleting the shared-visited substitution at `match_path.go:196`. |
| A-7 | `TestDistinctNodesBindsAShortestPath` | With a 1-hop and a 3-hop route to the same sink, `length(r)` on the single row is 1 | Red by seeding the queue depth-first. |

### Capability (b)

| id | Test | Assertion | How it goes red first |
|---|---|---|---|
| B-1 | `TestExpandFilterPrunesTheSubtree` | A gate node with a 3-node subtree behind it. The filter rejects the gate. **(i)** no subtree node appears in the rows, and **(ii)** the filter is never called with any subtree node — record every `Expansion.To.ID` in a slice inside the filter and assert the set | The recorded call list is the instrument, and it must be shown to contain the subtree IDs when the filter is moved after admission. Assertion (ii) is what makes this a pruning test rather than a filtering test. |
| B-2 | `TestNilExpandFilterIsUnchanged` (**negative control**) | The same graph with a nil filter returns every row | Must pass before and after. |
| B-3 | `TestExpandFilterAppliesInBothSemantics` | The same filter prunes under `AllSimplePaths` and under `DistinctNodes` | Proves the two capabilities are orthogonal, as §4.1 claims. |
| B-4 | `TestExpandFilterReceivesEdgeAndDepth` | A filter that rejects on `Edge.Type` and one that rejects on `Depth` each prune | Red before the struct carries the fields. |
| B-5 | `TestPanickingExpandFilterBecomesAnError` | A filter that panics yields an error, not a crash | Relies on the recover at `executor.go:75-82`. Red by calling the filter outside that recover. |
| B-6 | `TestExpandFilterNeverSeesAForeignTenantNode` | Two tenants, a filter that records every `To.TenantID`: only the querying tenant appears | Guards the new code path against bypassing `GetNodeForTenant` (`match_path.go:213`). Red by substituting the tenant-blind reader. |

### Instrument checks

Before trusting A-1, run it in `AllSimplePaths` and see 20. Before trusting B-1, remove the
filter and see the subtree appear. Before trusting A-4, confirm the counter moves at all on
a trivial query. A test that has not been watched go red is not a gate.

### Consumer contract

`docs/CONSUMER_CONTRACTS.md` requires a row when driving a consumer surfaces a
**divergence**. This is a new capability rather than a divergence, so no row is owed yet.
A-2 (the default is unchanged) is the assertion most worth pinning as `CC15` the day
interrogate ships against the mode. See **D9**.

---

## 7. Risks and open questions

**Risks**

- **R1 — a second traversal shape in one function.** `traverseVariablePath` is 134 lines
  and already carries two truncation rules and two depth rules. A mode flag threaded
  through the same loop will make it harder to read. Mitigation: extract the expansion loop
  (`match_path.go:205-235`) behind a small internal frontier type with two implementations,
  and leave the emit gate and both truncation checks in one place.
- **R2 — the opt-in is only as opt-in as its zero value.** `PathOptions{}` must mean today's
  behaviour on every path, including `OPTIONAL MATCH` (`executor_steps.go:484`), `PROFILE`
  (`executor_explain.go:64`) and both segments of a `WITH` chain (`executor.go:200`). A-2
  and A-3 must run on all four surfaces, not only on the plain one.
- **R3 — caller code inside the engine.** An `ExpandFilter` runs while the traversal holds
  no storage lock, but it runs on the query goroutine and under the query timeout. A slow
  filter looks exactly like a slow query. Document it, and do not add a filter timeout in
  v1.
- **R4 — the shortest-path tie is unspecified.** §3.1. If a consumer starts depending on
  which path comes back, the adjacency order becomes a compatibility surface that
  `pkg/storage` never promised.
- **R5 — the truncation signal has three holes today** (§2.5): `PROFILE`
  (`executor_explain.go:111`), the first `WITH` segment (`executor.go:200`), and the REST
  handler answering 500 with the rows discarded (`server_handlers.go:165-174`). The
  node-visited mode is aimed at a security question, and a security answer that is quietly
  incomplete is the exact defect `traversal_limit_honesty_test.go:23-25` records.

**Decisions for the user**

- **D1 — What does the row report in `DistinctNodes` mode?** Recommended: the BFS discovery
  path, which is *a* shortest path, with the tie explicitly unspecified. Alternatives: bind
  the relationship variable to nil, or refuse patterns that name one.
- **D2 — `MinHops >= 2` in `DistinctNodes` mode: refuse, or redefine?** Recommended: refuse
  with a named sentinel (A-5). Alternative: define the mode as "reported if and only if the
  **shortest** distance falls inside the window" and document it.
- **D3 — Where do the options live?** Recommended: **P3**, a new `ExecuteWithOptions` entry
  point. Alternatives: **P2** a `*Query` field (smaller, mixes policy into the AST), **P1**
  an `Executor` field (smallest, process-wide and racy).
- **D4 — Does either capability get a REST or GraphQL surface?** Recommended: **no** for
  v1. `pkg/graphql` does not import `pkg/query` at all (verified by grep, with the same grep
  finding five importers elsewhere), and it resolves edges per field with its own depth
  limit (`pkg/graphql/edges_types.go:87`, `pkg/graphql/depth.go:22`), so it needs nothing.
  `pkg/api` shares one executor across every request (`server_init.go:273`), so a
  process-wide switch there would change every tenant's rows; and an `ExpandFilter` is a Go
  closure, which cannot cross HTTP under any design.
- **D5 — Does the filter receive the loaded node, or only the edge?** Recommended: the
  loaded node, which costs one `GetNodeForTenant` per examined edge
  (`match_path.go:213`). Alternative: an edge-only filter that runs before the load, which
  is cheaper on a wide fan-out and cannot express a node rule. Both is possible and is more
  API than v1 needs.
- **D6 — Naming.** Recommended: `PathSemantics` with `AllSimplePaths` / `DistinctNodes`,
  rather than a `bool`. A third semantics (shortest path only, as `allShortestPaths`) is
  plausible enough that a bool would need replacing.
- **D7 — Fix the truncation holes first, alongside, or not at all?** Recommended: a separate
  PR **before** this work, covering `executor_explain.go:111` and `executor.go:200`. The
  cancellation return at `match_path.go:148-150` also claims completeness for an incomplete
  answer and belongs in the same PR.
- **D8 — The REST 500.** `server_handlers.go:165-174` turns `ErrTraversalTruncated` into
  HTTP 500 and discards the rows. Recommended: a separate PR that checks
  `errors.Is(err, query.ErrTraversalTruncated)` and answers 200 with the rows plus a header,
  matching the `X-Enumeration-Incomplete` shape that CC14 already established. Out of scope
  here, but it is the reason **D4** matters.
- **D9 — Add a `CC15` consumer-contract row now, or when interrogate ships?** Recommended:
  when it ships. A-2 is the assertion to pin.
- **D10 — Does the filter also apply to single-hop patterns (`traverseFixedPath`,
  `match_path.go:66`)?** Recommended: **no** for v1. A single hop has no subtree to prune,
  so the filter would only duplicate a `WHERE` clause.

---

## 8. Size estimate

Assuming **A1**, **P3**, the recommended answers to D1, D2, D5, D6, and D10.

| Item | Files | New / changed LOC |
|---|---|---|
| Option types (`PathSemantics`, `Expansion`, `ExpandFilter`, `PathOptions`, sentinel error) | new `pkg/query/path_options.go` | ~55 (mostly doc comments) |
| Capability (a): shared visited set, discovery records, path rebuild, `MinHops` refusal, truncation substitution | `pkg/query/match_path.go` | ~90–120 changed |
| Capability (b): the filter call in the expansion loop | `pkg/query/match_path.go` | ~15 |
| Plumbing (P3): entry point plus three internal signatures plus the `WITH` recursion | `pkg/query/executor.go`, `executor_plan.go`, `executor_explain.go` | ~30 |
| Tests (a): A-1 … A-7 plus the `fanGraph` fixture | new `pkg/query/distinct_nodes_traversal_test.go` | ~250 |
| Tests (b): B-1 … B-6 | new `pkg/query/expand_filter_test.go` | ~200 |
| Docs | `CHANGELOG.md`, doc comments | ~25 |

**Totals**: roughly 190 LOC of production code and 450 LOC of tests, across three changed
files and three new files.

**Out of scope, and unchanged**: `pkg/storage` (no new accessor is needed — the traversal
already reads through `GetNodeForTenant` and `Get*EdgesForTenant`), the on-disk snapshot
formats, `pkg/graphql`, `pkg/algorithms`, `pkg/query/traversal_bfs.go`,
`pkg/query/traversal_dfs.go`, `pkg/query/parallel_traversal.go`, and every client SDK.

If the user prefers **P2** over **P3**, subtract about 20 LOC of plumbing. If **D2** is
answered "redefine" instead of "refuse", subtract A-5 and about 15 LOC.

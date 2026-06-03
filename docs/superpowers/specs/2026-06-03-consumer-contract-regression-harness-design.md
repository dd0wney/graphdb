# Consumer-contract regression harness (Track Q / Q4) — design

**Date**: 2026-06-03
**Status**: design, pending implementation plan
**Track**: Q4 (the last open Track Q item; see `docs/NEXT_STEPS_2026-06-03.md`)

## Motivation

Track Q drove graphdb's two live consumers against `main` and found four bugs the
white-box unit suite structurally could not anticipate — three of them at integration
seams (REST decode path, cross-process snapshot reopen, batch-write → tenant-read), one
(#287) independently confirmed by a *second* consumer (Stór). The recurring shape:

> A behaviour a real consumer depends on is broken by a change to an apparently-unrelated
> internal path, and no graphdb-owned test asserts that behaviour, because the dependency
> only exists at the consumer's integration boundary.

Q4 generalises the one-off fixes into a **standing, graphdb-owned, growing** mechanism so the
*next* such breakage is caught in graphdb's own CI rather than in the field — without
depending on infrastructure graphdb cannot currently reach.

### The constraint that shapes the design

A live-consumer CI job (build + run the real consumers against `main` in graphdb's CI) is
**not buildable today**: `understand-graphdb` has **no git remote** (local-only — graphdb CI
cannot clone it) and `coi-screen` is **private** (would need CI deploy-key secrets). A job
that cannot run rots and gets ignored. So the enforceable core lives *in graphdb's repo*,
and the live drive is a reproducible on-demand drill with a documented promotion path.

## Goals

- Every consumer-relied invariant graphdb has learned about is a graphdb-owned test that
  runs in normal CI and fails against the pre-fix code.
- The set is **discoverable as a set** (greppable + catalogued), not scattered anonymously.
- Adding the next contract is a documented, low-friction step — the harness **grows by
  construction** as consumers surface divergences.
- The high-fidelity "drive the real consumers" check is **reproducible on demand**, with no
  external service/corpus/auth dependencies, via deterministic fixtures.

## Non-goals

- **No cross-repo CI job** running the real consumers (blocked: remoteless + private repos).
  Documented as future work with explicit prerequisites.
- **No real ICIJ corpus** dependency (absent locally; synthetic structurally-real corpus is
  sufficient for the contracts — proven this session).
- **No churn of the existing pins.** The five existing test funcs stay in their current files
  (correct homes + blame history); they are *labelled and indexed*, not moved or duplicated.
- No new production code. Q4 is test + doc + script infrastructure only.

## Architecture

Three components, each independently understandable:

```
1. Tagged contract tests   (enforced in CI, in pkg/storage + pkg/api)
        │  each carries a  // CONSUMER CONTRACT: <id> — <consumer> (#PR)  tag
        ▼
2. docs/CONSUMER_CONTRACTS.md   (the catalogue + the growth rule)
        │  invariant → consuming repo → guarding test(s) → originating bug
        ▼
3. scripts/consumer-drive.sh   (on-demand high-fidelity drill, deterministic)
```

The cross-package reality (contracts live in *both* `pkg/storage` and `pkg/api`) means there
cannot be a single consolidated `TestConsumerContract` func the way `audit_regression_test.go`
consolidates one package's guardrails. The unifying layer is therefore the **tag convention +
catalogue**, not a mega-test.

### Component 1 — Tagged contract tests

- **Convention**: each test that pins a consumer-relied invariant carries a one-line doc
  comment `// CONSUMER CONTRACT: <id> — <consumer(s)> (<guarding PR>)`. This makes the whole
  set greppable: `grep -rn "CONSUMER CONTRACT:" pkg/`.
- **Existing pins are retro-tagged in place** (a single comment line each — minimal churn,
  preserves homes + blame):

  | id | invariant | consumer(s) | test func(s) | PR |
  |----|-----------|-------------|--------------|----|
  | `CC1-rest-vector-ingest` | A JSON number-array property on a vector-indexed name is indexed + searchable over REST | understand-graphdb (neural) | `pkg/api` `TestVectorSearch_RESTFloatArrayIngestionRoundTrip` | #286 |
  | `CC2-vector-nn-identity` | Vector search returns the actually-nearest nodes by identity + order, not just count | understand-graphdb | `pkg/api` `TestVectorSearch_NearestNeighbourCorrectness`, `pkg/storage` `TestVectorSearchForTenant_KnownAnswerOrdering` | #283 |
  | `CC3-adjacency-reopen` | Edge adjacency survives a snapshot `Close()`→reopen under the default compression config | coi-screen, Stór | `pkg/storage` `TestEdgeAdjacencySurvivesReopen` | #287 |
  | `CC4-bulkimport-tenant-visible` | Data written via the batch/bulk-import path is visible to every `*ForTenant` reader, in-memory and after reopen | coi-screen (import-icij) | `pkg/storage` `TestBatchCommit_VisibleToForTenantReaders`, `TestBatchCommit_VisibleAfterReopen` | #288 |

- **Gap-fill**: Q4 also adds tests for consumer-relied invariants surfaced this session but
  *not yet pinned*. Initial gap to close: **`CC5-label-filtered-vector-search`** — `coi-screen`
  and `understand-graphdb`'s `vectorSearch` pass `filter_labels`; pin the label-filtered vector
  path end-to-end over REST under the float-array-ingestion path (composes CC1 × the existing
  label-filter test, which today only run separately). One new `pkg/api` test.

### Component 2 — `docs/CONSUMER_CONTRACTS.md`

The catalogue and the growth mechanism. Contains:
- A one-paragraph statement of what a "consumer contract" is and why it exists (the Q4
  motivation above, condensed).
- The contract table (the `CC*` rows above, kept in sync with the tags).
- **The growth rule**, stated explicitly: *"When driving a consumer surfaces a divergence,
  the fix lands with (a) a tagged contract test that fails against the pre-fix code and (b) a
  new row here. A contract is retired only when its consumer is."*
- A pointer to `scripts/consumer-drive.sh` for the full live drill.

### Component 3 — `scripts/consumer-drive.sh`

A reproducible, dependency-free local drill that exercises the *real* consumer code against a
freshly-built graphdb — the high-fidelity check CI can't host yet. It:
- builds graphdb from the current checkout;
- runs `coi-screen`'s suite via its `replace => ../graphdb` path (embedded-library contract),
  then imports a small **synthetic ICIJ-shaped corpus** (committed fixture or generated by a
  committed generator) and runs a screen asserting the planted conflict flags;
- starts a local graphdb server with a **deterministic OpenAI-compatible embeddings server**
  (token-hashing vectorizer, no external key) and runs `understand-graphdb`'s
  `GRAPHDB_INTEGRATION=1` suite + a neural ingest→search round-trip;
- prints a pass/fail summary; non-zero exit on any failure.
- Header-documents the **prerequisites to promote this to CI**: push `understand-graphdb` to a
  remote; add a `coi-screen` deploy key; then a `.github/workflows/consumer-contracts.yml` can
  invoke this script. (The script is structured so that promotion is "run it from CI," not a
  rewrite.)

The synthetic corpus generator and the deterministic embedder are committed under `scripts/`
(`scripts/gen-icij-synth.py`, `scripts/embed-server.py`) alongside `consumer-drive.sh`, so the
drill is self-contained and reproducible — not reconstructed each time. (These are the two
throwaway helpers this session built ad hoc; Q4 promotes them to committed fixtures.)

## Data flow

There is no runtime data flow — this is test/doc/script infrastructure. The "flow" is the
*process loop* the catalogue encodes: consumer divergence → graphdb fix + tagged test + new
catalogue row → CI enforces it forever after.

## Error handling / failure modes

- A broken contract = a red CI test in normal `go test` (Components 1). No special harness.
- `consumer-drive.sh` exits non-zero with a per-step summary on any consumer failure; it never
  silently passes (the "no silent caps" rule — if a consumer can't be driven because a
  prerequisite is missing, it says so and fails that step explicitly).

## Testing strategy

- The contract tests *are* the tests; they must each fail against pre-fix code (the
  neuter-and-fail bar already met by #286/#287/#288 this session; CC5 must meet it too).
- `consumer-drive.sh` is validated once by running it locally end-to-end (as done manually
  this session) and confirming green.
- No test-of-tests beyond that.

## Future work (explicitly out of scope for this spec)

- **Live-consumer CI job**, once `understand-graphdb` has a remote and `coi-screen` has a CI
  deploy key. `consumer-drive.sh` is the body; the workflow is a thin wrapper.
- **Batch delete/update tenant-index gap** (surfaced by #288's audit) — a graphdb fix + a CC
  row, when a consumer needs batch delete/update.
- **Real ~814K ICIJ corpus** resolution-precision run for coi-screen (corpus is an external
  download; the synthetic drill covers the structural contracts).

## Summary of deliverables

1. One-line `// CONSUMER CONTRACT:` tags on the 5 existing pin test funcs.
2. One new test: `CC5-label-filtered-vector-search` (`pkg/api`).
3. `docs/CONSUMER_CONTRACTS.md` — catalogue + growth rule.
4. `scripts/consumer-drive.sh` + committed synthetic-corpus generator + deterministic embedder.
5. A short pointer from `CLAUDE.md` ("Common workflows" or "Orient first") to
   `CONSUMER_CONTRACTS.md` so the convention is discoverable by future agents.

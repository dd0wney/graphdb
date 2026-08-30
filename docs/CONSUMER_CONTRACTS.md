# Consumer contracts

A **consumer contract** is a graphdb behaviour a real downstream consumer depends on, pinned
by a graphdb-owned test that fails against the pre-fix code. They exist because Track Q showed
that the dangerous bugs live at consumer integration seams (REST decode, cross-process
snapshot reopen, batch-write → tenant-read) that white-box unit tests structurally miss — and
were only found by driving the real consumers. This file is the registry; the tests are the
enforcement.

Find the tests: `grep -rn "CONSUMER CONTRACT:" pkg/`

| id | Invariant | Consumer(s) | Guarding test(s) | Origin |
|----|-----------|-------------|------------------|--------|
| CC1-rest-vector-ingest | A JSON number-array property on a vector-indexed name is indexed + searchable over REST | understand-graphdb (neural) | `pkg/api` `TestVectorSearch_RESTFloatArrayIngestionRoundTrip` | #286 |
| CC2-vector-nn-identity | Vector search returns the actually-nearest nodes by identity + order, not just count | understand-graphdb | `pkg/api` `TestVectorSearch_NearestNeighbourCorrectness`; `pkg/storage` `TestVectorSearchForTenant_KnownAnswerOrdering` | #283 |
| CC3-adjacency-reopen | Edge adjacency survives a snapshot `Close()`→reopen under the default compression config | coi-screen, Stór | `pkg/storage` `TestEdgeAdjacencySurvivesReopen` | #287 |
| CC4-bulkimport-tenant-visible | Data written via the batch/bulk-import path is visible to every `*ForTenant` reader, in-memory and after reopen | coi-screen / import-icij | `pkg/storage` `TestBatchCommit_VisibleToForTenantReaders`, `TestBatchCommit_VisibleAfterReopen` | #288 |
| CC5-label-filtered-vector-search | `filter_labels` post-filters correctly on float-array-ingested vectors over REST | understand-graphdb, coi-screen | `pkg/api` `TestVectorSearch_RESTFloatArrayLabelFilter` | Q4 |
| CC6-batch-delete-tenant-index | Data deleted via the batch path leaves the per-tenant indexes + tenant counts (the delete-side sibling of CC4) | coi-screen / import-icij | `pkg/storage` `TestBatchDeleteNode_MaintainsTenantIndexAndCounts`, `TestBatchDeleteEdge_MaintainsTenantEdgeCount` | (batch-tenant-index follow-up) |
| CC7-batch-partial-echo | `POST /nodes/batch` returns only the nodes actually created (partial success), in unspecified order, echoing each node's properties so a client can reconcile assigned IDs to a correlation key (jailgraph's `_key`) | jailgraph | `pkg/api` `TestBatchNodes_PartialOutOfOrderEchoesProperties` | #319 |
| CC8-label-list-properties-paginated | `GET /nodes?label=` returns nodes with their properties and is followable to completion via the `X-Next-Cursor` header | jailgraph | `pkg/api` `TestNodesByLabel_ReturnsPropertiesAcrossPages` | #319 |
| CC9-traverse-outgoing-depth | `POST /traverse` returns the nodes reachable via outgoing edges within `max_depth` | jailgraph | `pkg/api` `TestTraverse_OutgoingNeighborsAtDepth` | #319 |
| CC10-unique-create-after-mmap-reopen | `CreateNodeWithUniquePropertyForTenant` still rejects a duplicate after an mmap reopen, without a prior enumeration call to build the lazy per-tenant index | graphdb-coord | `pkg/storage` `TestMmapStage2_UniqueConstraintSurvivesReopen` | #412 |
| CC11-unreadable-not-missing | A record the snapshot directory lists but cannot decode returns `ErrRecordUnreadable`, never `ErrNodeNotFound` | all readers | `pkg/storage` `TestDamagedRecordIsNotReportedAsMissing`, `TestCheckInvariantsReportsADamagedRecord` | #512 |
| CC12-existence-indistinguishable | A caller who does not own a resource sees an observable identical to one for an ID that never existed, intact or damaged | all REST consumers | `pkg/api` `TestCrossPrincipalEquivalence_IntactStore`, `TestCrossPrincipalEquivalence_UnderFault` | #513 |
| CC13-enumeration-reports-incompleteness | An enumeration that skips a record it cannot decode returns the records that DID decode beside a non-nil error wrapping `ErrRecordUnreadable`; a nil error means the enumeration was complete | all readers | `pkg/storage` `TestEnumerationsReportADamagedRecord`, `TestEnumerationsReportNilOnAnIntactStore` | ADR 0003 |
| CC14-paginated-list-serves-partial-page | A paginated list endpoint whose scan meets an undecodable record still returns 200, the records that DID decode, and a usable `X-Next-Cursor`, with `X-Enumeration-Incomplete` carrying the COUNT skipped (absent when the scan was complete, and never an ID) | all REST consumers | `pkg/api` `TestPaginatedListServesPartialPageWhenARecordIsDamaged`, `TestPaginatedListOmitsTheHeaderOnAnIntactStore` | ADR 0003 |

**CC7–CC9 are pre-emptive guards, not bug fixes.** CC1–CC6 were each written *red* against a real divergence found by driving the consumer. CC7–CC9 instead pin behaviours the jailgraph consumer already relies on (it works against graphdb as-is) so the in-flight storage-hardening wave can't silently change them — they pass against the code they ship with. They were teeth-proven by temporarily breaking the pinned behaviour (property echo, cursor pagination) and confirming the test fails. Origin: `../jailgraph/docs/GRAPHDB_CONTRACTS_HANDOFF.md`.

## Enforcement

`scripts/contract-guard.sh` checks that this file and the tests still describe
each other, and pins the body of every guarding test in
`docs/consumer-contracts.lock`. It runs in CI and from `make contract-guard`.

It checks four things, and each one has a case in
`scripts/contract-guard-selftest.sh` that makes it fail:

1. Every `CONSUMER CONTRACT:` comment carries a `CC<n>-<slug>` id.
2. Every row here is enforced by at least one tagged test.
3. Every tagged id has a row here.
4. Every guarding test this file names exists, and its body matches the lock.

Point 4 is the reason the lock file exists. The first three catch a test that is
deleted or renamed. Only the digest catches an assertion weakened in place,
which is the failure that reads like an ordinary diff. The guard does not
prevent that edit. It makes the edit change a tracked file, so it has to be
explained rather than merged unnoticed.

**CC10 is why the guard exists.** Its contract comment had been in
`pkg/storage/mmap_reopen_test.go` since #412 with no id and no row here. The
guard found it on its first run against the real tree, before it was wired into
anything.

## Growth rule

When driving a consumer surfaces a divergence, the fix lands with **(a)** a tagged contract
test (`// CONSUMER CONTRACT: <id> — <consumer> (<PR>)`) that fails against the pre-fix code,
and **(b)** a new row here. A contract is retired only when its consumer is. New contract
tests live in the package that owns the behaviour (storage invariant → `pkg/storage`; REST
invariant → `pkg/api`); there is no single consolidated suite because contracts span packages.

## High-fidelity drive

The tests above are in-process. To drive the *real* consumers end-to-end on demand (the check
that originally found these bugs), run `scripts/consumer-drive.sh` — it builds graphdb, runs
`coi-screen` (embedded) against a synthetic ICIJ corpus, and runs `understand-graphdb`'s
integration suite against a local server with a deterministic embedder. No external keys or
corpus needed. Promoting it to CI is blocked today (understand-graphdb has no remote;
coi-screen is private) — see the script header for prerequisites.

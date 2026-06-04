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
| CC7-batch-partial-echo | `POST /nodes/batch` returns only the nodes actually created (partial success), in unspecified order, echoing each node's properties so a client can reconcile assigned IDs to a correlation key (jailgraph's `_key`) | jailgraph | `pkg/api` `TestBatchNodes_PartialOutOfOrderEchoesProperties` | #NN |
| CC8-label-list-properties-paginated | `GET /nodes?label=` returns nodes with their properties and is followable to completion via the `X-Next-Cursor` header | jailgraph | `pkg/api` `TestNodesByLabel_ReturnsPropertiesAcrossPages` | #NN |
| CC9-traverse-outgoing-depth | `POST /traverse` returns the nodes reachable via outgoing edges within `max_depth` | jailgraph | `pkg/api` `TestTraverse_OutgoingNeighborsAtDepth` | #NN |

**CC7–CC9 are pre-emptive guards, not bug fixes.** CC1–CC6 were each written *red* against a real divergence found by driving the consumer. CC7–CC9 instead pin behaviours the jailgraph consumer already relies on (it works against graphdb as-is) so the in-flight storage-hardening wave can't silently change them — they pass against the code they ship with. They were teeth-proven by temporarily breaking the pinned behaviour (property echo, cursor pagination) and confirming the test fails. Origin: `../jailgraph/docs/GRAPHDB_CONTRACTS_HANDOFF.md`.

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

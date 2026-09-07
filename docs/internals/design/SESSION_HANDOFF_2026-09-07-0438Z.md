# Session handoff — 2026-09-07 04:38 UTC

**Date**: 2026-09-07 (single session, ~50 minutes of work; 1 code PR merged, 1 planning-doc PR open, plus this handoff)
**Outgoing model**: Claude Fable 5.1 (1M context) — `settings.json` carried `"model": "claude-fable-5-1[1m]"` at session start, verified by reading the file, so the tier policy's premise held for the whole session.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

`CheckInvariants` on the mmap representation now checks the adjacency lists, the vector
index and the property indexes, through three helpers shared with the shard path (#569).
That closes the live follow-on #474 left. The two open questions from the 01:46 UTC
handoff are still open; this session gathered the evidence for one of them (the backup
branch holds nothing `main` lacks) and recorded a recommendation for both.

## 2. What is done this session

| PR | Merge | Title | Notes |
|---|---|---|---|
| #569 | `8d33c4c` | feat(storage): check adjacency, vector and property indexes on the mmap path | Two commits. The three inline shard-path checks became `checkAdjacency`, `checkVectorCounts` and `checkPropertyIndexes`, each taking ground truth as an argument; the mmap path feeds them the raw-record set it already built. **The property indexes were never named in the gap** but had the same shape, so they went in. Seven teeth tests corrupt one structure each; all seven failed with `got []` against the pre-#569 checker (output in the PR body). `TestCheckInvariants_MmapCleanAfterOverlayWrites` now exercises an update and a delete on an mmap store against all three structures and found **no write-path defect**. A sonnet reviewer confirmed message-for-message behaviour preservation on the shard path and no new lock. All ten checks green; the two benchmark jobs took ~30 minutes with the PR alone on the account, consistent with the 28-minute solo baseline. |

## 3. Current state

- **`origin/main`**: `8d33c4c` (#569).
- **Open PRs**: #570, the single-file planning-doc update (`docs/NEXT_STEPS_2026-06-18.md`
  item 1 marked closed, two follow-ons recorded); its checks were in progress when this was
  written, three of ten green. Plus this handoff's PR. **Both were left for the user to
  merge**, per the `planning-doc-update` and `session-handoff` skills' "stop before merge".
- **Local branches**: `main`, `main-prerebase-backup` (see §7), `docs/planning-mmap-invariants`
  (#570), and this handoff's branch. The #569 branch went with `--delete-branch`.
- **Worktrees**: one.
- **Uncommitted changes**: none.
- **Gate state on `8d33c4c`**: `go build ./pkg/... ./cmd/...`, `go vet`, `gofmt -s -l`
  (empty), `golangci-lint` v2.13.2 (0 issues), `go test ./pkg/storage/ -short` (ok),
  `go test -race ./pkg/storage/ -run 'TestCheckInvariants|TestMetamorphic|Mmap' -count=2`
  (ok), `make contract-guard` (15 contracts, 23 guarding tests) — all clean.

## 4. What is next

1. **Merge #570 and this handoff** once green. Read conclusions by SHA
   (`gh api repos/dd0wney/graphdb/commits/<sha>/check-runs`), not from a watcher's exit code.
2. **ADR 0001, the uniqueness-rules registry** — the recommended next code track.
   `docs/adr/0001-uniqueness-rules-registry.md`, status `proposed`, design settled across two
   sessions, nothing built. It removes graphdb's last coord-specific knowledge (the
   `:Claim`/`for_task` special case in `pkg/graphql/mutations_resolvers.go`, mirrored in
   `pkg/api/handlers_nodes.go`). Three things before the first line of code: run
   `/orchestrate` (the global "Significant Features" rule); read the ADR's killed
   alternatives, two of which look obviously right; and treat the registry-write exemption
   as a named requirement, not an emergent property. It adds persisted state to the data
   directory, so the format-stability rule applies from the first commit.
3. **`CC15` consumer-contract row** (V1-spike D9) — waits on `oit-cyber/interrogate`
   shipping against `DistinctNodes`. External.
4. **Mmap `CheckInvariants` residue** — the count chains (`stats`, `tenantStats`) and the
   sticky global label/type keys behind `GetAllLabels` still have no mmap check. Each is a
   short check plus a teeth test. Not queued; #570 records it.

### Surfaced this session, recorded in #570 but not yet a track

- **The metamorphic driver can run on the mmap path.** `invariant_metamorphic_test.go`'s
  `newWALMDriver` stays on JSON for a reason that stopped being true at #474 (the comment
  was corrected in #569). The remaining reason is only that `crashRecoveryConfig` forces
  JSON. Running the metamorphic scripts on mmap would be the strongest oracle the mmap path
  has had.

### An observation, not a finding — probe before trusting

- `getEdgeIDsForNode` (`pkg/storage/storage_helpers.go`) consults `compressedOutgoing` /
  `compressedIncoming` **first** when `useEdgeCompression` is set, and returns that list
  without the overlay map or the mmap base. Read while writing #569, not investigated.
  The default config leaves compression off and mmap on, so the default deployment does
  not hit it; `crashRecoveryConfig` in the tests turns compression on with JSON. Whether
  `compressEdges` ever runs on an mmap-backed store is unknown. If it does, an mmap store
  with compression would serve stale adjacency. Cheap to settle with one test.

### Carried from the 01:46 UTC handoff, still true

- The `admissible` helper in `match_path.go` loads a node at the depth cap when a filter is
  set. One extra `GetNodeForTenant` per edge at the cap frontier, unbenchmarked.

## 5. Stale assumptions to retire

1. **`docs/NEXT_STEPS_2026-06-18.md`, "Open — need a decision or a track" item 1: "the
   vector index and the adjacency lists have no mmap ground truth."** — Closed by #569.
   #570 carries the edit; nothing to do once it merges.

2. **`CLAUDE.md` § "Snapshot format stability", the `CheckInvariants` parenthetical** —
   already corrected in #569 (it now names what the mmap path still does not check).
   Nothing to retire; recorded so the next reader does not re-edit it.

3. **`docs/internals/design/SQLITE_TESTING_SCORECARD.md` row 12: "Each of its five
   invariants has a test that corrupts a store in the matching way."** — Undercounted. The
   mmap path alone now has eleven teeth tests (four from #474, seven from #569). Row state
   (◐) is unchanged, so no PR was opened; update the evidence column the next time the row
   moves.

4. **`CLAUDE.md` § "Build, test, lint": "`pkg/storage` needs `-timeout 300s` (suite runs
   ~120-170s as of 2026-06)."** — Measured **28.1 s** on this box, `-short -count=1`,
   2026-09-07. One machine, one run; the number may be machine-bound. Keep the timeout,
   but do not plan around a three-minute wait.

5. **The 01:46 UTC handoff §5 item 8 ("a feedback draft is queued … do not send it
   as-is").** — Moot. Feedback drafts do not survive a session; this session had none
   queued. Retire the item.

6. **The `remember` hook's injected handoff** (the 2026-09-04 one about PR #555 and the
   govulncheck advisories) — fully stale. #555 merged 2026-09-04, and the advisories closed
   in #556 the same day. The hook itself says it is "pending replacement"; running
   `/remember` replaces it.

7. **Auto-memory**: nothing this session invalidated. `auto-mode-git-blocks-in-graphdb`
   held: `git checkout -b`, `git rebase`, `git push -u` and `gh pr merge --delete-branch`
   all ran without a refusal.

## 6. Open questions for the user

1. **The library-API decision** (carried from the 01:46 UTC handoff §6.1, unchanged).
   `CAPABILITIES` says a Go library import works but carries no stability guarantee.
   Declaring `PathOptions`, `Expansion`, `ExpandFilter` and `Executor.ExecuteWithOptions`
   supported and versioned is still open and purely additive. **Recommendation**: version
   the surface on the day `oit-cyber/interrogate` ships against it, in the same PR as the
   `CC15` contract row — a SemVer promise on a surface with zero external callers buys
   nothing and costs a constraint.

2. **`main-prerebase-backup`** (carried from §6.2). Evidence gathered this session: four
   commits from 2026-05-16, 330 commits behind `main`, and **all four are on `main` by
   content** — `DeleteAllNodes` repair (`68c193f`) and `DELETE /nodes` (`ae5c5d0`) by
   subject; `TypeFloatArray` vector indexing lives in `vectorFromProperty`
   (`pkg/storage/vector_operations.go`); the HNSW min-heap candidate set is `candidateQueue`
   in `pkg/vector/hnsw_types.go`. **Recommendation**: delete it
   (`git branch -D main-prerebase-backup`). Not done — it is a manual safety net someone
   made, so its removal is the user's call.

## 7. Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## 8. How to use this handoff

1. Read this first; §5 and §6 are the sections that save turns.
2. Then `docs/NEXT_STEPS_2026-06-18.md`, as amended by #564 and #570.
3. If picking up ADR 0001, read `docs/adr/0001-uniqueness-rules-registry.md` end to end,
   including "Note on how this design was produced", before `/orchestrate`.
4. If touching the invariant checker, `pkg/storage/invariants.go`'s header comment now
   states exactly what each path covers; trust it over any older doc.

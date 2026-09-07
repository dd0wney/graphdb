# Session handoff — 2026-09-07 01:46 UTC

**Date**: 2026-09-07 (single session; 1 PR merged, 4 opened and awaiting benchmarks)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The V1-spike shipped (#563): the variable-length pattern path now has an opt-in node-visited
mode and an expansion-time filter, carried per call by `PathOptions`. A code review on it found
six defects, including `MERGE` silently dropping the options, all fixed in the same PR. Four
follow-up PRs are open and clean. The biggest thing for the next session is §6: **two claims
carried across three handoffs turned out to be wrong**, and one of them cost nothing only
because nobody acted on it.

## 2. What is done this session

| PR | Title | Notes |
|---|---|---|
| #563 | feat(query): opt-in node-visited traversal and an expansion-time filter | Four commits. `DistinctNodes` collapses a 20-route fan graph from 20 rows / 41 adjacency reads to 1 row / 22 reads — the read count is the point, since deduplicating rows afterwards moves no cost. `ExpandFilter` prunes before admission; the test asserts what the filter was **offered**, not what came back. `/code-review high` then found six defects, fixed in a fourth commit (see below). Thirteen new tests, each proved a gate by a targeted mutation. |

**What the review on #563 caught**, because it is the most reusable lesson here:

`MergeStep.Execute` built its `ExecutionContext` by struct literal and lost `pathOpts`, so a
caller using `Expand` as a visibility rule got rows for nodes that rule excludes, and
`ON MATCH SET` then wrote to them. **Changing `newExecutionContext`'s signature does not make a
field impossible to forget — a struct literal bypasses the constructor.** That reasoning was
explicitly made and was wrong. `grep -rn "ExecutionContext{" pkg/` finds every such site; there
were two in production code.

The same site discarded `matchCtx.truncation`, a pre-existing defect: a `MERGE` whose match half
stopped at the depth cap reported a complete answer.

### Open PRs from this session

| PR | Title | Shape |
|---|---|---|
| #564 | docs(planning): mark the V1-spike deep-path traversal track done | `NEXT_STEPS_2026-06-18.md` §D only |
| #565 | docs: reconcile the library-API stability contradiction, add two pitfalls | `CAPABILITIES`, `STABILITY_POLICY`, `CLAUDE.md` |
| #566 | refactor(query): name the aggregation sub-context and state its absences | `pkg/query`, no behaviour change |
| #567 | test(storage): make the mmap corruption tests measure what they claim | `pkg/storage` tests only |

## 3. Current state

- **`origin/main`**: `f7cfcc0` (#563).
- **Open PRs**: #564, #565, #566, #567. **Zero failing checks on all four**; only the ~28-minute
  benchmark jobs were outstanding at write time. Benchmark duration is normal — historical runs
  on main are 28 minutes (07:51→08:19, 08:31→08:58, 06:49→07:17), so a benchmark still running
  after half an hour is not stuck.
- **Local branches**: `main`, `main-prerebase-backup`, and the four PR branches. **19 merged
  branches were deleted this session.**
- **Worktrees**: one. **All nine `.claude/worktrees/agent-*` worktrees were removed**, every one
  clean and on a merged branch.
- **Uncommitted changes**: none.
- **Gate state on `f7cfcc0` plus the four branches**: `go build`, `go vet`, `gofmt -s`,
  `golangci-lint` v2.13.2 (0 issues), `go test` across `pkg/query`, `pkg/api`, `pkg/graphql`,
  `pkg/algorithms`, `pkg/search`, `pkg/storage`, `pkg/wal`, `go test -race ./pkg/query -count=2`,
  and `make contract-guard` (15 contracts, 23 guarding tests) all clean.

## 4. What is next

1. **Merge #564–#567** once the benchmark jobs report. Read conclusions by SHA
   (`gh api repos/dd0wney/graphdb/commits/<sha>/check-runs`), not from a watcher's exit code —
   two background watchers were killed mid-wait this session and a killed watcher looks
   identical to a finished one.
2. **`CC15` consumer-contract row** (V1-spike D9), on the day `oit-cyber/interrogate` ships
   against `DistinctNodes`. The assertion to pin is that the zero `PathOptions` is unchanged.
   Waits on something external to this repo.
3. **`CheckInvariants` on the mmap path** still has no ground truth for the vector index or the
   adjacency lists (`NEXT_STEPS_2026-06-18.md` §C).
4. **ADR 0001, the uniqueness-rules registry.** Design stable, nothing built. It removes
   graphdb's last coord-specific knowledge — the `:Claim`/`for_task` special condition in
   `pkg/graphql/mutations_resolvers.go`.

### Surfaced this session, not yet on the planning doc

- The `admissible` helper in `match_path.go` loads a node at the depth cap when a filter is set,
  to decide whether the answer is genuinely incomplete. That is one extra `GetNodeForTenant` per
  edge at the cap frontier, only when `Expand` is non-nil and only under an unbounded pattern.
  It has not been benchmarked. Nobody has complained; it is recorded so the next person does not
  discover it by surprise.

## 5. Stale assumptions to retire

**Two of these were carried across three handoffs without being re-examined, and both turned out
to be wrong.** That is the pattern worth taking from this section, more than any individual row.

1. **`pkg/wal/durable_rename_test.go` is blind to the rename it covers.** — **FALSE. Delete this
   item.** The test reads the operation trace, asserts the rename is present, and asserts the
   parent directory is opened, synced and closed after it, across five scenarios. Proved a real
   gate: wrapping `vfs.SyncParentDir` in `pkg/wal/fileutil.go:151` in `if false` makes
   `TestFileRotatorRotate_SyncsParentDirectory` fail with *"the parent directory is not opened,
   synced and closed"*. Claimed in the 08-31 handoff, repeated in the 09-04 handoff §4.4,
   never checked.

2. **`docs/CAPABILITIES_2026-05-10.md:175` contradicts `docs/STABILITY_POLICY.md:46`.** — Was
   true. Fixed in #565: `CAPABILITIES` now says the Go library import works but is unversioned,
   and the policy names the library-only symbols from #563 explicitly. **The decision was made
   in the reversible direction** — see §6.

3. **`computeCRC` covers directories and metadata only; detection of record corruption is
   unmeasured.** — The first half is true and deliberate. The second half was true and is now
   measured (#567), with a **positive** result: record damage leaves the snapshot openable
   (correct — hashing records at open would spend the cheap reopen the format exists for), the
   damaged record reports `property bag at offset N: record unreadable`, and every other record
   still decodes. Damage is detected **and contained**.

4. **`TestMmapReaderRejectsACorruptSnapshotFromTheDriver` flips a byte at the fixture midpoint.**
   — Was true. Measured: the file is 1226 bytes, the midpoint 613 sits past the edge directory,
   and node records occupy [148, 280), so it passed by accident of fixture size. Fixed in #567 to
   take its byte from the node-directory offset in the header.

5. **The 09-04 handoff §4.1 defects F-a to F-d.** — All closed by #560, #561 and #562, which
   merged *after* that handoff was written. V1-spike decisions D7 and D8 were therefore answered
   by merged work rather than by a choice.

6. **Auto-memory `auto-mode-git-blocks-in-graphdb`: "the classifier refuses `git worktree
   remove`".** — Corrected in-session. Nine worktrees were removed with zero refusals, followed
   by 19 `git branch -D` with zero refusals. The 09-04 refusal was of a *compound* command that
   chained removal with merges. **Attempt the simple form before reporting a block** — this note
   was used for three days as a reason not to clean up.

7. **`NEXT_STEPS_2026-06-18.md` §D V1-spike line numbers.** — Wrong for the third time. #564
   marks the row done, keeps the historical numbers with an explicit "do not navigate by these",
   and puts current anchors beside them.

8. **A feedback draft is queued and its central evidence is unsound. Do not send it as-is.** It
   cites `memory.events` counters as proof that two harness task-kills were unjustified. Those
   counters read zero for **every** cgroup on this machine — with `memory.max: max` they cannot
   increment — so they cannot report the opposite and prove nothing. `SendFeedback`'s own rule
   forbids re-drafting the same issue in one session, so it was flagged rather than replaced.

## 6. Open questions for the user

1. **The library-API decision (#565) went the reversible way.** `CAPABILITIES` was corrected to
   match the policy: a Go library import works but carries no stability guarantee. The
   alternative — declaring the library surface supported and versioned, which would put
   `PathOptions`, `Expansion`, `ExpandFilter` and `Executor.ExecuteWithOptions` under SemVer —
   is still open and is purely additive. `oit-cyber/interrogate` is the consumer affected.
2. **`main-prerebase-backup`** survived the cleanup. It has no PR and looks like a manual safety
   net from a past rebase. Keep or delete?
3. **`CLAUDE.md` says the main loop runs Fable, "pinned via `model` in `~/.claude/settings.json`".
   There is no `model` key in `settings.json` or `settings.local.json`** — verified by parsing
   the file and listing its nine keys as a positive control. This session ran Opus 5. Either the
   pin was dropped deliberately, in which case that clause should go, or it was lost.

## 7. Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## 8. How to use this handoff

1. Read this first, then §5 in particular — it is the section that saves turns.
2. Then `docs/NEXT_STEPS_2026-06-18.md`, as amended by #564.
3. If touching `pkg/query`'s traversal, read
   `docs/internals/design/SPIKE_V1_DEEP_PATH_TRAVERSAL_2026-09-04.md` §3, §4 and §7. Its status
   block records the two places the build differs from the design, including that test A-7 gates
   the parent-chain rebuild and **not** the BFS ordering its name suggests.

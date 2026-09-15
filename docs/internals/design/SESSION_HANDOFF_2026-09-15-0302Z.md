# Session handoff — 2026-09-15 03:02 UTC

**Date**: 2026-09-15 (single session, 11:30–17:05 AEST; five PRs merged, two open, one of them another session's)
**Outgoing model**: Claude Fable 5.1 for the first hour, then Claude Opus 5 (1M context) after the user downgraded the plan from Max x20 to Pro and asked for efficiency
**Delegation**: four subagents. A sonnet `Explore` mapped the WAL sync path and its report was used unchanged. A sonnet `general-purpose` shipped the consumer-drive SKIP policy end to end; the main loop reviewed the diff and re-ran the selftest before the PR. A second sonnet `general-purpose` got most of the way through the WAL poison work and then **died with HTTP 403 `oauth_org_not_allowed` mid-run** (the plan change); the main loop took over its uncommitted storage half, wrote the changelog, and ran every gate. A haiku `Explore` mapped the Python client and its report was used unchanged. **After the 403 the main loop did all implementation itself.** The tier policy still held where it could: haiku for the mechanical map, sonnet for execution, main loop for design calls and review.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The WAL durability arc is closed at both ends: a failed `fsync` poisons the WAL (#624), and every client SDK surfaces the resulting 202 applied-not-durable write (Go #618 last session, TypeScript #625 open, Python #626). The local consumer gate no longer exits 0 on a missing consumer under CI (#623).

**Then the last task on the queue turned out to be a live silent-data-loss defect, not the doc decision it was seeded as.** A WAL backend switch dropped acknowledged writes with no error (#629). Read §5's first bullet before trusting any statement about what `ErrWALBehindSnapshot` covers.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #623 | `chore(scripts)`: fail consumer-drive on a SKIP when CI is set | `82a9f61`. coord `graphdb:v1.4-consumer-drive-skip-policy`. `make consumer-drive-selftest` drives the SKIP path on purpose against empty fixture dirs; `CONSUMER_DRIVE_SCRIPT` is the negative control, `CONSUMER_DRIVE_SKIP_BUILD=1` keeps the selftest from building graphdb. `shellcheck` is not installed on this host; `bash -n` was the fallback. |
| #624 | `fix(wal,storage)`: poison the WAL after a sync failure | `efb2964`. coord `graphdb:v1.4-wal-sync-failure-rollback`. Closes the residual #619 recorded. Four append paths refuse with `ErrWALPoisoned`; `Snapshot()` truncates a poisoned WAL as `Close` already did. Gates read from real output: wal 5.6 s, storage `-short` 29.9 s, `-race -count=3` 18.2 s + 147.1 s, `gofmt -s` empty, golangci-lint v2.13.2 zero issues, contract-guard OK. |
| #626 | `feat(python-client)`: surface a 202 applied-not-durable write | `d2ecb1a`. coord `graphdb:v1.4-python-client-202-not-durable`. Mirrors #625's shape exactly. ruff clean, mypy clean over 40 files, pytest 178 passed / 2 skipped (the 2 are the pre-existing live-server integration tests). |

| #627 | `docs(planning)`: mark the four 2026-09-15 tasks done | `5ffdede`. Closes the residual sentence #619 left in the track-D row; adds a row for the 202 work across the three client SDKs and one for #623. |
| #629 | `fix(storage)`: refuse the open when a different WAL backend last wrote to the data directory | **Open at handoff, gates all green locally.** coord `graphdb:v1.4-wal-backend-switch-policy`. See §5. |

Also open, not mine: **#625** (the TypeScript client, `graphdb-coord-8b`'s work — do not merge or release it).

## 3. Current state

- `origin/main` HEAD: `5ffdede` (#627).
- **Open PRs**: #629 (WAL backend-switch refusal, mine, all local gates green, CI running at handoff). #625 (TypeScript client, owned by session `graphdb-coord-8b` — **do not merge or release it; that session owns the claim**).
- **Open branches**: `v1.4/v1.4-wal-backend-switch-policy` (#629), `v1.4/v1.4-ts-client-202-not-durable` (the peer's), plus this handoff branch. Every other task worktree is removed and its branch deleted.
- **Uncommitted changes**: none.
- Coord (graphdb): 40 tasks — 1 cancelled, 1 in-progress (`v1.4-ts-client-202-not-durable`, the peer's), `v1.4-wal-backend-switch-policy` claimed by this session and **awaiting release until #629 merges**, the rest done. **No graphdb task is pending.**
- Lessons recorded on release: #623, #624, #626. #629's lesson goes in when it merges.

## 4. What's next

1. **Merge #629, then `coord release --pr 629 graphdb:v1.4-wal-backend-switch-policy` with a lesson.** The claim is still open under agent id `graphdb-3d`; nothing else closes it.
2. **Benchmark CI step budget (new, not seeded, not on the planning doc).** See §5 — a real finding with a latent failure in it.
3. **Mark #629 done in the planning doc.** The track-D row does not mention the backend-switch defect at all; #627 landed before it was found.
4. **A drain path for a switched backend (new, not seeded).** #629 refuses and tells the operator to reopen with the previous setting and close cleanly. That works but is manual. The user chose the refusal alone over a built-in drain; revisit only if an operator asks.
5. **Go client versioning** — carried from the 2026-09-14 handoff, still unresolved: `clients/go` has no tag scheme while Python and TS do.
6. Off-path, unchanged: mmap clean-check cost (`buildMmapMetadata` clones every property index per `Close`), coi-screen M1 steps 3–5, GraphQL damage signal (ADR 0003), onboarding docs, the junk-test sweep.

## 5. Stale assumptions to retire

- **THE BIG ONE. Anything saying `ErrWALBehindSnapshot` catches a WAL backend switch is FALSE, and the code itself said so until #629.** Its doc comment in `pkg/storage/errors.go` and its error text in `pkg/storage/compact_wal.go` both listed a backend switch as one of three causes they refuse. They never caught the ordinary case. The plain and batched backends write `wal.log`, the compressed backend writes `wal_compressed.log`; flipping `StorageConfig.EnableCompression` reads a *different* file, so on a first switch that file does not exist, the recovered LSN is 0, and `guardWALNotBehindSnapshot` returns nil on the zero branch. The previous backend's file then sits on disk holding un-snapshotted writes that replay never sees. **A probe lost two acknowledged writes with the open reporting success.** #629 adds `ErrWALBackendSwitched`, refusing when the other backend's file still holds bytes, and corrects both texts. The lesson: this was seeded as a documentation decision ("document the refusal or add a migration") on the strength of that comment. Probing the premise before doing the work is what found it.
- **The 2026-09-14 handoff §4 item 3 and the #619 lesson both describe the backend switch as a cause the guard refuses.** Corrected as above.
- **`docs/NEXT_STEPS_2026-06-18.md`, the long track-D row (line ~85), sentence "Known residual: a sync failure does not roll back by design, so the boundary can exceed the durable LSN by the number of sync failures and a later open can refuse."** → Closed by #624. **PR #627 already rewrites this sentence**; if #627 merged, nothing to do.
- **coord lesson `lesson-1789366280-fd6dbc` (from #619)** says "A sync failure still does not roll back by design ... so the boundary can exceed the durable LSN by the number of sync failures." → Still true about the *LSN*, now false about the *consequence*. The poison caps the overshoot at one and `Snapshot()` truncates, so the boundary can no longer outrun the file. The #624 lesson (`lesson-1789440393-ec3c00`) carries the corrected version.
- **Anything claiming the Python or TypeScript client treats a 202 as an ordinary success** — false since #626 and #625.
- **Anything claiming `clients/python/README.md` repeats the TypeScript README's wrong "retries every 5xx" sentence** — it does not. Lines 135–137 already exclude non-idempotent methods and name the list. Verified by reading the file, not inferred from the TS fix.
- **The 2026-09-14 handoff §4 item 1 (sync-failure residual), item 2 (TS/Python 202)** — both closed. Item 3 (backend switch) and item 4 (Go client versioning) remain.

### New gap found this session, for the next planning checkpoint

**`.github/workflows/benchmark.yml` step "Run Unit Test Benchmarks" has no time budget and is on track to start failing.** Evidence gathered 2026-09-15 ~02:33Z:

- The step runs `go test -bench=. -benchmem -benchtime=5s ./pkg/storage ./pkg/lsm ./pkg/wal` with **no `-timeout` flag**, so Go's **10-minute default applies per package**.
- Benchmark function counts: **pkg/storage 96**, pkg/lsm 14, pkg/wal 1. Sub-benchmarks multiply that. At 5 s each plus setup, pkg/storage alone approaches the default.
- Two independent `main` runs and both PR runs were simultaneously stuck on this same step, one for 23 minutes. **Using `main` as the control is what proved it belongs to no PR's change.**
- **It is NOT runner contention**: every job got a runner within 3–5 s of run creation (`startedAt` minus `createdAt`).
- **The `timeout-minutes: 20` at line 62 is STEP-scoped to "Run Capacity Test (5M nodes)", not the job.** A peer session read it as a job-level bound and concluded a stuck job fails rather than waits. It does not.
- When this step does blow the default, it will fail as `test timed out after 10m0s` and **read like a code defect rather than a time budget**. A `concurrency` group — the obvious-looking fix — would not help, because queueing was never the problem.

Not seeded. It is shared CI and needs its own task and the user's approval.

## 6. Open questions for the user

- The benchmark step budget in §5: seed a task, or accept the slow step until it breaks? **Still open.**
- Carried and still unanswered from 2026-09-14: the `clients/go` tag scheme.

Answered this session, recorded so they are not re-asked:

- WAL sync failure → **poison the WAL** (the user chose this over recording a separate durable LSN). Shipped in #624.
- consumer-drive SKIP → **warn locally, fail when `CI` is set**. Shipped in #623.
- WAL backend switch → **refuse the open**, no built-in migration, and **correct the wrong comment in the same PR**. Shipped in #629.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this, then `docs/NEXT_STEPS_2026-06-18.md` (the track-D row carries the whole WAL arc, #598 → #624; it does NOT yet mention #629).
2. Merge #629 on green, then `git checkout main && git pull --ff-only`, then release its coord task.
3. **Do not touch #625 or `graphdb:v1.4-ts-client-202-not-durable`.** Session `graphdb-coord-8b` holds that claim. Message it before any coord write that is not a claim or release of your own task.
4. Run `make test-local` for the local package gate. Use `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./pkg/... ./cmd/...` for CI's exact linter.

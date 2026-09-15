# Session handoff — 2026-09-15 03:02 UTC

**Date**: 2026-09-15 (single session, 11:30–13:02 AEST; three PRs merged, two open, one of them another session's)
**Outgoing model**: Claude Fable 5.1 for the first hour, then Claude Opus 5 (1M context) after the user downgraded the plan from Max x20 to Pro and asked for efficiency
**Delegation**: four subagents. A sonnet `Explore` mapped the WAL sync path and its report was used unchanged. A sonnet `general-purpose` shipped the consumer-drive SKIP policy end to end; the main loop reviewed the diff and re-ran the selftest before the PR. A second sonnet `general-purpose` got most of the way through the WAL poison work and then **died with HTTP 403 `oauth_org_not_allowed` mid-run** (the plan change); the main loop took over its uncommitted storage half, wrote the changelog, and ran every gate. A haiku `Explore` mapped the Python client and its report was used unchanged. **After the 403 the main loop did all implementation itself.** The tier policy still held where it could: haiku for the mechanical map, sonnet for execution, main loop for design calls and review.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The WAL durability arc is now closed at both ends: a failed `fsync` poisons the WAL (#624), and every client SDK surfaces the resulting 202 applied-not-durable write (Go #618 last session, TypeScript #625 open, Python #626). The local consumer gate no longer exits 0 on a missing consumer under CI (#623).

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #623 | `chore(scripts)`: fail consumer-drive on a SKIP when CI is set | `82a9f61`. coord `graphdb:v1.4-consumer-drive-skip-policy`. `make consumer-drive-selftest` drives the SKIP path on purpose against empty fixture dirs; `CONSUMER_DRIVE_SCRIPT` is the negative control, `CONSUMER_DRIVE_SKIP_BUILD=1` keeps the selftest from building graphdb. `shellcheck` is not installed on this host; `bash -n` was the fallback. |
| #624 | `fix(wal,storage)`: poison the WAL after a sync failure | `efb2964`. coord `graphdb:v1.4-wal-sync-failure-rollback`. Closes the residual #619 recorded. Four append paths refuse with `ErrWALPoisoned`; `Snapshot()` truncates a poisoned WAL as `Close` already did. Gates read from real output: wal 5.6 s, storage `-short` 29.9 s, `-race -count=3` 18.2 s + 147.1 s, `gofmt -s` empty, golangci-lint v2.13.2 zero issues, contract-guard OK. |
| #626 | `feat(python-client)`: surface a 202 applied-not-durable write | `d2ecb1a`. coord `graphdb:v1.4-python-client-202-not-durable`. Mirrors #625's shape exactly. ruff clean, mypy clean over 40 files, pytest 178 passed / 2 skipped (the 2 are the pre-existing live-server integration tests). |

Also opened, not merged: **#627** (planning-doc update, this session's) and **#625** (the TypeScript client, `graphdb-coord-8b`'s work, not mine to merge or release).

## 3. Current state

- `origin/main` HEAD: `d2ecb1a` (#626).
- **Open PRs**: #627 (planning doc, single-file, CI running at handoff). #625 (TypeScript client, owned by session `graphdb-coord-8b` — **do not merge or release it; that session owns the claim**).
- **Open branches**: `docs/planning-2026-09-15-seeds` (#627), `v1.4/v1.4-ts-client-202-not-durable` (the peer's), plus this handoff branch. All my task worktrees are removed and their branches deleted.
- **Uncommitted changes**: none.
- Coord (graphdb): 40 tasks — 37 done, 1 cancelled, 1 in-progress (`v1.4-ts-client-202-not-durable`, the peer's), 1 pending (`v1.4-wal-backend-switch-policy`, unclaimed).
- Three lessons recorded on release, one per closed task.

## 4. What's next

1. **`graphdb:v1.4-wal-backend-switch-policy`** — the only pending graphdb task, seeded this session, unclaimed. #619 made the open refuse when a WAL backend switch leaves the other backend's file in place. Decide whether that refusal is the documented contract or a migration path is needed.
2. **Benchmark CI step budget (new, not seeded, not on the planning doc).** See §6 — this is a real finding with a latent failure in it.
3. **Go client versioning** — carried from the 2026-09-14 handoff, still unresolved: `clients/go` has no tag scheme while Python and TS do.
4. Off-path, unchanged: mmap clean-check cost (`buildMmapMetadata` clones every property index per `Close`), coi-screen M1 steps 3–5, GraphQL damage signal (ADR 0003), onboarding docs, the junk-test sweep.

## 5. Stale assumptions to retire

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

- The benchmark step budget above: seed a task, or accept the slow step until it breaks?
- `graphdb:v1.4-wal-backend-switch-policy` is pending and unclaimed. Document the refusal as the contract, or build a migration?
- Carried and still unanswered from 2026-09-14: the `clients/go` tag scheme.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this, then `docs/NEXT_STEPS_2026-06-18.md` (the track-D row carries the whole WAL arc, #598 → #624).
2. If #627 is open, merge it on green; then `git checkout main && git pull --ff-only`.
3. **Do not touch #625 or `graphdb:v1.4-ts-client-202-not-durable`.** Session `graphdb-coord-8b` holds that claim. Message it before any coord write that is not a claim or release of your own task.
4. Run `make test-local` for the local package gate. Use `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./pkg/... ./cmd/...` for CI's exact linter.

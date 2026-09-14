# Session handoff — 2026-09-14 06:12 UTC

**Date**: 2026-09-14 (one session in two stages, 09:51–16:15 AEST; supersedes `SESSION_HANDOFF_2026-09-14-0202Z.md`, which covers the first stage; nine PRs merged, one open)
**Outgoing model**: Claude Fable 5.1
**Delegation**: the user asked to "leverage all models efficiently" and to contact the other agent. Stage 1 (see the 02:02Z handoff): two sonnet Explore code maps, a sonnet tester, two sonnet implementers, two opus reviewers, a sonnet STE worker. Stage 2, after the user said "proceed": three sonnet `general-purpose` implementers ran in parallel from three main-loop specifications, one worktree each (guard, Go client, netns gate); two opus `reviewer` agents (guard, Go client) gave GO with findings, all applied by the same implementers; the netns change had no opus review (scripts only, a selftest with a negative control, CI green); a sonnet `worker` rewrote the guard PR body to zero STE findings. The main loop wrote the specifications, made the policy call on the guard (refuse), reviewed every diff, ran the race gates, rejected nothing in stage 2. Peer session `graphdb-coord-6f` seeded the three tasks (#615) after its user approved, verified two seed candidates against main, and was told before the claims and after every release.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The WAL boundary work is closed end to end: the snapshot records the boundary (#611), the API tells clients not to retry (#612), the Go client obeys (#618), and a downgrade or a damaged WAL refuses the open instead of dropping writes (#619). Two pre-existing WAL rollback defects were found and fixed on the way. The local test gate now runs inside a private network namespace (#617). graphdb has **0 pending, 0 claimed** coord tasks.

## 2. What's done this session

Stage 1 (02:02Z handoff): #610, #611, #612, #613, #614.

| PR | Title | Notes |
|---|---|---|
| #615 | chore(coord): seed the netns gate, go-client retry and downgrade guard tasks | Opened and merged by the coord session after its user approved. |
| #616 | docs(claude-md): name the WALBoundaryLSN field and the monotonic WAL LSN | `33b865c`. The paragraph the 02:02Z handoff listed as due. |
| #617 | chore(scripts): run the local Go test gate inside a private netns | `ffe43fd`. `make test-local`, `make netns-selftest` (four checks, one negative control), `scripts/lib/netns.sh` copied from graphdb-coord with the origin commit in the header. Full short gate 80 s inside the namespace. The host did not reproduce the 136 s oidc hang today; the wrapper stands on the isolation proof. `/sys/class/net` cannot show the isolation, `ip -o link show` can. |
| #618 | fix(go-client): do not retry POST or PATCH on a 5xx, return ErrNotDurable on a 202 | `3854fa7`. M-11 parity (the user did not choose between POST-only and POST+PATCH; parity with TS/Python was the default). Value-plus-error return on a 202. The module records no version; `CHANGELOG.md` carries the entries. C1 and C3 seen red. Opus review GO with five findings applied. |
| #619 | fix(storage,wal): refuse the open when the recovered WAL LSN is below the snapshot boundary; batched and flush rollbacks | `04caa8a`. `ErrWALBehindSnapshot` when `0 < recovered < boundary`; error names both numbers and three causes (downgrade, damage to synced entries, a WAL backend switch). G1 seen red on four backend rows. **Two pre-existing defects fixed red-first**: a flush failure did not roll the LSN back (only a write failure did), and `BatchedWAL` subtracted the whole batch length on a mid-batch failure (counter below durable, or underflow). A sync failure still does not roll back by design. Race ×3 clean on the final tree. Opus review GO on condition of two changes, both applied. |
| #620 | docs(planning): mark the three 2026-09-14 seeds done | **Open at handoff**, docs only, merge on green. |

## 3. Current state

- `origin/main` HEAD: `04caa8a` (#619), plus #620 when it merges.
- Open PRs: #620 (planning doc, CI running at handoff).
- Open local branches besides `main`: `docs/planning-three-seeds-done` (#620) and this handoff branch. All task worktrees removed, task branches deleted.
- Uncommitted changes: none.
- Test/lint state on `04caa8a`: `-race -count=3` storage 147 s and wal 18 s ok (run on the #619 tree before merge); golangci-lint v2.13.2 zero issues; `gofmt -s` empty; `make contract-guard` OK (16 contracts, 24 guarding tests); `clients/go` tests, vet, gofmt clean; CI green on every PR before merge.
- Coord (graphdb): 35 tasks, 32 done, 1 cancelled, 0 pending, 0 claimed. Six lessons recorded today.
- Budget window: 39 % at the start of stage 2.

## 4. What's next

No seeded task remains. Candidates for the next planning checkpoint, none seeded:

1. **Sync-failure rollback residual** (from #619). A WAL sync failure leaves the LSN counter advanced while the bytes may not be durable, so the recorded boundary can exceed the durable LSN and a later open can refuse with `ErrWALBehindSnapshot`. A design is needed that neither reuses an LSN nor lets the boundary run ahead (for example: mark the WAL as poisoned after a sync failure and force a snapshot before Close, or record the last synced LSN separately). Red first: force a sync failure after a successful flush, close, reopen.
2. **TS and Python clients on a 202**: they treat the applied-not-durable body as success and drop `applied`/`durable`. Same shape as #618 for each client.
3. **WAL backend switch on one data directory** (#619's third cause): the open refuses now, which is loud, but a repair or migration path does not exist. Decide whether to document the refusal as the contract or add a migration.
4. **Go client versioning**: the module has no tag scheme (`clients/go/vX.Y.Z`); Python and TS have one. Decide and tag.
5. Off-path, unchanged: mmap clean-check cost (`buildMmapMetadata` clones every property index per Close), consumer-drive SKIP policy, coi-screen M1 steps 3–5, GraphQL damage signal (ADR 0003), onboarding docs, the junk-test sweep.

## 5. Stale assumptions to retire

- The 02:02Z handoff §5 said the `CLAUDE.md` boundary paragraph was due; #616 added it. Nothing left to do there.
- Anything that says a failed WAL flush leaves the LSN counter advanced, or that `BatchedWAL` rolls back by subtracting the batch length. Both are fixed in #619 (`pkg/wal/wal.go` Append, `AppendBatchAtomic`, `compressed_wal_io.go`, `batched_wal.go`).
- Anything that says `recovered == boundary` is the only non-refusing equality case, or that the guard has two causes. Three causes; the backend switch is the third.
- The Go client "retries every 5xx" (a comment in `pkg/api/server_helpers.go` said so; corrected in #618).
- The auto-memory `localhost-connect-timeouts-are-portmaster` says "retry alone, then move on". The better answer now is `make test-local`, which removes Portmaster from the path.
- The 02:02Z handoff §6 open question on the torn-tail policy is resolved: refuse, with both numbers and three causes in the error text. The PATCH/PUT question is resolved by M-11 parity (PUT retried, PATCH not).

## 6. Open questions for the user

- The sync-failure residual (item 1 above): accept as documented, or seed a task.
- consumer-drive SKIP: fail or warn? Carried since 2026-09-13.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first, then `docs/NEXT_STEPS_2026-06-18.md` (the track D row under the Close no-rewrite item carries the whole WAL arc, #598 through #619).
2. If #620 is still open, merge on green, then `git checkout main && git pull --ff-only`.
3. No coord task is pending. Either seed from §4 with the coord session (message it first; it owns the seed file), or pick from §4 item 5.
4. Run `make test-local` for the local gate from now on; `make netns-selftest` if the namespace path is in doubt.

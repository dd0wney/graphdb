# Session handoff — 2026-08-29 02:56 UTC

**Date**: 2026-08-29 (single long session; 4 PRs merged, 2 substantial PRs left in flight)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

ADR 0002 stage 4 is done and sitting in #507 — `pkg/storage` and all three WAL
flavours are on the `pkg/vfs` driver, and `syscall.Mmap` has left `pkg/storage`
entirely. Along the way the session found **six defects, four of them in the
instruments rather than in the system**, including two in code the session had
itself just written and two in the measures that were supposed to catch them.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #503 | Three documents that stopped being correct | Opened by the previous session; merged here. DCCC measure exists, JSON rollback loses data, planning doc two releases behind |
| #504 | Refuse a JSON-mode open that would serve an empty database | The code repair for #503's warning. **Also exposed a crash-recovery test that had passed for nearly two months on an ID collision** — the fix to production code is what audited the test |
| #505 | Four claims in this repository that are not correct | `CLAUDE.md`'s clean-checkout claim, the ✅ DONE marker reading as a release, a README opening that said nothing, and the 2026-08-28 handoff's opening line |
| #506 | The second snapshot-mode defect now has a home in the queue | The both-snapshots-present case, § E of the planning doc |

### In flight, not merged

| PR | Commits | State |
|---|---|---|
| #507 | 8 | ADR 0002 stage 4 + the DCCC floor + two mmap reader fixes + the OOM sweep. CI re-running from the last push |
| #508 | 5 | The public write-up `docs/one-signal-two-worlds.html` + README link. **CLEAN, 10/10 SUCCESS** |

## Current state

- **`origin/main`**: `30f6234`
- **Open PRs**: #507 (UNSTABLE = 5 checks still running, none failed), #508 (CLEAN, ready)
- **Branches**: `main`, `feat/storage-vfs-driver` (#507), `docs/one-signal-two-worlds` (#508), `main-prerebase-backup` (pre-existing, not this session's)
- **Uncommitted**: none
- **Gates on the #507 branch**: `pkg/storage` 28.0s ok, `pkg/wal` 5.5s ok, `pkg/vfs` + `vfstest` ok, `pkg/alloc` + `alloctest` ok, `-race` clean on the snapshot and mmap paths, `gofmt -s` clean, `go vet` clean, `contract-guard` OK (10 contracts, 13 tests), `dccc-selftest` 7/7, `dccc` 576/628 exit 0

**Merging #508 publishes a public page.** `docs/` deploys to GitHub Pages on any
push to `main` touching `docs/**`. The page goes live at
`https://dd0wney.github.io/graphdb/one-signal-two-worlds.html`, and the README
already links it. That is intended, but it is a one-way action.

## What's next

The previous handoff's item 1 (ADR 0002 stage 4) is done, which **unblocks its
item 2**: concurrent fault injection for `pkg/storage` was explicitly waiting on
the driver, and the driver now reaches the snapshot path, the load path and the
WAL.

Ranked:

1. **Absent versus refused.** `getNode` returns `(nil, false)` for both "no such
   node" and "could not allocate one". The OOM sweep measured 1 of 3 nodes
   vanishing under fail-once and 2 of 3 under fail-all-from. Under memory
   pressure a query silently returns an incomplete result. The fix is a reason
   code instead of a bool across 8 internal call sites, reaching the public
   error boundary — a contract change that deserves its own PR.
2. **Concurrent fault injection for `pkg/storage`** (previous handoff item 2,
   now unblocked). Its 28 concurrent test files still inject nothing. The role
   classifier cannot be derived from paths there, so `pkg/vfs` needs a declared
   role, added additively.
3. **Snapshot-mode stale selection** — planning doc § E. A directory holding
   both snapshots serves the older one silently. Needs an on-disk format
   version bump, because no ordering marker exists.
4. **Scorecard row 4's remainder** — LSM blocks and query results are still
   ungated for OOM. `recordCursor.blob` is done.
5. **Scorecard row 8, boundary values** — the last ✗ worth attacking. Go has no
   `testcase()` equivalent, but a `boundary.Case(id, cond)` registry that
   asserts every declared boundary was reached is buildable and arguably
   stronger, because it is explicit rather than inferred from coverage.
6. **A DCCC coverage threshold**, which still needs a CI measurement. The
   statement-count floor is separate and already enforced.
7. **The mutation survivors in `sweep_role.go`** — `n > maxOps` and the
   `maxOps <= 0` default are documented failure paths no test reaches.

### New gaps this session surfaced

- **`enterprise-plugins/` is broken but out of scope here.** Both plugins import
  the pre-rename path `github.com/dd0wney/cluso-graphdb/...`, neither builds,
  and 16 files are not `gofmt -s` clean. The path is gitignored, so the repair
  belongs in `dd0wney/graphdb-enterprise` and cannot be committed to this repo.
- **`COVER_PKGS` and `couplings.tsv` can drift.** #507 adds `DCCC_PKGS` and
  `make dccc-cover`, but nothing mechanically keeps the two lists in step. A
  registry row in a package absent from the profile reports UNMATCHED.

## Stale assumptions to retire

1. **`docs/internals/design/NEXT_SESSION_PROMPT.md` says "Start ADR 0002 stage
   4".** Done, in #507. This handoff overwrites it.
2. **The user's auto-memory `mmap-json-mode-switch-loses-data`** says "the
   documented JSON rollback opens an empty store". Half-corrected by #504: that
   open now **returns an error naming `snapshot.mmap`**. The remaining hazard is
   different — a directory holding *both* snapshots serves the older one. The
   memory should be rewritten to the second case, not deleted.
3. **ADR 0002's call-site counts** (259 total, 179 for `pkg/storage`, 67 for
   `pkg/wal`) are inflated roughly 20x and mis-sized stage 4 as a track. The
   production surface was 8 filesystem operations. Corrected in place by #507;
   the correction is not on `main` until it merges.
4. **`COUPLING_AND_INTERFERENCE.md`'s DCCC baseline of 563/616** is superseded
   by 576/628 over 17 sites. Also in #507, also not yet on `main`.
5. **Scorecard rows 4, 5 and 11** were updated in #507. Row 11 goes ◐ → ✓; rows
   4 and 5 gain content and stay ◐. Tally on merge: 6 ✓ / 7 ◐ / 4 ✗.
6. **`docs/NEXT_STEPS_2026-06-18.md` is the current planning doc and is now
   partly stale again** — it does not know about stage 4, the DCCC re-measure,
   or the two mmap reader fixes. A `planning-doc-update` pass is due once #507
   merges.
7. **The 2026-08-28 handoff's opening line** was corrected by #505 and now
   carries a dated note. Do not re-quote "nine defects, five in the
   instruments" — it is 13 defects (9 coupling, 1 intra-component, 3 process or
   test-contract), and separately 5 existing tests found to be decoration.

## Open questions for the user

1. **Merge order for #507 and #508.** #508 is green and ready; #507 is still
   running. They touch disjoint files, so either order works.
2. **The `[paste the article URL]` placeholder** in `one-signal-two-worlds.html`
   footnote 1 is deliberate — the LinkedIn URL for "The Edge of Reason" is not
   known to this session and was not invented. It ships visible unless filled.
3. **No Exhibit D.** The write-up carries Exhibits A–C. The user's own article
   style closes on a deadpan image; none was chosen on his behalf.
4. **Carried over, unresolved from the previous handoff**: the ADR 0001 appendix
   (a peer session has its user's approval, not this user's), and whether
   `declare` should take an API key holding `write` or admin.

## Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## How to use this handoff

1. Read this, then check whether #507 and #508 merged.
2. Then `docs/NEXT_STEPS_2026-06-18.md` § E and § "Current state".
3. Then `CLAUDE.md` § "Orient first".
4. If picking up item 1, read `pkg/storage/mmap_oom_test.go` first — it measures
   the defect and explains why it was recorded rather than rushed.
5. **Before trusting any gate here, run its self-test.** Two of this session's
   six defects were in measures that reported success while measuring the wrong
   thing.

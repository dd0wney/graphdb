# Session handoff — 2026-09-11 04:31 UTC

**Date**: 2026-09-11 (single session, ~1.5 hours; 2 PRs merged; one coord task claimed, shipped and released; first session run under a coord task budget)
**Outgoing model**: Claude Fable 5.1 (1M context) — the main loop designed, ruled on both review rounds, and wrote the code itself (the diff was small enough that a worker dispatch would have cost more than it saved).
**Delegation**: 4 review dispatches, all sonnet. Round one: `reviewer` (REQUEST CHANGES: ADR 0003 window, `walkPages` discarding rows on late damage, a coercion nit) and `performance` (NO-GO: the `where` page loop re-sorts the full ID set per round). Both used — the loop was removed and the window was tested. Round two: `performance` GO, `reviewer` REQUEST CHANGES on the ADR comment wording only — used as-is, comment and doc rewritten, two test cases added. No implementer dispatch.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The GraphQL half of index-level pagination shipped (#585): every list field in the production limits schema takes `after: ID`, unfiltered pages cost O(limit), and `offset` keeps its meaning so graphdb-coord's offset walk still works. The coord daemon on :8090 already runs that build and the coord session started moving the coord client to the cursor.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #585 | feat(graphql): ID `after` cursor and index-level paging for list fields | Cursor = last item's ID, same contract as REST `X-Next-Cursor`; short page ends the walk. Unfiltered → one storage page call. `where` → materialise + `sort.Search` seek (a page loop would re-sort the full ID set per round; review round one caught it). `after` refuses `orderBy`, non-zero `offset` and a non-numeric cursor. ADR 0003: page-only path's damage window is the page scan; the page that meets damage refuses because a GraphQL field cannot carry a partial list beside an error. Six tests; four red with `Unknown argument "after"`, the window test red against main with the whole-set damage error. Legacy schemas (`edges_schema.go`, `filtering_schema.go`) untouched. |
| #586 | docs(planning): mark GraphQL index-level pagination done | Two-line diff in `NEXT_STEPS_2026-06-18.md` §D and `ROADMAP_post_1.0.md` v1.4.0. |

Coord: `graphdb:v1.4-graphql-index-pagination` claimed on branch checkout (hook), released with `--pr 585` and a lesson (`lesson-1789100983-327aac`).

## 3. Current state

- `origin/main` HEAD: `7d6c06d` (#586). `main` locally matches, tree clean.
- Open PRs: **#582** `docs: mark ADR 0001 accepted and refresh the next-session prompt for v1.4.0` (opened 2026-09-08, 10 checks green, `mergeable: UNKNOWN` at handoff time — GitHub had not recomputed after #585/#586; re-read before merging). Not this session's work; its local branch `docs/adr-0001-accepted` still exists.
- Open branches: `main`, `docs/adr-0001-accepted` (belongs to #582). The task branch and the planning branch were deleted at merge.
- Uncommitted changes: none.
- Test/lint state at #585: `go build`/`go vet` on `./pkg/... ./cmd/...` clean; full `-short` suite across `pkg` and `cmd` green; `golangci-lint` v2.13.2 0 issues on the module; `gofmt -s -l` empty; `make contract-guard` OK (16 contracts, 24 guarding tests). CI: all 11 checks green including both benchmark jobs (~29 min each, normal).
- Coord daemon on :8090: restarted by the coord session at 14:30 AEST on the #585 build (`coord-daemon.service`, both uniqueness rules registered). It probed `after` live and confirmed the refusal of `after` with `offset`.

## 4. What's next

`coord next` gives **`graphdb:v1.4-f3-compliance-http-api`** (branch `v1.4/v1.4-f3-compliance-http-api`; checkout claims it). It is the last blocker of `graphdb:v1.4-sdk-parity`, whose other dependency (#585) is now done. Scope F3 narrowly: the framework exists in `pkg/compliance`, only the HTTP surface is missing (`CLAUDE.md` § Known pitfalls; design in `docs/internals/design/F3_COMPLIANCE_API_DESIGN.md`).

Off-path options unchanged from `NEXT_STEPS_2026-06-18.md` §D: real-corpus coi-screen (`graphdb:v1.1-coi-screen-real-corpus`), onboarding docs, CI hygiene.

### Gaps surfaced this session (not yet on the planning doc)

- **GraphQL has no `X-Enumeration-Incomplete` equivalent.** On the page-only path the page that meets a damaged record refuses; REST serves the partial page plus the header. ADR 0003's "Consequences" text (`docs/adr/0003-enumeration-error-returns.md` ~line 291) groups `pkg/graphql` with the partial-page answer, which the code does not yet deliver. Candidate: an `extensions` entry on the GraphQL response, or a `pageInfo`-style wrapper. Noted in `pkg/graphql/limits.go` (index-level path comment) and `docs/API.md` § GraphQL List Pagination.
- **A predicate-aware storage page method** (`NodesByLabelPageForTenant` with a `keep` func) would let `where` + `after` go index-level too. Rejected for #585 because it changes the `pkg/storage` public API; worth its own task if `where` pages on large tenants show up in profiles.
- **Storage page methods re-sort the full ID set on every call** (`membership_index.go` `sortedBucketIDs`). REST accepted this in #366; a cached sorted view per bucket would make every page O(log n + limit). Performance, not correctness.
- **coord client follow-up** lives in graphdb-coord (the coord session owns it): move `listNodesPage`/`listEdgesPage` to `after`, and teach the fake daemon in coord's tests the `after` rule incl. the refusal of `offset` with `after`.

## 5. Stale assumptions to retire

- `docs/ROADMAP_post_1.0.md` v1.3 says the gofmt CI gate is "not started"; #490 added the `gofmt -s -l ./pkg ./cmd` step to `test.yml`. Carried over from the 2026-09-10 handoff; still not fixed.
- Auto-memory / last handoff: the coord session is **`graphdb-coord-a0`**, not `graphdb-coord-e7`. `ListAgents` is the source of truth each session.
- `docs/adr/0003-enumeration-error-returns.md` "Consequences": "`pkg/api` and `pkg/graphql` can give an honest answer … for the paginated endpoints that answer is the page plus `X-Enumeration-Incomplete`" — true for `pkg/api` only. GraphQL paginated fields refuse the page that meets damage (#585). Either amend the ADR or ship the GraphQL signal (§4 gap 1).
- `docs/API.md` line ~247 still shows `GET /nodes?…&offset=0` and line ~875 says "Always specify limit and offset for large datasets" — REST paginates by cursor since #366 and GraphQL since #585. The new § "GraphQL List Pagination" is correct; the older lines are not.
- Coord budget: a claimed task carries a dollar budget and the coord PreToolUse hook refuses **every** tool (Bash, Read, SendMessage, and `coord budget grant` itself) once it is spent. Only the user can grant, with `coord budget grant task:<project>:<id> <usd>` (the `task:` prefix is mandatory). #585 needed three grants ($10 → $15 → $25); subagent reviews and the whole-module lint are the expensive holds. Memory note `coord-budget-hook-blocks-all-tools` records this.

## 6. Open questions for the user

- **GraphQL damage signal**: amend ADR 0003 to say GraphQL refuses, or add the signal? The refusal is loud (repo objective) but a client walking a big tenant loses the page and has no cursor past the damaged record.
- **PR #582** has sat open since 2026-09-08 with green checks. Merge it, or is it superseded by the 2026-09-10 reconciliation (#583)? Its next-session-prompt content is now overwritten by this handoff either way.
- **Budget sizing**: should a feature task default to more than $10? This one cost ~$25 with two review rounds and full preflight.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first.
2. `ListAgents` → message `graphdb-coord-a0` before any coord write other than your own claim/release. Ask whether the coord client cursor follow-up has landed before you touch `pkg/graphql` list arguments again.
3. Then `docs/NEXT_STEPS_2026-06-18.md` §D and `docs/internals/design/F3_COMPLIANCE_API_DESIGN.md` if picking up F3.
4. Ask the user for a budget grant up front (`coord budget grant task:graphdb:v1.4-f3-compliance-http-api <usd>`) — the hook blocks everything once it is spent, including the request for more.

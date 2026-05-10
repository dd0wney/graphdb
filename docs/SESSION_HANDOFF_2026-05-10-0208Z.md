# Session handoff — 2026-05-10

**Date**: 2026-05-10 (single session, ~9 PRs merged + 1 in flight at write time)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The audit-track concurrency work is complete (A4 nodes + A4-edges; race-clean under `-count=3` across both). The planning doc was updated to mark the closures, and a cross-repo capabilities audit + a productization-gaps survey + a project-level `CLAUDE.md` were added. **The critical-path next is A8.2** (replica `/nodes` cross-tenant dump fix); off-path-parallel options are H2 and the unbuilt enterprise plugins.

## What's done this session

In merge order (oldest first):

| PR | Title | Notes |
|---|---|---|
| #65 | `chore: delete deprecated zmq replication variant (H1) + planning checkpoint` | Removed `pkg/replication/zmq_*.go` (5 files) + `cmd/graphdb-zmq-{primary,replica}/`; introduced `docs/NEXT_STEPS_2026-05-10.md`. |
| #66 | `ci: add tagged-build-nng job (H1 follow-up)` | CI matrix row exercising `go build -tags nng ./...`. |
| #67 | `feat(storage): A4 shard partition + clone-elision (correctness reframe)` | Partitioned `gs.nodes` → `[256]map`; per-shard lock-grain; clone-elision in vector post-filter. **Throughput criterion reframed**: 1.02× at 4 readers, not the audit's projected 2-4×; the value is structural correctness, not throughput. |
| #68 | `docs(planning): close A4 + H1, surface A4-edges follow-up` | Updated `NEXT_STEPS_2026-05-10.md` to mark A4 + H1 done, surfaced A4-edges as the next critical-path item. |
| #69 | `docs(planning): mark H3 done (local branch cleanup)` | Force-deleted 21 stale local branches whose PRs were squash-merged. |
| #70 | `feat(storage): A4-edges — partition gs.edges + lock-grain (correctness reframe)` | Same shape as A4 for edges. Three-bullet in-code race note collapsed to one (surfaces 2 + 3 turned out to be moot — Commit() holds gs.mu around apply* calls). |
| #71 | `docs(planning): add known-limitations + productization-gaps section` | Surfaced ~10 gaps. **Two were over-broad** — corrected by #72. |
| #72 | `docs(planning): add CAPABILITIES_2026-05-10.md (cross-repo audit)` | Catalogued OSS + enterprise repo with maturity tags. Found `pkg/cluster` (2.8k LOC) and `pkg/compliance` (with GDPR/SOC2 controls) and reconciled the gaps section's overstatements. |
| #73 | `docs: add CLAUDE.md project guide for agent iteration` | Repo-level instructions for future Claude Code sessions — orientation order, open-core split, idioms, infra patterns, pitfalls. |

## Current state

- **`origin/main` HEAD**: `8b93c7a` (PR #73). At time of handoff PR open, `origin/main` will move to the handoff PR's merge commit.
- **Open PRs**: this handoff PR is the only one open after merging.
- **Open branches**: just `main` (this handoff branch deletes on merge).
- **Uncommitted changes**: none on `main`.
- **Test/lint state**: race-clean under `-count=3` for `pkg/storage/`; `go vet ./...` and `golangci-lint run ./...` both clean.

## What's next (from `docs/NEXT_STEPS_2026-05-10.md`, post-A4-edges)

Critical-path queue, in order:

1. **A8.2** — replica `/nodes` GET unauth'd cross-tenant dump. Single PR, ~50-100 LOC, audit-regression row. Security finding mitigated-in-depth by `GRAPHDB_LEGACY_BINARY` gate, but deserves a real fix.
2. **F1.1-spike** — design doc for per-tenant LSA. Decides whether F1.1-impl is worth doing.
3. **F1.1-impl** (if spike says go) — adapt `pkg/search/tenant_indexes.go` pattern to `pkg/search/lsa.go`.
4. **F3** — Compliance API HTTP surface. `pkg/compliance/` framework already exists (per #72's audit); F3 is just `pkg/api/handlers_compliance.go` tying it to customer-callable endpoints.
5. **A8.1** — replication binary cleanup spike + impl. Decides delete-vs-rebuild on `cmd/server`.
6. **S1** — storage interface extraction spike with binary go/no-go. Output is the input to the **next** planning checkpoint.

Off-path parallel:
- **H2** — `requireAdmin` consolidation (~12 sites, 50-100 LOC).

Net-new from #71 + #72 (not on the 90-day plan, surfaced for next-checkpoint scoping):
- **The 4 unbuilt enterprise plugins** (`cloudflare-vectorize`, `cdc`, `multi-region-replication`, `saml-oidc-auth`) — credibility gap; either build or scrub the docs that promise them.
- **OpenTelemetry tracing** (in OSS core, since metrics are paid-tier).
- **Python SDK** as the first non-TypeScript first-party client.
- **Helm chart** as the first IaC primitive.
- **Commercial-offering decision** (founder-led).

## Stale assumptions to retire

For the user's auto-memory and for the planning doc:

1. **`feedback_planning_checkpoints.md`** says "after ~3-5 PRs in a session, offer a planning checkpoint." This session merged 9 PRs across two distinct stages — the threshold worked (planning checkpoints were offered at 3, 5, 7) but the user repeatedly chose to keep going. Memory entry stays valid; just note that the user may override.

2. **`project_zmq_build_broken.md`** says "H1 in NEXT_STEPS_2026-05-10 schedules deletion, not repair" — H1 is now **done**, not scheduled. Memory entry should update to "H1 closed via PRs #65 + #66 (2026-05-10)."

3. **`project_ci_red_state_tolerated.md`** said "May 2026 test-flake roster fixed (PRs #58-#62); remaining red on PRs is almost always Linux runner-cancellation or benchmark comment-step permissions." This claim is **still valid** and was confirmed across PRs #65-#73. CLAUDE.md (#73) now documents it canonically, so the memory entry could be retired or shortened to point at CLAUDE.md.

4. **`docs/NEXT_STEPS_2026-05-10.md` "Known limitations + productization gaps" section** (added in #71, lines roughly 217-251) has two over-broad claims now corrected by `docs/CAPABILITIES_2026-05-10.md`:
   - "Single-node assumption baked in" → should be "no sharded write path; `pkg/cluster/` substrate exists."
   - "No production-grade observability narrative beyond `pkg/metrics`" → should acknowledge `prometheus-metrics` is shipped in the enterprise repo; the OSS-side gap is **OpenTelemetry tracing** specifically.
   - Reconciliation note suggested but **not yet written** — left as an open task (see "Open questions" below).

## Open questions for the user

1. **Reconciliation note for `NEXT_STEPS_2026-05-10.md` productization-gaps section.** I raised this after merging #72. The user did not direct it; it remains a small docs PR worth doing eventually but not urgent. If the next session opens with this question, the answer is probably "yes, single-line correction note pointing readers to `CAPABILITIES_2026-05-10.md`."
2. **The 4 unbuilt enterprise plugins** (`cloudflare-vectorize`, `cdc`, `multi-region-replication`, `saml-oidc-auth`). Per #72's recommendation, decide between (a) build them on a stated timeline, (b) scrub the OSS docs that name them. Currently the worst of both worlds.
3. **Commercial-offering decision** (per #71's gaps section + #72's recommendation). Founder-led, not technical-track. Pricing, support model, SLA, OSS-vs-paid tier definitions. Shapes which other gaps are urgent.

## How to use this handoff

1. Read this file first.
2. Then read `docs/NEXT_STEPS_2026-05-10.md` — the planning doc the next session works from.
3. Then read `CLAUDE.md` § "Orient first" if you haven't already (but if you're a Claude Code agent in this repo, you've already been auto-loaded).
4. If picking up A8.2 (recommended), check the audit reference in `docs/AUDIT_security_2026-05-06.md` for the original finding, plus `pkg/api/audit_regression_test.go` for the test-row pattern other A* tasks used.

This handoff goes stale on the next substantive session. When that happens, write a new `docs/SESSION_HANDOFF_<DATE>.md` and don't bother updating this one.

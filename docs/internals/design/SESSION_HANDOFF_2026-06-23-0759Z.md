# Session handoff — 2026-06-23 07:59 UTC

**Date**: 2026-06-23 (single continuous session spanning 06-22→06-23; 10 PRs merged, one major + one minor release cut)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Shipped **v0.8.0** (hot backup/restore + verify/restore tooling, signed) and then drove the project to its **first GA: v1.0.0** — single-node, production-hardened, GPG-signed. Repaired the release-signing pipeline end-to-end (it had silently red-failed every release), published the signing key, wrote the API/format **stability policy** (the last 1.0 gate), backfilled the CHANGELOG, added onboarding docs, committed a **post-1.0 roadmap** (`docs/ROADMAP_post_1.0.md`, v1.1→v2.0), and **seeded the v1.1 tracks into coord**.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #429 | feat(v0.8.0): hot backup + verifiable, safe restore | ROADMAP B4. Adds `POST /admin/backup`, per-file manifest integrity (versioned, trailer), `pkg/backup` leaf pkg, `graphdb-admin backup verify`/`restore` (zip-slip + snapshot-mode guards), backup metrics, honest admin-UI. gosec G110 caught in review → bounded `io.CopyN`. |
| #430 | fix(release): repair GPG signing | **Found the release workflow had red-failed every release**: `Import GPG key` called `gpg --quick-add-uid` with one arg (needs two) + unguarded on empty secret. Removed the broken line, guarded on `env.GPG_PRIVATE_KEY != ''`. |
| #431 | docs: publish release signing public key | Root `KEYS` + `docs/RELEASE_SIGNING.md`. |
| #432 | ci(release): auto-attach release public key | Signing step now exports `graphdb-release-pubkey.asc` into `dist/` so every release publishes it. |
| #433 | docs(changelog): backfill 0.4.0–0.8.0 | CHANGELOG jumped `[0.6.0]→[0.3.0]`. Reconstructed 0.4.0/0.4.1/0.5.0 from git history; added 0.7.0 (folded into v0.8.0 tag) + 0.8.0. `[Unreleased]` left as-is pending human attribution. |
| #434 | docs(b5b): API & format stability policy | New `docs/STABILITY_POLICY.md` — the v1.0 gate. + ROADMAP/NEXT_STEPS reconciliation. |
| #435 | docs: Getting Started + README fixes | New `docs/GETTING_STARTED.md`. **README quickstart `docker run` was missing the required `JWT_SECRET`** (server fail-closes → command never started); fixed. Corrected stale `v0.4.1`→v0.8.0 and removed Windows binary claims (dropped in #421). |
| #436 | release: declare v1.0.0 GA | CHANGELOG `[1.0.0]`, ROADMAP GA, README. Tag `v1.0.0` cut on the merge commit. |
| #437 | docs(roadmap): post-1.0 arc | `docs/ROADMAP_post_1.0.md` — v1.1→v1.9→v2.0, all additive minors + the v2.0 breaking line. |
| #438 | chore(coord): seed v1.1 tracks | `coord/seed/graphdb.json`; applied live (coord project `graphdb`, 4 v1.1 tasks pending). |

**Releases cut**: `v0.8.0` (signed, 2026-06-22) and `v1.0.0` (signed GA, 2026-06-23). Both verified end-to-end: all artifacts carry `.asc` signatures + published pubkey; v1.0.0's Release workflow was **fully green** (first time the in-workflow Docker job ran instead of being skipped).

## Current state

- **`origin/main` HEAD**: `4be1cb2c` (#438) — *authoritative, per GitHub API*.
- **Open PRs**: none. **Open branches (remote)**: none (— `--delete-branch` discipline held).
- **Local checkout caveat**: the git-over-HTTPS read replica was persistently lagging (~serving `170d93b`/#436) and **SSH-over-1Password-agent auth was failing intermittently** all session — so all PRs were created/merged via the `gh` REST API, and tags via the git-refs API. Local `main` may show a stale tip and an untracked `coord/` (that file is the committed #438 seed; the checkout is just behind). A plain `git fetch origin` once the replica catches up clears both. **Nothing is actually uncommitted.**
- **Stale local branch**: `main-prerebase-backup` exists locally (pre-existing, not from this session) — candidate for cleanup.
- **Release pipeline**: signing now works. Key = ed25519, fpr `7E6B65441BFFCB61AE8C77D5E7C513EB926B660B`, UID `GraphDB Release <noreply@graphdb.io>`; private key + passphrase in **1Password** (account `my.1password.com`, Private vault, item "GraphDB Release GPG Key (CI signing)") and in repo Actions secrets `GPG_PRIVATE_KEY`/`GPG_PASSPHRASE`. Public key in `KEYS`, release assets, and keys.openpgp.org.
- **coord**: daemon was down; brought up via `graphdb-coord/scripts/coord-bootstrap.sh` (idempotent; reuses `~/.graphdb-coord-key`). Project `graphdb` now exists with 4 pending v1.1 tasks.

## What's next

The path to v1.0 is **complete**. The forward queue is `docs/ROADMAP_post_1.0.md`. Immediate actionable work is **v1.1 "Validate & observe"**, seeded in coord (`coord status --track v1.1`):

1. `graphdb:v1.1-coi-screen-validation` — **linchpin** (gates B-1 → v1.2 mmap-default + v1.5 perf). **Blocked on a local ~814K ICIJ corpus** (the same blocker that deferred it pre-1.0 — confirm corpus availability first).
2. `graphdb:v1.1-mmap-oracle-property-based` — no external dep; immediately actionable.
3. `graphdb:v1.1-otel-tracing` — no external dep; immediately actionable.
4. `graphdb:v1.1-reenable-fuzz-tests` — no external dep; cheap.

**New gaps surfaced this session (not yet on a planning doc):**
- A fresh dated `NEXT_STEPS_2026-06-23.md` checkpoint should be written (the live planning doc is still `NEXT_STEPS_2026-06-18.md` with a 06-23 reconciliation note appended — a new checkpoint reflecting "v1.0 GA shipped, v1.1 seeded" is cleaner).
- CHANGELOG `[Unreleased]` block needs a human attribution pass (flagged in #433 — items are mostly EXPERIMENTAL cluster/replication groundwork).

## Stale assumptions to retire

- **`docs/ROADMAP_v1.md`** — fully superseded; it's now *history*. All blockers B1–B6 closed, B5b done, GA declared+tagged. The live forward doc is **`docs/ROADMAP_post_1.0.md`**.
- **`docs/NEXT_STEPS_2026-06-18.md`** "Path to v1.0.0" / "Recommended next track" → **stale**: v1.0.0 GA shipped 2026-06-23. (A 06-23 reconciliation note was appended in #434, but the body still reads as pre-GA.)
- **Auto-memory `release-gpg-step-unconfigured.md`** — already updated in-session to reflect the #430 fix + configured key; it is now accurate (do not re-flag releases as "going red on the GPG step").
- **"Latest release is v0.6.0"** (anywhere it appears) → **v1.0.0**. Tags now: v0.6.0 → v0.8.0 → **v1.0.0**. Note **there is intentionally no `v0.7.0` tag** — 0.7.0's hardening shipped to main and was folded into the v0.8.0 tag (documented in CHANGELOG).
- **README** is now accurate (quickstart needs `JWT_SECRET`; Linux/macOS only, no Windows) — don't "re-fix" those.

## Open questions for the user

1. **CHANGELOG `[Unreleased]` attribution** — should the cluster/replication/OIDC items there be attributed to specific past versions, or do they stay genuinely unreleased (they relate to the EXPERIMENTAL, not-wired `pkg/cluster`)? Needs a human call.
2. **v1.1 sequencing** — the roadmap recommends *validation-first*, but `v1.1-coi-screen-validation` needs a local corpus that isn't available. Proceed with the corpus-free v1.1 tasks (oracle/OTel/fuzz) first, or source the corpus?

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## How to use this handoff

1. Read this first.
2. Then `docs/ROADMAP_post_1.0.md` (forward queue) — NOT `ROADMAP_v1.md` (history).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If picking up v1.1 coord work: `coord status --track v1.1`, then `coord claim graphdb:<task>` (the `coord-claim` skill). The coord daemon must be running — `bash ../graphdb-coord/scripts/coord-bootstrap.sh` from the graphdb repo if `coord status` errors.

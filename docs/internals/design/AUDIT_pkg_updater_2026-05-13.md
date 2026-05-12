# Addendum to AUDIT_gemini_track_claims_2026-05-13.md — pkg/updater/ deep-dive

This addendum upgrades the 2026-05-13 audit's row for the Software-update
track from "🟡 PARTIAL (needs to be verified against actual code)" to a
concrete verdict, in response to the user's question about
`cmd/graphdb-upgrade`'s fate (handoff §7 q2).

## Files audited

| File | LOC | Source |
|---|---|---|
| `pkg/updater/types.go` | 31 | `stash@{0}` untracked tree |
| `pkg/updater/updater.go` | 171 | `stash@{0}` untracked tree |
| `pkg/updater/updater_test.go` | 29 | `stash@{0}` untracked tree |
| `cmd/graphdb-admin/update.go` | 80 | `stash@{0}` untracked tree |
| `pkg/api/handlers_update.go` | 99 | `stash@{0}` untracked tree |
| `pkg/api/handlers_update_test.go` | 42 | `stash@{0}` untracked tree |
| `docs/internals/design/SOFTWARE_UPDATES.md` | (design intent) | `stash@{0}` untracked tree |

All code is on `origin/archive/gemini-bulk-2026-05-13` for permanent access.

## Verdict: 🔴 NOT LANDABLE AS-IS

The architecture is reasonable for a single-node-by-design world
(manifest → download → checksum → atomic-swap → restart-via-orchestrator),
but the implementation has **two critical security/correctness bugs** that
land *as no-ops you'd never notice in casual testing*:

### Issue 1: `VerifyChecksum` is unwired (security)

`pkg/updater/updater.go` defines a `VerifyChecksum(filePath, expectedChecksum)`
function that SHA256-hashes a file and compares to a manifest-provided
checksum. It is a **public, callable function with zero callers** in the
package. `DownloadRelease` does not call it. `ApplyUpdate` does not call it.

`Asset.Checksum` is parsed out of the manifest JSON (`pkg/updater/types.go:21`)
but the parsed value is never validated against the downloaded binary.

Impact: a malicious or tampered release manifest can point to any binary URL.
The updater will download it, atomically swap it into place as the running
executable, and the user has no way to detect tampering. The existence of
the unwired `VerifyChecksum` function is *worse than no checksum* — code
reviewers see it and assume tampering is detected, when it isn't.

### Issue 2: version comparison is broken (correctness)

```go
// pkg/updater/updater.go:148
func isVersionNewer(current, latest string) bool {
    // Remove 'v' prefix if present
    current = strings.TrimPrefix(current, "v")
    latest = strings.TrimPrefix(latest, "v")
    if current == "dev" || current == "" { return true }
    // Basic string comparison for spike (in real app, use semver library)
    return latest > current
}
```

String comparison fails the canonical case: `"0.9.0" > "0.10.0"` is `true`
in Go (because `'9' > '1'` lexicographically). A user running v0.9.0 with
v0.10.0 released would be told "you're up to date" forever.

**The author tagged this as a spike.** It is. The test (`TestIsVersionNewer`,
the *only* test in the package) tests cases that happen to work
lexicographically and does not cover the breaking case.

### Issue 3: `currentVersion` is hardcoded in both wrappers

```go
// cmd/graphdb-admin/update.go:27
currentVersion := "v1.0.0" // This should be injected at build time

// pkg/api/handlers_update.go:26
currentVersion := "v1.0.0" // Should be injected at build time
```

Every running instance reports itself as v1.0.0. Effect: a running v1.5.0
instance will compare itself to manifest's latest (e.g., v1.1.0), find it
"newer" (incorrectly, per Issue 2 — or *correctly*, by the simple length-
ordering case), and offer to **downgrade** itself. The build-time injection
is not wired up; comments acknowledge but don't fix.

### Issue 4: 0% test coverage on real update paths

`pkg/updater/updater_test.go` is 29 lines. Tests only `isVersionNewer`.
There are no tests for `CheckForUpdates`, `DownloadRelease`, `ApplyUpdate`,
or `VerifyChecksum` (the unwired one). `pkg/api/handlers_update_test.go`
is 42 lines — needs reading to verify whether HTTP handlers are tested
at all.

### Issue 5: background goroutine in HTTP apply path is fire-and-forget

`pkg/api/handlers_update.go:handleUpdateApply` starts the download+apply
in `go func()`, returns 202 Accepted, and the goroutine reports errors
via `fmt.Printf` to stderr. There is no status endpoint, no observable
outcome, no progress tracking. The handler also calls `os.Exit(0)` on
success to trigger an orchestrator-mediated restart — which works under
systemd/docker but kills the process silently if running standalone.

## What this means for `cmd/graphdb-upgrade`

The existing `cmd/graphdb-upgrade` is multi-node orchestrated upgrade
machinery (per `docs/internals/design/AUTOMATED_UPGRADES.md` — blue-green,
node promotion/demotion, coordinated failover). After A8.1's "single-node
by design" landing, this machinery is **over-engineered for what the
project actually ships**.

Gemini's `pkg/updater/` was the proposed replacement: simpler, single-node,
manifest-driven. But it has Issues 1-5 above. Three honest paths:

### Path A — Keep cmd/graphdb-upgrade, defer the replacement story (Recommended)

Acknowledge that `cmd/graphdb-upgrade` is dead code relative to A8.1, but
*do not delete it* without a working replacement. The dead code is
inert — it doesn't run unless invoked — so the cost of keeping it is
documentation confusion, not runtime risk. Add a deprecation note to its
top-of-file comment pointing to this audit.

### Path B — Delete cmd/graphdb-upgrade AND accept "users handle updates via container/package manager"

Most production graphdb users run via Docker (`docker pull` is their
updater), Kubernetes (image rolls), or systemd-on-VM (apt/yum). For those
users, an in-process updater is a curiosity, not a requirement. Delete
the orchestrated upgrade machinery, document the operator-driven update
model, and ship no in-process updater until/unless one is designed
properly from first principles.

### Path C — Land a fixed pkg/updater/ as a redesign

Fix the five issues above:
1. Wire `VerifyChecksum` into `DownloadRelease` / `ApplyUpdate`.
2. Replace `isVersionNewer` with a real semver comparison
   (`github.com/Masterminds/semver/v3` or equivalent).
3. Wire build-time version injection via `-ldflags '-X main.Version=...'`.
4. Add real tests for `CheckForUpdates`, `DownloadRelease`, `ApplyUpdate`
   (httptest server + tempdir for the swap). Add `VerifyChecksum` tests
   covering both pass and fail cases.
5. Replace the fire-and-forget goroutine with a status-tracked update job
   (job ID, status endpoint, progress).

Path C is roughly 1–2 days of focused work plus careful security review.

## Recommendation

**Path A**. The replacement story isn't ready. Keeping
`cmd/graphdb-upgrade` as dead-but-marked code is the lowest-risk choice
until either Path B (delete entirely) or Path C (proper redesign) becomes
the user's priority. Path C should not be cherry-picked from
`stash@{0}` — the five issues are deeply enough wired that fixing them
piecemeal would leave subtle bugs. Better to redesign from first
principles with this audit as the threat model.

## Outcome (recorded 2026-05-13)

User selected **Path C** — full redesign. This audit doc serves as the
threat model for the redesign work. Subsequent PRs will:

1. Replace `pkg/updater/{types,updater}.go` with a properly-engineered
   updater using `golang.org/x/mod/semver` for comparison, wired
   `VerifyChecksum`, and `-ldflags`-injected version strings.
2. Replace `cmd/graphdb-admin/update.go` and `pkg/api/handlers_update.go`
   with versions that pass real tests covering the happy path, not just
   manifest-unreachable failure paths.
3. Delete `cmd/graphdb-upgrade/main.go` (multi-node orchestration, now
   over-engineered post-A8.1) only AFTER the new updater is proven.

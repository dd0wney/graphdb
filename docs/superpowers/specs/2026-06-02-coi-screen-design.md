# Design: COI Screen — conflict-of-interest screening on graphdb

**Date:** 2026-06-02
**Status:** Design — pending user review
**Topic:** A conflict-of-interest (COI) screening tool built as a consumer of graphdb.

---

## 1. Problem & framing

A conflict of interest is, structurally, a **graph anomaly**: a short path that should
not exist between two parties meant to be at arm's length, running through a
relationship that confers *interest* (ownership, family, employment, prior
collaboration, intermediation).

The tool answers one question for the MVP: *given two (or more) named parties that are
supposed to be independent, is there a hidden chain of interest-bearing relationships
connecting them?* It returns a **ranked list of candidate conflicts** — each with its
evidential path, the provenance of every edge on that path, and a confidence score.

### Non-negotiable stance: candidates, never verdicts

A COI flag is an *accusation*. The tool surfaces candidates for human review; it does
not adjudicate. Two consequences are baked into the core data model from day one:

1. **Provenance per edge.** Every edge records which dataset/record/filing it came from.
   A path is only evidence if every hop is sourceable.
2. **Reversible, confidence-scored entity resolution.** Every alias-merge (§4, stage 2)
   carries a confidence score, is audited, and is **undoable**. A bad auto-merge
   fabricates a false conflict — the most damaging failure mode this tool has.

---

## 2. Scope

### In scope (MVP)

- **One domain vertical: corporate ownership / beneficial-owner interlocks**, built
  end-to-end on the **ICIJ Offshore Leaks** corpus (the existing `cmd/import-icij`
  importer in graphdb loads it — real, public, legally-clean data with zero connector
  work).
- **One workflow: ad-hoc pairwise screen.** Input two+ named parties; output ranked
  candidate conflict paths.
- Entity resolution by **record linkage** (deterministic blocking + string/phonetic +
  structured-attribute matching).
- A **scoring function** that ranks candidate paths (the COI policy — see §5).
- A defensible **report** (path + per-edge provenance + confidence).

### Explicitly out of scope (deferred, not abandoned)

- The other four domain verticals (procurement, research/peer-review,
  investment/regulatory, political). They share this core; we build profiles for them
  **after** the first vertical is validated — and only extract the profile abstraction
  when domain #2 forces it (YAGNI; no speculative plugin framework).
- Batch sweep, continuous monitoring, exploratory-visual workflows.
- Any UI beyond a CLI + structured (JSON) report.

---

## 3. Architecture: one engine, swappable profiles

```
┌─────────────────────────────────────────────────────────────┐
│  COI Screen (sibling consumer repo)                          │
│                                                              │
│   ┌────────────────────────────────────────────────────┐    │
│   │  COI core engine (domain-agnostic)                  │    │
│   │   ingest+provenance → resolve → connect →           │    │
│   │   score → report                                    │    │
│   └────────────────────────────────────────────────────┘    │
│                          ▲                                   │
│   ┌──────────────────────┴─────────────────────────────┐    │
│   │  Domain profile = { ontology, connectors, rulepack }│    │
│   │  MVP profile: ICIJ corporate-ownership              │    │
│   └────────────────────────────────────────────────────┘    │
│                          │ embeds                            │
└──────────────────────────┼──────────────────────────────────┘
                           ▼
        graphdb (Go library: pkg/storage, pkg/algorithms,
                 cmd/import-icij, optional pkg/vector)
```

**Code home:** a **new sibling consumer repo** that embeds graphdb as a Go library
(direct `pkg/storage` + `pkg/algorithms` import — rated `mature` in
`docs/CAPABILITIES_2026-05-10.md`). This mirrors the `understand-graphdb` boundary:
generic graph operations stay in the engine; COI policy lives in the consumer. No
server to run for the MVP.

> Decision deferred to implementation: exact repo name. Working name `coi-screen`.

---

## 4. Pipeline

Five stages. Each is a unit with one purpose, testable independently.

| # | Stage | Responsibility | graphdb primitive |
|---|-------|----------------|-------------------|
| 1 | **Load** | ICIJ corpus → in-memory graph, once per run/session. Tag every node & edge with source provenance. | `cmd/import-icij` + `pkg/storage` |
| 2 | **Resolve** | Match each input party name → ICIJ node(s); collapse intra-corpus aliases. **Type-aware, attribute-gated, three-band** (MATCH / REVIEW / NO); REVIEW-band merges are human-confirmed. Each merge confidence-scored + reversible. | **record linkage (new)**; `pkg/vector` HNSW only as optional ANN accelerator with caller-supplied vectors |
| 3 | **Connect** | Find paths ≤ N hops between resolved parties, restricted to interest-bearing edge types. | `pkg/algorithms` (shortest-path / bounded traversal) |
| 4 | **Score** | Rank each path into a candidate conflict (see §5). | new (consumer policy) |
| 5 | **Report** | Emit ranked candidates: path + per-edge provenance + confidence. No verdict. | new (thin) |

### ICIJ ontology (to confirm during the spike)

ICIJ node kinds: `Entity` (companies/trusts), `Officer` (people/orgs in a role),
`Intermediary` (law firms/agents), `Address`. Interest-bearing edges (canonical ICIJ
labels — **verify exact labels emitted by `cmd/import-icij` during Milestone 1**):

- `officer_of` (director/shareholder/beneficiary) — **strong** interest
- `intermediary_of` (set up / administers entity) — **medium**
- `registered_address` / shared-address — **weak** (co-location signal)
- `similar` — structural (ICIJ's own resolution), not interest-bearing

> **Spike finding (2026-06-02), verified against `cmd/import-icij/main.go`:** the importer
> passes ICIJ's coarse `rel_type` straight through as the edge *type* (fallback
> `RELATED_TO`). The *fine-grained* role — "shareholder of" vs "director of" vs
> "beneficiary of" — lives in the edge **`link` property**, not the type. So `ScorePath`
> must read the `link` property to weight co-ownership above a nominal directorship.
> Node kinds confirmed: `Entity`, `Officer`, `Intermediary`, `Address` (label =
> `node_type`). Node-level provenance is free (`source` property = panama/paradise/
> pandora), but **the importer does NOT tag edges with a source — edge provenance must be
> added in coi-screen's own ingestion** (stage 1).

### Entity resolution: record linkage, NOT LSA embeddings

**Critical correction over the initial idea.** graphdb's `/v1/embeddings` endpoint is
**LSA** — vocabulary-bound, returns HTTP 400 on out-of-vocabulary terms (see repo
memory `reference_graphdb_embedding_search_api.md`). Proper nouns are exactly the OOV
case, and *semantic* similarity is the wrong signal for name/record linkage anyway.

Entity resolution is therefore classic record linkage:

- **Blocking** to cut the comparison space (e.g. by surname soundex, jurisdiction).
- **String/phonetic** similarity (Soundex/Metaphone, Jaccard/Jaro-Winkler) on names.
- **Structured-attribute agreement** (DOB, address, jurisdiction, identifiers) to
  confirm.

`pkg/vector` HNSW remains available as an *optional* approximate-nearest-neighbour
accelerator **if** the consumer supplies its own vectors — it is not the resolution
mechanism, and the LSA endpoint is kept off the critical path.

**Spike-validated requirements (2026-06-02).** A throwaway matcher on a representative
labelled fixture of ICIJ-style name variants + hard negatives showed a naive
single-threshold matcher (Jaro-Winkler + Soundex) is **unusable: precision 0.44**. Three
changes — all of which the failure modes dictated — lifted auto-merge precision to ~0.83
with zero hard misses:

1. **Type-aware matching.** Person (`Officer`) vs company (`Entity`) need different
   logic: persons match on surname-agreement + given-name *compatibility* (a fully-spelled
   given name that differs, e.g. "Maria" vs "Mario", is **not** auto-mergeable); companies
   match on a token-set average with a *weakest-link* floor (so "Acme Holdings" ≠ "Acme
   Trading"). ICIJ's `node_type` supplies the kind for free.
2. **Attribute gating as a hard signal, not a nudge.** Known-but-differing jurisdiction
   blocks the match ("Robert Smith / BVI" ≠ "Robert Smith / Panama").
3. **A three-band output — MATCH / REVIEW / NO — with human confirmation for REVIEW.**
   Adversarial near-duplicates ("Williams" vs "Williamson") cannot be cleanly auto-separated
   by *any* string or semantic metric; they must route to human review. This is the empirical
   proof that "candidates, never verdicts / reversible merges" (§1) is mandatory, not
   defensive — auto-merge alone false-merges ~1-in-2 on hard pairs.

---

## 5. The COI scoring function (USER-OWNED policy)

This is the one piece of genuine domain policy with no single right answer, and it is
**deliberately left for the user to author at implementation** — not boilerplate to
guess.

Inputs available to the function, per candidate path:

- **Path length** (shorter chains = stronger conflict signal).
- **Edge-interest weights** along the path. **Read the edge `link` property, not just the
  edge type** (spike finding): co-ownership > shared-intermediary > shared-address, and
  "shareholder of" should outweigh a nominal directorship — that distinction is in `link`.
- **Resolution confidence** of the endpoint nodes (how sure are we these are the named
  parties — and any intermediate merges).

The function combines these into (a) a numeric score for ranking and (b) a flag
threshold. Prepared signature (to be filled in during implementation):

```go
// ScorePath ranks one candidate conflict path. Higher = stronger candidate.
// Returns the score and whether it crosses the flag threshold.
// THIS IS COI POLICY — the user authors the body.
func ScorePath(p Path, res ResolutionConfidence) (score float64, flagged bool)
```

---

## 6. Interface

CLI, single command, structured output:

```
coi screen --party "Acme Holdings Ltd" --party "Jane Q. Approver" \
           --dataset icij --max-hops 4 --format json
```

Output: ranked candidate conflicts, each as `{ path: [...edges with provenance...],
score, confidence, flagged }`. JSON for piping/automation; a human-readable table
variant is a nice-to-have, not MVP-blocking.

---

## 7. Milestones (implementation ordering)

1. ~~**SPIKE — entity resolution (riskiest assumption).**~~ **DONE 2026-06-02.** Verdict
   **PROCEED**: record linkage is viable *given* type-aware matching + hard attribute
   gating + a human-confirmed REVIEW band (see §4). LSA endpoint confirmed off the path
   (the experiment ran in pure Go, no server). Importer node/edge model confirmed against
   source. **Remaining for Milestone-1-proper: re-run the matcher against the real ~800K
   ICIJ download to get a true precision/recall number on adversarial real pairs** — the
   spike validated the *technique* on a representative fixture, not the full-corpus metric.
2. Load + provenance tagging (stage 1) against a real ICIJ slice.
3. Connect: bounded interest-bearing pathfinding (stage 3).
4. Score + report (stages 4–5); user authors `ScorePath`.
5. CLI wiring + end-to-end test on a known-conflict fixture.

---

## 8. Testing strategy

- **Resolution:** table-driven tests with hand-labelled ICIJ alias sets (true matches +
  hard negatives — distinct people sharing a name). Measure precision/recall; false
  merges are the worst outcome, so weight precision.
- **Pathfinding:** fixtures with known shared-owner / interlock structures; assert the
  expected path is found and spurious ones are not.
- **Scoring:** golden-file ranking tests so policy changes are visible in diffs.
- **End-to-end:** a small ICIJ slice with a planted, documented conflict; assert it
  surfaces as the top candidate with a correct, sourceable evidential path.

---

## 9. Risks

| Risk | Mitigation |
|------|------------|
| Entity resolution unreliable on real names | **Spike done — viable** with type-aware + attribute-gated + REVIEW-band matching; full-corpus precision number still to confirm in Milestone-1-proper. |
| False conflicts from over-eager merges | **Empirically required:** three-band MATCH/REVIEW/NO with human-confirmed REVIEW; reversible, confidence-scored merges; candidates-not-verdicts; provenance on every edge. |
| ~~ICIJ edge labels differ from assumed~~ | **Resolved by spike:** node kinds + edge model confirmed against `cmd/import-icij/main.go`; fine-grained role is in the `link` property. |
| Scope creep back to "all five domains" | YAGNI: one vertical end-to-end; extract profile abstraction only when domain #2 forces it. |
| LSA embeddings creeping onto the resolution path | Explicit design constraint (§4); spike verifies. |

---

## 10. Open questions for the user

- Repo name (working name `coi-screen`).
- Whether the spec should migrate into the new repo once it's created, or stay
  cross-referenced from graphdb.

---
name: coord-insight
description: Record, list, show, or promote an `:Insight` in coord — the raw-observation layer (lower bar than `:Lesson`). Promotion via `coord insight promote` creates a PROMOTED_TO edge so lineage is traceable.
when_to_use: Mid-task the agent notices something potentially-but-not-obviously useful. The user says "record an insight," "note that down," "I noticed X," "promote this insight to a lesson." Reviewing accumulated observations on a topic to find promotion candidates. Capturing a thought without deciding "is this lesson-worthy?" right now.
allowed-tools: Bash(../graphdb-coord/coord *) Bash(jq *)
---

# Coord insight

Raw observations tied to graphdb tasks. Lower bar than `:Lesson` — record now, decide if it generalizes later. The promote step is where curation happens.

## Current state

Live snapshot at skill-load time:

- Corpus: !`../graphdb-coord/coord insight list --limit 5 --json 2>/dev/null | jq -ce '{count, recent_ids: [.insights[0:3][].id]}' 2>/dev/null || echo '(coord daemon unreachable — see Pre-flight)'`

## Why insights vs. lessons?

- **`:Lesson`** = curated, surfaces on every related claim, drives recall.
- **`:Insight`** = observation, doesn't surface automatically, awaits curation.

The split exists because the cost of recording an observation should be much lower than the cost of recording a "thing that surfaces in agents' faces forever." Insights let an agent capture in-flight thinking without committing to durability.

## When to invoke

- Mid-task, the agent notices something potentially useful but unverified:
  - "These two storage tests share a setup pattern — could be extractable, not sure yet."
  - "F3 PR-3's masking might collide with H4.1's response helpers — flag for follow-up."
- The user says "record an insight," "note that down," "I noticed X," "save this thought."
- Reviewing recent work to look for promotable observations: "what insights have I logged on topic X?"
- Explicit promote request: "promote insight Z to a lesson," "this insight has earned its way to a lesson."

## What this skill does NOT do

- **Doesn't auto-surface insights on claim.** Only lessons do. If you want auto-surfacing, record a lesson (or promote an insight to one).
- **Doesn't deprecate insights.** Insights have a `deprecated_at` field but no first-class deprecation flow yet — they're append-only in practice. Use lesson invalidation for the curated path.
- **Doesn't replace `coord-lesson`.** If the observation is already concrete and conditional ("when X, do Y"), record it as a lesson directly. The insight tier is for "I noticed X" without the conditional yet.

## Pre-flight

```bash
COORD_BIN="${COORD_BIN:-../graphdb-coord/coord}"
if [[ ! -x "$COORD_BIN" ]]; then
  echo "coord binary not found at $COORD_BIN — build it: (cd ../graphdb-coord && go build -o coord ./cmd/coord)" >&2
  exit 1
fi
"$COORD_BIN" status >/dev/null || { echo "coord daemon not reachable" >&2; exit 1; }
```

## Process

### Record an insight

```bash
"$COORD_BIN" insight record \
  --content "Two storage tests in persistence_replay_test.go share an identical 11-line setup; could extract a helper if a third repeats." \
  --topic "storage-tests" \
  --task "graphdb:H4.3-followup"
```

All three optional:

- `--content` (or `--file path/to/text`): the observation itself.
- `--topic`: a coarse label for grouping. Stable strings (e.g. `storage-tests`, `compliance-api`) make `--topic` queries useful later.
- `--task`: ties the insight to a task via `DURING_TASK`. Omit for cross-cutting observations not bound to one task.

### List insights

```bash
# All insights, newest first.
"$COORD_BIN" insight list --limit 20

# Filter by topic.
"$COORD_BIN" insight list --topic "storage-tests" --limit 10

# Filter by task.
"$COORD_BIN" insight list --task "graphdb:H4.3-followup"

# Include deprecated entries.
"$COORD_BIN" insight list --include-deprecated
```

### Show one insight

```bash
"$COORD_BIN" insight show "insight-abc123"
```

### Promote insight to lesson

When an insight has accumulated enough evidence to deserve auto-surfacing on related claims, promote it:

```bash
"$COORD_BIN" insight promote "insight-abc123" "graphdb:F4" \
  --evidence-ref "https://github.com/dd0wney/graphdb/pull/111"
```

The task argument (`graphdb:F4`) is the task the new `:Lesson` should be tied to via `LEARNED_FROM`. This need NOT be the original `DURING_TASK` — promotion is the moment to pick the most representative task. The insight stays in place; a `PROMOTED_TO` edge and `promoted_to_lesson_id` property let you trace lineage.

One insight can be promoted multiple times (different tasks → different lessons). Idempotency: re-promoting against the same task creates a new lesson (Phase 2 item 8's content-hash dedup may collapse it if the content is identical).

## What a good insight looks like

- **Observation-shaped, not directive-shaped**: "I noticed X" vs. "always do Y." If it's already directive, skip to a lesson.
- **Time-stamped naturally**: the insight content should still make sense weeks later. Avoid pronouns referring to the current session ("this code," "what I just did").
- **Topic-tagged when possible**: bare insights are findable only via full-list scan; topic-tagged are findable via `--topic`.

## Edge cases

- **Recording an insight with no `--task` and no `--topic`.** Allowed; it's a free-floating observation. List queries without filters will surface it. Use this for cross-cutting hunches.
- **Insight content > a few sentences.** Use `--file` to point at a markdown file rather than inlining huge prose. The content is stored as a single property — large strings work but degrade the list view's preview.
- **Promoted insight's content drifts from the lesson's.** The promote step COPIES content at promote-time; later edits to either side don't sync. Treat insight + promoted lesson as a snapshot pair.

## Honest framing

Insights pull weight when:

- The agent is mid-investigation and needs a low-cost scratch pad that outlives the session.
- The user wants to capture a thought without deciding "is this lesson-worthy?" right now.
- The dreaming layer (`coord-dream`) later wants raw signal to cluster — but **not yet**: the v1 dreaming analyzer (`cross_lesson_similarity`) clusters lessons, not insights. Insights become dream-fuel only after promotion.

Insights do NOT pull weight when:

- The finding is already concrete enough to be a lesson. Skip the tier.
- The observation lives better in a planning doc or PR body (e.g. an architectural note that needs to surface to humans, not future agents).
- Solo single-session work where you'll act on the observation within the same hour.

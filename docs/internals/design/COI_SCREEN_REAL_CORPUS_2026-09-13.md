# Real-corpus coi-screen run (Milestone-1-proper) — 2026-09-13

Coord task `graphdb:v1.1-coi-screen-real-corpus`. Closes the gap that
`SPIKE_COI_SCREEN_VALIDATION_2026-07-01.md` § Limitations named: the real
consumer binary, on the real ICIJ corpus, in mmap mode. graphdb at `f960e41`
(main after #593), coi-screen at `28a759d` (branch `fix/graphdb-module-rename`,
see § What had to change first).

## TL;DR

- **The consumer runs end-to-end on the full corpus in mmap mode.** 5 of 5
  officer pairs that share an entity are flagged. The importer loads 2.0M
  nodes and 3.3M edges in 16 s.
- **graphdb's reads are not the cost.** Open is 11 ms, a 771K-node label
  enumeration is 0.6 s. The cost inside graphdb is `Close`: it rewrites the
  1.1 GB snapshot on a session that wrote nothing, 8.3 s and 7.9 GB resident.
  That is a safety defect as much as a performance one — every read-only
  consumer rewrites customer data on exit. See § Findings 1.
- **Decision B-1 (is full-graph enumeration a hot path?)**: not on the
  consumer's read path. It is on graphdb's own exit path, through `Close`.
- The runbook in the July spike could never have worked: the graphdb library
  does not read `GRAPHDB_STORAGE_MODE`, and coi-screen did not set
  `UseMmapSnapshot`, so every default import since #447 was refused at open.
  The coi step of `scripts/consumer-drive.sh` has the same shape and has
  skipped silently since coi-screen was never checked out.

## Corpus

ICIJ Offshore Leaks `full-oldb.LATEST.zip`, generated 2023-09-06, 68 MB
zipped, 626 MB unzipped. It was on this machine at
`/mnt/ssd2/Workspace/icij/offshoreleaks-data-packages/raw-data/` the whole
time the planning doc said "deferred for lack of a local corpus".

| File | Rows |
|---|---|
| nodes-entities.csv | 814,344 |
| nodes-officers.csv | 771,315 |
| nodes-addresses.csv | 402,246 |
| nodes-intermediaries.csv | 26,768 |
| nodes-others.csv | 2,989 |
| **nodes, total** | **2,017,662** |
| relationships.csv | 3,339,267 |

The "~814K ICIJ" in the task title is the entity count, not the node count.
`cmd/import-icij` takes one nodes file with a `node_type` column, and the
package ships five files with five headers and no such column.
`scripts/icij-merge-nodes.py` (added with this doc) projects them onto the
importer's ten columns in ~10 s.

Data caveat: 1,139 `node_id` values appear in two of the five files. The
importer creates a node per row and keeps the last mapping, so edges to those
ids attach to the last-created node. Not investigated further; it is an ICIJ
packaging property, and it does not affect the pairs below.

## What had to change first

None of this is in graphdb. coi-screen at `f5a52e8` did not build against
graphdb main:

1. It imported `github.com/dd0wney/cluso-graphdb`, the pre-rename module path.
2. `GetNodesByLabelForTenant` gained an error return in #531 (ADR 0003);
   `linkage.resolveCandidates` discarded it. Now it returns the error, so a
   damaged label bucket refuses loudly instead of resolving nothing.
3. `graphload.Open` never set `UseMmapSnapshot`. graphdb refuses to open a
   `snapshot.mmap` directory in JSON mode (the loud refusal from the
   mmap/JSON switch work), so every default import was unusable. `Open` now
   mirrors `cmd/server`: mmap unless `GRAPHDB_STORAGE_MODE=json`, any other
   value is an error. Red first: `TestUseMmap` failed with `undefined: useMmap`.

coi-screen commits `fee1300` and `28a759d` on branch `fix/graphdb-module-rename`.

## Method

Local Fedora dev machine, 32 cores, 62 GB. All timings are single runs with
`/usr/bin/time`; RSS is peak resident set.

1. Merge nodes, import twice: once in mmap mode (default), once with
   `--storage-mode json`.
2. Pick pairs from the corpus itself (`seed=7`): five officer pairs that share
   an entity through `officer_of` (a true 2-hop conflict) and two officer
   pairs with degree 1 that share no entity. The "negative" pairs can still
   connect through a shared address or intermediary, so they are not clean
   negatives; treat their flags as unlabelled, not as false positives.
3. Screen each pair with the real `cmd/coi` binary at `--max-hops 2`; two
   positive pairs again at `--max-hops 4` and against the JSON store.
4. A 40-line probe (scratchpad only) times the two graphdb calls the resolver
   depends on — `NewGraphStorageWithConfig` and `GetNodesByLabelForTenant` —
   plus `Close`, in each mode, to attribute the screen's wall time.

## Results

### Import

| Mode | Nodes | Edges | Import | Wall incl. snapshot | Peak RSS | On disk |
|---|---|---|---|---|---|---|
| mmap | 7.3 s (278K/s) | 8.8 s (381K/s) | 16.0 s | 32.7 s | 12.2 GB | 1.1 GB `snapshot.mmap` |
| json | — | — | — | 42.9 s | 17.9 GB | 1.7 GB `snapshot.json` |

### graphdb calls alone (probe, tenant `default`, label `Officer`, n=771,315)

| Call | mmap | json |
|---|---|---|
| open | **11 ms** | 25.7 s |
| `GetNodesByLabelForTenant`, 1st call | 639 ms | 549 ms |
| same, 2nd call | 444 ms | 548 ms |
| `Close` (no writes since open) | **8.3 s** | 25.0 s |
| process peak RSS | 7.9 GB | 17.5 GB |

`Close` rewrote the snapshot both times: the file mtime moved to the close
time, on a process that only read.

### Screens (`cmd/coi`, real binary)

| Pair | Kind | Hops | Mode | Wall | Peak RSS | Candidates | Flagged |
|---|---|---|---|---|---|---|---|
| ZHANG PENG / BENSAID DJAZAIRI | shared entity | 2 | mmap | 21.3 s | 7.9 GB | 2 | 1 |
| CIFFO, FRANCESCO / CIFFO, GIUSEPPE | shared entity | 2 | mmap | 21.5 s | 7.8 GB | 2 | 1 |
| Ling Kam Wai / Ko Chi | shared entity | 2 | mmap | 73.7 s | 8.3 GB | 217 | 133 |
| SIMOONS, JOHNNY KENETH / … (JR) | shared entity | 2 | mmap | 22.6 s | 8.5 GB | 40 | 12 |
| Higuchi Yuji / Fortune Vision Group Limited | shared entity | 2 | mmap | 71.9 s | 8.0 GB | 29 | 27 |
| MCKENDRY GERALD W / MANCUSO CLAUDIO | degree-1, no shared entity | 2 | mmap | 49.8 s | 8.1 GB | 25 | 14 |
| CORIGLIANO FRANK J. / MCCLAMMY EDWARD G. | degree-1, no shared entity | 2 | mmap | **killed at 783 s** | 1.5 GB | — | — |
| ZHANG PENG / BENSAID DJAZAIRI | shared entity | 2 | json | 74.3 s | 18.3 GB | 2 | 1 |
| CIFFO, FRANCESCO / CIFFO, GIUSEPPE | shared entity | 2 | json | 68.8 s | 18.5 GB | 2 | 1 |
| ZHANG PENG / BENSAID DJAZAIRI | shared entity | 4 | mmap | 63.4 s | 7.9 GB | 4 | 1 |
| CIFFO, FRANCESCO / CIFFO, GIUSEPPE | shared entity | 4 | mmap | 24.6 s | 8.4 GB | 148 | 1 |

Every positive pair flags with confidence 1.0 and a 2-edge path through the
shared entity. Candidate counts above 2 are the resolver matching several
graph nodes per name (the corpus has many "Ling Kam Wai"); each candidate
pair is screened.

The killed screen's goroutine dump (SIGQUIT after 13 min) shows the main
goroutine in `graphload.FindInterestPaths` → `adjacency` →
`storage.decodeNodeRecordAt` → `readProps`: the consumer's depth-bounded DFS,
decoding a node record from the mmap base for every neighbour it inspects.
Its RSS (1.5 GB) says it never finished the resolver's 8 GB label scan on
both parties before the search began, so the time is in path enumeration on
one resolved side, most likely through a hub the `maxDegree` guard did not
catch. Not attributed further here; it is the consumer's search, and it is
Milestone-1-proper step 5 in coi-screen's own plan.

## Findings

1. **`GraphStorage.Close` rewrites the snapshot on a session that wrote
   nothing.** `Close` calls `Snapshot` unconditionally
   (`pkg/storage/persistence.go`, `Close` → `snapshotWithBoundary`), and the
   mmap branch merges overlay ∪ base − tombstones into a fresh file even when
   the overlay is empty and there are no tombstones. Cost on this corpus:
   8.3 s and 7.9 GB in mmap mode, 25 s and 17.5 GB in JSON mode, per process
   exit. The 11 ms mmap open that ask #1 bought is spent 750× over on the way
   out. It is also the wrong shape for the repo's objective: a read-only
   consumer rewrites the customer-data-equivalent file every time it exits, and
   two read-only consumers exiting together rewrite it concurrently. Nothing in
   `pkg/storage` tracks "no writes since open". Candidate task: skip the
   rewrite when the WAL boundary equals the boundary at open and the overlay
   and tombstone sets are empty; a test must see the mtime unchanged after a
   read-only open/close, and changed after one write.
2. **The consumer's hot path is a label-bucket scan plus a DFS, not a
   full-graph enumeration** — the July spike's claim holds on the real corpus.
   `GetNodesByLabelForTenant` over 771K nodes is 0.6 s in either mode. The
   spike's other number, "mmap reopen ~1370× cheaper", is 25.7 s vs 11 ms
   here, ~2300×, on a corpus 2.2× the synthetic one.
3. **Decision B-1**: full-graph enumeration is not on the consumer's read
   path. It is on graphdb's own `Close` path (finding 1), which every consumer
   pays. That reframes DoD Levers 2–3: the lever is not a faster enumeration,
   it is not enumerating on a read-only exit.
4. **The consumer-drive coi gate never fired.** `scripts/consumer-drive.sh`
   skips when `../coi-screen` is absent, and it has been absent on every
   machine that ran it. Had it run, it would have failed on the module rename
   (since the rename), the #531 signature, and the mmap refusal (since #447).
   A gate that skips is not evidence; see `CLAUDE.md` § Red-first.
5. **The runbook's step 3 was wrong as written.** `GRAPHDB_STORAGE_MODE=mmap`
   in the environment does nothing to a library consumer; only `cmd/server`,
   `cmd/graphdb-admin` and `cmd/import-icij` read it. The env line is correct
   only after coi-screen `28a759d`, which reads the same variable itself.
6. **Screen wall time is dominated by things outside graphdb's reads.** Of a
   21 s screen: ~8 s is `Close` (finding 1), ~1.2 s is two label scans, and the
   remaining ~12 s is the resolver scoring 771K names in Go (coi-screen's
   documented O(label-set) scan) plus the DFS. JSON mode adds ~50 s of open
   on top. A daemon that keeps the store open (coi-screen's `serve` mode)
   pays the open and close once, not per screen.

## What this does not show

- No precision/recall number. Milestone-1-proper step 3 needs hand-labelled
  alias sets; the seven pairs here are structural ground truth (shared
  entity or not), not labelled linkage decisions. The "negative" pairs flag
  through shared addresses and intermediaries, which is the scoring policy
  working as designed, not a measured false-positive rate.
- Single runs, one machine, cold page cache after each import. Treat the
  seconds as magnitudes.
- The killed screen is not attributed below the package level.

## Follow-ups

graphdb:
- Task for finding 1 (`Close` rewrite on a read-only session). Safety first,
  performance second.
- `docs/NEXT_STEPS_2026-06-18.md` §D: retire "deferred for lack of a local
  corpus", record B-1 as answered here.
- `scripts/consumer-drive.sh`: unchanged. With coi-screen `28a759d` its coi
  step works as written; it still skips when the sibling is absent, and that
  skip is loud on stderr. Whether a skip should fail CI is a separate call.

coi-screen (its own plan, M1 steps 3–5):
- Push `fix/graphdb-module-rename`; without it the consumer does not build.
- Hand-label and measure linkage precision.
- Replace the linear resolver scan and bound the DFS on hubs; the killed pair
  is the reproduction.

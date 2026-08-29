# Testing a cluster: what PostgreSQL::Test::Cluster does that graphdb does not

**Date**: 2026-08-29
**Source**: `src/test/perl/PostgreSQL/Test/Cluster.pm` at `e18b0cb7` — 4,074 lines, 96 routines
**Status**: inspiration, not a plan. Nothing here is scheduled.

Companion to `SQLITE_TESTING_SCORECARD.md`. That document asked what SQLite does
to a single storage engine. This one asks what PostgreSQL does to a *set of
running servers*, because that is the question `pkg/cluster` has never been asked.

## The measurement that started this

`pkg/cluster` is ~2,800 lines of production code. `CLAUDE.md` already warns not
to claim it is unwired without checking. So this document checked.

**The production code dials TCP at three sites:**

| Site | Call |
|---|---|
| `pkg/cluster/discovery.go:152` | `net.DialTimeout("tcp", addr, 3*time.Second)` |
| `pkg/cluster/discovery.go:285` | `net.DialTimeout("tcp", seedAddr, 1*time.Second)` |
| `pkg/cluster/election_voting.go:52` | `net.DialTimeout("tcp", addr, em.config.VoteRequestTimeout)` |

**No test in the package imports `net`, `net/http`, or `os/exec`.** The five test
files import `testing`, `time`, `sync`, `sync/atomic`, and the AST packages the
import guard uses. Nothing else.

Positive control for that claim: the same grep against `pkg/api/*_test.go` returns
hits, so the instrument can find a network import when one is present.

The consequence is precise. `TestFullClusterSetup` (`integration_test.go:9`)
builds three `ClusterConfig` structs naming `localhost:9090`, `:9091` and `:9092`.
Nothing binds those ports. Nothing dials them. The test exercises in-process state
machines, which is worth doing and is not what its name claims. The three dial
paths above have no coverage at all.

Two smaller measurements, both of which `Cluster.pm` solved long ago:

- **78 hardcoded port references** across `pkg/cluster/*_test.go`.
- **Five bare `time.Sleep` calls** as the only synchronisation, and no polling
  helper anywhere in the package.

## What the harness provides

96 routines. The ones that matter here:

| Group | Routines |
|---|---|
| Lifecycle | `init`, `start`, `stop`, `restart`, `reload`, `kill9`, `promote`, `teardown_node` |
| Clone and recover | `backup`, `backup_fs_cold`, `init_from_backup` |
| Isolation | `get_free_port` |
| Drive a node | `safe_psql`, `psql`, `background_psql`, `interactive_psql` |
| Assert reachability | `connect_ok`, `connect_fails`, `raw_connect_works` |
| Synchronise | `poll_query_until`, `wait_for_event`, `wait_for_catchup`, `wait_for_replay_catchup`, `wait_for_slot_catchup`, `wait_for_subscription_sync`, `wait_for_log` |
| Read the node's own account of itself | `logfile`, `rotate_logfile`, `log_content`, `log_check`, `log_contains` |

It lives in `src/test/perl/`. The harness is not in the production tree. graphdb's
existing seams — `pkg/alloc`, `pkg/vfs` — are production packages by necessity,
because they sit on the call path. A node harness has no such excuse.

## The three ideas worth taking

### 1. `kill9`, and the comment above it

`Cluster.pm:1195`:

> Send SIGKILL (signal 9) to the postmaster.
>
> Note: if the node is already known stopped, this does nothing. **However, if we
> think it's running and it's not, it's important for this to fail. Otherwise,
> tests might fail to detect server crashes.**

Two separate lessons in one routine.

The first is the capability: crash safety is tested by killing a real process and
restarting it. graphdb's fault injection is entirely in-process — `pkg/alloc`
refuses an allocation, `pkg/vfs` fails a syscall. Nothing kills the process. An
in-process fake cannot prove the operating system wrote what the program believes
it wrote, which is the whole question a crash test asks.

`NEXT_STEPS_2026-06-18.md` already lists "crash/power-loss simulation with torn
writes and write reordering" as not started. `kill9` is the shape of the missing
piece, and it is eleven lines of Perl.

The second lesson is the one this repository learned the expensive way. `CLAUDE.md`
§ "Common mistakes" says:

> **A watcher must be able to report the negative**: `pgrep -f 'make …'` matched
> the shell running the check itself, so it always saw a build in progress. Before
> trusting a condition check, confirm it can return false.

PostgreSQL wrote that same rule into a harness comment, and it applies to the
harness itself: a `kill9` that quietly succeeds against a dead node turns every
crash test into a test of nothing. Relevant here beyond clustering — #504 found a
crash-recovery test that had passed for nearly two months on an ID collision.

### 2. `get_free_port` instead of a hardcoded port

`Cluster.pm:1878`. It walks a port range, skips ports already claimed by nodes the
harness knows about, and then **actually attempts to bind** each candidate before
handing it out. The comment block explains which addresses it tests on which
platform and why 0.0.0.0 alone is insufficient on native Windows Perl.

graphdb has 78 hardcoded port references in `pkg/cluster` tests. Today that costs
nothing, because nothing binds them. It costs the first time somebody wires the
dial paths and CI runs two suites at once — and the failure will look like
flakiness, not like a fixture bug.

Cheap to pre-empt: a `freePort(t)` helper that binds `:0`, reads the assigned
port, closes, and returns it. Go makes this four lines where Perl needed fifty.

### 3. `poll_query_until` instead of `time.Sleep`

Seven `wait_for_*` routines, and sleep is not used as a synchronisation primitive.
Each waits on an observable condition with a timeout, and each fails loudly when
the condition never arrives.

graphdb's cluster tests currently sleep 50ms, 100ms and 2µs and then assert. A
sleep that is too short is a flaky test. A sleep that is long enough is a slow
one. Neither reports *what* it was waiting for when it fails.

## What this document does not claim

- Not that `pkg/cluster` is broken. It is untested at the network boundary, which
  is a different and narrower statement.
- Not that graphdb should adopt a Perl harness, or a process-per-node model. Go's
  `httptest` and `t.Cleanup` cover much of what `Cluster.pm` had to build by hand.
- Not a schedule. Nothing here is on the queue. The `kill9`-shaped crash test is
  the one item that would earn a place, because it closes a gap the planning doc
  already names.

## Suggested first step, if this is ever picked up

One test, not a harness: start two `cmd/server` processes on free ports, form a
cluster, SIGKILL the leader mid-write, restart it, and assert the survivor's view
and the restarted node's recovered state agree. That single test would exercise
all three untested dial sites and the crash path at once, and it would tell us
whether a harness is worth building before one is built.

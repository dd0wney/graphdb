# High Availability — Current Status

> **Status (2026-06-12): there is no supported HA deployment of graphdb today.**
> This document replaces an earlier 3-node HA quickstart whose code path no
> longer exists. The earlier version is preserved in git history
> (`git log --follow docs/HA_QUICKSTART.md`).

## What happened

The earlier quickstart was written against `pkg/replication`, the standalone
primary/replica replication layer. That layer (and its `cmd/graphdb-{primary,replica}`
binaries) was **retired in A8.1** (PRs #129/#130/#133, 2026-05-12) because it
pre-dated multi-tenancy and routed all replicated writes to the default tenant.
The code samples in the old guide no longer compile.

`pkg/cluster` (leader election, epoch fencing, quorum voting) still exists as a
library, but it composed with the retired replication transport and has no
supported production wiring.

## What to do instead

graphdb is **single-node by design** today. Write throughput is bounded by one
Go process; durability comes from the WAL + snapshots, not from replicas.

For practical scale and availability strategies — sizing limits, per-tenant
instance sharding behind a load balancer, backup/restore — see
[`PRODUCTION_QUICKSTART.md`](PRODUCTION_QUICKSTART.md) § "Scale Considerations".

For the architectural decision and the multi-quarter horizontal-scale roadmap,
see `docs/internals/design/A8_1_SPIKE_2026-05-12.md`.

# 1. Uniqueness-rules registry

Date: 2026-08-28
Status: proposed

## Context

`pkg/graphql/mutations_resolvers.go:20` carries a TODO from 2026-05-10: the
constants `claimLabel = "Claim"` and `claimUniqueProperty = "for_task"` are the
one place graphdb still knows coord-domain vocabulary by name. graphdb-coord was
extracted to a sibling repo on that date; the storage primitive
(`CreateNodeWithUniquePropertyForTenant`) stayed here because it is a useful
generic primitive, but the label-and-property tuple did not follow it out.

PR #470 (2026-08-28) made that rule *correct* — it now fires on label
containment rather than an exact single-label match, closing a bypass where a
caller could take a held task by passing `["Claim","Urgent"]` and getting no
error. It did not make the rule *generic*. graphdb still ships the word "Claim".

The end state named in the TODO: a configurable uniqueness-rules registry, after
which graphdb has zero coord-specific knowledge and the rule lives in
graphdb-coord's bootstrap path. Estimated 150–300 LOC of Go, no caller migration
because it slots in at the same site.

**Constraints:**

- Two surfaces enforce uniqueness today (GraphQL resolver and REST handler) and
  they must not diverge. If one keeps a stricter rule, the other becomes the
  documented way around it.
- graphdb is a general-purpose database. Most deployments have no uniqueness
  rules and must not be made to think about the feature in order to not use it.
- The consumer (graphdb-coord) is live. Its store at `~/.graphdb-coord-data`
  holds `:Claim` nodes right now and is actively written by running agents.
- The change has an ordering hazard across two repos. Between removing the
  hardcoded rule and declaring the replacement, a daemon accepts two claims for
  one task, reports success, and looks correct. That is #470's bug returning
  through the migration rather than through the code.

## Decision

Introduce a uniqueness-rules registry in graphdb. Split the work across the two
repos:

- **graphdb owns the mechanism**: the registry, its persistence, the admin API
  that accepts a rule, and the enforcement at both write surfaces.
- **graphdb-coord owns the declaration**: `coord-bootstrap.sh` declares the
  `:Claim` / `for_task` rule after the daemon starts.

| Element | Decision |
|---|---|
| Rule shape | `(name, label, propertyKey, scope)`, scope = per-tenant |
| Trigger | label **containment**, matching #470 |
| Persistence | in the store data directory, loaded before the store serves |
| Declaration | idempotent upsert on the admin REST surface |
| Required-rule list | deployment configuration, **empty by default** |
| Fail-closed | per **deployment**, never per label |
| Registry writes | never refused, explicitly |
| Enforcement | both GraphQL and REST, from the same registry lookup |

**Fail-closed is a property of the deployment, not of the label.** The daemon
reads a list of required rule names from its own configuration. If a required
rule is not registered, the daemon refuses the node writes that rule covers.
graphdb therefore knows only "this deployment declares rule R must exist"; the
operator configuration names `Claim`, and graphdb-coord owns that configuration.

**Registry writes are exempt, and the exemption must be explicit in code.** A
declaration is an admin REST write to the same daemon. A daemon that refuses to
serve while a required rule is missing cannot receive the declaration that would
fix it: the only action that clears the condition is the action the condition
blocks.

## Alternatives Considered

### Migration ordering

| Option | Pros | Cons |
|---|---|---|
| Remove hardcoded rule first, then declare | Shortest path | Carries the full silent-duplicate window. Rejected. |
| Declare first, then remove | Window is safe while both rules agree | Depends on an operator performing two steps in the correct order |
| **Fail closed (chosen)** | Removes the window without depending on the operator | Needs the required-rule list and the registry-write exemption |

### Where fail-closed lives

| Option | Pros | Cons |
|---|---|---|
| Refuse `:Claim` when no rule covers it | Simple | **Rejected — self-defeating.** graphdb must know `Claim` needs a rule, which is the knowledge the registry removes. It relocates the hardcoding and reports success. |
| Daemon refuses to serve entirely | Loudest | **Rejected — deadlocks.** The declaration cannot reach a daemon that refuses to serve. |
| **Refuse only the node writes a missing rule covers (chosen)** | No deadlock, no coord knowledge | The exemption for registry writes must be stated, not assumed |

### Rule persistence

| Option | Pros | Cons |
|---|---|---|
| **Persist in the store data dir (chosen)** | A restart cannot silently weaken a constraint | A stale rule can outlive its client |
| Client declares at every start | No stale rules | A restart with no client present yields a store that accepts duplicates and **reports success** — #470's failure class arriving through a restart |

Persistence wins on the asymmetry of the failure modes. A stale rule fails
**loudly**: writes return a conflict and an operator sees it immediately. A
missing rule fails **silently**: duplicates are accepted and everything looks
correct.

### Scope of the required-rule list

An earlier draft of this decision argued the list was optional hardening,
because a store with no rule declared holds no claims, so the first claim has
nothing to conflict with and damage needs two.

**That bound is wrong, and the counter-example is live on this machine.** It
holds only for a store the registry build *creates*. An existing store upgraded
in place holds claims (it served before the registry existed) and holds no rule
(rules did not exist when it was written). `~/.graphdb-coord-data` is exactly
that store: 71 nodes, one `:Claim` with a `for_task` value, actively written.
A second claim on that task during the upgrade window would succeed and report
success.

A `remove` on a populated store produces the same state, which the earlier
argument also missed — it assumed rules are only ever added, which this ADR's
own API contradicts.

The required-rule list is therefore **load-bearing for the upgrade interval**.
It is the only element of the design that speaks before the first write, so it
is the only one that can protect a store that already holds claims.

A tri-state configuration ("no config read" vs "config read, required nothing")
was considered and rejected: graphdb is a general database, and a setting most
users must understand in order to ignore is a bad setting. A plain list with an
empty default costs a general user nothing, and a deployment that wants the
guarantee opts in.

## Consequences

### Positive

- graphdb ships no coord vocabulary. The TODO at `mutations_resolvers.go:20`
  closes.
- Any deployment can declare a uniqueness constraint without patching graphdb.
- The migration has no window in which duplicates are silently accepted.
- Both write surfaces enforce from one registry lookup, so they cannot diverge
  the way they had before #470.

### Negative

- New persisted state in the store data directory, which is customer-data
  adjacent and therefore subject to the format-stability rule in `CLAUDE.md`.
- A stale rule can outlive its client and reject writes an operator expects to
  succeed. Accepted deliberately: loud beats silent.
- The required-rule list is a new deployment concept, even with an empty
  default.

### Risks

- **Deadlock via over-broad refusal**: mitigated by the explicit registry-write
  exemption, which must be a named requirement in the implementation, not an
  emergent property.
- **The two surfaces diverging again**: mitigated by a single lookup path and by
  tests on both surfaces, as #470 established.
- **A refusal test that only tests one direction**: a refusal for an unrelated
  cause reads identically to a pass. Every refusal test must run both
  directions and they must be distinguishable — declare no rule and confirm the
  write is refused, declare the rule and confirm the *same* write succeeds.

## References

- `pkg/graphql/mutations_resolvers.go:20` — the TODO this ADR closes
- PR #470 — containment fix; established the trigger semantics this registry inherits
- `pkg/storage/node_operations.go:122` — `CreateNodeWithUniquePropertyForTenant`, the atomic primitive the registry calls
- graphdb-coord `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` — option B-full, the original design sketch
- graphdb-coord `docs/COORD_SETUP.md` — the bootstrap path that would declare the rule

## Note on how this design was produced

It was settled across two Claude Code sessions (graphdb and graphdb-coord)
reviewing each other. Every element above changed because one side found the
other wrong:

1. The first fail-closed proposal kept the coord knowledge the registry removes.
2. The repair moved fail-closed to the deployment, and one variant of it
   deadlocked.
3. The bound on the residual window was correct for a new store and false for an
   upgraded one, which a check of the live store confirmed.

No element survived unchallenged, and none of the corrections came from
agreement. That is recorded because the reasoning is more reusable than the
conclusion.

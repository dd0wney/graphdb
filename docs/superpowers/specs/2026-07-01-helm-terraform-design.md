# Design: v1.3.0 "Deploy anywhere" — Helm chart + Terraform module

**Date**: 2026-07-01
**Track**: v1.3.0 "Deploy anywhere" (roadmap `docs/ROADMAP_post_1.0.md` §v1.3.0)
**Scope of this spec**: Helm chart + Terraform module ONLY. The first-party Go-native
client and the `gofmt` CI gate — the other two v1.3.0 deliverables — are **separate
spec → plan → implement cycles** and are out of scope here.
**Status**: approved (design), pending implementation plan.

## Problem

graphdb is GA (v1.0) and stateful, but there is **no first-party way to deploy it on
Kubernetes** — the #1 named adoption gap. Today the deploy surface is a `Dockerfile`
(multi-stage, non-root, `graphdb-server` + `graphdb-cli`), a `.goreleaser.yml`, and a
`deployments/` dir of docker-compose + monitoring + DigitalOcean + Cloudflare assets.
There is **no Helm chart, no Terraform module, no raw k8s manifests**.

This spec closes that gap with a single-node Helm chart and a thin, provider-agnostic
Terraform wrapper around it.

## Constraints that shape the design

- **Stability promise (v1.0 `STABILITY_POLICY.md`)**: v1.3 is a MINOR — everything must be
  **purely additive and backward-compatible**. This work adds packaging artifacts and
  touches **zero** Go/API/on-disk surface.
- **graphdb is stateful**: it persists a snapshot (mmap default since #447, JSON opt-out)
  under the `-data` dir. The k8s primitive must give it stable storage.
- **Clustering/HA is v2.0**, not now (`CLAUDE.md`; `ROADMAP_post_1.0.md`). The chart must
  target a **single node** and actively prevent multi-replica misconfiguration.
- **Server config is 100% environment-variable driven** (only `-port`/`-data` flags exist).
  This is what makes the chart low-risk: values map directly to env vars; no Go changes.

### Server configuration surface (source of truth for chart values)

From `cmd/server/main.go`:

| Env var | Purpose | Secret? |
|---|---|---|
| `PORT` | HTTP port (default 8080; flag `-port` also) | no |
| `GRAPHDB_STORAGE_MODE` | `json` forces JSON snapshot; unset = mmap default | no |
| `GRAPHDB_EDITION` | e.g. `community` to run unlicensed | no |
| `GRAPHDB_LICENSE` / `GRAPHDB_LICENSE_KEY` / `GRAPHDB_LICENSE_PATH` | license material | **yes** |
| `LICENSE_SERVER_URL` | remote license server | no |
| `TLS_ENABLED`, `TLS_CERT_FILE`, `TLS_KEY_FILE`, `TLS_CA_FILE`, `TLS_AUTO_GENERATE`, `TLS_HOSTS`, `TLS_ORGANIZATION`, `TLS_MIN_VERSION`, `TLS_CLIENT_AUTH`, `TLS_INSECURE_SKIP_VERIFY` | TLS config | cert/key material **yes**, toggles no |
| `ENCRYPTION_ENABLED`, `ENCRYPTION_MASTER_KEY`, `ENCRYPTION_KEY_DIR` | at-rest encryption | master key **yes** |
| `GRAPHDB_TRACING_ENABLED` (+ OTLP endpoint vars) | OpenTelemetry (off by default) | no |
| `GRAPHDB_PLUGIN_DIR` | enterprise plugin `.so` dir | no |
| `GRAPHDB_ENABLE_TELEMETRY` | opt-in telemetry | no |

Runtime facts: listens on **8080**; unauthenticated **`/health`** (already used by the
Docker `HEALTHCHECK`) and `/metrics`; `/api/metrics` is admin-gated. Container runs as
non-root user `graphdb`; data dir `/data`.

## Chosen approach (and rejected alternatives)

### Helm chart: single-node StatefulSet

**Chosen: StatefulSet + PVC, `replicaCount: 1`, with a hard fail-guard on `> 1`.**

- StatefulSet gives stable storage + identity for the snapshot volume.
- A template guard (`fail`) rejects `replicaCount > 1` with a message pointing at
  "clustering is v2.0". Rationale: the friendliest-looking Helm knob (`--set replicaCount=3`)
  would otherwise silently corrupt data — N pods behind one Service, each writing its own
  snapshot to its own PVC, no coordination. Encoding the v2.0 boundary in the chart beats
  documenting it.
- **Rejected — Deployment + PVC**: a `ReadWriteOnce` PVC plus a rolling update = old and new
  pods both trying to mount one volume → stuck rollout. StatefulSet's ordered
  recreate-on-update avoids this at replicas:1.

### Terraform module: thin, provider-agnostic Helm wrapper

**Chosen: a `helm_release` around the in-repo chart, chart source overridable via variable.**

- Uses the `helm` + `kubernetes` Terraform providers. Installs onto an
  **already-existing** cluster (user brings their own) — truly "deploy anywhere."
- Chart source defaults to the in-repo path; a variable lets a user point at an OCI/git
  source later without a module change.
- Verifiable against **kind**/minikube with zero cloud spend.
- **Rejected — full cloud cluster provisioning**: cloud-specific auth/networking, large,
  and unverifiable in this environment. Overshoots v1.3's "M / low-risk" sizing.
- **Rejected — wrapper + one cloud example (DOKS)**: real infra cost to test; deferred.

### Chart distribution: in-repo only (MVP)

- Chart lives at `deployments/helm/graphdb`; users run `helm install ./deployments/helm/graphdb`
  or reference it via git. CI adds only `helm lint` + `helm template`.
- **Rejected for now — OCI to ghcr.io / gh-pages repo**: both are clean later additions but
  add a CI publish job + release-versioning tie-in. Documented as a follow-up; not blocking.

## Layout

```
deployments/helm/graphdb/
  Chart.yaml                       # chart SemVer (independent); appVersion tracks graphdb release
  values.yaml                      # documented values surface
  values.schema.json               # JSON-schema validation (client-side, pre-install)
  templates/
    _helpers.tpl
    statefulset.yaml               # StatefulSet, replicas:1 (guarded), env from ConfigMap+Secret,
                                    #   probes on /health, non-root securityContext, PVC template
    service.yaml                   # ClusterIP :8080
    configmap.yaml                 # non-secret env vars
    secret.yaml                    # rendered only if .Values.secrets.create
    serviceaccount.yaml            # created if .Values.serviceAccount.create
    ingress.yaml                   # opt-in, disabled by default
    servicemonitor.yaml            # opt-in (Prometheus Operator CRD), disabled by default
    poddisruptionbudget.yaml       # opt-in, disabled by default
    NOTES.txt                      # post-install reachability instructions
    tests/
      test-connection.yaml         # `helm test` hook: curl /health

deployments/terraform/graphdb/
  main.tf                          # helm_release, chart source = var (default: in-repo path)
  variables.tf                     # namespace, release name, values passthrough, chart source override
  outputs.tf                       # release name / namespace / status
  versions.tf                      # required_providers: helm, kubernetes
  README.md
  examples/kind/                   # runnable example against a local kind cluster
    main.tf
    README.md
```

## Values surface (shape, not exhaustive)

```yaml
image:
  repository: ghcr.io/dd0wney/graphdb        # confirm actual published image ref at impl time
  tag: ""                                     # defaults to .Chart.AppVersion
  pullPolicy: IfNotPresent
imagePullSecrets: []

replicaCount: 1                               # values.schema.json + template guard reject > 1

resources: {}                                 # user sets requests/limits
nodeSelector: {}
tolerations: []
affinity: {}

persistence:
  enabled: true
  size: 10Gi
  storageClass: ""                            # "" = cluster default
  accessMode: ReadWriteOnce

service:
  type: ClusterIP
  port: 8080

config:                                       # -> ConfigMap (non-secret env)
  storageMode: ""                             # "" = mmap default; "json" opts out
  edition: ""
  tracingEnabled: false
  tls:
    enabled: false
    autoGenerate: true
    hosts: ""
  extraEnv: {}                                # escape hatch for any other non-secret env var

secrets:
  create: false                               # true = build a Secret from the values below (dev)
  existingSecret: ""                          # prod: reference a pre-provisioned Secret
  license: ""                                 # GRAPHDB_LICENSE
  encryptionMasterKey: ""                     # ENCRYPTION_MASTER_KEY
  # TLS cert/key material referenced via existingSecret in prod

serviceAccount:
  create: true
  name: ""
  annotations: {}

podSecurityContext:                           # matches Dockerfile non-root graphdb user
  runAsNonRoot: true
  fsGroup: <graphdb-gid>                       # resolve from Dockerfile at impl time
securityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]

probes:
  liveness:  { path: /health, initialDelaySeconds: 10, periodSeconds: 10 }
  readiness: { path: /health, initialDelaySeconds: 5,  periodSeconds: 10 }

ingress:
  enabled: false
  className: ""
  annotations: {}
  hosts: []
  tls: []

serviceMonitor:
  enabled: false                              # requires Prometheus Operator CRDs
  interval: 30s

podDisruptionBudget:
  enabled: false
```

## Testing strategy

**Automatable in CI (no Go changes, no live cluster):**
- `helm lint deployments/helm/graphdb`.
- `helm template` renders for these values permutations, each asserted:
  1. defaults (StatefulSet, replicas:1, ConfigMap, no Secret, no Ingress),
  2. `secrets.create=true` (Secret rendered; keys present),
  3. `secrets.existingSecret` set (no Secret rendered; envFrom references it),
  4. `config.tls.enabled=true`,
  5. `ingress.enabled=true`,
  6. `serviceMonitor.enabled=true`.
- **Negative test**: `replicaCount=2` must fail `helm template` with the v2.0 guard message.
- `values.schema.json` rejects an unknown/typo'd key and an out-of-range `replicaCount`.
- Terraform: `terraform fmt -check` + `terraform validate` on the module and the kind example.

**Manual (documented runbook, NOT claimed working without it):**
- `kind create cluster` → `helm install` → `helm test` (curls `/health`) → port-forward and
  hit `/health`. Same flow driven via the Terraform kind example. This is the de-risking
  runbook per the repo's "unverifiable work" rule — a live cluster is not guaranteed in CI.

## Explicitly out of scope

- First-party **Go-native client** (separate v1.3 cycle).
- **`gofmt` CI gate** (separate, trivial cycle).
- **Chart publishing** (OCI/ghcr or gh-pages) — documented follow-up.
- **HPA** — meaningless at `replicas:1`.
- **Multi-node / clustering / HA** — v2.0.
- **Cloud cluster provisioning** in Terraform — rejected above.

## Open items to resolve at implementation time

- Confirm the **actual published container image reference** (ghcr path/tag) the release
  pipeline produces — the `image.repository` default must match it, not a guess.
- Resolve the **`graphdb` uid/gid** from the Dockerfile for `fsGroup`/`runAsUser`, and
  confirm `readOnlyRootFilesystem: true` is compatible with the data-dir write path
  (data is on the PVC mount, so root FS should be safe — verify with a `kind` run).
- Confirm the exact **OTLP endpoint env var names** for the tracing passthrough (from #442).

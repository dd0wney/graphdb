# graphdb Helm chart (single-node)

Deploys a single-node graphdb (StatefulSet + PVC) on Kubernetes ≥1.23.
Clustering/HA is planned for v2.0; `replicaCount` must be 1.

## Install

```bash
helm install graphdb ./deployments/helm/graphdb \
  --namespace graphdb --create-namespace \
  --set config.edition=community
```

## Common values

| Key | Default | Notes |
|---|---|---|
| `image.repository` | `dd0wney/graphdb` | override for your registry |
| `image.tag` | `.Chart.AppVersion` | pin your graphdb version |
| `persistence.size` | `10Gi` | snapshot volume |
| `config.storageMode` | `""` (mmap) | `json` to opt out of mmap |
| `secrets.jwtSecret` / `secrets.existingSecret` | `""` / `""` | a managed Secret with an auto-generated, upgrade-persistent `JWT_SECRET` is created by default; set `jwtSecret` to pin it or `existingSecret` to bring your own (must include `JWT_SECRET`) |
| `config.tls.enabled` | `false` | in-server TLS |
| `ingress.enabled` | `false` | HTTP ingress |
| `serviceMonitor.enabled` | `false` | Prometheus Operator |

Full surface: `values.yaml` (validated by `values.schema.json`).

**GitOps note:** the auto-generated `JWT_SECRET` persists across `helm upgrade`
via a cluster `lookup`. GitOps tooling that renders without cluster reads
(e.g. ArgoCD with `lookup` disabled, or `helm template` pipelines) will
regenerate it on every render and invalidate sessions — set `secrets.jwtSecret`
explicitly or use `secrets.existingSecret` in those setups.

## Verify

```bash
helm test graphdb -n graphdb        # curls /health
```

Note: with `helm install graphdb ...`, the release name already contains the
chart name, so `graphdb.fullname` collapses to `graphdb` — the StatefulSet
and Service are named `graphdb`, not `graphdb-graphdb`. Adjust the object
name in commands below if you install under a different release name.

## Manual smoke test (kind)

There's no live Kubernetes cluster in CI for this chart, so this is a
repeatable manual runbook rather than an automated test. Run it after any
change to the chart templates and capture the output:

```bash
kind create cluster --name graphdb-smoke
# load the locally-built image if not pulling from a registry:
#   docker build -t dd0wney/graphdb:local . && kind load docker-image dd0wney/graphdb:local --name graphdb-smoke
helm install graphdb ./deployments/helm/graphdb \
  --namespace graphdb --create-namespace \
  --set config.edition=community --set persistence.size=1Gi \
  --set image.tag=local --set image.pullPolicy=IfNotPresent
kubectl -n graphdb rollout status statefulset/graphdb --timeout=180s
helm test graphdb -n graphdb
kubectl -n graphdb delete pod -l app.kubernetes.io/name=graphdb --ignore-not-found
kind delete cluster --name graphdb-smoke
```

Expected: rollout completes; `helm test` reports the test-connection pod
`Succeeded` (curl of `/health` returns 200).

If the pod cannot start (e.g. `readOnlyRootFilesystem` blocks an unexpected
write path, or the license gate rejects `community`), that is a real
finding — don't paper over it. Capture `kubectl -n graphdb logs`, fix the
chart (e.g. add the needed writable mount, or set the right env), and
re-run. Only treat the chart as verified after this passes.

See also `deployments/terraform/graphdb/examples/kind/` for the equivalent
smoke test driven through the Terraform wrapper.

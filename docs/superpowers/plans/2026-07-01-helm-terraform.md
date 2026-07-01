# Helm Chart + Terraform Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a single-node Kubernetes Helm chart for graphdb plus a thin, provider-agnostic Terraform wrapper, closing the #1 "can't deploy on k8s" adoption gap.

**Architecture:** graphdb is stateful and 100% env-var configured, so the chart is a StatefulSet+PVC whose values map 1:1 onto the server's env vars (ConfigMap for non-secret, Secret for license/encryption/TLS). The Terraform module is a `helm_release` over the in-repo chart against a user-supplied cluster. Zero Go/API/on-disk changes; one Dockerfile change (pin a numeric uid so `runAsNonRoot` works in k8s).

**Tech Stack:** Helm 3 (v3.16 present), Terraform ≥1.5 (helm + kubernetes providers), kind (present) for the manual smoke runbook.

**Spec:** `docs/superpowers/specs/2026-07-01-helm-terraform-design.md`

## Global Constraints

- **Additive only** — v1.3 is a MINOR under `STABILITY_POLICY.md`. No Go/API/on-disk changes. The only non-chart change is pinning a numeric uid in `Dockerfile`.
- **Single node** — `replicaCount` is fixed at 1. `values.schema.json` sets `maximum: 1` AND a template `fail` rejects `> 1` with a "clustering is v2.0" message. Clustering/HA is v2.0.
- **Chart location** — `deployments/helm/graphdb`. **Terraform location** — `deployments/terraform/graphdb`.
- **In-repo distribution** — no OCI/gh-pages publishing this cycle. TF chart source defaults to the in-repo path, overridable via a variable.
- **Server facts (verbatim):** listens on `8080`; unauthenticated `/health`; data dir `/data`; container user `graphdb`; env vars `PORT`, `GRAPHDB_STORAGE_MODE`, `GRAPHDB_EDITION`, `GRAPHDB_LICENSE`, `ENCRYPTION_ENABLED`, `ENCRYPTION_MASTER_KEY`, `TLS_ENABLED`, `TLS_AUTO_GENERATE`, `TLS_HOSTS`, `GRAPHDB_TRACING_ENABLED`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_EXPORTER`, `OTEL_SERVICE_NAME`.
- **Pinned uid/gid:** `10001` (chosen; unused by base alpine).
- **Image default:** `dd0wney/graphdb` (Docker Hub — the release target; users override for their registry). Tag defaults to `.Chart.AppVersion`.
- **Commit style:** conventional commits, `feat(helm):` / `feat(terraform):` / `ci:` / `docs:`; end body with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Branch:** `v1.3/helm-terraform` (already created; the design spec is the first commit).
- **Build note:** never `go build ./...` locally (gitignored `enterprise-plugins/` breaks it). This plan changes no Go, so no Go build is needed.

---

### Task 1: Pin a numeric uid/gid in the Dockerfile

**Why:** BusyBox `adduser -S graphdb` assigns a nondeterministic uid and leaves `USER` non-numeric. Kubernetes `runAsNonRoot: true` **fails to start** a container whose image `USER` is a name it can't resolve to a number. Pinning `10001` makes the image k8s-safe and lets the chart set `runAsUser: 10001`.

**Files:**
- Modify: `Dockerfile:36` (the `addgroup`/`adduser` line)

**Interfaces:**
- Produces: image runs as numeric uid **10001**, gid **10001**; `/data` owned by `10001:10001`. Task 3 consumes these numbers in the chart `securityContext`.

- [ ] **Step 1: Read the current lines to anchor the edit**

Run: `grep -n "addgroup\|adduser\|chown" Dockerfile`
Expected: line 36 `RUN addgroup -S graphdb && adduser -S graphdb -G graphdb` and line 48 chown.

- [ ] **Step 2: Edit the Dockerfile to pin uid/gid**

Replace the `addgroup`/`adduser` line with an explicit numeric group and user:

```dockerfile
# Add non-root user with a fixed numeric uid/gid so Kubernetes runAsNonRoot works
RUN addgroup -S -g 10001 graphdb && adduser -S -u 10001 -G graphdb graphdb
```

(Leave the existing `chown -R graphdb:graphdb /data /app` and `USER graphdb` lines as-is — they now resolve to 10001.)

- [ ] **Step 3: Build the image and verify the numeric uid**

Run:
```bash
docker build -t graphdb:uidcheck . && \
docker run --rm --entrypoint id graphdb:uidcheck
```
Expected: `uid=10001(graphdb) gid=10001(graphdb) ...`

> If Docker is unavailable in this environment, skip the build and verify statically: `grep -n "10001" Dockerfile` must show both `-g 10001` and `-u 10001`. Record in the commit body that the numeric-uid runtime check is deferred to CI's docker-publish build.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile
git commit -m "$(cat <<'EOF'
build(docker): pin graphdb uid/gid to 10001 for k8s runAsNonRoot

BusyBox `adduser -S` assigns a nondeterministic uid and a non-numeric
USER, which makes Kubernetes `runAsNonRoot: true` refuse to start the
container. Pinning 10001 lets the Helm chart set runAsUser/fsGroup.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Chart scaffold + core StatefulSet/Service/ConfigMap

**Files:**
- Create: `deployments/helm/graphdb/Chart.yaml`
- Create: `deployments/helm/graphdb/values.yaml`
- Create: `deployments/helm/graphdb/templates/_helpers.tpl`
- Create: `deployments/helm/graphdb/templates/configmap.yaml`
- Create: `deployments/helm/graphdb/templates/service.yaml`
- Create: `deployments/helm/graphdb/templates/statefulset.yaml`

**Interfaces:**
- Produces: chart name helpers `graphdb.fullname`, `graphdb.name`, `graphdb.labels`, `graphdb.selectorLabels`, `graphdb.serviceAccountName`; a ConfigMap named `<fullname>-config`; a StatefulSet `<fullname>` with `serviceName: <fullname>`; a headless-capable ClusterIP Service `<fullname>` on port 8080. Later tasks add volumeClaimTemplate, probes, securityContext (Task 3), envFrom Secret (Task 4), schema/guard (Task 5), optional resources (Task 6).

- [ ] **Step 1: Write the failing render assertion (as a shell check)**

There is no `helm` chart yet, so the render must fail. Run:
```bash
helm lint deployments/helm/graphdb
```
Expected: FAIL — `Error: ... no such file or directory` (chart doesn't exist).

- [ ] **Step 2: Create `Chart.yaml`**

```yaml
apiVersion: v2
name: graphdb
description: Single-node Helm chart for graphdb, a GA graph database.
type: application
# Chart version is independent of the app; bump on chart changes.
version: 0.1.0
# Tracks the graphdb release this chart is validated against. Bump on release.
appVersion: "1.0.0"
home: https://github.com/dd0wney/graphdb
sources:
  - https://github.com/dd0wney/graphdb
maintainers:
  - name: dd0wney
kubeVersion: ">=1.23.0-0"
```

- [ ] **Step 3: Create `values.yaml`**

```yaml
# Default values for graphdb (single-node).
image:
  # Docker Hub release target. Override for your own registry / air-gapped mirror.
  repository: dd0wney/graphdb
  tag: ""            # defaults to .Chart.AppVersion
  pullPolicy: IfNotPresent
imagePullSecrets: []

nameOverride: ""
fullnameOverride: ""

# Single node only. clustering/HA is v2.0. Values > 1 are rejected (schema + template).
replicaCount: 1

resources: {}
nodeSelector: {}
tolerations: []
affinity: {}
podAnnotations: {}

persistence:
  enabled: true
  size: 10Gi
  storageClass: ""        # "" = cluster default
  accessMode: ReadWriteOnce

service:
  type: ClusterIP
  port: 8080

# Non-secret environment (rendered into a ConfigMap).
config:
  storageMode: ""         # "" = mmap default; "json" opts out
  edition: ""             # e.g. "community" to run unlicensed
  tracing:
    enabled: false
    otlpEndpoint: ""      # OTEL_EXPORTER_OTLP_ENDPOINT
    tracesExporter: ""    # OTEL_TRACES_EXPORTER
    serviceName: ""       # OTEL_SERVICE_NAME
  tls:
    enabled: false
    autoGenerate: true
    hosts: ""
  extraEnv: {}            # escape hatch: map of extra non-secret env vars

# Secret material. See Task 4 semantics.
secrets:
  create: false
  existingSecret: ""
  license: ""             # GRAPHDB_LICENSE
  encryptionEnabled: false
  encryptionMasterKey: "" # ENCRYPTION_MASTER_KEY

serviceAccount:
  create: true
  name: ""
  annotations: {}

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 10001
  runAsGroup: 10001
  fsGroup: 10001
securityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]

probes:
  liveness:
    path: /health
    initialDelaySeconds: 10
    periodSeconds: 10
  readiness:
    path: /health
    initialDelaySeconds: 5
    periodSeconds: 10

ingress:
  enabled: false
  className: ""
  annotations: {}
  hosts: []               # [{host: graphdb.example.com, paths: [{path: /, pathType: Prefix}]}]
  tls: []

serviceMonitor:
  enabled: false
  interval: 30s

podDisruptionBudget:
  enabled: false
  minAvailable: 1
```

- [ ] **Step 4: Create `templates/_helpers.tpl`**

```yaml
{{- define "graphdb.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "graphdb.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "graphdb.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "graphdb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "graphdb.selectorLabels" -}}
app.kubernetes.io/name: {{ include "graphdb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "graphdb.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "graphdb.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
```

- [ ] **Step 5: Create `templates/configmap.yaml`**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "graphdb.fullname" . }}-config
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
data:
  PORT: {{ .Values.service.port | quote }}
  {{- with .Values.config.storageMode }}
  GRAPHDB_STORAGE_MODE: {{ . | quote }}
  {{- end }}
  {{- with .Values.config.edition }}
  GRAPHDB_EDITION: {{ . | quote }}
  {{- end }}
  {{- if .Values.config.tracing.enabled }}
  GRAPHDB_TRACING_ENABLED: "true"
  {{- with .Values.config.tracing.otlpEndpoint }}
  OTEL_EXPORTER_OTLP_ENDPOINT: {{ . | quote }}
  {{- end }}
  {{- with .Values.config.tracing.tracesExporter }}
  OTEL_TRACES_EXPORTER: {{ . | quote }}
  {{- end }}
  {{- with .Values.config.tracing.serviceName }}
  OTEL_SERVICE_NAME: {{ . | quote }}
  {{- end }}
  {{- end }}
  {{- if .Values.config.tls.enabled }}
  TLS_ENABLED: "true"
  TLS_AUTO_GENERATE: {{ .Values.config.tls.autoGenerate | quote }}
  {{- with .Values.config.tls.hosts }}
  TLS_HOSTS: {{ . | quote }}
  {{- end }}
  {{- end }}
  {{- range $k, $v := .Values.config.extraEnv }}
  {{ $k }}: {{ $v | quote }}
  {{- end }}
```

- [ ] **Step 6: Create `templates/service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "graphdb.fullname" . }}
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - name: http
      port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
  selector:
    {{- include "graphdb.selectorLabels" . | nindent 4 }}
```

- [ ] **Step 7: Create `templates/statefulset.yaml` (core; hardened further in Task 3)**

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ include "graphdb.fullname" . }}
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
spec:
  serviceName: {{ include "graphdb.fullname" . }}
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "graphdb.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      {{- with .Values.podAnnotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      labels:
        {{- include "graphdb.selectorLabels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "graphdb.serviceAccountName" . }}
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: graphdb
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          args: ["--data", "/data"]
          ports:
            - name: http
              containerPort: {{ .Values.service.port }}
              protocol: TCP
          envFrom:
            - configMapRef:
                name: {{ include "graphdb.fullname" . }}-config
          volumeMounts:
            - name: data
              mountPath: /data
          {{- with .Values.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
  {{- if .Values.persistence.enabled }}
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["{{ .Values.persistence.accessMode }}"]
        {{- if .Values.persistence.storageClass }}
        storageClassName: {{ .Values.persistence.storageClass | quote }}
        {{- end }}
        resources:
          requests:
            storage: {{ .Values.persistence.size | quote }}
  {{- end }}
```

- [ ] **Step 8: Run lint + render, verify they pass**

Run:
```bash
helm lint deployments/helm/graphdb && \
helm template t deployments/helm/graphdb | grep -E "kind: (StatefulSet|Service|ConfigMap)" && \
helm template t deployments/helm/graphdb | grep -E "replicas: 1"
```
Expected: `1 chart(s) linted, 0 chart(s) failed`; three `kind:` lines; `replicas: 1`.

- [ ] **Step 9: Commit**

```bash
git add deployments/helm/graphdb
git commit -m "$(cat <<'EOF'
feat(helm): scaffold chart with core StatefulSet/Service/ConfigMap

Single-node StatefulSet (replicas:1), ClusterIP service on 8080, and a
ConfigMap mapping non-secret values to the server's env vars.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Harden the pod — persistence probes + securityContext

**Files:**
- Modify: `deployments/helm/graphdb/templates/statefulset.yaml` (add probes, pod+container securityContext, a `/tmp` emptyDir so `readOnlyRootFilesystem` works)

**Interfaces:**
- Consumes: `podSecurityContext` (uid/gid 10001 from Task 1), `securityContext`, `probes` from values.
- Produces: pod with liveness/readiness on `/health`, non-root uid 10001, read-only root FS + writable `/tmp` emptyDir.

- [ ] **Step 1: Add the failing assertion**

Run:
```bash
helm template t deployments/helm/graphdb | grep -E "readOnlyRootFilesystem: true"
```
Expected: FAIL (no output; grep exits 1) — securityContext not wired yet.

- [ ] **Step 2: Add `securityContext` + probes + `/tmp` volume to the StatefulSet**

In `templates/statefulset.yaml`, inside `spec.template.spec`, add the pod securityContext right after `serviceAccountName`:

```yaml
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
```

In the container block, add after `imagePullPolicy`:

```yaml
          securityContext:
            {{- toYaml .Values.securityContext | nindent 12 }}
```

Add probes after the `ports:` block:

```yaml
          livenessProbe:
            httpGet:
              path: {{ .Values.probes.liveness.path }}
              port: http
            initialDelaySeconds: {{ .Values.probes.liveness.initialDelaySeconds }}
            periodSeconds: {{ .Values.probes.liveness.periodSeconds }}
          readinessProbe:
            httpGet:
              path: {{ .Values.probes.readiness.path }}
              port: http
            initialDelaySeconds: {{ .Values.probes.readiness.initialDelaySeconds }}
            periodSeconds: {{ .Values.probes.readiness.periodSeconds }}
```

Add a `/tmp` mount to the existing `volumeMounts` (so read-only root FS still allows scratch writes):

```yaml
            - name: tmp
              mountPath: /tmp
```

Add a pod-level `volumes` block (before `nodeSelector`) for the emptyDir:

```yaml
      volumes:
        - name: tmp
          emptyDir: {}
```

- [ ] **Step 3: Verify the assertions pass**

Run:
```bash
helm template t deployments/helm/graphdb | grep -E "readOnlyRootFilesystem: true" && \
helm template t deployments/helm/graphdb | grep -E "runAsUser: 10001" && \
helm template t deployments/helm/graphdb | grep -E "path: /health" && \
helm lint deployments/helm/graphdb
```
Expected: each grep prints a match; lint passes.

- [ ] **Step 4: Commit**

```bash
git add deployments/helm/graphdb/templates/statefulset.yaml
git commit -m "$(cat <<'EOF'
feat(helm): harden pod (probes, non-root securityContext, ro-rootfs)

/health liveness+readiness, runAsNonRoot uid 10001, readOnlyRootFilesystem
with a writable /tmp emptyDir, drop-all capabilities.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Secret template (create-or-reference) + envFrom wiring

**Files:**
- Create: `deployments/helm/graphdb/templates/secret.yaml`
- Modify: `deployments/helm/graphdb/templates/statefulset.yaml` (add a second `envFrom` secretRef when secrets are configured)

**Interfaces:**
- Consumes: `secrets.{create,existingSecret,license,encryptionEnabled,encryptionMasterKey}`.
- Produces: when `secrets.create`, a Secret `<fullname>-secret` with keys `GRAPHDB_LICENSE`, `ENCRYPTION_ENABLED`, `ENCRYPTION_MASTER_KEY` (only the set ones). The StatefulSet gains `envFrom: secretRef` pointing at `existingSecret` if set, else `<fullname>-secret` if `create`, else nothing.

- [ ] **Step 1: Add failing assertions**

Run:
```bash
helm template t deployments/helm/graphdb --set secrets.create=true --set secrets.license=abc | grep -E "kind: Secret"
```
Expected: FAIL (no Secret template yet).

- [ ] **Step 2: Create `templates/secret.yaml`**

```yaml
{{- if .Values.secrets.create }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "graphdb.fullname" . }}-secret
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
type: Opaque
stringData:
  {{- with .Values.secrets.license }}
  GRAPHDB_LICENSE: {{ . | quote }}
  {{- end }}
  {{- if .Values.secrets.encryptionEnabled }}
  ENCRYPTION_ENABLED: "true"
  {{- with .Values.secrets.encryptionMasterKey }}
  ENCRYPTION_MASTER_KEY: {{ . | quote }}
  {{- end }}
  {{- end }}
{{- end }}
```

- [ ] **Step 3: Wire `envFrom` secretRef into the StatefulSet**

In `templates/statefulset.yaml`, replace the existing single-item `envFrom` block with one that conditionally appends the Secret:

```yaml
          envFrom:
            - configMapRef:
                name: {{ include "graphdb.fullname" . }}-config
            {{- if .Values.secrets.existingSecret }}
            - secretRef:
                name: {{ .Values.secrets.existingSecret }}
            {{- else if .Values.secrets.create }}
            - secretRef:
                name: {{ include "graphdb.fullname" . }}-secret
            {{- end }}
```

- [ ] **Step 4: Verify the three permutations**

Run:
```bash
# create=true -> Secret rendered + secretRef to <fullname>-secret
helm template t deployments/helm/graphdb --set secrets.create=true --set secrets.license=abc | grep -E "kind: Secret" && \
helm template t deployments/helm/graphdb --set secrets.create=true --set secrets.license=abc | grep -E "name: t-graphdb-secret"
# existingSecret -> no Secret rendered, secretRef to the named secret
helm template t deployments/helm/graphdb --set secrets.existingSecret=mysec | grep -E "secretRef" | grep -q "mysec" && echo "existingSecret OK"
test -z "$(helm template t deployments/helm/graphdb --set secrets.existingSecret=mysec | grep 'kind: Secret')" && echo "no Secret rendered OK"
# default -> no Secret, no secretRef
test -z "$(helm template t deployments/helm/graphdb | grep 'secretRef')" && echo "default no-secret OK"
helm lint deployments/helm/graphdb
```
Expected: `kind: Secret` + `name: t-graphdb-secret`; `existingSecret OK`; `no Secret rendered OK`; `default no-secret OK`; lint passes.

- [ ] **Step 5: Commit**

```bash
git add deployments/helm/graphdb/templates/secret.yaml deployments/helm/graphdb/templates/statefulset.yaml
git commit -m "$(cat <<'EOF'
feat(helm): secret template with create-or-reference semantics

secrets.create builds a Secret from values (dev); secrets.existingSecret
references a pre-provisioned one (prod). License/encryption key never land
in the ConfigMap.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: values.schema.json + replicaCount fail-guard + NOTES + helm test

**Files:**
- Create: `deployments/helm/graphdb/values.schema.json`
- Modify: `deployments/helm/graphdb/templates/statefulset.yaml` (add `fail` guard at top)
- Create: `deployments/helm/graphdb/templates/NOTES.txt`
- Create: `deployments/helm/graphdb/templates/tests/test-connection.yaml`

**Interfaces:**
- Produces: install-time rejection of `replicaCount > 1` and of unknown/mistyped values; a `helm test` pod that curls `/health`.

- [ ] **Step 1: Add failing assertions**

Run:
```bash
helm template t deployments/helm/graphdb --set replicaCount=2
```
Expected: currently renders `replicas: 2` (no guard yet) — this is the bug we fix.

- [ ] **Step 2: Add the `fail` guard to the top of `statefulset.yaml`**

Insert as the very first lines of `templates/statefulset.yaml` (above `apiVersion`):

```yaml
{{- if gt (int .Values.replicaCount) 1 }}
{{- fail "graphdb is single-node in v1.x: replicaCount must be 1. Clustering/HA is planned for v2.0 (see ROADMAP_post_1.0.md). Running multiple replicas behind one Service each write an independent snapshot and WILL corrupt/diverge your data." }}
{{- end }}
```

- [ ] **Step 3: Create `values.schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 1, "maximum": 1 },
    "image": {
      "type": "object",
      "properties": {
        "repository": { "type": "string", "minLength": 1 },
        "tag": { "type": "string" },
        "pullPolicy": { "enum": ["Always", "IfNotPresent", "Never"] }
      },
      "required": ["repository"]
    },
    "persistence": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" },
        "size": { "type": "string" },
        "storageClass": { "type": "string" },
        "accessMode": { "type": "string" }
      }
    },
    "service": {
      "type": "object",
      "properties": {
        "type": { "type": "string" },
        "port": { "type": "integer", "minimum": 1, "maximum": 65535 }
      }
    },
    "config": {
      "type": "object",
      "properties": {
        "storageMode": { "enum": ["", "mmap", "json"] }
      }
    },
    "secrets": {
      "type": "object",
      "properties": {
        "create": { "type": "boolean" },
        "existingSecret": { "type": "string" }
      }
    }
  }
}
```

- [ ] **Step 4: Create `templates/NOTES.txt`**

```txt
graphdb has been deployed as release {{ .Release.Name }} (single node).

1. Wait for the pod to become ready:
   kubectl -n {{ .Release.Namespace }} rollout status statefulset/{{ include "graphdb.fullname" . }}

2. Reach the API (port-forward):
   kubectl -n {{ .Release.Namespace }} port-forward svc/{{ include "graphdb.fullname" . }} {{ .Values.service.port }}:{{ .Values.service.port }}
   curl http://localhost:{{ .Values.service.port }}/health

{{- if .Values.ingress.enabled }}

3. Ingress is enabled at:
{{- range .Values.ingress.hosts }}
   http{{ if $.Values.ingress.tls }}s{{ end }}://{{ .host }}
{{- end }}
{{- end }}

Storage mode: {{ .Values.config.storageMode | default "mmap (default)" }}.
Clustering/HA is not supported in v1.x — this is a single-node release.
```

- [ ] **Step 5: Create `templates/tests/test-connection.yaml`**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: {{ include "graphdb.fullname" . }}-test-connection
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": test
spec:
  restartPolicy: Never
  containers:
    - name: curl
      image: curlimages/curl:8.10.1
      command: ["curl", "-fsS"]
      args: ["http://{{ include "graphdb.fullname" . }}:{{ .Values.service.port }}/health"]
```

- [ ] **Step 6: Verify guard + schema + render**

Run:
```bash
# guard rejects >1
helm template t deployments/helm/graphdb --set replicaCount=2 2>&1 | grep -q "single-node" && echo "guard OK"
# schema rejects >1 (schema runs on install/template with values file; --set bypasses some coercion, so test via a values file)
printf 'replicaCount: 5\n' > /tmp/bad-values.yaml
helm template t deployments/helm/graphdb -f /tmp/bad-values.yaml 2>&1 | grep -Eq "schema|single-node" && echo "reject-5 OK"
# schema rejects bad storageMode
printf 'config:\n  storageMode: mmp\n' > /tmp/bad-sm.yaml
helm template t deployments/helm/graphdb -f /tmp/bad-sm.yaml 2>&1 | grep -q "storageMode" && echo "bad-storageMode OK"
# helm test pod + NOTES render
helm template t deployments/helm/graphdb --show-only templates/tests/test-connection.yaml | grep -q "test-connection" && echo "test pod OK"
helm lint deployments/helm/graphdb
rm -f /tmp/bad-values.yaml /tmp/bad-sm.yaml
```
Expected: `guard OK`, `reject-5 OK`, `bad-storageMode OK`, `test pod OK`, lint passes.

- [ ] **Step 7: Commit**

```bash
git add deployments/helm/graphdb/values.schema.json deployments/helm/graphdb/templates/statefulset.yaml deployments/helm/graphdb/templates/NOTES.txt deployments/helm/graphdb/templates/tests/test-connection.yaml
git commit -m "$(cat <<'EOF'
feat(helm): schema validation, replicaCount>1 guard, NOTES, helm test

values.schema.json validates values client-side; a template fail-guard
rejects multi-replica (single-node in v1.x, clustering is v2.0); NOTES.txt
gives reachability; helm test curls /health.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Optional resources — serviceaccount, ingress, servicemonitor, PDB

**Files:**
- Create: `deployments/helm/graphdb/templates/serviceaccount.yaml`
- Create: `deployments/helm/graphdb/templates/ingress.yaml`
- Create: `deployments/helm/graphdb/templates/servicemonitor.yaml`
- Create: `deployments/helm/graphdb/templates/poddisruptionbudget.yaml`

**Interfaces:**
- Consumes: `serviceAccount`, `ingress`, `serviceMonitor`, `podDisruptionBudget` from values.
- Produces: each resource rendered only when its `.enabled`/`.create` flag is set.

- [ ] **Step 1: Add failing assertion**

Run:
```bash
helm template t deployments/helm/graphdb --set ingress.enabled=true --set ingress.hosts[0].host=x.example.com | grep -E "kind: Ingress"
```
Expected: FAIL (no ingress template yet).

- [ ] **Step 2: Create `templates/serviceaccount.yaml`**

```yaml
{{- if .Values.serviceAccount.create }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "graphdb.serviceAccountName" . }}
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
  {{- with .Values.serviceAccount.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
```

- [ ] **Step 3: Create `templates/ingress.yaml`**

```yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "graphdb.fullname" . }}
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
  {{- with .Values.ingress.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- with .Values.ingress.className }}
  ingressClassName: {{ . }}
  {{- end }}
  {{- with .Values.ingress.tls }}
  tls:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  rules:
    {{- range .Values.ingress.hosts }}
    - host: {{ .host | quote }}
      http:
        paths:
          {{- range .paths }}
          - path: {{ .path }}
            pathType: {{ .pathType | default "Prefix" }}
            backend:
              service:
                name: {{ include "graphdb.fullname" $ }}
                port:
                  number: {{ $.Values.service.port }}
          {{- end }}
    {{- end }}
{{- end }}
```

- [ ] **Step 4: Create `templates/servicemonitor.yaml`**

```yaml
{{- if .Values.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "graphdb.fullname" . }}
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "graphdb.selectorLabels" . | nindent 6 }}
  endpoints:
    - port: http
      path: /metrics
      interval: {{ .Values.serviceMonitor.interval }}
{{- end }}
```

- [ ] **Step 5: Create `templates/poddisruptionbudget.yaml`**

```yaml
{{- if .Values.podDisruptionBudget.enabled }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "graphdb.fullname" . }}
  labels:
    {{- include "graphdb.labels" . | nindent 4 }}
spec:
  minAvailable: {{ .Values.podDisruptionBudget.minAvailable }}
  selector:
    matchLabels:
      {{- include "graphdb.selectorLabels" . | nindent 6 }}
{{- end }}
```

- [ ] **Step 6: Verify opt-in rendering**

Run:
```bash
# default: serviceaccount present (create:true), others absent
helm template t deployments/helm/graphdb | grep -q "kind: ServiceAccount" && echo "SA default OK"
test -z "$(helm template t deployments/helm/graphdb | grep -E 'kind: (Ingress|ServiceMonitor|PodDisruptionBudget)')" && echo "opt-ins off by default OK"
# enable each
helm template t deployments/helm/graphdb --set ingress.enabled=true --set 'ingress.hosts[0].host=x.example.com' --set 'ingress.hosts[0].paths[0].path=/' | grep -q "kind: Ingress" && echo "ingress OK"
helm template t deployments/helm/graphdb --set serviceMonitor.enabled=true | grep -q "kind: ServiceMonitor" && echo "SM OK"
helm template t deployments/helm/graphdb --set podDisruptionBudget.enabled=true | grep -q "kind: PodDisruptionBudget" && echo "PDB OK"
helm lint deployments/helm/graphdb
```
Expected: all five `... OK` lines; lint passes.

- [ ] **Step 7: Commit**

```bash
git add deployments/helm/graphdb/templates/serviceaccount.yaml deployments/helm/graphdb/templates/ingress.yaml deployments/helm/graphdb/templates/servicemonitor.yaml deployments/helm/graphdb/templates/poddisruptionbudget.yaml
git commit -m "$(cat <<'EOF'
feat(helm): opt-in serviceaccount, ingress, servicemonitor, PDB

All off by default (serviceAccount.create defaults true); each renders only
when its flag is set.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Terraform thin Helm-release module + kind example

**Files:**
- Create: `deployments/terraform/graphdb/versions.tf`
- Create: `deployments/terraform/graphdb/variables.tf`
- Create: `deployments/terraform/graphdb/main.tf`
- Create: `deployments/terraform/graphdb/outputs.tf`
- Create: `deployments/terraform/graphdb/README.md`
- Create: `deployments/terraform/graphdb/examples/kind/main.tf`
- Create: `deployments/terraform/graphdb/examples/kind/README.md`

**Interfaces:**
- Consumes: the chart at `deployments/helm/graphdb` (via `chart_path` var).
- Produces: a `helm_release.graphdb` and outputs `release_name`, `namespace`, `status`.

> **Env note:** `terraform` is not installed in this environment. Verify with `terraform fmt -check` + `terraform validate` in CI (Task 8) and via the kind runbook (Task 9). If you want a local check first: install Terraform ≥1.5, then run the commands in Step 6.

- [ ] **Step 1: Create `versions.tf`**

```hcl
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    helm = {
      source  = "hashicorp/helm"
      version = ">= 2.12.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
  }
}
```

- [ ] **Step 2: Create `variables.tf`**

```hcl
variable "release_name" {
  description = "Helm release name."
  type        = string
  default     = "graphdb"
}

variable "namespace" {
  description = "Kubernetes namespace to deploy into."
  type        = string
  default     = "graphdb"
}

variable "create_namespace" {
  description = "Create the namespace if it does not exist."
  type        = bool
  default     = true
}

variable "chart_path" {
  description = "Path to the graphdb Helm chart. Defaults to the in-repo chart."
  type        = string
  default     = "../../helm/graphdb"
}

variable "chart_version" {
  description = "Chart version (only used when sourcing from a repo/OCI, not a local path)."
  type        = string
  default     = null
}

variable "values" {
  description = "Raw YAML values passed to the chart (helm -f equivalent)."
  type        = string
  default     = ""
}

variable "set_values" {
  description = "Map of scalar overrides (helm --set equivalent)."
  type        = map(string)
  default     = {}
}
```

- [ ] **Step 3: Create `main.tf`**

```hcl
resource "helm_release" "graphdb" {
  name             = var.release_name
  namespace        = var.namespace
  create_namespace = var.create_namespace

  chart   = var.chart_path
  version = var.chart_version

  values = var.values != "" ? [var.values] : []

  dynamic "set" {
    for_each = var.set_values
    content {
      name  = set.key
      value = set.value
    }
  }
}
```

- [ ] **Step 4: Create `outputs.tf`**

```hcl
output "release_name" {
  description = "The deployed Helm release name."
  value       = helm_release.graphdb.name
}

output "namespace" {
  description = "The namespace graphdb was deployed into."
  value       = helm_release.graphdb.namespace
}

output "status" {
  description = "The Helm release status."
  value       = helm_release.graphdb.status
}
```

- [ ] **Step 5: Create `examples/kind/main.tf`**

```hcl
# Deploys graphdb onto a local kind cluster using the current kubeconfig context.
# Prereqs: `kind create cluster` has run and kubectl points at it.
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    helm       = { source = "hashicorp/helm", version = ">= 2.12.0" }
    kubernetes = { source = "hashicorp/kubernetes", version = ">= 2.25.0" }
  }
}

provider "kubernetes" {
  config_path    = "~/.kube/config"
  config_context = "kind-kind"
}

provider "helm" {
  kubernetes {
    config_path    = "~/.kube/config"
    config_context = "kind-kind"
  }
}

module "graphdb" {
  source     = "../.."
  chart_path = "../../../../helm/graphdb"
  set_values = {
    "config.edition"     = "community"
    "persistence.size"   = "1Gi"
  }
}

output "status" {
  value = module.graphdb.status
}
```

- [ ] **Step 6: Create `README.md` and `examples/kind/README.md`**

`deployments/terraform/graphdb/README.md`:

````markdown
# graphdb Terraform module

Thin wrapper that installs the in-repo graphdb Helm chart onto an **existing**
Kubernetes cluster via `helm_release`. Provider-agnostic — bring your own cluster.

## Usage

```hcl
module "graphdb" {
  source     = "github.com/dd0wney/graphdb//deployments/terraform/graphdb"
  namespace  = "graphdb"
  set_values = {
    "image.tag"        = "1.0.0"
    "persistence.size" = "50Gi"
  }
}
```

Configure the `helm` and `kubernetes` providers to point at your cluster
(kubeconfig, cloud auth, etc.) in your root module.

## Inputs

| Name | Description | Default |
|---|---|---|
| `release_name` | Helm release name | `graphdb` |
| `namespace` | Target namespace | `graphdb` |
| `create_namespace` | Create namespace if absent | `true` |
| `chart_path` | Path to the chart | `../../helm/graphdb` |
| `chart_version` | Chart version (repo/OCI source only) | `null` |
| `values` | Raw YAML values (`-f`) | `""` |
| `set_values` | Scalar overrides (`--set`) | `{}` |

## Outputs

`release_name`, `namespace`, `status`.

## Local kind smoke test

See `examples/kind/`.
````

`deployments/terraform/graphdb/examples/kind/README.md`:

````markdown
# kind smoke test

```bash
kind create cluster
terraform init
terraform apply -auto-approve
kubectl -n graphdb rollout status statefulset/graphdb-graphdb --timeout=180s
kubectl -n graphdb port-forward svc/graphdb-graphdb 8080:8080 &
curl -fsS http://localhost:8080/health
terraform destroy -auto-approve
kind delete cluster
```
````

- [ ] **Step 7: Validate (CI or local if terraform installed)**

If terraform is installed locally:
```bash
cd deployments/terraform/graphdb && terraform fmt -check -recursive && terraform init -backend=false && terraform validate
```
Expected: `fmt` clean; `Success! The configuration is valid.`

If terraform is NOT installed: skip and rely on Task 8's CI job. Record in the commit body that local validation was deferred to CI.

- [ ] **Step 8: Commit**

```bash
git add deployments/terraform/graphdb
git commit -m "$(cat <<'EOF'
feat(terraform): thin helm_release wrapper module + kind example

Provider-agnostic module installs the in-repo chart onto an existing
cluster; chart_path/values/set_values passthrough; kind example runbook.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: CI — helm lint/template + terraform validate/fmt

**Files:**
- Create: `.github/workflows/deploy-artifacts.yml`

**Interfaces:**
- Produces: a CI workflow gating chart + module on every PR touching `deployments/**`.

- [ ] **Step 1: Create `.github/workflows/deploy-artifacts.yml`**

```yaml
name: Deploy Artifacts
on:
  push:
    branches: [main]
    paths: ["deployments/helm/**", "deployments/terraform/**", ".github/workflows/deploy-artifacts.yml"]
  pull_request:
    paths: ["deployments/helm/**", "deployments/terraform/**", ".github/workflows/deploy-artifacts.yml"]

jobs:
  helm:
    name: helm lint + template
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: azure/setup-helm@v4
        with:
          version: v3.16.2
      - name: helm lint
        run: helm lint deployments/helm/graphdb
      - name: helm template (default)
        run: helm template t deployments/helm/graphdb > /dev/null
      - name: helm template (secrets + tls + ingress + servicemonitor)
        run: |
          helm template t deployments/helm/graphdb \
            --set secrets.create=true --set secrets.license=x \
            --set config.tls.enabled=true \
            --set ingress.enabled=true --set 'ingress.hosts[0].host=x.example.com' --set 'ingress.hosts[0].paths[0].path=/' \
            --set serviceMonitor.enabled=true > /dev/null
      - name: replicaCount>1 must be rejected
        run: |
          if helm template t deployments/helm/graphdb --set replicaCount=2 2>/dev/null; then
            echo "ERROR: replicaCount=2 was accepted but must be rejected"; exit 1
          fi
          echo "guard correctly rejected replicaCount=2"

  terraform:
    name: terraform fmt + validate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: "1.9.5"
      - name: fmt
        run: terraform -chdir=deployments/terraform/graphdb fmt -check -recursive
      - name: validate
        run: |
          terraform -chdir=deployments/terraform/graphdb init -backend=false
          terraform -chdir=deployments/terraform/graphdb validate
```

- [ ] **Step 2: Verify the helm half locally (mirrors CI)**

Run:
```bash
helm lint deployments/helm/graphdb && \
helm template t deployments/helm/graphdb > /dev/null && \
if helm template t deployments/helm/graphdb --set replicaCount=2 2>/dev/null; then echo FAIL; else echo "guard OK"; fi
```
Expected: lint passes, template succeeds, `guard OK`. (Terraform half runs in CI only — not installed locally.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/deploy-artifacts.yml
git commit -m "$(cat <<'EOF'
ci: lint+template the helm chart and validate the terraform module

New Deploy Artifacts workflow: helm lint, helm template permutations, a
replicaCount>1 negative test, and terraform fmt/validate. Path-filtered to
deployments/**.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Docs + kind smoke-test runbook

**Files:**
- Create: `deployments/helm/graphdb/README.md`
- Modify: `docs/DEPLOYMENT_GUIDE.md` (add a "Kubernetes (Helm)" section linking the chart + module)

**Interfaces:**
- Produces: user-facing install docs + a repeatable manual smoke-test runbook (per the repo's unverifiable-work rule — a live cluster isn't guaranteed in CI).

- [ ] **Step 1: Create `deployments/helm/graphdb/README.md`**

````markdown
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
| `secrets.create` / `secrets.existingSecret` | `false` / `""` | license + encryption key |
| `config.tls.enabled` | `false` | in-server TLS |
| `ingress.enabled` | `false` | HTTP ingress |
| `serviceMonitor.enabled` | `false` | Prometheus Operator |

Full surface: `values.yaml` (validated by `values.schema.json`).

## Verify

```bash
helm test graphdb -n graphdb        # curls /health
```
````

- [ ] **Step 2: Add a Kubernetes section to `docs/DEPLOYMENT_GUIDE.md`**

Append (adjust the anchor to the file's existing structure):

```markdown
## Kubernetes (Helm)

A single-node Helm chart lives at `deployments/helm/graphdb` and a thin
Terraform wrapper at `deployments/terraform/graphdb`.

```bash
helm install graphdb ./deployments/helm/graphdb \
  --namespace graphdb --create-namespace --set config.edition=community
kubectl -n graphdb rollout status statefulset/graphdb-graphdb
```

Clustering/HA is v2.0 — `replicaCount` is fixed at 1. See the chart README
for the values surface and the Terraform module README for IaC usage.
```

- [ ] **Step 3: Run the manual kind smoke test (records real evidence)**

`kind` is available in this environment. Run and capture output:
```bash
kind create cluster --name graphdb-smoke
# load the locally-built image if not pulling from a registry:
#   docker build -t dd0wney/graphdb:local . && kind load docker-image dd0wney/graphdb:local --name graphdb-smoke
helm install graphdb ./deployments/helm/graphdb \
  --namespace graphdb --create-namespace \
  --set config.edition=community --set persistence.size=1Gi \
  --set image.tag=local --set image.pullPolicy=IfNotPresent
kubectl -n graphdb rollout status statefulset/graphdb-graphdb --timeout=180s
helm test graphdb -n graphdb
kubectl -n graphdb delete pod -l app.kubernetes.io/name=graphdb --ignore-not-found
kind delete cluster --name graphdb-smoke
```
Expected: rollout completes; `helm test` reports the test-connection pod `Succeeded` (curl of `/health` returns 200).

> If the pod cannot start (e.g. readOnlyRootFilesystem blocks an unexpected write path, or the license gate rejects `community`), that is a real finding — do NOT paper over it. Capture the `kubectl logs`, fix the chart (e.g. add the needed writable mount, or set the right env), and re-run. Only claim the chart works after this passes.

- [ ] **Step 4: Commit**

```bash
git add deployments/helm/graphdb/README.md docs/DEPLOYMENT_GUIDE.md
git commit -m "$(cat <<'EOF'
docs: helm chart README + Kubernetes section in deployment guide

Install/values docs for the chart and a pointer to the Terraform module;
kind smoke-test runbook.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review

**Spec coverage:**
- Chart layout (Chart.yaml, values, schema, all templates, NOTES, tests) → Tasks 2–6. ✓
- StatefulSet+PVC replicas:1 → Task 2; fail-guard → Task 5. ✓
- Values→ConfigMap+Secret, create-or-reference → Tasks 2 & 4. ✓
- Probes/securityContext/non-root → Tasks 1 & 3. ✓
- Opt-in ingress/servicemonitor/PDB/SA → Task 6. ✓
- Terraform thin wrapper + kind example → Task 7. ✓
- CI (helm lint/template + tf validate/fmt + negative test) → Task 8. ✓
- Docs + manual runbook → Task 9. ✓
- Three open items (image ref, uid/gid, OTLP vars) → resolved in Global Constraints + Tasks 1/2. ✓

**Placeholder scan:** No TBD/TODO; every code step has full content. The only deferred verifications (docker run in Task 1, terraform validate in Task 7) are explicitly gated to CI with a recorded reason, not hand-waved. ✓

**Type/name consistency:** `graphdb.fullname` yields `<release>-graphdb` (e.g. `t-graphdb`, `graphdb-graphdb`); StatefulSet/Service/ConfigMap/Secret names, `serviceName`, and rollout target all use it consistently. Secret name `<fullname>-secret`, ConfigMap `<fullname>-config` used identically in Tasks 2/4. Port name `http` used by Service targetPort, probes, and ServiceMonitor. ✓

**Notable risk to watch during execution:** `readOnlyRootFilesystem: true` + the license `community` edition path — the kind run in Task 9 Step 3 is the real gate; if the server writes anywhere outside `/data` and `/tmp`, that surfaces there and the fix is an added writable mount.

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

**Caveat for remote sourcing**: when sourcing this module remotely (as in the
`source = "github.com/dd0wney/graphdb//..."` example above), the default
`chart_path` (`../../helm/graphdb`) will NOT resolve — the Helm provider
resolves a local chart path relative to the root module's working directory,
not the fetched module's directory. Remote consumers must set `chart_path`
explicitly to a locally-available chart path (or, in the future, an OCI/
`oci://` source).

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

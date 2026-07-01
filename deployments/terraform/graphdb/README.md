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

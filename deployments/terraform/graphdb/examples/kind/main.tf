# Deploys graphdb onto a local kind cluster using the current kubeconfig context.
# Prereqs: `kind create cluster` has run and kubectl points at it.
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    helm       = { source = "hashicorp/helm", version = ">= 2.12.0, < 3.0.0" }
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
    "config.edition"   = "community"
    "persistence.size" = "1Gi"
  }
}

output "status" {
  value = module.graphdb.status
}

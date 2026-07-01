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

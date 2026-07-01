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

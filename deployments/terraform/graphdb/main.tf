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

# kind smoke test

```bash
cd deployments/terraform/graphdb/examples/kind
kind create cluster
terraform init
terraform apply -auto-approve
kubectl -n graphdb rollout status statefulset/graphdb --timeout=180s
kubectl -n graphdb port-forward svc/graphdb 8080:8080 &
curl -fsS http://localhost:8080/health
terraform destroy -auto-approve
kind delete cluster
```

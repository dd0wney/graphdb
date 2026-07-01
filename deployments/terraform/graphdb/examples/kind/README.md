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

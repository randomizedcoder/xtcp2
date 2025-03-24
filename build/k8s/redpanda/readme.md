# redpanda

readme

https://docs.redpanda.com/current/deploy/deployment-option/self-hosted/kubernetes/k-production-deployment/

helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --set crds.enabled=true \
  --namespace cert-manager  \
  --create-namespace

kubectl kustomize "https://github.com/redpanda-data/redpanda-operator//operator/config/crd?ref=v2.3.5-24.3.2" | kubectl apply --server-side -f -

helm repo add redpanda https://charts.redpanda.com
helm upgrade --install redpanda-controller redpanda/operator \
  --namespace redpanda \
  --create-namespace \
  --values redpanda-operator-values.yaml

kubectl --namespace redpanda rollout status --watch deployment/redpanda-controller-operator

kubectl apply -n redpanda -f redpanda-cluster.yaml
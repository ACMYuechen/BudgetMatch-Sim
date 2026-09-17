#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

if [[ "$EUID" -ne 0 ]]; then
  echo 'Run this script as root on the existing BudgetMatch K3s server.' >&2
  exit 1
fi
for command in k3s curl sha256sum tar; do
  command -v "$command" >/dev/null
done

kubectl() { k3s kubectl "$@"; }
kubectl -n budgetmatch-sim get secret runtime -o name >/dev/null
kubectl -n budgetmatch-sim get pvc agent-workspace -o name >/dev/null
for deployment in postgres auth-rpc mall-rpc seckill-rpc agent-rpc payment-rpc app admin web-ui; do
  kubectl -n budgetmatch-sim get deployment "$deployment" -o name >/dev/null
done
if [[ "$(uname -m)" != x86_64 ]]; then
  echo 'This deployment is configured for the existing amd64 server only.' >&2
  exit 1
fi

runtime_dir="$(mktemp -d)"
trap 'rm -rf "$runtime_dir"' EXIT
backup_dir="/var/backups/budgetmatch-gitops/$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 700 "$backup_dir"
kubectl -n budgetmatch-sim get deployments,services,configmaps,persistentvolumeclaims -o yaml >"$backup_dir/resources.yaml"
kubectl -n budgetmatch-sim get secret runtime -o yaml >"$backup_dir/runtime-secret.yaml"
kubectl -n budgetmatch-sim exec deployment/postgres -- \
  pg_dump -U budgetmatch -d budgetmatch-sim -Fc >"$backup_dir/database.dump"
echo "Existing application configuration and database backed up to $backup_dir."

download() {
  local address="$1" destination="$2" checksum="$3"
  curl --fail --silent --show-error --location --retry 3 --max-time 300 "$address" -o "$destination"
  printf '%s  %s\n' "$checksum" "$destination" | sha256sum --check --status
}

if [[ -z "$(kubectl -n argocd get deployment argocd-server --ignore-not-found -o name)" ]]; then
  download \
    'https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.3/manifests/install.yaml' \
    "$runtime_dir/install.yaml" \
    '7efe2d6bbc03f63623640f1e4198f16c84009d510fb810ef71e56df1b7614ba9'
  cat >"$runtime_dir/kustomization.yaml" <<'YAML'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: argocd
resources:
  - install.yaml
replicas:
  - name: argocd-dex-server
    count: 0
  - name: argocd-applicationset-controller
    count: 0
  - name: argocd-notifications-controller
    count: 0
patches:
  - target:
      kind: Deployment
      name: argocd-(server|repo-server)
    patch: |-
      - op: add
        path: /spec/template/spec/containers/0/resources
        value:
          requests: {cpu: 25m, memory: 64Mi}
          limits: {memory: 256Mi}
  - target:
      kind: Deployment
      name: argocd-redis
    patch: |-
      - op: add
        path: /spec/template/spec/containers/0/resources
        value:
          requests: {cpu: 25m, memory: 32Mi}
          limits: {memory: 128Mi}
  - target:
      kind: StatefulSet
      name: argocd-application-controller
    patch: |-
      - op: add
        path: /spec/template/spec/containers/0/resources
        value:
          requests: {cpu: 50m, memory: 128Mi}
          limits: {memory: 512Mi}
  - target:
      kind: ConfigMap
      name: argocd-cmd-params-cm
    patch: |-
      - op: add
        path: /data
        value:
          controller.operation.processors: "2"
          controller.status.processors: "5"
          reposerver.parallelism.limit: "1"
YAML
  kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply --server-side -k "$runtime_dir"
else
  echo 'Argo CD already exists; its installation and settings will not be overwritten.'
fi

if [[ -z "$(kubectl -n kube-system get deployment sealed-secrets-controller --ignore-not-found -o name)" ]]; then
  download \
    'https://github.com/bitnami/sealed-secrets/releases/download/v0.40.0/controller.yaml' \
    "$runtime_dir/sealed-secrets.yaml" \
    'ac8aabccdd9110430d117501705818115b2ab0993700a9c118d4d42cf4015a17'
  kubectl apply --server-side -f "$runtime_dir/sealed-secrets.yaml"
  kubectl -n kube-system set resources deployment sealed-secrets-controller \
    --requests=cpu=25m,memory=32Mi --limits=memory=128Mi
else
  echo 'Sealed Secrets already exists; its installation and keys will not be overwritten.'
fi

for deployment in argocd-server argocd-repo-server argocd-redis; do
  kubectl -n argocd rollout status "deployment/$deployment" --timeout=300s
done
kubectl -n argocd rollout status statefulset/argocd-application-controller --timeout=300s
kubectl -n kube-system rollout status deployment/sealed-secrets-controller --timeout=300s

download \
  'https://github.com/bitnami/sealed-secrets/releases/download/v0.40.0/kubeseal-0.40.0-linux-amd64.tar.gz' \
  "$runtime_dir/kubeseal.tar.gz" \
  '9314c35916646e9d59c8f06b1314574b4e79c4d76f079433607ed7b697bb5eb7'
tar -xzf "$runtime_dir/kubeseal.tar.gz" -C "$runtime_dir" kubeseal
install -m 755 "$runtime_dir/kubeseal" /usr/local/bin/kubeseal
install -d -m 755 /var/lib/budgetmatch-gitops
kubeseal --kubeconfig /etc/rancher/k3s/k3s.yaml \
  --controller-namespace kube-system --controller-name sealed-secrets-controller \
  --fetch-cert >/var/lib/budgetmatch-gitops/sealed-secrets.pem
chmod 644 /var/lib/budgetmatch-gitops/sealed-secrets.pem
kubectl -n kube-system get secret -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml \
  >"$backup_dir/sealing-keys.yaml"
echo "Argo CD and Sealed Secrets are ready. Private sealing keys are backed up in $backup_dir."
echo 'No application has been synced yet. Keep an encrypted off-server copy of the backup.'
echo 'Next: publish a release, verify server image access, then apply deploy/argocd/project.yaml and application.yaml.'

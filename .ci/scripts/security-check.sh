#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly TRIVY_IMAGE="aquasec/trivy:0.73.0@sha256:7cced7cae583819fc7806d4cbc0dbbc7cad18b99f7d3e235192e6da8c091045c"
TRIVY_CACHE_VOLUME=""
SECURITY_FAILED=0

cleanup() {
  if [[ -n "${TRIVY_CACHE_VOLUME}" ]]; then
    docker volume rm "${TRIVY_CACHE_VOLUME}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

run_required() {
  if ! "$@"; then
    SECURITY_FAILED=1
  fi
}

cd "${REPO_ROOT}"

echo "Checking GitHub Actions syntax"
run_required go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7

echo "Scanning the current tree for leaked secrets"
run_required go run github.com/zricethezav/gitleaks/v8@v8.28.0 dir --no-banner --redact .

echo "Checking reachable Go vulnerabilities"
run_required go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

echo "Reporting high-confidence gosec findings (non-blocking during initial adoption)"
run_required go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 \
  -no-fail -severity high -confidence high \
  -exclude-dir=web-ui/node_modules ./...

command -v docker >/dev/null 2>&1 || {
  echo "Docker is required for the Trivy filesystem scan" >&2
  exit 1
}
TRIVY_CACHE_VOLUME="budgetmatch-ci-trivy-$$-${RANDOM}"
docker volume create "${TRIVY_CACHE_VOLUME}" >/dev/null

echo "Reporting High, Medium, and Low filesystem findings (non-blocking)"
run_required docker run --rm \
  --volume "${REPO_ROOT}:/workspace:ro" \
  --volume "${TRIVY_CACHE_VOLUME}:/root/.cache/" \
  --workdir /workspace \
  "${TRIVY_IMAGE}" fs \
  --db-repository ghcr.io/aquasecurity/trivy-db:2 \
  --scanners vuln,misconfig \
  --severity HIGH,MEDIUM,LOW \
  --exit-code 0 \
  --no-progress \
  --skip-check-update \
  --skip-dirs /workspace/web-ui/node_modules \
  --skip-dirs /workspace/web-ui/dist \
  /workspace

echo "Blocking on Critical filesystem vulnerabilities or misconfigurations"
run_required docker run --rm \
  --volume "${REPO_ROOT}:/workspace:ro" \
  --volume "${TRIVY_CACHE_VOLUME}:/root/.cache/" \
  --workdir /workspace \
  "${TRIVY_IMAGE}" fs \
  --db-repository ghcr.io/aquasecurity/trivy-db:2 \
  --scanners vuln,misconfig \
  --severity CRITICAL \
  --exit-code 1 \
  --no-progress \
  --skip-check-update \
  --skip-dirs /workspace/web-ui/node_modules \
  --skip-dirs /workspace/web-ui/dist \
  /workspace

if ((SECURITY_FAILED != 0)); then
  exit 1
fi

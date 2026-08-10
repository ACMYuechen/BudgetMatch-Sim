#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly BUILD_CONTEXT_DIR="$(mktemp -d)"
readonly HADOLINT_IMAGE="hadolint/hadolint:v2.14.0-debian@sha256:158cd0184dcaa18bd8ec20b61f4c1cabdf8b32a592d062f57bdcb8e4c1d312e2"
readonly TRIVY_IMAGE="aquasec/trivy:0.73.0@sha256:7cced7cae583819fc7806d4cbc0dbbc7cad18b99f7d3e235192e6da8c091045c"

declare -a BUILT_IMAGES=()
TRIVY_CACHE_VOLUME=""
CONTAINER_FAILED=0

cleanup() {
  if [[ -n "${TRIVY_CACHE_VOLUME}" ]]; then
    docker volume rm "${TRIVY_CACHE_VOLUME}" >/dev/null 2>&1 || true
  fi
  rm -rf "${BUILD_CONTEXT_DIR}"
}
trap cleanup EXIT

prepare_build_context() {
  local path target_dir

  while IFS= read -r -d '' path; do
    if [[ ! -e "${path}" && ! -L "${path}" ]]; then
      continue
    fi
    target_dir="${BUILD_CONTEXT_DIR}/$(dirname "${path}")"
    mkdir -p "${target_dir}"
    cp -a -- "${path}" "${BUILD_CONTEXT_DIR}/${path}"
  done < <(git ls-files --cached --others --exclude-standard -z)
}

validate_image_tag() {
  if [[ -z "$1" || "$1" == *[!a-zA-Z0-9_.-]* ]]; then
    echo "CI_IMAGE_TAG must contain only letters, digits, dots, underscores, or dashes" >&2
    return 1
  fi
}

build_image() {
  local name="$1"
  local context="$2"
  local dockerfile="$3"
  shift 3

  local image="budgetmatch-sim/${name}:${IMAGE_TAG}"
  local -a build_args=("$@")

  echo "Building ${image}"
  if docker buildx version >/dev/null 2>&1; then
    local -a cache_args=()
    if [[ "${GITHUB_ACTIONS:-false}" == "true" ]]; then
      cache_args+=(
        --cache-from "type=gha,scope=budgetmatch-${name}"
        --cache-to "type=gha,mode=max,scope=budgetmatch-${name}"
      )
    fi
    if ! docker buildx build \
      --file "${dockerfile}" \
      --tag "${image}" \
      --load \
      "${cache_args[@]}" \
      "${build_args[@]}" \
      "${context}"; then
      return 1
    fi
  else
    if ! docker build \
      --file "${dockerfile}" \
      --tag "${image}" \
      "${build_args[@]}" \
      "${context}"; then
      return 1
    fi
  fi

  BUILT_IMAGES+=("${image}")
}

scan_image() {
  local image="$1"

  echo "Reporting High, Medium, and Low vulnerabilities in ${image} (non-blocking)"
  if ! docker run --rm \
    --volume /var/run/docker.sock:/var/run/docker.sock \
    --volume "${TRIVY_CACHE_VOLUME}:/root/.cache/" \
    "${TRIVY_IMAGE}" image \
    --db-repository ghcr.io/aquasecurity/trivy-db:2 \
    --scanners vuln \
    --severity HIGH,MEDIUM,LOW \
    --exit-code 0 \
    --no-progress \
    "${image}"; then
    CONTAINER_FAILED=1
  fi

  echo "Blocking on Critical vulnerabilities in ${image}"
  if ! docker run --rm \
    --volume /var/run/docker.sock:/var/run/docker.sock \
    --volume "${TRIVY_CACHE_VOLUME}:/root/.cache/" \
    "${TRIVY_IMAGE}" image \
    --db-repository ghcr.io/aquasecurity/trivy-db:2 \
    --scanners vuln \
    --severity CRITICAL \
    --exit-code 1 \
    --no-progress \
    "${image}"; then
    CONTAINER_FAILED=1
  fi
}

cd "${REPO_ROOT}"
command -v docker >/dev/null 2>&1 || {
  echo "Docker is required for container checks" >&2
  exit 1
}
TRIVY_CACHE_VOLUME="budgetmatch-ci-trivy-$$-${RANDOM}"
docker volume create "${TRIVY_CACHE_VOLUME}" >/dev/null

IMAGE_TAG="${CI_IMAGE_TAG:-$(git rev-parse --short=12 HEAD)}"
readonly IMAGE_TAG
validate_image_tag "${IMAGE_TAG}"

docker compose --env-file .env.ci config --quiet

echo "Linting Dockerfiles"
docker run --rm --interactive "${HADOLINT_IMAGE}" hadolint --failure-threshold error - < Dockerfile
docker run --rm --interactive "${HADOLINT_IMAGE}" hadolint --failure-threshold error - < web-ui/Dockerfile

prepare_build_context

build_image auth-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/auth --build-arg PORT=10003 || CONTAINER_FAILED=1
build_image seckill-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/seckill --build-arg PORT=10004 || CONTAINER_FAILED=1
build_image mall-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/mall --build-arg PORT=10005 || CONTAINER_FAILED=1
build_image agent-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/agent --build-arg PORT=10006 || CONTAINER_FAILED=1
build_image payment-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/payment --build-arg PORT=10007 || CONTAINER_FAILED=1
build_image app "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./cmd/app --build-arg PORT=10002 || CONTAINER_FAILED=1
build_image admin "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./cmd/admin --build-arg PORT=10001 || CONTAINER_FAILED=1
build_image web-ui "${BUILD_CONTEXT_DIR}/web-ui" "${BUILD_CONTEXT_DIR}/web-ui/Dockerfile" || CONTAINER_FAILED=1

for image in "${BUILT_IMAGES[@]}"; do
  scan_image "${image}"
done

if ((CONTAINER_FAILED != 0)); then
  exit 1
fi

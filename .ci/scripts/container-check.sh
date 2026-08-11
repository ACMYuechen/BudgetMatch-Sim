#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly BUILD_CONTEXT_DIR="$(mktemp -d)"
readonly HADOLINT_IMAGE="hadolint/hadolint:v2.14.0-debian@sha256:158cd0184dcaa18bd8ec20b61f4c1cabdf8b32a592d062f57bdcb8e4c1d312e2"
readonly TRIVY_IMAGE="aquasec/trivy:0.73.0@sha256:7cced7cae583819fc7806d4cbc0dbbc7cad18b99f7d3e235192e6da8c091045c"
readonly BACKEND_CACHE_SCOPE="budgetmatch-backend-dependencies"
readonly -a IMAGE_TARGETS=(
  auth-rpc
  seckill-rpc
  mall-rpc
  agent-rpc
  payment-rpc
  app
  admin
  web-ui
)

declare -a BUILT_IMAGES=()
declare -A SELECTED_IMAGE_TARGETS=()
TRIVY_CACHE_VOLUME=""
CONTAINER_FAILED=0

cleanup() {
  if [[ -n "${TRIVY_CACHE_VOLUME}" ]]; then
    docker volume rm "${TRIVY_CACHE_VOLUME}" >/dev/null 2>&1 || true
  fi
  rm -rf "${BUILD_CONTEXT_DIR}"
}
trap cleanup EXIT

retry_command() {
  local max_attempts="$1"
  shift

  local attempt
  for ((attempt = 1; attempt <= max_attempts; attempt++)); do
    if "$@"; then
      return 0
    fi

    if ((attempt == max_attempts)); then
      return 1
    fi

    echo "Command failed (attempt ${attempt}/${max_attempts}); retrying in $((attempt * 5)) seconds" >&2
    sleep "$((attempt * 5))"
  done
}

ensure_image() {
  local image="$1"

  if docker image inspect "${image}" >/dev/null 2>&1; then
    return 0
  fi

  echo "Pulling ${image}"
  retry_command 3 docker pull "${image}"
}

select_image_targets() {
  local raw_targets="${CI_IMAGE_TARGETS:-all}"
  local image target
  local -a requested_targets=()

  raw_targets="${raw_targets//,/ }"
  read -r -a requested_targets <<< "${raw_targets}"
  if ((${#requested_targets[@]} == 0)); then
    echo "CI_IMAGE_TARGETS must select at least one image" >&2
    return 1
  fi

  for target in "${requested_targets[@]}"; do
    if [[ "${target}" == "all" ]]; then
      for image in "${IMAGE_TARGETS[@]}"; do
        SELECTED_IMAGE_TARGETS["${image}"]=1
      done
      break
    fi

    case "${target}" in
      auth-rpc|seckill-rpc|mall-rpc|agent-rpc|payment-rpc|app|admin|web-ui)
        SELECTED_IMAGE_TARGETS["${target}"]=1
        ;;
      *)
        echo "Unknown CI image target: ${target}" >&2
        return 1
        ;;
    esac
  done

  echo "Selected image targets:"
  for target in "${IMAGE_TARGETS[@]}"; do
    if [[ -n "${SELECTED_IMAGE_TARGETS[${target}]:-}" ]]; then
      echo "- ${target}"
    fi
  done
}

image_target_selected() {
  local target="$1"
  [[ -n "${SELECTED_IMAGE_TARGETS[${target}]:-}" ]]
}

backend_target_selected() {
  local target
  for target in "${IMAGE_TARGETS[@]}"; do
    if [[ "${target}" != "web-ui" ]] && image_target_selected "${target}"; then
      return 0
    fi
  done
  return 1
}

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

prepare_backend_cache() {
  if ! backend_target_selected; then
    return
  fi

  echo "Preparing shared backend dependency cache"
  if docker buildx version >/dev/null 2>&1; then
    local -a cache_args=()
    if [[ "${GITHUB_ACTIONS:-false}" == "true" ]]; then
      cache_args+=(
        --cache-from "type=gha,scope=${BACKEND_CACHE_SCOPE}"
        --cache-to "type=gha,mode=max,scope=${BACKEND_CACHE_SCOPE}"
      )
    else
      cache_args+=(--load)
    fi

    retry_command 3 docker buildx build \
      --file "${BUILD_CONTEXT_DIR}/Dockerfile" \
      --target dependencies \
      "${cache_args[@]}" \
      "${BUILD_CONTEXT_DIR}"
  else
    retry_command 3 docker build \
      --file "${BUILD_CONTEXT_DIR}/Dockerfile" \
      --target dependencies \
      "${BUILD_CONTEXT_DIR}"
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
      if [[ "${name}" != "web-ui" ]]; then
        cache_args+=(--cache-from "type=gha,scope=${BACKEND_CACHE_SCOPE}")
      fi
      cache_args+=(
        --cache-from "type=gha,scope=budgetmatch-${name}"
        --cache-to "type=gha,mode=max,scope=budgetmatch-${name}"
      )
    fi
    if ! retry_command 3 docker buildx build \
      --file "${dockerfile}" \
      --tag "${image}" \
      --load \
      "${cache_args[@]}" \
      "${build_args[@]}" \
      "${context}"; then
      return 1
    fi
  else
    if ! retry_command 3 docker build \
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

validate_container_definitions() {
  docker compose --env-file .env.ci config --quiet

  echo "Preparing container lint image"
  ensure_image "${HADOLINT_IMAGE}"

  echo "Linting Dockerfiles"
  if backend_target_selected; then
    docker run --rm --interactive "${HADOLINT_IMAGE}" hadolint --failure-threshold error - < Dockerfile
  fi
  if image_target_selected web-ui; then
    docker run --rm --interactive "${HADOLINT_IMAGE}" hadolint --failure-threshold error - < web-ui/Dockerfile
  fi
}

build_selected_images() {
  if image_target_selected auth-rpc; then
    build_image auth-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/auth --build-arg PORT=10003 || CONTAINER_FAILED=1
  fi
  if image_target_selected seckill-rpc; then
    build_image seckill-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/seckill --build-arg PORT=10004 || CONTAINER_FAILED=1
  fi
  if image_target_selected mall-rpc; then
    build_image mall-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/mall --build-arg PORT=10005 || CONTAINER_FAILED=1
  fi
  if image_target_selected agent-rpc; then
    build_image agent-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/agent --build-arg PORT=10006 || CONTAINER_FAILED=1
  fi
  if image_target_selected payment-rpc; then
    build_image payment-rpc "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./services/rpc/payment --build-arg PORT=10007 || CONTAINER_FAILED=1
  fi
  if image_target_selected app; then
    build_image app "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./cmd/app --build-arg PORT=10002 || CONTAINER_FAILED=1
  fi
  if image_target_selected admin; then
    build_image admin "${BUILD_CONTEXT_DIR}" "${BUILD_CONTEXT_DIR}/Dockerfile" --build-arg SERVICE_PATH=./cmd/admin --build-arg PORT=10001 || CONTAINER_FAILED=1
  fi
  if image_target_selected web-ui; then
    build_image web-ui "${BUILD_CONTEXT_DIR}/web-ui" "${BUILD_CONTEXT_DIR}/web-ui/Dockerfile" || CONTAINER_FAILED=1
  fi
}

scan_built_images() {
  local image

  echo "Preparing container scan image"
  ensure_image "${TRIVY_IMAGE}"
  TRIVY_CACHE_VOLUME="budgetmatch-ci-trivy-$$-${RANDOM}"
  docker volume create "${TRIVY_CACHE_VOLUME}" >/dev/null

  for image in "${BUILT_IMAGES[@]}"; do
    scan_image "${image}"
  done
}

cd "${REPO_ROOT}"
command -v docker >/dev/null 2>&1 || {
  echo "Docker is required for container checks" >&2
  exit 1
}

IMAGE_TAG="${CI_IMAGE_TAG:-$(git rev-parse --short=12 HEAD)}"
readonly IMAGE_TAG
validate_image_tag "${IMAGE_TAG}"
select_image_targets

case "${CI_CONTAINER_PHASE:-all}" in
  prepare)
    validate_container_definitions
    prepare_build_context
    prepare_backend_cache
    ;;
  build-scan)
    prepare_build_context
    build_selected_images
    scan_built_images
    ;;
  all)
    validate_container_definitions
    prepare_build_context
    build_selected_images
    scan_built_images
    ;;
  *)
    echo "Unknown CI_CONTAINER_PHASE: ${CI_CONTAINER_PHASE}" >&2
    exit 1
    ;;
esac

if ((CONTAINER_FAILED != 0)); then
  exit 1
fi

#!/usr/bin/env bash

set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly REPO_ROOT
DIFF_FILE="$(mktemp)"
readonly DIFF_FILE
readonly -a BACKEND_IMAGES=(
  auth-rpc
  seckill-rpc
  mall-rpc
  agent-rpc
  payment-rpc
  app
  admin
)
readonly -a ALL_IMAGES=("${BACKEND_IMAGES[@]}" web-ui)

declare -a CHANGED_FILES=()
declare -A SELECTED_IMAGES=()
GO_CHECK=false
WEB_CHECK=false
SECURITY_CHECK=false
CONTAINER_CHECK=false
FULL_CI=false
FULL_CI_REASON=""

cleanup() {
  rm -f "${DIFF_FILE}"
}
trap cleanup EXIT

add_images() {
  local image
  for image in "$@"; do
    SELECTED_IMAGES["${image}"]=1
  done
}

enable_backend_checks() {
  GO_CHECK=true
  SECURITY_CHECK=true
  CONTAINER_CHECK=true
}

enable_web_checks() {
  WEB_CHECK=true
  SECURITY_CHECK=true
  CONTAINER_CHECK=true
  add_images web-ui
}

enable_full_ci() {
  FULL_CI=true
  GO_CHECK=true
  WEB_CHECK=true
  SECURITY_CHECK=true
  CONTAINER_CHECK=true
  add_images "${ALL_IMAGES[@]}"
}

add_service_targets() {
  local service="$1"
  local path="$2"
  local image

  case "${service}" in
    auth) image=auth-rpc ;;
    seckill) image=seckill-rpc ;;
    mall) image=mall-rpc ;;
    agent) image=agent-rpc ;;
    payment) image=payment-rpc ;;
    *)
      FULL_CI_REASON="unrecognized RPC service path: ${path}"
      enable_full_ci
      return
      ;;
  esac

  add_images "${image}"

  # Generated protobuf and client packages are imported by downstream binaries.
  case "${path}" in
    services/rpc/"${service}"/pb/*|services/rpc/"${service}"/client/*|services/rpc/"${service}"/proto/*)
      case "${service}" in
        auth) add_images app admin ;;
        seckill) add_images app admin ;;
        mall) add_images app admin seckill-rpc agent-rpc payment-rpc ;;
        agent) add_images app ;;
        payment) add_images app ;;
      esac
      ;;
  esac
}

classify_path() {
  local path="$1"
  local service

  case "${path}" in
    .github/*|.ci/*)
      FULL_CI_REASON="CI definition changed: ${path}"
      enable_full_ci
      ;;
    docs/*|*.md|LICENSE|LICENSE.*|.gitignore|.editorconfig)
      ;;
    web-ui/*)
      enable_web_checks
      ;;
    cmd/app/*)
      enable_backend_checks
      add_images app
      ;;
    cmd/admin/*)
      enable_backend_checks
      add_images admin
      ;;
    services/rpc/*)
      enable_backend_checks
      service="${path#services/rpc/}"
      service="${service%%/*}"
      add_service_targets "${service}" "${path}"
      ;;
    infra/*)
      enable_backend_checks
      add_images "${BACKEND_IMAGES[@]}"
      ;;
    go.mod|go.sum|Dockerfile|.dockerignore)
      enable_backend_checks
      add_images "${BACKEND_IMAGES[@]}"
      ;;
    docker-compose.yml|.env.ci)
      SECURITY_CHECK=true
      CONTAINER_CHECK=true
      add_images "${ALL_IMAGES[@]}"
      ;;
    .env.example|Makefile|package-lock.json|scripts/*|tpls/*)
      SECURITY_CHECK=true
      ;;
    *)
      FULL_CI_REASON="unclassified path changed: ${path}"
      enable_full_ci
      ;;
  esac
}

load_changed_files() {
  if [[ -n "${CI_CHANGED_FILES_FILE:-}" ]]; then
    mapfile -d '' CHANGED_FILES < "${CI_CHANGED_FILES_FILE}"
    return
  fi

  case "${CI_EVENT_NAME:-}" in
    workflow_dispatch)
      FULL_CI_REASON="manual workflow dispatch"
      enable_full_ci
      return
      ;;
    pull_request)
      if [[ -z "${CI_BASE_SHA:-}" || -z "${CI_HEAD_SHA:-}" ]]; then
        echo "CI_BASE_SHA and CI_HEAD_SHA are required for pull_request events" >&2
        return 1
      fi
      git diff --name-only -z "${CI_BASE_SHA}...${CI_HEAD_SHA}" > "${DIFF_FILE}"
      mapfile -d '' CHANGED_FILES < "${DIFF_FILE}"
      ;;
    push)
      if [[ -z "${CI_BASE_SHA:-}" || -z "${CI_HEAD_SHA:-}" ]]; then
        echo "CI_BASE_SHA and CI_HEAD_SHA are required for push events" >&2
        return 1
      fi
      if [[ "${CI_BASE_SHA}" =~ ^0+$ ]]; then
        FULL_CI_REASON="initial branch push"
        enable_full_ci
        return
      fi
      git diff --name-only -z "${CI_BASE_SHA}" "${CI_HEAD_SHA}" > "${DIFF_FILE}"
      mapfile -d '' CHANGED_FILES < "${DIFF_FILE}"
      ;;
    *)
      echo "Unsupported CI_EVENT_NAME: ${CI_EVENT_NAME:-<unset>}" >&2
      return 1
      ;;
  esac
}

join_selected_images() {
  local image
  local joined=""

  for image in "${ALL_IMAGES[@]}"; do
    if [[ -n "${SELECTED_IMAGES[${image}]:-}" ]]; then
      joined+="${joined:+ }${image}"
    fi
  done

  printf '%s' "${joined}"
}

write_output() {
  local name="$1"
  local value="$2"

  printf '%s=%s\n' "${name}" "${value}" >> "${GITHUB_OUTPUT:-/dev/stdout}"
}

write_summary() {
  if [[ -z "${GITHUB_STEP_SUMMARY:-}" ]]; then
    return
  fi

  {
    echo "## CI change selection"
    echo
    echo "| Check | Selected |"
    echo "| --- | --- |"
    echo "| Go | ${GO_CHECK} |"
    echo "| Web | ${WEB_CHECK} |"
    echo "| Security | ${SECURITY_CHECK} |"
    echo "| Container | ${CONTAINER_CHECK} |"
    echo
    echo "Container targets: \`${CONTAINER_TARGETS:-none}\`"

    if [[ -n "${FULL_CI_REASON}" ]]; then
      echo
      echo "Full CI reason: ${FULL_CI_REASON}"
    fi

    if ((${#CHANGED_FILES[@]} > 0)); then
      echo
      echo "Changed files:"
      local path
      local shown=0
      for path in "${CHANGED_FILES[@]}"; do
        printf -- '- %s\n' "${path}"
        shown=$((shown + 1))
        if ((shown == 50 && ${#CHANGED_FILES[@]} > shown)); then
          echo "- ... $(( ${#CHANGED_FILES[@]} - shown )) more files"
          break
        fi
      done
    fi
  } >> "${GITHUB_STEP_SUMMARY}"
}

cd "${REPO_ROOT}"
load_changed_files

if [[ "${FULL_CI}" != true ]]; then
  for path in "${CHANGED_FILES[@]}"; do
    classify_path "${path}"
    if [[ "${FULL_CI}" == true ]]; then
      break
    fi
  done
fi

CONTAINER_TARGETS="$(join_selected_images)"
readonly CONTAINER_TARGETS

write_output go_check "${GO_CHECK}"
write_output web_check "${WEB_CHECK}"
write_output security_check "${SECURITY_CHECK}"
write_output container_check "${CONTAINER_CHECK}"
write_output container_targets "${CONTAINER_TARGETS}"

echo "Selected checks: go=${GO_CHECK} web=${WEB_CHECK} security=${SECURITY_CHECK} container=${CONTAINER_CHECK}"
echo "Selected container targets: ${CONTAINER_TARGETS:-none}"
if [[ -n "${FULL_CI_REASON}" ]]; then
  echo "Full CI reason: ${FULL_CI_REASON}"
fi

write_summary

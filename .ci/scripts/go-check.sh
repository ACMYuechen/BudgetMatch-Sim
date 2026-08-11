#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly RUNTIME_DIR="$(mktemp -d)"
readonly ETCD_IMAGE="quay.io/coreos/etcd:v3.5.15@sha256:0934690612905554eb61ddefb9faaaecb47c2f6931dbb453e694358092ee8990"
readonly PGVECTOR_IMAGE="pgvector/pgvector:pg16@sha256:a36250871de0833b8757561c72f2477ef1ddd1101afa4e617fb552e0de514c6b"
declare -a CI_CONTAINERS=()
declare -a CHANGED_FILES=()
declare -a ALL_TEST_PACKAGES=()
declare -a RACE_TEST_PACKAGES=()
declare -A PACKAGE_BY_DIR=()
declare -A REVERSE_DEPENDENTS=()
FULL_RACE=false
FULL_RACE_REASON=""

cleanup() {
  if ((${#CI_CONTAINERS[@]} > 0)); then
    docker rm --force --volumes "${CI_CONTAINERS[@]}" >/dev/null 2>&1 || true
  fi
  rm -rf "${RUNTIME_DIR}"
}
trap cleanup EXIT

wait_for_container() {
  local container_name="$1"
  shift

  for _ in {1..30}; do
    if docker exec "${container_name}" "$@" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  docker logs "${container_name}" >&2 || true
  return 1
}

start_etcd_if_needed() {
  if [[ -n "${ETCD_HOSTS:-}" ]]; then
    return
  fi

  command -v docker >/dev/null 2>&1 || {
    echo "ETCD_HOSTS is unset and Docker is unavailable" >&2
    return 1
  }

  local container_name="budgetmatch-ci-etcd-$(basename "${RUNTIME_DIR}")"
  docker run --detach --rm \
    --name "${container_name}" \
    --publish 127.0.0.1::2379 \
    --env ETCD_NAME=budgetmatch-ci \
    --env ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379 \
    --env ETCD_ADVERTISE_CLIENT_URLS=http://0.0.0.0:2379 \
    "${ETCD_IMAGE}" >/dev/null
  CI_CONTAINERS+=("${container_name}")

  wait_for_container "${container_name}" etcdctl endpoint health
  local port
  port="$(docker port "${container_name}" 2379/tcp | awk -F: 'NR == 1 { print $NF }')"
  export ETCD_HOSTS="127.0.0.1:${port}"
}

start_postgres_if_needed() {
  if [[ -n "${RAG_TEST_PG_DSN:-}" ]]; then
    return
  fi

  command -v docker >/dev/null 2>&1 || {
    echo "RAG_TEST_PG_DSN is unset and Docker is unavailable" >&2
    return 1
  }

  local container_name="budgetmatch-ci-postgres-$(basename "${RUNTIME_DIR}")"
  docker run --detach --rm \
    --name "${container_name}" \
    --publish 127.0.0.1::5432 \
    --tmpfs /var/lib/postgresql/data \
    --env POSTGRES_USER=root \
    --env POSTGRES_PASSWORD=ci-postgres-password \
    --env POSTGRES_DB=budgetmatch_ci \
    "${PGVECTOR_IMAGE}" >/dev/null
  CI_CONTAINERS+=("${container_name}")

  wait_for_container "${container_name}" pg_isready --username root --dbname budgetmatch_ci
  local port
  port="$(docker port "${container_name}" 5432/tcp | awk -F: 'NR == 1 { print $NF }')"
  export RAG_TEST_PG_DSN="host=127.0.0.1 user=root password=ci-postgres-password dbname=budgetmatch_ci port=${port} sslmode=disable"
}

check_formatting() {
  local -a go_files=()
  local output

  mapfile -d '' go_files < <(
    find . -type f -name '*.go' \
      -not -path './vendor/*' \
      -not -path './web-ui/node_modules/*' \
      -not -path './web-ui/dist/*' \
      -print0
  )
  output="$(gofmt -l "${go_files[@]}")"
  if [[ -n "${output}" ]]; then
    echo "The following Go files are not formatted:" >&2
    echo "${output}" >&2
    return 1
  fi
}

# Reads the repository package graph once and records production and test imports.
load_package_graph() {
  local package_output
  local import_path package_dir imports test_imports xtest_imports has_tests
  local relative_dir import_list dependency current_dependents
  local -a dependencies=()

  package_output="$(
    go list -f '{{.ImportPath}}{{"\x1f"}}{{.Dir}}{{"\x1f"}}{{join .Imports " "}}{{"\x1f"}}{{join .TestImports " "}}{{"\x1f"}}{{join .XTestImports " "}}{{"\x1f"}}{{if or .TestGoFiles .XTestGoFiles}}true{{else}}false{{end}}' ./...
  )" || return

  while IFS=$'\x1f' read -r import_path package_dir imports test_imports xtest_imports has_tests; do
    if [[ -z "${import_path}" ]]; then
      continue
    fi

    case "${package_dir}" in
      "${REPO_ROOT}") relative_dir="." ;;
      "${REPO_ROOT}"/*) relative_dir="${package_dir#"${REPO_ROOT}/"}" ;;
      *)
        echo "Package directory is outside the repository: ${package_dir}" >&2
        return 1
        ;;
    esac
    PACKAGE_BY_DIR["${relative_dir}"]="${import_path}"

    if [[ "${has_tests}" == "true" ]]; then
      ALL_TEST_PACKAGES+=("${import_path}")
    fi

    for import_list in "${imports}" "${test_imports}" "${xtest_imports}"; do
      if [[ -z "${import_list}" ]]; then
        continue
      fi

      dependencies=()
      read -r -a dependencies <<< "${import_list}"
      for dependency in "${dependencies[@]}"; do
        current_dependents="${REVERSE_DEPENDENTS[${dependency}]:-}"
        REVERSE_DEPENDENTS["${dependency}"]="${current_dependents}${current_dependents:+ }${import_path}"
      done
    done
  done <<< "${package_output}"

  if ((${#PACKAGE_BY_DIR[@]} == 0)); then
    echo "No Go packages were found" >&2
    return 1
  fi
}

enable_full_race() {
  if [[ "${FULL_RACE}" != true ]]; then
    FULL_RACE_REASON="$1"
  fi
  FULL_RACE=true
}

# Uses the same base-to-head comparison as the workflow change detector.
load_changed_files() {
  local diff_file="${RUNTIME_DIR}/changed-files"

  if [[ -n "${CI_CHANGED_FILES_FILE:-}" ]]; then
    mapfile -d '' CHANGED_FILES < "${CI_CHANGED_FILES_FILE}"
    return
  fi

  case "${CI_EVENT_NAME:-}" in
    workflow_dispatch)
      enable_full_race "manual workflow dispatch"
      ;;
    pull_request)
      if [[ -z "${CI_BASE_SHA:-}" || -z "${CI_HEAD_SHA:-}" ]]; then
        enable_full_race "pull request base or head SHA is unavailable"
      elif git diff --name-only -z "${CI_BASE_SHA}...${CI_HEAD_SHA}" > "${diff_file}"; then
        mapfile -d '' CHANGED_FILES < "${diff_file}"
      else
        enable_full_race "pull request diff could not be calculated"
      fi
      ;;
    push)
      if [[ -z "${CI_BASE_SHA:-}" || -z "${CI_HEAD_SHA:-}" ]]; then
        enable_full_race "push base or head SHA is unavailable"
      elif [[ "${CI_BASE_SHA}" =~ ^0+$ ]]; then
        enable_full_race "initial branch push"
      elif git diff --name-only -z "${CI_BASE_SHA}" "${CI_HEAD_SHA}" > "${diff_file}"; then
        mapfile -d '' CHANGED_FILES < "${diff_file}"
      else
        enable_full_race "push diff could not be calculated"
      fi
      ;;
    "")
      enable_full_race "local invocation without an event context"
      ;;
    *)
      enable_full_race "unsupported event: ${CI_EVENT_NAME}"
      ;;
  esac
}

# Expands changed packages through reverse production and test dependencies.
select_race_test_packages() {
  local path directory package current dependent dependents
  local index=0
  local -A changed_packages=()
  local -A affected_packages=()
  local -a queue=()
  local -a direct_dependents=()

  if [[ "${FULL_RACE}" != true && ${#CHANGED_FILES[@]} -eq 0 ]]; then
    enable_full_race "no changed files were found"
  fi

  if [[ "${FULL_RACE}" != true ]]; then
    for path in "${CHANGED_FILES[@]}"; do
      path="${path#./}"
      case "${path}" in
        go.mod|go.sum|go.work|go.work.sum)
          enable_full_race "Go dependency definition changed: ${path}"
          break
          ;;
        .ci/scripts/*|.github/workflows/*)
          enable_full_race "CI execution definition changed: ${path}"
          break
          ;;
        *.proto)
          enable_full_race "protobuf contract changed: ${path}"
          break
          ;;
        *.go)
          if [[ ! -e "${REPO_ROOT}/${path}" ]]; then
            enable_full_race "changed Go file no longer exists: ${path}"
            break
          fi

          directory="$(dirname -- "${path}")"
          package="${PACKAGE_BY_DIR[${directory}]:-}"
          if [[ -z "${package}" ]]; then
            enable_full_race "changed Go file could not be mapped to a package: ${path}"
            break
          fi
          changed_packages["${package}"]=1
          ;;
        docs/*|*.md|LICENSE|LICENSE.*|.gitignore|.editorconfig|web-ui/*|Dockerfile|.dockerignore|docker-compose.yml|.env.ci|.env.example|package-lock.json|scripts/*|tpls/*)
          ;;
        cmd/*|services/rpc/*|infra/*)
          enable_full_race "non-Go backend file changed: ${path}"
          break
          ;;
        *)
          enable_full_race "changed path could not be scoped safely: ${path}"
          break
          ;;
      esac
    done
  fi

  if [[ "${FULL_RACE}" == true ]]; then
    RACE_TEST_PACKAGES=("${ALL_TEST_PACKAGES[@]}")
    echo "Race test scope: all test packages (${FULL_RACE_REASON})"
    return
  fi

  if ((${#changed_packages[@]} == 0)); then
    echo "Race test scope: no Go source packages changed"
    return
  fi

  for package in "${!changed_packages[@]}"; do
    affected_packages["${package}"]=1
    queue+=("${package}")
  done

  while ((index < ${#queue[@]})); do
    current="${queue[${index}]}"
    index=$((index + 1))
    dependents="${REVERSE_DEPENDENTS[${current}]:-}"
    if [[ -z "${dependents}" ]]; then
      continue
    fi

    direct_dependents=()
    read -r -a direct_dependents <<< "${dependents}"
    for dependent in "${direct_dependents[@]}"; do
      if [[ -z "${affected_packages[${dependent}]:-}" ]]; then
        affected_packages["${dependent}"]=1
        queue+=("${dependent}")
      fi
    done
  done

  for package in "${ALL_TEST_PACKAGES[@]}"; do
    if [[ -n "${affected_packages[${package}]:-}" ]]; then
      RACE_TEST_PACKAGES+=("${package}")
    fi
  done

  echo "Race test scope: ${#RACE_TEST_PACKAGES[@]} test packages affected by ${#changed_packages[@]} changed Go packages"
}

run_race_tests() {
  load_package_graph
  load_changed_files
  select_race_test_packages

  if ((${#RACE_TEST_PACKAGES[@]} == 0)); then
    echo "No affected Go packages with tests were found"
    return
  fi

  printf 'Running race tests for %d packages:\n' "${#RACE_TEST_PACKAGES[@]}"
  printf -- '- %s\n' "${RACE_TEST_PACKAGES[@]}"
  go test -race "${RACE_TEST_PACKAGES[@]}"
}

cd "${REPO_ROOT}"
start_etcd_if_needed
start_postgres_if_needed

go mod download
go mod verify
# check_formatting
go vet ./...
run_race_tests
go build ./...

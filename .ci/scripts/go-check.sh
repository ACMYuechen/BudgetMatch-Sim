#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly RUNTIME_DIR="$(mktemp -d)"
readonly ETCD_IMAGE="quay.io/coreos/etcd:v3.5.15@sha256:0934690612905554eb61ddefb9faaaecb47c2f6931dbb453e694358092ee8990"
readonly PGVECTOR_IMAGE="pgvector/pgvector:pg16@sha256:a36250871de0833b8757561c72f2477ef1ddd1101afa4e617fb552e0de514c6b"
declare -a CI_CONTAINERS=()

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

build_services() {
  local -a services=(
    "admin:./cmd/admin"
    "app:./cmd/app"
    "auth-rpc:./services/rpc/auth"
    "seckill-rpc:./services/rpc/seckill"
    "mall-rpc:./services/rpc/mall"
    "agent-rpc:./services/rpc/agent"
    "payment-rpc:./services/rpc/payment"
  )
  local service name package

  for service in "${services[@]}"; do
    name="${service%%:*}"
    package="${service#*:}"
    echo "Building ${name}"
    go build -o "${RUNTIME_DIR}/${name}" "${package}"
  done
}

cd "${REPO_ROOT}"
start_etcd_if_needed
start_postgres_if_needed

go mod download
go mod verify
# check_formatting
go vet ./...
go test -count=1 -race ./...
build_services

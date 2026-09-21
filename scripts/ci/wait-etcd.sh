#!/usr/bin/env bash

set -Eeuo pipefail

for _attempt in {1..30}; do
  if curl --fail --silent http://127.0.0.1:2379/health \
    | grep --quiet '"health":"true"'; then
    exit 0
  fi
  sleep 1
done

echo "etcd did not become healthy" >&2
curl --silent --show-error http://127.0.0.1:2379/health >&2 || true
exit 1

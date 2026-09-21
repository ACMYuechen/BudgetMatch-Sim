#!/bin/bash
set -Eeuo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Default: the project's Compose container. An explicit endpoint never falls back
# to another instance. Tests that write keys belong to isolated integration test environments.
if (($#)); then
    command -v etcdctl >/dev/null || { echo '显式 endpoint 需要本机 etcdctl' >&2; exit 1; }
    etcd=(etcdctl "--endpoints=$1")
else
    etcd=(docker compose exec -T etcd etcdctl --endpoints=http://127.0.0.1:2379)
fi
"${etcd[@]}" endpoint status --write-out=table
for prefix in auth.rpc seckill.rpc mall.rpc agent.rpc payment.rpc /config/; do
    printf '\n%s 键名：\n' "$prefix"
    "${etcd[@]}" get "$prefix" --prefix --keys-only
done

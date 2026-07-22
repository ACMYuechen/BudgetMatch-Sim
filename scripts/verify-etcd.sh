#!/bin/bash
set -e

# 验证 etcd 服务注册、发现与分布式锁
# 用法：./scripts/verify-etcd.sh [etcd-endpoint]

ENDPOINT=${1:-${DEV_ETCD_ENDPOINT:-${ETCD_HOSTS:-127.0.0.1:22379}}}

if ! command -v etcdctl &> /dev/null; then
    echo "etcdctl not found, trying docker exec..."
    ETCDCTL="docker exec budgetmatch-sim-etcd etcdctl"
else
    ETCDCTL="etcdctl --endpoints=$ENDPOINT"
fi

echo "=== 1. Check etcd cluster health ==="
$ETCDCTL endpoint health

echo ""
echo "=== 2. Check RPC service registration ==="
for key in auth.rpc seckill.rpc mall.rpc agent.rpc payment.rpc; do
    echo "-- $key --"
    $ETCDCTL get --prefix "$key" || true
done

echo ""
echo "=== 3. Check dynamic config ==="
$ETCDCTL get --prefix /config

echo ""
echo "=== 4. Simple distributed lock test (requires Go) ==="
go test -v ./infra/dlock/... 2>&1 || true

echo ""
echo "=== 5. Check configcenter unit test ==="
go test -v ./infra/configcenter/... 2>&1 || true

echo ""
echo "Verification complete."

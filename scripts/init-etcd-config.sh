#!/bin/bash
set -e

# 初始化 etcd 中的默认动态配置
# 用法：./scripts/init-etcd-config.sh [etcd-endpoint]
# 默认 endpoint 对应本地 Docker Compose 中的 etcd 服务

ENDPOINT=${1:-127.0.0.1:2379}

if ! command -v etcdctl &> /dev/null; then
    echo "etcdctl not found, trying docker exec..."
    ETCDCTL="docker exec budgetmatch-sim-etcd etcdctl"
else
    ETCDCTL="etcdctl --endpoints=$ENDPOINT"
fi

echo "Initializing dynamic config in etcd at $ENDPOINT..."

# 全局配置
$ETCDCTL put /config/global '{
  "rpcTimeoutMs": 60000,
  "restTimeoutMs": 600000
}'

# 秒杀服务动态配置
$ETCDCTL put /config/seckill.rpc '{
  "activityRateLimit": {
    "windowSeconds": 5,
    "max": 1000
  },
  "userRateLimit": {
    "capacity": 5,
    "rate": 1,
    "intervalSeconds": 60
  },
  "features": {
    "enableNewOrderFlow": false
  },
  "lowStockThreshold": 100
}'

# auth / mall 预留空配置，后续按需扩展
$ETCDCTL put /config/auth.rpc '{}'
$ETCDCTL put /config/mall.rpc '{}'
$ETCDCTL put /config/app '{}'
$ETCDCTL put /config/admin '{}'

echo "Dynamic config initialized."
$ETCDCTL get --prefix /config

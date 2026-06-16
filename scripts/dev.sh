#!/bin/bash
set -e

LOG_DIR="logs"
PID_DIR=".pids"

mkdir -p $LOG_DIR $PID_DIR

# 加载本地环境变量（供 conf.UseEnv() 替换配置中的 ${VAR}）
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi

# 清理旧进程
for pidfile in $PID_DIR/*.pid; do
    [ -f "$pidfile" ] && kill $(cat "$pidfile") 2>/dev/null || true
    rm -f "$pidfile"
done

# 兜底：按端口清理可能残留的本地服务进程
for port in 10000 10001 10002 10003 10004 10005 10006 2379; do
    pids=$(ss -ltnp 2>/dev/null | grep ":$port " | grep -oP 'pid=\K[0-9]+' | sort -u)
    for pid in $pids; do
        kill -9 "$pid" 2>/dev/null || true
    done
done

# 短暂等待，确保端口释放
sleep 1

# 启动基础设施
echo "🐳 启动基础设施 (postgres + redis + rocketmq + etcd)..."
docker compose up postgres redis rocketmq-namesrv rocketmq-broker etcd -d

echo "⏳ 等待基础设施就绪..."
sleep 8

# 初始化 etcd 默认动态配置（etcdctl 不存在时跳过）
if command -v etcdctl &> /dev/null; then
    echo "🔧 初始化 etcd 动态配置..."
    bash scripts/init-etcd-config.sh 127.0.0.1:2379 || true
fi

# 设置 etcd 地址，供 conf.UseEnv() 替换配置中的 ${ETCD_HOSTS}
export ETCD_HOSTS=127.0.0.1:2379

# 启动各服务（cd 到对应目录后再 go run，否则找不到 etc/config.yaml）
# 串行启动：auth-rpc 先启动并自动创建数据库表
echo "🚀 启动服务..."

echo "  → auth-rpc (port 10003) — 先启动创建数据库表"
(cd services/rpc/auth && nohup go run . > ../../../$LOG_DIR/auth-rpc.log 2>&1 &)
echo $! > $PID_DIR/auth-rpc.pid

sleep 5

echo "  → seckill-rpc (port 10004)"
(cd services/rpc/seckill && nohup go run . > ../../../$LOG_DIR/seckill-rpc.log 2>&1 &)
echo $! > $PID_DIR/seckill-rpc.pid

sleep 3

echo "  → mall-rpc (port 10005)"
(cd services/rpc/mall && nohup go run . > ../../../$LOG_DIR/mall-rpc.log 2>&1 &)
echo $! > $PID_DIR/mall-rpc.pid

sleep 3

echo "  → app (port 10002)"
(cd cmd/app && nohup go run . > ../../$LOG_DIR/app.log 2>&1 &)
echo $! > $PID_DIR/app.pid

sleep 2

echo "  → admin (port 10001)"
(cd cmd/admin && nohup go run . > ../../$LOG_DIR/admin.log 2>&1 &)
echo $! > $PID_DIR/admin.pid

echo ""
echo "✅ 所有服务已启动"
echo ""
echo "查看日志:"
echo "  tail -f logs/auth-rpc.log     # auth RPC"
echo "  tail -f logs/seckill-rpc.log  # seckill RPC"
echo "  tail -f logs/mall-rpc.log     # mall RPC"
echo "  tail -f logs/app.log          # app 服务"
echo "  tail -f logs/admin.log        # admin 服务"
echo ""
echo "停止服务: make dev-stop"

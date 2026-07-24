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

# 清理旧进程：先通过 PID 文件杀，再按端口兜底
# make dev 中 RPC/API 运行在宿主机，不能使用 Docker 网络内的 etcd:2379。
# 因此本地开发时让 etcd advertise 宿主机可访问的端口；全容器模式仍使用 docker-compose.yml 默认值。
DEV_ETCD_CLIENT_PORT=${DEV_ETCD_CLIENT_PORT:-22379}
DEV_ETCD_ENDPOINT=${DEV_ETCD_ENDPOINT:-127.0.0.1:${DEV_ETCD_CLIENT_PORT}}
DEV_ROCKETMQ_NAMESRV_PORT=${DEV_ROCKETMQ_NAMESRV_PORT:-19876}
DEV_ROCKETMQ_NAMESERVERS=${DEV_ROCKETMQ_NAMESERVERS:-127.0.0.1:${DEV_ROCKETMQ_NAMESRV_PORT}}
for pidfile in $PID_DIR/*.pid; do
    if [ -f "$pidfile" ]; then
        kill $(cat "$pidfile") 2>/dev/null || true
        rm -f "$pidfile"
    fi
done

# 按端口清理残留进程（fuser -k 直接发 SIGKILL，比 ss+grep+kill 更可靠）
LOCAL_PORTS=(10001 10002 10003 10004 10005 10006 10007 "$DEV_ETCD_CLIENT_PORT" "$DEV_ROCKETMQ_NAMESRV_PORT")
for port in "${LOCAL_PORTS[@]}"; do
    fuser -k "$port/tcp" 2>/dev/null || true
done

# 短暂等待，确保端口释放
sleep 1

# 启动基础设施
echo "🐳 启动基础设施 (postgres + redis + rocketmq + etcd)..."
export ETCD_CLIENT_PORT=$DEV_ETCD_CLIENT_PORT
export ETCD_ADVERTISE_CLIENT_URLS=http://$DEV_ETCD_ENDPOINT
export ROCKETMQ_NAMESRV_PORT=$DEV_ROCKETMQ_NAMESRV_PORT
docker compose up postgres redis rocketmq-namesrv rocketmq-broker etcd -d

echo "⏳ 等待基础设施就绪..."
sleep 8

# 初始化 etcd 默认动态配置（seckill 等服务启动前必须存在）
echo "🔧 初始化 etcd 动态配置..."
bash scripts/init-etcd-config.sh "$DEV_ETCD_ENDPOINT" || true

# 设置 etcd 地址，供 conf.UseEnv() 替换配置中的 ${ETCD_HOSTS}
export ETCD_HOSTS=$DEV_ETCD_ENDPOINT
export ROCKETMQ_NAMESERVERS=$DEV_ROCKETMQ_NAMESERVERS

# 启动各服务（go run -C 指定工作目录，无需 subshell，$! 直接捕获 go run PID）
# 串行启动：auth-rpc 先启动并自动创建数据库表
echo "🚀 启动服务..."

echo "  → auth-rpc (port 10003)"
nohup go run -C services/rpc/auth . > $LOG_DIR/auth-rpc.log 2>&1 &
echo $! > $PID_DIR/auth-rpc.pid
sleep 3

echo "  → seckill-rpc (port 10004)"
nohup go run -C services/rpc/seckill . > $LOG_DIR/seckill-rpc.log 2>&1 &
echo $! > $PID_DIR/seckill-rpc.pid
sleep 3

echo "  → mall-rpc (port 10005)"
nohup go run -C services/rpc/mall . > $LOG_DIR/mall-rpc.log 2>&1 &
echo $! > $PID_DIR/mall-rpc.pid
sleep 3

echo "  → agent-rpc (port 10006)"
nohup go run -C services/rpc/agent . > $LOG_DIR/agent-rpc.log 2>&1 &
echo $! > $PID_DIR/agent-rpc.pid
sleep 3

echo "  → payment-rpc (port 10007)"
nohup go run -C services/rpc/payment . > $LOG_DIR/payment-rpc.log 2>&1 &
echo $! > $PID_DIR/payment-rpc.pid
sleep 3

echo "  → app (port 10002)"
nohup go run -C cmd/app . > $LOG_DIR/app.log 2>&1 &
echo $! > $PID_DIR/app.pid
sleep 2

echo "  → admin (port 10001)"
nohup go run -C cmd/admin . > $LOG_DIR/admin.log 2>&1 &
echo $! > $PID_DIR/admin.pid

echo ""
echo "✅ 所有服务已启动"
echo ""
echo "查看日志:"
echo "  tail -f logs/auth-rpc.log     # auth RPC"
echo "  tail -f logs/seckill-rpc.log  # seckill RPC"
echo "  tail -f logs/mall-rpc.log     # mall RPC"
echo "  tail -f logs/agent-rpc.log    # agent RPC"
echo "  tail -f logs/payment-rpc.log  # payment RPC"
echo "  tail -f logs/app.log          # app 服务"
echo "  tail -f logs/admin.log        # admin 服务"
echo ""
echo "停止服务: make dev-stop"

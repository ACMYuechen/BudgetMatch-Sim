#!/bin/bash
set -e

LOG_DIR="logs"
PID_DIR=".pids"

mkdir -p $LOG_DIR $PID_DIR

# 清理旧进程
for pidfile in $PID_DIR/*.pid; do
    [ -f "$pidfile" ] && kill $(cat "$pidfile") 2>/dev/null || true
done
rm -f $PID_DIR/*.pid

# 启动基础设施
echo "🐳 启动基础设施 (postgres + redis)..."
docker compose up postgres redis -d

echo "⏳ 等待数据库就绪..."
sleep 5

# 启动各服务（cd 到对应目录后再 go run，否则找不到 etc/config.yaml）
# 串行启动：auth-rpc 先创建表，gateway 随 auth-rpc 一起启动
echo "🚀 启动服务..."

echo "  → auth-rpc (port 10003 + gateway 10000) — 先启动创建数据库表"
(cd services/rpc/auth && nohup go run . > ../../../$LOG_DIR/auth-rpc.log 2>&1 &)
echo $! > $PID_DIR/auth-rpc.pid

sleep 5

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
echo "  tail -f logs/auth-rpc.log   # auth RPC + gateway"
echo "  tail -f logs/app.log        # app 服务"
echo "  tail -f logs/admin.log      # admin 服务"
echo ""
echo "停止服务: make dev-stop"

#!/bin/bash

PID_DIR=".pids"

if [ ! -d "$PID_DIR" ]; then
    echo "没有找到运行中的服务"
    exit 0
fi

echo "停止本地服务..."
for pidfile in $PID_DIR/*.pid; do
    if [ -f "$pidfile" ]; then
        name=$(basename "$pidfile" .pid)
        kill $(cat "$pidfile") 2>/dev/null && echo "  ✓ $name 已停止" || true
    fi
done
rm -f $PID_DIR/*.pid

echo "停止基础设施..."
docker compose down 2>/dev/null || true

echo "✅ 清理完成"

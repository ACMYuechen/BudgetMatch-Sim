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
        rm -f "$pidfile"
    fi
done

# 兜底：清理可能残留的服务进程
for port in 10000 10001 10002 10003 10004 10005; do
    pids=$(ss -ltnp 2>/dev/null | grep ":$port " | grep -oP 'pid=\K[0-9]+' | sort -u)
    for pid in $pids; do
        if kill -9 "$pid" 2>/dev/null; then
            echo "  ✓ 端口 $port 的残留进程 $pid 已停止"
        fi
    done
done

echo "停止基础设施..."
docker compose down 2>/dev/null || true

echo "✅ 清理完成"

#!/bin/bash

PID_DIR=".pids"

if [ ! -d "$PID_DIR" ]; then
    echo "没有找到运行中的服务 (缺少 .pids 目录)"
else
    echo "停止本地服务（通过 PID 文件）..."
    for pidfile in $PID_DIR/*.pid; do
        if [ -f "$pidfile" ]; then
            name=$(basename "$pidfile" .pid)
            kill $(cat "$pidfile") 2>/dev/null && echo "  ✓ $name 已停止" || true
            rm -f "$pidfile"
        fi
    done
fi

# 按端口清理残留进程（fuser -k 直接发 SIGKILL）
LOCAL_PORTS=(10001 10002 10003 10004 10005 10006 10007)
STILL_ALIVE=()
for port in "${LOCAL_PORTS[@]}"; do
    if fuser "$port/tcp" 2>/dev/null | grep -q .; then
        fuser -k "$port/tcp" 2>/dev/null && echo "  ✓ 端口 $port 残留进程已清理"
    fi
done

# 验证端口已释放
sleep 1
for port in "${LOCAL_PORTS[@]}"; do
    if fuser "$port/tcp" 2>/dev/null | grep -q .; then
        STILL_ALIVE+=("$port")
    fi
done

if [ ${#STILL_ALIVE[@]} -gt 0 ]; then
    echo "⚠️  以下端口仍被占用: ${STILL_ALIVE[*]}"
else
    echo "✅ 所有服务端口已释放"
fi

# 停止基础设施容器
echo "停止基础设施..."
docker compose down 2>/dev/null || true

echo "✅ 清理完成"

#!/bin/bash
# 秒杀并发压测脚本
# 前置条件：已通过 make dev 启动所有服务，且 docker/redis/postgres 可访问
# 默认使用 hey 进行高并发压测；若未安装则使用 curl 串行演示

set -e

BASE_URL=${BASE_URL:-http://localhost:10002}
ADMIN_URL=${ADMIN_URL:-http://localhost:10001}
AUTH_URL=${AUTH_URL:-http://localhost:10000}

JWT_SECRET=${JWT_SECRET:-test-secret}
ADMIN_EMAIL=${ADMIN_EMAIL:-admin@test.com}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-Admin@123}

# 颜色
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_err()  { echo -e "${RED}[ERROR]${NC} $1"; }

# 1. 注册/登录管理员（若用户不存在则邮箱注册需要验证码，这里直接使用用户名登录接口演示）
# 为简化压测，假设 auth 服务已存在管理员账号，或调用 UsernameLogin
login_admin() {
  local resp=$(curl -s -X POST "$AUTH_URL/api/auth/login/username" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"admin123\"}")
  ADMIN_TOKEN=$(echo "$resp" | grep -o '"token":"[^"]*' | cut -d'"' -f4)
  if [ -z "$ADMIN_TOKEN" ]; then
    log_err "管理员登录失败: $resp"
    exit 1
  fi
  log_info "管理员登录成功"
}

# 2. 创建秒杀活动
create_activity() {
  local start=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local end=$(date -u -d "+1 hour" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -v+1H -u +"%Y-%m-%dT%H:%M:%SZ")
  local resp=$(curl -s -X POST "$ADMIN_URL/api/admin/seckill/activities" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"title\":\"压测秒杀\",\"description\":\"stress test\",\"start_time\":\"$start\",\"end_time\":\"$end\"}")
  ACTIVITY_ID=$(echo "$resp" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
  if [ -z "$ACTIVITY_ID" ]; then
    log_err "创建活动失败: $resp"
    exit 1
  fi
  log_info "创建活动成功: $ACTIVITY_ID"
}

# 3. 创建 SKU
create_sku() {
  local resp=$(curl -s -X POST "$ADMIN_URL/api/admin/seckill/skus" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"activity_id\":\"$ACTIVITY_ID\",\"title\":\"压测商品\",\"original_price\":10000,\"seckill_price\":100,\"stock\":100}")
  SKU_ID=$(echo "$resp" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
  if [ -z "$SKU_ID" ]; then
    log_err "创建 SKU 失败: $resp"
    exit 1
  fi
  log_info "创建 SKU 成功: $SKU_ID (库存 100)"
}

# 4. 上线 + 预热
online_and_preheat() {
  curl -s -X POST "$ADMIN_URL/api/admin/seckill/activities/$ACTIVITY_ID/online" \
    -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null
  log_info "活动已上线"

  curl -s -X POST "$ADMIN_URL/api/admin/seckill/activities/$ACTIVITY_ID/preheat" \
    -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null
  log_info "活动已预热"
}

# 5. 普通用户登录
token_pool=()
login_users() {
  local n=${1:-100}
  for i in $(seq 1 $n); do
    local resp=$(curl -s -X POST "$AUTH_URL/api/auth/login/username" \
      -H 'Content-Type: application/json' \
      -d "{\"username\":\"user$i\",\"password\":\"pass$i\"}")
    local t=$(echo "$resp" | grep -o '"token":"[^"]*' | cut -d'"' -f4)
    [ -n "$t" ] && token_pool+=("$t")
  done
  log_info "已准备 ${#token_pool[@]} 个用户 token"
}

# 6. 为每个用户领取秒杀令牌
acquire_tokens() {
  declare -gA USER_TOKENS
  local idx=0
  for t in "${token_pool[@]}"; do
    idx=$((idx+1))
    local resp=$(curl -s -X POST "$BASE_URL/api/seckill/token" \
      -H "Authorization: Bearer $t" \
      -H 'Content-Type: application/json' \
      -d "{\"activity_id\":\"$ACTIVITY_ID\",\"sku_id\":\"$SKU_ID\"}")
    local tok=$(echo "$resp" | grep -o '"token":"[^"]*' | cut -d'"' -f4)
    [ -n "$tok" ] && USER_TOKENS[$idx]="$tok"
  done
  log_info "已领取 ${#USER_TOKENS[@]} 个秒杀令牌"
}

# 7. 压测下单
stress_submit() {
  local concurrency=${1:-100}
  local total=${2:-1000}
  log_info "开始压测: concurrency=$concurrency, total=$total"

  if command -v hey >/dev/null 2&; then
    # 构造请求体文件，每个请求使用不同 token 需要更复杂脚本；这里使用单一 token 测试接口可用性
    echo '{"activity_id":"'$ACTIVITY_ID'","sku_id":"'$SKU_ID'","token":"'${USER_TOKENS[1]}'","quantity":1}' > /tmp/seckill_body.json
    hey -z 30s -c $concurrency -m POST -T application/json -D /tmp/seckill_body.json "$BASE_URL/api/seckill/orders"
  else
    log_info "未检测到 hey，使用 curl 并行简单压测"
    local pids=()
    for i in $(seq 1 $concurrency); do
      (
        local token=${USER_TOKENS[$i]:-${USER_TOKENS[1]}}
        for _ in $(seq 1 $((total/concurrency))); do
          curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BASE_URL/api/seckill/orders" \
            -H "Authorization: Bearer $token" \
            -H 'Content-Type: application/json' \
            -d "{\"activity_id\":\"$ACTIVITY_ID\",\"sku_id\":\"$SKU_ID\",\"token\":\"$token\",\"quantity\":1}"
        done
      ) &
      pids+=($!)
    done
    for pid in "${pids[@]}"; do wait "$pid"; done
  fi
}

# 8. 校验库存一致性
check_stock() {
  local resp=$(curl -s "$ADMIN_URL/api/admin/seckill/skus/$SKU_ID" \
    -H "Authorization: Bearer $ADMIN_TOKEN")
  local sold=$(echo "$resp" | grep -o '"sold":[0-9]*' | cut -d: -f2)
  log_info "SKU sold=$sold (应 <= 100)"
}

# 主流程
login_admin
create_activity
create_sku
online_and_preheat
login_users 100
acquire_tokens
stress_submit 50 500
check_stock

log_info "压测完成，请检查 logs 与数据库订单量"

#!/bin/bash
# 商城系统冒烟测试脚本
# 前置条件：已通过 make dev 启动所有服务

set -e

BASE_URL=${BASE_URL:-http://localhost:10002}
ADMIN_URL=${ADMIN_URL:-http://localhost:10001}
AUTH_URL=${AUTH_URL:-http://localhost:10000}

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_err()  { echo -e "${RED}[ERROR]${NC} $1"; }

# 1. 管理员登录
login_admin() {
  local resp=$(curl -s -X POST "$AUTH_URL/api/auth/login/username" \
    -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"admin123"}')
  ADMIN_TOKEN=$(echo "$resp" | grep -o '"token":"[^"]*' | cut -d'"' -f4)
  if [ -z "$ADMIN_TOKEN" ]; then
    log_err "管理员登录失败: $resp"
    exit 1
  fi
  log_info "管理员登录成功"
}

# 2. 创建商品
create_product() {
  local resp=$(curl -s -X POST "$ADMIN_URL/api/admin/mall/products" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"spu_code":"SPU001","name":"测试商品","category_id":"cat1","brand":"TestBrand","main_image":"http://example.com/img.jpg","detail":"{}"}')
  PRODUCT_ID=$(echo "$resp" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
  if [ -z "$PRODUCT_ID" ]; then
    log_err "创建商品失败: $resp"
    exit 1
  fi
  log_info "创建商品成功: $PRODUCT_ID"
}

# 3. 创建 SKU
create_sku() {
  local resp=$(curl -s -X POST "$ADMIN_URL/api/admin/mall/skus" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"product_id\":\"$PRODUCT_ID\",\"sku_code\":\"SKU001\",\"name\":\"测试SKU\",\"specs\":\"{}\",\"price\":1000,\"stock\":10}")
  SKU_ID=$(echo "$resp" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
  if [ -z "$SKU_ID" ]; then
    log_err "创建 SKU 失败: $resp"
    exit 1
  fi
  log_info "创建 SKU 成功: $SKU_ID (库存 10)"
}

# 4. 普通用户登录
login_user() {
  local resp=$(curl -s -X POST "$AUTH_URL/api/auth/login/username" \
    -H 'Content-Type: application/json' \
    -d '{"username":"user1","password":"pass1"}')
  USER_TOKEN=$(echo "$resp" | grep -o '"token":"[^"]*' | cut -d'"' -f4)
  if [ -z "$USER_TOKEN" ]; then
    log_err "用户登录失败: $resp"
    exit 1
  fi
  log_info "用户登录成功"
}

# 5. 商品列表
list_products() {
  local resp=$(curl -s "$BASE_URL/api/mall/products")
  log_info "商品列表: $(echo "$resp" | grep -o '"total":[0-9]*' | cut -d: -f2) 条"
}

# 6. 商品详情
get_product() {
  local resp=$(curl -s "$BASE_URL/api/mall/products/$PRODUCT_ID")
  log_info "商品详情: $(echo "$resp" | grep -o '"name":"[^"]*' | head -1)"
}

# 7. 创建订单
create_order() {
  local idemp=$(date +%s%N)
  local resp=$(curl -s -X POST "$BASE_URL/api/mall/orders" \
    -H "Authorization: Bearer $USER_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"sku_id\":\"$SKU_ID\",\"quantity\":2,\"idempotency_key\":\"$idemp\"}")
  ORDER_ID=$(echo "$resp" | grep -o '"order_id":"[^"]*' | cut -d'"' -f4)
  if [ -z "$ORDER_ID" ]; then
    log_err "创建订单失败: $resp"
    exit 1
  fi
  log_info "创建订单成功: $ORDER_ID"
}

# 8. 订单详情
get_order() {
  local resp=$(curl -s "$BASE_URL/api/mall/orders/$ORDER_ID" \
    -H "Authorization: Bearer $USER_TOKEN")
  log_info "订单详情: status=$(echo "$resp" | grep -o '"status":[0-9]*' | cut -d: -f2)"
}

# 9. 取消订单
cancel_order() {
  local resp=$(curl -s -X POST "$BASE_URL/api/mall/orders/$ORDER_ID/cancel" \
    -H "Authorization: Bearer $USER_TOKEN")
  log_info "取消订单成功"
}

# 10. 验证库存回滚
check_stock() {
  local resp=$(curl -s "$ADMIN_URL/api/admin/mall/skus/$SKU_ID" \
    -H "Authorization: Bearer $ADMIN_TOKEN")
  local stock=$(echo "$resp" | grep -o '"stock":[0-9]*' | cut -d: -f2)
  local sold=$(echo "$resp" | grep -o '"sold":[0-9]*' | cut -d: -f2)
  log_info "SKU stock=$stock, sold=$sold (应 stock=10, sold=0)"
  if [ "$stock" != "10" ] || [ "$sold" != "0" ]; then
    log_err "库存回滚异常"
    exit 1
  fi
}

# 主流程
login_admin
create_product
create_sku
login_user
list_products
get_product
create_order
get_order
cancel_order
check_stock

log_info "商城冒烟测试通过"

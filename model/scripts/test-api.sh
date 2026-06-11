#!/bin/bash

# 注意：不要加 set -e，测试脚本需要跑完所有检查再汇总

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 服务端点
AUTH_URL="http://localhost:10000"
APP_URL="http://localhost:10002"
ADMIN_URL="http://localhost:10001"

# 测试计数
PASS=0
FAIL=0

# 辅助函数：发送请求
call() {
    local method=$1
    local url=$2
    local body=$3
    local headers=$4
    local desc=$5

    echo -e "\n${BLUE}▶ $desc${NC}"
    echo "  $method $url"

    if [ -n "$body" ]; then
        echo "  Body: $body"
    fi

    if [ -n "$headers" ]; then
        echo "  Headers: $headers"
    fi

    local http_code
    local response

    if [ -n "$body" ]; then
        if [ -n "$headers" ]; then
            response=$(curl -s -w "\n%{http_code}" -X "$method" "$url" \
                -H "Content-Type: application/json" \
                -H "$headers" \
                -d "$body" 2>/dev/null || echo -e "\n000")
        else
            response=$(curl -s -w "\n%{http_code}" -X "$method" "$url" \
                -H "Content-Type: application/json" \
                -d "$body" 2>/dev/null || echo -e "\n000")
        fi
    else
        if [ -n "$headers" ]; then
            response=$(curl -s -w "\n%{http_code}" -X "$method" "$url" \
                -H "$headers" 2>/dev/null || echo -e "\n000")
        else
            response=$(curl -s -w "\n%{http_code}" -X "$method" "$url" 2>/dev/null || echo -e "\n000")
        fi
    fi

    http_code=$(echo "$response" | tail -n1)
    body_content=$(echo "$response" | sed '$d')

    if [ "$http_code" = "000" ]; then
        echo -e "${RED}  ✗ 连接失败（服务可能没启动）${NC}"
        ((FAIL++))
        return 1
    elif [ "$http_code" -ge 200 ] && [ "$http_code" -lt 300 ]; then
        echo -e "${GREEN}  ✓ HTTP $http_code${NC}"
        echo "  Response: $body_content"
        ((PASS++))
        return 0
    else
        echo -e "${RED}  ✗ HTTP $http_code${NC}"
        echo "  Response: $body_content"
        ((FAIL++))
        return 1
    fi
}

# 提取 JSON 字段
json_get() {
    echo "$1" | grep -o '"'$2'"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo ""
}

echo "========================================"
echo "  College Career Agent API 测试脚本"
echo "========================================"

# =============================================================================
# 1. 健康检查
# =============================================================================
echo -e "\n${YELLOW}【1/3】健康检查${NC}"

call GET "$AUTH_URL/api/health" "" "" "Auth 服务健康检查"
call GET "$APP_URL/api/health" "" "" "App 服务健康检查"
call GET "$ADMIN_URL/api/health" "" "" "Admin 服务健康检查"

# =============================================================================
# 2. 注册流程（可选，需要真实邮箱接收验证码）
# =============================================================================
echo -e "\n${YELLOW}【2/3】Auth 认证流程${NC}"

# 2.1 发送验证码（需要真实邮箱，默认跳过）
# call POST "$AUTH_URL/api/auth/send_code" \
#     '{"email":"your-email@example.com"}' \
#     "" \
#     "发送验证码（请替换真实邮箱）"

echo -e "\n${YELLOW}  提示: 注册需要邮箱验证码，测试前请确保:${NC}"
echo "    1. 用真实邮箱调用 POST /api/auth/send_code 获取验证码"
echo "    2. 再调用 POST /api/auth/register/email 完成注册"

# 2.2 用户名登录（需要已存在的用户）
echo -e "\n${BLUE}▶ 用户名登录（默认用户: admin / 123456）${NC}"
LOGIN_RESP=$(curl -s -X POST "$AUTH_URL/api/auth/login/username" \
    -H "Content-Type: application/json" \
    -d '{"username":"admin","password":"123456"}' 2>/dev/null || echo '{"token":""}')

TOKEN=$(json_get "$LOGIN_RESP" "token")

if [ -n "$TOKEN" ]; then
    echo -e "${GREEN}  ✓ 登录成功${NC}"
    echo "  Token: ${TOKEN:0:30}..."
    ((PASS++))
else
    echo -e "${RED}  ✗ 登录失败（用户不存在，请先注册）${NC}"
    echo "  Response: $LOGIN_RESP"
    ((FAIL++))
    TOKEN=""
fi

# =============================================================================
# 3. 需要 Token 的接口（登录成功后才测试）
# =============================================================================
echo -e "\n${YELLOW}【3/3】鉴权接口测试${NC}"

if [ -n "$TOKEN" ]; then
    AUTH_HEADER="Authorization: Bearer $TOKEN"

    # 3.1 验证 token
    call POST "$AUTH_URL/api/auth/validate" \
        "{\"token\":\"$TOKEN\"}" \
        "" \
        "验证 Token"

    # 3.2 获取用户信息
    call GET "$AUTH_URL/api/user/info" "" "$AUTH_HEADER" "获取用户信息"

    # 3.3 获取个人资料
    call GET "$AUTH_URL/api/user/profile" "" "$AUTH_HEADER" "获取个人资料"

    # 3.4 简历列表
    call GET "$APP_URL/api/resumes" "" "$AUTH_HEADER" "获取简历列表"

    # 3.5 Admin 用户列表（需要 admin 角色，普通用户会 403）
    echo -e "\n${BLUE}▶ Admin 用户列表${NC}"
    call GET "$ADMIN_URL/api/admin/users" "" "$AUTH_HEADER" "Admin 用户列表"

    # 3.6 职业规划对话（需要 LLM 配置，可能超时）
    echo -e "\n${BLUE}▶ 职业规划对话（需要 LLM 配置，可能较慢）${NC}"
    call POST "$APP_URL/api/chat" \
        '{"message":"我是一名计算机专业的大三学生，未来想做后端开发，有什么建议？"}' \
        "$AUTH_HEADER" \
        "职业规划对话（LLM）"

else
    echo -e "${YELLOW}  跳过（未获取到 Token，请先注册并登录）${NC}"
fi

# =============================================================================
# 汇总
# =============================================================================
echo -e "\n========================================"
echo -e "  测试结果: ${GREEN}通过 $PASS${NC}  ${RED}失败 $FAIL${NC}"
echo "========================================"

if [ $FAIL -gt 0 ]; then
    exit 1
fi

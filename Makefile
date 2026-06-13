adminPWD = cmd/admin
adminDesc = $(adminPWD)/desc

appPWD = cmd/app
appDesc = $(appPWD)/desc

authRpcPWD = services/rpc/auth
authRpcDesc = $(authRpcPWD)/desc

seckillRpcPWD = services/rpc/seckill
seckillRpcDesc = $(seckillRpcPWD)/desc

# =============================================================================
# 帮助
# =============================================================================
.PHONY: help
help:
	@echo "常用命令:"
	@echo "  make dev            一键启动全部服务（本地 go run + Docker 基础设施）"
	@echo "  make dev-stop       一键停止全部本地服务"
	@echo "  make docker-up      docker-compose 启动全部服务"
	@echo "  make docker-down    docker-compose 停止"
	@echo "  make api-all        生成所有服务的代码"

# =============================================================================
# API / RPC 代码生成
# =============================================================================
.PHONY: api-all
api-all:
	@goctl -v
	@echo "生成 admin API..."
	@goctl env -w GOCTL_EXPERIMENTAL=off
	@goctl api format --dir $(adminDesc)
	@goctl api go -home ./tpls -api $(adminPWD)/desc/admin.api -dir $(adminPWD) -style go_zero
	@mkdir -p docs
	@goctl api swagger -filename admin-api -api $(adminDesc)/admin.api -dir ./docs
	@rm -f $(adminPWD)/admin.go $(adminPWD)/etc/admin.yaml
	@echo "生成 app API..."
	@goctl api format --dir $(appDesc)
	@goctl api go -home ./tpls -api $(appPWD)/desc/app.api -dir $(appPWD) -style go_zero
	@mkdir -p docs
	@goctl api swagger -filename app-api -api $(appDesc)/app.api -dir ./docs
	@rm -f $(appPWD)/app.go $(appPWD)/etc/app.yaml
	@echo "生成 auth RPC..."
	@goctl api format --dir $(authRpcDesc)
	@goctl rpc protoc $(authRpcDesc)/auth.proto --go_out=$(authRpcPWD) --go-grpc_out=$(authRpcPWD) --zrpc_out=$(authRpcPWD) --style=go_zero -m -I . -I $(authRpcDesc)
	@rm -f $(authRpcPWD)/auth.go $(authRpcPWD)/etc/auth.yaml
	@echo "生成 seckill RPC..."
	@goctl rpc protoc $(seckillRpcDesc)/seckill.proto --go_out=$(seckillRpcPWD) --go-grpc_out=$(seckillRpcPWD) --zrpc_out=$(seckillRpcPWD) --style=go_zero -m -I . -I $(seckillRpcDesc)
	@rm -f $(seckillRpcPWD)/seckill.go $(seckillRpcPWD)/etc/seckill.yaml
	@echo "生成 gateway proto descriptor..."
	@mkdir -p $(authRpcPWD)/gateway
	@protoc -I $(authRpcDesc) --descriptor_set_out=$(authRpcPWD)/gateway/auth.pb --include_imports $(authRpcDesc)/auth.proto
	@echo "所有代码生成完成"

# =============================================================================
# 本地开发
# =============================================================================
.PHONY: dev
dev:
	@bash scripts/dev.sh

.PHONY: dev-stop
dev-stop:
	@bash scripts/dev-stop.sh

# =============================================================================
# Docker
# =============================================================================
.PHONY: docker-up
docker-up:
	@docker compose up --build -d

.PHONY: docker-down
docker-down:
	@docker compose down

.PHONY: docker-logs
docker-logs:
	@docker compose logs -f

# =============================================================================
# 开发工具
# =============================================================================
.PHONY: test
test:
	@go test -v ./...

.PHONY: smoke-test
smoke-test:
	@echo "🔥 冒烟测试..."
	@echo ""
	@echo "1. Auth Gateway 健康检查"
	@curl -s -o /dev/null -w "   HTTP %{http_code}\n" http://localhost:10000/api/health || echo "   ❌ 连接失败"
	@echo ""
	@echo "2. App 服务健康检查"
	@curl -s -o /dev/null -w "   HTTP %{http_code}\n" http://localhost:10002/api/health || echo "   ❌ 连接失败"
	@echo ""
	@echo "3. Admin 服务健康检查"
	@curl -s -o /dev/null -w "   HTTP %{http_code}\n" http://localhost:10001/api/health || echo "   ❌ 连接失败"
	@echo ""
	@echo "4. PostgreSQL 连通性"
	@docker compose exec -T postgres pg_isready -U root >/dev/null 2>&1 && echo "   ✅ 正常" || echo "   ❌ 异常"
	@echo ""
	@echo "5. Redis 连通性"
	@docker compose exec -T redis redis-cli -a 12345678 ping >/dev/null 2>&1 && echo "   ✅ 正常" || echo "   ❌ 异常"
	@echo ""
	@echo "✅ 冒烟测试完成"

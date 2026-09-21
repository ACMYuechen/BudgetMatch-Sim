SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
.PHONY: help run stop web api

help: ## 查看本地开发命令
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  make %-12s %s\n", $$1, $$2}' Makefile

run: ## 启动本地后端和 Docker 依赖
	@bash scripts/dev.sh

stop: ## 停止本地后端和依赖，保留数据卷
	@bash scripts/dev-stop.sh

web: ## 前台启动前端，Ctrl+C 停止
	@npm --prefix web-ui run dev

api: ## 生成 API、RPC 和 Swagger，执行后检查差异
	@goctl -v
	@goctl env -w GOCTL_EXPERIMENTAL=off
	@mkdir -p docs
	@for name in admin app; do \
		dir="cmd/$$name"; \
		goctl api format --dir "$$dir/desc"; \
		goctl api go -home ./tpls -api "$$dir/desc/$$name.api" -dir "$$dir" -style go_zero; \
		goctl api swagger -filename "$$name-api" -api "$$dir/desc/$$name.api" -dir ./docs; \
		rm -f "$$dir/$$name.go" "$$dir/etc/$$name.yaml"; \
	done
	@for name in auth seckill mall agent payment; do \
		dir="services/rpc/$$name"; \
		goctl rpc protoc "$$dir/proto/$$name.proto" --go_out="$$dir" --go-grpc_out="$$dir" \
			--zrpc_out="$$dir" --style=go_zero -m -I . -I "$$dir/proto"; \
		rm -f "$$dir/$$name.go" "$$dir/etc/$$name.yaml"; \
	done

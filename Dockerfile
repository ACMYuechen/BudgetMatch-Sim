# 构建阶段
FROM golang:1.26-alpine AS dependencies

WORKDIR /build

# 安装 git（某些依赖可能需要）
RUN apk add --no-cache git

# 先复制 go.mod 和 go.sum，利用 Docker 缓存
COPY go.mod go.sum ./
RUN go mod download

# 编译阶段复用独立的依赖层，供 CI 中的并行服务构建共享缓存
FROM dependencies AS builder

# 复制全部源代码
COPY . .

# 构建参数：服务路径、入口包路径、端口
ARG SERVICE_PATH
ARG PORT

# 编译静态二进制
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ${SERVICE_PATH}

# 运行阶段
FROM alpine:3.19

# 安装 ca-certificates（HTTPS 请求需要）和时区数据
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/server .

# 从构建阶段复制配置文件
ARG SERVICE_PATH
COPY --from=builder /build/${SERVICE_PATH}/etc/config.yaml ./etc/

# 暴露端口
ARG PORT
EXPOSE ${PORT}

# 使用非 root 用户运行
RUN adduser -D -u 1000 appuser
USER appuser

ENTRYPOINT ["./server", "-f", "etc/config.yaml"]

FROM golang:1.26.8-alpine AS builder
WORKDIR /build
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY services ./services
COPY infra ./infra
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out/bin && \
    for service in auth mall seckill agent payment; do \
      CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o "/out/bin/${service}" "./services/rpc/${service}" || exit 1; \
    done && \
    for service in app admin; do \
      CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o "/out/bin/${service}" "./cmd/${service}" || exit 1; \
    done

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 appuser
WORKDIR /app
COPY --from=builder /out/bin ./bin
USER appuser

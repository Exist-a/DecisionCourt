# BuildKit 内置 parser(无需 # syntax=docker/dockerfile:1.6,删了因为
# 国内环境 auth.docker.io 不通拉不到 frontend parser 镜像)
#
# v0.8.3 安全(P0-3):加 USER 指令,容器以非 root 运行。
#
# 多阶段构建:
#   builder  → 编译 Go binary(临时镜像)
#   runtime  → alpine + binary + 非 root user
#
# 为什么不用 distroless:alpine 体积稍大但 apk add 灵活,适合加 ca-certificates / tzdata。
#
# 国内构建优化:GOPROXY=goproxy.cn + alpine 镜像源(避免 VPN 穿透到 Docker container)
# 生产部署时(阿里云内网)也适用。
FROM golang:1.26-alpine AS builder

# 国内镜像源(可被 --build-arg 覆盖):goproxy.cn + tuna alpine
ARG GOPROXY=https://goproxy.cn,direct
ARG ALPINE_REPO=https://mirrors.tuna.tsinghua.edu.cn/alpine

ENV GOPROXY=${GOPROXY}

WORKDIR /app
RUN sed -i "s|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g" /etc/apk/repositories && \
    apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server

# ============= runtime stage =============
FROM alpine:3.20

# v0.8.3 安全：升级到具体版本(非 :latest),同步加 ca-certificates + tzdata
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app
COPY --from=builder /app/server /app/server

# v0.8.3 安全：USER 切到非 root。
# 镜像里的 GID 1001 UID 1001 与 docker-compose.yml 的 user 字段对齐。
USER appuser

EXPOSE 8080

# v0.8.3 安全：加 HEALTHCHECK(给 docker compose / k8s 用)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1

CMD ["./server"]

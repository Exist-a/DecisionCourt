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

# PR-B: version ldflags — 注入 main.version，避免启动日志撒谎 24 天
# （详见 production-retrospective-2026-08-05.md §4 P1-2）
# R13: 同一份 VERSION 也注入 promptlab.buildGitSHA —— 否则
# GET /api/v1/prompts/version 的 git_sha 恒为空，"线上跑的是哪个 commit 的
# prompt"只能靠猜，prompt 优化也就无法按版本归因。
# ⚠️ -X 的包路径写错是**静默失败**（实测：链接器 exit 0，不报错，变量仍为空）。
#    改这行后务必实测一次 —— 最省事的办法是部署后看一眼
#    `curl -s https://<域名>/api/v1/prompts/version | jq .data.git_sha`，
#    它应当等于镜像 tag（构建镜像时用 `--build-arg VERSION=$(git rev-parse HEAD)` 验证过，
#    2026-10-09 本地 build 得到 `version=verify1234` / `git_sha=verify1234`）。
# 默认 "dev" 用于 `docker build` 不传 --build-arg VERSION 的场景（本地调试）。
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
        -ldflags "-X main.version=${VERSION} \
                  -X github.com/decisioncourt/backend/internal/promptlab.buildGitSHA=${VERSION}" \
        -o /app/server ./cmd/server

# ============= runtime stage =============
FROM alpine:3.20

# v0.8.3 安全：升级到具体版本(非 :latest),同步加 ca-certificates + tzdata
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app
COPY --from=builder /app/server /app/server

# Prompt Lab (v1.0.3) 的 baseRules YAML。runtime 阶段只拷了 binary，
# 导致 promptlab 在容器里找不到 prompts/base.yaml → 每次启动 WARN +
# 永久降级到 hardcoded fallback（dev 栈 bind mount 源码，所以本地一直
# 走 YAML —— 线上跑的恰是没被测过的那条路）。把 YAML 一起打进来。
COPY --from=builder /app/prompts /app/prompts

# R12: 预建日志目录并归 appuser 所有。
# prod compose 把宿主机的 ./logs/backend 挂到 /app/logs；那个目录的属主是
# **宿主机** 决定的，镜像改不了（Docker 自动创建时会建成 root:root）。
# 在镜像里预建 /app/logs 有两个作用：① 明确声明"这个目录该归 appuser"；
# ② 若将来改成命名卷，Docker 会用镜像里目录的属主初始化卷 → 无需宿主机 chown。
RUN mkdir -p /app/logs && chown appuser:appgroup /app/logs

# v0.8.3 安全：USER 切到非 root。
# 镜像里的 UID/GID 是 10001（见上方 adduser -u 10001）；docker-compose.yml 的
# `user:` 必须与之一致，否则容器会以镜像里不存在的 uid 运行（见 R12）。
USER appuser

EXPOSE 8080

# v0.8.3 安全：加 HEALTHCHECK(给 docker compose / k8s 用)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1

CMD ["./server"]

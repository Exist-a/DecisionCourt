# BuildKit 内置 parser(无需 # syntax=docker/dockerfile:1.6,删了因为
# 国内环境 auth.docker.io 不通拉不到 frontend parser 镜像)
#
# v0.8.3 安全(P0-3 + P1-7):
#   1. 用 next start(prod)而非 pnpm dev(dev 会暴露 source maps + HMR)
#   2. 加 USER 指令,容器以非 root 运行
#   3. output: standalone 让镜像只含必需文件
#
# 注意：next.config.mjs 需配 `output: 'standalone'`(见 P2 修复)。
# 我们的 build 输出是 /app/.next/standalone + /app/public + /app/.next/static。

# ============= deps stage =============
FROM node:20-alpine AS deps
# pnpm-lock.yaml 是 lockfileVersion: 9.0,只支持 pnpm 9.x
# corepack pnpm@latest 装到 11.x 会撞 ERR_UNKNOWN_BUILTIN_MODULE
RUN corepack enable && corepack prepare pnpm@9.15.4 --activate
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# ============= builder stage =============
FROM node:20-alpine AS builder
RUN corepack enable && corepack prepare pnpm@9.15.4 --activate
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# v0.9 修复:NEXT_PUBLIC_* 是 build-time 变量,必须在 builder stage 通过 ARG 注入,
# 否则运行时浏览器 baseUrl="" → 所有 API 请求走 frontend 自身:3000 → 404。
# docker-compose.yml 通过 build.args 传递这些值。
ARG NEXT_PUBLIC_API_URL=http://backend:8080
ARG NEXT_PUBLIC_WS_URL=ws://backend:8080
ARG NEXT_PUBLIC_USE_MOCK=false
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_WS_URL=$NEXT_PUBLIC_WS_URL
ENV NEXT_PUBLIC_USE_MOCK=$NEXT_PUBLIC_USE_MOCK

# v0.8.3 安全:Next.js 标准 public/ 目录(放 favicon 等静态资源)。
# 即使这个项目目前没有静态资源,也 mkdir 保证 runtime stage 的
# COPY --from=builder /app/public 能命中空目录而不会崩。
RUN mkdir -p /app/public
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production
RUN pnpm build

# ============= runtime stage =============
FROM node:20-alpine AS runtime

RUN addgroup -g 10001 -S nodejs && \
    adduser -u 10001 -S nextjs -G nodejs

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

# standalone 输出含完整 node_modules 精简版
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public

USER nextjs

EXPOSE 3000

# v0.8.3 安全：next start 走 prod 模式
CMD ["node", "server.js"]

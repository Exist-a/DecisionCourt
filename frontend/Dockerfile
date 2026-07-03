# syntax=docker/dockerfile:1.6
# v0.8.3 安全(P0-3 + P1-7)：
#   1. 用 next start(prod)而非 pnpm dev(dev 会暴露 source maps + HMR)
#   2. 加 USER 指令,容器以非 root 运行
#   3. output: standalone 让镜像只含必需文件
#
# 注意：next.config.mjs 需配 `output: 'standalone'`(见 P2 修复)。
# 我们的 build 输出是 /app/.next/standalone + /app/public + /app/.next/static。

# ============= deps stage =============
FROM node:20-alpine AS deps
RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# ============= builder stage =============
FROM node:20-alpine AS builder
RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production
RUN pnpm build

# ============= runtime stage =============
FROM node:20-alpine AS runtime

RUN addgroup -g 1001 -S nodejs && \
    adduser -u 1001 -S nextjs -G nodejs

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

// v0.8.3 安全(P1-5 + P2-4)：
//   1. output: 'standalone' 让 docker 镜像只含必需文件
//   2. async headers() 注入全套 HTTP 安全头(CSP / HSTS / X-Frame-Options 等)
//
// 注意：next.config.mjs 在 build 时被读取,CSP nonce 模式需要 middleware
// 才能注入 nonce——我们这里用静态 self-only CSP,够 MVP 演示用。
//
// v0.9.1 修复：CSP connect-src 不再硬编码 localhost:8080。
// 之前写死导致生产域名(https://yourdomain.com / wss://yourdomain.com)
// 被 CSP block,WebSocket 连接直接拒,庭审无法实时同步。
// 现在从 runtime env 动态派生:NEXT_PUBLIC_API_URL / NEXT_PUBLIC_WS_URL
// (Next.js headers() 是 request-time 执行,改 .env 重启容器即生效)。
// 保留 localhost + backend:8080 兜底,本地开发 / SSR 直连 backend 不破。

// 把 ws:// → wss://,http:// → https:// 的小工具
const toHttps = (u) => (u && u.startsWith("http://") ? u.replace(/^http:/, "https:") : u);
const toWss = (u) => (u && u.startsWith("ws://") ? u.replace(/^ws:/, "wss:") : u);

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  poweredByHeader: false, // 不暴露 X-Powered-By
  reactStrictMode: true,
  // P1-5：HTTP 安全头(给所有响应加)
  async headers() {
    // v0.9.1：connect-src 派生自 env。客户端 bundle 用的 NEXT_PUBLIC_*
    // 是 build-time 内联;这里用 runtime 值(改 .env 即可),运行时与构建时
    // 必须一致(否则客户端连的 URL 跟 CSP 允许的不一样)。Fallback 给
    // localhost / backend 兼容本地开发 + SSR 直连 backend。
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
    const wsUrl = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080";
    const connectSrc = [
      "'self'",
      wsUrl,
      toWss(wsUrl),
      apiUrl,
      toHttps(apiUrl),
      // 本地开发 + 容器间 SSR 直连(绕开 Caddy)
      "http://localhost:8080",
      "ws://localhost:8080",
      "wss://localhost:8080",
      "http://backend:8080",
      "ws://backend:8080",
    ].join(" ");

    return [
      {
        source: '/:path*',
        headers: [
          // HSTS：强制 HTTPS(生产部署后 1 年内浏览器自动转 https)
          {
            key: 'Strict-Transport-Security',
            value: 'max-age=63072000; includeSubDomains; preload',
          },
          // 防 clickjacking
          { key: 'X-Frame-Options', value: 'DENY' },
          // 防 MIME sniffing
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          // 限制 referrer
          { key: 'Referrer-Policy', value: 'no-referrer' },
          // 关闭危险 API
          {
            key: 'Permissions-Policy',
            value: 'camera=(), microphone=(), geolocation=(), payment=()',
          },
          // CSP：MVP 简化版,允许 self + inline(Next.js 注入)
          // 真正严格需要 nonce + middleware,留作 P2
          {
            key: 'Content-Security-Policy',
            value: [
              "default-src 'self'",
              // Next.js dev 模式需要 unsafe-eval;prod 仍保留以防第三方库
              "script-src 'self' 'unsafe-inline' 'unsafe-eval'",
              "style-src 'self' 'unsafe-inline'",
              "img-src 'self' data: blob:",
              "font-src 'self' data:",
              // v0.9.1：动态派生(见上方 connectSrc 计算)
              `connect-src ${connectSrc}`,
              "frame-ancestors 'none'",
              "base-uri 'self'",
            ].join('; '),
          },
        ],
      },
    ];
  },
};

export default nextConfig;

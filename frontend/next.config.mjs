// v0.8.3 安全(P1-5 + P2-4)：
//   1. output: 'standalone' 让 docker 镜像只含必需文件
//   2. async headers() 注入全套 HTTP 安全头(CSP / HSTS / X-Frame-Options 等)
//
// 注意：next.config.mjs 在 build 时被读取,CSP nonce 模式需要 middleware
// 才能注入 nonce——我们这里用静态 self-only CSP,够 MVP 演示用。

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  poweredByHeader: false, // 不暴露 X-Powered-By
  reactStrictMode: true,
  // P1-5：HTTP 安全头(给所有响应加)
  async headers() {
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
              // 允许连后端(localhost / 容器内 backend)
              `connect-src 'self' ws://localhost:8080 wss://localhost:8080 http://localhost:8080 http://backend:8080`,
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

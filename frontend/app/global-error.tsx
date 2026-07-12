"use client";

// v0.10.17 silent-error-fix PR 7: Next.js App Router 根 ErrorBoundary
//
// global-error.tsx 是 Next.js 最后的兜底:
//   - 替换 ROOT html/body 结构 (其他 error.tsx 不能替换)
//   - 只在 root layout 抛出时触发
//   - 必须含 <html><body> 否则 Next.js 警告
//   - 必须 "use client"
//
// 极简实现: 黑白屏 + 重载按钮。无 Toast(ToastContainer 可能也崩了)。

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <html lang="zh-CN">
      <body
        style={{
          fontFamily:
            "-apple-system, BlinkMacSystemFont, 'PingFang SC', sans-serif",
          margin: 0,
          padding: 0,
          minHeight: "100vh",
          background: "#fafaf7",
          color: "#1a1a1a",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <div style={{ maxWidth: 480, textAlign: "center", padding: 24 }}>
          <h1
            style={{
              fontSize: 28,
              fontWeight: 600,
              marginBottom: 12,
              color: "#8b1e1e",
            }}
          >
            应用出错
          </h1>
          <p
            style={{
              fontSize: 14,
              color: "#666",
              marginBottom: 16,
              lineHeight: 1.6,
            }}
          >
            抱歉,应用遇到了未预期的错误,无法继续显示页面。
          </p>
          {error.message && (
            <pre
              style={{
                fontSize: 11,
                fontFamily: "monospace",
                color: "#999",
                background: "#f0f0eb",
                padding: 8,
                borderRadius: 4,
                marginBottom: 16,
                overflow: "auto",
                maxHeight: 120,
                textAlign: "left",
              }}
            >
              {error.message}
            </pre>
          )}
          {error.digest && (
            <p style={{ fontSize: 11, color: "#aaa", marginBottom: 16 }}>
              digest: {error.digest}
            </p>
          )}
          <button
            onClick={() => reset()}
            style={{
              padding: "10px 24px",
              fontSize: 14,
              background: "#1a1a1a",
              color: "#fff",
              border: "none",
              borderRadius: 6,
              cursor: "pointer",
              marginRight: 8,
            }}
          >
            重试
          </button>
          <button
            onClick={() => (window.location.href = "/")}
            style={{
              padding: "10px 24px",
              fontSize: 14,
              background: "#fff",
              color: "#1a1a1a",
              border: "1px solid #ccc",
              borderRadius: 6,
              cursor: "pointer",
            }}
          >
            返回首页
          </button>
        </div>
      </body>
    </html>
  );
}
"use client";

// v0.10.17 silent-error-fix PR 7: Next.js App Router 路由级 ErrorBoundary
//
// Next.js 在 app/<route>/error.tsx 路径下自动识别为该路由的 error boundary:
//   - 路由下任何组件 render 抛出 → 触发此 component
//   - 用户能看到友好错误页面而不是白屏 / dev console
//   - 必须 "use client" 才能用 useState/reset()
//
// 触发范围:
//   - /court/[id] 下任何 client component throw (applyCourtEvent 类型异常等)
//   - /verdict/[id] 页面 render throw
//   - 不会捕获: server component error (用 global-error.tsx), event handler error (用 toast)

import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { toastFatal } from "@/lib/errorBus";

export default function RouteError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    // v0.10.17 silent-error-fix: 错误时同时弹 toast,让用户立即看到反馈
    // (即使他们在错误页停留,toast 也在右下角持续显示)。
    toastFatal("页面渲染出错,已记录到日志", {
      code: "ROUTE_RENDER_ERROR",
      actions: [
        { type: "retry", label: "重试", onClick: () => reset() },
      ],
    });
    // 同时 console.error 保留 dev 排查能力
    console.error("[RouteError]", error);
  }, [error, reset]);

  return (
    <div className="min-h-screen bg-paper text-ink flex items-center justify-center paper-overlay">
      <div className="max-w-md mx-auto text-center px-6 py-12">
        <div className="text-5xl mb-4 text-seal">⚠</div>
        <h1 className="text-2xl font-display font-semibold mb-3">
          页面出了点问题
        </h1>
        <p className="text-sm text-inkSoft mb-2 leading-relaxed">
          渲染此页面时遇到错误。可能是数据格式异常或网络中断。
        </p>
        {error.message && (
          <p className="text-xs font-mono text-inkFaint mb-6 break-all">
            {error.message}
          </p>
        )}
        {error.digest && (
          <p className="text-[10px] font-mono text-inkFaint mb-6">
            digest: {error.digest}
          </p>
        )}
        <div className="flex gap-3 justify-center">
          <Button
            onClick={() => reset()}
            className="bg-ink text-paper hover:bg-inkSoft"
          >
            重试
          </Button>
          <Button
            onClick={() => (window.location.href = "/")}
            variant="outline"
          >
            返回首页
          </Button>
        </div>
        <p className="mt-8 text-[11px] text-inkFaint">
          如反复出现,请
          <a
            href="https://github.com/decisioncourt/issues"
            className="ml-1 underline"
          >
            报告问题
          </a>
          并附上 digest 编号。
        </p>
      </div>
    </div>
  );
}
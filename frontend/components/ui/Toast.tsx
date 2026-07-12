"use client";

// v0.10.17 silent-error-fix PR 2: 前端 Toast UI 组件
//
// 数据层在 lib/toastStore.ts (无 React/JSX, 可被测试 import)。
// 这里只放 UI 渲染: <Toast /> 单条 + <ToastContainer /> 顶层容器。
//
// 分类:
//  - 普通 toast   → 右下角堆叠
//  - degraded     → 顶部 banner (code 以 "BANNER_" 开头)

import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Info,
  X,
} from "lucide-react";
import { useEffect, useRef } from "react";

import {
  type ToastAction,
  type ToastItem,
  type ToastLevel,
  useToastStore,
} from "@/lib/toastStore";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

// ============== 组件 ==============

const ICONS: Record<ToastLevel, React.ComponentType<{ className?: string }>> = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  error: AlertCircle,
};

const LEVEL_STYLES: Record<ToastLevel, string> = {
  info: "border-blue-200 bg-blue-50 text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100",
  success:
    "border-green-200 bg-green-50 text-green-900 dark:border-green-800 dark:bg-green-950 dark:text-green-100",
  warning:
    "border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100",
  error:
    "border-destructive/40 bg-destructive/10 text-destructive dark:border-destructive/50 dark:bg-destructive/20",
};

interface ToastProps {
  toast: ToastItem;
  onDismiss: (id: string) => void;
}

function Toast({ toast, onDismiss }: ToastProps) {
  const Icon = ICONS[toast.level];

  return (
    <div
      role={toast.level === "error" ? "alert" : "status"}
      aria-live={toast.level === "error" ? "assertive" : "polite"}
      data-testid={`toast-${toast.id}`}
      data-code={toast.code ?? ""}
      className={cn(
        "group pointer-events-auto flex w-full max-w-md items-start gap-3 rounded-lg border p-3 shadow-md transition-all",
        LEVEL_STYLES[toast.level],
      )}
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="flex-1 min-w-0">
        {toast.title && (
          <div className="font-semibold text-sm leading-tight">
            {toast.title}
          </div>
        )}
        <div className="text-sm leading-snug">{toast.message}</div>
        {toast.detail && (
          <div
            data-testid="toast-detail"
            className="mt-1 text-xs opacity-70 font-mono break-all"
          >
            {toast.detail}
          </div>
        )}
        {toast.actions && toast.actions.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-2">
            {toast.actions.map((a, i) => (
              <Button
                key={`${toast.id}-action-${i}`}
                variant={toast.level === "error" ? "default" : "outline"}
                size="xs"
                onClick={() => {
                  if (a.onClick) a.onClick();
                  onDismiss(toast.id);
                }}
              >
                {a.label}
              </Button>
            ))}
          </div>
        )}
      </div>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="关闭"
        onClick={() => onDismiss(toast.id)}
        className="opacity-60 hover:opacity-100"
      >
        <X className="size-3" />
      </Button>
    </div>
  );
}

/**
 * 顶层 Container,需要在 app/layout.tsx 挂载一次。
 * 自动管理 durationMs > 0 的 toast 在到期后自动 dismiss。
 * 区分 transient toast (右下角堆叠) vs degraded banner (顶部横幅,以 BANNER_ code 识别)。
 */
export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);

  const timersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  useEffect(() => {
    const timers = timersRef.current;
    const liveIds = new Set(toasts.map((t) => t.id));

    // 清理已消失 toast 的 timer
    Object.keys(timers).forEach((id) => {
      if (!liveIds.has(id)) {
        clearTimeout(timers[id]);
        delete timers[id];
      }
    });

    // 给新 toast 设 timer
    for (const toast of toasts) {
      if (toast.durationMs <= 0) continue;
      if (timers[toast.id] !== undefined) continue;
      const elapsed = Date.now() - toast.createdAt;
      const remaining = toast.durationMs - elapsed;
      timers[toast.id] = setTimeout(
        () => dismiss(toast.id),
        remaining <= 0 ? 0 : remaining,
      );
    }

    return () => {
      Object.values(timers).forEach(clearTimeout);
      Object.keys(timers).forEach((k) => delete timers[k]);
    };
    // dismiss 是 zustand selector 拿到的稳定函数引用
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toasts]);

  const transient = toasts.filter(
    (t) => t.code === undefined || !t.code.startsWith("BANNER_"),
  );
  const banners = toasts.filter((t) => t.code?.startsWith("BANNER_"));

  return (
    <>
      {banners.length > 0 && (
        <div
          data-testid="toast-banner"
          className="pointer-events-none fixed inset-x-0 top-0 z-[60] flex flex-col items-center gap-2 px-4 pt-2"
        >
          {banners.map((t) => (
            <Toast key={t.id} toast={t} onDismiss={dismiss} />
          ))}
        </div>
      )}
      <div
        data-testid="toast-stack"
        className="pointer-events-none fixed bottom-4 right-4 z-50 flex flex-col gap-2"
      >
        {transient.map((t) => (
          <Toast key={t.id} toast={t} onDismiss={dismiss} />
        ))}
      </div>
    </>
  );
}

// 重新导出 ToastAction 让业务代码可以从 components/ui/Toast 单点导入
export type { ToastAction };
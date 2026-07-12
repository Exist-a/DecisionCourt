// v0.10.17 silent-error-fix PR 2: Toast 数据层(无 UI 依赖)
//
// 拆分原因: errorBus.ts 和测试都需要 useToastStore + severityFromUFE,
// 但 Toast.tsx 含 JSX + React 客户端组件,不能被 Node --strip-types
// 直接 import (Node 不解析 paths alias + 不解析 .tsx)。
//
// 这里只放纯 TS: zustand store + 类型 + 工具函数。UI 渲染放在
// components/ui/Toast.tsx。

import { create } from "zustand";

// ============== 类型 ==============

export type ToastLevel = "info" | "success" | "warning" | "error";

export interface ToastAction {
  /** 后端 RecoveryAction.type 或本地语义 ("retry" / "dismiss") */
  type: string;
  label: string;
  onClick?: () => void;
  /** 后端 action 名 (e.g. "restart_opening"),由调用方解析 */
  backendAction?: string;
  backendPayload?: Record<string, unknown>;
}

export interface ToastItem {
  id: string;
  level: ToastLevel;
  title?: string;
  message: string;
  detail?: string;
  code?: string;
  /** 0 = 不自动消失 */
  durationMs: number;
  actions?: ToastAction[];
  createdAt: number;
}

// ============== zustand store ==============

interface ToastState {
  toasts: ToastItem[];
  push: (toast: Omit<ToastItem, "id" | "createdAt"> & { id?: string }) => string;
  dismiss: (id: string) => void;
  clear: () => void;
  upsert: (toast: Omit<ToastItem, "id" | "createdAt"> & { id: string }) => void;
}

let counter = 0;
const genId = () =>
  `toast-${Date.now()}-${(counter++).toString(36)}-${Math.random()
    .toString(36)
    .slice(2, 6)}`;

export const useToastStore = create<ToastState>((set, get) => ({
  toasts: [],
  push: (toast) => {
    const id = toast.id ?? genId();
    const existing = get().toasts.findIndex((t) => t.id === id);
    const item: ToastItem = { ...toast, id, createdAt: Date.now() };
    if (existing >= 0) {
      set((s) => ({
        toasts: s.toasts.map((t, i) => (i === existing ? item : t)),
      }));
    } else {
      set((s) => ({ toasts: [...s.toasts, item] }));
    }
    return id;
  },
  upsert: (toast) => {
    get().push(toast);
  },
  dismiss: (id) => {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
  },
  clear: () => set({ toasts: [] }),
}));

// ============== helpers ==============

/**
 * 把后端 UFE class 映射到 ToastLevel + durationMs。
 * 映射规则见 silent-error-fix-plan.md §2.3.3。
 */
export function severityFromUFE(className: string): {
  level: ToastLevel;
  durationMs: number;
} {
  switch (className) {
    case "user_input":
      return { level: "info", durationMs: 3000 };
    case "transient":
      return { level: "warning", durationMs: 5000 };
    case "degraded":
      return { level: "warning", durationMs: 0 };
    case "fatal":
      return { level: "error", durationMs: 0 };
    default:
      return { level: "info", durationMs: 4000 };
  }
}
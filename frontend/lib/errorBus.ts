// v0.10.17 silent-error-fix PR 2: errorBus
//
// 职责:
//  1. 定义 UserFacingErrorPayload(前端镜像后端 courtroom/errors.go 的 UFE JSON 结构)
//  2. handleUserFacingError(payload):把 UFE 翻译成 toast + 动作按钮
//  3. handleWsError(event.payload):WS event.type === "error" 时调用
//  4. handleApiError(httpResponse):HTTP 4xx/5xx 时尝试解析 user_facing_error 字段
//
// 调用方:
//  - lib/api.ts::fetchJson 在 !res.ok 时调用 handleApiError
//  - components/courtroom/CourtroomScene 的 WS onmessage 在 event.type === "error" 时调 handleWsError
//  - app/auth/login 页的 catch 块(PR 4)

import {
  type ToastAction,
  type ToastItem,
  severityFromUFE,
  useToastStore,
} from "./toastStore.ts";

// ============== 类型镜像 ==============
// 与后端 courtroom/errors.go 保持字段一致。doc:
// https://github.com/.../docs/adr/0024-silent-error-fix-pr1.md §2.1

export interface RecoveryActionWire {
  type: string;
  label: string;
  action?: string;
  payload?: Record<string, unknown>;
  navigate_to?: string;
}

export interface UserFacingErrorPayload {
  class:
    | "user_input"
    | "transient"
    | "degraded"
    | "fatal"
    | string;
  code: string;
  message: string;
  detail?: string;
  recoverable?: boolean;
  recovery?: RecoveryActionWire[] | null;
  session_uuid?: string;
}

// 安全检查:任意对象能否当作 UFE 处理?
export function isUserFacingError(x: unknown): x is UserFacingErrorPayload {
  if (!x || typeof x !== "object") return false;
  const obj = x as Record<string, unknown>;
  return (
    typeof obj["class"] === "string" &&
    typeof obj["code"] === "string" &&
    typeof obj["message"] === "string"
  );
}

// ============== 通用处理入口 ==============

/**
 * 核心:把 UFE 翻译成 toast 并 push 到 store。
 * 如果 UFE.class === "degraded",code 加 BANNER_ 前缀,ToastContainer 会
 * 渲染成顶部 banner 而不是右下角 toast。
 *
 * recovery[] 字段映射成 ToastAction[],button click 时由调用方在
 * 创建 toast 时通过 onClick 注入(CourtroomScene 注入 api.sendAction)。
 */
export function handleUserFacingError(
  payload: UserFacingErrorPayload,
  options?: {
    /** 注入 recovery button onClick,例如 (action) => api.sendAction(...) */
    onRecoveryClick?: (recovery: RecoveryActionWire) => void;
    /** toast id,upsert 用(WS reconnecting 同 id 复用) */
    id?: string;
  },
): string {
  const { level, durationMs } = severityFromUFE(payload.class);

  // degraded 用 BANNER_ 前缀让 Container 识别
  const codeForToast =
    payload.class === "degraded" ? `BANNER_${payload.code}` : payload.code;

  // recovery → ToastAction[]
  const recovery = payload.recovery ?? [];
  const actions: ToastAction[] = recovery.map((r) => ({
    type: r.type,
    label: r.label,
    backendAction: r.action,
    backendPayload: r.payload,
    onClick: options?.onRecoveryClick
      ? () => options.onRecoveryClick!(r)
      : undefined,
  }));

  const item: Omit<ToastItem, "id" | "createdAt"> & { id?: string } = {
    level,
    message: payload.message,
    detail: payload.detail,
    code: codeForToast,
    durationMs,
    actions: actions.length > 0 ? actions : undefined,
  };

  return useToastStore.getState().push({ ...item, id: options?.id });
}

// ============== WS / API 适配器 ==============

/**
 * WebSocket event.type === "error" 时调用。
 * WS payload 已经是 UserFacingError JSON(由后端 BroadcastUserFacingError 投递)。
 */
export function handleWsError(payload: unknown): string | null {
  if (!isUserFacingError(payload)) {
    // 兜底: 后端可能广播了非标准 error 事件(class/code/message 缺失)
    // 用 info toast 提示用户,不静默吞
    const fallback = {
      class: "transient" as const,
      code: "UNKNOWN_WS_ERROR",
      message:
        typeof payload === "object" && payload !== null
          ? JSON.stringify(payload).slice(0, 200)
          : "未知错误",
    };
    return handleUserFacingError(fallback);
  }
  return handleUserFacingError(payload);
}

/**
 * fetchJson 在 !res.ok 时调用。尝试从 res.json() 解析 user_facing_error 字段。
 *
 * 401 特殊处理:清 token + 提示用户重新进入。
 * 5xx 兜底:Toast 提示 + 把原始 status 给到 detail 方便排查。
 */
export async function handleApiError(res: Response): Promise<never> {
  let ufe: UserFacingErrorPayload | null = null;
  let rawMessage: string | null = null;

  try {
    const body = await res.clone().json();
    if (isUserFacingError(body.user_facing_error)) {
      ufe = body.user_facing_error as UserFacingErrorPayload;
    } else if (typeof body.message === "string") {
      rawMessage = body.message;
    } else if (typeof body.error === "string") {
      rawMessage = body.error;
    }
  } catch {
    // res 不是 JSON,直接走兜底
  }

  // 401 特殊:清 token,提示用户
  if (res.status === 401) {
    if (typeof window !== "undefined") {
      localStorage.removeItem("dc_token");
      localStorage.removeItem("dc_token_exp");
    }
    handleUserFacingError({
      class: "user_input",
      code: "AUTH_TOKEN_EXPIRED",
      message: "登录状态已失效,请刷新页面或重新进入",
    });
    throw new ApiError(401, "AUTH_TOKEN_EXPIRED", "登录状态已失效", ufe);
  }

  if (ufe) {
    handleUserFacingError(ufe);
    throw new ApiError(res.status, ufe.code, ufe.message, ufe);
  }

  // 兜底:非结构化错误
  handleUserFacingError({
    class: res.status >= 500 ? "fatal" : "transient",
    code: `HTTP_${res.status}`,
    message: rawMessage ?? `请求失败 (${res.status})`,
    detail: `${res.status} ${res.statusText} ${res.url}`.trim(),
  });
  throw new ApiError(
    res.status,
    `HTTP_${res.status}`,
    rawMessage ?? `请求失败 (${res.status})`,
    null,
  );
}

/**
 * fetchJson 替代 throw new Error(...) 的 typed error。
 * 调用方可以 catch 后用 err.code 区分错误类型。
 */
export class ApiError extends Error {
  status: number;
  code: string;
  ufe: UserFacingErrorPayload | null;

  constructor(
    status: number,
    code: string,
    message: string,
    ufe: UserFacingErrorPayload | null,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.ufe = ufe;
  }
}

// ============== 手动 push helpers ==============
// 业务代码不需要直接构造 UFE,但偶尔需要手动 toast(网络断线、本地校验失败等)。

export function toastInfo(message: string, code = "INFO"): string {
  return useToastStore.getState().push({
    level: "info",
    message,
    code,
    durationMs: 3000,
  });
}

export function toastSuccess(message: string, code = "SUCCESS"): string {
  return useToastStore.getState().push({
    level: "success",
    message,
    code,
    durationMs: 3000,
  });
}

export function toastWarning(message: string, code = "WARNING"): string {
  return useToastStore.getState().push({
    level: "warning",
    message,
    code,
    durationMs: 5000,
  });
}

export function toastFatal(
  message: string,
  options?: { code?: string; actions?: ToastAction[]; id?: string },
): string {
  return useToastStore.getState().push({
    level: "error",
    message,
    code: options?.code,
    durationMs: 0,
    actions: options?.actions,
    id: options?.id,
  });
}
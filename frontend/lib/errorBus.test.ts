// v0.10.17 silent-error-fix PR 2: errorBus 单元测试
//
// 覆盖:
//  - isUserFacingError 类型守卫
//  - severityFromUFE class → level/durationMs 映射
//  - handleUserFacingError: toast push,recovery actions 注入,degraded 加 BANNER_ 前缀
//  - handleWsError: 标准 UFE + 兜底(非标准 payload)
//  - handleApiError: UFE 字段、401 清 token、5xx 兜底、抛 ApiError
//  - toastInfo/Success/Warning/Fatal 快速 helper

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  ApiError,
  handleApiError,
  handleUserFacingError,
  handleWsError,
  isUserFacingError,
  toastFatal,
  toastInfo,
  toastSuccess,
  toastWarning,
} from "./errorBus.ts";
import { severityFromUFE, useToastStore } from "./toastStore.ts";

// ============== helpers ==============

function clearStore() {
  useToastStore.setState({ toasts: [] });
}

// ============== isUserFacingError ==============

test("isUserFacingError: 标准 UFE 通过", () => {
  assert.equal(
    isUserFacingError({
      class: "fatal",
      code: "OPENING_SPEECHES_FAILED",
      message: "开庭陈述失败",
    }),
    true,
  );
});

test("isUserFacingError: 缺 class 拒绝", () => {
  assert.equal(
    isUserFacingError({ code: "X", message: "y" }),
    false,
  );
});

test("isUserFacingError: 缺 code 拒绝", () => {
  assert.equal(
    isUserFacingError({ class: "transient", message: "y" }),
    false,
  );
});

test("isUserFacingError: null/undefined/string 拒绝", () => {
  assert.equal(isUserFacingError(null), false);
  assert.equal(isUserFacingError(undefined), false);
  assert.equal(isUserFacingError("hello"), false);
  assert.equal(isUserFacingError(42), false);
});

// ============== severityFromUFE ==============

test("severityFromUFE: 4 class 映射正确", () => {
  assert.deepEqual(severityFromUFE("user_input"), {
    level: "info",
    durationMs: 3000,
  });
  assert.deepEqual(severityFromUFE("transient"), {
    level: "warning",
    durationMs: 5000,
  });
  assert.deepEqual(severityFromUFE("degraded"), {
    level: "warning",
    durationMs: 0,
  });
  assert.deepEqual(severityFromUFE("fatal"), {
    level: "error",
    durationMs: 0,
  });
});

test("severityFromUFE: 未知 class 用默认 info 4s", () => {
  assert.deepEqual(severityFromUFE("unknown_class"), {
    level: "info",
    durationMs: 4000,
  });
});

// ============== handleUserFacingError ==============

test("handleUserFacingError: fatal 翻译成 error toast + 不自动消失", () => {
  clearStore();
  const id = handleUserFacingError({
    class: "fatal",
    code: "OPENING_SPEECHES_FAILED",
    message: "开庭失败",
    recovery: [
      { type: "restart_opening", label: "重试", action: "restart_opening" },
      { type: "skip_opening", label: "跳过", action: "force_skip_opening" },
    ],
  });

  const t = useToastStore.getState().toasts.find((x) => x.id === id);
  assert.ok(t);
  assert.equal(t.level, "error");
  assert.equal(t.code, "OPENING_SPEECHES_FAILED");
  assert.equal(t.durationMs, 0); // fatal 不自动消失
  assert.equal(t.message, "开庭失败");
  assert.equal(t.actions?.length, 2);
  assert.equal(t.actions?.[0].type, "restart_opening");
  assert.equal(t.actions?.[0].backendAction, "restart_opening");
});

test("handleUserFacingError: degraded 加 BANNER_ 前缀", () => {
  clearStore();
  handleUserFacingError({
    class: "degraded",
    code: "BREAKER_DEGRADED",
    message: "搜索服务降级",
  });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "BANNER_BREAKER_DEGRADED");
  assert.equal(t.durationMs, 0); // 持续显示
});

test("handleUserFacingError: user_input 3s 自动消失", () => {
  clearStore();
  handleUserFacingError({
    class: "user_input",
    code: "ACTION_STATE_REJECTED",
    message: "当前阶段不允许",
  });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.level, "info");
  assert.equal(t.durationMs, 3000);
});

test("handleUserFacingError: onRecoveryClick 注入", () => {
  clearStore();
  let called = 0;
  let calledAction = "";
  const id = handleUserFacingError(
    {
      class: "fatal",
      code: "OPENING_SPEECHES_FAILED",
      message: "x",
      recovery: [
        { type: "restart_opening", label: "重试", action: "restart_opening" },
      ],
    },
    {
      onRecoveryClick: (r) => {
        called++;
        calledAction = r.action ?? "";
      },
    },
  );

  const t = useToastStore.getState().toasts.find((x) => x.id === id);
  assert.ok(t);
  // 调用 onClick
  t.actions![0].onClick!();
  assert.equal(called, 1);
  assert.equal(calledAction, "restart_opening");
});

test("handleUserFacingError: 同 id upsert 复用", () => {
  clearStore();
  const id1 = handleUserFacingError(
    { class: "transient", code: "WS_RECONNECTING", message: "1" },
    { id: "ws-recon" },
  );
  const id2 = handleUserFacingError(
    { class: "transient", code: "WS_RECONNECTING", message: "2" },
    { id: "ws-recon" },
  );
  assert.equal(id1, id2);
  assert.equal(useToastStore.getState().toasts.length, 1);
  assert.equal(useToastStore.getState().toasts[0].message, "2");
});

// ============== handleWsError ==============

test("handleWsError: 标准 UFE 走 handleUserFacingError", () => {
  clearStore();
  handleWsError({
    class: "user_input",
    code: "WS_THROTTLED",
    message: "操作过于频繁",
  });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "WS_THROTTLED");
});

test("handleWsError: 非标准 payload 走兜底 transient toast", () => {
  clearStore();
  handleWsError({ foo: "bar" });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "UNKNOWN_WS_ERROR");
  assert.equal(t.level, "warning"); // transient
});

test("handleWsError: null/string 兜底", () => {
  clearStore();
  handleWsError(null);
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "UNKNOWN_WS_ERROR");
});

// ============== handleApiError ==============

test("handleApiError: 解析 user_facing_error 字段", async () => {
  clearStore();
  const res = new Response(
    JSON.stringify({
      error: "rate_limit_exceeded",
      message: "trial limit exceeded",
      user_facing_error: {
        class: "fatal",
        code: "TRIAL_RATE_LIMITED",
        message: "今日庭审额度已用完",
      },
    }),
    { status: 429, headers: { "Content-Type": "application/json" } },
  );
  await assert.rejects(handleApiError(res), (e: unknown) => {
    assert.ok(e instanceof ApiError);
    assert.equal((e as ApiError).status, 429);
    assert.equal((e as ApiError).code, "TRIAL_RATE_LIMITED");
    return true;
  });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "TRIAL_RATE_LIMITED");
  assert.equal(t.level, "error");
});

test("handleApiError: 401 清 token + 抛 ApiError", async () => {
  clearStore();
  // 模拟 localStorage + window(Node 跑测试时 window 不存在,需要 mock)
  const storage: Record<string, string> = {
    dc_token: "old-token",
    dc_token_exp: "123",
  };
  globalThis.localStorage = {
    getItem(k: string) {
      return storage[k] ?? null;
    },
    setItem(k: string, v: string) {
      storage[k] = v;
    },
    removeItem(k: string) {
      delete storage[k];
    },
    clear() {
      for (const k of Object.keys(storage)) delete storage[k];
    },
    key: () => null,
    length: 0,
  };
  // @ts-expect-error Node 测试环境无 window
  globalThis.window = {};

  const res = new Response(JSON.stringify({ error: "unauthorized" }), {
    status: 401,
    headers: { "Content-Type": "application/json" },
  });
  await assert.rejects(handleApiError(res), (e: unknown) => {
    assert.ok(e instanceof ApiError);
    assert.equal((e as ApiError).code, "AUTH_TOKEN_EXPIRED");
    return true;
  });
  assert.equal(globalThis.localStorage.getItem("dc_token"), null);
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "AUTH_TOKEN_EXPIRED");
});

test("handleApiError: 5xx 兜底 fatal toast + 抛 ApiError", async () => {
  clearStore();
  const res = new Response("internal error", { status: 500 });
  await assert.rejects(handleApiError(res), (e: unknown) => {
    assert.ok(e instanceof ApiError);
    assert.equal((e as ApiError).code, "HTTP_500");
    return true;
  });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "HTTP_500");
  assert.equal(t.level, "error");
});

test("handleApiError: 4xx 兜底 transient toast", async () => {
  clearStore();
  const res = new Response("bad request", { status: 400 });
  await assert.rejects(handleApiError(res), (e: unknown) => {
    assert.ok(e instanceof ApiError);
    return true;
  });
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.code, "HTTP_400");
  assert.equal(t.level, "warning");
});

// ============== toast helpers ==============

test("toastInfo: 3s 自动消失", () => {
  clearStore();
  toastInfo("hi");
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.level, "info");
  assert.equal(t.durationMs, 3000);
});

test("toastSuccess: success level", () => {
  clearStore();
  toastSuccess("ok");
  assert.equal(useToastStore.getState().toasts[0].level, "success");
});

test("toastWarning: warning level 5s", () => {
  clearStore();
  toastWarning("warn");
  assert.equal(useToastStore.getState().toasts[0].level, "warning");
  assert.equal(useToastStore.getState().toasts[0].durationMs, 5000);
});

test("toastFatal: error level 不自动消失 + 支持 id 复用", () => {
  clearStore();
  toastFatal("oh no", { code: "MY_FATAL", id: "fix-1" });
  toastFatal("oh no 2", { code: "MY_FATAL", id: "fix-1" }); // upsert
  assert.equal(useToastStore.getState().toasts.length, 1);
  const t = useToastStore.getState().toasts[0];
  assert.equal(t.level, "error");
  assert.equal(t.durationMs, 0);
});
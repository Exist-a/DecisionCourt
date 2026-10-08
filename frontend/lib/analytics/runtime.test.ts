// v0.10 前端埋点 (ADR 0020):runtime 单例 TDD 测试
//
// 跑测试命令：
//   node --experimental-strip-types --test frontend/lib/analytics/runtime.test.ts

import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  initAnalytics,
  getAnalytics,
  getTransport,
  flushNow,
  registerUnloadFlush,
  _resetForTesting,
  _currentSessionUUIDForTesting,
} from "./runtime.ts";

beforeEach(() => {
  _resetForTesting();
});

// ============== 单例行为 ==============

test("getAnalytics lazily initializes with empty sessionUUID", () => {
  const a = getAnalytics();

  assert.ok(a, "must return a valid Analytics instance");
  assert.equal(_currentSessionUUIDForTesting(), "");
});

test("initAnalytics(sessionUUID) binds the sessionUUID for subsequent tracks", () => {
  initAnalytics("sess-A");

  assert.equal(_currentSessionUUIDForTesting(), "sess-A");
});

test("initAnalytics can rebind sessionUUID on session switch", () => {
  initAnalytics("sess-A");
  initAnalytics("sess-B");

  assert.equal(_currentSessionUUIDForTesting(), "sess-B",
    "switching sessionUUID must take effect for next track call");
});

test("getAnalytics returns the same instance across calls", () => {
  initAnalytics("sess-X");

  const a1 = getAnalytics();
  const a2 = getAnalytics();

  assert.equal(a1, a2, "must be a singleton");
});

// ============== Transport 暴露 ==============

test("getTransport returns the underlying transport after init", () => {
  initAnalytics("sess-Y");

  const t = getTransport();
  assert.ok(t, "must expose transport for flushNow/manual triggers");
});

test("getTransport returns null before init", () => {
  assert.equal(getTransport(), null);
});

test("flushNow is no-op before init (does not throw)", async () => {
  await assert.doesNotReject(flushNow());
});

// ============== 集成：track 在 mock 模式 / SSR 都不崩 ==============

test("track on freshly-init analytics does not throw under SSR-like state", () => {
  initAnalytics("sess-Z");

  const a = getAnalytics();
  // 不抛错即可：mock 模式 / SSR 检测由 runtime 内部处理
  assert.doesNotThrow(() => {
    a.track("fe.trial_started", { phase: "opening" });
  });
});

// ============== page unload flush (v2.13) ==============
//
// 背景：非关键事件走 5s 批量窗口，此前只靠 setTimeout 发出 —— 用户在窗口内
// 跳转/关页时事件直接丢失。registerUnloadFlush 把 flushNow 挂到页面离开事件上
// 兜住这批事件。

/** 装一个只记 addEventListener 的假 window，返回卸载函数。 */
function stubWindow(): { types: string[]; restore: () => void } {
  const types: string[] = [];
  const previous = (globalThis as { window?: unknown }).window;
  (globalThis as { window?: unknown }).window = {
    addEventListener: (type: string) => {
      types.push(type);
    },
  };
  return {
    types,
    restore: () => {
      if (previous === undefined) {
        delete (globalThis as { window?: unknown }).window;
      } else {
        (globalThis as { window?: unknown }).window = previous;
      }
    },
  };
}

test("registerUnloadFlush hooks pagehide + beforeunload", () => {
  const win = stubWindow();
  try {
    registerUnloadFlush();

    assert.deepEqual(win.types.sort(), ["beforeunload", "pagehide"],
      "must hook both: beforeunload for desktop close/refresh, pagehide for mobile Safari / bfcache");
  } finally {
    win.restore();
  }
});

test("registerUnloadFlush is idempotent (no duplicate listeners per session switch)", () => {
  const win = stubWindow();
  try {
    registerUnloadFlush();
    registerUnloadFlush();
    registerUnloadFlush();

    assert.equal(win.types.length, 2,
      "repeated calls must not stack listeners (initAnalytics runs on every session switch)");
  } finally {
    win.restore();
  }
});

test("initAnalytics registers the unload flush hook", () => {
  const win = stubWindow();
  try {
    initAnalytics("sess-unload");

    assert.ok(win.types.includes("pagehide"),
      "initAnalytics must arm the unload flush so batched events survive navigation");
  } finally {
    win.restore();
  }
});

test("registerUnloadFlush is a no-op without window (SSR / node)", () => {
  // 不装 window：SSR 渲染路径与 node 测试环境都走这里，必须不抛错。
  assert.doesNotThrow(() => registerUnloadFlush());
});

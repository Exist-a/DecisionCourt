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
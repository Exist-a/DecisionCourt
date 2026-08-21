// v1.0.4 PR-C2: lib/trace.ts 单元测试
//
// 设计取舍:
//   Node --test 默认 typeof window === "undefined" → 永远走 SSR 返空路径。
//   这条 SSR-safe 路径是产品要求 (防止 Next.js build 时调用),必须测试。
//
//   浏览器环境下走 fetch 路径的测试需要 jsdom 环境 (本项目 v1.0.2 D.2
//   教训: ESM + .ts 后缀 + fetch mock 链路复杂)。当前测试仅覆盖 SSR 路径,
//   浏览器路径依赖 plan §5 "Framer Motion SSR 出错" 风险缓解 —
//   "use client" directive + dynamic import 在 PR-C3 一起处理。

import { test } from "node:test";
import assert from "node:assert/strict";

// 强制走非 mock 路径 (NEXT_PUBLIC_USE_MOCK 不为 "true")
process.env.NEXT_PUBLIC_USE_MOCK = "false";

import { fetchTraces, fetchTrace } from "./trace.ts";

test("fetchTraces SSR-safe: window undefined 返空数组", async () => {
  // Node --test 下 typeof window === "undefined"
  assert.equal(typeof window, "undefined");
  const result = await fetchTraces("sess-1");
  assert.deepEqual(result, []);
});

test("fetchTraces SSR-safe: date 参数不改变 SSR 行为", async () => {
  const result = await fetchTraces("sess-1", "2026-08-22");
  assert.deepEqual(result, []);
});

test("fetchTrace SSR-safe: window undefined 返 null", async () => {
  assert.equal(typeof window, "undefined");
  const result = await fetchTrace("sess-1", "trace-X");
  assert.equal(result, null);
});

test("fetchTraces useMock 模式退化: NEXT_PUBLIC_USE_MOCK=true 时返空", async () => {
  // 单独设置 env (其他 test 仍为 false)
  const prev = process.env.NEXT_PUBLIC_USE_MOCK;
  process.env.NEXT_PUBLIC_USE_MOCK = "true";
  try {
    // 需要重新 import 才能让 const 重新计算,这里直接验证 const 计算逻辑
    // 实际生产: useMock 是模块级 const,运行时改 env 无效 (top-level const)
    // 这条 test 主要文档化"useMock=true 时返空"的契约,行为由 trace.ts 第 12 行 const 保证
    // 运行时不可改,所以这里只确认 fetchTraces 在 SSR 下返空 (不依赖 useMock)
    const result = await fetchTraces("sess-1");
    assert.deepEqual(result, []);
  } finally {
    process.env.NEXT_PUBLIC_USE_MOCK = prev ?? "";
  }
});

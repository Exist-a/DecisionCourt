﻿// v0.10 前端埋点 (ADR 0020):transport 单元测试
//
// 设计要点（详见 transport.ts 顶部注释）：
//   - 纯函数式工厂：createTransport(config) → { enqueue, flush, size }
//   - 依赖注入 fetcher / scheduleFlush / now,测试用 fake 替代真实 timer/fetch
//   - 关键事件立即 flush,普通事件批量(默认 5s 窗口)
//   - 失败 console.warn 不抛,不阻塞前端
//
// 跑测试命令：
//   node --experimental-strip-types --test frontend/lib/transport.test.ts

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createTransport,
  type FrontendEvent,
  type TransportDeps,
} from "./transport.ts";

// ============== 测试 helper ==============

/**
 * makeDeps 构造测试用的依赖：捕获 fetch 调用、可控的 flush 触发器。
 * - fetcher: 替代 globalThis.fetch,记录每次调用 (url, body, headers)
 * - onWarn: 替代 console.warn,记录警告消息
 * - scheduleFlush: 替代 setTimeout,记录每次调用 + 提供手动 trigger
 */
function makeDeps(overrides: Partial<TransportDeps> = {}) {
  const fetchCalls: Array<{
    url: string;
    body: unknown;
    headers: Record<string, string>;
  }> = [];
  const warns: string[] = [];
  let trigger: (() => void) | null = null;

  const deps: TransportDeps = {
    fetcher: async (url, body, headers) => {
      fetchCalls.push({ url, body, headers });
      return { ok: true, status: 200 };
    },
    onWarn: (msg) => warns.push(msg),
    scheduleFlush: (t) => {
      trigger = t;
    },
    triggerFlush: () => {
      if (trigger) trigger();
    },
    now: () => Date.now(),
    useMock: false,
    ...overrides,
  };

  return { deps, fetchCalls, warns };
}

const sampleEvent: FrontendEvent = {
  session_uuid: "sess-1",
  event_type: "fe.trial_started",
  payload: { foo: "bar" },
  duration_ms: 0,
  status: "ok",
};

// ============== 基础行为 ==============

test("enqueue adds event to queue without firing fetch", () => {
  const { deps, fetchCalls } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080", batchIntervalMs: 5000 }, deps);

  t.enqueue(sampleEvent);

  assert.equal(t.size(), 1, "enqueued event must stay in queue");
  assert.equal(fetchCalls.length, 0, "enqueue alone must not call fetch");
});

test("flush posts queued events as a single fetch call", async () => {
  const { deps, fetchCalls } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080", batchIntervalMs: 5000 }, deps);

  t.enqueue(sampleEvent);

  await t.flush();

  assert.equal(fetchCalls.length, 1);
  assert.equal(fetchCalls[0].url, "http://localhost:8080/api/v1/courtrooms/sess-1/events");
  assert.equal(
    (fetchCalls[0].body as FrontendEvent).event_type,
    "fe.trial_started",
  );
  assert.equal(t.size(), 0, "queue must be empty after successful flush");
});

test("flush with empty queue is no-op", async () => {
  const { deps, fetchCalls } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  await t.flush();

  assert.equal(fetchCalls.length, 0);
});

// ============== 失败降级 ==============

test("flush failure logs warning and does not throw", async () => {
  const { deps, fetchCalls, warns } = makeDeps({
    fetcher: async (url, body, headers) => {
      fetchCalls.push({ url, body, headers });
      return { ok: false, status: 500 };
    },
  });
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  t.enqueue(sampleEvent);

  await assert.doesNotReject(t.flush());

  assert.equal(fetchCalls.length, 1);
  assert.equal(warns.length, 1, "failure must emit exactly one warning");
  assert.match(warns[0], /event flush failed/);
  // 失败时事件不丢：queue 保留,等待下次 flush
  assert.equal(t.size(), 1, "failed events must remain queued for next attempt");
});

test("flush network error (thrown) is caught and logged", async () => {
  const { deps, warns } = makeDeps({
    fetcher: async () => {
      throw new Error("network down");
    },
  });
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  t.enqueue(sampleEvent);

  await assert.doesNotReject(t.flush());
  assert.equal(warns.length, 1);
  assert.match(warns[0], /network down/);
});

// ============== 关键事件 ==============

test("enqueue marks critical events for immediate flush", async () => {
  const { deps, fetchCalls } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  t.enqueue({ ...sampleEvent, event_type: "fe.verdict_feedback" });

  // 关键事件 enqueue 后立即触发 fetch(同步入队 + 异步 flush)
  // 等一个 microtask 让 flush 跑完
  await new Promise((r) => setImmediate(r));
  assert.ok(fetchCalls.length >= 1, "critical event must trigger immediate flush");
  assert.equal(t.size(), 0);
});

test("non-critical events stay in queue for batching", () => {
  const { deps } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  t.enqueue({ ...sampleEvent, event_type: "fe.phase_entered" });

  assert.equal(t.size(), 1, "non-critical event must wait for batch window");
});

// ============== 边界场景 ==============

test("event without session_uuid is dropped silently", () => {
  const { deps, fetchCalls } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  t.enqueue({ ...sampleEvent, session_uuid: "" });

  assert.equal(t.size(), 0, "event without session must be dropped");
  assert.equal(fetchCalls.length, 0);
});

test("mock mode disables all network calls", async () => {
  const { deps, fetchCalls } = makeDeps({ useMock: true });
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  t.enqueue(sampleEvent);
  await t.flush();

  assert.equal(fetchCalls.length, 0, "mock mode must never call fetch");
  assert.equal(t.size(), 0, "mock mode drops events without queueing");
});

test("queue caps at maxQueueSize to prevent memory leak", () => {
  const { deps } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080", maxQueueSize: 3 }, deps);

  for (let i = 0; i < 3; i++) {
    t.enqueue({ ...sampleEvent, event_type: `fe.ev_${i}` });
  }
  assert.equal(t.size(), 3);

  // 第 4 个挤掉最早的
  t.enqueue({ ...sampleEvent, event_type: "fe.ev_3" });
  assert.equal(t.size(), 3, "queue must not exceed maxQueueSize");
});

test("oversized payload is rejected before enqueue", () => {
  const { deps } = makeDeps();
  const t = createTransport(
    { baseUrl: "http://localhost:8080", maxPayloadBytes: 100 },
    deps,
  );

  const big = "x".repeat(200);
  t.enqueue({ ...sampleEvent, payload: { blob: big } });

  assert.equal(t.size(), 0, "oversized event must be dropped to protect backend");
});

test("events with non-object payload are normalized to undefined", () => {
  const { deps } = makeDeps();
  const t = createTransport({ baseUrl: "http://localhost:8080" }, deps);

  // payload 不是 plain object(数组/字符串)→ 视为空 payload
  t.enqueue({ ...sampleEvent, payload: "not an object" as unknown as Record<string, unknown> });

  assert.equal(t.size(), 1, "non-object payload is allowed but normalized at flush time");
});

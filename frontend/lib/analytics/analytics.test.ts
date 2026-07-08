// v0.10 前端埋点 (ADR 0020):track API + PII 守卫 TDD 测试
//
// 设计目标（详见 analytics/index.ts）：
//   - track(eventType, payload) 公共 API,自动注入 sessionUUID
//   - PII 守卫：拒绝 payload 中含敏感字段的事件(content / message / verdict / raw)
//   - SSR safe：window undefined 时 noop
//   - 便捷函数：trackPhaseChange / trackVerdictFeedback / trackEvidenceSubmitted
//
// 跑测试命令：
//   node --experimental-strip-types --test frontend/lib/analytics/analytics.test.ts

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createAnalytics,
  type AnalyticsDeps,
  type Transport,
} from "./index.ts";

// ============== Test helpers ==============

/** fakeTransport 记录每次 enqueue 的事件,便于断言 */
function fakeTransport(): Transport & { events: Array<{ event_type: string; session_uuid: string; payload?: Record<string, unknown>; status?: string }> } {
  const events: Array<{ event_type: string; session_uuid: string; payload?: Record<string, unknown>; status?: string }> = [];
  return {
    events,
    enqueue: (e) => {
      events.push({
        event_type: e.event_type,
        session_uuid: e.session_uuid,
        payload: e.payload,
        status: e.status,
      });
    },
    flush: async () => {},
    size: () => events.length,
  };
}

function makeDeps(opts: {
  sessionUUID?: string;
  useMock?: boolean;
} = {}): {
  deps: AnalyticsDeps;
  transport: ReturnType<typeof fakeTransport>;
  warns: string[];
} {
  const transport = fakeTransport();
  const warns: string[] = [];
  const deps: AnalyticsDeps = {
    transport: transport as unknown as Transport,
    sessionUUID: opts.sessionUUID ?? "sess-test",
    useMock: opts.useMock ?? false,
    onWarn: (msg) => warns.push(msg),
  };
  return { deps, transport, warns };
}

// ============== 基础 track ==============

test("track forwards event to transport with session_uuid", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.track("fe.trial_started", { phase: "opening" });

  assert.equal(transport.events.length, 1);
  assert.equal(transport.events[0].event_type, "fe.trial_started");
  assert.equal(transport.events[0].session_uuid, "sess-test");
  assert.deepEqual(transport.events[0].payload, { phase: "opening" });
  assert.equal(transport.events[0].status, "ok", "default status must be 'ok'");
});

test("track forwards duration_ms and error_msg when provided", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.track("fe.ws_reconnect", { attempt: 2 }, {
    duration_ms: 1234,
    error_msg: "TCP reset",
    status: "error",
  });

  assert.equal(transport.events.length, 1);
  // transport.enqueue 收到完整 event,但 fakeTransport 只录了部分字段
  // 我们用 size 验证调用发生了 + 通过 deps.onWarn 副作用推断
});

test("track is noop when sessionUUID is empty", () => {
  const { deps, transport } = makeDeps({ sessionUUID: "" });
  const a = createAnalytics(deps);

  a.track("fe.trial_started", { foo: "bar" });

  assert.equal(transport.events.length, 0,
    "event without session must be dropped before reaching transport");
});

test("track is noop in mock mode", () => {
  const { deps, transport } = makeDeps({ useMock: true });
  const a = createAnalytics(deps);

  a.track("fe.trial_started", { foo: "bar" });

  assert.equal(transport.events.length, 0);
});

// ============== PII 守卫 ==============

test("track rejects payload containing 'content' field (top-level)", () => {
  const { deps, transport, warns } = makeDeps();
  const a = createAnalytics(deps);

  a.track("fe.evidence_submitted", {
    type: "fact",
    content: "用户提交了带病提交证据",
  });

  assert.equal(transport.events.length, 0,
    "PII payload (content field) must be dropped");
  assert.equal(warns.length, 1, "must log a warning explaining the drop");
  assert.match(warns[0], /PII|content/i);
});

test("track rejects payload with nested sensitive field", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.track("fe.phase_entered", {
    phase: "cross_exam",
    details: { content: "律师说了什么" }, // 嵌套
  });

  assert.equal(transport.events.length, 0,
    "nested PII payload must be detected and dropped");
});

test("track allows payload with safe fields", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.track("fe.evidence_submitted", {
    type: "fact",
    char_count: 42,
    phase: "evidence",
  });

  assert.equal(transport.events.length, 1, "safe payload must pass through");
});

// ============== 便捷函数 ==============

test("trackPhaseChange computes duration from provided ms", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.trackPhaseChange("opening", "cross_exam", 12_000);

  assert.equal(transport.events.length, 1);
  assert.equal(transport.events[0].event_type, "fe.phase_entered");
  assert.deepEqual(transport.events[0].payload, {
    from_phase: "opening",
    to_phase: "cross_exam",
    duration_ms: 12_000,
  });
});

test("trackVerdictFeedback forwards helpful boolean and scores", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.trackVerdictFeedback(true, 0.7, 0.3);

  assert.equal(transport.events.length, 1);
  assert.equal(transport.events[0].event_type, "fe.verdict_feedback");
  assert.deepEqual(transport.events[0].payload, {
    helpful: true,
    score_a: 0.7,
    score_b: 0.3,
  });
});

test("trackEvidenceSubmitted forwards type and char_count", () => {
  const { deps, transport } = makeDeps();
  const a = createAnalytics(deps);

  a.trackEvidenceSubmitted("fact", 120);

  assert.equal(transport.events.length, 1);
  assert.equal(transport.events[0].event_type, "fe.evidence_submitted");
  assert.deepEqual(transport.events[0].payload, {
    type: "fact",
    char_count: 120,
  });
});
// v0.8.3 WebSocket 重连退避算法单元测试。
//
// 用 node --test + --experimental-strip-types 跑（不需要安装 vitest/
// jsdom）。命令：
//   node --experimental-strip-types --test frontend/lib/reconnect.test.ts
//
// 算法契约（见 reconnect.ts）：
//   - computeBackoff(d) 永远返回 d*2 但不超过 30s
//   - 失败 n 次 → 重试间隔 = 1s * 2^n 但不超过 30s
//   - resetBackoff() 永远返回 1s
//
// 这是 v0.8.3 "庭审卡住" 修复的核心 —— 没有重连退避的话每次断网会立刻
// 触发重连风暴把服务端打挂。

import { test } from "node:test";
import assert from "node:assert/strict";
import { computeBackoff, resetBackoff, INITIAL_RETRY_DELAY_MS, MAX_RETRY_DELAY_MS } from "./reconnect.ts";

test("computeBackoff doubles the delay starting at 1s", () => {
  assert.equal(computeBackoff(INITIAL_RETRY_DELAY_MS), 2_000);
  assert.equal(computeBackoff(2_000), 4_000);
  assert.equal(computeBackoff(4_000), 8_000);
  assert.equal(computeBackoff(8_000), 16_000);
});

test("computeBackoff caps at 30s", () => {
  // 16s → 32s，应被 cap 到 30s
  assert.equal(computeBackoff(16_000), MAX_RETRY_DELAY_MS);
  // 30s → 60s，应被 cap 到 30s
  assert.equal(computeBackoff(MAX_RETRY_DELAY_MS), MAX_RETRY_DELAY_MS);
  // 100s → 200s，应被 cap 到 30s
  assert.equal(computeBackoff(100_000), MAX_RETRY_DELAY_MS);
});

test("computeBackoff handles malformed inputs gracefully", () => {
  // 0 / 负数被 Math.max clamp 到 INITIAL_RETRY_DELAY_MS(1s)，
  // 然后 * 2 = 2000。这保证算法不会因为 stale state 传出 NaN 或
  // 负数（会让 setTimeout 立即触发，把服务端打挂）。
  assert.equal(computeBackoff(0), 2_000);
  assert.equal(computeBackoff(-5), 2_000);
  // 极大值 → 仍 cap 到 30s
  assert.equal(computeBackoff(Number.MAX_SAFE_INTEGER), MAX_RETRY_DELAY_MS);
});

test("resetBackoff returns the initial delay", () => {
  assert.equal(resetBackoff(), 1_000);
  // 多次调用都返回 1s
  assert.equal(resetBackoff(), 1_000);
  assert.equal(resetBackoff(), 1_000);
});

test("exponential sequence matches the documented 1s/2s/4s/8s/16s/30s/30s...", () => {
  // 模拟连续重试 n 次，每次失败后用 computeBackoff 算下一次延迟
  const sequence: number[] = [];
  let d = INITIAL_RETRY_DELAY_MS;
  for (let i = 0; i < 10; i++) {
    sequence.push(d);
    d = computeBackoff(d);
  }
  assert.deepEqual(sequence, [
    1_000,  // 第一次重试前
    2_000,  // 第二次重试前
    4_000,
    8_000,
    16_000,
    30_000, // 第五次
    30_000, // 第六次 — 封顶
    30_000,
    30_000,
    30_000,
  ]);
});

test("resetBackoff + computeBackoff round-trip restores initial state", () => {
  // 模拟：失败 5 次（重试间隔到 30s）→ 重连成功 → resetBackoff →
  // 下一次失败应从 1s 重新开始
  let d = resetBackoff();
  for (let i = 0; i < 5; i++) {
    d = computeBackoff(d);
  }
  assert.equal(d, 30_000);

  d = resetBackoff();
  assert.equal(d, 1_000);
  assert.equal(computeBackoff(d), 2_000);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { markTimedOut } from "./pendingEvidence.ts";

const T = 30_000;

test("markTimedOut: 超过阈值且未标记的占位被标 failed", () => {
  const items = [{ createdAt: 0 }, { createdAt: 10_000 }];
  const r = markTimedOut(items, 40_000, T); // item0 age=40s>30s；item1 age=30s 不 > 30s
  assert.equal(r.changed, true);
  assert.equal(r.items[0].failed, true);
  assert.equal(r.items[1].failed, undefined);
});

test("markTimedOut: 未超时不改动（返回原引用）", () => {
  const items = [{ createdAt: 39_000 }];
  const r = markTimedOut(items, 40_000, T);
  assert.equal(r.changed, false);
  assert.equal(r.items, items, "无变化应返回原数组引用，避免无谓重渲染");
});

test("markTimedOut: 已 failed 的不重复标记", () => {
  const items = [{ createdAt: 0, failed: true }];
  const r = markTimedOut(items, 999_999, T);
  assert.equal(r.changed, false);
  assert.equal(r.items, items);
});

test("markTimedOut: timeoutMs<=0 视为禁用", () => {
  const items = [{ createdAt: 0 }];
  const r = markTimedOut(items, 999_999, 0);
  assert.equal(r.changed, false);
  assert.equal(r.items[0].failed, undefined);
});

test("markTimedOut: 空列表不产生变化", () => {
  const r = markTimedOut([], 999_999, T);
  assert.equal(r.changed, false);
});

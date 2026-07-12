// v0.10.17 silent-error-fix PR 2: toast store 单元测试
//
// 覆盖:
//  - push: 自动生成 id
//  - push with explicit id: 视为 upsert
//  - dismiss: 单条删除
//  - clear: 全部清空
//  - 多次 push 不同 id: 累加

import { test } from "node:test";
import assert from "node:assert/strict";

import { useToastStore } from "./toastStore.ts";

function clearStore() {
  useToastStore.setState({ toasts: [] });
}

test("push: 自动生成 id + 加入列表", () => {
  clearStore();
  const id = useToastStore.getState().push({
    level: "info",
    message: "hi",
    durationMs: 3000,
  });
  assert.ok(id.startsWith("toast-"));
  const t = useToastStore.getState().toasts;
  assert.equal(t.length, 1);
  assert.equal(t[0].id, id);
  assert.equal(t[0].message, "hi");
});

test("push with explicit id: 视为 upsert, 同 id 替换不累加", () => {
  clearStore();
  const id1 = useToastStore.getState().push(
    { level: "info", message: "1", durationMs: 3000, id: "stable" },
  );
  const id2 = useToastStore.getState().push(
    { level: "info", message: "2", durationMs: 3000, id: "stable" },
  );
  assert.equal(id1, id2);
  assert.equal(id1, "stable");
  assert.equal(useToastStore.getState().toasts.length, 1);
  assert.equal(useToastStore.getState().toasts[0].message, "2");
});

test("push 多次不同 id: 累加", () => {
  clearStore();
  useToastStore.getState().push({
    level: "info",
    message: "a",
    durationMs: 3000,
  });
  useToastStore.getState().push({
    level: "warning",
    message: "b",
    durationMs: 5000,
  });
  useToastStore.getState().push({
    level: "error",
    message: "c",
    durationMs: 0,
  });
  assert.equal(useToastStore.getState().toasts.length, 3);
});

test("dismiss: 删除指定 id", () => {
  clearStore();
  const id = useToastStore.getState().push({
    level: "info",
    message: "x",
    durationMs: 0,
  });
  assert.equal(useToastStore.getState().toasts.length, 1);
  useToastStore.getState().dismiss(id);
  assert.equal(useToastStore.getState().toasts.length, 0);
});

test("dismiss 不存在的 id: noop", () => {
  clearStore();
  useToastStore.getState().dismiss("non-existent");
  assert.equal(useToastStore.getState().toasts.length, 0);
});

test("clear: 清空全部", () => {
  clearStore();
  useToastStore.getState().push({ level: "info", message: "a", durationMs: 0 });
  useToastStore.getState().push({ level: "info", message: "b", durationMs: 0 });
  useToastStore.getState().clear();
  assert.equal(useToastStore.getState().toasts.length, 0);
});

test("upsert 与 push 行为一致", () => {
  clearStore();
  useToastStore.getState().upsert({
    id: "x",
    level: "info",
    message: "first",
    durationMs: 3000,
  });
  useToastStore.getState().upsert({
    id: "x",
    level: "info",
    message: "second",
    durationMs: 3000,
  });
  assert.equal(useToastStore.getState().toasts.length, 1);
  assert.equal(useToastStore.getState().toasts[0].message, "second");
});
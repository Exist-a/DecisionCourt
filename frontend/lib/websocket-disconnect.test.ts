// v1.0-patch (2026-08-22): 回归测试 — 切换 trial 不应触发 "连接已断开" toast。
//
// 根因: websocket.ts disconnect() 显式调 onConnectionStateChange("closed"),
// 而 CourtroomScene.tsx cleanup 里调 socket.disconnect() 触发 closed 回调 →
// toastFatal("连接已断开, 请刷新页面")。每次切换 trial 都闪一次。
//
// 修复: disconnect() 不再调 onConnectionStateChange("closed") (让 onclose 触发),
// onclose 内部 if (this.closedByUser) return; 在调 onConnectionStateChange 之前。

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const wsPath = join(import.meta.dirname, "websocket.ts");
const src = readFileSync(wsPath, "utf8");

test("websocket.ts: disconnect() 不显式调 onConnectionStateChange(\"closed\")", () => {
  // 找 disconnect() 函数体 — 应不含 onConnectionStateChange("closed")
  // 这是关键: 用户主动 disconnect 时不应广播 "closed" 状态,
  // 让 onclose 自然触发 (但 onclose 内部有 closedByUser 检查, 不会真 broadcast)。
  const disconnectMatch = src.match(/disconnect\(\)\s*\{[\s\S]*?\n\s*\}/);
  assert.ok(disconnectMatch, "disconnect() 函数未找到");
  const disconnectBody = disconnectMatch[0];
  assert.ok(
    !disconnectBody.includes('onConnectionStateChange?.("closed")'),
    "disconnect() 不应直接调 onConnectionStateChange(\"closed\") — 由 onclose 触发",
  );
});

test("websocket.ts: onclose 在 closedByUser=true 时不调 onConnectionStateChange", () => {
  // 找 onclose 内部: closedByUser 检查必须 onConnectionStateChange 之前
  // 否则 closed 回调仍会 toast
  const oncloseMatch = src.match(/onclose\s*=\s*\(\)\s*=>\s*\{[\s\S]*?\n\s*\};/);
  assert.ok(oncloseMatch, "onclose handler 未找到");
  const oncloseBody = oncloseMatch[0];
  // 检查 closedByUser 在 onConnectionStateChange 之前
  const closedByUserIdx = oncloseBody.indexOf("closedByUser");
  const stateChangeIdx = oncloseBody.indexOf('onConnectionStateChange?.("closed")');
  assert.ok(closedByUserIdx >= 0, "onclose 应有 closedByUser 检查");
  assert.ok(stateChangeIdx >= 0, "onclose 应调 onConnectionStateChange");
  assert.ok(
    closedByUserIdx < stateChangeIdx,
    "closedByUser 检查必须在 onConnectionStateChange 之前, 否则 closed toast 仍会弹",
  );
});

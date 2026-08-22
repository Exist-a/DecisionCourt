// v1.0-patch (2026-08-22): 回归测试 — 防止 sourceIcons[diff.source] undefined 渲染。
// 根因: BeliefDiffCard line 57 `const SourceIcon = sourceIcons[diff.source]`
// 当 diff.source 是 sourceIcons 表里没列的值时返 undefined, 后续 <SourceIcon />
// 渲染 undefined → React 报 "Element type is invalid"。
// 修复: 防御性 fallback `?? null` + 类型断言。
// Node 24 --experimental-strip-types 不支持 .tsx (v1.0.2 D.2 已知坑),
// 改用 source-grep 模式 (同 lib/silhouettes.test.ts), 不实际渲染 React。

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

function readSrc(): string {
  return readFileSync(
    join(import.meta.dirname, "..", "..", "components", "courtroom", "BeliefDiffCard.tsx"),
    "utf8",
  );
}

test("BeliefDiffCard: SourceIcon 用 ?? null fallback (修复 Element type undefined)", () => {
  const src = readSrc();
  // 关键防御: sourceIcons[diff.source ...] ?? null
  assert.ok(
    src.includes("?? null"),
    "SourceIcon 必须有 ?? null fallback, 否则未知 source 值会渲染 undefined",
  );
  // 必须用类型断言把 string index 转成 keyof typeof sourceIcons
  assert.ok(
    src.includes("as keyof typeof sourceIcons"),
    "SourceIcon 必须用 keyof typeof sourceIcons 类型断言",
  );
});

test("BeliefDiffCard: sourceIcons 表包含 3 个已知 source (evidence / weaken / anchor_pull)", () => {
  const src = readSrc();
  for (const key of ["evidence", "weaken", "anchor_pull"]) {
    assert.ok(
      src.includes(`${key}:`),
      `sourceIcons/sourceLabels 表必须包含 ${key}`,
    );
  }
});

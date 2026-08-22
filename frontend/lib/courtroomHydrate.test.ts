// v1.0-patch (2026-08-23, fix U1/U2): 回归测试 — hydrate memory 段只能
// 走 applyCourtEvent 循环, 不能再次调 setMemoryEntries 二次覆盖。
//
// 根因 (docs/todo/bugfix-log-2026-08-23.md §U1/U2):
//   hydrate 完成后, 旧代码额外调一次
//     setMemoryEntries(memory.map(r => ({ ..., content: r.content ?? "" })))
//   但 r.content 在顶层永远是 undefined, 正确字段在 r.payload.content。
//   setMemoryEntries 在 applyCourtEvent 之后执行, 用空字符串整体覆盖了
//   已正确写入的数据; appendMemoryEntry 按 id 幂等 (store L499) 又阻断了
//   CourtroomScene 的二次补救, 救不回 → 判决书 / 历史庭审策略笔记全空。
//
// 此测试用静态源码扫描断言: memory 段不调 setMemoryEntries(即可)。

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const hydratePath = join(import.meta.dirname, "courtroomHydrate.ts");
const src = readFileSync(hydratePath, "utf8");

/**
 * 过滤掉源码中的所有注释行 (// 或 / * ... * / 块), 返回只剩代码的纯文本。
 * 这样 setMemoryEntries 加括号 这种字样只匹配真正的调用, 不会命中注释里的提及。
 */
function stripComments(s: string): string {
  return s
    // 块注释
    .replace(/\/\*[\s\S]*?\*\//g, "")
    // 行注释
    .replace(/^\s*\/\/.*$/gm, "")
    .replace(/\s+\/\/.*$/gm, "");
}

test("hydrate memory 段只用 applyCourtEvent 循环, 不调 setMemoryEntries", () => {
  const bodyMatch = src.match(
    /export async function hydrateCourtroomStore[\s\S]*?\n\}\s*$/,
  );
  assert.ok(bodyMatch, "hydrateCourtroomStore 函数未找到");
  const codeOnly = stripComments(bodyMatch[0]);

  // 函数体内不应出现 setMemoryEntries( 调用
  // (允许 actions 解构里出现名字, 因为我们保留 setter; 也允许注释里提到)
  const callMatches = codeOnly.match(/setMemoryEntries\s*\(/g) ?? [];
  assert.equal(
    callMatches.length,
    0,
    `hydrate 函数体不应调 setMemoryEntries (U1/U2 修复回归保护); 找到 ${callMatches.length} 次`,
  );
});

test("hydrate memory 段必须包含 applyCourtEvent({ type: 'a2a.message' }) 循环", () => {
  const bodyMatch = src.match(
    /export async function hydrateCourtroomStore[\s\S]*?\n\}\s*$/,
  );
  assert.ok(bodyMatch);
  const codeOnly = stripComments(bodyMatch[0]);
  assert.match(
    codeOnly,
    /applyCourtEvent\(\s*\{\s*type:\s*"a2a\.message"/,
    "hydrate 必须用 applyCourtEvent({ type: 'a2a.message', payload: row }) 写入 memory",
  );
});

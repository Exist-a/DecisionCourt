// v2.1 O-3.5 F2 修复: TrialReplay phaseBelief 按 phase 过滤
//
// 原实现直接返回 beliefDiffs 最后一条, 不区分 phase, 导致 opening /
// evidence / cross_exam / closing tab 都显示同一最终 belief — 误导用户。
//
// 修复: phaseBelief 先按 BeliefDiff.phase 字段过滤, 失败时 fallback 到
// reviewPhase 之前 rounds 的最后一条, 最后全局兜底。
//
// 这里是 source-grep 测试, 与 silhouettes.test.ts 一致 — Node 24 暂不
// 支持 .tsx 渲染, 真正的 React 行为靠 tsc + 浏览器实测保证。

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const ROOT = join(import.meta.dirname, "..", "..");

function readFile(relPath: string): string {
  return readFileSync(join(ROOT, relPath), "utf8");
}

test("F2 修复: phaseBelief 按 reviewPhase 过滤 beliefDiffs", () => {
  const src = readFile("components/trace/TrialReplay.tsx");

  // 修复存在 — 块内应包含按 phase 过滤的代码
  assert.match(
    src,
    /samePhase\s*=\s*beliefDiffs\.filter\(\s*\(\s*d\s*\)\s*=>\s*d\.phase\s*===\s*reviewPhase/,
    "F2: phaseBelief 应按 d.phase === reviewPhase 过滤"
  );

  // 不应再有原 buggy 的"过滤 round >= 0"代码
  assert.ok(
    !/beliefDiffs\.filter\(\s*\(\s*d\s*\)\s*=>\s*d\.round\s*&&\s*d\.round\s*>=\s*0\s*\)/.test(
      src
    ),
    "F2: 旧的 'round >= 0' 无意义过滤必须删除"
  );

  // 必须含三层 fallback 注释/逻辑
  assert.match(src, /全局兜底|fallback/, "F2: 应有兜底逻辑说明");
});

test("F2: phaseBelief 的 useMemo 依赖包含 reviewPhase", () => {
  const src = readFile("components/trace/TrialReplay.tsx");
  // 直接定位 phaseBelief useMemo 块, 用 non-greedy 限制
  // 块起: phaseBelief = useMemo(..., [deps])
  // 块止: 第一个 "]\n  );" (useMemo 闭合)
  const blockMatch = src.match(
    /phaseBelief\s*=\s*useMemo\(\(\)\s*=>\s*\{[\s\S]*?\},\s*\[([^\]]+)\]\s*\);/
  );
  assert.ok(blockMatch, "应能找到 phaseBelief useMemo 块");
  const deps = blockMatch![1];
  assert.match(deps, /beliefDiffs/, "deps 含 beliefDiffs");
  assert.match(deps, /messages/, "deps 含 messages");
  assert.match(deps, /reviewPhase/, "deps 含 reviewPhase (F2 修复关键)");
});

test("F2: 5 色盘按标准顺序 judge → prosecutor → investigator → clerk → defender", () => {
  const src = readFile("components/trace/TrialReplay.tsx");
  // 实际源码里顺序写跨多行 + 带类型注解. 用 split-string 比 regex 更稳.
  assert.ok(
    src.includes('"judge"') &&
      src.includes('"prosecutor"') &&
      src.includes('"investigator"') &&
      src.includes('"clerk"') &&
      src.includes('"defender"'),
    "5 角色类型必须都出现"
  );
  // 顺序断言: judge 出现在 prosecutor 之前 (在 Array<...> 类型注解内)
  const idxJudge = src.indexOf('"judge"');
  const idxProsecutor = src.indexOf('"prosecutor"');
  const idxInvestigator = src.indexOf('"investigator"');
  const idxClerk = src.indexOf('"clerk"');
  const idxDefender = src.indexOf('"defender"');
  assert.ok(
    idxJudge < idxProsecutor && idxProsecutor < idxInvestigator &&
      idxInvestigator < idxClerk && idxClerk < idxDefender,
    "5 色盘顺序必须为 judge → prosecutor → investigator → clerk → defender"
  );
});

test("F2: reviewPhase 没发生过时自动跳到最后一个已发生 phase", () => {
  const src = readFile("components/trace/TrialReplay.tsx");
  assert.match(
    src,
    /occurredPhases\[occurredPhases\.length\s*-\s*1\]/,
    "未发生 phase 应 fallback 到最后一个已发生"
  );
});

// v2.2 fix(trace): TrialReplay 对话流文字加 whitespace-pre-wrap
//
// 原 <p>{msg.content}</p> 直接渲染, 后端 LLM 输出含 \n\n (段落) 会被
// HTML 折叠成空格, 多段内容渲染成单行 — 用户看到的"庭审回访对话流
// 文字渲染问题" 之一。
//
// 修复: <p> 加 whitespace-pre-wrap + break-words, 保留 \n + 自动断行。
test("v2.2 修复: 对话流 <p> 应含 whitespace-pre-wrap", () => {
  const src = readFile("components/trace/TrialReplay.tsx");

  // 验证 1: 修复存在 — phaseMessages 渲染的 <p> 必须含 whitespace-pre-wrap
  // 抓 phaseMessages.map 到 msg.content 的整段 JSX (Badge 中间有 ~150 字符)
  const msgBlock = src.match(/phaseMessages\.map\([\s\S]{0,1500}msg\.content/);
  assert.ok(msgBlock, "应能找到 phaseMessages.map 块");
  assert.match(
    msgBlock![0],
    /whitespace-pre-wrap/,
    "v2.2: 对话流 <p> 必须含 whitespace-pre-wrap, 保留 LLM 输出 \\n\\n 段落"
  );

  // 验证 2: 保留 break-words (防英文长词溢出)
  assert.match(
    msgBlock![0],
    /break-words/,
    "v2.2: 对话流 <p> 必须保留 break-words 防英文长词溢出"
  );
});
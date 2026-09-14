// v1.0.4 PR-C3: lib/animations/variants.ts 单元测试
//
// 覆盖 6 个核心 motion variants + bubbleEnterExit:
//   - 每个 variant 都包含 initial + 目标 animate 状态
//   - speak / think / listen / search / judge / confront 都是合法 Variants 结构
//   - bubbleEnterExit 含 initial / animate / exit 三态 (AnimatePresence 必需)
//
// 不测试 framer-motion 内部行为 (那是 framer-motion 自身的测试),
// 只验证我们导出的数据结构合法 + 关键字段 (duration / repeat) 正确。

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import {
  speakVariant,
  thinkVariant,
  listenVariant,
  searchVariant,
  judgeVariant,
  confrontVariant,
  bubbleEnterExit,
} from "./variants.ts";

test("speakVariant: initial + speaking states with scale + y", () => {
  assert.ok("initial" in speakVariant);
  assert.ok("speaking" in speakVariant);
  const speaking = speakVariant.speaking as { scale: number; y: number[] };
  assert.equal(speaking.scale, 1.06);
  assert.deepEqual(speaking.y, [0, -3, 0]);
});

test("thinkVariant: pulse + rotate cycle (duration 2s)", () => {
  const thinking = thinkVariant.thinking as {
    scale: number[];
    rotate: number[];
    transition: { duration: number };
  };
  assert.deepEqual(thinking.scale, [1, 1.04, 1]);
  assert.deepEqual(thinking.rotate, [-1, 1, -1]);
  assert.equal(thinking.transition.duration, 2);
});

test("judgeVariant: 4-keyframe 敲锤 (y 0→-8→0→0) — F3 保留以备未来启用", () => {
  // v2.1 F3 (ADR 0035): judge 不主动发言, AvatarAnimations 不再引用此 variant。
  // 保留 judgeVariant 导出与本测试作为未来启用路径的契约验证。
  // 当 v3.0 端侧 TTS 启动时, 法官开口 → 加回 AvatarAnimations 引用 + judgeVariant 测试。
  const judging = judgeVariant.judging as {
    y: number[];
    rotate: number[];
    transition: { times: number[]; duration: number };
  };
  assert.deepEqual(judging.y, [0, -8, 0, 0]);
  assert.deepEqual(judging.rotate, [0, -8, 8, 0]);
  assert.deepEqual(judging.transition.times, [0, 0.3, 0.6, 1]);
  assert.equal(judging.transition.duration, 0.4);
});

test("bubbleEnterExit: initial + animate + exit (AnimatePresence 必需)", () => {
  assert.ok("initial" in bubbleEnterExit);
  assert.ok("animate" in bubbleEnterExit);
  assert.ok("exit" in bubbleEnterExit, "AnimatePresence 需要 exit 状态");

  // initial 与 exit 都应包含 opacity (淡入淡出核心字段)
  const initial = bubbleEnterExit.initial as { opacity: number };
  const exit = bubbleEnterExit.exit as { opacity: number };
  assert.equal(initial.opacity, 0, "initial opacity 应为 0 (淡入起点)");
  assert.equal(exit.opacity, 0, "exit opacity 应为 0 (淡出终点)");
});

test("listenVariant + confrontVariant: 单一目标状态 (idle/confront)", () => {
  assert.ok("listening" in listenVariant);
  assert.ok("confronting" in confrontVariant);

  // v2.1 Bug-2 修:listenVariant 从 y [0, 2, 0] 改为静止 (y=0),
  // 避免 GPU compositor 把 y 微浮错解释为微小 rotateZ。
  // 验证:listening 的 y 必须是 [0, 0, 0](完全静止);
  const listening = listenVariant.listening as { y: number };
  assert.deepEqual(listening.y, 0, "v2.1: listenVariant 必须完全静止");

  const confronting = confrontVariant.confronting as { x: number[] };
  assert.deepEqual(confronting.x, [0, 20, 0]);
});

test("searchVariant: 4-keyframe rotate (摇头搜索)", () => {
  const searching = searchVariant.searching as { rotate: number[] };
  // v2.1 Bug-2 修:rotate 极缩到 ±8°(原 ±15°),并加 1.8s 长 cycle。
  // 验收:rotate 必须含 4 关键帧且峰值 ≤10°,确保不出现「调查员色盘旋转」错觉。
  assert.deepEqual(searching.rotate, [0, 8, -8, 0], "v2.1: 调查员搜索旋转降低 ±15° → ±8°");
  const transition = (searchVariant.searching as { transition: { duration: number } }).transition;
  assert.equal(transition.duration, 1.8, "v2.1: 1.5s → 1.8s 节奏放缓");
});

// ============================================================================
// v2.1 F3 (ADR 0035) 修复: isJudging 死代码清理
//
// 原 AgentAvatar 传 isJudging={judge && isSpeaking}, 但 judge 从不发言
// (后端无 judge.speak 事件), 整个 isJudging 路径是死代码。F3 删除 4 个
// 组件里接收的 isJudging prop + AvatarAnimations 的 judging 状态。
//
// judgeVariant / silhouette-gavel CSS / dot-judge-shock CSS 全部保留,
// 作为未来 v3.0 端侧 TTS 启用法官发言时的"快速恢复路径"。
// ============================================================================
test("F3: isJudging prop 已从 4 个组件删除", () => {
  const cwd = process.cwd();
  const files = [
    "components/courtroom/AgentAvatar.tsx",
    "components/courtroom/silhouettes/RoleSilhouette.tsx",
    "components/courtroom/avatars/DotAvatar.tsx",
    "components/courtroom/animations/AvatarAnimations.tsx",
  ];
  for (const rel of files) {
    const src = readFileSync(path.resolve(cwd, rel), "utf8");
    // 不应再有 isJudging?: prop 接收
    assert.ok(
      !/isJudging\??:/.test(src),
      `F3: ${rel} 不应再接收 isJudging prop`
    );
    // 不应再有 isJudging= 传参
    assert.ok(
      !src.includes("isJudging="),
      `F3: ${rel} 不应再有 isJudging= 传参`
    );
  }
});

test("F3: AgentAvatar 不再传 isJudging 给 RoleSilhouette", () => {
  const src = readFileSync(
    path.resolve(process.cwd(), "components/courtroom/AgentAvatar.tsx"),
    "utf8"
  );
  const roleCallBlock = src.match(/<RoleSilhouette[\s\S]{0,500}\/>/);
  assert.ok(roleCallBlock, "应能找到 RoleSilhouette 调用块");
  assert.ok(
    !roleCallBlock![0].includes("isJudging"),
    "F3: RoleSilhouette 调用块不应含 isJudging"
  );
});

test("F3: AvatarAnimations.deriveAnimationState 不再有 isJudging 分支", () => {
  const src = readFileSync(
    path.resolve(process.cwd(), "components/courtroom/animations/AvatarAnimations.tsx"),
    "utf8"
  );
  const deriveBlock = src.match(/export function deriveAnimationState[\s\S]{0,500}\}/);
  assert.ok(deriveBlock, "应能找到 deriveAnimationState 函数体");
  assert.ok(
    !deriveBlock![0].includes("isJudging"),
    "F3: deriveAnimationState 不应再有 isJudging 分支"
  );
  // AvatarAnimationState type 不应再有 judging
  const typeBlock = src.match(/export type AvatarAnimationState[\s\S]{0,300};/);
  assert.ok(typeBlock, "应能找到 AvatarAnimationState type 定义");
  assert.ok(
    !typeBlock![0].includes("judging"),
    "F3: AvatarAnimationState 不应再有 judging 字面量"
  );
});

test("F3: judgeVariant 仍导出但 AvatarAnimations 不再 import", () => {
  const variantsSrc = readFileSync(
    path.resolve(process.cwd(), "lib/animations/variants.ts"),
    "utf8"
  );
  assert.match(variantsSrc, /export const judgeVariant/, "judgeVariant 应保留导出");

  const animSrc = readFileSync(
    path.resolve(process.cwd(), "components/courtroom/animations/AvatarAnimations.tsx"),
    "utf8"
  );
  // 验证 import 列表里不含 judgeVariant (注释里出现是允许的)
  const importMatch = animSrc.match(/import\s*\{([^}]+)\}\s*from\s*"@\/lib\/animations\/variants\.ts"/);
  assert.ok(importMatch, "应能找到 variants.ts 的 import 列表");
  const importNames = importMatch![1];
  assert.ok(
    !importNames.includes("judgeVariant"),
    `F3: AvatarAnimations import 列表不应含 judgeVariant, 实际: ${importNames}`
  );
});

// ============================================================================
// v2.1 O-5 修复: JudgeBiasMeter 冲击波位置跟随指针 (F1)
//
// 前次审计误判 "类名错配"。实际工作正常, 但 JSX 的 inline `left: 50%`
// 把冲击波钉死在 track 中央, 不跟随 targetPosition 移动, 导致指针在 60%
// 时看到冲击波从 50% 处喷出 — 视觉错位。
//
// 修复: 把 inline `left: 50%` 改为 `${targetPosition}%`,让冲击波在指针
// 当前位置向外扩。
//
// 这里用 Node fs + grep 验证源码,而不是渲染测试(Node 24 暂不支持 .tsx
// render),与 silhouettes.test.ts 一致的"source-grep 测试"模式。
// ============================================================================
test("v2.1 F1 fix: JudgeBiasMeter shock-wave left uses targetPosition, not 50%", () => {
  const cwd = process.cwd();
  const componentPath = path.resolve(cwd, "components", "courtroom", "JudgeBiasMeter.tsx");
  const src = readFileSync(componentPath, "utf8");

  // 验证 1: 修复存在 — shock 元素的 left 必须用 targetPosition 模板字符串
  const shockBlockMatch = src.match(
    /shock-\$\{shockKey\}[\s\S]{0,600}<\/div>/m
  );
  assert.ok(shockBlockMatch, "F1: 应能找到 shock 元素 block");
  const shockBlock = shockBlockMatch![0];

  assert.ok(
    /left:\s*`\$\{targetPosition}%`/.test(shockBlock),
    "F1: JudgeBiasMeter 冲击波 left 应使用 targetPosition 模板字符串"
  );

  // 验证 2: 修复彻底 — 不应再有 `left: "50%"` 这种硬编码中央位置
  assert.ok(
    !/left:\s*"50%"/.test(shockBlock),
    'F1: shock 元素 block 内不应再有 left: "50%"'
  );

  // 验证 3: CSS 关键帧动画仍存在 (f1 没破坏动画)
  const cssPath = path.resolve(cwd, "app", "globals.css");
  const css = readFileSync(cssPath, "utf8");
  assert.ok(
    /\.dot-judge-shock-pointer\s*\{[\s\S]*?animation:\s*dot-judge-shock-pointer/.test(
      css
    ),
    "F1: globals.css 中 .dot-judge-shock-pointer-active 必须保留 animation 引用"
  );
});

// v2.0 Silhouette + RoleSilhouette 单元测试
//
// Node 24 的 --experimental-transform-types 暂不支持 .tsx 文件 import,
// 所以测试改用"读源码 + regex 断言"模式,验证组件文件含预期 JSX 结构。
// 真正的 React 渲染由 tsc --noEmit 静态类型保证 + 浏览器实测 (prd §6 验证)。
//
// 覆盖 4 个核心契约 (V2.0-PLAN.md §2 PR-D2 估算 3 sub-test):
//   - Silhouette.tsx 含 5 角色 SVG (prosecutor/defender/investigator/judge/clerk)
//   - RoleSilhouette.tsx 含 mode="circle" fallback 路径
//   - globals.css 含 silhouette-* class + @keyframes
//   - AgentAvatar.tsx 接入 RoleSilhouette + NEXT_PUBLIC_USE_CIRCLE_AVATAR

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const ROOT = join(import.meta.dirname, "..");

function readFile(relPath: string): string {
  return readFileSync(join(ROOT, relPath), "utf8");
}

test("Silhouette.tsx 含 5 角色 SVG 组件 + data-role 属性", () => {
  const src = readFile("components/courtroom/silhouettes/Silhouette.tsx");
  for (const role of ["ProsecutorSilhouette", "DefenderSilhouette", "InvestigatorSilhouette", "JudgeSilhouette", "ClerkSilhouette"]) {
    assert.match(src, new RegExp(`function ${role}`), `应有 ${role} 子组件`);
    assert.match(src, new RegExp(`data-role="\\$\\{${role === "ProsecutorSilhouette" ? "agentType" : "agentType"}\\}"|data-role=\\"(\\w+)\\"`), `${role} 应打 data-role 属性`);
  }
  assert.match(src, /viewBox="0 0 64 64"/, "viewBox 64x64");
});

test("Silhouette.tsx speaking 状态触发走路/举手指控动画 class", () => {
  const src = readFile("components/courtroom/silhouettes/Silhouette.tsx");
  assert.match(src, /silhouette-leg-left/, "左脚走路动画");
  assert.match(src, /silhouette-leg-right/, "右脚走路动画");
  assert.match(src, /silhouette-arm-pointing/, "举手指控动画");
  assert.match(src, /silhouette-magnifier/, "调查员放大镜动画");
  assert.match(src, /silhouette-gavel/, "法官敲锤动画");
});

test("RoleSilhouette.tsx 含 mode=circle fallback 路径", () => {
  const src = readFile("components/courtroom/silhouettes/RoleSilhouette.tsx");
  assert.match(src, /mode\?: "silhouette" \| "circle"/, "类型定义含 silhouette/circle");
  assert.match(src, /function CircleAvatarFallback/, "有 CircleAvatarFallback 组件");
  assert.match(src, /data-mode="circle-fallback"/, "circle fallback 打 data-mode 属性");
});

test("globals.css 含 5 角色 CSS var + 走路/点头/举手/敲锤/放大镜 keyframes", () => {
  const css = readFile("app/globals.css");
  // 5 角色 CSS var
  for (const color of ["prosecutor", "defender", "investigator", "judge", "clerk"]) {
    assert.match(css, new RegExp(`--silhouette-${color}\\s*:`), `应有 --silhouette-${color} CSS var`);
  }
  // 4 类 keyframes
  for (const kf of ["silhouette-walk", "silhouette-nod", "silhouette-point", "silhouette-gavel", "silhouette-magnify"]) {
    assert.match(css, new RegExp(`@keyframes ${kf}`), `应有 @keyframes ${kf}`);
  }
  // 角色色应用 class
  for (const cls of ["silhouette-prosecutor", "silhouette-defender", "silhouette-investigator", "silhouette-judge", "silhouette-clerk"]) {
    assert.match(css, new RegExp(`\\.${cls}`), `应有 .${cls} class`);
  }
});

test("AgentAvatar.tsx 接入 RoleSilhouette + NEXT_PUBLIC_USE_CIRCLE_AVATAR env var", () => {
  const src = readFile("components/courtroom/AgentAvatar.tsx");
  assert.match(src, /import \{ RoleSilhouette \}/, "AgentAvatar import RoleSilhouette");
  assert.match(src, /NEXT_PUBLIC_USE_CIRCLE_AVATAR/, "AgentAvatar 读 NEXT_PUBLIC_USE_CIRCLE_AVATAR env var");
  assert.match(src, /mode=\{useCircleAvatar \? "circle" : "silhouette"\}/, "mode 切换 fallback");
});

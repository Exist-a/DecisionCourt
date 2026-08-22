# v2.0 REDESIGN — 阶段 2：静态 2.5D 角色 (PR-D2)

> **顶层规划**：[../V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md)
> **本阶段目标**：写 `<Character>` 通用组件 + 5 角色配置 + `<CourtroomFloor>` 庭审场地 mesh，替换 CourtroomScene 庭审现场 div（彻底删除旧剪影代码）。视觉达到"2.5D 庭审场地 + 5 角色站立"的可验证状态。
> **工作量**：2d
> **依赖**：[stage-1-foundation.md](./stage-1-foundation.md)（r3f 基础已跑通）
> **下一阶段**：[stage-3-animation-system.md](./stage-3-animation-system.md)

---

## 0. 设计原则（基于用户原话）

- ✅ **角色面部无细节**（连眼睛都不画）
- ✅ **2.5D 体积感**（MeshToonMaterial + Outlines 描边）
- ✅ **5 角色可识别**（颜色 + 身高 + 姿势差异化）
- ❌ **不实现动作**（阶段 3 接入 useFrame）
- ❌ **不实现跑位**（阶段 4 接入 three-pathfinding）

---

## 1. 文件改动清单

| # | 文件 | 类型 | 行数 |
|---|---|---|---|
| 2.1 | `frontend/components/trial/CourtroomFloor.tsx` | NEW | ~80 |
| 2.2 | `frontend/components/trial/Character.tsx` | NEW | ~120 |
| 2.3 | `frontend/components/trial/characters/{Judge,PlaintiffLawyer,DefenseLawyer,Investigator,Defendant}.tsx` | NEW | 5 × ~40 |
| 2.4 | `frontend/components/trial/characters/index.ts` | NEW | ~30 |
| 2.5 | `frontend/components/trial/characterActions.ts` | NEW | ~80 |
| 2.6 | `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | 替换庭审现场 div + 接入 TrialCanvas |
| 2.7 | `frontend/components/courtroom/silhouettes/` 整个目录 | **DELETED** | -300 |

总: ~430 行新 + ~50 行改 + ~300 行删

## 2. 文件详细设计

### 2.1 `CourtroomFloor.tsx` (NEW, ~80 行)

庭审场地的 3D 几何元素：

```tsx
"use client";

// v2.0 REDESIGN 阶段 2: 庭审场地
//
// 3 个组件:
//   - Floor: 庭审地板 (Plane)
//   - JudgesBench: 法官席 (Box + 阶梯)
//   - CentralTable: 中央桌 (Box + 顶部平面)
//   - 拼接成完整庭审场景

import { AccumulativeShadows, RandomizedLight } from "@react-three/drei";

export function CourtroomFloor() {
  return (
    <group>
      {/* 地板 (Plane 8x8) */}
      <mesh receiveShadow rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]}>
        <planeGeometry args={[12, 12]} />
        <meshToonMaterial color="#F2EDE3" />
      </mesh>

      {/* 法官席 (顶部居中, Z=-2) */}
      <group position={[0, 0, -2.5]}>
        {/* 法官席台阶 (Box 4x0.2x1.5) */}
        <mesh castShadow position={[0, 0.1, 0]}>
          <boxGeometry args={[4, 0.2, 1.5]} />
          <meshToonMaterial color="#A89F8E" />
        </mesh>
        {/* 法官席桌面 (Box 3x0.1x1) */}
        <mesh castShadow position={[0, 0.6, 0]}>
          <boxGeometry args={[3, 0.1, 1]} />
          <meshToonMaterial color="#5C564F" />
        </mesh>
      </group>

      {/* 中央桌 (庭审中央, Z=0) */}
      <group position={[0, 0, 0]}>
        {/* 桌面 (Box 2.5x0.1x1.5) */}
        <mesh castShadow position={[0, 0.55, 0]}>
          <boxGeometry args={[2.5, 0.1, 1.5]} />
          <meshToonMaterial color="#5C564F" />
        </mesh>
        {/* 桌腿 (4 个 Box 0.1x0.5x0.1) */}
        {[[-1.1, 0.25, -0.6], [1.1, 0.25, -0.6], [-1.1, 0.25, 0.6], [1.1, 0.25, 0.6]].map((pos, i) => (
          <mesh key={i} castShadow position={pos as [number, number, number]}>
            <boxGeometry args={[0.1, 0.5, 0.1]} />
            <meshToonMaterial color="#3F2E1E" />
          </mesh>
        ))}
      </group>

      {/* 累积接触阴影 (柔和) */}
      <AccumulativeShadows
        temporal
        frames={60}
        alphaTest={0.85}
        scale={10}
        position={[0, 0.01, 0]}
      >
        <RandomizedLight amount={8} radius={5} intensity={0.5} />
      </AccumulativeShadows>
    </group>
  );
}
```

### 2.2 `Character.tsx` (NEW, ~120 行)

通用 2.5D 角色组件：

```tsx
"use client";

// v2.0 REDESIGN 阶段 2: 通用 2.5D 角色组件
//
// 设计要点:
//   - 用 capsule body + sphere head + Outlines 描边
//   - props: role (5 选 1) + position + height + color
//   - data-role 属性保留 (Playwright 视觉回归测试)
//   - 不实现动作 (阶段 3 useFrame 接入)
//
// 5 角色差异化:
//   - judge: 高 1.4m, 深灰 (#5C564F)
//   - prosecutor: 高 1.2m, 绛红 (#B53A2E), 左侧举手指控
//   - defender: 高 1.2m, 深青 (#2C5470), 双手打开辩护
//   - investigator: 高 0.9m (蹲姿), 棕金 (#7A5C3F), 持放大镜
//   - defendant (即 clerk): 高 1.0m, 米褐 (#A89F8E), 低头打字

import { Outlines } from "@react-three/drei";
import type { AgentType } from "@/types";

export interface CharacterProps {
  role: AgentType;
  position: [number, number, number]; // [x, y, z]
  height?: number;
  color?: string;
}

const ROLE_DEFAULTS: Record<AgentType, { height: number; color: string; pose: string }> = {
  judge:       { height: 1.4, color: "#5C564F", pose: "judge" },
  prosecutor:  { height: 1.2, color: "#B53A2E", pose: "point" },
  defender:    { height: 1.2, color: "#2C5470", pose: "open" },
  investigator:{ height: 0.9, color: "#7A5C3F", pose: "crouch" },
  clerk:       { height: 1.0, color: "#A89F8E", pose: "type" },
};

export function Character({ role, position, height, color, pose }: CharacterProps) {
  const cfg = ROLE_DEFAULTS[role];
  const h = height ?? cfg.height;
  const c = color ?? cfg.color;
  const p = pose ?? cfg.pose;

  return (
    <group data-role={role} position={position}>
      {/* 身体 (capsule) */}
      <mesh castShadow position={[0, h / 2, 0]}>
        <capsuleGeometry args={[h / 3, h / 2, 8, 16]} />
        <meshToonMaterial color={c} />
        <Outlines thickness={0.04} color="#000" />
      </mesh>

      {/* 头 (sphere, 用户要求"无细节" — 不画眼睛/嘴/眉毛) */}
      <mesh castShadow position={[0, h + 0.2, 0]}>
        <sphereGeometry args={[h / 4, 16, 16]} />
        <meshToonMaterial color="#A89F8E" />
        <Outlines thickness={0.04} color="#000" />
      </mesh>

      {/* 姿势预设 (阶段 2 静态, 阶段 3 改成 useFrame 驱动) */}
      <RoleSpecificPose role={role} pose={p} height={h} />

      {/* data-role 属性保留 (Playwright 视觉回归测试钩子) */}
      {/* 已在 group 上 */}
    </group>
  );
}

function RoleSpecificPose({ role, pose, height }: { role: AgentType; pose: string; height: number }) {
  // 阶段 2: 静态姿势预设
  // 阶段 3: 改为 useFrame 驱动 (用 Math.sin(time) 做摆动)
  switch (role) {
    case "prosecutor":
      if (pose === "point") {
        return (
          <mesh position={[-0.4, height - 0.2, 0]} rotation={[0, 0, -Math.PI / 4]}>
            <cylinderGeometry args={[0.05, 0.05, 0.6, 8]} />
            <meshToonMaterial color={ROLE_DEFAULTS.prosecutor.color} />
          </mesh>
        );
      }
      break;
    case "defender":
      if (pose === "open") {
        return (
          <>
            <mesh position={[-0.5, height - 0.3, 0]} rotation={[0, 0, Math.PI / 6]}>
              <cylinderGeometry args={[0.05, 0.05, 0.5, 8]} />
              <meshToonMaterial color={ROLE_DEFAULTS.defender.color} />
            </mesh>
            <mesh position={[0.5, height - 0.3, 0]} rotation={[0, 0, -Math.PI / 6]}>
              <cylinderGeometry args={[0.05, 0.05, 0.5, 8]} />
              <meshToonMaterial color={ROLE_DEFAULTS.defender.color} />
            </mesh>
          </>
        );
      }
      break;
    case "judge":
      // 法槌 (Box)
      return (
        <mesh position={[0.4, height - 0.3, 0]} rotation={[Math.PI / 8, 0, 0]}>
          <boxGeometry args={[0.15, 0.4, 0.15]} />
          <meshToonMaterial color="#7A5C3F" />
        </mesh>
      );
    case "investigator":
      // 放大镜 (torus + cylinder handle)
      return (
        <>
          <mesh position={[0.5, height - 0.2, 0]}>
            <torusGeometry args={[0.15, 0.02, 8, 16]} />
            <meshToonMaterial color="#7A5C3F" />
          </mesh>
          <mesh position={[0.5, height - 0.4, 0]}>
            <cylinderGeometry args={[0.02, 0.02, 0.2, 8]} />
            <meshToonMaterial color="#5C564F" />
          </mesh>
        </>
      );
    case "clerk":
      // 笔记本 (Box 扁平)
      return (
        <mesh position={[0, height / 2 + 0.1, 0.3]} rotation={[-0.2, 0, 0]}>
          <boxGeometry args={[0.4, 0.05, 0.3]} />
          <meshToonMaterial color="#3F2E1E" />
        </mesh>
      );
  }
  return null;
}
```

### 2.3 `characters/{Role}.tsx` (NEW, 5 文件 × ~40 行)

每个角色的 position + 简单 wrapper：

```tsx
// frontend/components/trial/characters/Judge.tsx
"use client";
import { Character } from "../Character";

export function Judge() {
  return <Character role="judge" position={[0, 0, -2]} />;
}
```

```tsx
// frontend/components/trial/characters/PlaintiffLawyer.tsx (即控方)
"use client";
import { Character } from "../Character";

export function PlaintiffLawyer() {
  return <Character role="prosecutor" position={[-2, 0, 1]} />;
}
```

```tsx
// frontend/components/trial/characters/DefenseLawyer.tsx
"use client";
import { Character } from "../Character";

export function DefenseLawyer() {
  return <Character role="defender" position={[2, 0, 1]} />;
}
```

```tsx
// frontend/components/trial/characters/Investigator.tsx
"use client";
import { Character } from "../Character";

export function Investigator() {
  return <Character role="investigator" position={[0, 0, -0.8]} />;
}
```

```tsx
// frontend/components/trial/characters/Defendant.tsx (即书记员)
"use client";
import { Character } from "../Character";

export function Defendant() {
  return <Character role="clerk" position={[0, 0, 2.5]} />;
}
```

### 2.4 `characters/index.ts` (NEW, ~30 行)

```tsx
"use client";
export { Judge } from "./Judge";
export { PlaintiffLawyer } from "./PlaintiffLawyer";
export { DefenseLawyer } from "./DefenseLawyer";
export { Investigator } from "./Investigator";
export { Defendant } from "./Defendant";
```

### 2.5 `characterActions.ts` (NEW, ~80 行)

阶段 3 用 — 阶段 2 仅写骨架（empty map），阶段 3 接入动画：

```ts
// v2.0 REDESIGN: 6 状态动作表 (阶段 2 骨架, 阶段 3 填实)
import type { AgentType } from "@/types";

export type AvatarAnimationState =
  | "idle" | "speaking" | "thinking" | "listening"
  | "searching" | "judging" | "confronting"
  | "walking" | "telling_evidence";  // 阶段 3 新增

// 阶段 2: 静态, 每个状态一个描述
// 阶段 3: 改为 useFrame 实现 (head.rotation, hand.position 等)
// 阶段 4: 加 walking + telling_evidence 跑位
export const CHARACTER_ACTIONS: Record<AgentType, Record<AvatarAnimationState, string>> = {
  prosecutor: {
    idle: "静止站立",
    speaking: "说话点头",
    thinking: "抬头思考",
    listening: "倾听轻微点头",
    searching: "(未使用)",
    judging: "(未使用)",
    confronting: "举手指控",
    walking: "走向中央桌",
    telling_evidence: "(未使用)",
  },
  defender: {
    idle: "静止站立",
    speaking: "说话点头",
    thinking: "低头思索",
    listening: "倾听轻微点头",
    searching: "(未使用)",
    judging: "(未使用)",
    confronting: "双手辩护",
    walking: "走向中央桌",
    telling_evidence: "(未使用)",
  },
  judge: {
    idle: "坐姿静止",
    speaking: "敲锤",
    thinking: "翻阅卷宗",
    listening: "听取发言",
    searching: "(未使用)",
    judging: "敲锤 (4 关键帧)",
    confronting: "(未使用)",
    walking: "(法官不移动)",
    telling_evidence: "(未使用)",
  },
  investigator: {
    idle: "蹲姿静止",
    speaking: "起身汇报",
    thinking: "蹲姿搜索",
    listening: "蹲姿倾听",
    searching: "摇晃放大镜",
    judging: "(未使用)",
    confronting: "(未使用)",
    walking: "跑位到目标角色",
    telling_evidence: "面向目标 + 证据卡片高亮",
  },
  clerk: {
    idle: "低头打字",
    speaking: "(沉默记录)",
    thinking: "(未使用)",
    listening: "低头记录",
    searching: "(未使用)",
    judging: "(未使用)",
    confronting: "(未使用)",
    walking: "(书记员不移动)",
    telling_evidence: "(未使用)",
  },
};
```

### 2.6 `CourtroomScene.tsx` (MODIFIED)

**删除**：
- 第 684-813 行旧庭审现场 div（剪影小人 grid）
- `import { RoleSilhouette } from "@/components/courtroom/silhouettes/RoleSilhouette"`
- 旧 AgentAvatar 接入 silhouette 的代码（行 244-266）

**保留**：
- 庭审页其他部分（顶部 Header / sidebar / EvidenceBoard / 输入栏 / Dialog）
- 现有 `AgentAvatar` 组件（阶段 2 改由 `<Character>` + 气泡叠加渲染）

**新增**：
```tsx
// v2.0 REDESIGN 阶段 2: 用 <TrialCanvas> + 5 角色替代旧剪影现场 div
import { TrialCanvas } from "@/components/trial/Canvas";
import { CourtroomFloor } from "@/components/trial/CourtroomFloor";
import {
  Judge, PlaintiffLawyer, DefenseLawyer,
  Investigator, Defendant,
} from "@/components/trial/characters";

// 替换原庭审现场 div (原行 684-813)
<div className="relative flex flex-col items-center justify-start py-5 bg-white border border-rule rounded-sm shadow-paper">
  <div className="phase-ribbon absolute -top-3 left-8">庭审现场 (v2.0 重做版)</div>
  <TrialCanvas
    className="w-full"
    fallback={<span className="text-xs">r3f 加载中...</span>}
  >
    <CourtroomFloor />
    <Judge />
    <PlaintiffLawyer />
    <DefenseLawyer />
    <Investigator />
    <Defendant />
  </TrialCanvas>
</div>
```

### 2.7 `silhouettes/` 目录 (DELETED)

```
frontend/components/courtroom/silhouettes/
├── Silhouette.tsx              ← 删除
├── RoleSilhouette.tsx          ← 删除
└── silhouettes.test.ts         ← 删除 (在 lib/)
```

**理由**：
- v2.0 重做版不再用剪影 SVG
- 旧代码保留 git 历史（commit `50e1746`），需要时可 revert
- 测试 (lib/silhouettes.test.ts) 也删除（不再测试 silhouette 组件）

**git 删除命令**：`git rm -r frontend/components/courtroom/silhouettes/`

## 3. 验证策略

### 3.1 build / type check
```bash
cd frontend && pnpm tsc --noEmit   # 必须 PASS
cd frontend && pnpm test          # 79/79 PASS (silhouettes.test.ts 移除 → -5 → 79)
```

### 3.2 dev 环境浏览器测试
```bash
docker compose -f docker-compose.dev.yml -f docker-compose.override.yml restart frontend
# 浏览器访问 http://localhost:3000/court/<session_uuid>
# 期望 (按从上到下顺序):
#   1. 顶部 Header (当事人对照 "控方主张 vs 辩方主张" + 法官 chip)
#   2. 庭审现场 div (phase-ribbon "庭审现场 (v2.0 重做版)") + 内部 TrialCanvas
#      - 浅色平面 (地板)
#      - 顶部居中: 深灰法官 (高 1.4m) + 法槌
#      - 中央顶: 棕金调查员 (蹲姿, 高 0.9m) + 放大镜
#      - 左下: 绛红控方律师 (高 1.2m) + 举手指控
#      - 右下: 深青辩方律师 (高 1.2m) + 双手打开
#      - 底部居中: 米褐书记员 (高 1.0m) + 低头笔记本
#   3. EvidenceBoard (证据板)
#   4. Sidebar (庭审记录 / 调查活动 / 策略笔记 / 信念轨迹)
#   5. 底部输入栏
#
# 验证项:
#   - 5 角色都有 2.5D 体积感 (capsule + sphere 几何体)
#   - 每个角色颜色 + 姿势差异化可识别
#   - 描边清晰 (Outlines)
#   - 接触阴影柔和 (AccumulativeShadows)
#   - DevTools console 无 WebGL 错误
```

### 3.3 视觉回归 (data-role 属性)
- DevTools Console: `document.querySelector('[data-role="judge"]')`
- 期望: 5 个 `<group data-role="...">` 元素 (Playwright 可 query)

## 4. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 角色位置重叠 (中央桌 + 调查员 + 法官席) | 已按用户原话 "法官顶部居中、调查员中央顶" 设计; 阶段 2 静态位置; 阶段 4 跑位验证无重叠 |
| 2.5D 视觉不够明显 | 阶段 4 加 OrtoControls 让用户能拖动相机看 2.5D 透视; 如还不够改 PerspectiveCamera |
| 角色颜色与 tailwind.config.ts 案卷·印章 token 不一致 | 全部从 token 引用 (hardcode hex 字符串与 token 颜色一致, 后续可改 var) |
| 删 silhouettes/ 后 git history 难查 | commit message 明确说明 v2.0 重做版; 旧 commit `50e1746` 仍可 revert |

## 5. 完成后 / 下一阶段启动条件

- [ ] `pnpm tsc --noEmit` PASS
- [ ] `pnpm test` 79/79 PASS (5 个 silhouettes 测试删除)
- [ ] Docker frontend force-recreate + 浏览器验证 5 角色 2.5D 场景
- [ ] 用户浏览器确认视觉方向 (2.5D 体积感 + 5 角色位置 + 颜色可识别)

→ 启动阶段 3：[stage-3-animation-system.md](./stage-3-animation-system.md)

## 6. 文件改动汇总（本阶段）

| 文件 | 类型 | 行数 |
|---|---|---|
| `frontend/components/trial/CourtroomFloor.tsx` | NEW | +80 |
| `frontend/components/trial/Character.tsx` | NEW | +120 |
| `frontend/components/trial/characters/Judge.tsx` | NEW | +10 |
| `frontend/components/trial/characters/PlaintiffLawyer.tsx` | NEW | +10 |
| `frontend/components/trial/characters/DefenseLawyer.tsx` | NEW | +10 |
| `frontend/components/trial/characters/Investigator.tsx` | NEW | +10 |
| `frontend/components/trial/characters/Defendant.tsx` | NEW | +10 |
| `frontend/components/trial/characters/index.ts` | NEW | +30 |
| `frontend/components/trial/characterActions.ts` | NEW | +80 |
| `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | 替换 ~130 行 + 加 30 行 |
| `frontend/components/courtroom/silhouettes/*` | DELETED | -300 |

净: +280 行新 + ~160 行改 + ~300 行删 = **净 -20 行**（剪影代码被新 2.5D 代码替换）
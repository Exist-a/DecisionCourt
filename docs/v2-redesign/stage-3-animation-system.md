# v2.0 REDESIGN — 阶段 3：动作系统 (PR-D3)

> **顶层规划**：[../V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md)
> **本阶段目标**：用 `useFrame` + `Math.sin` 驱动 6 状态动作（法官转头 / 律师点头叉腰 / 调查员蹲姿摇晃）+ 状态机映射 framer-motion variants。角色能"动起来"，但还**不能跑位**（阶段 4）。
> **工作量**：2d
> **依赖**：[stage-2-static-characters.md](./stage-2-static-characters.md)（5 角色 mesh 已就位）
> **下一阶段**：[stage-4-pathfinding-polish.md](./stage-4-pathfinding-polish.md)

---

## 0. 关键设计原则

### 0.1 framer-motion variants 保留为驱动层

**现有 6 状态**（来自 V1.0.4 PR-C3）：
- `idle / speaking / thinking / listening / searching / judging / confronting`

**保留用途**：
- DOM 元素动画（按钮浮入 / tab 切换 / 证据卡片）
- 作为**驱动层**告诉 r3f 当前状态是什么

**r3f 内用 `useFrame` + `useAnimations` 渲染**（不用 framer-motion 的 motion.div 驱动 mesh）：
- `useFrame` 是 r3f 的 RAF hook，每帧调用
- 用 `useRef` 引用 mesh group，通过 ref.current.rotation.x += sin(t*freq)*dt 设置动画
- 与 framer-motion 完全不同的动画系统（设计目标不同）

### 0.2 状态机映射

```
[framer-motion state] → [r3f action] → [useFrame + useRef animation]

状态来源:
  - speaking: agentType + isSpeaking (AgentAvatar props)
  - searching: agentType + isSearching
  - thinking: store.activeThinking[agentType]
  - judging: agentType === "judge" && isSpeaking
  - idle: 兜底

r3f action:
  - 法官转头: head.rotation.y = lerp(发言者位置.x > 0 ? -0.3 : 0.3, 0.05)
  - 律师点头: head.rotation.x = sin(t*4)*0.05
  - 律师叉腰: hand.position.y += sin(t*2)*0.05
  - 调查员蹲姿摇晃: body.rotation.z = sin(t*3)*0.03
  - 调查员放大镜: magnifyingGlass.rotation.z = sin(t*4)*0.5
  - 法官敲锤: gavel.rotation = 4-keyframe animation (0.4s loop)
```

## 1. 文件改动清单

| # | 文件 | 类型 | 行数 |
|---|---|---|---|
| 3.1 | `frontend/components/trial/animationController.tsx` | NEW | ~150 |
| 3.2 | `frontend/components/trial/characterStateMachine.ts` | NEW | ~80 |
| 3.3 | `frontend/components/trial/Character.tsx` | MODIFIED | +30 行 (接 useFrame) |
| 3.4 | `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | 把 AgentAvatar props (isSpeaking/isSearching/showThinking) 透传到 TrialCanvas 内部 |
| 3.5 | `frontend/lib/animations/variants.ts` | MODIFIED | 加 `walking` + `telling_evidence` 2 个新 variant |
| 3.6 | `frontend/components/courtroom/animations/AvatarAnimations.tsx` | MODIFIED | 加 2 个新状态 |

总: ~230 行新 + ~60 行改

## 2. 文件详细设计

### 2.1 `animationController.tsx` (NEW, ~150 行)

r3f 内的 useFrame 动作驱动：

```tsx
"use client";

// v2.0 REDESIGN 阶段 3: 6 状态动作控制器
//
// 设计要点:
//   - 每个角色 1 个 <AnimatedCharacter> 包裹 mesh group
//   - useFrame 内根据当前 state 设置 head/hand/body rotation/position
//   - framer-motion variants 不直接用于 mesh (r3f 用 useFrame)
//   - 数据来源: characterStateMachine 算出的当前 state

import { useRef } from "react";
import { useFrame } from "@react-three/fiber";
import { Character, type CharacterProps } from "./Character";
import { useCharacterState } from "@/store/characterStore";
import type { AgentType } from "@/types";

export interface AnimatedCharacterProps extends CharacterProps {
  role: AgentType;
  sessionUUID: string;
}

export function AnimatedCharacter({ role, sessionUUID, position }: AnimatedCharacterProps) {
  const groupRef = useRef<THREE.Group>(null);
  const headRef = useRef<THREE.Mesh>(null);
  const handLeftRef = useRef<THREE.Mesh>(null);
  const handRightRef = useRef<THREE.Mesh>(null);
  const gavelRef = useRef<THREE.Mesh>(null);

  const currentState = useCharacterState(sessionUUID, role);

  useFrame((state, delta) => {
    if (!groupRef.current) return;
    const t = state.clock.elapsedTime;

    // 法官转头 (head.rotation.y 跟随发言者位置)
    if (role === "judge" && headRef.current) {
      // ... lerp logic (阶段 3 详细实现)
    }

    // 律师点头 (head.rotation.x 摆动)
    if ((role === "prosecutor" || role === "defender") && headRef.current) {
      if (currentState === "speaking") {
        headRef.current.rotation.x = Math.sin(t * 4) * 0.08;
      } else if (currentState === "thinking") {
        headRef.current.rotation.x = -0.2 + Math.sin(t * 2) * 0.05;
      }
    }

    // 律师叉腰 (hand.position 摆动)
    if (handLeftRef.current && handRightRef.current) {
      if (currentState === "confronting") {
        handLeftRef.current.position.y = Math.sin(t * 2) * 0.05;
      }
    }

    // 调查员蹲姿摇晃
    if (role === "investigator" && groupRef.current) {
      if (currentState === "searching") {
        groupRef.current.rotation.z = Math.sin(t * 3) * 0.03;
      }
    }

    // 法官敲锤 (4 关键帧 0.4s loop)
    if (role === "judge" && gavelRef.current) {
      if (currentState === "judging") {
        const phase = (t * 2.5) % 1;
        if (phase < 0.3) {
          gavelRef.current.rotation.z = -0.3 * (phase / 0.3);
        } else if (phase < 0.6) {
          gavelRef.current.rotation.z = 0.3 * ((phase - 0.3) / 0.3);
        } else {
          gavelRef.current.rotation.z = 0;
        }
      }
    }
  });

  return (
    <group ref={groupRef} position={position} data-role={role}>
      {/* 身体 (Character 内部) */}
      <Character
        role={role}
        position={[0, 0, 0]}
        refs={{ headRef, handLeftRef, handRightRef, gavelRef }}
      />
    </group>
  );
}
```

### 2.2 `characterStateMachine.ts` (NEW, ~80 行)

从 framer-motion variants 派生 r3f 当前状态：

```ts
// v2.0 REDESIGN 阶段 3: 角色状态机映射
//
// 现有 AvatarAnimations.tsx 的 deriveAnimationState() 输出 framer-motion state,
// 这里重新设计: 输出 r3f 内部 state (增加 walking / telling_evidence)

import type { AgentType } from "@/types";

export type CharacterState =
  | "idle" | "speaking" | "thinking" | "listening"
  | "searching" | "judging" | "confronting"
  | "walking" | "telling_evidence";

export interface StateContext {
  isSpeaking?: boolean;
  isSearching?: boolean;
  showThinking?: boolean;
  isJudging?: boolean;
  isWalking?: boolean;
  targetRole?: AgentType;  // telling_evidence 目标
}

export function deriveCharacterState(
  role: AgentType,
  ctx: StateContext,
): CharacterState {
  // 阶段 3 优先级
  if (role === "investigator" && ctx.isWalking && ctx.targetRole) {
    return "walking";
  }
  if (role === "investigator" && ctx.isSpeaking && ctx.targetRole) {
    return "telling_evidence";
  }
  if (ctx.isSearching && role === "investigator") return "searching";
  if (ctx.isJudging && role === "judge") return "judging";
  if (ctx.isSpeaking) return "speaking";
  if (ctx.showThinking) return "thinking";
  return "idle";
}
```

### 2.3 `Character.tsx` (MODIFIED, +30 行)

加 refs prop 把 mesh refs 暴露给父组件（用于 useFrame 操作）：

```tsx
// 在 CharacterProps 加 refs
import { type RefObject } from "react";
import * as THREE from "three";

export interface CharacterRefs {
  headRef: RefObject<THREE.Mesh>;
  handLeftRef?: RefObject<THREE.Mesh>;
  handRightRef?: RefObject<THREE.Mesh>;
  gavelRef?: RefObject<THREE.Mesh>;
}

export interface CharacterProps {
  role: AgentType;
  position: [number, number, number];
  height?: number;
  color?: string;
  pose?: string;
  refs?: CharacterRefs;  // 阶段 3 新增
}

// 在 Character 组件内, 给 mesh 加 ref={refs?.headRef}
<mesh castShadow ref={refs?.headRef} position={[0, h + 0.2, 0]}>
  <sphereGeometry args={[h / 4, 16, 16]} />
  <meshToonMaterial color="#A89F8E" />
  <Outlines thickness={0.04} color="#000" />
</mesh>
```

### 2.4 `CourtroomScene.tsx` (MODIFIED)

把 AgentAvatar 的 isSpeaking / isSearching / showThinking 状态传到 TrialCanvas 内部：

```tsx
// 替换原 5 个 <Judge /> 等为 <AnimatedCharacter>
import { AnimatedCharacter } from "@/components/trial/animationController";

<TrialCanvas ...>
  <CourtroomFloor />
  <AnimatedCharacter role="judge" sessionUUID={sessionId} position={[0, 0, -2]} />
  <AnimatedCharacter role="prosecutor" sessionUUID={sessionId} position={[-2, 0, 1]} />
  <AnimatedCharacter role="defender" sessionUUID={sessionId} position={[2, 0, 1]} />
  <AnimatedCharacter role="investigator" sessionUUID={sessionId} position={[0, 0, -0.8]} />
  <AnimatedCharacter role="clerk" sessionUUID={sessionId} position={[0, 0, 2.5]} />
</TrialCanvas>
```

**状态共享**：
- 当前 AgentAvatar props (isSpeaking/isSearching/showThinking) 来自 store
- 改用 Zustand `useCharacterState(sessionUUID, role)` 直接从 store 读（避免 prop drilling）
- 新增 `frontend/store/characterStore.ts` 派生 r3f 状态

### 2.5 `variants.ts` (MODIFIED)

新增 2 个 framer-motion variants（用于 DOM 元素动画，与 r3f 状态对齐）：

```ts
// 新增: walking variant (DOM 元素用, 不用于 mesh)
export const walkVariant: Variants = {
  initial: { opacity: 1, scale: 1 },
  walking: {
    opacity: 1,
    scale: 1.02,
    transition: { duration: 0.5, repeat: Infinity, repeatType: "reverse", ease: "easeInOut" },
  },
};

// 新增: telling_evidence variant (气泡高亮用)
export const tellingEvidenceVariant: Variants = {
  initial: { boxShadow: "0 0 0 0 rgba(181,58,46,0)" },
  telling_evidence: {
    boxShadow: "0 0 0 8px rgba(181,58,46,0.3)",
    transition: { duration: 0.6, repeat: Infinity, repeatType: "reverse" },
  },
};
```

### 2.6 `AvatarAnimations.tsx` (MODIFIED)

`AvatarAnimationState` 加 2 个状态：

```ts
export type AvatarAnimationState =
  | "idle" | "speaking" | "thinking" | "listening"
  | "searching" | "judging" | "confronting"
  | "walking" | "telling_evidence";  // 新增

// deriveAnimationState() 加 walking / telling_evidence 优先级
```

### 2.7 `frontend/store/characterStore.ts` (NEW, ~50 行)

Zustand 派生 r3f 状态：

```ts
// v2.0 REDESIGN 阶段 3: r3f 角色状态 store
import { create } from "zustand";
import { useCourtroomStore } from "@/store/courtroomStore";
import { deriveCharacterState, type CharacterState } from "@/components/trial/characterStateMachine";
import type { AgentType } from "@/types";

export const useCharacterState = (
  sessionUUID: string,
  role: AgentType,
): CharacterState => {
  return useCourtroomStore((s) => {
    // 从现有 courtroomStore 派生 isSpeaking/isSearching/showThinking
    // 简化版: 阶段 3 实现细节
    const isSpeaking = /* ... */;
    const isSearching = role === "investigator" && /* ... */;
    const showThinking = /* ... */;
    return deriveCharacterState(role, {
      isSpeaking, isSearching, showThinking,
    });
  });
};
```

## 3. 验证策略

### 3.1 build / type check
```bash
cd frontend && pnpm tsc --noEmit   # 必须 PASS
cd frontend && pnpm test          # 79/79 PASS (无新增测试, 6+4 状态映射纯逻辑可在阶段 5 加)
```

### 3.2 dev 环境浏览器测试
```bash
docker compose -f docker-compose.dev.yml -f docker-compose.override.yml restart frontend
# 浏览器访问 http://localhost:3000/court/<session_uuid> + 触发一个 trial
# 期望 (按角色):
#   - 法官说话时: 头缓慢左右摆 (看发言者位置)
#   - 控方说话时: 头点头 (Math.sin 摆动)
#   - 辩方说话时: 头点头 (同控方)
#   - 调查员搜索时: 身体摇晃 (rotate.z)
#   - 法官敲锤时: 法槌 4 关键帧动画
#   - 调查员不移动 (阶段 4 才跑位)
#
# 验证项:
#   - 所有动作可见
#   - 60fps 流畅 (DevTools Performance)
#   - DevTools console 无 r3f 错误
```

### 3.3 视觉回归 (data-role 属性)
- 现有 `data-role` 属性保留, Playwright 视觉回归测试钩子继续可用

## 4. 风险与缓解

| 风险 | 缓解 |
|---|---|
| useFrame 60fps 性能开销 | 5 角色 × useFrame 每帧执行 5 次, 现代浏览器开销 < 1ms; dpr [1,2] 限制 |
| framer-motion variants 与 r3f state 双源状态 | characterStateMachine.ts 单一源; framer-motion variants 仅用于 DOM 元素 (按钮/tab/卡片) |
| mesh ref forwarding + TypeScript 类型 | 用 `THREE.Mesh` 类型; 阶段 3 测试覆盖 Character refs prop |
| 法官转头 "看错方向" | lerp 逻辑需根据发言人 x 坐标 (左 < 0, 右 > 0); 阶段 3 单元测试 |

## 5. 完成后 / 下一阶段启动条件

- [ ] `pnpm tsc --noEmit` PASS
- [ ] `pnpm test` 79/79 PASS
- [ ] Docker frontend force-recreate + 浏览器验证 6 状态动作肉眼可见
- [ ] 用户浏览器确认动作语义 (点头节奏 / 转头速度 / 法官敲锤是否符合预期)

→ 启动阶段 4：[stage-4-pathfinding-polish.md](./stage-4-pathfinding-polish.md)

## 6. 文件改动汇总（本阶段）

| 文件 | 类型 | 行数 |
|---|---|---|
| `frontend/components/trial/animationController.tsx` | NEW | +150 |
| `frontend/components/trial/characterStateMachine.ts` | NEW | +80 |
| `frontend/components/trial/Character.tsx` | MODIFIED | +30 |
| `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | +5 |
| `frontend/store/characterStore.ts` | NEW | +50 |
| `frontend/lib/animations/variants.ts` | MODIFIED | +15 |
| `frontend/components/courtroom/animations/AvatarAnimations.tsx` | MODIFIED | +10 |

总: +340 行
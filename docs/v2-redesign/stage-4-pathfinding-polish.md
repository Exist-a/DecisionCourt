# v2.0 REDESIGN — 阶段 4：跑位 + 视觉打磨 (PR-D4)

> **顶层规划**：[../V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md)
> **本阶段目标**：用 `three-pathfinding` 接入 NavMesh + 调查员跑位（从 A 点跑向 B 点，tell evidence 状态）+ 灯光/阴影/描边视觉微调。
> **工作量**：2d
> **依赖**：[stage-3-animation-system.md](./stage-3-animation-system.md)（6 状态动作已就位）
> **下一阶段**：阶段 5（release notes + ADR + V1-ROADMAP 同步 + tag）

---

## 0. 关键设计原则

### 0.1 跑位 = pathfinding + transform animation

**two-step**：
1. **Pathfinding**：`three-pathfinding` 计算 A → B 最短路径（基于 NavMesh）
2. **Animation**：`useFrame` 沿路径 `position.lerp(path[i], 0.02)` 平滑移动

**为什么不用 NavMesh 自动寻路？**
- 5 角色 + 6 节点 + 几条直线段，手写寻路足够（无需完整 NavMesh）
- three-pathfinding 提供 demo navmesh 工具生成静态网格
- 阶段 4 仅实现"调查员跑到目标" + "返回原位" 2 条固定路径

### 0.2 视觉打磨范围

- ✅ **OrbitControls 替代 OrthographicCamera 固定视角**（用户能拖动看 2.5D 透视）
- ✅ **directional light 改暖色调**（橙黄 #F2A65A，更法庭感）
- ✅ **AccumulativeShadows 调高 frames**（60 → 120 更柔和）
- ✅ **角色边缘描边加粗**（thickness 0.04 → 0.06 更明显）
- ❌ **不加 particle / glow / 后期 bloom**（保持轻量）

## 1. 文件改动清单

| # | 文件 | 类型 | 行数 |
|---|---|---|---|
| 4.1 | `frontend/components/trial/NavMesh.tsx` | NEW | ~80 |
| 4.2 | `frontend/components/trial/PathController.ts` | NEW | ~120 |
| 4.3 | `frontend/components/trial/Character.tsx` | MODIFIED | 调查员 + 目标 role refs 暴露 |
| 4.4 | `frontend/components/trial/CourtroomFloor.tsx` | MODIFIED | 暖色调灯光 + 接触阴影调优 |
| 4.5 | `frontend/components/trial/Canvas.tsx` | MODIFIED | OrbitControls 可拖动视角 |
| 4.6 | `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | 调查员 "Tell evidence" 按钮 + pathfinding 调用 |

总: ~200 行新 + ~50 行改

## 2. 文件详细设计

### 2.1 `NavMesh.tsx` (NEW, ~80 行)

```tsx
"use client";

// v2.0 REDESIGN 阶段 4: NavMesh 导航网格
//
// 5 个关键位置节点 (NavMesh vertices):
//   - judge: [0, 0, -2]
//   - investigator: [0, 0, -0.8]
//   - prosecutor: [-2, 0, 1]
//   - defender: [2, 0, 1]
//   - clerk: [0, 0, 2.5]
//
// 路径段 (NavMesh segments): 5 节点全连接
// (实际只有 4 条路径需要: investigator → prosecutor / defender / judge / clerk)

import { Pathfinding } from "three-pathfinding";
import * as THREE from "three";

export interface NavMeshData {
  vertices: Float32Array;
  segments: Uint32Array;
}

export function createCourtroomNavMesh(): NavMeshData {
  const positions: Record<string, [number, number, number]> = {
    judge: [0, 0, -2],
    investigator: [0, 0, -0.8],
    prosecutor: [-2, 0, 1],
    defender: [2, 0, 1],
    clerk: [0, 0, 2.5],
  };

  const vertices: number[] = [];
  const segments: number[] = [];
  const keys = Object.keys(positions);
  keys.forEach((k) => {
    vertices.push(...positions[k as keyof typeof positions]);
  });
  // 全连接 (5 节点 = C(5,2) = 10 条 segment)
  for (let i = 0; i < keys.length; i++) {
    for (let j = i + 1; j < keys.length; j++) {
      segments.push(i, j);
    }
  }

  return {
    vertices: new Float32Array(vertices),
    segments: new Uint32Array(segments),
  };
}

export function findPath(
  navMeshData: NavMeshData,
  start: [number, number, number],
  end: [number, number, number],
): [number, number, number][] {
  const pathfinding = new Pathfinding();
  const zone = pathfinding.createZone(
    navMeshData.vertices,
    navMeshData.segments,
  );

  const groupID = pathfinding.getGroup(zone, start);
  const path = pathfinding.findPath(start, end, zone, groupID);

  return (path as ArrayLike<number>[]).map((p) => [p[0], p[1], p[2]]);
}
```

### 2.2 `PathController.ts` (NEW, ~120 行)

```ts
// v2.0 REDESIGN 阶段 4: 路径动画控制器
//
// 调查员沿 path 移动 (useFrame + position.lerp)
//
// 设计要点:
//   - 收到 from→to, 调用 findPath 算路径数组
//   - 存当前 path + index + progress 到 Zustand store
//   - 调查员 mesh ref 用 useFrame 沿 path[index]→path[index+1] 移动
//   - 移动完成时设 telling_evidence 状态 (调查员面向目标 + 证据卡片高亮)

import { create } from "zustand";
import { findPath, createCourtroomNavMesh } from "./NavMesh";

export interface PathState {
  activePath: [number, number, number][] | null;
  targetRole: string | null;
  isWalking: boolean;
}

interface PathStore extends PathState {
  startPath: (from: [number, number, number], to: [number, number, number], targetRole: string) => void;
  clearPath: () => void;
}

export const usePathStore = create<PathStore>((set) => ({
  activePath: null,
  targetRole: null,
  isWalking: false,
  startPath: (from, to, targetRole) => {
    const navMesh = createCourtroomNavMesh();
    const path = findPath(navMesh, from, to);
    set({ activePath: path, targetRole, isWalking: true });
  },
  clearPath: () => {
    set({ activePath: null, targetRole: null, isWalking: false });
  },
}));
```

### 2.3 `Character.tsx` (MODIFIED)

调查员 mesh 加 groupRef 暴露：

```tsx
export interface CharacterRefs {
  headRef: RefObject<THREE.Mesh>;
  bodyRef: RefObject<THREE.Group>;  // 阶段 4 新增 (调查员移动用)
  handLeftRef?: RefObject<THREE.Mesh>;
  handRightRef?: RefObject<THREE.Mesh>;
  gavelRef?: RefObject<THREE.Mesh>;
}
```

### 2.4 `CourtroomFloor.tsx` (MODIFIED)

暖色调灯光 + 接触阴影调优：

```tsx
// 把
<directionalLight position={[5, 10, 5]} intensity={1.2} />
// 改为
<directionalLight position={[5, 10, 5]} intensity={1.4} color="#F2A65A" />
<ambientLight intensity={0.6} color="#FFEFD5" />

// 接触阴影 frames 60 → 120
<AccumulativeShadows temporal frames={120} alphaTest={0.85} scale={10}>
```

### 2.5 `Canvas.tsx` (MODIFIED)

加 OrbitControls 让用户能拖动相机看 2.5D：

```tsx
import { OrbitControls } from "@react-three/drei";

// 在 Canvas 内, OrthographicCamera 后加
<OrbitControls
  enablePan={false}
  enableZoom={true}
  minZoom={30}
  maxZoom={100}
  target={[0, 0.5, 0]}
  // 限制旋转角度 (保持 2.5D 等距视角, 不让用户转到完全平面)
  minPolarAngle={Math.PI / 6}     // 30 度
  maxPolarAngle={Math.PI / 3}     // 60 度
/>
```

### 2.6 `CourtroomScene.tsx` (MODIFIED)

调查员 "Tell evidence" 按钮：

```tsx
// 在调查员 panel (或庭审现场旁边) 加按钮
<Button
  size="sm"
  variant="outline"
  onClick={() => {
    // 调用 startPath, 调查员跑到目标角色
    const target = currentSpeakingAgentType;  // 当前发言者
    const from: [number, number, number] = [0, 0, -0.8]; // investigator
    const to = POSITIONS[target];  // prosecutor / defender
    usePathStore.getState().startPath(from, to, target);
  }}
>
  Tell Evidence
</Button>

// 在 AnimatedCharacter investigator 内接 usePathStore
const activePath = usePathStore((s) => s.activePath);
const targetRole = usePathStore((s) => s.targetRole);

useFrame((state, delta) => {
  if (activePath && role === "investigator") {
    // 沿 path 移动
    const t = state.clock.elapsedTime;
    // ... lerp logic (阶段 4 详细实现)
  }
});
```

## 3. 验证策略

### 3.1 build / type check
```bash
cd frontend && pnpm tsc --noEmit   # 必须 PASS
cd frontend && pnpm test          # 79/79 PASS
```

### 3.2 dev 环境浏览器测试
```bash
docker compose -f docker-compose.dev.yml -f docker-compose.override.yml restart frontend
# 浏览器访问 http://localhost:3000/court/<session_uuid> + 触发一个 trial
# 期望:
#   1. 庭审场景比阶段 3 视觉更柔和 (暖色调灯光 + 柔阴影)
#   2. 用户能拖动鼠标看 2.5D 透视 (OrbitControls)
#   3. 调查员 "Tell evidence" 按钮点击后:
#      - 调查员从中央顶跑向发言者 (左下或右下)
#      - 路径动画 1-2 秒
#      - 到达后设 telling_evidence 状态 (调查员面向目标 + 证据卡片高亮)
#      - 5 秒后返回原位
```

### 3.3 视觉回归 (data-role 属性)
- 调查员 mesh 上 `data-role="investigator"` 保留
- 阶段 4 视觉差异 (灯光 + 阴影) 不影响 data-role 测试钩子

## 4. 风险与缓解

| 风险 | 缓解 |
|---|---|
| three-pathfinding 文档少, 集成难 | 用 Pathfinding.findPath + createZone demo; 5 节点全连接, 不依赖 navmesh 工具 |
| OrbitControls 让用户能转到非 2.5D 视角 (违反"颠覆过去 UI" 精神) | minPolarAngle + maxPolarAngle 限制 30-60 度 (基本仍是等距视角) |
| 调查员跑位与现有角色位置重叠 | investigator 默认 [0,0,-0.8] (中央顶), 跑向 [-2,0,1] 或 [2,0,1] 不重叠 |
| 暖色调灯光让角色颜色失真 | 测试 5 角色在新灯光下的辨识度; 阶段 5 release notes 文档化 |
| 视觉打磨过多导致性能下降 | dpr [1,2] + AccumulativeShadows frames 不超过 120 |

## 5. 完成后 / 下一阶段启动条件

- [ ] `pnpm tsc --noEmit` PASS
- [ ] `pnpm test` 79/79 PASS
- [ ] Docker frontend force-recreate + 浏览器验证调查员跑位 + 暖色调灯光
- [ ] 用户浏览器确认视觉打磨 (灯光 / 阴影 / 描边 / OrbitControls)

→ 启动阶段 5（PR-D5）：release notes + ADR 0034 supersede + V1-ROADMAP 同步 + tag

## 6. 文件改动汇总（本阶段）

| 文件 | 类型 | 行数 |
|---|---|---|
| `frontend/components/trial/NavMesh.tsx` | NEW | +80 |
| `frontend/components/trial/PathController.ts` | NEW | +120 |
| `frontend/components/trial/Character.tsx` | MODIFIED | +5 |
| `frontend/components/trial/CourtroomFloor.tsx` | MODIFIED | +5 |
| `frontend/components/trial/Canvas.tsx` | MODIFIED | +10 |
| `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | +30 |

总: +250 行
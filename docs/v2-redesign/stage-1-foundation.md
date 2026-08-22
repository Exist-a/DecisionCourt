# v2.0 REDESIGN — 阶段 1：基础设施 (PR-D1)

> **顶层规划**：[../V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md)
> **本阶段目标**：装 r3f + drei + three-pathfinding 三个包，配置 Next.js 14 集成，新建 `<Canvas>` wrapper（懒加载 + ssr:false），验证 dev 环境能跑通。
> **工作量**：0.5d
> **依赖**：—（起点）
> **下一阶段**：[stage-2-static-characters.md](./stage-2-static-characters.md)

---

## 1. 任务清单

| # | 任务 | 命令 | 文件 |
|---|---|---|---|
| 1.1 | 装 r3f + drei + three + three-pathfinding + @types/three | `pnpm add three @react-three/fiber@^8.17.10 @react-three/drei@^10.7.8 three-pathfinding && pnpm add -D @types/three` | `frontend/package.json` |
| 1.2 | 配 `transpilePackages` | 编辑 `next.config.mjs` 加 `transpilePackages: ['three']` | `frontend/next.config.mjs` |
| 1.3 | 新建 `<Canvas>` wrapper（懒加载 + ssr:false + fallback）| 新文件 `frontend/components/trial/Canvas.tsx` | `frontend/components/trial/Canvas.tsx` |
| 1.4 | 集成到 CourtroomScene 占位（暂不替换庭审现场 div，只挂 1 个空 `<Canvas>` + 调试 overlay）| 修改 `frontend/components/courtroom/CourtroomScene.tsx` 加 import + 占位 div | `frontend/components/courtroom/CourtroomScene.tsx` |
| 1.5 | 验证：dev server 启动，浏览器访问 `/court/[id]`，确认无 hydration 错误 + 空 Canvas 渲染 | 手动浏览器 / curl `/health` | — |

## 2. 文件改动清单

### 2.1 `frontend/package.json` (MODIFIED)

新增 dependencies：
```json
"dependencies": {
  // ... 现有 ...
  "three": "^0.170.0",
  "@react-three/fiber": "^8.17.10",
  "@react-three/drei": "^10.7.8",
  "three-pathfinding": "^1.3.0"
},
"devDependencies": {
  // ... 现有 ...
  "@types/three": "^0.170.0"
}
```

**版本约束**：
- `three.js r170+`（最新稳定，截至 2026-08）
- `@react-three/fiber ^8.17.10`（v9 需 React 19，项目用 React 18 → 用 v8）
- `@react-three/drei ^10.7.8`（2026-08-05 release）
- `three-pathfinding ^1.3.0`（Don McCurdy 维护）

### 2.2 `frontend/next.config.mjs` (MODIFIED)

加 `transpilePackages: ['three']`：

```js
const nextConfig = {
  // ... 现有配置 ...
  transpilePackages: ['three'],  // r3f 必需 (Three.js 用了 ESM 语法)
  webpack: (config) => {
    // ... 现有 webpack 配置 ...
  },
};

export default nextConfig;
```

**为什么需要 transpilePackages**：
- Three.js r170+ 用了一些 Next.js 默认不识别的 ES2022 语法（top-level await + 私有字段）
- 不加会触发 `Module parse failed` 错误
- 来源：r3f 官方安装文档

### 2.3 `frontend/components/trial/Canvas.tsx` (NEW, ~30 行)

```tsx
"use client";

// v2.0 REDESIGN 阶段 1: r3f Canvas wrapper
//
// 设计要点:
//   - 'use client' + dynamic import + ssr:false (r3f Canvas 必须 client only)
//   - 默认 OrthographicCamera 2.5D 等距视角 (position [8,8,8] zoom 50)
//   - 默认 directional light + ambient light (基础照明)
//   - children prop 透传给场景内部组件
//   - fallback prop: Canvas 加载中显示的占位 UI
//
// 下一阶段 (PR-D2) 会加 <CourtroomFloor> <Character> 等场景内容

import { Suspense, type ReactNode } from "react";
import { Canvas } from "@react-three/fiber";
import { OrthographicCamera } from "@react-three/drei";

export interface TrialCanvasProps {
  children?: ReactNode;
  className?: string;
  fallback?: ReactNode;
}

export function TrialCanvas({ children, className, fallback }: TrialCanvasProps) {
  return (
    <div className={`relative w-full h-full ${className ?? ""}`}>
      {fallback && (
        <div className="absolute inset-0 flex items-center justify-center text-stone-500">
          {fallback}
        </div>
      )}
      <Canvas
        // shadows 启用接触阴影 (r3f + drei AccumulativeShadows 配合)
        shadows
        // 背景透明 (let parent 控制庭审页 bg-paper 颜色)
        gl={{ alpha: true, antialias: true }}
        // 性能: dpr 限制 (避免 retina 屏 4x 渲染)
        dpr={[1, 2]}
        // 容错: WebGL 不可用时显示 fallback
        onCreated={({ gl }) => {
          gl.setClearColor(0x000000, 0);
        }}
      >
        {/* 2.5D 等距视角 (固定, 不旋转) */}
        <OrthographicCamera makeDefault position={[8, 8, 8]} zoom={50} />

        {/* 基础照明 (下一阶段会换 AccumulativeShadows) */}
        <ambientLight intensity={0.5} />
        <directionalLight
          position={[5, 10, 5]}
          intensity={1.2}
          castShadow
          shadow-mapSize={[1024, 1024]}
        />

        <Suspense fallback={null}>{children}</Suspense>
      </Canvas>
    </div>
  );
}
```

### 2.4 `frontend/components/courtroom/CourtroomScene.tsx` (MODIFIED, +10 行)

加 import + 占位 div（**不替换**现有庭审现场 div，只是先挂 1 个 `<TrialCanvas>` 占位确认能跑通）：

```tsx
// v2.0 REDESIGN 阶段 1: 引入 r3f Canvas 占位 (下阶段 PR-D2 才接入庭审场景)
import { TrialCanvas } from "@/components/trial/Canvas";

// 在庭审现场 div 之前加 1 个测试占位 (PR-D2 会替换)
// 位置: 在 <div className="flex-1 flex flex-col gap-5 ..."> 内部
//   庭审现场 div 之前
<div className="relative flex flex-col items-center justify-start py-5 bg-white border border-rule rounded-sm shadow-paper">
  <div className="phase-ribbon absolute -top-3 left-8">庭审现场 (v2.0 重做版 - 阶段 1 占位)</div>
  <TrialCanvas
    className="w-full h-64"
    fallback={<span className="text-xs">r3f 加载中...</span>}
  >
    {/* PR-D2 会加 <CourtroomFloor /> <Character /> 等 */}
    <mesh position={[0, 0.5, 0]}>
      <boxGeometry args={[1, 1, 1]} />
      <meshStandardMaterial color="#B53A2E" />
    </mesh>
  </TrialCanvas>
</div>
{/* 保留旧庭审现场 div 不动 (PR-D2 才删) */}
<div className="relative flex flex-col items-center justify-start py-5 ...">
  {/* ... 旧的剪影现场 ... */}
</div>
```

**为什么加占位测试**：
- 阶段 1 只验证 r3f 能跑通（不立即替换旧 UI）
- 用户浏览器看一眼：旧剪影 + 上面有 1 个红色方块 = 成功
- 下一阶段 PR-D2 才把旧庭审现场 div 整个替换

## 3. 验证策略

### 3.1 build / type check
```bash
cd frontend && pnpm tsc --noEmit   # 必须 PASS
cd frontend && pnpm test          # 84/84 PASS（不破坏现有测试）
```

### 3.2 dev 环境浏览器测试
```bash
docker compose -f docker-compose.dev.yml -f docker-compose.override.yml restart frontend
# 浏览器访问 http://localhost:3000/court/<session_uuid>
# 期望:
#   1. 庭审页顶部 (当事人对照行) 下方出现 1 个红色方块 (TrialCanvas 占位)
#   2. 旧剪影庭审现场 div 仍然存在 (向下滚动可见)
#   3. 浏览器 DevTools console 无 "WebGL" / "Module parse failed" 错误
#   4. 浏览器 DevTools Network 无 404 (r3f chunk 正常加载)
```

### 3.3 单元测试 (本阶段不新增)
- 阶段 1 是基础设施, 无新增组件逻辑
- 现有 84 sub-test 全部继续 PASS

## 4. 风险与缓解

| 风险 | 缓解 |
|---|---|
| r3f bundle +200KB 影响 LCP | TrialCanvas 用 `dynamic + ssr:false`; 主页 / 登录页不引入 |
| three.js ESM 语法解析失败 | `next.config.mjs` 加 `transpilePackages: ['three']` (r3f 官方文档) |
| Next.js 14 hydration mismatch | `<Canvas>` 必须 `dynamic` + `ssr:false` (r3f 官方) |
| WebGL 不可用 (老 GPU / 虚拟机) | `onCreated` setClearColor alpha=true + `gl.setClearColor` 让 Canvas 透明 fallback |
| retina 屏 4x 渲染卡顿 | `dpr={[1, 2]}` 限制 dpr 上限 2 |

## 5. 完成后 / 下一阶段启动条件

- [ ] `pnpm tsc --noEmit` PASS
- [ ] `pnpm test` 84/84 PASS
- [ ] Docker frontend force-recreate + 浏览器验证红色方块可见
- [ ] 用户浏览器确认 r3f 跑通 + 视觉方向

→ 启动阶段 2：[stage-2-static-characters.md](./stage-2-static-characters.md)

## 6. 文件改动汇总（本阶段）

| 文件 | 类型 | 行数 |
|---|---|---|
| `frontend/package.json` | MODIFIED | +5 |
| `frontend/next.config.mjs` | MODIFIED | +1 |
| `frontend/components/trial/Canvas.tsx` | NEW | ~50 |
| `frontend/components/courtroom/CourtroomScene.tsx` | MODIFIED | +25 |

总: +80 行（含注释）
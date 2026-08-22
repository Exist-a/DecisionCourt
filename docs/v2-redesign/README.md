# v2.0 REDESIGN — 2.5D 角色动画重构（多阶段文档索引）

> **顶层规划**：[../V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md) — 用户原话 + 技术选型 + 风险 + 时间线
> **触发的反馈**：用户 2026-08-22 "v2.0 简陋，要 2.5D 重做"
> **取代**：[../V2.0-PLAN.md](../V2.0-PLAN.md)（厕所标识剪影小人，已落地但不满足用户）

## 阶段文档（按串行实施顺序）

| 阶段 | 文档 | PR | 工作量 | 状态 |
|---|---|---|---|---|
| **阶段 1** | [stage-1-foundation.md](./stage-1-foundation.md) | PR-D1 | 0.5d | ⏸ 待启动 |
| **阶段 2** | [stage-2-static-characters.md](./stage-2-static-characters.md) | PR-D2 | 2d | ⏸ 依赖阶段 1 |
| **阶段 3** | [stage-3-animation-system.md](./stage-3-animation-system.md) | PR-D3 | 2d | ⏸ 依赖阶段 2 |
| **阶段 4** | [stage-4-pathfinding-polish.md](./stage-4-pathfinding-polish.md) | PR-D4 | 2d | ⏸ 依赖阶段 3 |
| **阶段 5** | （在顶层规划 PR-D5 / release notes 内）| PR-D5 | 0.5d | ⏸ 依赖阶段 4 |

## 各阶段一句话总结

- **阶段 1 (foundation)**：装 r3f + drei + three-pathfinding 四个包，配 `next.config.mjs` 的 `transpilePackages`，新建 `Canvas` wrapper（dynamic + ssr:false）。
- **阶段 2 (static-characters)**：写 `<Character role position>` 通用组件 + 5 角色配置 + 庭审场地 mesh（地板/法官席/中央桌）+ 替换 CourtroomScene 庭审现场 div。废弃 `silhouettes/`。
- **阶段 3 (animation)**：用 `useFrame` + `Math.sin` 驱动 6 状态动作（法官转头/律师点头叉腰/调查员蹲姿摇晃）+ 状态机映射 framer-motion variants。
- **阶段 4 (pathfinding)**：用 `three-pathfinding` 接入 NavMesh + 调查员跑位 + 灯光/阴影/描边视觉微调。
- **阶段 5 (release)**：release notes + ADR 0034 supersede + V1-ROADMAP 同步 + tag。

## 串行依赖图

```
阶段 1 ──→ 阶段 2 ──→ 阶段 3 ──→ 阶段 4 ──→ 阶段 5 (release)
  │           │           │           │
  └─ 文档      └─ 文档      └─ 文档      └─ 文档
     各阶段独立 commit + push + 用户浏览器实测
```

每阶段交付后等用户浏览器实测确认 → 启动下一阶段。

## 用户授权节点（位于顶层规划 §9）

- 阶段 1 完成：验证 r3f 基础能在 Next.js 14 跑通 → 用户浏览器看一眼空 Canvas
- 阶段 2 完成：5 角色 2.5D 庭审场景可见 → 用户确认视觉方向
- 阶段 3 完成：法官转头 + 律师点头叉腰 → 用户确认动作语义
- 阶段 4 完成：调查员跑位 + 视觉打磨 → 用户最终验收
- 阶段 5 完成：文档 + tag → v2.0 重做版正式发布
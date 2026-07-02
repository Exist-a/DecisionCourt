# ADR 0009: 庭审页面可视化简化

> **状态**：✅ Accepted & Implemented (2026-07)  
> **决策日期**：2026-07  
> **影响范围**：`frontend/components/courtroom/ArgumentMap.tsx`、`frontend/components/courtroom/CourtroomScene.tsx`

## 背景

庭审主界面原本 `grid-cols-2` 并排展示两个图表：

1. **观点地图（ArgumentMap）** —— ReactFlow 网络图，含选项节点 + Agent 节点 + 证据节点 + 复杂边
2. **立场变化曲线（StanceChart）** —— Recharts 折线图，绘制信念度随轮次变化

存在两个问题：

1. **认知负担高**：观点地图节点多（最多 17 个）、连线密集，普通用户难以快速理解
2. **页面拥挤**：两个图表并排占用大量垂直空间

## 选项对比

| 维度 | A. 保留两个图表 | B. 删 StanceChart，精简 ArgumentMap | C. 全部删除，换文字描述 |
|---|---|---|---|
| 信息密度 | ✅ 高 | ✅ 中 | ❌ 低 |
| 交互性 | ✅ 高 | ✅ 中 | ❌ 无 |
| 视觉清爽度 | ❌ 拥挤 | ✅ 提升 | ✅ 极简 |
| 用户理解成本 | ❌ 高 | ✅ 低 | ✅ 最低 |

## 决策

采用 **方案 B** —— 保留 ReactFlow 观点地图但精简，删除立场变化曲线组件。

### ArgumentMap 精简

- ❌ 删除证据节点循环（`evidences.forEach` 整段）
- ❌ 删除证据相关的边（`edge-${id}-a`、`edge-${id}-b`）
- ✅ 保留选项 A/B 节点（左右两侧）
- ✅ 保留 Agent 节点（中间一列）
- ✅ 保留 Agent → A/B 边（带信念度百分比）
- ✅ ReactFlow 容器高度从 320px → 280px
- ✅ 移除 Background 网格底纹
- ✅ 保留 Controls（缩放/拖拽）

### StanceChart 处理

- ❌ 在 `CourtroomScene` 中删除 import + 集成（`grid-cols-2` 容器替换为单卡片全宽）
- ✅ **保留 `StanceChart.tsx` 文件**（避免破坏性删除，未来可恢复）
- ✅ 删除 `beliefSnapshots` 解构字段
- ✅ 信念变化曲线信息改由 `BeliefTrajectoryTab`（v0.6 信念审计）提供

### 关键理由

- 证据节点密度高，布局混乱；证据影响通过"边的线宽"已能体现
- 用户明确选择"删除"立场变化曲线（降低认知负担）
- 立场变化曲线信息没丢 —— `BeliefTrajectoryTab` 在 verdict 页提供更详细的信念审计
- ReactFlow 框架保留（比静态柱状图更具交互性）

## 后果

### 收益

- ✅ 庭审页面更清爽，认知负担降低
- ✅ 观点地图节点数从最多 17 减到 7
- ✅ 信念审计信息保留到 `BeliefTrajectoryTab`（verdict 页），信息量更大

### 代价

- ⚠️ `StanceChart.tsx` 文件保留但无引用（dead code，技术债）
- ⚠️ 用户在庭审页看不到信念变化曲线（要等 verdict 页解锁）

## 关联

- 完整设计原文：[`../archive/庭审可视化简化计划.md`](../archive/庭审可视化简化计划.md)
- 信念审计：[`frontend/components/courtroom/BeliefTrajectoryTab.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/BeliefTrajectoryTab.tsx)
- ArgumentMap：[`frontend/components/courtroom/ArgumentMap.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/ArgumentMap.tsx)
- 集成：[`frontend/components/courtroom/CourtroomScene.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/CourtroomScene.tsx)
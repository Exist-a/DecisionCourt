# ADR 0008: 质证轮次控制改为用户触发

> **状态**：✅ Accepted & Implemented (2026-07)  
> **决策日期**：2026-07  
> **影响范围**：`backend/internal/courtroom/service.go`、`frontend/components/courtroom/CourtroomScene.tsx`

## 背景

质证阶段（cross_exam）原本是后端 `for` 循环自动连续跑完所有轮次（直到达到 `max_rounds` 或智能收敛）。问题：

1. **用户无停顿**：每轮 LLM 调用通常 5-15 秒，连续 3-5 轮就是 1-2 分钟用户看不到任何"自己可以介入"的节点
2. **用户来不及补充证据**：用户在第 1 轮结束后想插证据，但 LLM 已经在跑第 2 轮
3. **没有"快慢自如"的节奏感**：庭审变成"等待机器完成"，而不是"用户在主持"

## 选项对比

| 维度 | A. 后端自动连续跑 | B. 每轮结束后广播等待事件，用户点击继续 | C. 每条发言后都暂停（更细粒度）|
|---|---|---|---|
| 用户控制感 | ❌ 弱 | ✅ 中 | ✅ 强 |
| 实现复杂度 | ✅ 低 | ⚠️ 中 | ❌ 高 |
| UX 节奏 | ⚠️ 拖沓感 | ✅ 自然 | ⚠️ 太多按钮 |
| 提交证据后行为 | 自动继续 | 自动继续（不打断） | 自动继续 |

## 决策

采用 **方案 B** —— 每轮质证完成后广播 `round.waiting_for_user` 事件，前端显示"开始第 N+1 轮质证"按钮，用户点击后调用 `continue_cross_exam` action。

### 后端变更

- `runCrossExamRound` 不再是 `for` 循环，每调用一次只执行一轮
- 轮次结束后广播 `round.waiting_for_user` 事件（payload: `current_round` / `next_round` / `max_rounds`）
- `ProcessUserAction` 新增 `continue_cross_exam` action，调用 `runCrossExamRound` 执行下一轮
- `SubmitEvidence` 中移除 `go func` 自动触发下一轮（提交证据是用户主动行为，不自动继续）

### 前端变更

- 监听 `round.waiting_for_user` 事件
- 在 PhaseGuide 下方显示"开始第 N+1 轮质证"按钮
- 按钮点击后调 `sendAction({ action: "continue_cross_exam" })`

### 关键理由

- 用户作为"法官"需要节奏控制权，而不是看机器表演
- 一轮结束后用户通常需要 5-15 秒看发言内容、判断是否插证据
- 提交证据仍然自动继续（不打断是合理 UX）
- 收敛时仍直接进入结案（无需用户确认）

## 后果

### 收益

- ✅ 用户可以控制庭审节奏
- ✅ 用户有充足时间补充证据
- ✅ 庭审体验更接近"主持一场真实辩论"而非"看 AI 表演"
- ✅ 庭审时长可控（用户可中途休息）

### 代价

- ⚠️ 庭审总时长变长（用户思考 + 点击时间）
- ⚠️ 需要前端新增按钮 + 事件监听
- ⚠️ 自动化测试需要模拟 `continue_cross_exam` action

## 关联

- 完整设计原文：[`../archive/质证阶段轮次控制修改计划.md`](../archive/质证阶段轮次控制修改计划.md)
- 代码：[`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go)
- 前端：[`frontend/components/courtroom/CourtroomScene.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/CourtroomScene.tsx)
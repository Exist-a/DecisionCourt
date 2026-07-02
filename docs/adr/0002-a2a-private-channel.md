# ADR 0002: 私有记忆底层迁移到 A2A 私有通道

> **状态**：✅ Accepted & Implemented (2026-06-30)  
> **决策日期**：2026-06-30  
> **影响范围**：`internal/a2a/`、`internal/private_memory/`、`internal/agent/orchestrator.go`

## 背景

v0.4 之前，私有记忆和 A2A 消息是两套独立基础设施：

- **A2A** —— `a2a_messages` 表 + `Bus` + 可见性隔离
- **Private Memory** —— `private_memories` 表 + `Repository` 接口

两表存在三大差距：

1. **私有记忆写入 100%、读取 0%** —— LLM 看不到自己之前的策略笔记（Orchestrator 只 write 不 read）
2. **A2A SanitizedPayload 未生效** —— `Message.SanitizedPayload()` 方法存在但无人调用，控辩方仍能在对方 public 消息中看到 `reasoning`（隐患）
3. **两表基础设施重复** —— 隔离 / 审计 / 路由分散在两套表里，维护成本翻倍

## 选项对比

| 维度 | A. 私有记忆迁移到 A2A private | B. 保留独立表 + 加视图层 | C. 双写 |
|---|---|---|---|
| 隔离 / 审计基础设施 | ✅ 复用 A2A | ❌ 重复实现 | ⚠️ 双倍维护 |
| 前端可视化 | ✅ `a2a.message` 已推送 | ❌ 需新增 WS 事件 | ⚠️ 双源不一致风险 |
| 隔离测试 | ✅ 12 项已覆盖 | ✅ 9 项已覆盖 | ✅ 合并 |
| 迁移成本 | ⚠️ 需写迁移脚本 | ✅ 0 | ⚠️ 持续成本 |
| LLM 能否读到自己的策略笔记 | ✅ 通过 `BuildContextView` | ❌ 仍断链 | ✅（读任一表均可）|

## 决策

采用 **方案 A** —— 把私有记忆底层迁移到 A2A 私有消息通道，所有情节记忆条目以 `visibility=private` 的 A2A 消息存储。

### 关键实现

- 新增 4 个 `MessageType`：`strategy_note` / `opponent_weakness` / `self_correction` / `evidence_eval`
- 新增 `internal/a2a/context_view.go` 的 `BuildContextView()` 投影层
- Orchestrator 调用 `BuildContextView(sessionID, selfAgent)` 在每次 LLM 调用前构造 sanitized context
- 旧 `private_memories` 表保留 1 个版本周期做影子读（**开发期正式切读不做**，决策 2026-07-01）

### 关键理由

- A2A Bus 已经提供完整的隔离 / 审计 / 路由基础设施，重复实现是反 DRY
- 前端 MemoryAuditPanel 直接订阅 `a2a.message` 事件，零新增 WS 事件
- "LLM 看不到对方的私有推理链" 的不变量由 `SanitizedPayload()` 集中保证，比分散在两表更易审计

## 后果

### 收益

- ✅ Orchestrator 调一次 `BuildContextView()` 即可拿到"自己的策略笔记 + 对方 public（剥离 reasoning）"
- ✅ 前端 MemoryAuditPanel 复用 `a2a.message` 事件，无需新增 WS 协议
- ✅ 12 项隔离测试 + 10 项 ContextView 测试覆盖完整路径

### 代价

- ⚠️ 旧 `private_memories` 表需要保留一段时间做对照（开发期未正式切读，保留双写现状）
- ⚠️ 当前 `SanitizedPayload()` 只剥 `reasoning` 字段，未来其他敏感字段（如 tool_call 的 raw 工具输出）需要扩展

## 后续

- 开发期无需启动 Expand-Contract 三阶段迁移（决策 2026-07-01）
- 商业化前重新评估 `archive/memory-a2a-redesign-v1.2.md` §3 流程

## 关联

- 完整设计原文：[`../archive/memory-a2a-redesign-v1.2.md`](../archive/memory-a2a-redesign-v1.2.md)
- 代码位置：[`backend/internal/a2a/context_view.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/context_view.go)
- 测试：[`backend/internal/a2a/context_view_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/context_view_test.go)（10 项）
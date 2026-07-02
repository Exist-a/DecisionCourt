# ADR 0003: LLM Prompt ContextView 投影层

> **状态**：✅ Accepted & Implemented (2026-06-30)  
> **决策日期**：2026-06-29  
> **影响范围**：`internal/a2a/context_view.go`、`internal/agent/orchestrator_context.go`

## 背景

多 Agent 辩论中，如果控方看到辩方的私有推理链（如"我准备攻击 E003 的数据来源"），会出现两种负面效果：

1. **针对性防御**：控方提前修补自己的论点，导致辩论变成"信息博弈"而非证据博弈
2. **立场漂移**：某一方看到对方推理更有说服力后倾向附和，失去对抗性

但**完全隔离**又会让 LLM 失去上下文感知（看不到对方公开发言内容），辩论无法进行。

需要一种机制：**给每个 Agent 一个"按自己视角投影过的"上下文**，包含自己的私有记忆 + 对方公开信息（剥离推理）+ 公共证据板。

## 选项对比

| 维度 | A. Orchestrator 拼接完整 context 后过滤 | B. BuildContextView(selfAgent) 投影 | C. 让 LLM 自己过滤 |
|---|---|---|---|
| 集中控制 | ❌ 散落在每个调用点 | ✅ Bus 单点投影 | ❌ 不可控 |
| 测试覆盖 | ⚠️ 每个调用点都要写测试 | ✅ Bus 单点测 10 项 | ❌ 黑盒 |
| 性能 | ✅ 一次性拼接 | ✅ 一次 DB query + map 操作 | ⚠️ LLM 多花 tokens |
| 隔离不变量保证 | ⚠️ 容易漏 | ✅ SanitizedPayload 集中实现 | ❌ 不可信 |

## 决策

采用 **方案 B** —— `internal/a2a/context_view.go` 提供 `Bus.BuildContextView(sessionID, selfAgent)`，在 Orchestrator 构造 LLM prompt 前调用一次，返回 `LLMContext` 结构体：

```go
type LLMContext struct {
    WorkingMemory []model.A2AMessage  // public，对方消息已 sanitized
    PrivateMemory []model.A2AMessage  // self-only，全部
    Beliefs       map[string]float64
}
```

### 核心规则

1. **私有消息**：`from == selfAgent` 或 `to == selfAgent` 的 `visibility=private` 消息进 `PrivateMemory`（完整 payload）
2. **公开消息对方**：`from != selfAgent && from != orchestrator` 的 `visibility=public` 消息 → 调 `SanitizedPayload()` 剥离 `reasoning` 字段
3. **公开消息自己**：自己发的 `public` 消息保留完整 payload（让自己能反思自己的推理）
4. **orchestrator 视角**：传 `AddressOrchestrator` 作为 `selfAgent` 可看全部（仅供审计）

### 关键理由

- 隔离不变量由 `SanitizedPayload()` 集中实现，所有"剥离 reasoning"路径都走同一函数
- Orchestrator 业务代码无感知，只需调一个 `BuildContextView()` 即可
- 测试覆盖 10 项 + 总共 25 项 a2a 包测试

## 后果

### 收益

- ✅ 控辩方互相看不到对方 `reasoning`（Schema freeze 测试守门）
- ✅ Agent 仍能看到对方公开 `content` + 公共证据板（辩论必要信息保留）
- ✅ 单点实现 + 单点测试，扩展新 MessageType 不需要改 Orchestrator

### 代价

- ⚠️ `SanitizedPayload()` 当前只剥离 `reasoning` 字段 —— 未来其他敏感字段需要扩展
- ⚠️ 50 条 strategy_note × 200 token = 10K 上限当前用"全文注入"，未来超限需要滚动 + 摘要策略

## 关联

- 设计原文：[`../archive/memory-a2a-redesign-v1.2.md` §2 PR 1](../archive/memory-a2a-redesign-v1.2.md)
- 代码：[`backend/internal/a2a/context_view.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/context_view.go)
- 测试：[`backend/internal/a2a/context_view_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/context_view_test.go)
- 调用方：[`backend/internal/agent/orchestrator_context.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/orchestrator_context.go)
# ADR 0007: Token Budget 默认 reject-when-exhausted

> **状态**：✅ Accepted & Implemented (2026-07-01)  
> **决策日期**：2026-07-01  
> **影响范围**：`internal/agent_gateway/token_budget.go`、`internal/agent_gateway/gateway_config.go`、`.env.example` 默认值

## 背景

v0.5+ Token Budget 默认 `RejectWhenExhausted=false`。这导致一个隐蔽的 bug：

- 当 `budget_ratio` 超过 1.0 后，inner LLM **仍然被调用**、计费**继续累加**
- 但 Gateway 仍然返回 `status:success` 给调用方
- 审计日志里能看到 `budget_ratio=1.46` 但 `status:success` 的"隐性超额"
- 用户账单超支但审计看不出来

这是典型的"对内透明 + 对外沉默"反模式。

## 选项对比

| 维度 | A. 保持 false（默认放行） | B. 改为 true（默认拒绝） | C. 加告警 + 放行 |
|---|---|---|---|
| 账单可预测性 | ❌ 隐性超额 | ✅ 显式拒绝 | ⚠️ 仍有超额 |
| 审计透明度 | ❌ 沉默 | ✅ `status=error` | ⚠️ 警告易忽略 |
| 兼容性 | ✅ 老部署行为 | ⚠️ 老部署需显式设 false | ✅ |
| 用户体验 | ⚠️ 超额无声 | ✅ 立即看到 ErrBudgetExhausted | ⚠️ |

## 决策

采用 **方案 B** —— `AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED` 默认值从 `false` 改为 `true`（决策 2026-07-01）。

### 行为变更

- **预算耗尽时**：Gateway 直接返回 `ErrBudgetExhausted`，**不调用 inner LLM**
- **审计行**：写一条 `status=error, error_msg="budget exhausted"` 的审计行
- **GatewayConfig.IsRejectWhenExhaustedEnabled()**：加 child-default 同步逻辑，保持只设 `ENABLED=true` 也能开

### 向后兼容

老部署可在 `.env` 显式设 `AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED=false` 恢复兼容行为。

### 关键理由

- 账单可预测性 > 兼容性 —— 超支是更严重的 bug
- 默认行为应该"安全"（拒绝）而不是"宽松"（放行） —— fail-closed 原则
- 老部署可以显式覆盖，新部署从一开始就有正确的默认值

## 后果

### 收益

- ✅ 账单可预测 —— 超额立即返回 error，调用方可以选择降级
- ✅ 审计透明度 —— `llm_calls` 表里能清楚看到哪些是 budget 拒绝
- ✅ 符合 fail-closed 原则

### 代价

- ⚠️ 老部署升级后可能看到"莫名 error" —— 需要在 changelog 显式提示
- ⚠️ 调用方需要处理 `ErrBudgetExhausted` —— 通常是 fallback 到不调 LLM 或返回降级结果

## 关联

- 代码：[`backend/internal/agent_gateway/token_budget.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/token_budget.go)
- 配置：[`backend/internal/config/config.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/config/config.go)（`viper.SetDefault` 默认值变更）
- 关联 ADR：[ADR 0006 Smart Compression](./0006-smart-prompt-compression.md) —— Smart 减少单次 tokens 间接降低 budget 触达频率
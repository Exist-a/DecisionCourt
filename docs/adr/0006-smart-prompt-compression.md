# ADR 0006: Agent Gateway v2 Smart Prompt Compression

> **状态**：✅ Accepted & Implemented (2026-07-01)  
> **决策日期**：2026-07-01  
> **影响范围**：`internal/agent_gateway/prompt_compressor.go`、新增 `prompt_scorer.go` / `prompt_atomic.go` / `prompt_greedy.go` / `prompt_summary.go`
>
> **状态更新（2026-10-09 复测）**：⚠️ **下表的数字（含本行复测值）是合成 fixture 微基准，不是生产实测** —— 输入是测试里手写的假庭审（重复填充文本 + fake LLM + 人工灌预算到 throttle 档，见 `compression_eval_test.go` 的 `fixedTrialTranscript()`），且真实庭审的预算峰值 69.4% 压在 70% 触发线下、压缩从未触发（`OBSERVABILITY.md` §8.6）。它**只能用于策略横向对比，不能当产品指标**。实跑 `go test ./internal/agent_gateway/ -run TestCompressionEval_StrategyComparison -v` 现为：Legacy 19→6 条 / **70.3%**（不变）；Smart 19→**13** 条 / **34.1%**（原子组 17 / 保留 9 / 最近强制 1）。差异来源：v2.10 [ADR 0044](0044-token-compression-strategy-review.md) #5 把预算单位从"字符数"换成内容感知 `EstimateTokens`（CJK 1.5 tok/char、ASCII 0.25 tok/char），保留组数由 10 变 9。另注意：该 eval 测试只断言下限（legacy ≥25%、smart >0、`atomicKept ≥ 9`），**不钉**节省率与保留条数 —— 见 [deferred-items-2026-10-09 §M6](../todo/deferred-items-2026-10-09.md)。

## 背景

v0.5+ 的 "Legacy keep-5" Prompt 压缩策略（保留 system + 最近 5 条 user/assistant 消息）虽然简单有效，但在庭审多角色交叉引用场景下有 3 个问题：

1. **证据丢失**：cross-exam 中律师的 tool_call（investigator_search）+ tool output + 后续 evidence 引用是因果链，硬切会同时切掉
2. **法官推理丢失**：judge assess 这一类 meta-reasoning 不是 message 但对后续发言有指引
3. **价值均匀假设**：客服/coding 这类上下文价值均匀的场景没问题，但庭审场景"证据 + 推理"价值密度不均匀

业界参考：PROCLAIM 2026 关于"高一致可能是错的"的警示 → 不要均匀压缩。

## 选项对比

| 策略 | 实现 | 庭审场景适用性 | 节省 |
|---|---|---|---|
| **Legacy keep-5** | system + 最近 5 条 | ❌ 丢证据链 + 丢法官推理 | 70.3% |
| **固定窗口摘要** | 最近 N 条 + 之前 LLM 摘要 | ⚠️ 引入额外 LLM 调用 | 50% |
| **Smart 评分压缩** ✅ | 评分 + 原子组 + 贪心打包 + 兜底摘要 | ✅ 保留因果链 + 高价值优先 | 31.4% |

## 决策

采用 **Smart 评分压缩**（v2 方案）。

### 核心算法

1. **评分（Scorer）** —— 给每条消息打分（基于 evidence_id 引用、tool_call_id、@角色提及、轮次位置等）
2. **原子组（Atomic Group）** —— 把同一证据/工具调用相关的多条消息识别成"原子组"（如 tool_call + observation + 后续引用 evidence 的发言）
3. **贪心打包（Greedy Pack）** —— 按"高密度 + 低 token"原则贪心填充 token 预算
4. **强制保留** —— 最近 N 条（默认 3）强制保留（防"丢光"）
5. **兜底摘要** —— 丢弃 > K 条时插入 1 条 earlier-context 摘要 note（列出 evidence_id / @prosecutor 等锚点）

### Baseline Benchmark（2026-07-01）

12 轮庭审 transcript（19 条消息，4763 字符 ≈ 1190 tokens）：

| 策略 | applied | 消息 19 → | chars 4763 → | tokens 节省 | 节省比 | 信息密度 |
|---|---|---|---|---|---|---|
| 禁用压缩（baseline） | ❌ | 19 | 4763 | 0 | 0.0% | — |
| Legacy keep-5（v0.5+） | ✅ | 6 | 1415 | 837 | **70.3%** | 235 chars/msg |
| Smart 评分压缩（v2） | ✅ | **14** | 3264 | 374 | 31.4% | 233 chars/msg |

### 关键发现

1. Legacy 数字更漂亮（70.3%），但把 evidence、judge assess、tool_call 整组都扔了 → **下一轮 LLM 幻觉**
2. Smart 留更多条（14 vs 6），但每条都是"高价值"；信息密度 ≈ Legacy
3. 单庭审 10 次压缩估算：Legacy ≈ ¥0.84，Smart ≈ ¥0.37 —— 多花 ¥0.47/庭审换不丢事实链

### 关键理由

- 庭审场景"证据 + 推理"价值密度不均匀，均匀压缩会丢因果链
- 贪心打包 + 原子组识别是业界推荐方案（参考 Inductivee Context Window Management）
- 兜底摘要保证即使预算极紧也不会完全丢历史

## 后果

### 收益

- ✅ 庭审场景不丢证据链 → 判决质量提升
- ✅ 默认 Smart（`AGENT_GATEWAY_SMART_COMPRESSION=true` 可开关对比）
- ✅ 通过 `TestCompressionEval_StrategyComparison` 守门 baseline

### 代价

- ⚠️ 实现复杂度高于 Legacy（新增 4 个文件：scorer / atomic / greedy / summary）
- ⚠️ 节省率从 70.3% 降到 31.4% —— Token 成本更高
- ⚠️ 评分函数可能对未知场景欠拟合，需要在真实庭审数据上回归

## 关联

- 完整设计原文：[`../archive/agent-gateway-advanced-plan.md`](../archive/agent-gateway-advanced-plan.md)
- Baseline 数据：[`backend/internal/agent_gateway/compression_eval_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/compression_eval_test.go)
- 代码：[`backend/internal/agent_gateway/prompt_compressor.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/prompt_compressor.go)、`prompt_scorer.go`、`prompt_atomic.go`、`prompt_greedy.go`、`prompt_summary.go`
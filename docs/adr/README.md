# ADR · 架构决策记录

> **ADR**（Architecture Decision Record）是业界标准的"决策追溯"机制 —— 每份记录一个**已确定**的关键架构决策，包含**背景 / 选项对比 / 决策 / 后果**，便于后续维护者理解"为什么是这样"。
>
> 本目录收录 9 份关键决策，编号递增。修改 ADR 必须保留"决策当时"的上下文；如果决策变更，应该新增一份"撤销 / 替代"的 ADR。

---

## 索引

| # | 决策 | 状态 | 关联代码 |
|---|---|---|---|
| [0001](./0001-mvp-tech-stack.md) | MVP 技术栈（Go + Next.js + PG + Redis + DeepSeek） | ✅ | `backend/cmd/server/main.go` |
| [0002](./0002-a2a-private-channel.md) | 私有记忆底层迁移到 A2A 私有通道 | ✅ | `internal/a2a/`、`internal/private_memory/` |
| [0003](./0003-contextview-projection.md) | LLM prompt 投影层（BuildContextView） | ✅ | `internal/a2a/context_view.go` |
| [0004](./0004-bayesian-belief-engine.md) | v0.6 信念引擎升级（贝叶斯 log-odds + 锚定） | ✅ | `internal/belief/engine_v06.go` |
| [0005](./0005-investigation-findings.md) | 调查发现独立表（与用户证据严格分离） | ✅ | `internal/investigation/` |
| [0006](./0006-smart-prompt-compression.md) | Agent Gateway v2 Smart Prompt Compression | ✅ | `internal/agent_gateway/prompt_*` |
| [0007](./0007-token-budget-rejection.md) | Token Budget 默认 reject-when-exhausted | ✅ | `internal/agent_gateway/token_budget.go` |
| [0008](./0008-cross-exam-user-trigger.md) | 质证轮次控制改为用户触发 | ✅ | `internal/courtroom/service.go` |
| [0009](./0009-courtroom-vis-simplify.md) | 庭审页面可视化简化 | ✅ | `frontend/components/courtroom/ArgumentMap.tsx` |
| [0010](./0010-whitebox-observability.md) | v0.8 后端白盒化（slog + Prometheus + OTel-Span + decision_events） | ✅ | `internal/observability/` |

---

## ADR 模板

新增 ADR 时使用以下结构：

```markdown
# ADR XXXX: <决策标题>

> **状态**：✅ Accepted / ⏳ Proposed / ❌ Deprecated / 🔄 Superseded by YYYY  
> **决策日期**：YYYY-MM-DD  
> **影响范围**：<涉及的模块 / 文件 / 表>

## 背景

<为什么要做这个决策？业务/技术上的痛点是什么？>

## 选项对比

| 维度 | A. <选项 A> | B. <选项 B> | C. <选项 C> |
|---|---|---|---|

## 决策

采用 **方案 X** —— <一句话总结决策>。

### 关键理由

- <理由 1>
- <理由 2>

## 后果

### 收益

- ✅ <收益 1>
- ✅ <收益 2>

### 代价

- ⚠️ <代价 1>
- ⚠️ <代价 2>

## 关联

- 主文档：<path>
- 代码：<path>
- 测试：<path>
```

---

## 写作规约

- **编号递增**：永远不重写旧 ADR，新增 ADR 必须新编号
- **保留历史**：决策变更时不要修改原 ADR，新增"撤销 ADR"
- **关联文档**：每个 ADR 末尾必须有"关联"区块指向代码 / 测试 / 主文档
- **状态标记**：
  - `✅ Accepted` —— 决策已落地并保持
  - `⏳ Proposed` —— 提议中，未实装
  - `❌ Deprecated` —— 决策已被替代（关联到新 ADR）
  - `🔄 Superseded by XXXX` —— 决策被新 ADR 替代
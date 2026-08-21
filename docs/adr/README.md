# ADR · 架构决策记录

> **ADR**（Architecture Decision Record）是业界标准的"决策追溯"机制 —— 每份记录一个**已确定**的关键架构决策，包含**背景 / 选项对比 / 决策 / 后果**，便于后续维护者理解"为什么是这样"。
>
> 本目录收录 34 份关键决策，编号递增。修改 ADR 必须保留"决策当时"的上下文；如果决策变更，应该新增一份"撤销 / 替代"的 ADR。

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
| [0011](./0011-llm-probability-hard-clamp.md) | LLM 输出概率值后端硬编码 Clamp（v0.8.4 修复 DeepSeek 抽风） | ✅ | `internal/agent/probability.go` |
| [0012](./0012-ha-and-concurrency.md) | v0.9 单机部署（含公网）的高可用与并发防护（5 子项） | ✅ | `internal/courtroom/` · `internal/api/` · `internal/agent_gateway/` |
| [0013](./0013-llm-gateway-engineering.md) | v0.9 LLM Gateway 工程化（per-call Timeout + Response Cache + Circuit Breaker） | ✅ | `internal/agent_gateway/` |
| [0014](./0014-user-rate-limit.md) | v0.9 用户级 Trial 限流（每用户每天 N 次） | ✅ | `internal/ratelimit/`（新）+ `internal/api/` |
| [0015](./0015-evidence-fidelity-no-hallucination.md) | v0.9.1 证据真实性与 LLM 幻觉防御（短证据输入触发编造细节） | ✅ | `internal/agent/prompts.go` + `orchestrator.go` |
| [0017](./0017-websocket-uuid-credential.md) | WebSocket 鉴权改为"UUID 即凭证"（owner 软校验） | ✅ | `internal/api/websocket.go` |
| [0018](./0018-websocket-origincheck-init-timing.md) | WebSocket CheckOrigin 改为运行时重读 config（Go init-timing 修复） | ✅ | `internal/api/websocket.go` |
| [0020](./0020-frontend-analytics-via-decision-events.md) | 前端埋点复用 v0.8 decision_events 基础设施（v0.10） | ✅ | `internal/api/handler_events.go` · `frontend/lib/transport.ts` · `frontend/lib/analytics/` |
| [0021](./0021-llm-hallucination-output-validator.md) | LLM 输出硬编码反幻觉验证器（v0.10.1 加固 ADR 0015） | ✅ | `internal/agent/output_validator.go` · `react_runner.go` · `prompts.go` |
| [0022](./0022-github-actions-ci-cd.md) | GitHub Actions CI/CD（v0.10.2：test + tag push deploy） | ✅ | `.github/workflows/test.yml` · `deploy.yml` |
| [0023](./0023-github-actions-ci-pause.md) | GitHub Actions CI 暂停 + 恢复完整复盘（v0.10.2 ~ v0.10.15 共 14 版迭代） | ✅ | `.github/workflows/test.yml` + `deploy.yml` + 8 个 Secrets |
| [0024](./0024-silent-error-fix-pr1.md) | 静默错误全局修复 PR 1（后端 UserFacingError 类型 + broadcast + 状态机新 action） | ✅ | `internal/courtroom/errors.go` + `statemachine.go` + `service.go` + `api/handler.go` + `api/websocket.go` + `agent/react_runner.go` |
| [0025](./0025-security-p0-closeout.md) | 安全审计 P0 阶段收尾（P0-2 JWT_SECRET 默认值 + P0-3 容器 UID 10001） | ✅ | `internal/config/config.go` + `Dockerfile` + `cmd/server/main.go` |
| [0026](./0026-viper-bindenv-fix.md) | Viper BindEnv 显式绑定 — v0.10.18/19 安全 P0 收尾补充（5 个关键 env 修复） | ✅ | `internal/config/config.go` + SSH 诊断 |
| [0027](./0027-rate-limit-defense-in-depth.md) | 4 层限流防御深度（Defense in Depth, ADR 0012 单机部署假设下） | ⏳ Proposed | `internal/ratelimit/` + `middleware/session_ratelimit.go`（新）+ `courtroom/concurrency.go`（新）+ `observability/metrics.go` |
| [0028](./0028-env-or-default-helper.md) | envOrDefault helper 全面修复 viper 25+ env lowercase bug（v0.10.21 PR-C） | ✅ | `internal/config/env.go` + `config.go` + 26 sub-test |
| [0029](./0029-deepseek-v4-migration.md) | DeepSeek v3→v4 模型硬迁移（v1.0.0 P0-前置）| ✅ | `internal/config/config.go` + `.env.example` + 8 个 mock test + 3 个 config test |
| [0030](./0030-evidence-rebuttal-state-machine.md) | **候选 4: 已反驳证据集合跟踪状态机（v1.0.2 PRD §4.3.3 roadmap §0）** | ✅ | `internal/model/evidence_rebuttal_link.go` (NEW) + AutoMigrate |
| [0031](./0031-prompt-lab-architecture.md) | **Prompt Lab 架构：baseRules YAML 化 + 热加载 + LLM-as-judge Eval + A/B（v1.0.3 V1-ROADMAP M2）** | ✅ | `backend/prompts/base.yaml` (NEW) + `internal/promptlab/` (NEW) + REST `/api/v1/prompts/*` |
| [0032](./0032-remove-argument-map.md) | **移除 ArgumentMap 观点地图（叙事流优先于信息图重写，v1.0.3）** | ✅ | 删除 `frontend/components/courtroom/ArgumentMap.tsx` + 移除 `reactflow` 依赖 + 更新 PRD / tech-spec / roadmap / README |
| [0033](./0033-llm-trace-architecture.md) | **LLM Trace 架构：FileLogger JSON Lines 读端聚合 + REST + 前端时间轴可视化（v1.0.4 V1-ROADMAP M3）** | ✅ | `backend/internal/trace/` (NEW) + `backend/internal/api/handler_trace.go` (NEW) + `frontend/components/trace/` (NEW) + `frontend/lib/animations/` (NEW) |
| [0034](./0034-silhouette-architecture.md) | **厕所标识剪影小人架构：原生 SVG + CSS keyframes + env var fallback（v2.0 V1-ROADMAP M4）** | ✅ | `frontend/components/courtroom/silhouettes/` (NEW) + `frontend/app/globals.css` (MODIFIED, + 90 行) + `frontend/components/courtroom/AgentAvatar.tsx` (MODIFIED) |

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
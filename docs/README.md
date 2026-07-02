# 决策庭 · 项目文档索引

> 面向**项目内部开发者**（包括后续维护者、协作者、自己 3 个月后回来接手）的文档索引。
>
> **外部 GitHub 访客请看仓库根目录的 [`README.md`](../README.md)。**
>
> 最后整理：2026-07-02

---

## 1. 阅读路径建议

按下面顺序读，对项目全貌的建立最快：

1. [`decisioncourt-prd.md`](./decisioncourt-prd.md) — 产品定位、MVP 边界、Agent 协作设计
2. [`decisioncourt-tech-spec.md`](./decisioncourt-tech-spec.md) — 整体技术栈、模块划分、目录结构
3. [`decisioncourt-agent-design.md`](./decisioncourt-agent-design.md) — 庭审状态机、Agent 协作时序、Prompt 设计
4. [`decisioncourt-api-design.md`](./decisioncourt-api-design.md) — REST + WebSocket 协议
5. [`decisioncourt-db-design.md`](./decisioncourt-db-design.md) — 表结构、ER 关系
6. [`decisioncourt-roadmap.md`](./decisioncourt-roadmap.md) — 已实装 / 待办 / 第二阶段

进阶阅读：

- [`decisioncourt-ux-refinement.md`](./decisioncourt-ux-refinement.md) — UX 决策与视觉细节
- [`adr/`](./adr/) — 9 份关键架构决策记录（每个决策背后的"为什么"）
- [`archive/`](./archive/) — 已完成的详细设计文档（完整原文，不删）

---

## 2. 主项目文档清单（8 份）

| # | 文档 | 内容 | 当前版本 |
|---|---|---|---|
| 1 | [decisioncourt-prd.md](./decisioncourt-prd.md) | 产品需求 + Agent 角色 + 庭审流程 + 信念引擎 + MVP 边界 | v0.7 |
| 2 | [decisioncourt-api-design.md](./decisioncourt-api-design.md) | REST + WebSocket 协议 + A2A 事件 + 错误码 + 幂等性 | v0.7 |
| 3 | [decisioncourt-db-design.md](./decisioncourt-db-design.md) | 表结构 + ER 关系 + 业务流 | v0.6 |
| 4 | [decisioncourt-tech-spec.md](./decisioncourt-tech-spec.md) | 技术栈 + 目录结构 + Agent Gateway + WebSearch + 高可用规划 | v0.7 |
| 5 | [decisioncourt-agent-design.md](./decisioncourt-agent-design.md) | 状态机 + Agent 协作时序 + Prompt 模板 + 防止附和 | v0.7 |
| 6 | [decisioncourt-ux-refinement.md](./decisioncourt-ux-refinement.md) | UX 决策 + 视觉规范 + 已知 bug 清单 | v0.5 |
| 7 | [decisioncourt-roadmap.md](./decisioncourt-roadmap.md) | 实施阶段 + 里程碑 + 进度快照 | v0.7 |
| 8 | [project-ideas.md](./project-ideas.md) | 选题说明（简历叙事 + 核心亮点） | v0.3 |

---

## 3. 架构决策记录（ADR）

[`adr/`](./adr/) 收录 9 份关键架构决策，每份 1 个文件。每份 ADR 包含：**背景 / 选项对比 / 决策 / 后果**。

| # | 决策 | 关联代码 |
|---|---|---|
| [0001](./adr/0001-mvp-tech-stack.md) | MVP 技术栈（Go + Next.js + PG + Redis + DeepSeek） | `backend/cmd/server/main.go` |
| [0002](./adr/0002-a2a-private-channel.md) | 私有记忆底层迁移到 A2A 私有通道 | `internal/a2a/`、`internal/private_memory/` |
| [0003](./adr/0003-contextview-projection.md) | LLM prompt 投影层（BuildContextView） | `internal/a2a/context_view.go` |
| [0004](./adr/0004-bayesian-belief-engine.md) | v0.6 信念引擎升级（贝叶斯 log-odds + 锚定） | `internal/belief/engine_v06.go` |
| [0005](./adr/0005-investigation-findings.md) | 调查发现独立表（与用户证据严格分离） | `internal/investigation/`、`investigation_findings` 表 |
| [0006](./adr/0006-smart-prompt-compression.md) | Agent Gateway v2 Smart Prompt Compression | `internal/agent_gateway/prompt_*` |
| [0007](./adr/0007-token-budget-rejection.md) | Token Budget 默认 reject-when-exhausted | `internal/agent_gateway/token_budget.go` |
| [0008](./adr/0008-cross-exam-user-trigger.md) | 质证轮次控制（用户点击触发每轮） | `internal/courtroom/service.go` |
| [0009](./adr/0009-courtroom-vis-simplify.md) | 庭审页面可视化简化 | `frontend/components/courtroom/ArgumentMap.tsx` |
| [0010](./adr/0010-whitebox-observability.md) | v0.8 后端白盒化（slog + Prometheus + OTel-Span + decision_events） | ✅ | `internal/observability/` |

### 5.5 v0.8+ 持续可观测性完善计划

按"使用数据驱动"思路分五阶段推进：

| 阶段 | 版本 | 触发条件 | 工作量 |
|---|---|---|---|
| **Phase A** 数据采集 | v0.8.1 | 跑 5-10 场真实庭审 | 1-2 周 |
| **Phase B** 增量埋点 | v0.8.x | Phase A 统计报告 | 2-3 周 |
| **Phase C** Prometheus | v0.9.0 | 日均 LLM > 100 | 2-3 周 |
| **Phase D** OTLP / Jaeger | v1.0.0 | 多实例部署 | 3-4 周 |
| **Phase E** 数据仓库 | v1.x | 商业化启动 | 4-6 周 |

详细计划：[`roadmap/whitebox-roadmap.md`](./roadmap/whitebox-roadmap.md)

---

## 4. 历史归档（[`archive/`](./archive/)）

原 `.trae/documents/` 下的"进行中设计文档"已经全部落地，归档保留完整原文。**这些是历史快照，未来修改代码请以 `docs/` 下主文档和 ADR 为准**。

- `archive/memory-a2a-redesign-v1.2.md` — v0.5 记忆系统 + A2A 重设计完整原文（PR 1-4 已落地，v0.5+ 修补已完成）
- `archive/todolist1-pr1-contextview.md` — PR 1 ContextView 投影详细 todo 列表
- `archive/agent-gateway-advanced-plan.md` — Agent Gateway v0.5+ 高级能力实施计划（已落地）
- `archive/gateway-necessity-evaluation.md` — Agent Gateway 必要性评判（已落地）
- `archive/庭审可视化简化计划.md` — 庭审页观点地图 + 立场曲线 简化方案
- `archive/质证阶段轮次控制修改计划.md` — 质证阶段每轮用户触发的详细修改计划

---

## 5. 实装状态矩阵（截至 2026-07-02）

> **模块 × 状态 × 代码位置** 速查。**✅ = 已实装**，**⏳ = 计划中**。

### 5.1 后端核心

| 模块 | 状态 | 代码位置 |
|---|---|---|
| A2A 消息总线 + 路由 + 可见性隔离 | ✅ | `internal/a2a/bus.go`（12 项测试） |
| ContextView 投影 + 4 种 private MessageType | ✅ | `internal/a2a/context_view.go`（10 项测试） |
| 私有记忆池（Repository 接口 + InMemory/GORM） | ✅ | `internal/private_memory/`（9 项测试） |
| 调查发现独立表 + Service | ✅ | `internal/investigation/`（10 项测试） |
| ReAct Runner（thought / tool_call / reflect / speak） | ✅ | `internal/agent/react_runner.go` |
| Orchestrator Prompt 注入（ContextView + private memory） | ✅ | `internal/agent/orchestrator_context.go` |
| ReAct reflect 自动分类写记忆 | ✅ | `internal/agent/reflect_classifier.go` |
| 信念引擎（贝叶斯 log-odds + 锚定 + weaken 边） | ✅ | `internal/belief/engine_v06.go` |
| 信念审计 trail（belief_diffs + GET /belief-diffs） | ✅ | `internal/belief/diff.go`、`internal/model/belief_diff.go` |
| 智能收敛（推理震荡 > 共识 > 稳定 > 兜底） | ✅ | `internal/belief/convergence.go` |
| 庭审状态机（idle → opening → cross_exam → closing → verdict） | ✅ | `internal/courtroom/statemachine.go` |
| 质证轮次用户触发（round.waiting_for_user + continue_cross_exam） | ✅ | `internal/courtroom/service.go` |
| LLM 流式（StreamComplete + hub.Broadcast sleep 30ms） | ✅ | `internal/llm/client.go`、`internal/api/hub.go` |
| Agent Gateway 白盒子集（统一接入 + 审计 + trace） | ✅ | `internal/agent_gateway/gateway.go` |
| Agent Gateway v0.5+ 高级能力（压缩 / 预算 / 限流 / Fallback / 文件日志） | ✅ | `internal/agent_gateway/` |
| Agent Gateway v2（Smart Compression + Token Budget Reject） | ✅ | `internal/agent_gateway/` |
| **白盒化 — slog 结构化日志** | ✅ (v0.8) | `internal/observability/logger.go` |
| **白盒化 — Prometheus-兼容业务指标** | ✅ (v0.8) | `internal/observability/metrics.go`（11 类业务指标 + 4 类系统指标） |
| **白盒化 — Span + decision_events 业务事件审计** | ✅ (v0.8) | `internal/observability/trace.go` + `internal/model/decision_event.go` |
| **白盒化 — Trace / Metrics / Recovery Gin middleware** | ✅ (v0.8) | `internal/observability/middleware.go` |
| **白盒化 — 端到端 trace_id 串联（HTTP → ctx → A2A → LLM）** | ✅ (v0.8) | `TraceMiddleware` + `websocket.go` 改造 + `X-Request-ID` header |
| **白盒化 — `GET /metrics` 端点** | ✅ (v0.8) | `internal/api/handler.go` `MetricsHandler` |

### 5.2 前端核心

| 模块 | 状态 | 代码位置 |
|---|---|---|
| 首页 / 立案页 | ✅ | `frontend/app/page.tsx` |
| 庭审主界面（白底极简 + 凹陷输入框） | ✅ | `frontend/app/court/[id]/page.tsx` |
| AgentAvatar 头部气泡（调查 > 流式 > 思考 > 发言） | ✅ | `frontend/components/courtroom/AgentAvatar.tsx` |
| InvestigatorPanel（独立 Tab） | ✅ | `frontend/components/courtroom/InvestigatorPanel.tsx` |
| MemoryAuditPanel（4 种 kind 配色 + 真实法庭 toggle） | ✅ | `frontend/components/courtroom/MemoryAuditPanel.tsx` |
| BeliefDiffCard / BeliefTrajectoryTab / ConvergenceBadge | ✅ | `frontend/components/courtroom/Belief*` |
| 观点地图 ArgumentMap（精简版） | ✅ | `frontend/components/courtroom/ArgumentMap.tsx` |
| 判决书页 + trial_summary + JSON/PDF 导出 | ✅ | `frontend/app/verdict/[id]/page.tsx` |

### 5.3 第二阶段 / v0.9+ 计划（不在 MVP）

| 项 | 状态 | 备注 |
|---|---|---|
| Agent Gateway 模型路由 | ⏳ | 当前手工选 V3/R1 |
| Agent Gateway 响应缓存 | ⏳ | 庭审唯一性场景，价值低 |
| Agent Gateway 多实例 Token Budget 持久化 | ⏳ | 现仅内存 |
| 强制立场一致性检查（LLM-as-judge 打回重生成） | ⏳ | 现靠 Prompt 自约束 |
| 新意度检查（Jaccard 相似度 > 60% 强制换角度） | ⏳ | 现靠 Prompt 自约束 |
| 发言长度硬截断（300 字 + 重试） | ⏳ | 现靠 Prompt 自约束 |
| "已反驳证据"集合跟踪 | ⏳ | 未实装状态机 |
| Redis 分布式 WebSocket 广播 | ⏳ | 现单节点 Hub |
| 后端高可用 + 水平扩展 | ⏳ | **下一步讨论 → ADR 0011+** |
| OTel OTLP exporter / Prometheus text exporter | ⏳ | 当前 JSON 格式，未来可切换 |
| 业务级 span 全量埋点（courtroom service / orchestrator） | ⏳ | 当前仅 state_transition 已埋 |
| 专家证人 / 陪审团 / 历史庭审 / PDF 导出 | ❌ | 商业化前不启动 |

### 5.4 明确不做（决策日期 2026-07-01）

- ❌ 问题澄清与选项生成（用户只给模糊问题时）
- ❌ Agent 主动提问（识别信息缺口后向用户提问，回答转为证据）
- ❌ LLM 调用审计可视化（前端 dashboard）—— 后端 `llm_calls` 表 + JSON 文件日志已够
- ❌ v0.5 数据迁移 Phase 1-3 —— 开发期数据无保留价值

---

## 6. 下一步讨论（2026-07-02 起）

**主文档整合已完成。下一轮讨论：后端白盒化 / 高可用 / 并发防护**。

主要议题：

1. **白盒化** —— 让 LLM 调用、A2A 路由、状态机迁移全部可观测（trace-id 串联、决策日志、性能指标）
2. **高可用** —— 多实例 backend + WebSocket 分布式广播 + Redis Pub/Sub + LLM 调用异步化 + 数据库主从 + 熔断降级
3. **并发防护** —— 同一 session 的并发请求互斥、用户快速点击幂等、LLM 调用超时与重试、agent 死锁检测

议题产物规划：

- 第一步：发散讨论 → 收敛到 `docs/adr/0010-*.md` 系列 ADR
- 第二步：白盒化落地 → 在 `internal/observability/` 新增模块
- 第三步：高可用改造 → 在 `internal/distlock/` + `internal/wsbroker/` 新增模块

---

## 7. 文档维护规约

- **修改代码 → 同步更新对应文档**（AGENTS.md §1.2 强制）
- **裁决类逻辑（法官判决 / 证据有效性判定）** —— 先讨论后实现，AGENTS.md §2 强制
- **新增 ADR** —— 在 `docs/adr/` 递增编号，保持格式统一
- **归档已完成的设计** —— 移到 `docs/archive/`，主文档只保留"当前态"
- **README 索引更新** —— 实装状态矩阵每次有模块变更后同步

---

<p align="center"><sub>Built with ⚖️ · 让 AI 像法庭一样帮你把复杂决策看全、看透、看出可执行结论</sub></p>
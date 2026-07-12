# 决策庭 · 项目文档索引

> 面向**项目内部开发者**（包括后续维护者、协作者、自己 3 个月后回来接手）的文档索引。
>
> **外部 GitHub 访客请看仓库根目录的 [`README.md`](../README.md)。**
>
> 最后整理：2026-07-05（v0.9.1 部署就绪整合 + ADR 0015 防幻觉文档化）

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
| [0011](./adr/0011-llm-probability-hard-clamp.md) | v0.8.4 LLM 输出概率值后端硬编码 Clamp（DeepSeek 抽风修复） | ✅ | `internal/agent/probability.go` |
| [0022](./adr/0022-github-actions-ci-cd.md) | v0.10.2 GitHub Actions CI/CD（test.yml + deploy.yml + tag-based deploy） | ✅ | `.github/workflows/` |
| [0023](./adr/0023-github-actions-ci-pause.md) | v0.10.7~15 CI 暂停与恢复完整复盘（14 版迭代，✅ v0.10.15 端到端跑通） | ✅ | ADR 0023 §5 当前 dev 工作流 |

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
- `archive/refresh-and-reopen-fix-v0.8.3.md` — v0.8.3 修复"刷新丢数据 + 判决书回退无法继续开庭" 5 个根因 + 修复方案 + 测试矩阵

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
| **v0.9 单机部署 — session 互斥补锁**（ADR 0012 PR1） | ✅ (v0.9) | `internal/courtroom/service.go` `sessionLocks` |
| **v0.9 单机部署 — Idempotency-Key 客户端 + 后端**（ADR 0012 PR2） | ✅ (v0.9) | `internal/idempotency/`（新）+ `frontend/lib/api.ts` |
| **v0.9 单机部署 — `runCrossExamRound` panic 兜底**（ADR 0012 PR4） | ✅ (v0.9) | `internal/courtroom/service.go` defer recover |
| **v0.9 单机部署 — 启动扫描恢复 active session**（ADR 0012 PR5） | ✅ (v0.9) | `internal/courtroom/recovery.go`（新） |
| **v0.9 LLM Gateway — per-call Timeout 90s**（ADR 0013） | ✅ (v0.9) | `internal/agent_gateway/gateway.go` |
| **v0.9 LLM Gateway — Response Cache（sync.Map + LRU + TTL）**（ADR 0013） | ✅ (v0.9) | `internal/agent_gateway/cache.go` |
| **v0.9 LLM Gateway — Circuit Breaker（sony/gobreaker）**（ADR 0013） | ✅ (v0.9) | `internal/agent_gateway/breaker.go` |
| **v0.9 用户级 Trial 限流**（ADR 0014） | ✅ (v0.9) | `internal/ratelimit/`（新）+ `handler.TrialRateLimiter` |
| **v0.9.1 证据真实性与 LLM 幻觉防御**（ADR 0015） | ✅ (v0.9.1) | `internal/agent/prompts.go` + `orchestrator.go` |

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

### 5.3 第二阶段 / v0.10+ 计划（不在 v0.9 范围）

| 项 | 状态 | 备注 |
|---|---|---|
| Agent Gateway 模型路由 | ⏳ | 当前手工选 V3/R1 |
| Agent Gateway 多实例 Token Budget 持久化 | ⏳ | 现仅内存 |
| 强制立场一致性检查（LLM-as-judge 打回重生成） | ⏳ | 现靠 Prompt 自约束 |
| 新意度检查（Jaccard 相似度 > 60% 强制换角度） | ⏳ | 现靠 Prompt 自约束 |
| 发言长度硬截断（300 字 + 重试） | ⏳ | 现靠 Prompt 自约束 |
| "已反驳证据"集合跟踪 | ⏳ | 未实装状态机 |
| Redis 分布式 WebSocket 广播 | ⏳ | 现单节点 Hub（v0.9 ADR 0012 已决策不引入） |
| 后端高可用 + 水平扩展 | ⏳ | ADR 0012 决策**单机部署**，架构层面不引入 Redis Pub/Sub |
| OTel OTLP exporter / Prometheus text exporter | ⏳ | 当前 JSON 格式，未来可切换 |
| 业务级 span 全量埋点（courtroom service / orchestrator） | ⏳ | 当前仅 state_transition 已埋 |
| LLM Output 验证（防幻觉正则扫） | ⏳ | ADR 0015 决策暂不做，留待 v1.x |
| 专家证人 / 陪审团 / 历史庭审 / PDF 导出 | ❌ | 商业化前不启动 |

### 5.4 明确不做（决策日期 2026-07-01）

- ❌ 问题澄清与选项生成（用户只给模糊问题时）
- ❌ Agent 主动提问（识别信息缺口后向用户提问，回答转为证据）
- ❌ LLM 调用审计可视化（前端 dashboard）—— 后端 `llm_calls` 表 + JSON 文件日志已够
- ❌ v0.5 数据迁移 Phase 1-3 —— 开发期数据无保留价值

---

## 6. v0.9.1 部署就绪总览（2026-07-04 同步）

v0.9 全部决策已落地,代码 + 测试 + 文档三向对齐,准备部署到阿里云单 ECS(2C2G + 香港免备案)。

### 6.1 已完成事项（2026-07-04 一日内清空）

| 维度 | 决策/ADR | 关键产出 |
|---|---|---|
| **高可用 / 并发** | [ADR 0012](./adr/0012-ha-and-concurrency.md) | 5 子项 PR 全落地:session 互斥补锁 + Idempotency-Key + LLM Timeout(已迁 0013) + panic 兜底 + 启动恢复 |
| **LLM Gateway** | [ADR 0013](./adr/0013-llm-gateway-engineering.md) | per-call Timeout 90s + Response Cache + Circuit Breaker(sony/gobreaker) |
| **用户限流** | [ADR 0014](./adr/0014-user-rate-limit.md) | 每用户每天 5 次 StartTrial(sync.Map + 滑动窗口) |
| **防幻觉** | [ADR 0015](./adr/0015-evidence-fidelity-no-hallucination.md) | baseRules 严禁编造细节 + buildContext source 标签 + user_interrupt 注入 |

### 6.2 部署就绪 checklist（2026-07-04）

- ✅ 后端 15 包测试全过(40+ 新测试,无回归)
- ✅ 前端 TypeScript 编译通过,Idempotency-Key 注入生效
- ✅ Dockerfile 多阶段 + aliyun 镜像(国内稳) + 非 root
- ✅ docker-compose 五服务齐全(postgres/redis/backend/frontend/caddy)
- ✅ `.env.example` 完整 + v0.9 配置全暴露 + 生产 .env 模板(`deploy/.env.production.template`)
- ✅ Caddy 反代配置 + 自动 HTTPS Let's Encrypt
- ✅ 集成测试通过(host curl 验证 /auth/anon + /courtrooms + /metrics)
- ✅ CORS preflight 允许 Idempotency-Key header
- ⏸️ **真域名 + DNS 解析**(用户责任)
- ⏸️ **真 ECS 部署 + Caddy 证书实测**(等域名)

### 6.3 下一轮议题(v0.10+ / Phase A 数据驱动)

按 [`roadmap/whitebox-roadmap.md`](./roadmap/whitebox-roadmap.md) 推进:

- ⏳ **Phase A 数据采集**:跑 5-10 场真实庭审,统计 `decision_events` 报告
- ⏳ **Phase B 增量埋点**:基于 Phase A 报告补业务级 span(courtroom service / orchestrator)
- 📦 **Phase C Prometheus exporter + Grafana**(2026-Q4)
- 📦 **数据库迁移管理**(golang-migrate,触发条件:首次 destructive schema 变更)

### 6.4 第二阶段 / v1.x 待办(不在 v0.10 范围)

- 多实例 backend + Redis Pub/Sub + LLM 异步化 + DB 主从
- Agent Gateway 模型路由 / 多 provider fail-over
- 强制立场一致性检查 / 新意度检查 / 300 字硬截断 / "已反驳证据"集合跟踪
- LLM Output 正则扫(ADR 0015 暂缓方案)
- 专家证人 / 陪审团 / 历史庭审 / PDF 导出 / 商业化

### 6.5 议题产物规划

- ✅ v0.9.1 阶段(2026-07-04 完成):4 份新 ADR(0012-0015)+ 9 个 PR 落地 + 部署就绪
- ✅ **v0.10 阶段(2026-07-12 完成)**:CI/CD 端到端跑通(ADR 0022+0023)+ 前端埋点(ADR 0020)+ 反幻觉加固(ADR 0021)+ 静默错误修复(待 PR 1)+ 安全审计(待 PR 1)
- 📦 v0.11+ 阶段(2026-Q4):Phase C Prometheus + Grafana + 静默错误 PR 1-7 + 安全审计 P0 修复
- 📦 v1.0 阶段(2027+):第二阶段商业化前置(可选)

---

## 7. 文档维护规约

- **修改代码 → 同步更新对应文档**（AGENTS.md §1.2 强制）
- **裁决类逻辑（法官判决 / 证据有效性判定）** —— 先讨论后实现，AGENTS.md §2 强制
- **新增 ADR** —— 在 `docs/adr/` 递增编号，保持格式统一
- **归档已完成的设计** —— 移到 `docs/archive/`，主文档只保留"当前态"
- **README 索引更新** —— 实装状态矩阵每次有模块变更后同步

---

<p align="center"><sub>Built with ⚖️ · 让 AI 像法庭一样帮你把复杂决策看全、看透、看出可执行结论</sub></p>
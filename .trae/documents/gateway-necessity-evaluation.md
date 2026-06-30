# 两个网关的必要性评判

> **范围**：评判 + 明确"白盒 + 日志"目标的最小实现路径
> **日期**：2026-06-30
> **参考**：[JavaGuide LLM Gateway](https://javaguide.cn/ai/system-design/llm-gateway.html)
> **结论摘要**：API Gateway **必要且已实装**；Agent Gateway **MVP 阶段不必要完整版，但"白盒化 + 日志记录"是项目本身的硬需求**，应按"最小可观测子集"立即实施，**而非等到第二阶段**。

---

## 1. 现状摸底

### 1.1 文档中提到的"网关"共两个

| 名称 | 位置 | 文档来源 | 当前代码状态 |
|------|------|----------|--------------|
| **API Gateway** | HTTP/WS 入口 | PRD §9.1、tech-spec §2.3、README | ✅ 已实装 = `internal/api`（Gin REST + WebSocket Hub）|
| **Agent Gateway** | Agent Orchestration ↔ LLM 中间层 | PRD §9.4、tech-spec §6.4、agent-design §10.3、roadmap §14.3、PRD §10.2 | ❌ 未实装（无 `internal/agent_gateway` 目录，标记为第二阶段、非 MVP）|

### 1.2 准备度检查

- `llm_calls` 表已在 [db-design §3.7](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-db-design.md) 定义，字段齐全（token / cost / latency / status），但**全仓库零写入**。
- `internal/llm/client.go` 的 `Client` interface 已包含 `Complete` / `StreamComplete`，是天然的 Gateway 挂载点。
- `config.AppConfig` 中 LLM 配置仅支持单 provider（`deepseek`），无模型路由字段。

---

## 2. 用户诉求对齐：为什么 Agent Gateway 不应等到第二阶段

参考 [JavaGuide LLM Gateway](https://javaguide.cn/ai/system-design/llm-gateway.html) 总结的"直连模型典型问题"：

> "用户投诉『刚才 AI 胡说』，排查时找不到模型输入输出。"
> "散了就很难管。"

**DecisionCourt 的特殊场景**：
- 多 Agent 并发（4-5 个 Agent × 3-5 轮 = 20+ 次 LLM 调用/庭审）；
- A2A 协议 + 私有记忆 + 信念更新，每次发言都会触发 ReAct 多步推理；
- 庭审是核心业务，**任何幻觉/路由错/超时都直接影响判决可信度**；
- 当前 `internal/llm/client.go` 的 `Complete`/`StreamComplete` 完全无日志、无 trace、无成本归因。

**因此 Agent Gateway 在本项目里不是"未来优化项"，而是"业务可观测性的基础"**——即使不做完整版，"白盒化 + 日志记录"也必须从第一版就打通。这与 PRD 把 Agent Gateway 标为"第二阶段"不矛盾：本评判不要求做完整版，只要求做"白盒 + 日志"这一子集。

---

## 3. 必要性评判

### 3.1 API Gateway

**结论：必要，已实装，无需改造。**

- REST + WebSocket 入口统一在 [`internal/api`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api)；
- CORS / Recovery / Logger 已挂载；
- 是否补鉴权/限流/trace-id 取决于产品形态，MVP 不阻塞。

### 3.2 Agent Gateway —— 完整版

**结论：MVP 阶段不必要（沿用 PRD 标记）。**

完整版包括：多模型路由、Fallback、限流、配额、Token 预算、Prompt 压缩、缓存、语义缓存。
- MVP 单 provider（DeepSeek）已够；
- 没有用户配额诉求；
- ROI 低。

**留到第二阶段（PRD §10.2）。**

### 3.3 Agent Gateway —— "白盒 + 日志"最小子集

**结论：必要，应立即实施。**

按 [JavaGuide](https://javaguide.cn/ai/system-design/llm-gateway.html) 的能力清单，把本项目真正需要的"白盒 + 日志"列出来：

| 能力 | 必要性 | 说明 |
|------|--------|------|
| **统一接入**（decorator 包 `llm.Client`）| **必要** | 让所有 LLM 调用经过一个入口；为后续加路由/限流留位置 |
| **观测与审计**（写 `llm_calls` 表 + 结构化日志）| **必要** | 白盒化的核心；能回答"这次庭审每次 LLM 调了什么、花了多少、走哪个模型" |
| **Trace 关联**（`session_uuid` + `agent_type` + `request_id`）| **必要** | 把 20+ 次调用串联到一次庭审 + 一个 Agent |
| 多模型路由 | 可选 | 当前 Orchestrator 手工选 R1/V3 已够 |
| 优雅降级 / Fallback | 可选 | MVP 单 provider，不需要 |
| 限流 / 配额 | 不必要 | 没用户配额 |
| Token 预算 | 不必要 | 没用户配额 |
| 成本归因（按租户/场景）| **低优** | MVP 没租户，但可顺手把 scene 字段写进 `llm_calls` |
| Prompt 压缩 | 不必要 | 输入量小 |
| 缓存 / 语义缓存 | 不必要 | 庭审是唯一性场景，缓存价值低 |

**"白盒 + 日志"子集包含前 3 项，是本次实施范围。**

---

## 4. 实施路径（最小子集，不做完整 Gateway）

按 [JavaGuide 推荐](https://javaguide.cn/ai/system-design/llm-gateway.html) 的"第一版 Gateway 可以很轻，只做统一封装、超时、重试和日志"，本项目最小子集如下：

### 4.1 新增 `internal/agent_gateway/`
```
internal/agent_gateway/
├── gateway.go          # 实现 llm.Client 装饰器，包内嵌 openaiClient
├── recorder.go         # 写 llm_calls 表的 hook
└── trace.go            # session_uuid / agent_type / request_id 注入 ctx
```

### 4.2 装饰器模式接入
- 现有 [`internal/llm/client.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/llm/client.go) 的 `Client` interface 不变；
- 新增 `agent_gateway.NewClient(inner llm.Client, recorder *Recorder) llm.Client` 返回装饰器；
- `Complete`/`StreamComplete` 装饰逻辑：埋点 → 转发 → 记录 usage/latency/err → 写库；
- 修改 `cmd/server/main.go` 装配链：`agent_gateway.NewClient(llm.NewClient())` 替换 `llm.NewClient()`；
- Orchestrator 内部调用入口不变，**对业务零侵入**。

### 4.3 数据落库
- 复用 [db-design §3.7](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-db-design.md) 的 `llm_calls` 表；
- 字段映射：

| llm_calls 字段 | 写入来源 |
|------|------|
| `id` / `llm_call_id` | uuid.New() |
| `session_id` | ctx（ctx 注入） |
| `agent_type` | ctx（ctx 注入） |
| `model` | `opts.Model` |
| `provider` | 写死 `deepseek`（MVP）|
| `prompt_tokens` / `completion_tokens` / `total_tokens` | `Usage` |
| `latency_ms` | `time.Since(start)` |
| `status` | `success` / `error` |
| `error_message` | err.Error()（截断 500 字）|
| `request_id` | uuid.New() |
| `created_at` | time.Now() |

### 4.4 ctx 注入点
需要在 orchestrator 的 `speakWithReAct`、`JudgeAssess`、`ClerkSummary`、ReAct `react_runner.go` 等所有 `llm.Client.Complete/StreamComplete` 调用前注入：

```go
ctx = agent_gateway.WithTrace(ctx, sessionUUID, agentType)
```

### 4.5 受影响文件清单
- 新增：[`backend/internal/agent_gateway/gateway.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gateway.go)
- 新增：[`backend/internal/agent_gateway/recorder.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/recorder.go)
- 新增：[`backend/internal/agent_gateway/trace.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/trace.go)
- 新增：[`backend/internal/agent_gateway/gateway_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gateway_test.go)
- 修改：[`backend/cmd/server/main.go`](file:///d:/源码/FullStack/DecisionCourt/backend/cmd/server/main.go) — 装配链
- 修改：[`backend/internal/agent/orchestrator.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/orchestrator.go) — ctx 注入
- 修改：[`backend/internal/agent/react_runner.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/react_runner.go) — ctx 注入
- 修改：[`docs/decisioncourt-prd.md`](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-prd.md) §9.4 — 标记"白盒子集"在 MVP 实装
- 修改：[`docs/decisioncourt-tech-spec.md`](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-tech-spec.md) §6.4 — 同上
- 修改：[`docs/decisioncourt-roadmap.md`](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-roadmap.md) — 把 Agent Gateway 白盒子集从第二阶段移到 v0.5+

### 4.6 验证
1. 跑一次完整庭审；
2. SQL 查 `llm_calls` 应有 20+ 行；
3. 每次庭审的 `session_id` 都出现；
4. `agent_type` 涵盖 prosecutor / defender / judge / clerk / investigator；
5. `total_tokens` / `latency_ms` 合理；
6. 单元测试：模拟错误路径，`status=error` 与 `error_message` 正确写入；
7. 文档同步：PRD/tech-spec/roadmap 反映"白盒子集已实装"。

---

## 5. 不在本评判范围

- 不做模型路由 / Fallback / 限流 / 配额 / Token 预算 / Prompt 压缩 / 缓存；
- 不改 `llm.Client` interface；
- 不动 `internal/llm/` 任何实现（保持向后兼容）。

---

## 6. 整体结论

| 网关 | 必要性 | 范围 | 建议动作 |
|------|--------|------|----------|
| **API Gateway** | 必要 | 全部（HTTP/WS/中间件）| 保留现状，不动 |
| **Agent Gateway 完整版** | MVP 不必要 | 模型路由 / Fallback / 限流 / 压缩 / 缓存 | **不做**，留到第二阶段 |
| **Agent Gateway 白盒子集** | **必要** | 装饰器 + 审计落库 + ctx trace | **下一轮实施**（约 2-3 小时）|

---

## 7. 参考资料

- [JavaGuide: 大模型网关详解](https://javaguide.cn/ai/system-design/llm-gateway.html)
- [PRD §9.4 Agent Gateway（非 MVP，第二阶段）](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-prd.md)
- [tech-spec §6.4 Agent Gateway 设计](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-tech-spec.md)
- [db-design §3.7 llm_calls 表](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-db-design.md)

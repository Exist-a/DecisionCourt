# DecisionCourt 完整链路与设计理念（面试向 · v0.8.1）

> **目的**：本文档用于**面试问答**时向面试官讲清楚 DecisionCourt 后端架构的全貌。
> **阅读对象**：候选人 / 面试官 / 新加入的工程师。
> **配套真实案例**：[`case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)（v0.8 白盒化 demo 时白盒化让隐藏 bug 显形）
> **更新于**：2026-07-02

---

## 0. 一句话定义

> **DecisionCourt 是一个"AI 庭审模拟系统"**：用户提供事实和双方立场，系统派出"AI 法官 + 控方 + 辩方 + 调查员 + 书记员" 5 个 Agent 角色，模拟整个开庭 → 质证 → 评议 → 判决的完整流程，最终给出"采纳 A 方案 / 采纳 B 方案 / 部分采纳"的判决及理由。
>
> 整套系统**对外看起来像产品**，**对内是"AI Agent 编排系统"**——比传统 SaaS 多 3 层抽象：Agent 角色层、Agent 间 A2A 通信层、白盒化可观测层。

---

## 1. 设计理念（"为什么"）

### 1.1 三个核心设计哲学

#### 哲学 1：业务语义 = 数据结构

不试图用通用 LLM 对话做"法庭"，而是把**"法庭"本身的语义**显式建模成数据结构：
- **庭审阶段** = 有限状态机（`idle → opening → cross_exam → closing → verdict`）
- **Agent 角色** = 类型枚举（prosecutor / defender / judge / investigator / clerk）
- **证据 / 调查发现** = 不可变数据 + 来源追溯
- **信念** = 贝叶斯后验（v0.6 升级）

**好处**：每一步都可审计、可回放、可中断、可继续。**不靠 LLM 自觉性，靠系统级约束**。

#### 哲学 2：A2A 总线 = 单一事实源

不靠 prompt engineering 让 agent "自己记住"对方说了什么，而是把 **Agent 间通信**显式走 A2A 总线（[`internal/a2a/bus.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/bus.go)），**所有消息都落库**到 `a2a_messages` 表，**有可见性隔离**（public / private / team_only）。

**好处**：
- 审判长决策可追溯（"为什么 AI 法官采信了 A？" → 查 A2A 消息 + 信念轨迹）
- 公开 vs 私有策略分得开（控方草稿笔记不会泄露给对方）
- 支持"庭审回放"——把 1 场庭审的 A2A 流重放成新一场

#### 哲学 3：白盒化 = 可调试性

不靠"调 LLM 试试看"，而是**白盒化每一处 LLM 调用 + 状态机迁移 + 业务事件**：
- LLM 调用 = 47 字段审计 + JSON 文件日志
- 状态机迁移 = `decision_events` 表 + metrics counter
- 业务事件 = trace_id 串联 + decision_event 落库

**好处**：上线后任何"庭审卡住"的问题，都能在 30 秒内定位到具体 trace_id 下的具体事件。

### 1.2 不做什么（同等重要）

| 不做 | 原因 |
|---|---|
| 不用 LangChain / AutoGen | 抽象太重，调试不透明；我们要"白盒化每一处" |
| 不用向量数据库 | 庭审规模小，PG + 全文检索够用；YAGNI |
| 不用 Redis 缓存业务数据 | 同上 |
| 不用 message queue | 单实例够用；用 Postgres 持久 + goroutine 异步 |
| 不用 K8s / Service Mesh | MVP 阶段用户量小，Docker Compose 足矣 |

---

## 2. 整体架构（"长什么样"）

```
┌─────────────────────────────────────────────────────────────────┐
│                        Frontend (Next.js)                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │ Trial UI │  │  Memory  │  │  Belief  │  │ AuditLog │        │
│  │ 庭审可视化│  │  记忆面板 │  │  信念面板 │  │ 审计面板  │        │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │
└──────────────────────────┬──────────────────────────────────────┘
                           │ HTTP / WebSocket
┌──────────────────────────▼──────────────────────────────────────┐
│                Gin HTTP Server (cmd/server/main.go)              │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Gin Middleware（v0.8 白盒化）                            │   │
│  │  - TraceMiddleware: 生成 / 提取 X-Request-ID → ctx.Trace │   │
│  │  - MetricsMiddleware: HTTP 延迟直方图                     │   │
│  │  - RecoveryMiddleware: panic 恢复 + 计数                  │   │
│  │  - CORSMiddleware: 跨域                                  │   │
│  └──────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ API Handler (internal/api/handler.go)                    │   │
│  │  - CreateCourtroom / GetCourtroom / StartTrial           │   │
│  │  - SubmitEvidence / UserAction / GetMessages             │   │
│  │  - /metrics (v0.8)                                       │   │
│  └──────────────────────────────────────────────────────────┘   │
└────────┬──────────────────────────────────────┬─────────────────┘
         │                                      │
         │ ctx.Trace                            │ ctx.Trace
         ▼                                      ▼
┌─────────────────────────┐      ┌─────────────────────────────┐
│  WebSocket Hub          │      │  Courtroom Service          │
│  (internal/api/hub.go)  │◄─────│  (internal/courtroom/...)   │
│  - 房间路由（session_uuid）│     │  - StateMachine             │
│  - A2A 事件推送          │      │  - 状态迁移 + 埋点（v0.8） │
│  - Metrics 计数（v0.8）  │      │  - 信念引擎 v0.6            │
└─────────────────────────┘      └────┬────────────────────────┘
                                    │ ctx.Trace
                                    ▼
┌──────────────────────────────────────────────────────────────┐
│  Agent Orchestrator (internal/agent/...)                      │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐              │
│  │ Prosecutor │  │  Defender  │  │   Judge    │  ...         │
│  │  (ReAct)   │  │  (ReAct)   │  │  (ReAct)   │              │
│  └────────────┘  └────────────┘  └────────────┘              │
│  - ReAct Loop: think → tool_call → reflect → speak           │
│  - 私有记忆自动分类（v0.5 reflect_classifier）                 │
│  - ContextView 投影（v0.5）                                   │
└──────────────────────┬───────────────────────────────────────┘
                       │ ctx.Trace
                       ▼
┌──────────────────────────────────────────────────────────────┐
│  Agent Gateway（统一 LLM 入口 · internal/agent_gateway/...）  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ v0.5+ 高级能力：                                       │  │
│  │  - Recorder: 每次 LLM 调用 47 字段审计                 │  │
│  │  - Prompt Compression: 超阈值自动压缩                  │  │
│  │  - Token Budget: 滑动窗口预算控制                      │  │
│  │  - Throttling: 超限排队                                │  │
│  │  - Fallback: LLM 失败自动降级                          │  │
│  │  - FileLogger: JSON 文件日志                           │  │
│  │  - ContextView Filter: 投影层过滤（v0.5）              │  │
│  └────────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ v2 升级（v0.7+）：                                     │  │
│  │  - Smart Prompt Compression: 关键消息不压缩            │  │
│  │  - Reject-When-Exhausted: budget 耗尽直接拒绝          │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────┬───────────────────────────────────────┘
                       │ HTTP (LLM API)
                       ▼
            ┌───────────────────────┐
            │  LLM API（DeepSeek）   │
            └───────────────────────┘
```

### 配套存储层

```
┌─────────────────────────────────────────────┐
│  PostgreSQL（单实例 Docker）                 │
│  - court_sessions: 庭审主表                 │
│  - messages: A2A 消息全量                    │
│  - evidences / investigation_findings: 证据  │
│  - beliefs / belief_diffs: 信念引擎（v0.6）  │
│  - private_memories: 私有记忆池              │
│  - llm_calls: LLM 调用审计                  │
│  - verdicts: 判决                            │
│  - decision_events: 业务事件（v0.8 新增）    │
│  - a2a_messages: A2A 落库                   │
│  - court_agents: Agent 注册                  │
└─────────────────────────────────────────────┘
┌─────────────────────────────────────────────┐
│  Redis（单实例 Docker，目前仅做 KV）         │
│  - (预留) 分布式锁、限流、布隆过滤器          │
└─────────────────────────────────────────────┘
```

---

## 3. 核心链路（"怎么做"——面试重点）

### 3.1 一次完整庭审的链路（端到端 trace_id 串联）

```
[用户] 浏览器 → 打开庭审页
   │
   │  ① HTTP GET /api/v1/courtrooms/abc-123
   │     X-Request-ID: trace-001
   ▼
[Gin] TraceMiddleware
   │  - 提取/生成 X-Request-ID → ctx.Trace{RequestID: "trace-001"}
   │  - 写响应头 X-Request-Id: trace-001
   ▼
[Handler] GetCourtroom
   │  - 从 model.DB 读 court_sessions 表
   │  - 返回 JSON
   ▼
[用户] 浏览器 → 立案
   │
   │  ② HTTP POST /api/v1/courtrooms/abc-123/start
   │     X-Request-ID: trace-002
   ▼
[Gin] TraceMiddleware (trace-002)
   ▼
[Handler] StartTrial
   │
   ▼
[Service] StartTrial（同步）
   │
   │  ③ transitionPhase(idle → opening)
   │     - 状态机校验 CanTransition(idle, opening) ✓
   │     - 写库 UPDATE court_sessions SET current_phase=opening
   │     - 🆕 v0.8 埋点：
   │        · counter: courtroom_state_transition_total{from=idle,to=opening}++
   │        · decision_events: 写一行 {event_type: state_transition, payload: {from,to,round}}
   │        · slog: {"msg":"state transition","session_uuid":"abc-123","from":"idle","to":"opening"}
   │     - 广播 phase.changed 事件到 Hub
   │
   │  ④ 异步 goroutine: speakWithReAct (prosecutor 开场陈述)
   │     - 写 agent.thinking_started 事件 → 推送给前端
   │     - 调 agent.Orchestrator.RunReAct (ctx 透传 trace-002)
   │     - 调 Agent Gateway (ctx 透传 trace-002)
   │     - 调 DeepSeek API
   │     - 返回 response → 流式推送给 Hub → 推送到前端
   │     - 🆕 v0.8 埋点：
   │        · Recorder 写 llm_calls 表（47 字段审计 + JSON 文件日志）
   │        · slog: {"msg":"[a2a] prosecutor → ... (speech, visibility=public, round=0)"}
   │
   ▼
[Hub] Broadcast → 推送 phase.changed + agent.speak
   │  - 通过 WebSocket 推送到前端
   │  - 🆕 v0.8 埋点：
   │     · counter: a2a_message_throughput_total{event_type="phase.changed"}++
   │     · counter: a2a_message_throughput_total{event_type="agent.speak"}++
   ▼
[用户] 浏览器收到实时推送（WebSocket）
```

**面试强调点**：

1. **trace_id 一以贯之** —— 同一个 X-Request-ID 从 HTTP 进入 → 走遍 service / orchestrator / gateway / LLM → 落到 `decision_events` 表 → 出现在 slog 日志中
2. **埋点不阻塞主流程** —— 失败仅 log warn，不 panic，不影响业务
3. **同步 vs 异步分明** —— 状态机迁移是同步（HTTP 响应能看到结果），Agent 推理是异步（流式推送）

### 3.2 状态机迁移链路（最关键的同步链路）

```go
// internal/courtroom/service.go transitionPhase
func (s *Service) transitionPhase(session *CourtSession, phase Phase, round int) error {
    // 1. 状态机校验（防非法迁移）
    if !s.stateMachine.CanTransition(session.CurrentPhase, phase) {
        return fmt.Errorf("cannot transition from %s to %s", ...)
    }

    // 2. 写库
    s.db.Model(session).Updates(map[string]interface{}{...})

    // 3. 🆕 v0.8 三重埋点
    s.metrics.IncCounter(MetricStateTransitionTotal, labels)        // (1) 业务指标
    s.recorder.Record(ctx, DecisionEventRecord{EventType: "state_transition", ...})  // (2) 落库
    slog.Info("state transition", "session_uuid", ..., "from", ..., "to", ...)       // (3) 结构化日志

    // 4. 广播 A2A 事件
    s.broadcastEvent(session.SessionUUID, Event{Type: "phase.changed", ...})

    return nil
}
```

**面试强调点**：

- **为什么状态机校验在 service 层、不能放在 LLM prompt 里** —— prompt 是软约束，状态机是硬约束。LLM 想 transition 也得过这道关。
- **为什么埋点写 3 处** —— metrics（实时统计）+ decision_events（审计回放）+ slog（排错）三支柱，覆盖不同查询场景。

### 3.3 LLM Gateway 装饰器模式（Agent Gateway）

```go
// 装饰器链：Recorder → PromptCompression → TokenBudget → Throttling → Fallback → LLM
func (g *Gateway) Complete(ctx, req) (resp, err) {
    // 1. Recorder.Record(in)  // v0.5+ 47 字段审计
    // 2. PromptCompression.Compress(req)  // 超阈值压缩
    // 3. TokenBudget.Check(req)  // 预算检查 / reject
    // 4. Throttling.Wait(req)  // 排队
    // 5. resp, err = g.inner.Complete(ctx, req)  // 真调 LLM
    // 6. Fallback.OnError(err)  // 失败降级
    // 7. Recorder.Record(out)  // v0.5+ 47 字段审计
    return resp, err
}
```

**面试强调点**：

- **Decorator Pattern** —— 高级能力（压缩 / 预算 / 限流 / 降级）通过**装饰器链**叠加，不侵入内层 LLM
- **可插拔** —— config 开关控制每个装饰器启用 / 禁用
- **可观测** —— 每个装饰器都进 Recorder 审计，事后可回放"这次为什么变慢"

### 3.4 A2A 总线 + 可见性隔离

```go
// internal/a2a/bus.go
type Bus struct {
    repo    Repository
    broadcaster func(sessionUUID, eventType string, payload map[string]interface{})
}

func (b *Bus) Publish(ctx, msg Message) error {
    // 1. 写库 a2a_messages 表（无论可见性，都落库）
    b.repo.Insert(msg)

    // 2. 广播（仅 public / team_only 广播；private 不广播）
    if msg.Visibility == VisibilityPublic {
        b.broadcaster(msg.SessionUUID, msg.Type, msg.Payload)
    }

    return nil
}
```

**3 种可见性**：
- **public** —— 双方都看得到（控方发言、辩方发言）
- **team_only** —— 同方可见（控方团队内部讨论）
- **private** —— 仅 agent 自己（私有草稿，**不广播**，**但仍落库**用于审计）

**v0.5 修复的经典 bug**：`SessionUUID` 房间钥匙 bug —— 之前用 `SessionID.String()`（DB 主键）当 WebSocket 房间 key，跟 session_uuid（业务 key）不一致，导致消息进错房间。**v0.5 修复后所有消息必须用 `Message.SessionUUID` 跟 hub room key 一致**。

### 3.5 信念引擎（v0.6 升级）

```go
// internal/belief/engine_v06.go
type BayesianEngine struct {
    diffRepo   DiffRepository    // belief_diffs 审计 trail
    weakenRepo WeakenRepository  // evidence_weaken_links weaken 边
}

// 每次 Agent 接收新证据 / 主张时：
func (e *BayesianEngine) UpdateBelief(agentID, evidenceID string) error {
    prior := e.getPrior(agentID)        // 上一次 posterior
    likelihood := e.calcLikelihood(evidence)  // 证据 → likelihood
    weakenFactor := e.getWeakenFactor(evidence) // weaken 边 → 抑制

    posterior := bayesianUpdate(prior, likelihood, weakenFactor)  // log-odds 更新

    // 审计：写 belief_diffs
    e.diffRepo.Insert(BeliefDiff{
        AgentID: agentID,
        EvidenceID: evidenceID,
        PriorLogOdds: prior,
        PosteriorLogOdds: posterior,
        DeltaLogOdds: posterior - prior,
    })
}
```

**v0.6 升级亮点**：
- **贝叶斯 log-odds** 而非简单 0-100 分（数学上更严谨）
- **weaken 边** —— 证据 A 反驳证据 B（"A 的存在降低 B 的可信度"）
- **锚定** —— 强证据不因反驳而大幅漂移
- **审计 trail** —— `belief_diffs` 表全量记录每次变化，可视化为 BeliefTrajectoryTab

**收敛判断**（4 信号优先级）：
1. 推理震荡收敛（连续 N 轮 belief_diffs 变化幅度 < 阈值）
2. 控辩共识（双方 belief 距离 < 阈值）
3. 稳定轮次（N 轮 belief_diffs 数量 < 阈值）
4. 兜底（达到 max_rounds 强制判决）

---

## 4. 完整文件清单（面试时方便指代码）

### 4.1 核心模块（按层）

| 层 | 路径 | 关键文件 |
|---|---|---|
| 入口 | `backend/cmd/server/main.go` | 装配链路 |
| HTTP | `backend/internal/api/handler.go` | REST 端点 |
| HTTP | `backend/internal/api/hub.go` | WebSocket 路由 |
| HTTP | `backend/internal/api/websocket.go` | WS 消息分发 |
| 白盒化 | `backend/internal/observability/*.go` | slog / metrics / span / middleware |
| 业务 | `backend/internal/courtroom/service.go` | 庭审 + 状态机 + 收敛 |
| 业务 | `backend/internal/courtroom/statemachine.go` | 状态机定义 |
| Agent | `backend/internal/agent/orchestrator.go` | 5 Agent 编排 |
| Agent | `backend/internal/agent/react_runner.go` | ReAct 循环 |
| Agent | `backend/internal/agent/reflect_classifier.go` | 私有记忆自动分类（v0.5） |
| 通信 | `backend/internal/a2a/bus.go` | A2A 消息总线 |
| 通信 | `backend/internal/a2a/context_view.go` | ContextView 投影（v0.5） |
| 通信 | `backend/internal/private_memory/*.go` | 私有记忆池 |
| 信念 | `backend/internal/belief/engine_v06.go` | 贝叶斯引擎 |
| 信念 | `backend/internal/belief/convergence.go` | 4 信号收敛 |
| 网关 | `backend/internal/agent_gateway/gateway.go` | 装饰器链入口 |
| 网关 | `backend/internal/agent_gateway/recorder.go` | 47 字段审计 |
| 网关 | `backend/internal/agent_gateway/prompt_compress.go` | 压缩（v0.5） |
| 网关 | `backend/internal/agent_gateway/token_budget.go` | 预算（v0.5+ / v2） |
| 网关 | `backend/internal/agent_gateway/throttling.go` | 限流 |
| 网关 | `backend/internal/agent_gateway/fallback.go` | 降级 |
| 网关 | `backend/internal/agent_gateway/file_logger.go` | JSON 文件日志 |
| 模型 | `backend/internal/model/*.go` | GORM 表结构 |
| 配置 | `backend/internal/config/config.go` | viper + .env |

### 4.2 数据库表（11 张）

```
court_sessions       庭审主表
court_agents         Agent 注册（每庭审 5 个）
evidences            用户提交证据
investigation_findings 调查员发现（v0.2+ 跟用户证据严格分离）
messages             庭审消息（含 phase / round / visibility）
private_memories     私有记忆池（v0.5）
beliefs              Agent 信念（v0.6）
belief_diffs         信念变化审计（v0.6）
evidence_weaken_links 异构论辩图谱 weaken 边（v0.6）
verdicts             判决
llm_calls            LLM 调用审计（v0.5+ 47 字段）
a2a_messages         A2A 消息全量（v0.5+）
decision_events      业务事件审计（v0.8 新增）
```

---

## 5. 面试常见问题

### Q1：为什么不用 LangChain / AutoGen？

> **A1**：抽象太重，调试不透明。我们的核心诉求是"白盒化每一处"——LangChain 把 prompt / tool / memory / agent 都藏在黑盒里。DecisionCourt 是 AI 编排系统不是 AI 对话产品，**业务语义优先**（庭审阶段、Agent 角色、信念、证据都是显式数据），**LLM 退化为推理引擎**。

### Q2：状态机为什么不用 LangGraph 之类的？

> **A2**：LangGraph 是图状态机，适合**复杂非确定性流转**。我们庭审是**有限状态机**（5 个状态、有限迁移），用 Go 原生 enum + map 表达更直接，**强类型 + 编译期校验**。LangGraph 的灵活性在 5 状态场景是 over-engineering。

### Q3：白盒化 = 怎么做的？

> **A3**：v0.8 实现"三大支柱 + 业务事件"：
> - **Logging**: `log/slog`（Go 1.21+ 标准库） + `slog.With(trace)` 自动注入 trace_id
> - **Metrics**: 进程级 `Metrics` 接口 + 内存实现 + `GET /metrics` 端点（JSON）
> - **Tracing**: 自实现 `Span` 接口 + `decision_events` 表落库（按 session_uuid / request_id 索引）
> - **端到端 trace 串联**: Gin `TraceMiddleware` 生成 / 提取 `X-Request-ID` → `ctx.Trace` → 全链路透传
>
> **未来**: Phase C 加 Prometheus exporter / Phase D 加 OTel OTLP（**接口预留，不改业务代码**）

### Q4：LLM Gateway 装饰器模式解决了什么问题？

> **A4**：把"高级能力"（压缩 / 预算 / 限流 / 降级）和"LLM 调用"**解耦**。每个能力独立可插拔、独立可测、独立可开关。Decorator Pattern 的经典应用，但用在 LLM 编排上需要解决：
> - **审计统一**：每个装饰器都进 Recorder，47 字段覆盖 prompt / completion / latency / tokens / cost / compressed / throttled / budget_ratio
> - **context 透传**：trace_id 一路到 DeepSeek API
> - **降级链**：Fallback 不是简单 catch，而是按模型列表 / 按 prompt 简版 / 按 mock 顺序降级

### Q5：A2A 总线和直接调 LLM 有什么区别？

> **A5**：直接调 LLM = Agent 自己 query 历史。**问题**：跨 Agent 信息隔离做不到、私有草稿做不到、行为可追溯做不到。
> **A2A 总线** = Agent 间的"微信"，所有消息**显式发出去 / 显式收到 / 显式落库 / 显式设可见性**。**好处**：
> - 公开 vs 私有策略分得开（控方草稿笔记不会泄露给对方）
> - 审判长决策可追溯（"为什么 AI 法官采信 A？" → 查 A2A 流 + 信念轨迹）
> - 支持庭审回放（把 1 场庭审的 A2A 流重放成新一场）
> - 跨 Agent 的"通信"和"协作"有了 first-class 数据结构

### Q6：v0.6 信念引擎为什么用贝叶斯 log-odds？

> **A6**：v0.5 之前用 0-100 分，**问题**：分数不可加、不可减、不可复合。贝叶斯 log-odds 的好处：
> - **数学上严谨**：独立证据的 log-odds 可加
> - **weaken 边可表达**：证据 A 反驳证据 B = A 的 log-odds 减去 B 的 log-odds
> - **锚定自然**：log-odds 的范围是无穷大，强证据不会因反驳"归零"
> - **审计可视化**：log-odds 差值 = 信念变化幅度，可直接画 BeliefTrajectory

### Q7：项目最有技术挑战的部分是什么？

> **A7**：(1) **白盒化**——"AI 系统"天然黑盒化，强制把 LLM 调用 / 状态机迁移 / 业务事件全部上日志 / metrics / decision_events，**单 PR 改动 800+ 行**。(2) **A2A 可见性隔离**——3 种可见性 × 5 个 Agent × 异步 goroutine × WS 推送 × 落库审计 = 大量边界 case。(3) **ReAct reflect 私有记忆自动分类**——LLM 输出的"反思"文本，要分类到 4 种 MessageType（strategy_note / opponent_weakness / self_correction / evidence_eval），分类错误会导致草稿笔记泄露或私有策略丢失。

### Q8：如何保证生产可用？

> **A8**：**当前状态 = L2 级白盒化 + 单实例 MVP**。已做：白盒化（v0.8）、测试覆盖（22 个包都有 _test.go）、结构化日志、关键埋点。**未做（v0.9+ 计划）**：Prometheus exporter（Phase C）、多实例水平扩展（Phase D 需要 Redis Pub/Sub）、LLM 熔断（v0.9+）、PostgreSQL 主从 + 读写分离。**白盒化让"未做"的优先级有数据可依**——v0.8 跑两天看 metrics 就能决定 Phase C/D/E 哪个先做。

---

## 6. 真实案例（强烈建议在面试中说）

详见 [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)：
- **故事**：v0.8 demo 跑 1 场庭审，**白盒化**让 v0.5 之前就有的 `llm_calls` 外键 bug 立刻显形
- **价值**：没白盒化 = bug 持续 2 周但没人发现 = `llm_calls` 表空 = token 成本 / 调用次数 / 失败率**完全无法统计**
- **白盒化后**：30 秒定位 bug，5 行代码修复

---

## 7. 准备建议（面试前 1 小时）

1. **讲 1 遍完整链路**（按本文档 §3.1 顺序）
2. **解释 3 个设计哲学**（§1.1）
3. **回答 8 个常见问题**（§5，**至少答出 5 个**）
4. **背真实案例**（§6，30 秒讲完）
5. **不背代码**（指代码路径就行，**不要**逐行背实现）

---

**更新于**：2026-07-02
**作者**：DecisionCourt Team
**配套**：[`case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md) · [`../adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md)

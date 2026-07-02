# 01 · 整体架构思想（少代码，重思想）

> 面试 5 分钟必问题："简单讲下你这个项目的架构"
> 我讲 5 分钟，覆盖 3 件事：业务抽象 / 系统分层 / 4 个设计哲学
> **绝不背代码** —— 知道模块在哪、为什么这么分、互相怎么调用就行

---

## 1. 业务抽象（30 秒）

> DecisionCourt 把"AI 庭审"建模为 **5 个角色 + 1 个状态机 + 1 个总线 + 1 个信念引擎**。
>
> - **5 个角色** = 控方 / 辩方 / 法官 / 调查员 / 书记员
> - **1 个状态机** = idle → opening → cross_exam → closing → verdict
> - **1 个总线** = A2A 消息总线（所有 Agent 间的通信）
> - **1 个信念引擎** = 控辩双方各自对"事实"的相信度（贝叶斯 log-odds）

**关键洞察**：业务语义（庭审、阶段、证据、信念）全部是**显式数据结构**，**不是 LLM 自己理解的**。LLM 只是"推理引擎"，业务逻辑是上层硬约束。

---

## 2. 系统分层（5 层架构）

我从上到下分 5 层，每层只依赖下一层（不跨层）：

| 层 | 职责 | 关键模块 |
|---|---|---|
| **L1 入口** | HTTP / WebSocket 入口 + 中间件 | Gin + Trace middleware + Metrics middleware |
| **L2 API + 业务编排** | 庭审生命周期 + 状态机 | Service + State Machine + 5 个 Agent Orchestrator |
| **L3 Agent 引擎** | 单个 Agent 推理循环 | ReAct Runner + ContextView 投影 + 私有记忆 |
| **L4 基础设施** | A2A 通信 / 信念引擎 / 调查员 | A2A Bus + Bayesian Engine + Investigation Service |
| **L5 装饰器链网关** | LLM 调用的所有附加能力 | Agent Gateway（压缩 / 预算 / 限流 / 降级 / 审计） |

**关键洞察**：**L1 ~ L4 都是"业务侧"，L5 才是"AI 侧"**。LLM 退化为推理引擎，业务语义在 L1-L4 显式建模。

---

## 3. 4 个设计哲学（**最重要的部分**）

我用 4 个"主义"驱动整个项目决策。面试时**背这 4 个**，比背"做了什么"重要 10 倍。

### 哲学 1：**业务语义 > LLM 能力**

**不靠 LLM 自觉性，靠系统级硬约束**。

举例：
- 状态机迁移不放在 prompt 里让 LLM "记得当前阶段"，而是显式状态机 + 数据库字段 + 状态机校验函数。LLM 想 transition 也得过 `stateMachine.CanTransition()` 这道关。
- A2A 消息可见性不放在 prompt 里让 LLM "自己保密草稿"，而是 `Visibility = public / private / team_only` 枚举，bus 自动按可见性广播。
- 信念不用 0-100 主观分，用贝叶斯 log-odds 数学严谨计算。

**为什么**：LLM 不可靠（幻觉、漂移、遗忘），**业务语义放 LLM 里等于放定时炸弹**。

### 哲学 2：**A2A 总线 = 单一事实源**

**Agent 间通信不走 prompt 拼接，走显式消息总线**。

- 所有消息**显式发出 / 显式收到 / 显式落库 / 显式设可见性**
- 公开 vs 私有策略**分得开**（控方草稿笔记不会泄露给对方）
- 审判长决策**可追溯**（"为什么 AI 法官采信 A？" → 查 A2A 流 + 信念轨迹）
- 支持**庭审回放**（把 1 场庭审的 A2A 流重放成新一场）

**为什么**：直接调 LLM = Agent 自己 query 历史。**问题**：跨 Agent 信息隔离做不到、私有草稿做不到、行为可追溯做不到。A2A 把"通信"做成 first-class 数据结构。

### 哲学 3：**白盒化 = 可调试性**

**不靠"调 LLM 试试看"，靠白盒化每一处**。

- LLM 调用 = 47 字段审计（每次调用的 prompt / completion / latency / tokens / cost / compressed / throttled / budget_ratio 全记）
- 状态机迁移 = `decision_events` 表（业务级 span 落库）
- 业务事件 = `slog` JSON + `request_id` 串联（grep 一次能查到全链路）
- 实时指标 = `GET /metrics` 端点（11 类业务指标 + HTTP 延迟直方图）

**为什么**：AI 系统**天然黑盒化**。没白盒化 = "庭审卡住了，去翻日志 + 翻 DB + 翻文件日志 + 拼线索"。**v0.8 demo 当天就是靠白盒化立刻发现了 P1 bug**（详见 [`06-bug-stories.md`](./06-bug-stories.md)）。

### 哲学 4：**Decorator Pattern = 可插拔能力**

**不把所有能力塞进 LLM 客户端，把"高级能力"做成装饰器链**。

```
[LLM Client]
   ↓ 装饰
[Recorder]  ← 47 字段审计
[Compression] ← 超阈值自动压缩
[Budget] ← 滑动窗口预算
[Throttling] ← 超限排队
[Fallback] ← 失败降级
   ↓
[真实 LLM API]
```

**为什么**：高级能力（压缩 / 预算 / 限流 / 降级 / 审计）**独立可插拔、独立可测、独立可开关**。新增能力 = 加一个装饰器，不改其他。

---

## 4. 数据流（一次庭审）

```
[User] 浏览器
   │
   │ HTTP POST /api/v1/courtrooms/.../start (X-Request-ID: trace-1)
   ▼
[Gin Middleware] trace_id 注入 ctx.Trace
   ▼
[Service.StartTrial]
   │ 1. transitionPhase(idle → opening)   ← 同步，硬约束
   │    写库 + 写 decision_events + 写 metric + slog
   ▼
[Orchestrator.RunReAct]  ← 异步，goroutine
   │ 1. 调 Gateway.Complete(ctx, req)
   │    ctx.Trace 一路透传到 LLM API
   ▼
[Gateway.Decorators]  ← 装饰器链
   │ 1. Recorder.Record (审计) 47 字段
   │ 2. PromptCompression.Compress (智能压缩)
   │ 3. TokenBudget.Check (预算检查)
   │ 4. Throttling.Wait (排队)
   │ 5. inner.Complete (真实调 LLM)
   │ 6. Fallback.OnError (失败降级)
   │ 7. Recorder.Record (审计)
   ▼
[DeepSeek API] → response
   ▼
[A2A Bus.Publish]  ← Agent 间通信走这里
   │ 1. 落库 a2a_messages
   │ 2. 广播给 Hub (按可见性)
   │ 3. metric 计数
   ▼
[Hub.Broadcast] → WebSocket
   ▼
[User] 浏览器实时收到 agent.speak / agent.thinking_started
```

**关键洞察**：**trace_id 一以贯之**。同一个 `X-Request-ID` 从 HTTP 入口 → 走遍 service / orchestrator / gateway / LLM → 落到 `decision_events` 表 → 出现在 slog 日志中。**任何"庭审卡住"的问题 30 秒内能定位**。

---

## 5. 4 大亮点 vs 4 个哲学（怎么记住）

| 哲学 | 落地的模块 | 章节 |
|---|---|---|
| 业务语义 | 状态机 + 信念引擎 + A2A 可见性 | [`02`](./02-a2a-bus.md) [`03`](./03-belief-engine.md) |
| A2A 总线 | A2A Bus + ContextView 投影 + 私有记忆 | [`02`](./02-a2a-bus.md) |
| 白盒化 | 三大支柱 + decision_events + trace 串联 | [`05`](./05-whitebox-observability.md) |
| 装饰器 | Agent Gateway v2 装饰器链 | [`04`](./04-agent-gateway-v2.md) |

---

## 6. 关键名词（首次出现给定义）

| 名词 | 1 句话 |
|---|---|
| **A2A Bus** | Agent-to-Agent 消息总线。3 种可见性 + 落库审计 + 实时广播。 |
| **ReAct** | Reasoning + Acting 循环。LLM "思考 → 行动 → 观察" 模式。 |
| **ContextView** | LLM 提示词投影层。把 A2A 历史 + 私有记忆 + 公开状态投影到 LLM 看得懂的 prompt。 |
| **Bayesian log-odds** | 贝叶斯对数几率。相信度的数学表达，可加可减。 |
| **Decorator Pattern** | 装饰器模式。把"附加能力"叠加到核心功能上。 |
| **state_machine** | 状态机。庭审阶段的硬约束。 |
| **decision_events** | 业务事件审计表。白盒化的核心数据。 |
| **slog** | Go 1.21+ 结构化日志库。 |
| **trace_id** | 一次请求的唯一 ID。端到端串联。 |
| **BeliefDiffs** | 信念变化审计表。每次 belief 更新都留 trail。 |

---

## 【反思】

**关于"讲架构"的 3 个我自己的总结**：

1. **不要按文件讲**（"我有 main.go / handler.go / service.go..."）—— 面试官听不懂。**要按"分层 + 哲学"讲**。分层让面试官有"地图"，哲学让面试官有"理解"。

2. **4 个哲学比"做了什么"重要 10 倍**。我面试时如果只能留 1 件事给面试官记忆，我会留这 4 个哲学 + 1 个 bug 故事。**不背模块名，背思想**。

3. **数据流用"画箭头"讲**。不要念"先是 X 调 Y，Y 调 Z..." —— 在纸上画箭头（或者手势比划），面试官的"心智模型"会被箭头引导。**trace_id 是数据流的"灵魂"，一定要强调**。

---

**配套**：[`02-a2a-bus.md`](./02-a2a-bus.md)（A2A 详解）· [`03-belief-engine.md`](./03-belief-engine.md)（信念引擎）· [`05-whitebox-observability.md`](./05-whitebox-observability.md)（白盒化）

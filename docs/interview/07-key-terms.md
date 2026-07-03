# 07 · 关键技术名词速查（面试前 5 分钟扫一遍）

> **目标**：把所有 DecisionCourt / AI Agent / Observability 涉及的技术名词集中解释，**每个名词 1-3 句话 + "为什么它重要"**。**面试前 5 分钟扫一遍**就能自信地说出。
> **配套**：[`../glossary.md`](../../README.md) · [`../architecture/link-overview.md`](../architecture/link-overview.md)

---

## 0. 一句话总结

> 这一章是**字典**——**你需要的时候来查**，**面试前 5 分钟扫一遍**。**每个名词都附了"为什么它重要"**——面试讲术语不只是背定义，**讲清"为啥这个概念存在"才是真懂**。

---

## 1. AI / Agent 基础

### LLM (Large Language Model)
大语言模型。**DeepSeek / OpenAI / Claude / Qwen** 都是 LLM provider。LLM 通过自然语言 prompt 生成响应。**LLM 是黑盒**——它给响应，但**解释不了为什么这么回答**。

### Agent
"自主行动者"。LLM 本身只是文本生成器；**Agent = LLM + 工具调用 + 状态记忆 + 决策循环**。DecisionCourt 的"控方律师"是 1 个 Agent——它能循环思考（ReAct）→ 调工具（search 证据）→ 写发言稿（speak）。

### ReAct (Reason + Act)
Agent 的**核心循环**：`Thought → Tool Call → Observation → Thought → Speak`。DecisionCourt 5 个 Agent 每个都用 ReAct：先思考"我方论点是什么" → 调搜索找事实 → 决定要不要改变论点 → 输出。

### Prompt
给 LLM 的输入文本。**Prompt 设计 = LLM 应用的核心工程**。DecisionCourt 每个 Agent 有自己的**系统 prompt + 用户 prompt + 工具 prompt**，**Smart Compression** 严格保留这些。

### Context Window
LLM 单次能处理的 token 上限（DeepSeek 是 32K，Claude 是 200K）。**超出 = 截断 / 报错**。DecisionCourt 用 **Smart Compression 控制 context window**。

### Token
LLM 的"字"单位。**1 token ≈ 0.75 个英文单词 ≈ 1.5 个中文字符**。**token = 钱**——DeepSeek 价格 ¥2/百万 input token。**业务必须统计 token**（llm_calls 表的 `prompt_tokens` + `completion_tokens`）。

### Embedding
把文本转成高维向量（1536 维等）。**DecisionCourt v0.8 不依赖 embedding**——因为我们用全文 + Belief Engine 而不是 vector retrieval。

### Function Call
LLM 调用外部工具的机制。**OpenAI Function Calling / Claude Tools / DeepSeek 都支持**。DecisionCourt 用**自己定义 tool_calls**（搜索 / dispatch / reflect），通过 Function Calling 协议传给 LLM。

---

## 2. AI 工程模式

### State Machine（状态机）
有限状态自动机。**State = 当前阶段**（idle / opening / cross_exam / closing / deliberation），**Transition = 状态迁移规则**。DecisionCourt 的庭审流程是 **5 状态 × 多 round × 5 Agent** 的状态机。

### RAG (Retrieval Augmented Generation)
"检索增强生成"——给 LLM 找相关上下文再让 LLM 生成。**业内 AI 应用 80% 用 RAG**。DecisionCourt v0.6 的**Belief Engine** 是 RAG 的广义版：**先计算信念再用 belief 构造 prompt**。

### Multi-Agent
多 Agent 协作。**业内 3 种模式**：
1. **层级**（一个总 Agent + 多个子 Agent）—— DecisionCourt 部分场景
2. **协作**（多个 Agent 平级 + 消息总线）—— **DecisionCourt 主要模式**
3. **辩论**（多个 Agent 互相对抗）—— **DecisionCourt 庭审模式（控辩审调书）**

### Episodic Memory
"情景记忆"——Agent 记住自己"经历过的事"。**DecisionCourt v0.5 的私有策略笔记 = episodic memory**——Agent 在 ReAct 反思阶段写下"我刚才为什么这么回答"。

### Working Memory
"工作记忆"——Agent 当前的输入上下文。**DecisionCourt 把 working memory 设计成 3 部分**：①evidence 2. 当前 round 3. 最近 3 轮对话。

### 消息总线 (Agent-to-Agent Bus)
Agent 间通信。**业内有 2 个含义**：
- **Google A2A 协议（2025-04）**：跨组织 / 跨架构 Agent 通信标准，JSON-RPC over HTTP
- **DecisionCourt 含义**：进程内事件总线，**消息总线模式**（5 个 Agent 共享消息）

**答面试要小心区分**。

### Bus（DecisionCourt 含义）
进程内消息总线。**3 种可见性**（public / private / team_only）+ 落库审计。**核心约束**：所有 Agent 通信**必须有可见性标签 + 必须落 `a2a_messages` 表**。

---

## 3. Belief Engine 领域

### Bayesian Update
贝叶斯更新规则。`P(A|B) ∝ P(B|A) * P(A)`，**给定 evidence 更新 belief**。DecisionCourt 用 **logit 空间** 让更新**线性可加**：`new_logit = old_logit + evidence_logit`。

### Logit / Log-Odds
**对数优势比**：`logit(p) = log(p / (1 - p))`。让 0-1 概率**线性可加**。DecisionCourt 的核心数学工具——`belief_diffs` 表的 `prior_logit` / `posterior_logit` 是关键字段。

### Sigmoid
logit 的逆运算：`1 / (1 + e^-x)`。DecisionCourt 用 `sigmoid(logit)` 把 logit 转回 0-1 概率。

### Weaken Edge
**对方质疑此证据时降低权重**。DecisionCourt 用 `effective_w = w * (1 - weaken_factor)`，**典型 weaken_factor 0.2-0.5**。

### Anchor Pull
**把信念拉回 prior**，防止 Agent 信念漂移失控。DecisionCourt 每 N 轮强制 anchor 一次。

### Convergence
**4 个 Agent 信念高度一致 = 庭审收敛**。DecisionCourt 用 **3 信号 OR 逻辑**：variance 阈值（4 Agent 信念方差 < 0.01）/ delta 阈值（双方 logit 差异 > 2.0）/ 时间窗口（≥ 2 轮没新证据）。

### Audit Trail
**审计 trail**——所有 belief 变化的历史记录。`belief_diffs` 表 = belief 的 audit trail；`a2a_messages` 表 = Agent 通信的 audit trail；`decision_events` 表 = 业务事件的 audit trail。**3 个独立 trail = DecisionCourt 核心可解释性来源**。

---

## 4. Observability / 白盒化

### Observability
**可观测性**——通过外部输出（log/metric/trace）推断系统内部状态。**业内 3 支柱**：Logs / Metrics / Traces。

### Three Pillars（三大支柱）
- **Logs**：离散事件（发生了什么）
- **Metrics**：时间序列数值（现在有多严重）
- **Traces**：请求级链路（哪里慢 / 卡在哪）

### Whitebox
"白盒化"——与黑盒（只知道输出）相对，**让系统内部状态可观测、可调试、可审计**。DecisionCourt v0.8 的核心交付。

### Cardinality
**metric label 组合数**。`{session_uuid="abc-123"}` 1000 user = 1000 cardinality → Prometheus 内存爆炸。**业内戒律**：**label 用有限集合**（agent_type / status / phase）。

### Structured Log
**结构化日志**——JSON 格式，每条 log 是 1 行 JSON。**vs text log**：`"user abc did X"` 聚合不到 vs `{"user":"abc","action":"X"}` 能 grep / 能 SQL。

### Trace ID / Span ID / Parent Span ID
- **Trace ID**：整条请求链的全局 ID（UUID）
- **Span ID**：单个步骤的 ID
- **Parent Span ID**：父步骤的 ID → 构成 **span tree**

DecisionCourt 的 **HTTP X-Request-ID → ctx.Trace** 链路让 trace_id 端到端贯穿。

### OpenTelemetry / OTLP
**CNCF 标准的 observability 协议**。DecisionCourt v0.8 的 `Span` / `Tracer` interface 设计预留 OTel 兼容属性，未来切 OTel 不改业务。

### Prometheus / Grafana
- **Prometheus** = metrics 抓取 + 存储
- **Grafana** = 仪表盘（基于 Prometheus）

**DecisionCourt v0.9+ 接入**（Phase C roadmap），不预先做（避免过度工程化）。

### Jaeger / Tempo
- **Jaeger** = Uber 出品的 trace 系统
- **Tempo** = CNCF 的 trace 系统（成本低，用对象存储）

**DecisionCourt v0.9+ L3 阶段接入**，不预先做。

### ELK / Loki
- **ELK** = Elasticsearch + Logstash + Kibana（日志收集 + 索引 + 可视化）
- **Loki** = CNCF 日志系统（更轻）

**DecisionCourt v0.9+ 接入** stdout 日志持久化。

---

## 5. 软件工程模式

### Decorator Pattern（装饰器模式）
**用组合代替继承扩展功能**。DecisionCourt Gateway v2 的 5 个装饰器是经典应用：每个装饰器只关心一件事，可独立测试、可关闭、可扩展。

### Circuit Breaker（熔断器）
分布式系统经典模式——**失败率超阈值时熔断**，避免雪崩。Netflix Hystrix / Resilience4j / Sentinel 等库。**DecisionCourt Reliability 装饰器实现**。

### Retry with Exponential Backoff
重试 + 指数退避。`wait * 1s, 2s, 4s, 8s`——避免雪崩。DecisionCourt v0.7 默认 maxRetries=3, base=1s。

### Fallback Chain（降级链）
**provider 切换**：deepseek → openai → ollama → mock。**关键**：mock **永远**在最后兜底，**绝不会全失败**。

### Idempotency（幂等性）
**同一操作执行 N 次只产生 1 次效果**。DecisionCourt **store.appendBeliefDiff** 用 id 检查去重（**这是幂等保护**——但 bug 4 暴露了幂等保护本身的缺陷）。

### Smart Compression
DecisionCourt 自定义——**保留关键约束**（system prompt / 角色历史 / 最近 3 轮）**+ 压缩重复信息**（早期对话 / 重复陈述）。

### Hooks（钩子）
**在固定流程点插入自定义逻辑**。DecisionCourt 的 `slog.With(trace)` 是"log 钩子"——任何 log 都会带 trace_id；`transitionPhase` 是"状态机钩子"——任何状态迁移都会写 metric + decision_events。

### Snapshot Testing
**用真实快照测试**。DecisionCourt `engine_v06_test.go` 的 50+ 测试用例用真实 belief 计算实例，**assert posterior_belief_a ≈ expected**。这不是 simple unit test，是**行为快照**。

---

## 6. 业务流程领域

### Courtroom
"法庭"。DecisionCourt 的核心隐喻——**一切都是庭审**。**5 个 Agent** = 5 个角色（控方 / 辩方 / 法官 / 调查员 / 书记员）。**5 个阶段** = 5 个 phase（idle / opening / cross_exam / closing / deliberation）。

### Court Session（庭审会话）
1 次庭审 = 1 个 `court_sessions` row + N 个 evidence + N 个 belief_diff + N 条消息 + 最终 1 个 verdict。

### Verdict（判决书）
庭审最终输出——`verdicts` 表的 row，含**法官意见 + 双方分数 + 推荐 + 庭审纪要 + Markdown 格式判决书正文**。

### Options / Decision
庭审的"决策"——2 个选项 **option_a vs option_b**。**控辩双方各自代表一个**。**DecisionCourt = "推荐 / 不推荐" 风格的决策辅助工具**。

---

## 7. 数据库 / Schema 关键表

| 表 | 含义 | 关键列 |
|---|---|---|
| `court_sessions` | 庭审主表 | session_uuid, current_phase, current_round |
| `evidences` | 证据 | evidence_id (varchar), content, credibility_score |
| `agents` | Agent 配置 | agent_type, belief_a, belief_b, model |
| `belief_diffs` | 信念变化 | prior_logit, posterior_logit, delta_belief_a, reason |
| `a2a_messages` | Agent 通信 | from_agent, to_agent, message_type, visibility |
| `private_memories` | 私有策略笔记 | agent_id, type, content, linked_evidence_ids |
| `investigation_findings` | 调查发现 | dispatcher, query, summary, source_provider |
| `llm_calls` | LLM 调用审计 | total_tokens, latency_ms, status, cost_usd |
| `decision_events` | 业务事件 | event_type, trace_id, payload (jsonb) |
| `verdicts` | 判决 | option_a_score, option_b_score, recommendation |

---

## 8. 关键英文缩写速查

| 缩写 | 全称 |
|---|---|
| MVP | Minimum Viable Product |
| SLO | Service Level Objective（4 个 9 = 99.99%） |
| SLA | Service Level Agreement |
| L4 / L3 / L2 / L1 | Observability 5 级模型的对应级别 |
| OSS | Open Source Software |
| LLM | Large Language Model |
| RAG | Retrieval-Augmented Generation |
| 消息总线 | Agent-to-Agent Bus |
| OTel | OpenTelemetry |
| OTLP | OpenTelemetry Protocol |
| CNCF | Cloud Native Computing Foundation |
| Pn/P1/P2/P3 | Priority Number（优先级） |
| ADR | Architecture Decision Record |
| YAGNI | You Aren't Gonna Need It |
| DRY | Don't Repeat Yourself |
| KISS | Keep It Simple, Stupid |
| CRUD | Create / Read / Update / Delete |

---

## 9. DecisionCourt 专有名词

| 名词 | 含义 |
|---|---|
| DecisionCourt | 项目名 |
| Trial / Courtroom / Hearing | 都指"庭审"（同义） |
| Phase | 庭审阶段（idle/opening/cross_exam/closing/deliberation） |
| Round | 同一 phase 内的轮次（如 cross_exam round 1, round 2） |
| Belief / Stance | "相信度"（A 还是 B 的概率） |
| Verdict | 判决书 |
| Investigation | 调查（dispatch investigator 找证据） |
| Finding | 调查发现 |
| Hearing / Trial | 庭审 |

---

## 10. 面试常用黑话（避免掉的坑）

| 黑话 | 真正含义 |
|---|---|
| "我们用了微服务" | 通常 = 拆得过细 / Saga 复杂度过高 |
| "我们用了 LangChain" | = 复用抽象 / 但失去控制 / debug 难 |
| "我们上了 Kubernetes" | = 上 K8s 不代表云原生 / 通常运维反复杂度高 |
| "AI 幻觉" | = LLM 输出看似合理但错误 |
| "我们的 AI 是 99% 准确" | = 通常训练集 99%，测试集 70% |
| "我们用了 React" | = 默认前端栈 / 没差异化 |
| "我们做了 CI/CD" | = 大部分公司 = GitHub Actions 跑个 build |

**避免在面试里"用技术名词代替解释"**。**面试官听得懂，但要看你的理解深度，不是名词列表**。

---

## 11. 5 个核心设计哲学（必背）

1. **业务驱动 vs 技术驱动**：业务硬需求 = 必做（消息总线 / 贝叶斯 / 白盒化）；技术崇拜 = 不做（GraphQL / 微服务 / LangChain）。
2. **意图与实现一致**：注释写啥实现做啥，**不要"意图 vs 现实"脱节**。
3. **数据层正确 ≠ 链路正确**：必须端到端真实跑业务 + 每层数据自洽。
4. **可解释优于可优化**：法庭场景下"能解释为什么判 A 不判 B" 比"判对了"更重要。
5. **接口预留 = 未来不返工**：`Metrics` / `Span` interface 设计预留 OTel 兼容属性。

---

**下一步**：
- [`08-faq-30-questions.md`](08-faq-30-questions.md) —— 30 个面试问题（含"为什么用"、"为什么不"、"怎么实现的"3 类）
- [`09-data-snapshot.md`](09-data-snapshot.md) —— 真实数据快照


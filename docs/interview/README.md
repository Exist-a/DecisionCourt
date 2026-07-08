# DecisionCourt · 面试目录（interview/）

> **本地专用 · 第一人称 · AI 全栈工程师向**
> **2026-07-02 建立**
> **作者视角**：我是 DecisionCourt 的设计者，本目录用于我**面试前 1 小时快速 reference**，以及**面试中遇到问题快速找答案**。

---

## 0. 我是谁（30 秒版）

后端工程师转型 AI 全栈，做了 DecisionCourt —— 一个"AI 庭审模拟系统"。**对外像产品**（用户提交事实、双方立场，看到判决），**对内是 AI Agent 编排系统**（5 个 Agent 角色 + 状态机 + Agent 间通信 + 信念引擎 + 装饰器链网关 + 白盒化）。**最大亮点是白盒化**——让 LLM 系统像传统后端一样可调试。

---

## 1. 5 分钟快速 navigation（按面试官常问顺序）

| 顺序 | 面试官问 | 跳到 |
|---|---|---|
| ① | "简单介绍下这个项目" | [`00-self-intro.md`](./00-self-intro.md) |
| ② | "整体架构怎么设计的" | [`01-architecture-mindmap.md`](./01-architecture-mindmap.md) |
| ③ | "为什么用 消息总线不直接调 LLM" |  |
| ④ | "信念引擎是什么" | [`03-belief-engine.md`](./03-belief-engine.md) |
| ⑤ | "Gateway 装饰器怎么做的" | [`04-agent-gateway-v2.md`](./04-agent-gateway-v2.md) |
| ⑥ | "白盒化具体怎么做的" | [`05-whitebox-observability.md`](./05-whitebox-observability.md) |
| ⑦ | "讲一个真实 bug 故事" | [`06-bug-stories.md`](./06-bug-stories.md) |
| ⑧ | "我不懂 XX 这个名词" | [`07-key-terms.md`](./07-key-terms.md) |
| ⑨ | "如果面试官问 XX 你怎么答" | [`08-faq-30-questions.md`](./08-faq-30-questions.md) |
| ⑩ | "给我看真实数据" | [`09-data-snapshot.md`](./09-data-snapshot.md) |
| ⑪ | "前端埋点 / 可观测性怎么延伸到前端" | [`10-frontend-analytics.md`](./10-frontend-analytics.md)（**v0.10 新增**） |
| ⑫ | "LLM 输出幻觉 / Prompt 不够 / 怎么防编造" | [`11-hallucination-validation.md`](./11-hallucination-validation.md)（**v0.10.1 新增**） |

---

## 2. 文档结构（12 章节 + data/）

```
interview/
├── README.md                       ← 你正在看
├── 00-self-intro.md                ← ① 自我介绍
├── 01-architecture-mindmap.md      ← ② 整体架构思想
├── 02-a2a-bus.md                   ← ③ 消息总线
├── 03-belief-engine.md             ← ④ v0.6 贝叶斯信念引擎
├── 04-agent-gateway-v2.md          ← ⑤ v0.7 Gateway 装饰器
├── 05-whitebox-observability.md     ← ⑥ v0.8 白盒化
├── 06-bug-stories.md               ← ⑦ 真实 bug 故事
├── 07-key-terms.md                 ← ⑧ 技术名词解释
├── 08-faq-30-questions.md          ← ⑨ 30 个面试问题
├── 09-data-snapshot.md             ← ⑩ 真实数据快照
├── 10-frontend-analytics.md        ← ⑪ v0.10 前端埋点（**2026-07-08 新增**）
└── data/                           ← 真实数据（从 /metrics / DB 导出）
    ├── metrics-snapshot.json
    ├── decision-events-snapshot.json
    ├── llm-calls-snapshot.json
    └── bug-fix-comparison.md
```

---

## 3. 写作风格（自约束）

| 项 | 标准 |
|---|---|
| **视角** | 第一人称（"我设计..."、"我学到..."） |
| **代码** | **能少则少**（重思想，重名词） |
| **重思想** | 多讲"为什么这么做"，少讲"具体实现" |
| **重名词** | 每个技术名词首次出现时给定义 |
| **【反思】** | 每章末尾有【反思】一节，讲"我从中学到什么 / 如果重来怎么改 / 面试被问怎么答" |
| **真实数据** | 引 [`data/`](./data/) 下的真实快照，不编造 |
| **公开 GitHub** | interview/ 入 GitHub（2026-07-02 决策修正） |

---

## 4. 5 大亮点（用这 5 个撑起整个面试）

| 亮点 | 章节 | 30 秒电梯版 |
|---|---|---|
| **消息总线** | §02 | Agent 间通信不走 prompt，走显式消息总线。3 种可见性（public/private/team）+ 落库审计 = 庭审可回放。 |
| **信念引擎 v0.6** | §03 | 不用 0-100 主观分，用**贝叶斯 log-odds** 数学严谨地表达"AI 法官的相信度"。weaken 边 + 锚定 + belief_diffs 审计 trail。 |
| **Gateway v2 装饰器** | §04 | 把"压缩 / 预算 / 限流 / 降级 / 审计"做成**装饰器链**，可插拔、可独立测。Smart Compression 关键消息不压缩。 |
| **白盒化 v0.8** | §05 | **AI 系统的可观测性** = 三大支柱（slog / metrics / decision_events）+ 端到端 trace_id 串联。**最强杀手锏**——让 AI 调试像传统后端一样。 |
| **前端埋点 v0.10** | §10 | **白盒化的横向延伸**——前端事件进同一张 `decision_events` 表，用 `fe.` 前缀区分。**跨源查询**：用户行为 + AI 决策一条 SQL join。8 个事件接入了 8 个实际用户操作。 |
| **幻觉硬验证 v0.10.1** | §11 | **Prompt 规则 + post-validation 双层防御**——LLM 在 stress 下违反 60% 规则，post-validation 修到 0%。OpenAI/Anthropic 同套路（schema 约束 + output 校验）。 |

---

## 5. 5 个真实 bug 故事（v0.8 + v0.8.3 demo 当天发现）

详见 [`06-bug-stories.md`](./06-bug-stories.md)：

| # | bug | 严重度 | 暴露 |
|---|---|---|---|
| 1 | llm_calls 外键约束失败 | 🔴 P1 | v0.8 demo 当天 stdout |
| 2 | SessionID fallback WARN | 🟡 P2 | v0.8 demo 当天 stdout |
| 3 | a2a_message_throughput 计数缺失 | 🟢 P3 | v0.8 demo /metrics |
| 4 | 信念轨迹只显示 1 条（应是 16 条） | 🟡 P2 | v0.8.3 真实庭审回归 |
| 5 | 判决书"AI 可视化"按钮无响应 | 🟢 P3 UX | v0.8.3 真实庭审回归 |

**核心启示**：
- "业务跑得欢 ≠ 系统健康"（bug 1 / bug 3）
- "意图与实现脱节 = bug 经典形态"（bug 1 / bug 5）
- **"数据层正确 ≠ 链路正确"**（bug 4，**最深刻**）
- "持续回归 → 持续暴露 → 持续修复"循环（白盒化 ROI）

---

## 6. 面试前 1 小时 checklist

- [ ] 通读 [`01-architecture-mindmap.md`](./01-architecture-mindmap.md) 一遍
- [ ] 把 **5 大亮点**的 30 秒电梯版背熟（§02 消息总线 / §03 信念 / §04 网关 / §05 白盒化 / **§10 前端埋点 / §11 幻觉硬验证**）
- [ ] **重点复习 bug 4**（`engine_v06.go` ID 错配）—— 最能体现"数据层正确 ≠ 链路正确"
- [ ] 翻 [`08-faq-30-questions.md`](./08-faq-30-questions.md) 30 个问题至少答出 25 个
- [ ] 看 [`09-data-snapshot.md`](./09-data-snapshot.md) —— 真实数据 + 单场庭审 4 evidence × 4 agent = 16 belief_diff 数字
- [ ] **如果被问"前端 / 可观测性 / 埋点"** —— 看 [`10-frontend-analytics.md`](./10-frontend-analytics.md)（v0.10，2026-07-08 新增）
- [ ] **如果被问"LLM 幻觉 / Prompt 不够 / 怎么防编造"** —— 看 [`11-hallucination-validation.md`](./11-hallucination-validation.md)（v0.10.1，2026-07-08 新增）
- [ ] 不背代码 —— 知道代码在哪就行
- [ ] 看 [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md) §11 面试故事模板（v0.8.3 版）

---

## 7. 5 大亮点（用这 5 个撑起整个面试）

| 亮点 | 章节 | 30 秒电梯版 |
|---|---|---|
| **消息总线** | §02 | Agent 间通信不走 prompt，走显式消息总线。3 种可见性（public/private/team）+ 落库审计 = 庭审可回放。 |
| **信念引擎 v0.6** | §03 | 不用 0-100 主观分，用**贝叶斯 log-odds** 数学严谨地表达"AI 法官的相信度"。weaken 边 + 锚定 + belief_diffs 审计 trail。 |
| **Gateway v2 装饰器** | §04 | 把"压缩 / 预算 / 限流 / 降级 / 审计"做成**装饰器链**，可插拔、可独立测。Smart Compression 关键消息不压缩。 |
| **白盒化 v0.8** | §05 | **AI 系统的可观测性** = 三大支柱（slog / metrics / decision_events）+ 端到端 trace_id 串联。**最强杀手锏**——让 AI 调试像传统后端一样。 |
| **前端埋点 v0.10** | §10 | **白盒化的横向延伸**——前端事件进同一张 `decision_events` 表，用 `fe.` 前缀区分前后端。**跨源查询**：用户行为 + AI 决策一条 SQL join。8 个事件接入了 8 个实际用户操作。 |
| **5 个 bug 故事 v0.8.3** | §06 | 白盒化第一天暴露 3 个 + 真实庭审回归又暴露 2 个，**没有一个被单元测试发现**。**最有说服力的"业务跑得欢 ≠ 系统健康"实证**。 |

---

## 8. 给面试官的 3 份文件

**面试官希望看技术细节时给他这 3 份**（不是第一人称的 interview/）：

- [`../architecture/link-overview.md`](../architecture/link-overview.md) — 完整链路（技术深度版）
- [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md) — 真实案例 v1.1（事实版，含 5 个 bug 故事）
- [`../adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md) — 白盒化 ADR（决策版）

**interview/ 是我自己用的"面试前查表"**。**也入 GitHub**（2026-07-02 决策）—— 因为内容质量高、有反思，但**不主动给面试官看**。

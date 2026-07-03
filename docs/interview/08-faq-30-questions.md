# 08 · 30 个面试高频问题（背诵版）

> **目标**：用 30 个面试高频问题 + 标准答案，**覆盖 80% 的 DecisionCourt 项目面试提问**。**每个答案 60-150 字**，背 30 个答案 = 30 分钟练习 + 面试前的查表。
> **配套**：[`04-agent-gateway-v2.md`](04-agent-gateway-v2.md) · [`05-whitebox-observability.md`](05-whitebox-observability.md) · [`06-bug-stories.md`](06-bug-stories.md) · [`07-key-terms.md`](07-key-terms.md)

---

## 0. 一句话总结

> 30 个高频问题按 5 大主题分类（项目 / 技术栈 / AI / 系统设计 / 行为面试），**每个答案用第一人称 + 业务驱动 + 真实案例**。背 30 个答案 = 面试前的"安全网"。

---

## 1. 项目相关（5 个）

### Q1: 介绍下你的项目？
> **DecisionCourt** 是一个**AI 庭审模拟系统**——多 Agent（控辩审调书 5 个角色）协作帮用户做"二选一"类型决策（买房 / 投资 / 离婚 / 学习...）。**核心价值**：让 AI 的决策过程**可解释 / 可审计 / 可追溯**。技术上：**Go backend + Next.js frontend + PostgreSQL + DeepSeek LLM + Bocha 搜索**。**v0.8 我刚完成完整白盒化系统**，让"业务跑得欢 ≠ 系统健康"的隐藏 bug 立刻显形。

### Q2: 这个项目最有技术亮点的是什么？
> **白盒化（observability）系统**——让 AI 系统"自己知道自己在干什么"。我做了完整实现：**slog JSON + 11 类业务指标 + 端到端 trace 串联 + decision_events 业务事件表**。**最有说服力的事实**：v0.8 + v0.8.3 一共暴露 **5 个隐藏 bug**（含 1 个"每层都对但链路 ID 错配"的隐蔽 bug），**没有一个被单元测试发现**。这才是 AI 系统区别于传统软件的核心——**没有白盒化 = 黑盒**。

### Q3: 这个项目最难的技术挑战是什么？
> **AI 系统的可解释性**。法庭场景下，"为什么判 A 不判 B"必须能查。我用**贝叶斯 log-odds** 把"相信度"变数学可解释：每条证据触发一次 Bayesian Update，写入 `belief_diffs` 审计表（含 `prior_logit` / `posterior_logit` / `delta` / `reason`）。**4 个 Agent × 每 evidence = 4 条 belief_diff**，**linear 可加 + 数学可审计**。

### Q4: 项目的下一个迭代会做什么？
> **v0.9+ 高可用 + 并发防护**。包括 4 个 ADR：
> - ADR 0011: Redis 分布式锁（用户点击幂等）
> - ADR 0012: WebSocket 分布式广播（多实例 backend）
> - ADR 0013: LLM 异步化 + 熔断降级
> - ADR 0014: 数据库主从 + 死锁检测
>
> 按优先级：白盒化已完成 → 高可用 → 商业化（接外部 Agent）。

### Q5: 你一个人做的吗？
> 我是**主开发者 + 架构师**。**AI 工具大量使用**（Claude / GPT / MCP）—— **但设计决策都是我做的**（ADR 形式记录），**代码部分 GitHub 上 commit 信息可考**（我做 commit message 习惯真实，谁写了什么、做了什么决策都有审计）。**项目迭代从 v0.5 到 v0.8，每个大版本都是 1-2 个月的密集开发**。

---

## 2. 技术栈相关（5 个）

### Q6: 为什么用 Go 不用 Python？
> **Go 的优势对 DecisionCourt 是决定性的**：
> - **goroutine 适合多 Agent 并发**——5 个 Agent 同时跑，Python GIL 是瓶颈
> - **typing 严格 + 编译期错误**——AI 项目里 LLM 输出是黑盒，但**代码必须严**，Go 的 typing + compile 检查帮我
> - **单 binary 部署简单**——`go build` 出 1 个 exe，docker compose 启动简单
> - **observability 生态好**——slog / prometheus client / otel 都有官方支持
>
> Python 适合**研究员 / 算法工程师快速验证**，DecisionCourt 是**生产级 AI 应用**——Go 更合适。

### Q7: 为什么用 PostgreSQL 不用 MongoDB？
> 业务数据**结构化 + 强关系**——10 张表都有明确外键约束（`fk_court_sessions_llm_calls` 等）。**Bug 故事**：v0.8 当天，正是 PostgreSQL 的**外键约束**让我立刻发现 `llm_calls` 写入失败的 bug——MongoDB 没外键，**这种 bug 会悄悄发生**。**关系型数据库 + 外键 = 业务约束的工程化**。**JSONB 也保留灵活性**——`decision_events.payload` / `verdicts.consensus_points` 都是 jsonb。

### Q8: 为什么用 Next.js 不用 Vue / React 纯写？
> **Next.js = 路由 + SSR + API proxy** 一站式：
> - **路由**：`app/court/[id]/page.tsx` 动态路由天然
> - **API proxy**：本来想做 next.config.js rewrites 反代到 backend，但 v0.8 用环境变量更简单
> - **React 生态**：Mantine UI / shadcn-ui / framer-motion 都有现成组件
>
> **实际选择原因**：项目开始时 v0.5，那时 Next.js 14 刚发布，App Router + Server Components 概念符合我"组件化前端"的想法。**Vue 不是不能做，是 React 生态更适合 AI 项目的复杂状态**（庭审要实时推 WS、belief trajectory、记忆可视化等，React + Zustand 比 Vuex 顺手）。

### Q9: 为什么不用 LangChain / LlamaIndex？
> **明确理由**（3 个）：
> 1. **抽象不对齐**——我的"5 个 Agent 庭审"场景不需要它们的 Chain / Tool / Memory 抽象，**反而要把我的设计"翻译"成它们的语言**
> 2. **debug 难**——LangChain 50 万行代码，**任何隐藏行为我都讲不清**
> 3. **学习曲线 vs 收益**——我自己写的 Agent Gateway v2 是 **250 行 Go + 5 个装饰器**，**每个我能讲清**。LangChain 替代方案需要 2000+ 行集成代码 + 大量"魔法"配置
>
> **更大的问题**：业内很多 LangChain 项目**无法自定义**——需要绕弯子。我的需求"5 Agent 不同人格 + 不同工具 + 自己的记忆系统"用 LangChain 反而难写。**技术选型 = 业务需求 驱动，不是技术崇拜**。

### Q10: 为什么 v0.8 才做 observability，不一开始就做？
> **业内经验**：很多项目 L0 一上来接 Prometheus + Jaeger + ELK，**搭监控数周**。**MVP 阶段百级用户做 L3 是过度工程化**。**我的策略**：**L2 完整 + L3 接口预留**——业务跑得欢 + 系统可查，未来不返工。
>
> **教训**：**白盒化的成本不是"做"，是"持续投入"**——每加新功能要补埋点。L3 提前做 = 高额运营成本。**Phase A 数据采集后再决策**（按 [`whitebox-roadmap.md`](../roadmap/whitebox-roadmap.md)）。

---

## 3. AI / Agent 相关（8 个）

### Q11: 怎么做的 Multi-Agent 通信？
> **in-process 消息总线模式**——5 个 Agent 通过 Bus 通信。**3 个关键设计**：
> 1. **3 种可见性**（public / private / team_only）——控方草稿不能泄露给对方
> 2. **必须落 `a2a_messages` 表**——庭审可回放
> 3. **跨 Agent 状态同步**——状态机迁移是 5 个 Agent 共同感知的事实
>
> **业内对比**：不用 function call 直接调来调去，因为有**可见性 + 审计 + 跨 Agent 同步** 3 个硬需求，消息总线 ROI 为正。**详见 [`01-architecture-mindmap.md`](01-architecture-mindmap.md) §3 哲学 2（消息总线 = 单一事实源）**。

### Q12: 为什么用 Bayesian log-odds 不用 LLM 直接打分？
> **2 个核心原因**：
> 1. **可解释性**——LLM 打 60→70，**为什么是 70 不是 64**？答不上。贝叶斯 logit：每次更新**数学明确**（`new_logit = old_logit + evidence_logit`），可审计
> 2. **法庭场景的硬需求**——高 stakes 决策（推荐买房 / 投资 / 离婚）必须能"为何推荐 A 推 B"答出来
>
> **业内对比**：LLM 评分常见做法（一个 prompt 直出 0-100 分）是**主观 + 不可回放 + 不可审计**。贝叶斯是**数学 + 可解释 + 可回放 + 可审计**。**详见 [`03-belief-engine.md`](03-belief-engine.md) §1**。

### Q13: 怎么避免 AI 幻觉？
> 4 个机制：
> 1. **Belief Engine 数学约束**——LLM 不能"凭空"写判决书，必须基于 `belief_diffs` 表算出来的平均信念
> 2. **私有策略笔记审计**——LLM 写"我刚才为什么这么回答"必须入库，可查
> 3. **庭审回放**——LLM 输出 + 完整 prompt 都留 `llm_calls` 表，幻觉出现时能定位
> 4. **审计 trail**——`a2a_messages` + `decision_events` 让每一步决策都可追溯
>
> **不幻想 0 幻觉**——LLM 总会有 hallucination，**关键是"出现幻觉时能快速发现 + 修复"**。

### Q14: 怎么保证 5 个 Agent 不互相对抗失控？
> **收敛机制**——3 个信号 OR 逻辑：
> 1. **variance 阈值**（4 Agent 信念方差 < 0.01）= 高度一致
> 2. **delta 阈值**（双方 logit 差异 > 2.0）= 一面倒
> 3. **时间窗口**（≥ 2 轮没新影响）= 停滞
>
> 任一信号触发即收敛。**详见 [`03-belief-engine.md`](03-belief-engine.md) §3**。**为什么不无限循环**：强制收敛保护用户体验（不能让庭审无限跑下去），但**收敛原因必须可解释**（写入 `decision_events`）。

### Q15: ReAct 循环怎么避免死循环？
> **max_step 上限** —— Agent ReAct 循环最多跑 N 步（默认 8）。**超过 = 强制走 safe path**（"已尽我所能，下面是 summary"）。**业内经典做法**：超时 + 步数限制 + 显式 fallback（不静默）。

### Q16: DeepSeek / Claude 你怎么选？
> **fallback 链**：deepseek → openai → ollama → mock，**按成本 / 性能 trade-off 切换**。**当前默认 deepseek** 因为中文 + 价格 + 我个人 DeepSeek 账号有 credit。
>
> **架构优势**：换 LLM provider **只改 1 个配置**，不改业务代码（v0.7 Gateway 设计）。

### Q17: 怎么处理 token 成本？
> **3 个机制**（v0.7 Gateway + v0.8 白盒化）：
> 1. **Smart Compression** —— 保留 system prompt + 角色历史 + 最近 3 轮，压缩重复信息
> 2. **Budget 装饰器** —— 单 session 20000 token 上限
> 3. **白盒化审计** —— `llm_calls` 表 47 字段（含 prompt_tokens / completion_tokens / cost_usd）
>
> **业务价值**：白盒化后我发现**之前审计表 0 行**（bug 1）。**修了一行 + 4 项测试后** —— 现在每个庭审花了多少 token / 多少成本**实时可查**。

### Q18: AI 系统最让你头疼的是什么？
> **可解释性 + 调试**。LLM 是黑盒 + 业务复杂 + 实时交互 + 多个 Agent。**没白盒化 = 一旦出问题靠"猜"**。
>
> **v0.8 解决了**——v0.8.3 那个"信念只显示 1 条"的 bug，我 30 分钟定位（数据层 + API + WS + 前端逐一自洽），5 行代码修复。**白盒化之前我可能要查半天**——前端 render bug？API 错？DB 没写对？每个都要查。**白盒化让"对/错"清单化**。

---

## 4. 系统设计相关（7 个）

### Q19: 怎么设计高可用？
> **v0.9+ ADR 0011-0014 计划**：
> 1. **Redis 分布式锁**（同一 session 互斥 + 用户点击幂等 + LLM 幂等）
> 2. **WebSocket 分布式广播**（多实例 backend + Redis Pub/Sub）
> 3. **LLM 异步化 + 熔断降级**（限流 + 熔断 + fallback）
> 4. **数据库主从 + LLM 死锁检测**（read replica + watchdog + dead letter）
>
> **核心原则**：**外部依赖 = 分布式系统的外部节点**，**必须 Gateway 化**（v0.7 已是）+ **State 共享化**（v0.9 Redis 化）。

### Q20: 怎么设计可观测性？
> **v0.8 L2 完整**：slog + 11 类业务指标 + trace + decision_events + 4 sink 设计。
> **关键决策**：
> 1. **业务级指标**（不是 CPU/内存，是"每场庭审花了多少 token"）
> 2. **cardinality 控制**（label 用有限集合）
> 3. **业务事件独立落库**（区别于普通日志，schema 固定 + 长期保留）
> 4. **接口预留**（`Metrics` / `Span` interface 未来切 OTel 不改业务）
>
> **业内 trade-off**：不上 L3（Prometheus）因为 MVP 阶段用户量小。**详见 [`05-whitebox-observability.md`](05-whitebox-observability.md) §6 防质疑思考**。

### Q21: 状态机怎么设计？
> **5 阶段 × 多 round × 5 Agent**：
> ```
> idle → opening → cross_exam (N round) → closing → deliberation
> ```
> **关键设计**：
> 1. **transition 函数 = 唯一入口** —— `transitionPhase(from, to)` 集中所有状态迁移逻辑
> 2. **不允许跳级** —— 必须 opening → cross_exam，不能直接 cross_exam → deliberation
> 3. **每迁移 = 1 条 decision_events 行 + 1 个 metric count + 1 条 slog** —— 这是 v0.8 白盒化的"钩子点"
>
> **业内对比**：不用数据库外键约束 transition（太严格，调试难），用代码层 transition 函数 + 测试覆盖。

### Q22: 数据库 schema 怎么设计的？
> **10 张表 + 3 大约束**：
> 1. **关系性优先** —— 外键约束（v0.8 bug 1 是外键失败的暴露，靠它立刻发现）
> 2. **JSONB 保留灵活性** —— `decision_events.payload` / `verdicts.consensus_points`
> 3. **审计 trail 独立** —— belief_diffs / a2a_messages / decision_events 都是 audit trail 表
>
> **反范式设计**：`belief_diffs` 的 `prior_logit` / `posterior_logit` 是反范式（可计算但存表），**这是为了让审计行可自包含**，不需要 JOIN agents 表。

### Q23: WebSocket 推送怎么设计？
> **Bus → hub.Broadcast() → 各订阅者**。**3 个可靠性考量**：
> 1. **流式 chunk throttle** —— 30ms 一个 chunk，给前端"打字机"效果
> 2. **消息不丢失** —— 前端 reconnect 时根据 last received id 重发（v0.6 实现）
> 3. **多实例后** —— v0.9+ 接 Redis Pub/Sub（避免每个实例只发给本实例订阅者）

### Q24: 流式响应怎么处理？
> **30ms 节流 chunk** —— `chunkCb` 回调每 30ms 触发一次，hub.Broadcast 推送 `agent.speak_chunk` 事件，**前端 React useState 累积 + 渲染**。**好处**：用户体验真的"打字机"效果，不是"等 8 秒后整段出现"。**这是 DecisionCourt UX 核心**。

### Q25: 怎么保证庭审可重放？
> **3 个 audit trail 表 = 完整重放能力**：
> 1. **decision_events** —— 业务事件（状态迁移 + 业务级 span）
> 2. **a2a_messages** —— Agent 通信
> 3. **llm_calls** —— LLM 调用
> 4. **belief_diffs** —— 信念变化
>
> **回放流程**：根据 session_uuid JOIN 4 张表，按 created_at 排序，**用 `verdicts` 表的 verdict 反向重建 prompt → 用 LLM 重放（可选）**。**完整 implementation 见 v0.5+ ADR**。

---

## 5. 行为面试（5 个）

### Q26: 你最有挑战性的技术决策？
> **选 Go 不用 Python 写 LLM 应用**。当时公司内部很多人 push 用 Python（LangChain / LlamaIndex 生态）。我的判断：**MVP 是生产级应用，不是研究项目**——并发性能 + 类型严格 + 部署简单 + 调试友好 = Go 更合适。**1 年后回看**，这个决策让我后续 v0.7 Gateway + v0.8 observability 都轻松实现。**反之如果用 Python + LangChain，bug 4（链路 ID 错配）我可能根本发现不了**（LangChain 抽象掉太多 trace 细节）。

### Q27: 你最自豪的技术实现？
> **白盒化系统让 5 个隐藏 bug 显形**。**最有说服力的是 bug 4**：4 个 evidence 应该是 16 条 belief_diff，但用户只看到 1 条。**可怕的真相**：数据库 + API + 后端 + 前端**每一层单独看都对**，但数据在某个中间环节丢了。**根因是 engine_v06.go 创建 belief_diff 时未显式分配 ID**，**修复 = 加一行 `uuid.New()`**。
>
> **核心意义**：**白盒化让我从"调试 LLM 黑盒应用"变成"调试一个完全可观测的系统"**。**这是 AI 应用最缺的能力**。

### Q28: 团队合作中你怎么贡献？
> **2 个具体例子**：
> 1. **AGENTS.md §8 敏感文件红线** —— 我把"我犯过的 .env 错"变成团队的 agent 行为规范（2026-07-02 自查）
> 2. **ADR 形式记录所有架构决策** —— 让团队新人能 30 分钟看懂项目演进（11 份 ADR）
>
> **我的合作哲学**：**用文档代替会议**。**写下来的决策不依赖人脑传话**。这是"远程 + AI 工具"团队协作的必备能力。

### Q29: 你最大的弱点？
> **过度追求技术深度有时会 YAGNI 边界模糊**。**实例**：v0.8 我建议用 30 分钟就上 Prometheus，agent 建议先观察。**我接受**——但我留下了 Metrics interface 预留，**未来需要 1 天接 Prometheus 不改业务**。
>
> **改进方法**：**用业务价值衡量技术深度**。**业务价值 = 痛点 × 频率 × 影响范围**。**如果痛点是"几乎不会发生"或"影响范围很小"，技术深度可让步**。

### Q30: 你未来 1 年的规划？
> **DecisionCourt 商业化**：
> - **v0.9** 接外部 Agent（如有需求评估接 Google A2A 协议，当前 YAGNI）
> - **v1.0** 接 Prometheus + Grafana
> - **数据仓库** —— decision_events 入 ClickHouse 做 BI
>
> **个人成长**：
> - **继续在 AI 系统可观测性领域深耕**（业内还没形成共识，这是机会）
> - **接外部开源** —— 消息总线模式 + Bayesian Belief Engine 抽象出来可独立库
> - **写技术博客** —— "AI 系统白盒化" 系列，复盘 5 个 bug 故事

---

## 6. 反问环节（8 个经典反问）

### 你最希望了解公司 / 团队的什么？精选 8 个：

1. **"AI 工程团队目前最大的技术痛点是什么？"**（展示你关心实际工程问题）
2. **"你们怎么看待 LLM 应用的 ROI？"**（聊商业意识）
3. **"团队现在的代码 review / ADR 文化是怎样的？"**（聊工程化）
4. **"公司未来 1 年的 AI 战略？"**（聊战略视野）
5. **"你们的 observability 链路是怎样的？我看到用了什么工具？"**（聊特定技术）
6. **"v0.8 我做白盒化时发现 5 个 bug，你们历史上踩过哪些大坑？"**（聊实战经验）
7. **"如果你做这个项目，你会怎么设计 Multi-Agent？"**（聊设计思维）
8. **"团队里 junior engineer 成长路径？"**（聊职业发展）

**反问 = 面试最后的杀手锏**。**好的反问 = 把我不会的领域信号发给面试官**。

---

## 7. 30 题快速速记（cheat sheet）

| # | 主题 | 关键词 |
|---|---|---|
| 1 | 项目 | AI 庭审 / 5 Agent / 白盒化 |
| 2 | 亮点 | 5 个隐藏 bug / sneaky production bug |
| 3 | 难点 | AI 可解释 / Bayesian log-odds |
| 4 | 迭代 | 高可用 + 并发防护 |
| 5 | 团队 | 主开发 + AI 工具 |
| 6 | Go vs Python | 并发 / 类型 / observability |
| 7 | Postgres vs Mongo | 外键 = bug 立刻发现 |
| 8 | Next.js | 路由 + SSR + API proxy |
| 9 | LangChain 不用 | 抽象不对齐 / debug 难 |
| 10 | 白盒化时机 | L2 完整 / 避免过度 |
| 11 | Agent 间通信 | 总线 / 可见性 / 审计 |
| 12 | Bayesian vs LLM 评分 | 可解释 / 可审计 |
| 13 | AI 幻觉 | 4 个机制 / 不幻想 0 幻觉 |
| 14 | 收敛机制 | 3 信号 OR |
| 15 | ReAct 死循环 | max_step + safe path |
| 16 | LLM 选型 | fallback 链 |
| 17 | token 成本 | Compression + Budget + Audit |
| 18 | 头疼的事 | AI debug = 没白盒化靠猜 |
| 19 | 高可用 | ADR 0011-0014 |
| 20 | observability 设计 | 业务级指标 / cardinality |
| 21 | 状态机 | 单一入口 / transitionPhase |
| 22 | schema | 关系优先 + JSONB 弹性 + audit trail |
| 23 | WebSocket | Bus + Redis Pub/Sub（v0.9+） |
| 24 | 流式 | 30ms 节流 chunk |
| 25 | 可重放 | 4 audit trail 表 |
| 26 | 选 Go 不用 Python | 1 年后回看正确 |
| 27 | 最自豪 | bug 4 = AI 系统白盒化 |
| 28 | 团队合作 | AGENTS.md + ADR |
| 29 | 弱点 | YAGNI 边界模糊 |
| 30 | 未来规划 | 商业化 + 数据仓库 |

---

## 8. 面试前的准备策略

| 时间 | 任务 |
|---|---|
| **T-7 天** | 通读 [README.md](README.md) + [01-architecture-mindmap.md](01-architecture-mindmap.md) + [link-overview.md](../architecture/link-overview.md) |
| **T-3 天** | 细读 [05-whitebox-observability.md](05-whitebox-observability.md) + [06-bug-stories.md](06-bug-stories.md)（杀手锏） |
| **T-1 天** | 背 30 个 Q 的关键词（§7 cheat sheet） |
| **T-1 小时** | 扫 [07-key-terms.md](07-key-terms.md)（名词置信） |
| **面试前 5 分钟** | 默念：[README.md](README.md) §0 + [case-study](../observability/case-study-2026-07-02.md) §11 面试故事 |

---

## 9. 名词速查

| 名词 | 含义 |
|---|---|
| Cheat sheet | 速查表 / 提纲 |
| Replay attack | 重放攻击（这里指庭审回放） |
| Cardinality | metric label 组合数 |
| DRY / KISS / YAGNI | 工程原则 |
| L4 / L3 / L2 / L1 | observability 5 级模型 |
| ADR | Architecture Decision Record |

---

**下一步**：
- [`09-data-snapshot.md`](09-data-snapshot.md) —— 项目真实数据快照（让数据替你说话）
- 最后看 [README.md](README.md) 速查表


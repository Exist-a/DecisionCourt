# 02 · A2A 消息总线（防堆砌质疑 + 真正的 Google A2A 协议规划）

> 面试高频问题：
> ① "A2A 是不是 Google 那个跨组织协议？你这单进程 5 个 Agent 用 A2A 是不是堆砌技术？"
> ② "你这个项目能用真正的 Google A2A 协议吗？"
> **本节同时回答这两个问题。**

---

## 0. 我的 A2A 三层定义（必背）

为了避免混乱，我在面试前先给"A2A"这个词做 3 层定义：

| 层级 | 含义 | 适用范围 | 我的项目状态 |
|---|---|---|---|
| **L1 思想层** | "Agent 间通信应该是 first-class 概念，不藏在 prompt 拼接里" | 所有多 Agent 系统 | ✅ **我做了** |
| **L2 模式层** | in-process event bus（消息总线模式） | 单进程 / 单后端 | ✅ **我做了**（v0.5+） |
| **L3 协议层** | **Google A2A 协议**（2025）：JSON-RPC over HTTP + Agent Card + Task / Message / Artifact + Discovery | 跨组织 / 跨厂商 | ⏳ **v0.9+ 计划**（接外部 Agent） |

**我的项目**：
- **5 个内部 Agent** = L2（消息总线模式）
- **外部 Agent 接入** = L3（v0.9+ 用 Google A2A 协议）

---

## 1. 我设计的 A2A 总线（5 个内部 Agent 用）

### 1.1 3 个核心业务需求（**为什么用消息总线**）

| 需求 | 如果不用消息总线会怎样 |
|---|---|
| **可见性隔离**（public / private / team_only） | 每个调用点都要写 if visibility 判逻辑，**重复代码 + 容易漏** |
| **落库审计**（所有 A2A 消息必须入 `a2a_messages` 表） | 每个 Agent 都要自己写审计，**漏一份就破案** |
| **跨 Agent 状态同步**（状态机迁移是 5 Agent 共同感知事实） | 单变量传递会出现"接收方还没更新"的 race condition |

**这 3 个需求 = 消息总线的经典用例**。

### 1.2 消息总线模式（messaging pattern）

用了 40 年的成熟模式：
- **企业服务总线**（ESB）：1990s+
- **消息队列**（MQ）：1970s+
- **Actor 模型 mailbox**：1973+
- **DDD 领域事件**：2004+

**消息总线不是堆砌技术，是软件工程基本功**。

### 1.3 3 种可见性（**业务硬需求**）

| 可见性 | 含义 | 举例 |
|---|---|---|
| `public` | 双方都看得到 | 控方陈述、辩方反驳 |
| `team_only` | 同方可见 | 控方团队内部讨论 |
| `private` | 仅 agent 自己（不广播） | 控方草稿笔记、反思 |

**`private` 的关键设计**：**不广播**给其他 Agent，**但仍然落库**（用于审计 + 庭审回放）。**不广播 ≠ 不落库**。

---

## 2. 防堆砌质疑（**面试官最可能问的**）

### 2.1 面试官问的原文（推演）

> "你说的 A2A，是不是 Google 2025 年提的那个 Agent2Agent 协议？用于跨组织 / 跨架构的 Agent 通信。但你这个项目是**同一个后端 5 个 Agent**（同一 Go 进程、同一架构），用 A2A 是不是**为用而用 / 堆砌技术**？"

### 2.2 我的回答（90 秒版）

> "问得好。**A2A 这个词现在确实撞车了**——
>
> 1. **Google A2A 协议**（2025）：跨组织 / 跨架构 / 跨厂商的 Agent 通信标准，走 JSON-RPC over HTTP，带 auth / capability discovery / agent card。这是**协议层**。
>
> 2. **我项目里的 A2A**：单进程内 5 个 Agent 之间的消息总线，**本质是 in-process event bus**（进程内事件总线）。这是**架构模式层**。
>
> 我用的是后者。**名字撞车是历史偶然**（v0.5 设计时我没想到 Google 之后会提同名协议），但本质是"消息总线模式"——这是软件工程里**用了 40 年的成熟模式**（参考企业服务总线 ESB / 消息队列 MQ / Actor 模型 mailbox）。
>
> **为什么我不用 function call 调来调去**？因为我有 3 个**业务硬需求**：
>
> - **可见性隔离** —— 控方草稿笔记**不能**泄露给对方
> - **落库审计** —— 庭审要可回放
> - **跨 Agent 状态同步** —— 状态机迁移是 5 Agent 共同感知
>
> 这 3 个需求**没消息总线就很丑**。
>
> **类比**：
>
> - OS 同一台机器的两个进程通信用消息队列 —— **不会因为"在同一台机器"就不用消息队列**
> - 同一后端的 5 个微服务通信用消息总线 —— **不会因为"同进程"就不用总线**
>
> 所以**不是堆砌技术，是业务驱动的架构选择**。"

### 2.3 核心论点（**背 3 句话就够**）

1. **"A2A" 撞车了** —— 我用的是 in-process event bus（消息总线模式），不是 Google A2A 协议
2. **业务硬需求驱动** —— 可见性隔离 + 落库审计 + 跨 Agent 状态同步，没消息总线就重复代码
3. **成熟模式** —— 消息总线用了 40 年（ESB / MQ / Actor mailbox），不是我发明的，也没为用而用

### 2.4 如果面试官追问"为什么不直接 function call"

> "如果 Agent 数量 = 1，直接 LLM call。如果 Agent 数量 = 2-3 且无业务隔离需求，function call 可以。
>
> **一旦有 3 个需求之一**（可见性 / 审计 / 跨 Agent 同步），消息总线的 ROI 立刻为正。
>
> DecisionCourt 有 5 个 Agent + 3 种可见性 + 庭审回放需求 = 必须用消息总线。"

### 2.5 如果面试官追问"那 Google A2A 协议你了解吗"

> "了解。Google A2A 是 2025 年 4 月提的协议标准，**解决跨厂商 Agent 互操作**，用 JSON-RPC over HTTP + agent card（能力描述）。
>
> 我项目里的 A2A 是**单进程总线**，**跟它完全不是一回事**。但**思想有共鸣**——都是"Agent 通信应该是 first-class 概念，不应该藏在 prompt 拼接里"。
>
> 如果未来 DecisionCourt 要支持"跨厂商协作"（比如接入外部法律 AI），我会评估接 Google A2A 协议。**现在不接**是 YAGNI。"

---

## 3. 关键决策：项目到底能不能用真正的 A2A 协议？

> **用户问题（2026-07-02）**：
> "我们需要使用 A2A（谷歌的那个），因为有一个简历交上去了，我们需要自圆其说。这个项目是不能用真正的 A2A 吗？"

### 3.1 直接回答

**能，但要分情况。**

| 方案 | 做什么 | 工程量 | 简历能怎么写 |
|---|---|---|---|
| **A. 协议抽象层** | 5 个 Agent 都加 A2A 风格的 agent-card.json + JSON-RPC 风格消息；底座还是 in-process | 1-2 天 | "基于 A2A 协议规范设计 Agent 接口" |
| **B. 真正 A2A 协议**（**推荐**） | 保留 5 个内部 Agent 走 in-process（性能好）；**新增"外部 A2A Agent 接入层"**（v0.9+ 计划） | 2-3 天 | "基于 Google A2A 协议设计 Agent 互操作（内部 in-process + 外部 A2A 协议）" |
| **C. 全微服务** | 5 个 Agent 拆成 5 个独立 HTTP 服务，用 A2A SDK 通信 | 2-3 周 | "基于 Google A2A 协议实现多 Agent 协作" |

### 3.2 我选 **方案 B**（最务实 + 真正用 A2A 协议）

**核心理由**：
- **工程量可控**（2-3 天）—— 主要是加"外部 Agent 接入层"
- **真用 A2A 协议** —— 简历能写"基于 Google A2A 协议"，被追问能 show 出代码
- **保留性能优势** —— 5 个内部 Agent 仍走 in-process（不破坏 v0.8 白盒化等已实装模块）
- **业务合理** —— "接外部法律 AI 顾问"是真实业务场景（v0.9+ 商业化阶段需要）

### 3.3 方案 B 的架构图（**v0.8.2 已实装** ✅）

```
                 ┌─────────────────────────────┐
                 │  A2A 协议外部接入层（v0.8.2 实装）│
                 │  - /.well-known/agent-card.json  │
                 │  - /a2a/agents/:type/agent-card  │
                 │  - /a2a/tasks/send (202)         │
                 │  - bridge → 内部 Bus.Send        │
                 └─────────────────┬────────────┘
                                  │
        ┌─────────────────────────┴─────────────────────┐
        │         现有 in-process A2A 总线（保留）        │
        │                                                 │
        │  5 个内部 Agent:                                │
        │  - Prosecutor / Defender / Judge                │
        │  - Investigator / Clerk                         │
        │  - 通过 Bus.Send / Bus.Send 通信                 │
        │  - 3 种可见性 + 落库审计（v0.5+ 已有）            │
        └─────────────────────────────────────────────────┘
```

### 3.4 v0.8.2 已实装模块

| 模块 | 状态 | 代码 |
|---|---|---|
| `internal/a2a/external/agent_card.go` | ✅ 已实装 | L3 协议层 |
| `internal/a2a/external/server.go` | ✅ 已实装 | L3 协议层 |
| `internal/a2a/external/bridge.go` | ✅ 已实装 | L2-L3 桥接 |
| `internal/a2a/external/embed.go` | ✅ 已实装 | L3 配置层（//go:embed 编译时嵌入） |
| `internal/a2a/external/agent_cards/*.json` | ✅ 已实装 5 个 | L3 协议层 |
| 13 项单元测试 | ✅ 全部 PASS | |
| JSON-RPC 2.0 消息格式 | ✅ | L3 协议层 |
| 完整 task 端点（streaming） | ⏳ v0.9+ 商业化 | |

### 3.5 业务场景示例（v0.9+ 商业化）

- 接入"外部法律 AI 顾问"（Claude 法律专精 / Kimi 长文）
- 接入"跨组织法律 Agent"（企业内 / 律所间）
- 接入"标准化 Agent 市场"（未来 A2A Agent 注册中心）

### 3.6 自圆其说的简历话术

| 写法 | 风险 |
|---|---|
| ❌ "基于 Google A2A 协议实现多 Agent 协作" | **太满** — 实际只有外部接入用 A2A，内部是 in-process |
| ✅ "**基于 Google A2A 协议（2025）设计 Agent 互操作** —— 内部用 in-process 消息总线（性能优先），**外部接入层遵循 A2A 协议标准**（JSON-RPC + Agent Card）" | **诚实 + 具体** |

### 3.7 方案 B 的实施时间表

| 阶段 | 时间 | 工作量 |
|---|---|---|
| **调研** | 0.5 天 | 读 Google A2A 协议规范 + 找 1 个开源实现（a2a-python / a2a-go） |
| **设计** | 0.5 天 | 设计 agent-card + JSON-RPC 端点 + 外部→内部消息转换 |
| **实装** | 1-1.5 天 | 写 server.go / jsonrpc.go / bridge.go + 5 项单元测试 + 1 项集成测试 |
| **文档** | 0.5 天 | docs/a2a/a2a-external-integration.md + ADR |
| **总计** | **2-3 天** | 1 个 PR |

---

## 4. A2A 总线内部结构（少代码，重思想）

### 4.1 消息结构（A2A 风格，但不强制 JSON-RPC）

```go
// 内部 A2A 消息
type Message struct {
    ID           uuid.UUID
    MessageUUID  string         // 业务唯一 key
    SessionID    uuid.UUID      // DB 主键
    SessionUUID  string         // 业务 key（v0.5 修复：必须跟 hub room key 一致）
    From         string         // sender agent type
    To           string         // receiver agent type
    MessageType  MessageType    // speech / strategy_note / dispatch / report
    Visibility   Visibility     // public / private / team_only
    Payload      map[string]interface{}
    Phase        string
    Round        int
    CreatedAt    time.Time
}
```

**关键点**：`SessionID`（DB 主键）和 `SessionUUID`（业务 key）**严格区分**。v0.5 修复过 WS 房间钥匙 bug —— 之前用 `SessionID.String()` 当 hub room key，跟 session_uuid 不一致，导致消息进错房间。

### 4.2 4 种 MessageType（v0.5 private 通道）

| MessageType | 可见性 | 用途 |
|---|---|---|
| `speech` | public | 控辩方发言（双方都看） |
| `dispatch` | private | 控方 → 调查员（私有） |
| `report` | private | 调查员 → 控方（私有） |
| `strategy_note` | private | 控方自己的草稿笔记（**完全不广播**） |
| `opponent_weakness` | private | 记录对方弱点 |
| `self_correction` | private | 自我反思 |
| `evidence_eval` | private | 证据评估 |

**4 种 private 类型的业务价值**：让 AI 庭审有"内心戏"，**对外是辩论，对内是策略**。前端 MemoryAuditPanel 显示这 4 种类型的 timeline。

### 4.3 总线操作（伪代码）

```
Bus.Publish(msg):
    1. 写库 a2a_messages 表（无论可见性都落库）
    2. broadcast(msg.SessionUUID, "a2a.message", payload)
       - if visibility == public: 广播给 hub
       - if visibility == private: 不广播（但已落库）
       - if visibility == team_only: 广播给同方订阅者
    3. metric: a2a_message_throughput_total{event_type=...}++
```

**关键设计**：**写库和广播分离**。这样能保证：
- 落库永远成功（即使广播失败）
- 广播失败不影响审计 trail
- 庭审回放可重放（从 `a2a_messages` 表重读）

---

## 5. 关键名词（首次出现给定义）

| 名词 | 1 句话 |
|---|---|
| **A2A** | Agent-to-Agent。**3 层定义**：思想层 / 模式层 / 协议层。**避免名字撞车**。 |
| **Google A2A 协议** | 2025 年 4 月 Google 提的协议标准。**跨组织** / **跨厂商** / **跨框架**的 Agent 通信。 |
| **JSON-RPC** | 一种基于 JSON 的远程过程调用协议。A2A 协议用它做消息格式。 |
| **Agent Card** | A2A 协议里 Agent 的"身份证"。JSON 文件，描述 Agent 能力、URL、auth 信息。 |
| **Task** | A2A 协议里 Agent 协作的基本单位。一个 Task 可能产生多个 Message 和 Artifact。 |
| **Artifact** | A2A 协议里 Agent 产出的工件（如报告、文件、结构化数据）。 |
| **Visibility** | 消息可见性枚举。public / private / team_only。 |
| **message_type** | 消息类型枚举。speech / strategy_note / dispatch / report / 4 种 private。 |
| **session_uuid** | 业务 key（公开）。WebSocket 房间 key。 |
| **SessionID** | DB 主键（内部）。FK 指向它。 |
| **Actor 模型** | 一种并发计算模型。Agent 间的 mailbox 就是消息总线。 |
| **ESB** | Enterprise Service Bus。企业服务总线。1990s+ 模式。 |
| **消息总线模式** | messaging pattern。软件工程用了 40 年的成熟模式。 |

---

## 【反思】

### 关于"防堆砌质疑"的 3 个我自己的总结

1. **被质疑时不要辩护，要承认 + 重新框定**。"是的，名字撞车了" — 这种开场比"不是的，你误解了" 让面试官觉得你诚实 + 懂行。然后**重新定义术语**（消息总线模式 vs Google A2A 协议），把讨论拉到你能赢的地盘。

2. **业务硬需求 = 最好的辩护**。"我不用 Y 因为没用，我用 X 是因为业务需要 ABC" — 这个框架可以**回答 80% 的"为什么用 / 不用某技术"问题**。背这个框架。

3. **承认"如果 X 则不必要"**。"如果 Agent 数量 1-2 我不会用" — 这种主动设限让面试官觉得你**不是技术驱动而是业务驱动**。**技术驱动 = 堆砌；业务驱动 = 架构**。

### 关于"项目能不能用真正 A2A"的 3 个我自己的总结

1. **简历已经写了 A2A → 战略已定，自圆其说**。这时候**不是问"要不要用 A2A"，而是问"怎么用 A2A 才能既诚实又有亮点"**。方案 B（内部 in-process + 外部 A2A 协议）就是"既诚实又有亮点"的答案。

2. **"用什么协议"和"用什么模式"是两件事**。我内部用消息总线模式（messaging pattern），外部接 A2A 协议（protocol）。**模式 = 内部架构选择；协议 = 外部接口标准**。**两者不冲突**。

3. **工程量 vs 简历亮点的权衡**：
   - 方案 A（1-2 天）= 简历亮点弱
   - 方案 B（2-3 天）= **简历亮点强 + 真正用 A2A** ← 选这个
   - 方案 C（2-3 周）= 简历亮点最强，但破坏现有架构（拆微服务），**得不偿失**

### 关于"工程哲学"的总结

**软件工程的真相**：
- **没有"最好"的技术，只有"最适合业务"的技术**
- **工程量是约束条件，不是次要考虑**
- **"分阶段实装"是常态** —— 不要追求"一步到位"
- **外部接口标准（协议）vs 内部实现（模式）= 分开考虑** —— 这是"分层架构"的精髓

---

**配套**：[`01-architecture-mindmap.md`](./01-architecture-mindmap.md) §3 设计哲学 · [`03-belief-engine.md`](./03-belief-engine.md) · [`05-whitebox-observability.md`](./05-whitebox-observability.md)

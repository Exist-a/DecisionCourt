# 决策庭（DecisionCourt）产品需求文档

> **版本**：v0.8.3
> **状态**：v0.5 MemoryBus 统一 + ContextView 投影 + ReAct 私有策略自动分类 + 前端 MemoryAuditPanel；v0.5+ 修补 SessionUUID 房间钥匙 bug + MemoryEntry 结构化字段 + AgentAvatar 思考脉冲动画；v0.6 信念引擎升级（贝叶斯 log-odds + 锚定 + weaken 边）+ 智能收敛 4 信号优先级 + belief_diffs 审计 trail + BeliefDiffCard/BeliefTrajectoryTab/ConvergenceBadge + evidence_id 归一化；v0.7 整合文档结构 + ADR 提炼；v0.8 白盒化（slog 结构化日志 / Prometheus-兼容业务指标 / Span + decision_events 业务事件审计 / 端到端 trace_id 串联）+ 文档整合；**v0.8.3 修复"刷新丢数据 + 判决书回退无法继续开庭"5 个根因（verdict 页补齐水合 / memoryEntries REST 端点重启 / WS 心跳+重连 / reopen_trial action / 顶部按钮 phase 派生）**。
> **目标**：为复杂人生/工作决策提供一个结构化、可审计、人机协作的多 Agent 辩论法庭。
> **架构决策**：[`docs/adr/0002-a2a-private-channel.md`](./adr/0002-a2a-private-channel.md)、[`docs/adr/0003-contextview-projection.md`](./adr/0003-contextview-projection.md)、[`docs/adr/0004-bayesian-belief-engine.md`](./adr/0004-bayesian-belief-engine.md)、[`docs/adr/0005-investigation-findings.md`](./adr/0005-investigation-findings.md)
> **设计演进（已归档）**：[`docs/archive/memory-a2a-redesign-v1.2.md`](./archive/memory-a2a-redesign-v1.2.md)
> **v0.8.3 实施记录**：[`archive/refresh-and-reopen-fix-v0.8.3.md`](./archive/refresh-and-reopen-fix-v0.8.3.md)
> **v0.9.1 整合时同步 + 2026-07-03 v0.8.3 修复同步 + 2026-07-04 v0.9 4 份新 ADR + 2026-07-05 v0.9.1 防幻觉修复同步**：本版本号对齐后端代码实装现状（参见 [`docs/README.md` 实装状态矩阵](./README.md)）。

---

## 1. 项目概述

**决策庭（DecisionCourt）** 是一个以"法庭"为隐喻的多 Agent 决策辅助平台。用户面对复杂决策时，不再向单一 AI 要一个"答案"，而是发起一场"庭审"：由多个专业化 AI Agent 分别扮演控方、辩方、调查员、书记员等角色，围绕候选选项进行结构化辩论；用户作为"法官/当事人"可以实时提交证据、传唤调查、打断质询；最终系统输出一份可执行的《决策判决书》。

### 1.1 核心差异化

| 维度 | 传统 AI 问答 | 决策庭 |
|---|---|---|
| 输出形态 | 单一建议 | 结构化判决书 |
| 信息处理 | 黑盒生成 | 多 Agent 对抗 + 证据链 |
| 用户角色 | 被动提问者 | 主动提交证据的法官/当事人 |
| 可审计性 | 低 | 每份证据、每次质证、每轮立场变化均可追溯 |
| 适用场景 | 简单问答 | 职业选择、投资、产品路线、重大生活决策 |

### 1.2 一句话定位

> **让 AI 像法庭一样帮你把复杂决策看全、看透、看出可执行结论。**

---

## 2. 问题陈述

### 2.1 用户痛点

1. **单一 AI 答案太顺滑**：ChatGPT/Claude 会给出看似合理但片面的建议，用户不知道它漏掉了什么。
2. **复杂决策多维度拉扯**：职业选择同时涉及收入、成长、风险、家庭、价值观，单一模型难以权衡。
3. ** confirmation bias（确认偏误）**：用户倾向于只看到自己愿意看到的信息。
4. **缺乏可审计的决策过程**：做完决定后，无法回顾"当时为什么这样选"。

### 2.2 市场机会

- **Kialo / DebateGraph**：擅长人工组织论证，但 AI 不自动参与辩论。
- **DebateAI**：AI 自动辩论，但用户是观众，不能插证据。
- **Courtroom / Court of Agents**：法律专用或模拟娱乐，不做通用决策。
- **空白**：通用复杂决策 + AI Agent 自动对抗 + 用户实时插证据 + 输出判决书。

---

## 3. 用户画像与使用场景

### 3.1 核心用户

| 用户 | 典型场景 | 需求 |
|---|---|---|
| **职场人** | 跳槽 vs 留下、创业 vs 稳定 | 看到选择的全貌，降低后悔 |
| **产品经理/创业者** | 产品路线 A vs B、是否All-in某个功能 | 结构化评估风险和收益 |
| **投资者** | 买入/卖出/持有某个资产 | 多维度证据对抗 |
| **学生/研究者** | 论文选题、研究方向 | 理解不同立场的证据强度 |
| **普通人** | 买房 vs 租房、出国 vs 留下 | 重大人生决策需要"第二意见" |

### 3.2 用户旅程

```
1. 立案 → 输入决策问题
2. 问题澄清 → 如果问题模糊，Agent 提问帮助用户明确边界
3. 选项生成 → 系统生成 2-3 个候选选项，用户选择两个进入庭审
4. 开庭 → Agent 做开场陈述
5. 举证 → 用户/调查员 Agent 提交证据
6. 质证 → 双方对证据进行攻防
7. 调查员补充 → 用户要求调查员搜索新证据
8. 结案陈词 → 双方总结
9. 判决 → 系统生成决策判决书
10. 上诉/再审 → 用户可补充证据后重新开庭
```

---

## 4. 法庭角色与 Agent 设计

### 4.1 角色定义

| 角色 | Agent 名称 | 职责 | 目标函数 |
|---|---|---|---|
| **控方** | ProsecutorAgent | 强力支持某个选项 | 最大化该选项的胜诉概率 |
| **辩方** | DefenderAgent | 强力反对控方选项，或维护现状 | 最小化控方选项的吸引力 |
| **调查员** | InvestigatorAgent | 主动检索外部信息、澄清问题、生成候选选项、提交证据 | 帮助用户把模糊问题转化为可辩论的明确选项，并找到相关证据 |
| **书记员** | ClerkAgent | 记录庭审、整理证据、生成判决书 | 准确、中立、完整 |
| **法官** | **用户本人** | 提交证据、决定调查方向、最终裁决 | 做出最适合自己的决策 |

### 4.2 Agent 行为规范

- **对抗但文明**：可以激烈反驳，但不能人身攻击、不能使用逻辑谬误。
- **证据导向**：每个主张尽量引用证据，不能空口无凭。
- **实时响应证据**：新证据提交后，Agent 必须重新评估并调整策略。
- **可被打断**：用户可以随时暂停庭审、提问、要求调查员搜索。

### 4.3 防止 Agent 互相附和

#### 4.3.1 设计原则

- 使用**不同的 system prompt 和温度参数**。
- 控方和辩方使用**不同的模型**（如果成本允许）；MVP 为公平性使用同一模型时，通过 prompt 和信念引擎保证对抗性。
- **Disagree-or-Commit 协议**：调查员提交证据时必须明确说明支持/削弱哪一方。

#### 4.3.2 信念引擎（Belief Engine，v0.6）

每个 Agent 维护对两个选项的信念度 `belief_A` 和 `belief_B`，满足 `belief_A + belief_B = 1`。

**v0.6 升级**：v0.5 的"加法+clip"公式被替换为 **贝叶斯 log-odds + 锚定** 更新（参考 Belief Engine 2026 / ScioMind 2026）。公式：

```
logit(p)_{t+1} = (1 - Anchor) · [logit(p)_t + Uptake · w · sign · ln(2)]
                 + Anchor · logit(PriorA)
w              = cred · relevance · |impact| · (1 - maxWeaken)
sign           = +1 (confirmation) | -1 (contradiction) | 0 (neutral)
new_p          = sigmoid(logit(p)_{t+1}), clamp 到 [0.05, 0.95]
```

| 角色 | PriorA | Uptake | Anchor | 设计意图 |
|---|---|---|---|---|
| 控方 | 0.7 | 0.4 | 0.7 | 强锚定 A，受证据"确认"放大但难被一次反向证据推翻 |
| 辩方 | 0.3 | 0.4 | 0.7 | 对称：强锚定 B |
| 调查员 | 0.5 | 0.8 | 0.2 | 弱锚定（吃 evidence 方向），高 Uptake，对应"中立搜证" |
| 书记员 | 0.5 | 0.0 | 1.0 | 完全不更新（Uptake=0） |
| 法官 | 0.5 | 0.6 | 0.3 | 弱锚定（综合双方），由 JudgeAssess 单独更新 |

**Confirmation vs Contradiction（sign）**：
- 控方（prior 偏向 A）+ 支持 A 的证据 → `sign = +1`（确认）
- 控方 + 支持 B 的证据 → `sign = -1`（矛盾）
- 调查员（prior = 0.5）→ 直接吃 evidence 方向（neutral passthrough）
- 防止"无方向证据把律师往同一方向推"导致的对抗性坍缩（PROCLAIM 2026）

**Weaken 边（异构论辩图谱）**：
- 律师可以主动质疑某条证据（attacker → target_agent），声明 `weaken_strength ∈ [0, 1]`
- 引擎下次更新时把 `w` 乘以 `1 - max(weaken strength targeting that agent)`
- 支持"质疑 → 削弱 → 法官不被该条证据带偏"的法律推理链

**智能收敛判断（v0.6 多信号）**：四信号按优先级触发：

1. **推理震荡**（最高优先，PROCLAIM 2026 警示"高一致可能是错的"）：最近两条来自不同 agent 的发言 Jaccard > 0.6
2. **双方共识**：控方+辩方都偏向同一侧（都 ≥0.85 或都 ≤0.15）
3. **信念稳定**：连续 N 轮（默认 2）单轮最大 Δ < 0.05
4. **最大轮次兜底**：达到 5 轮强制结束

每条信号触发时通过 `belief.convergence` 事件携带结构化原因，前端展示为 ConvergenceBadge。

**审计 trail**：
- 每次信念更新写一行 `belief_diffs`（prior/posterior/Δ/logit/weight/weaken_factor/reason）
- 前端 BeliefDiffCard 渲染成单条 diff 卡片
- REST 端点 `GET /api/v1/courtrooms/:uuid/belief-diffs[?agent=...&round=...]` 支持查询
- 支持回放/合规审计/法官解释判决依据

**强制立场一致性**：
- Agent 生成发言前，系统先检查其当前信念度。
- ⏳ **v0.7+ 计划**：如果发言内容与信念度方向不一致（例如控方在 `belief_A < 0.5` 时仍在支持 option A），LLM-as-judge 会打回重生成。MVP 阶段 LLM 直接生成，未做反向校验；观测上若发现立场漂移，再启用打回重生成。
- ✅ 发言中必须引用至少一条证据，不能空泛表态（`speaker.EvidenceRefs` 在 Orchestrator `recordSideEffects` 阶段 + Prompt 模板双重约束）。

#### 4.3.3 防止诡辩与重复

- ⏳ **v0.7+ 计划**：限制每轮发言长度（最多 300 字）—— MVP 阶段靠 Prompt 自约束，未做后端硬截断 + 重试。
- ⏳ **v0.7+ 计划**：禁止引用已经被反驳且未翻盘的证据 —— MVP 阶段 LLM 自由引用，未做"已反驳证据"集合跟踪。
- ⏳ **v0.7+ 计划**：引入"新意度"检查：如果 Agent 本轮发言与之前轮次重复度超过 60%，要求其更换角度 —— MVP 阶段未实装 Jaccard 相似度计算，重复发言靠 Prompt 自约束。

### 4.4 Agent 通信协议（A2A）

决策庭的 Agent 之间**不直接共享完整上下文**，而是通过一套标准化的 **A2A（Agent-to-Agent）消息协议**进行通信。该协议由 `Agent Orchestrator` 统一路由，确保消息可追溯、可审计，并为"私有记忆隔离"提供边界。

#### 4.4.1 设计目标

1. **消息边界清晰**：每个 Agent 只能收到它应该收到的消息，避免 Orchestrator 一次性把所有历史上下文塞进同一个 prompt。
2. **立场隔离**：控方和辩方不能看到对方的私有推理链（CoT），防止"看了对方思路后叛变"。
3. **可插拔**：新增 Agent（如专家证人、陪审团）只需遵守 A2A 消息格式即可接入，无需修改核心编排。
4. **可审计**：所有 A2A 消息持久化，便于事后复盘 Agent 的决策链路。

#### 4.4.2 A2A 消息格式

```json
{
  "message_id": "msg_xxx",
  "session_id": "court_xxx",
  "round": 2,
  "phase": "cross_exam",
  "from": "prosecutor_1",
  "to": "orchestrator",
  "message_type": "speech",
  "payload": {
    "action": "rebut_evidence",
    "target_evidence_id": "E001",
    "content": "正式发言内容",
    "reasoning": "内部推理摘要（仅接收方可见）",
    "evidence_refs": ["E001", "E002"],
    "confidence": 0.82
  },
  "memory_references": ["pm_pro_001"],
  "created_at": "2026-06-25T12:00:00Z"
}
```

**字段说明**：

| 字段 | 说明 |
|---|---|
| `from` / `to` | 发送方与接收方 Agent ID，`orchestrator` 为总线 |
| `message_type` | `speech` / `evidence` / `challenge` / `inquiry` / `verdict_task` |
| `payload.reasoning` | 发送方的私有推理链，**仅接收方（以及审计日志）可见** |
| `memory_references` | 引用发送方私有记忆条目 ID，接收方只能看到引用标签，不能读取记忆内容 |

#### 4.4.3 消息流转规则

```
用户动作 / 新证据
       │
       ▼
┌───────────────┐
│ Orchestrator  │  1. 根据当前阶段构造 A2A 消息
│   (A2A Bus)   │  2. 将消息路由给目标 Agent
└───────┬───────┘  3. 仅注入该 Agent 允许访问的记忆和证据
        │
        ├──────────────► Prosecutor（控方）
        ├──────────────► Defender（辩方）
        ├──────────────► Investigator（调查员）
        └──────────────► Clerk（书记员）
```

**关键约束**：

- 控方消息包中**不包含**辩方的 `payload.reasoning`。
- 辩方消息包中**不包含**控方的 `payload.reasoning`。
- 双方共享的上下文仅限：**公共证据板、庭审阶段、轮次编号、对方已公开的正式发言内容**。
- 调查员和书记员可接收双方消息，但书记员生成判决书时**不读取私有记忆**，只读取公共庭审记录。

### 4.5 情节记忆模块（Episodic Memory，v0.4 重设计）

> **v0.4 重大变更**：私有记忆底层从独立 `private_memories` 表迁移到 **A2A 私有消息通道**，统一为单一"MemoryBus"基础设施。详见 [memory-a2a-redesign.md](../.trae/documents/memory-a2a-redesign.md)。

为防止 Agent 在辩论过程中看到对方思路后立场漂移或"叛变"，决策庭引入**情节记忆层（Episodic Memory）**，作为对抗性保证的第三道防线。

#### 4.5.1 设计原则（v0.4）

- **一人一池**：控方、辩方、调查员各自维护自己的情节记忆池，书记员原则上不需要情节记忆。
- **走 A2A 私有通道**：所有情节记忆条目以 `visibility=private` 的 A2A 消息形式存储（`from=self_agent, to=self_agent`），复用 A2A Bus 的隔离/审计/路由能力。
- **互不访问**：任何 Agent 不能读取其他 Agent 的情节记忆；Orchestrator 通过 `BuildContextView(selfAgent)` 投影后才能注入自己 prompt。
- **写入时机**：ReAct 循环的 `reflect` 步骤显式输出 `memory_type` + `memory_note`，ReActRunner 自动通过 A2A Bus 私有通道写入。
- **可审计**：所有读/写都触发 `a2a.message` WebSocket 事件，前端可完整回放。

#### 4.5.2 记忆结构（v0.4 / v0.5+）

情节记忆作为 A2A 私有消息的 Payload 存储。**v0.5+** 字段增强：

```json
{
  "id": "uuid-a2a-msg-xxx",
  "message_type": "strategy_note",
  "from": "prosecutor",
  "to": "prosecutor",
  "visibility": "private",
  "session_uuid": "ws-room-key-xxx",
  "session_id": "uuid-pk-yyy",
  "payload": {
    "memory_type": "strategy_note",
    "stance": "pro_a",
    "confidence": 0.82,
    "reasoning": "辩方对 E002 的质疑存在漏洞，下一轮应强调 E002 的数据来源权威性。",
    "content": "立场 支持选项A · 置信度 82% · 辩方对 E002 的质疑存在漏洞...",
    "linked_evidence_ids": ["E002"],
    "linked_messages": ["msg_def_003"]
  },
  "created_at": "2026-06-29T12:00:00Z"
}
```

**v0.5+ 字段增补说明**：

| 字段 | 来源 | 用途 |
|---|---|---|
| `session_uuid` | `court_sessions.session_uuid`（字符串列） | **WebSocket hub 房间钥匙**。v0.5+ 必填，否则 a2a.message 广播进"鬼屋"被静默丢弃（参见 §4.5.6 bug 修复） |
| `session_id` | `court_sessions.id`（uuid 主键） | 数据库 FK，**与 session_uuid 是不同钥匙** |
| `payload.stance` | `speaker.Stance` 字符串 | 立场 chip：pro_a / pro_b / challenge |
| `payload.confidence` | `speaker.Confidence` 0..1 | 置信度条 + 数字 |
| `payload.reasoning` | `speaker.Reasoning` 字符串 | 推理段独立展示，不再拼进 content 字符串里 |
| `payload.content` | 后端生成的 fallback 摘要 | 老调用方 fallback 用，前端优先读结构化字段 |

#### 4.5.6 v0.5+ 关键 bug 修复：SessionUUID 房间钥匙

**问题**：v0.5 实装时，`Bus.Send` 把 `Message.SessionID.String()`（uuid 主键）当作 WebSocket `hub.Broadcast` 的 room key。但 WebSocket 客户端 join 的是 `session.SessionUUID`（字符串列）—— 两把钥匙不一致，导致所有 `a2a.message` 广播进了"鬼屋"（无客户端的房间），被 hub 静默丢弃。前端 `MemoryAuditPanel` 永远显示 0 条笔记，但 `agent.speak` 事件正常（走的是另一条 `broadcastAgentSpeak` 路径，正确使用 `SessionUUID`）。

**修复**：
1. `a2a.Message` 加 `SessionUUID` 字段（与 `SessionID` 并存，前者广播用、后者 DB FK 用）
2. `Bus.Send` 优先用 `SessionUUID` 当 room key；fallback `SessionID.String()` 时打 WARN 日志
3. `Orchestrator.recordSideEffects` 2 处构造 Message 时填 `SessionUUID`
4. `bus_test.go` 加 `TestBus_Send_BroadcastRoomKey_UsesSessionUUIDNotSessionID` 显式 `assert.NotEqual(SessionID.String())` 锁住回归

**测试守门**：任何人改回 `SessionID.String()` 当 room key 都会立刻爆红。

**记忆类型**（4 种 + 注入方式）：

| 类型 | MessageType | 写入时机 | 示例 |
|---|---|---|---|
| `strategy_note` | `MessageTypeStrategyNote` | 每次 speak 后自动 + ReAct reflect | "下一轮攻击重点是 E002 的数据来源" |
| `opponent_weakness` | `MessageTypeOpponentWeakness` | ReAct reflect 显式声明 | "辩方没反驳 E001 是核心弱点" |
| `self_correction` | `MessageTypeSelfCorrection` | ReAct reflect 显式声明 | "我之前论证 X 有误" |
| `evidence_eval` | `MessageTypeEvidenceEval` | ReAct tool_call(search) 后自动 | "E001 对 option_a 强度 0.7" |

> **v0.4 升级点**：通过在 ReAct 的 `reflect` 步骤扩展 JSON schema（增加 `memory_type` + `memory_note` 字段），让 LLM 在反思时直接产出 4 类记忆，无需额外 LLM 调用。详细参见 PR 2。

#### 4.5.3 三层防线（v0.4 重构）

情节记忆模块与信念引擎、A2A 协议（含 ContextView 投影）共同构成防止 Agent 附和与叛变的三层防线：

1. **信念引擎（Belief Engine）**：量化立场，强制发言方向与信念度一致。
2. **A2A 协议 + ContextView 投影**：限制 Agent 之间的上下文暴露 —— 不仅按 `visibility` 过滤消息，还通过 `SanitizedPayload()` 在生成对方 prompt 前剥离 `reasoning` 字段（防止内部推理链泄露）。
3. **情节记忆池（Episodic Memory）**：保护 Agent 的私有策略，避免被对方推理污染；通过 A2A 私有通道实现，每轮 speak 前 `BuildContextView()` 把自己的历史策略注入 prompt。

> **示例**：如果控方在情节记忆里写下"E003 是弱点，不要主动提"，辩方永远看不到这条记录（既不在 public 流，也不在 private 流里），因此无法针对性攻击。辩方只能通过控方的公开发言和公共证据板进行反驳。

#### 4.5.4 注入策略

按 v0.4 决策：单场庭审 ≤ 50 条 strategy_note × ~200 token = **10K tokens**，**全文注入**。当数量超过阈值时（第二阶段扩展），启用滚动窗口+摘要策略。

#### 4.5.5 数据迁移（v0.4 → v0.5）

旧 `private_memories` 表保留 1 个版本周期的**双写过渡期**（PR 4 完成时启动），按业界 Expand-Contract 三阶段零停机迁移：

```
Phase 1: 双写 1 周（验证两表一致性）
Phase 2: 迁移历史 + 影子读对比
Phase 3: 全量切读到 a2a_messages → drop 旧表
```

---

## 4.6 Verdict 页面 UX 增量（v0.5+）

> **为什么这一节独立**：判决书生成逻辑本身没变（仍是 `ClerkAgent` + 法官信念度），但 verdict 页面要回答两个用户痛点：
> 1. 庭审结束时"我刚看了什么？给我一句话总结"
> 2. 庭审结束后"我想把这场庭审带走"
>
> 这两个是 UX 增量，**不**是 memory architecture 改动。

### 4.6.1 庭审纪要（Trial Summary）

- **新增字段**：`verdict.trial_summary`（TEXT），与现有 `summary`（采纳建议）并列
- **生成时机**：`ClerkAgent` 撰写判决书时一次性输出，复用现有 JSON Mode 响应
- **生成策略**：在 `ClerkPromptWithJudgeDecision` 增加 1 字段，要求 1-2 句叙事
  - 关注点：双方核心攻防 + 关键转折点
  - **不要**复述最终裁决（避免与 `summary` 重复）
  - 示例：*"控方在第 2 轮抛出 E001 的数据来源质疑，辩方未及时回应导致失分；最终比分在第 3 轮才拉开。"*
- **渲染位置**：verdict 页面"采纳建议"卡之后、"判决书正文"之前的独立卡片（`bg-paperDeep + border-l-2 border-judge`）
- **数据迁移**：老 verdict 行 `trial_summary` 为空字符串，前端 `v-if` 不渲染（不显示空卡片）

### 4.6.2 导出（Export）—— JSON + PDF

#### 设计目标
- 让用户能"带走"自己的庭审：JSON 留底、PDF 打印/转发
- **不**在产品层强制：默认隐藏（不污染主流程），verdict 页面顶部 header 出现按钮

#### JSON 导出

- **新增端点**：`GET /api/v1/courtrooms/:session_uuid/export`
- **响应 Content-Type**：`application/json; attachment; filename=decisioncourt-<uuid>-<ts>.json`
- **Payload 包含**：
  - `session`：庭审元数据
  - `verdict`：含 `summary` + `trial_summary` + 全文 `content`
  - `evidences`：所有用户提交证据
  - `messages`：完整庭审 transcript（含 agent_type 通过 JOIN agents 拿）
  - `a2a_messages`：**只含 `ListVisibleTo("user")` 能看到的**（public + user 自己的 private memory）
- **可见性保证**：通过 `a2a.Bus.ListVisibleTo(sessionID, "user")` 复用 SQL 隔离，**对家 private memory 不会泄漏到导出文件**

#### PDF 导出

- **后端零改动**：用浏览器 `window.print()` + 打印样式
- **打印样式**（`globals.css` `@media print` 块）：
  - 隐藏交互元素（按钮、印章）
  - 强制白底黑字（覆盖 paper 暖色，省墨）
  - 颜色印刷适配（玫红/蓝/琥珀 → 黑/灰/深灰）
  - 每个 section `page-break-inside: avoid`
  - transcript 容器解除 `max-h-[480px]` 限制
- **触发按钮**：verdict 页 header "导出 PDF" 按钮 → `window.print()` → 浏览器原生 PDF 保存

#### 设计权衡：为什么 PDF 不走后端

- 后端加 PDF 库（`gofpdf` / `unidoc`）= ~30MB 依赖 + 维护成本
- 浏览器原生 PDF 质量足够（用户打印用），不损失任何功能
- 当前用户量下，PDF 导出使用率 < 5%，不值得优化
- 如果将来需要：可加 `?format=pdf` query 参数，触发后端 wkhtmltopdf 路径

### 4.6.3 错误反馈 UX（v0.10.17 silent-error-fix）

> **为什么这一节独立**：错误反馈是用户感知"系统是否在工作"的最直接信号。v0.10.17 之前
> 12 处静默错误黑洞让用户产生"庭审卡住" / "网络问题" / "我哪里做错了" 的困惑（实际是
> 后端错误但前端没显示）。这一节定义 UX 规范。

**核心原则**：**永远不让用户看到"操作无反应"**。所有面向用户的失败都通过结构化错误反馈，
用户能看到"发生了什么 + 怎么恢复"。

**4 类反馈渠道**（按用户感知强度递增）：

| 渠道 | 触发场景 | 自动消失 | 用户操作 |
|---|---|---|---|
| Toast（右下角堆叠） | 临时性错误（操作太快 / 操作无效） | 3-5s | 关闭 / 点击按钮 |
| Banner（顶部横幅） | 系统降级（搜索不可用 / breaker 打开） | **不消失** | 自定义恢复动作 |
| Modal（页面中央） | 资源耗尽（trial 配额 / budget） | **不消失** | 强制决策（去判决页 / 等明天） |
| 页面级 setState | 业务级错误（导出失败 / 重开失败） | **不消失** | 重试按钮 |

**Toast 4 class 映射**（与 ErrorClass 对应）：

| Class | 用户场景 | 反馈 |
|---|---|---|
| `user_input` | 操作错（按钮按错 / 阶段不允许） | Toast 3s + 无按钮 |
| `transient` | 临时失败（网络抖动 / 5xx） | Toast 5s + "重试"按钮 |
| `degraded` | 系统降级（搜索不可用） | 顶部 Banner 持续 |
| `fatal` | 无法继续（budget 耗尽 / ReAct 卡死） | Toast 不消失 + 强制 recovery 按钮 |

**反模式**（避免）：

- ❌ `window.alert(...)`：阻塞 + 丑样式 + 用户体验差
- ❌ `console.error` 后静默：用户完全无感知
- ❌ 重复反馈：toast + 页面 setState + console.error 三处都说同一件事

**正确做法**（v0.10.17 实装）：

- ✅ HTTP 错误由 `lib/api.ts::fetchJson` → `errorBus.handleApiError` 自动 toast
- ✅ WS 错误事件由 `CourtroomScene` handler → `handleWsError` 自动 toast
- ✅ 业务代码 catch 块只 `console.debug`（toast 已展示）+ 页面 setState（持久详情）
- ✅ recovery 按钮 onClick 由调用方注入（典型：`ws.send({action: "restart_opening"})`）

完整规范见 [ADR 0024 §2.1](./adr/0024-silent-error-fix-pr1.md) + [tech-spec §8.3](./decisioncourt-tech-spec.md)。

---

## 5. 证据系统

### 5.1 证据来源

> **v0.3 修订**：调查员 WebSearch 的产物**不**再写入 `evidences` 表——而是写入独立的 `investigation_findings` 表（详见 §5.6）。这是因为调查发现与用户证据语义不同：用户证据是用户主动提交的事实/约束，调查发现是 LLM 派遣调查员后的搜索结果，**两类来源在 UI 和数据库层面严格分离**。

| 来源 | 说明 | MVP 支持 | 落地表 |
|---|---|---|---|
| **用户提交** | 用户输入的事实、偏好、约束 | ✅ | `evidences` |
| **Agent 主动提问** | Agent 识别信息缺口后向用户提问，回答自动转为证据 | ✅ | `evidences` |
| **调查员 WebSearch** | 控辩方 LLM 在 ReAct 思考中派遣调查员搜索 | ✅ | `investigation_findings`（独立表） |
| **专家证人** | 特定领域专家 Agent 提供专业意见 | ❌（第二阶段） | — |
| **历史庭审** | 引用类似决策的过往判决书 | ❌（第二阶段） | — |

### 5.2 证据类型

| 类型 | 示例 | 可信度权重 |
|---|---|---|
| **事实证据** | "我有 18 个月应急基金" | 高（用户自述） |
| **数据证据** | "该赛道 2025 年增长率 30%" | 高（需来源） |
| **专家意见** | "职业顾问认为创业适合 30 岁以下" | 中（需背书） |
| **个人偏好** | "我更看重工作自主性" | 中（主观但关键） |
| **约束条件** | "我必须留在上海" | 高（硬约束） |

### 5.3 证据结构化字段

```json
{
  "evidence_id": "E001",
  "type": "fact",
  "source": "user",
  "content": "我已存下 18 个月生活费的应急基金",
  "url": null,
  "timestamp": "2026-06-24T10:00:00Z",
  "credibility_score": 0.9,
  "relevance_score": 0.85,
  "impact_on_option_A": 0.7,
  "impact_on_option_B": -0.3,
  "status": "admitted"
}
```

### 5.4 证据规则

- **可采性规则**：证据必须与争议焦点相关，且在时效范围内。
- **质证机制**：对方 Agent 可以质疑证据的相关性、可信度、完整性。
- **传闻证据过滤**：无法验证的第三方传言需要标记为低可信度。
- **硬约束优先**：用户明确给出的约束条件自动成为判决边界。

### 5.5 Agent 主动提问机制

#### 5.5.1 触发条件

Agent 在以下情况下会主动向用户提问：

1. 立案信息存在明显缺口（如未说明收入、家庭状况、风险偏好等）。
2. 某条证据需要补充上下文才能评估（如"你说想创业，但你有多少储蓄？"）。
3. 控辩双方对某个关键事实都缺乏证据支撑。

#### 5.5.2 提问流程

1. Agent 分析当前证据板，识别信息缺口。
2. 生成 1-3 个高价值问题，按优先级排序。
3. 向前端发送 `user.action.required` 事件，显示问题。
4. 用户回答后，自动将回答结构化为证据并加入证据板。
5. 控辩双方 Agent 基于新证据继续辩论。

#### 5.5.3 问题示例

| 决策场景 | 问题 | 转化为证据类型 |
|---|---|---|
| 跳槽选择 | "你目前的月收入范围是多少？" | 事实证据 |
| 创业选择 | "你能接受多长时间没有稳定收入？" | 约束/偏好 |
| 投资选择 | "如果这笔投资亏损 30%，你会怎么做？" | 风险偏好 |
| 城市选择 | "你的家庭成员是否需要你留在某个城市？" | 约束条件 |

#### 5.5.4 用户体验原则

- 每次只问 1 个问题，避免信息过载。
- 提供"跳过"按钮，不强迫用户回答。
- 问题必须明确标注目的（"为了评估你的风险承受能力"）。
- 用户跳过 3 次后，Agent 不再主动提问，避免打扰。

---

### 5.6 调查发现（Investigation Finding）— v0.3 新增

> 控辩方 LLM 在 ReAct 思考中通过 `agent.cot_step(tool_call, tool=investigator_search)` 内部决策派遣调查员。**调查结果不是证据**，而是一种新概念：**调查发现**。

#### 5.6.1 与证据的区别

| 维度 | 证据（Evidence） | 调查发现（Investigation Finding） |
|---|---|---|
| 来源 | 用户手动提交 | 控辩方 LLM 派遣调查员搜索 |
| 落地表 | `evidences` | `investigation_findings`（独立） |
| 展示位置 | EvidenceBoard | InvestigatorPanel（独立 Tab） |
| 行为影响 | 进入公共证据板 + 触发信念更新 | **不**更新信念（仅供 LLM 参考） |
| 是否引用进发言 | 是（evidence_refs） | 否（仅作 LLM 上下文，**不**在 evidence_refs 出现） |
| 透明度 | 用户可看 | 用户可看（点开看完整 raw_results） |

#### 5.6.2 调查发现结构

```json
{
  "finding_id": "f-xxx",
  "dispatcher": "prosecutor",
  "investigator": "investigator",
  "query": "睡眠对健康的好处 科学证据",
  "summary": "前 3 条搜索结果摘要",
  "raw_results": [
    { "title": "睡眠不足的 7 大危害", "url": "https://...", "content": "..." }
  ],
  "result_count": 10,
  "source_provider": "bocha",
  "created_at": "2026-06-29T12:01:00Z"
}
```

#### 5.6.3 可见性

**公开**（类比正常庭审记录）—— A2A 消息的 `visibility=public`。理由：
- 庭审记录对所有旁观者公开是核心隐喻
- 用户需要看到双方各调动了哪些证据
- "调查发现" 与"证据"在 UI 上明确区分即可

#### 5.6.4 前端表现

启动庭审时调 `GET /api/v1/courtrooms/:uuid/investigations` 拉历史，灌入 InvestigatorPanel（独立 Tab）。
调查员被派遣时，avatar 出现「正在调查：...」气泡 + 旋转 spinner，状态机推进到 searching → completed（用户可点击 ✓ 行的"查看 N 条搜索结果"展开 raw_results）。

---

## 6. WebSearch 方案

### 6.1 搜索提供商抽象

```go
type SearchProvider interface {
    Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error)
}
```

### 6.2 环境切换

| 环境 | 提供商 | 原因 |
|---|---|---|
| **本地开发** | **Bocha API**（默认）/ SearXNG（Docker） | Bocha 国内可稳定访问；SearXNG 免费本地 |
| **单元测试** | **MockProvider** | 零依赖，可预测输出 |
| **测试/部署** | **Bocha / Tavily** | Bocha 国内友好；Tavily 海外质量高 |

> **v0.8.3 修订**：v0.3 弃用了原 v0.2 的 DuckDuckGo（反爬）,v0.8.3 进一步简化:provider 只剩 Bocha / Mock(DuckDuckGo 全部代码 + 测试删除)。

### 6.3 调查员搜索流程

> **v0.3 修订**：原 v0.2 描述的"将结果结构化为证据"已改为"写入 investigation_findings 表"。详见 §5.6。

1. 控辩方 LLM 在 ReAct 思考中输出 `action=tool_call, tool=investigator_search, tool_input={query: ...}`
2. 后端 `courtroom.Service.DispatchInvestigator` 调 `investigation.Service.RecordFinding`
3. 公开 A2A `dispatch_investigator` 消息 → 推送 `search.started` ws 事件
4. 调 SearchProvider（Bocha / SearXNG / Mock）
5. 持久化到 `investigation_findings` 表（含 `raw_result` 完整搜索结果）
6. 公开 A2A `investigation_report` 消息 → 推送 `search.completed` ws 事件（payload 含 finding_id / result_count / summary / raw_results）
7. 前端 InvestigatorPanel 显示该 finding（用户可点开查看完整搜索结果）

---

## 7. 庭审流程状态机

### 7.1 庭审模式

用户立案时可选择庭审模式：

| 模式 | 质证轮数 | 预计时长 | 适用场景 |
|---|---|---|---|
| **快速模式** | 2 轮 | 3-5 分钟 | 简单二选一 |
| **标准模式（默认）** | 3 轮 | 5-10 分钟 | 一般复杂决策 |
| **深度模式** | 5 轮 | 10-20 分钟 | 重大人生/商业决策 |

### 7.2 状态机

```
[Idle] 立案 (Filing) + 选择庭审模式
  ↓
[问题判断] 用户是否提供了两个明确选项？
  ├─ 是 → 直接进入 [Opening]
  └─ 否 → 进入 [Clarification]
             ↓
         [Clarification] Agent 向用户提问澄清
             ↓
         [OptionGeneration] 生成 2-3 个候选选项
             ↓
         [用户选择] 用户从中选择两个选项
             ↓
         [Opening] 开庭陈述
             ↓
         [Evidence] 举证阶段
             ↓ 用户提交证据 / 调查员搜索 / Agent 主动提问
         [CrossExam] 质证阶段（第 1 轮）
             ↓
         [CrossExam] 质证阶段（第 2 轮）
             ↓
         [CrossExam] 质证阶段（第 N 轮，N ≤ 模式设定轮数）
             ↓
         [智能收敛检查] 连续两轮信念度变化 < 5%？
           ├─ 是 → 提前进入 [Closing]
           └─ 否 → 继续下一轮（未达上限）/ 进入 [Closing]（已达上限）
             ↓
         [Closing] 结案陈词
             ↓
         [Deliberation] 书记员整理
             ↓
         [Verdict] 生成判决书
             ↓
         [Appeal] 用户补充证据 / 修改偏好 → 回到 Evidence
         （v0.8.3 新增 fast-path：[Verdict] → 直接 reopen_trial → [Evidence]）
```

**v0.8.3 状态机更新**：
- `verdict → evidence` 新增合法边（保留 `verdict → appeal` 旧边）。`reopen_trial` action 在 `verdict` 和 `appeal` 两个阶段都接受；fast-path 让"补充证据重开"按钮可以一步到位回 evidence 阶段（不必走 appeal 中间状态）。
- `reopen_trial` 保持当前 round 不变。用户后续点 `continue_cross_exam` 触发 `round+1` ——"原来打 3 轮，重开后接着第 4 轮"。
- beliefs / evidences / messages / verdict 行全部保留，律师能看到完整历史（这是 B-4 的产品决策）。

### 7.3 用户控制点

- **问题澄清阶段**：用户可回答 Agent 的澄清问题，也可选择"跳过，直接生成选项"。
- **选项选择**：用户从生成的 2-3 个候选选项中选择两个进入庭审，或手动输入自己的选项。
- **跳过当前 Agent**：用户可跳过某个 Agent 的发言。
- **直接判决**：用户可随时点击"直接判决"，跳过剩余轮次。
- **要求调查**：用户可要求调查员搜索特定主题。
- **提交证据**：任何阶段用户都可以提交新证据。
- **暂停庭审**：用户可暂停，稍后恢复。

### 7.1 每个阶段的前端表现

| 阶段 | 前端表现 |
|---|---|
| 立案 | 表单输入决策问题、选项、背景 |
| 开庭 | 控方、辩方 Agent 头像分别做开场陈述 |
| 举证 | 中间出现"证据板"，新证据飞入 |
| 质证 | 被质疑的证据高亮，Agent 指向证据发言 |
| 调查 | 调查员 Agent 入场，展示搜索过程和结果 |
| 结案 | 双方 Agent 回到各自位置做总结 |
| 判决 | 书记员头像展示判决书 |

### 7.5 问题澄清与选项生成机制

#### 7.5.1 触发条件

系统判断是否需要进入问题澄清阶段的条件：

1. 用户只输入了问题描述，没有提供两个明确选项。
2. 用户提供的选项数量不是 2 个。
3. 用户问题中包含"不知道"、"怎么"、"如何"、"应该"等模糊词。
4. 系统无法从问题中识别出清晰的对立维度。

#### 7.5.2 问题澄清流程

```
用户输入："我不知道该不该跳槽"
  ↓
调查员 Agent 分析：缺少哪些关键信息？
  ↓
生成 2-3 个澄清问题：
  1. "你对现在工作最不满意的地方是什么？"
  2. "你有没有具体的跳槽目标或 offer？"
  3. "你的家庭财务状况能支持收入波动吗？"
  ↓
用户回答（可跳过）
  ↓
进入选项生成阶段
```

#### 7.5.3 选项生成流程

基于用户问题和澄清回答，调查员 Agent 生成 2-3 个候选选项：

```json
{
  "options": [
    {
      "id": "opt_a",
      "label": "留在现公司，争取内部晋升或转岗",
      "rationale": "风险最低，适合当前环境不确定的情况"
    },
    {
      "id": "opt_b",
      "label": "跳槽到同行业成熟公司",
      "rationale": "收入和稳定性都有保障，成长空间有限"
    },
    {
      "id": "opt_c",
      "label": "加入创业公司核心团队",
      "rationale": "高风险高回报，适合追求快速成长"
    }
  ]
}
```

用户选择其中两个进入庭审，或手动输入自己的两个选项。

#### 7.5.4 前端表现

| 阶段 | 前端表现 |
|---|---|
| 问题澄清 | 中央显示"法官助理正在了解案情"，下方逐个显示问题卡片 |
| 选项生成 | 显示候选选项卡片，用户点击选择两个 |
| 选项确认 | 显示最终进入庭审的两个选项，用户确认后开始 |

#### 7.5.5 设计原则

- **不强迫回答**：每个澄清问题都可跳过。
- **快速生成**：最多 3 个澄清问题，避免用户流失。
- **用户有最终决定权**：Agent 生成选项，用户选择或修改。
- **保留简单路径**：如果用户直接输入"A vs B"，跳过澄清直接进入庭审。

---

## 8. 可视化设计

### 8.1 风格

**极简白底法庭风格**：以白色为主色调，配合柔和的 slate 灰、黑色强调色，营造干净、专注的庭审氛围。输入框采用凹陷内阴影设计，增强可交互感。整体视觉现代、轻盈，不依赖木质纹理或深色背景。

### 8.2 首页 / 立案页

- 纯白背景，中央为立案卡片。
- 输入框使用凹陷阴影（inset shadow），呈现"可按压"的视觉暗示。
- 表单包含：决策问题、选项 A/B、背景信息、庭审模式。
- 底部三个轻量特性介绍卡片。

### 8.3 庭审主界面布局

```
┌─────────────────────────────────────────────────────────────┐
│  顶部：庭审标题 + 当前阶段 + 直接判决按钮                      │
├──────────────────────────┬──────────────────────────────────┤
│                          │                                  │
│                          │   Agent 发言气泡                 │
│                          │                                  │
│   庭审记录 / 历史消息     │   ●  ●  ●  ●                     │
│   （包含 CoT 推理）       │   控方 调查员 书记员 辩方        │
│                          │                                  │
│                          │   证据板（横向滚动卡片）          │
│                          │                                  │
├──────────────────────────┴──────────────────────────────────┤
│  [+][搜索][跳过][语音]  凹陷输入框 ...  [发送]                │
└─────────────────────────────────────────────────────────────┘
```

#### Agent 表现

- Agent 以彩色圆点表示（控方玫瑰红、辩方蓝、调查员绿、书记员琥珀黄）。
- 发言时圆点放大并出现脉冲动画，上方弹出文字气泡显示当前发言内容。
- 未来可升级为像素小人或自定义头像。

#### 历史消息栏

- 位于右侧（桌面端），记录所有 Agent 发言、系统事件、用户操作。
- 支持展示 CoT 推理过程（metadata.reasoning）。
- 移动端可折叠为底部抽屉。

#### 底部输入区

- 中央是凹陷风格输入框，用户可随时输入证据或打断庭审。
- 输入框左侧排列功能按钮：提交证据、要求调查、跳过当前 Agent、语音输入等。
- 右侧为发送按钮。

### 8.4 关键可视化元素（v0.3 增补；v0.4 增补 MemoryAuditPanel）

- **Agent 头像气泡**（v0.3 增补）：律师开始思考时头像上方立即出现云朵呼吸动画 + 「思考中（N 步）」；开始 speak 时切到**逐字打字**气泡（流式 chunk）+ 末尾闪烁光标；调查员被派遣时头像出现旋转 spinner + 「正在调查：..."」气泡。气泡优先级：调查 > 流式 > 思考 > 完整发言。
- **Agent 气泡**：当前发言以气泡形式实时展示在对应 Agent 圆点上方。
- **庭审记录**：右侧滚动面板展示完整历史，包含 Agent 角色、阶段、轮次、引用证据。
- **证据板**：横向滚动的证据卡片，显示证据类型、来源、影响与质疑状态。
- **调查活动面板**（v0.3 新增）：右侧 Tab「调查活动」列出所有调查发现 + 实时派遣/回报状态切换（searching → completed/failed）。每条 finding 可点击展开完整 raw_results。
- **记忆审计面板**（v0.4 新增）：右侧 Tab「策略笔记」按类型（strategy_note / opponent_weakness / self_correction / evidence_eval）过滤 A2A 私有消息流，展示双方 Agent 的情节记忆时间线。**默认开启**（差异化卖点：用户能看到律师"内心戏"）；提供 **"真实法庭模式" toggle**，开启后仅显示 type 和数量，隐藏具体内容（更接近真实法庭的私密性）。
- **幕后视角详情页**（v0.4 新增）：庭审结束后解锁 `/verdict/:id` 页的"幕后视角" Tab，展示完整的私有策略演化路径，作为可审计的决策记录。
- **判决书页**：白底卡片式布局，展示选项得分、共识点、争议焦点、可执行建议。

---

## 9. 技术架构

### 9.1 推荐架构

```
┌─────────────────────────────────────────────┐
│              Frontend (Next.js 14)          │
│  - React + TypeScript                       │
│  - Tailwind CSS + shadcn/ui                 │
│  - D3.js / React-Flow 可视化                 │
│  - Socket.io-client（实时庭审流）            │
└───────────────────┬─────────────────────────┘
                    │ HTTP / WebSocket
┌───────────────────▼─────────────────────────┐
│              API Gateway (Go/Gin)           │
│  - 庭审会话管理                              │
│  - 证据 CRUD                                │
│  - Agent 编排接口                            │
└───────────────────┬─────────────────────────┘
                    │
┌───────────────────▼─────────────────────────┐
│         Agent Orchestration Service         │
│  - 自定义状态机 / LangGraph                  │
│  - A2A 消息总线（Agent-to-Agent）            │
│  - 私有记忆池（按 Agent 隔离）               │
│  - 信念引擎                                  │
└───────────────────┬─────────────────────────┘
                    │
┌───────────────────▼─────────────────────────┐
│            Agent Gateway（非 MVP）           │
│  - 模型路由与选择                            │
│  - Prompt 压缩与上下文管理                   │
│  - Token 预算与成本追踪                      │
│  - 响应缓存与限流                            │
│  - 调用审计                                  │
└───────────────────┬─────────────────────────┘
                    │
┌───────────────────▼─────────────────────────┐
│              LLM Providers                  │
│  - DeepSeek-V3 / DeepSeek-R1                │
│  - OpenAI / Claude（可切换）                │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│              Data Layer                     │
│  - PostgreSQL：庭审记录、证据、判决书         │
│  - PostgreSQL：A2A 消息与私有记忆             │
│  - Redis：会话状态、WebSocket 订阅            │
│  - Vector DB（可选）：证据语义检索            │
└─────────────────────────────────────────────┘
```

**说明**：Agent Gateway 是第二阶段扩展组件，位于 Agent Orchestration 与 LLM Provider 之间。MVP 阶段 Agent Orchestration 直接调用 LLM；第二阶段引入 Gateway，实现可量化的 Token 成本优化和模型路由策略。

### 9.2 后端核心模块

| 模块 | 职责 |
|---|---|
| `courtroom` | 庭审会话、阶段状态机、DispatchInvestigator 单条 entry 状态机、speakWithReAct 流式 |
| `agent` | Agent 角色定义、prompt 管理、ReAct 循环、工具调度 |
| `agent/tools` | 调查员搜索工具等可插拔工具 |
| `a2a` | Agent 间消息协议、消息路由、访问控制 |
| `private_memory` | 各 Agent 私有记忆池的 CRUD 与隔离 |
| `evidence` | **仅**用户证据的 CRUD 与影响评估 |
| `investigation`（v0.3 新增） | 调查发现的 CRUD + 公开 A2A 集成 |
| `search` | WebSearch 提供商抽象与实现（Bocha 默认） |
| `belief_engine` | 信念度更新、立场变化计算 |
| `argument_graph` | 论点-反驳关系图 |
| `verdict` | 判决书生成 |
| `websocket` | 实时推送到前端（hub.Broadcast sleep 30ms 保流式 spacing） |
| `llm` | DeepSeek 客户端（Complete + StreamComplete） |
| `api` | REST handler + WebSocket Hub |
| `agent_gateway`（第二阶段）| LLM 模型路由、Prompt 压缩、Token 预算、审计 |

### 9.3 Agent 调用协议（A2A）

Agent 之间的调用不再通过一个大而全的 `context` 对象直接传递，而是包装成 A2A 消息，由 Orchestrator 根据接收方权限注入上下文。

**基本消息格式**：

```json
{
  "message_id": "msg_xxx",
  "session_id": "court_xxx",
  "round": 2,
  "phase": "cross_exam",
  "from": "orchestrator",
  "to": "prosecutor_1",
  "message_type": "speech_task",
  "payload": {
    "action": "rebut_evidence",
    "target_evidence_id": "E001",
    "evidence_board": [...],
    "debate_history": [...],
    "belief_state": {...}
  },
  "private_memory": ["pm_pro_001", "pm_pro_002"],
  "created_at": "2026-06-25T12:00:00Z"
}
```

**上下文隔离规则**：

| 信息类型 | 控方可见 | 辩方可见 | 调查员可见 | 书记员可见 |
|---|---|---|---|---|
| 公共证据板 | ✅ | ✅ | ✅ | ✅ |
| 双方公开发言 | ✅ | ✅ | ✅ | ✅ |
| 控方私有推理 / 记忆 | ✅ | ❌ | ❌ | ❌ |
| 辩方私有推理 / 记忆 | ❌ | ✅ | ❌ | ❌ |
| 调查员私有评估 | ❌ | ❌ | ✅ | ❌ |
| 完整 A2A 消息日志 | Orchestrator 与审计系统可见 | | | |

> 书记员生成判决书时**只能读取公共记录**，不能读取控辩双方的私有记忆，确保判决客观中立。

### 9.4 Agent Gateway

Agent Gateway 是位于 Agent Orchestration 与 LLM Provider 之间的中间层，专门解决多 Agent 系统中的**成本、效率、可观测性**问题。

> **v0.5+ 范围调整**：白盒子集（统一接入 + 审计落库 + trace 关联）已在 MVP 阶段实装。完整版（Prompt 压缩 / Token 预算 / 限流 / Fallback）已在 v0.5+ 实装，作为可开关的高级能力；模型路由 / 缓存仍留到第二阶段。
>
> **基线 benchmark（2026-07-01）**：v0.5+ Legacy keep-5 节省 **70.3%**（837 tokens / 12 轮庭审）；v2 Smart 评分压缩节省 **31.4%**（374 tokens / 12 轮庭审）；DecisionCourt 庭审场景推荐 Smart —— 见 `.trae/documents/compression-eval-baseline.md`。

#### 9.4.0 v0.5+ 已实装

| 能力 | 状态 | 说明 |
|------|------|------|
| 统一接入（`internal/agent_gateway` 装饰器）| ✅ 已实装 | 所有 `llm.Client` 调用经过 `agent_gateway.NewWithConfig` / `Wrap`，orchestrator / evidence / react_runner 都接入 |
| 观测与审计（写 `llm_calls` 表）| ✅ 已实装 | 每次调用写一行：model / provider / prompt+completion tokens / latency_ms / status / error_msg |
| Trace 关联（ctx 注入 session/agent/task）| ✅ 已实装 | `WithTrace` 在每次 `Complete`/`StreamComplete` 前注入；`llm_calls` 可按 session / agent_type 聚合 |
| Prompt 压缩 | ✅ 已实装（v2 升级） | 默认 "system + 最近 5 条"；启用 `SmartCompression` 后走 "评分 + 原子组 + 贪心打包 + 兜底摘要" 新策略，针对庭审多角色交叉引用场景 |
| Token 预算 | ✅ 已实装（v2 升级） | 按 `session_uuid` 内存维护；多维 input/output/cost；sliding 5min；`OnWarning` 回调；`RejectWhenExhausted` 时返回 `ErrBudgetExhausted` |
| 限流 | ✅ 已实装 | 预算达到阈值时降低 `max_tokens` / temperature；可开关 |
| Fallback（退避重试）| ✅ 已实装 | 失败 500ms/1s/2s 退避重试 3 次；仅 Complete 生效；可开关 |
| 文件日志（JSON 追加）| ✅ 已实装 | 输出到 `backend/logs/agent_gateway_YYYY-MM-DD.log`，含压缩/限流/重试/预算快照字段，用于对比实验 |
| 模型路由 | ⏳ 第二阶段 | 当前 Orchestrator 在关键轮次手工选 R1；自动路由留到下一阶段 |
| 响应缓存 | ⏳ 第二阶段 | 庭审唯一性场景，缓存价值低 |
| **前端审计可视化** | ❌ **不做** | 后端 `llm_calls` 表 + `backend/logs/agent_gateway_*.log` 已足够（开发期排查用 tail/grep/JSON 工具；产品级 dashboard 不在 MVP 范围）。**决策日期 2026-07-01**：LLM 审计是开发者关心的内部观测项，不是用户关心的产品功能；放日志里查询即可，不增加前端组件复杂度。 |

#### 9.4.1 核心职责

| 职责 | 说明 | 状态 |
|---|---|---|
| **模型路由** | 根据任务复杂度选择模型：开场/质证用轻量模型，最终判决/复杂推理用强模型 | ⏳ 第二阶段 |
| **Prompt 压缩** | 对历史上下文进行摘要、去重、截断，减少输入 Token | ✅ 已实装 |
| **Token 预算** | 为每个庭审设定 Token 上限，超预算时触发压缩或降级策略 | ✅ 已实装 |
| **响应缓存** | 缓存相似请求的响应，减少重复调用 | ⏳ 第二阶段 |
| **限流与降级** | 超预算时降低 max_tokens / temperature，API 失败时退避重试 | ✅ 已实装 |
| **调用审计** | 记录每次调用的模型、Token 数、成本、延迟、响应摘要 | ✅ 已实装 |

#### 9.4.2 可量化优化指标

Agent Gateway 需要输出可量化的成本优化数据：

| 指标 | 定义 | 示例目标 |
|---|---|---|
| **Token 节省率** | （未优化 Token - 实际 Token）/ 未优化 Token | ≥ 30% |
| **模型降级率** | 简单任务使用轻量模型的比例 | ≥ 60% |
| **缓存命中率** | 命中缓存的请求占比 | ≥ 20% |
| **平均单次庭审成本** | 完整庭审的 LLM 调用成本 | ≤ 0.5 元 |
| **P95 延迟** | 95% 请求的响应延迟 | ≤ 3s |
| **模型切换成功率** | 主模型失败时切换到备用模型的成功率 | ≥ 99% |

#### 9.4.3 路由策略示例

```go
func RouteModel(task TaskType, complexity float64, budget TokenBudget) ModelConfig {
    switch task {
    case TaskOpeningStatement, TaskRoutineRebuttal:
        if complexity < 0.6 {
            return DeepSeekV3Lite  // 轻量、便宜
        }
        return DeepSeekV3
    case TaskFinalVerdict, TaskComplexReasoning:
        return DeepSeekR1         // 强推理
    default:
        return DeepSeekV3
    }
}
```

#### 9.4.4 压缩策略

- **历史摘要**：超过 N 轮后，将早期对话压缩为摘要。
- **证据去重**：相同证据不重复注入上下文。
- **结构化上下文**：用 JSON/表格替代自然语言描述，减少 Token。
- **动态截断**：按 Token 预算截断低相关性内容，优先保留证据和信念状态。

---

## 10. MVP 功能边界

### 10.1 第一阶段：MVP（4-6 周）

**必须做：**
- [x] 通用决策模板：用户可自定义问题和两个选项
- [ ] ❌ **不做**：问题澄清与选项生成（用户只给模糊问题时） —— MVP 明确不做，见 §15.3
- [x] 庭审模式选择：快速（2 轮）/ 标准（3 轮，默认）/ 深度（5 轮）
- [x] 智能收敛：多信号按优先级触发（推理震荡 > 双方共识 > 信念稳定 > 最大轮次兜底），详见 §4.3.2 v0.6
- [x] 4 个核心 Agent：控方、辩方、调查员、书记员
- [x] 信念引擎：每个 Agent 维护并更新对选项的信念度（v0.6 Bayesian log-odds + anchoring）
- [x] 用户提交证据
- [ ] ❌ **不做**：Agent 主动提问（识别信息缺口后向用户提问，回答转为证据）—— MVP 明确不做，见 §15.3
- [x] 调查员 Agent 做简单 WebSearch（开发用 Bocha，HTTP 200 实测）
- [x] 基础庭审流程：开庭 → 举证 → 质证（用户触发每轮）→ 调查补充 → 结案 → 判决
- [x] 观点地图（ArgumentMap 精简版，详见 `.trae/documents/庭审可视化简化计划.md`）
- [x] 立场变化曲线 —— ❌ **不做**：组件保留 `StanceChart.tsx` 文件但庭审页不展示，改为 BeliefTrajectoryTab（详见 §4.3.2 v0.6 信念审计）
- [x] 判决书生成与导出：JSON（`GET /export`）+ PDF（前端 `window.print()` + `@media print`）
- [ ] ⏳ **Docker Compose 一键启动**：配置已写，待 Docker 环境验证
- [x] v0.6 信念审计 trail：`belief_diffs` 表 + `GET /belief-diffs` + 前端 `BeliefDiffCard` + `BeliefTrajectoryTab` + `ConvergenceBadge`
- [x] v0.5 MemoryBus / 私有记忆 / ContextView：详见 §4.5
- [x] 调查发现独立表：`investigation_findings` + `GET /investigations` + 前端 `InvestigatorPanel`

**不做：**
- [ ] 专家证人系统
- [ ] 陪审团多模型投票
- [ ] 真实网络搜索/RAG 的高级语义检索
- [ ] 用户注册与历史记录
- [x] ❌ **不做**：LLM 调用审计可视化（前端）—— 后端 `llm_calls` 表 + `backend/logs/agent_gateway_*.log` 已足够；产品级 dashboard 不在 MVP 范围。决策 2026-07-01。
- [x] ❌ **不做**：v0.5 数据迁移 Phase 1-3 —— 项目处于开发期，数据无保留价值；`private_memory` ↔ `a2a_messages` 双写不做正式切换。决策 2026-07-01。

**v0.7+ 计划（不在 MVP）：**
- [ ] ⏳ **强制立场一致性检查**：LLM-as-judge 打回重生成（详见 §4.3.2）
- [ ] ⏳ **新意度检查**：Jaccard 相似度 > 60% 强制换角度（详见 §4.3.3）
- [ ] ⏳ **300 字发言长度硬截断**：后端 `speakers[i].Content` 长度校验 + 重试（详见 §4.3.3）
- [ ] ⏳ **"已反驳证据"集合跟踪**：禁止引用被反驳且未翻盘的证据（详见 §4.3.3）

### 10.2 第二阶段（2-3 个月后）

- [x] **Agent Gateway**：统一接入、调用审计、Prompt 压缩、Token 预算与成本追踪、限流、Fallback（退避重试）
- [ ] 专家证人 Agent
- [ ] 陪审团多模型投票
- [ ] 历史庭审记录与再审
- [ ] PDF 导出与分享
- [ ] 预设决策模板库（职业、投资、产品路线等）

---

## 11. 风险与挑战

| 风险 | 影响 | 缓解方案 |
|---|---|---|
| **Agent 互相附和** | 辩论失去对抗性 | 不同 prompt/温度、信念引擎、Disagree-or-Commit |
| **Agent 编造证据** | 输出不可信 | 强制证据来源字段 / LLM-as-judge 验证 |
| **WebSearch 引入低质证据** | 污染辩论 | 来源可信度评分、用户可排除证据 |
| **庭审过程冗长** | 用户体验差 | 用户可选庭审模式 / 智能收敛 / 随时直接判决 |
| **Token 成本高** | 多轮多 Agent 消耗大 | Prompt 压缩、Token 预算、限流（Agent Gateway 已实装，可开关对比） |
| **用户不知所措** | 不知道提交什么证据 | Agent 主动提问 / 证据模板 / 跳过机制 |
| **Agent 立场漂移** | 辩论失去对抗性 | 信念引擎 + 强制立场一致性检查 |
| **Agent 重复发言** | 庭审拖沓 | 新意度检查 + 发言长度限制 |

---

## 12. 关键指标

| 指标 | 定义 | 目标 |
|---|---|---|
| **庭审完成率** | 用户完成一次完整庭审的比例 | > 60% |
| **用户插证据次数** | 每场庭审用户主动提交证据数 | > 2 次 |
| **调查员调用率** | 用户主动要求调查员搜索的比例 | > 30% |
| **判决书采纳率** | 用户对判决书表示"有帮助"的比例 | > 70% |
| **平均庭审时长** | 从立案到判决的时间 | 5-10 分钟（标准模式）|
| **Agent 引用证据率** | Agent 发言中引用证据的比例 | > 80% |
| **智能收敛触发率** | 因信念度收敛而提前结束庭审的比例 | > 30% |
| **Agent 提问采纳率** | 用户回答 Agent 主动提问的比例 | > 50% |
| **信念度一致性** | Agent 发言与其信念度方向一致的比例 | > 90% |

---

## 13. 简历叙事建议

```markdown
**决策庭（DecisionCourt）— 多 Agent 法庭式决策辅助平台**
- 针对单一 AI 决策建议片面、不可审计的痛点，设计以"法庭"为隐喻的多 Agent 辩论系统
- 实现控方、辩方、调查员、书记员四类 Agent，完成立案、开庭、举证、质证、判决全流程
- 构建证据可采性评估与信念更新引擎，支持用户实时提交证据并触发 Agent 策略调整
- 设计庭审模式选择与智能收敛机制，平衡决策深度与用户体验
- 实现 Agent 主动提问机制，自动识别信息缺口并引导用户补充关键证据
- 抽象 WebSearch 提供商接口，开发期使用免费搜索，部署期切换 Tavily 保证质量
- 设计 Agent Gateway 扩展架构，支持模型路由、缓存（v0.5+ 已实装压缩 / 预算 / 限流 / Fallback / 文件日志）
- 开发交互式观点地图与立场变化曲线，将复杂决策中的证据影响和争议焦点可视化
- 输出结构化《决策判决书》，包含双方主张、证据链、争议焦点与可执行建议
```

---

## 14. 已确认事项

| 事项 | 决策 |
|---|---|
| 中文名 | **决策庭** |
| 英文名 | **DecisionCourt** |
| 前端技术栈 | Next.js 14 + TypeScript + Tailwind + shadcn/ui |
| 后端技术栈 | Go + Gin + GORM + PostgreSQL + Redis |
| MVP Agent | 控方、辩方、调查员、书记员（4 个） |
| Agent 通信协议 | **A2A（Agent-to-Agent）**，由 Orchestrator 统一路由 |
| 记忆模型 | **公共证据板 + 私有记忆池**：控方、辩方、调查员各自维护私有记忆，互不访问 |
| 模型策略 | 单一模型，通过 prompt/信念引擎/私有记忆保证对抗性 |
| WebSearch | 生产用 Bocha，dev 可用 Mock (v0.8.3 起) |
| 可视化风格 | 极简白底法庭风格 |
| 部署 | Docker Compose 一键启动 |
| **运营阶段（2026-07-04 确认）** | **系统处于测试阶段，对公众开放，按用户每日限制 trial 配额（详见 [ADR 0012 §决策 6 + tech-spec §6.4](../adr/0012-ha-and-concurrency.md)）** |
| **阿里云部署（2026-07-04 确认）** | 单 ECS 2C2G，公网域名 + SSL；架构层面不引入 Redis Pub/Sub / 多实例，详见 [ADR 0012](./adr/0012-ha-and-concurrency.md) + [ADR 0013](./adr/0013-llm-gateway-engineering.md) |
| **用户配额（2026-07-04 确认）** | **测试阶段每用户每天最多 N=5 次 trial（UTC 日界重置），超限返回 429。生产可调到 20，DAU > 5000 触发 Redis 切换，详见 [ADR 0014](./adr/0014-user-rate-limit.md)** |
| Agent Gateway（v0.5+） | 统一接入、审计、Prompt 压缩、Token 预算、限流、Fallback |

---

## 15. 当前进度与状态（截至 2026-07-12 v0.10.17 收尾）

### 15.1 已实现

| 模块 | 状态 | 说明 |
|---|---|---|
| **静默错误全局修复 v0.10.17** | ✅ 已实装 | 7 个 PR（PR 1 后端 UFE + PR 2-4 前端 Toast/errorBus + PR 5 文档 + PR 7 ErrorBoundary），详见 [ADR 0024 §2-§8](./adr/0024-silent-error-fix-pr1.md) + [release-notes/v0.10.17.md](./release-notes/v0.10.17.md) + [§4.6.3 错误反馈 UX](#463-错误反馈-uxv01017-silent-error-fix) |
| 项目骨架 | ✅ | `frontend/`、`backend/`、`docker-compose.yml`、`.env.example`、README 已创建 |
| 前端页面 | ✅ | 首页/立案页、庭审主界面（含右侧 Tab + 调查活动面板）、判决书页，白色极简主题 + 凹陷输入框 |
| 前端 Real 驱动 | ✅ | Zustand store + 真实 WebSocket 订阅所有 ReAct 事件，pnpm tsc 通过 |
| 前端编译 | ✅ | `pnpm build` 通过 |
| 后端 LLM 客户端 | ✅ | DeepSeek 客户端含 `Complete` + `StreamComplete` 流式方法 |
| 后端 Agent 编排 | ✅ | Orchestrator + Prompt 模板 + ReAct 协议（action / tool_call / reflect / speak） |
| 后端庭审状态机 | ✅ | idle → opening → cross_exam → closing → deliberation → verdict（v0.8.3 新增 `verdict → evidence` fast-path via `reopen_trial`）|
| 后端证据服务 | ✅ | 证据提交、影响评估（**仅**用户证据） |
| 后端 WebSocket | ✅ | Hub + Room 广播，hub.Broadcast sleep 30ms 保流式帧间隔 |
| 后端判决书 | ✅ | ClerkAgent 生成结构化判决书 |
| 后端编译 | ✅ | `go build` 通过 |
| 架构设计 | ✅ | A2A 协议、私有记忆池、信念引擎、ReAct、流式 写入 PRD/Agent/技术文档 |
| **WebSearch（Bocha）** | ✅ | `internal/search/bocha.go` 实装,HTTP 200 实测。SearXNG/DuckDuckGo 已在 v0.8.3 删除 |
| **A2A 消息总线** | ✅ | `internal/a2a` 包实装：Bus + Repository 接口 + InMemory/GORM 实现 + 12 项隔离测试。`A2AMessage` 表 + 公开/私有可见性 + Orchestrator `a2a.message` 审计广播 + `dispatch_investigator` / `investigation_report` 公开 |
| **私有记忆池** | ✅ | `internal/private_memory` 包实装：Repository 接口 + InMemory/GORM 实现 + 9 项隔离测试。`PrivateMemory` 表 + 四类记忆 + Orchestrator `ProsecutorSpeak/DefenderSpeak` 自动写 `strategy_note` |
| **ReAct 循环** | ✅ | `internal/agent/react_runner.go` 实装 ReAct 协议 + `OnIterStart` / `OnSpeakChunk` 钩子 + `ActionReflect` |
| **LLM 流式** | ✅ | 后端 `llm.Client.StreamComplete` + `hub.Broadcast` sleep 30ms + 前端 `applySpeakChunk` 用 `flushSync` 强制 commit |
| **调查发现独立表** | ✅ | `internal/investigation/` 包实装 + `investigation_findings` 表 + `GET /investigations` 端点 + 10 项测试。`evidence` 表**不**会被调查员搜索写入 |
| **Avatar 头部气泡** | ✅ | 优先级：调查 > 流式 > 思考 > 完整发言。`AgentAvatar.tsx` 订阅 `store.streamingContent` / `activeThinking` / `activeInvestigation` |
| **调查员视觉** | ✅ | `InvestigatorPanel.tsx` + Avatar isSearching 状态 + 状态机 dispatch→report 升级（单条 entry，不重复） |

### 15.2 进行中 / 待验证 / v0.7+ 计划

| 模块 | 状态 | 说明 |
|---|---|---|
| 真实后端联调 | ⏳ | PostgreSQL 在跑，依赖真实 LLM API key |
| Docker Compose 验证 | ⏳ | Docker 未安装，无法一键启动 |
| 信念引擎动态更新 | ✅ | 已实装：`updateBeliefsAndBroadcast` 在 SubmitEvidence/搜索结果后触发 |
| 智能收敛 | ✅ | `service.go:559 isConverged` 实现，多信号按优先级触发，trial.converged 事件已广播 |
| React 18 batching 与 LLM 流式冲突已解决 | ✅ | `flushSync` 在 `applySpeakChunk` 强制同步 commit |
| **A2A ContextView 投影（v0.5 PR 1）** | ✅ | 已实装 `internal/a2a/context_view.go` + `BuildContextView()`，剥离对方 reasoning；详见 [memory-a2a-redesign.md](../.trae/documents/memory-a2a-redesign.md) |
| **Episodic Memory via A2A（v0.5 PR 2 ✅ / PR 3 ✅）** | ✅ | PR 2 ✅ + PR 3 ✅ 都已实装：ReAct reflect 自动写记忆（14 项单测）+ Orchestrator 在 system prompt 注入"## 你之前的策略笔记"段落（10 项单测） |
| **前端 MemoryAuditPanel（v0.5 PR 4）** | ✅ | 已实装 MemoryAuditPanel + MemoryTimeline + BehindTheScenesPanel；「策略笔记」tab + 真实法庭 toggle + verdict 页幕后视角 |
| **v0.6 BeliefTrajectoryTab + ConvergenceBadge + BeliefDiffCard** | ✅ | 信念审计 trail 完整实装 |
| **Verdict 页面 trial_summary + 导出（JSON + PDF）** | ✅ | `ClerkPromptWithJudgeDecision` 输出 trial_summary；`GET /export` 端点 + Content-Disposition；前端 `window.print()` + `@media print` |
| **质证阶段轮次控制** | ✅ | `round.waiting_for_user` 事件 + `continue_cross_exam` action + 前端"开始第 N+1 轮"按钮（详见 `.trae/documents/质证阶段轮次控制修改计划.md`） |
| **强制立场一致性检查** | ⏳ v0.7+ 计划 | LLM-as-judge 打回重生成未实装；当前 LLM 直接生成，立场漂移靠 Prompt 自约束（详见 §4.3.2 / §4.3.3）|
| **新意度检查** | ⏳ v0.7+ 计划 | Jaccard 相似度计算未实装；重复发言靠 Prompt 自约束（详见 §4.3.3）|
| **300 字发言长度硬截断** | ⏳ v0.7+ 计划 | 后端未做长度校验 + 重试；Prompt 自约束（详见 §4.3.3）|
| **"已反驳证据"集合跟踪** | ⏳ v0.7+ 计划 | 未实装证据反驳状态机（详见 §4.3.3）|

### 15.3 明确不做（MVP 范围 / 决策日期 2026-07-01）

- ❌ 问题澄清与选项生成（用户只给模糊问题时）
- ❌ Agent 主动提问（识别信息缺口后向用户提问，回答转为证据）
- ❌ Agent Gateway 完整版中的模型路由 / 响应缓存（留第二阶段）
- ❌ 专家证人、陪审团、历史庭审、PDF 导出
- ❌ **LLM 调用审计可视化（前端）**：后端 `llm_calls` 表 + `backend/logs/agent_gateway_*.log` 已足够；产品级 dashboard 不增加。决策 2026-07-01。
- ❌ **v0.5 数据迁移 Phase 1-3**：项目处于开发期，数据无保留价值；`private_memory` ↔ `a2a_messages` 双写不做正式切换。决策 2026-07-01。详见 [memory-a2a-redesign.md §3](../.trae/documents/memory-a2a-redesign.md#3-双写迁移时间线pr-4-完成后启动)。

### 15.4 当前阻塞

1. **真实 LLM API key 未配置**：依赖 DeepSeek API key 才能联调真实庭审。
2. **Docker 未安装**：无法使用 `docker-compose` 验证完整环境。
3. **建议**：配置 `LLM_API_KEY` + 安装 Docker Desktop 后再端到端验证。

---

## 16. 下一步：任务拆分

当前文档已落地 A2A 协议与私有记忆设计。下一步建议优先解决环境阻塞，再推进功能：

1. **解决环境阻塞**：安装 Docker / 启动 PostgreSQL，验证后端能运行。
2. **信念引擎动态化**：实现证据进入后的信念度更新公式。
3. **智能收敛**：实现连续两轮信念度变化 < 5% 提前结束庭审。
4. **真实搜索接入**：接入 SearXNG 或 Tavily 替换 MockProvider。
5. **端到端联调**：前端连接真实后端，跑通立案 → 庭审 → 判决。
6. **代码实现 A2A + 私有记忆** ✅：已完成。`internal/a2a` + `internal/private_memory` 已实装；Orchestrator 每次发言自动通过 Bus 发 speech 消息 + 写 strategy_note。后续演进：把"调用 LLM 前剥离对方 reasoning"也接入 Bus 的 ContextView（当前只发了消息但 LLM 仍能读到对方的 reasoning，需要在下一步实现视图投影）。

是否需要我先输出一份**技术设计文档（TDD）** 和 **任务清单**，然后直接开始写代码？

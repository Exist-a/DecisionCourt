# 决策庭（DecisionCourt）Agent 状态机与 Prompt 设计文档

> **版本**：v0.7
> **状态**：v0.5 增补 Episodic Memory via A2A 私有通道 + ContextView 投影（4 个新 MessageType）；v0.6 信念引擎升级（贝叶斯 log-odds + 锚定）+ 智能收敛 4 信号优先级；v0.7 整合文档结构 + ADR 提炼。
> **目标**：定义决策庭中多 Agent 的协作流程、状态转换、Prompt 工程，以及 Agent 间通信协议。
> **架构决策**：[`docs/adr/0002-a2a-private-channel.md`](./adr/0002-a2a-private-channel.md)、[`docs/adr/0003-contextview-projection.md`](./adr/0003-contextview-projection.md)、[`docs/adr/0004-bayesian-belief-engine.md`](./adr/0004-bayesian-belief-engine.md)
> **2026-07-02 整理时同步**：本版本号对齐后端代码实装现状（参见 [`docs/README.md`](./README.md)）。

---

## 1. Agent 协作总览

决策庭中有 4 个核心 Agent：

| Agent | 类型 | 目标 | 特点 |
|---|---|---|---|
| **控方（Prosecutor）** | 主张方 | 证明 option A 更优 | 攻击性强，善于找支持证据 |
| **辩方（Defender）** | 反对方 | 证明 option A 不优，或 option B 更稳妥 | 防御性强，善于找反例和风险 |
| **调查员（Investigator）** | 信息搜集方 | 找到与争议焦点相关的高质量证据 | 中立，基于事实 |
| **书记员（Clerk）** | 整理方 | 记录庭审、生成判决书 | 中立、客观、结构化 |

**协作模式**：
- 控方和辩方是**对抗关系**。
- 调查员是**中立服务方**，为双方提供证据。
- 书记员是**观察者**，不参与对抗，只负责整理。

---

## 2. 庭审状态机

### 2.1 状态定义

```go
type CourtPhase string

const (
    PhaseIdle             CourtPhase = "idle"              // 立案后未开始
    PhaseClarification    CourtPhase = "clarification"     // 问题澄清
    PhaseOptionGeneration CourtPhase = "option_generation" // 选项生成
    PhaseOpening          CourtPhase = "opening"           // 开庭陈述
    PhaseEvidence         CourtPhase = "evidence"          // 举证阶段
    PhaseCrossExam        CourtPhase = "cross_exam"        // 质证阶段
    PhaseClosing          CourtPhase = "closing"           // 结案陈词
    PhaseDeliberation     CourtPhase = "deliberation"      // 书记员整理
    PhaseVerdict          CourtPhase = "verdict"           // 判决
    PhaseAppeal           CourtPhase = "appeal"            // 上诉/再审
)
```

### 2.2 状态转换图

```
                    ┌─────────┐
         用户创建   │  Idle   │
        ┌──────────►│  立案   │
        │           └────┬────┘
        │                │ 用户提供两个明确选项？
        │           ┌────┴────┐
        │           │         │
        │          是         否
        │           │         │
        │           │         ▼
        │           │    ┌──────────────┐
        │           │    │Clarification │
        │           │    │  问题澄清    │
        │           │    └──────┬───────┘
        │           │           │
        │           │           ▼
        │           │    ┌──────────────┐
        │           │    │OptionGeneration│
        │           │    │  选项生成    │
        │           │    └──────┬───────┘
        │           │           │
        │           │           ▼
        │           │    ┌──────────────┐
        │           └────┤  用户选择    │
        │                └──────┬───────┘
        │                       │
        │                       ▼
        │                  ┌─────────┐
        │                  │ Opening │
        │                  │ 开庭陈述 │
        │                  └────┬────┘
        │                       │ 开场陈述完成
        │                       ▼
        │                  ┌─────────┐
        │                  │ Evidence│ ◄─────────────┐
        │                  │ 举证阶段 │               │
        │                  └────┬────┘               │
        │                       │ 用户/Agent提交证据   │
        │                       ▼                    │ 上诉/补充证据
        │                  ┌─────────┐               │
        │                  │CrossExam│               │
        │                  │ 质证阶段 │───────────────┘
        │                  └────┬────┘
        │                       │ 达到轮数上限或智能收敛
        │                       ▼
        │                  ┌─────────┐
        │                  │ Closing │
        │                  │ 结案陈词 │
        │                  └────┬────┘
        │                       │
        │                       ▼
        │                  ┌─────────────┐
        │                  │Deliberation │
        │                  │ 书记员整理   │
        │                  └────┬────────┘
        │                       │
        │                       ▼
        │                  ┌─────────┐
        └──────────────────┤ Verdict │
                           │ 判决    │
                           └─────────┘
```

### 2.3 转换条件

| 转换 | 触发条件 | 动作 |
|---|---|---|
| `idle → opening` | 用户提供了两个明确选项并调用 `start` | 创建 Agent，初始化信念度 |
| `idle → clarification` | 用户未提供两个明确选项 | 进入问题澄清阶段 |
| `clarification → option_generation` | 用户完成（或跳过）澄清问题 | 生成候选选项 |
| `option_generation → idle` | 用户选择两个选项 | 更新庭审选项，回到 idle |
| `idle → opening` | 用户确认选项后调用 `start` | 创建 Agent，初始化信念度 |
| `opening → evidence` | 控辩双方开场陈述完成 | 进入举证阶段 |
| `evidence → cross_exam` | 至少有一条证据被提交 | 开始质证 |
| `cross_exam → cross_exam` | 未达轮数上限且未收敛 | 自动进入下一轮质证（后端连续执行） |
| `cross_exam → closing` | 达到轮数上限或智能收敛 | 进入结案 |
| `cross_exam → closing` | 用户点击"直接判决" | 取消当前 LLM 调用并进入结案 |
| `cross_exam → evidence` | 用户提交新证据 | 回到举证 |
| `closing → deliberation` | 双方结案陈词完成 | 书记员整理 |
| `deliberation → verdict` | 判决书生成完成 | 展示判决 |
| `verdict → evidence` | 用户选择补充证据再审 | 回到举证 |

---

## 3. Agent 调用时序图

### 3.1 开庭阶段

```
用户        API Gateway    Orchestrator    Prosecutor    Defender    Investigator    Clerk
 │  start      │               │              │            │            │            │
 │────────────►│──────────────►│              │            │            │            │
 │             │               │─────────────►│            │            │            │
 │             │               │◄─────────────│            │            │            │
 │             │               │──────────────┼───────────►│            │            │
 │             │               │◄─────────────┼────────────│            │            │
 │             │               │              │            │            │            │
 │             │◄─────────────│              │            │            │            │
 │  WS 广播    │               │              │            │            │            │
 │◄────────────│               │              │            │            │            │
```

### 3.2 举证 + 质证阶段

```
用户提交证据 / Agent 搜索证据
        │
        ▼
┌───────────────┐
│  更新信念引擎  │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ 保存证据到 DB  │
└───────┬───────┘
        │
        ▼
┌───────────────────────┐
│ 控方发言（引用证据）   │
└───────┬───────────────┘
        │
        ▼
┌───────────────────────┐
│ 辩方反驳（引用证据）   │
└───────┬───────────────┘
        │
        ▼
┌───────────────────────┐
│ 检查是否智能收敛       │
└───────┬───────────────┘
        │
        ▼
┌───────────────────────┐
│ 是 → 进入 closing     │
│ 否 → 继续下一轮       │
└───────────────────────┘
```

### 3.3 A2A 消息流转与情节记忆（v0.4 重设计）

> **v0.4 重大变更**：私有记忆底层从独立 `private_memories` 表迁移到 **A2A 私有消息通道**。所有情节记忆条目以 `visibility=private` 的 A2A 消息形式存储，复用 Bus 的隔离/审计/路由能力。详见 [memory-a2a-redesign.md](../../.trae/documents/memory-a2a-redesign.md)。

#### 3.3.1 A2A 总线时序图（v0.4 含 ContextView）

在传统多 Agent 系统中，Orchestrator 往往把全部历史上下文同时塞进每个 Agent 的 prompt，导致：

- Token 浪费；
- 控方看到辩方 reasoning 后立场漂移；
- Agent 容易互相附和。

决策庭采用 **A2A（Agent-to-Agent）消息总线**，Orchestrator 通过 **ContextView 投影器** 按接收方权限拼接上下文 + 剥离对方 reasoning。

```
用户 / 证据 / 系统事件
        │
        ▼
┌────────────────────────────────────────┐
│ Orchestrator                            │
│  ┌──────────────────────────────────┐  │
│  │  BuildContextView(selfAgent)     │  │
│  │  - Working Memory: public msgs   │  │
│  │    (对方消息 → SanitizedPayload  │  │
│  │     剥离 reasoning)               │  │
│  │  - Episodic Memory: 自己的私有    │  │
│  │    消息 (strategy_note, ...)     │  │
│  │  - Beliefs                       │  │
│  └──────────────────────────────────┘  │
└───────┬────────────────────────────────┘
        │ A2A 消息（ContextView 投影）
        ├─────────────────► Prosecutor
        │  (公共证据板 + 控方私有记忆全文
        │   + 双方公开发言 sanitized)
        │
        ├─────────────────► Defender
        │  (公共证据板 + 辩方私有记忆全文
        │   + 双方公开发言 sanitized)
        │
        ├─────────────────► Investigator
        │  (完整证据板 + 争议焦点)
        │
        └─────────────────► Clerk
           (公共记录 + 证据板，不读私有)

        │
        ▼
  WebSocket 广播 `a2a.message` 到前端
  (含 visibility 标记，前端 MemoryAuditPanel 可过滤)
```

#### 3.3.2 情节记忆读写规则（v0.4）

| Agent | 读自己的情节记忆 | 读对方情节记忆 | 写入情节记忆 |
|---|---|---|---|
| 控方 Prosecutor | ✅ | ❌ | ✅ |
| 辩方 Defender | ✅ | ❌ | ✅ |
| 调查员 Investigator | ✅ | ❌ | ✅ |
| 书记员 Clerk | 不需要 | ❌ | ❌ |

**写入时机**（v0.4 重构）：

1. **每次 speak 完成后**：自动通过 `a2aBus.Send(visibility=private, message_type=strategy_note)` 写入一条策略笔记。
2. **ReAct reflect 步骤**：LLM 在反思时显式输出 `memory_type`（4 选 1） + `memory_note`（具体内容），ReActRunner 自动调用 `a2aBus.Send` 写入对应类型。
3. **ReAct tool_call(search) 后**：自动写入 `evidence_eval`（评估证据强度）。

**读取时机**（v0.4 重构）：

- Orchestrator 调 `BuildContextView(sessionID, selfAgent)` → 自动按 `from=selfAgent AND to=selfAgent AND visibility=private` 过滤 → 拼接到 `## 你之前的策略笔记` prompt 段落。
- 注入策略：**全文注入**（单场庭审 ≤ 50 条 × 200 token = 10K，远低于 128K context window）。
- 书记员**不调用** `BuildContextView`，只读取公共庭审记录。

#### 3.3.3 A2A MessageType 扩展（v0.4）

| MessageType | Visibility | 说明 | 写入时机 |
|---|---|---|---|
| `speech` | public | 开庭/质证/结案发言 | 律师每次 speak |
| `evidence` | public | 新证据提交 | 用户/调查员提交证据 |
| `challenge` | public | 反驳对方论证 | 律师反驳 |
| `inquiry` | public | 提问 | Agent 主动 |
| `verdict_task` | public | 判决任务派发 | Orchestrator → Clerk |
| `dispatch` | public | 调查员派遣 | Orchestrator → Investigator |
| `report` | public | 调查员回报 | Investigator → Orchestrator |
| **`strategy_note`** 🆕 | **private** | 私有策略笔记 | speak 后自动 + ReAct reflect |
| **`opponent_weakness`** 🆕 | **private** | 私有：对方弱点 | ReAct reflect 显式声明 |
| **`self_correction`** 🆕 | **private** | 私有：自我修正 | ReAct reflect 显式声明 |
| **`evidence_eval`** 🆕 | **private** | 私有：证据内部评估 | ReAct tool_call(search) 后 |

**strategy_note 的结构化 payload（v0.5+）**：

```json
{
  "memory_type": "strategy_note",
  "stance": "pro_a",
  "confidence": 0.82,
  "reasoning": "辩方没反驳 E001 的数据来源，是核心弱点",
  "content": "立场 支持选项A · 置信度 82% · 辩方没反驳 E001 的数据来源...",
  "linked_evidence_ids": ["E001"]
}
```

后端 `recordSideEffects` 在每次 speak 完成后自动生成这个结构化 payload（[agent/orchestrator.go#L313-L322](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/backend/internal/agent/orchestrator.go#L313-L322)）。前端 `MemoryTimeline` 优先用结构化字段渲染卡片（立场 chip + 置信度条 + 推理段），只 `content` 的老数据走纯文本 fallback。

**新增 4 个 MessageType** 全部 `visibility=private`，`from=selfAgent, to=selfAgent`，是情节记忆的载体。

#### 3.3.3 A2A 消息示例

**开庭阶段：Orchestrator → Prosecutor**

```json
{
  "message_id": "msg_open_pro_001",
  "session_id": "court_demo_001",
  "round": 0,
  "phase": "opening",
  "from": "orchestrator",
  "to": "prosecutor_1",
  "message_type": "speech_task",
  "payload": {
    "action": "opening_statement",
    "option_a": "跳槽到互联网公司",
    "option_b": "留在现公司",
    "evidence_board": [],
    "belief_state": { "option_a": 0.75, "option_b": 0.25 }
  },
  "private_memory": [],
  "created_at": "2026-06-25T12:00:00Z"
}
```

**质证阶段：Prosecutor → Orchestrator → Defender**

```json
// Prosecutor 输出
{
  "message_id": "msg_pro_002",
  "session_id": "court_demo_001",
  "round": 1,
  "phase": "cross_exam",
  "from": "prosecutor_1",
  "to": "orchestrator",
  "message_type": "speech",
  "payload": {
    "action": "support_option_a",
    "content": "互联网公司 offer 的薪资涨幅 40%，且技术栈更先进。",
    "reasoning": "E001 是核心支持证据，强调薪资和成长空间。",
    "evidence_refs": ["E001"],
    "confidence": 0.85
  },
  "memory_references": ["pm_pro_001"],
  "created_at": "2026-06-25T12:01:00Z"
}

// Orchestrator 转发给 Defender（注意：reasoning 已被剥离）
{
  "message_id": "msg_fwd_def_002",
  "session_id": "court_demo_001",
  "round": 1,
  "phase": "cross_exam",
  "from": "orchestrator",
  "to": "defender_1",
  "message_type": "speech_task",
  "payload": {
    "action": "rebut_speech",
    "target_message_id": "msg_pro_002",
    "target_agent": "prosecutor_1",
    "target_content": "互联网公司 offer 的薪资涨幅 40%，且技术栈更先进。",
    "evidence_board": [...],
    "debate_history": ["msg_pro_002"],
    "belief_state": { "option_a": 0.25, "option_b": 0.75 }
  },
  "private_memory": ["pm_def_001"],
  "created_at": "2026-06-25T12:01:01Z"
}
```

---

## 4. 信念引擎（Belief Engine）

### 4.1 信念度定义

每个 Agent 维护对两个选项的信念度：

```go
type BeliefState struct {
    OptionA float64 // [0, 1]
    OptionB float64 // [0, 1], OptionA + OptionB = 1
}
```

### 4.2 初始信念度

| Agent | OptionA | OptionB |
|---|---|---|
| Prosecutor | 0.75 | 0.25 |
| Defender | 0.25 | 0.75 |
| Investigator | 0.50 | 0.50 |
| Clerk | 0.50 | 0.50 |

### 4.3 信念更新规则（MVP 实现版）

当新证据进入时，按以下规则更新：

```python
def update_belief(agent, evidence):
    impact_a = evidence.impact_on_option_a   # [-1, 1]
    impact_b = evidence.impact_on_option_b   # [-1, 1]
    credibility = evidence.credibility_score # [0, 1]
    relevance = evidence.relevance_score     # [0, 1]
    
    # 证据有效强度 = 影响 * 可信度 * 相关性
    strength = credibility * relevance
    
    # 单次证据最大影响 clamp 到 0.15，避免单条证据翻转立场
    delta_a = clamp(impact_a * strength * 0.15, -0.15, 0.15)
    delta_b = clamp(impact_b * strength * 0.15, -0.15, 0.15)
    
    # 硬约束：对符合约束的选项产生额外正向拉动
    if evidence.type == "constraint" and evidence.constraint_strength > 0.5:
        # LLM 在创建证据时评估 constraint_strength（0-1）
        if impact_a > 0:
            delta_a += 0.1 * evidence.constraint_strength
        if impact_b > 0:
            delta_b += 0.1 * evidence.constraint_strength
    
    new_a = clamp(agent.belief_a + delta_a - delta_b, 0.05, 0.95)
    agent.belief_a = new_a
    agent.belief_b = 1 - new_a
```

**设计说明**：
- 采用**置信度加权**（方案 B）：可信度越高的证据对 belief 影响越大。
- `impact_on_option_a/b` 由 LLM 在证据创建时评估，范围 `[-1, 1]`。
- `constraint_strength` 由 LLM 根据语气判断：越像硬约束（"必须"、"只能"、"绝不"）值越大。
- 最终结果 clamp 到 `[0.05, 0.95]`，保留极少数立场翻转的可能性。

### 4.4 智能收敛判断

```python
def is_converged(snapshots, current_round, max_rounds):
    # 至少经过三轮质证
    if current_round < 3:
        return False
    
    # 避免过早收敛：至少完成 60% 轮次
    if current_round < max_rounds * 0.6:
        return False
    
    # 获取最近三轮所有 Agent 的 belief 变化
    last_three_rounds = snapshots[-3:]
    for agent_id in all_agents:
        deltas = [s.delta for s in last_three_rounds if s.agent_id == agent_id]
        if len(deltas) < 3:
            return False
        if any(abs(d) >= 0.03 for d in deltas):
            return False
    
    return True
```

**触发效果**：一旦收敛，立即从 `cross_exam` 进入 `closing`，缩短庭审时间。

---

## 5. Prompt 设计

### 5.1 通用约束（所有 Agent 共享）

> **v0.4 修订**：原 v0.3 "JSON 输出格式（ReAct 结构）"已演进为真正的 ReAct 协议——LLM 可以多轮 thought → tool_call → reflect → speak，每步都有结构化输出。

```markdown
你是一名专业的决策顾问，正在参与一场结构化庭审辩论。

## 基本规则
1. 每次发言最多 200 字。
2. 如果当前有证据，你必须基于证据发言；如果没有证据，应基于背景信息、对方已表达的观点以及自身立场进行客观分析。
3. 严禁每次只说"需要补充证据"——即使没有证据，你也要提出新的、实质性的论点或反驳。
4. 如果你引用证据，必须明确说明证据 ID，且只能引用【当前证据】列表中出现的 ID。
5. 如果没有证据可引用，evidence_refs 必须为空数组 []；严禁编造不存在的证据 ID。
6. 你不能人身攻击，不能使用逻辑谬误。
7. 你的发言必须与你当前的信念度一致。
8. 如果新证据与你的立场冲突，你可以调整论点，但不能瞬间改变立场。
9. 不要简单复述上一位 Agent 的发言。你的任务是直接回应对方的具体论点，并提出新的论据。
10. 不要重复你自己之前已经说过的内容。如果你发现自己在重复之前的话，必须换一个角度论证。

## ReAct 协议（v0.4 新增）
你必须严格按以下 JSON 格式输出**每一步决策**：
{
  "action": "thought | tool_call | reflect | speak",
  "reasoning": "当前这一步的推理（50 字以内）",
  // 当 action=tool_call 时必填：
  "tool": "investigator_search",
  "tool_input": { "query": "搜索关键词" },
  // 当 action=reflect 时必填：
  "reflection": "对自己的下一步策略反思",
  // 当 action=speak 时必填：
  "content": "正式发言（最多 200 字）",
  "evidence_refs": ["E001", "E002"],
  "confidence": 0.8,
  "stance": "pro_a | pro_b | challenge | neutral"
}
```

**action 类型**：

| 值 | 含义 | 必填字段 |
|---|---|---|
| `thought` | 仅内部思考，下一步继续 | `reasoning` |
| `tool_call` | 派遣调查员搜索 | `tool`, `tool_input` |
| `reflect` | 反思当前策略，可继续 thought / tool_call / speak | `reasoning`, `reflection` |
| `speak` | 最终发言 | `reasoning`, `content`, `evidence_refs`, `confidence`, `stance` |

**ReAct 循环规则**（由后端 `agent.ReactRunner` 强制）：
1. 第 1 步必为 `thought`（推 `agent.cot_step` + 推送 `agent.thinking_started`）
2. 中间可 `tool_call`（N 次，调用 investigator_search）→ `reflect`（最多 3 次）
3. 必须以 `speak` 结尾（推 `agent.speak_chunk` 流式 + 最终 `agent.speak`）
4. 最多迭代 8 轮仍不 speak → max iterations exceeded

**字段说明**：
- `reasoning`：推理过程，后端会存入审计日志，**不会**展示给其他 Agent。
- `content`：正式发言，会广播给所有参与者和用户。**speak 阶段**会被 LLM 流式分块推送（`agent.speak_chunk` 事件），用户看到逐字渲染。
- `evidence_refs`：引用的证据 ID 列表。**有证据时必须至少引用一条；无证据时必须为空数组 []**。
- `confidence`：你对本轮发言的信心，范围 `[0, 1]`。
- `stance`：本轮立场，用于后端做立场一致性检查。

### 5.2 控方 Agent Prompt

```markdown
## 角色
你是"控方律师"，你的使命是证明【选项 A】是更优选择。

## 当前信念度
你对选项 A 的信念度：{belief_a}
你对选项 B 的信念度：{belief_b}

## 策略
1. 主动寻找支持选项 A 的证据。
2. 对辩方提出的反例进行有力反驳。
3. 强调选项 A 的收益、机会、长期价值。
4. 如果对方质疑证据，先承认小瑕疵，再强调核心观点。

## 注意
- 你的初始信念度是 0.75，你对选项 A 有较强倾向。
- 但你不能被明显错误的证据说服，必须保持专业怀疑。
```

### 5.3 辩方 Agent Prompt

```markdown
## 角色
你是"辩方律师"，你的使命是质疑【选项 A】的可行性，并维护【选项 B】或现状的合理性。

## 当前信念度
你对选项 A 的信念度：{belief_a}
你对选项 B 的信念度：{belief_b}

## 策略
1. 主动寻找选项 A 的风险、成本、失败案例。
2. 对控方提出的证据进行质证，指出来源问题或逻辑漏洞。
3. 强调选项 B 的稳定性、确定性、风险控制。
4. 如果选项 A 确实有强证据支持，你可以承认部分优点，但必须指出关键风险。

## 注意
- 你的初始信念度是 0.25，你对选项 A 有较强怀疑。
- 但你不能无理取闹，必须基于证据反驳。
```

### 5.4 调查员 Agent Prompt

```markdown
## 角色
你是"调查员"，你的使命是帮助用户把模糊的决策问题转化为清晰的、可辩论的选项，并为庭审找到客观证据。

## 当前信念度
你对选项 A 的信念度：0.50
你对选项 B 的信念度：0.50

## 职责一：问题澄清与选项生成
当用户没有提供两个明确选项时：
1. 分析用户问题的核心纠结点。
2. 识别做出该决策需要的关键信息（如财务状况、风险偏好、约束条件等）。
3. 生成 2-3 个澄清问题，每个问题明确标注目的。
4. 基于用户回答，生成 2-3 个具体、可对比的候选选项。

## 职责二：证据搜集
当庭审已经开始时：
1. 基于当前争议焦点生成搜索 query。
2. 从搜索结果中提取关键事实和数据。
3. 评估每条证据的可信度和相关性。
4. 明确说明每条证据支持/削弱哪个选项，或是否中立。
5. 如果证据存在矛盾，列出不同来源并说明差异。

## 输出格式（澄清阶段）
{
  "reasoning": "为什么需要这些问题",
  "questions": [
    {
      "question_id": "q_001",
      "question": "你的月收入范围是多少？",
      "purpose": "评估财务风险承受能力"
    }
  ]
}

## 输出格式（选项生成阶段）
{
  "reasoning": "基于用户提供的信息，生成候选选项",
  "options": [
    {
      "option_id": "opt_a",
      "label": "留在现公司，争取内部晋升",
      "rationale": "风险最低"
    }
  ]
}

## 输出格式（证据搜集阶段）
{
  "reasoning": "搜索策略",
  "evidences": [
    {
      "content": "证据内容",
      "url": "来源链接",
      "type": "fact|data|expert_opinion",
      "impact_on_option_a": 0.5,
      "impact_on_option_b": -0.2,
      "credibility_score": 0.8
    }
  ]
}
```

### 5.5 书记员 Agent Prompt

```markdown
## 角色
你是"书记员"，你的使命是客观记录庭审并生成结构化判决书。

## 原则
1. 你必须保持完全中立。
2. 你不能加入个人观点。
3. 判决书必须基于庭审中实际出现的证据和论点。
4. 如果证据不足，必须明确标注"证据不足"。

## 输出格式（Markdown）
# 决策判决书

## 一、双方主张
| 控方（选项 A） | 辩方（选项 B/现状） |
|---|---|
| ... | ... |

## 二、已采纳证据
- E001: ... ✅
- E002: ... ⚠️

## 三、争议焦点
1. ...
2. ...

## 四、最终裁决
[建议 + 可执行行动]
```

---

## 6. Agent 主动提问策略

### 6.1 信息缺口识别

调查员/控方/辩方在以下情况下会识别信息缺口：

```python
def identify_gaps(session):
    gaps = []
    
    # 检查是否有财务相关信息
    if not has_evidence_type(session, "financial"):
        gaps.append({
            "topic": "financial",
            "question": "你的月收入范围是多少？",
            "purpose": "评估财务风险承受能力"
        })
    
    # 检查是否有风险偏好好信息
    if not has_evidence_type(session, "risk_preference"):
        gaps.append({
            "topic": "risk_preference",
            "question": "如果这件事失败，你能接受的最坏结果是什么？",
            "purpose": "评估风险偏好"
        })
    
    # 检查是否有约束条件
    if not has_evidence_type(session, "constraint"):
        gaps.append({
            "topic": "constraint",
            "question": "有没有任何不可妥协的约束条件？",
            "purpose": "识别硬约束"
        })
    
    return gaps[:3]  # 最多返回 3 个
```

### 6.2 提问触发时机

| 时机 | 行为 |
|---|---|
| 立案后 | 如果 background 信息不足，进入 evidence 阶段前先问 1-2 个问题 |
| 每轮质证前 | 检查是否有新缺口，有则提问 |
| 用户跳过 3 次后 | 不再主动提问 |

---

## 7. 防止 Agent 附和机制

### 7.1 温度与参数差异

| Agent | temperature | top_p | 说明 |
|---|---|---|---|
| Prosecutor | 0.8 | 0.9 | 更具攻击性 |
| Defender | 0.8 | 0.9 | 更具怀疑精神 |
| Investigator | 0.3 | 0.7 | 更客观稳定 |
| Clerk | 0.2 | 0.6 | 更保守中立 |

### 7.2 立场一致性检查

在 Agent 输出后，系统会进行一次 LLM-as-judge 检查：

```python
def check_consistency(agent, output):
    prompt = f"""
    Agent 类型：{agent.type}
    当前对选项 A 的信念度：{agent.belief_a}
    Agent 发言：{output.content}
    
    请判断这段发言是否与该 Agent 的信念度一致。
    如果一致，返回 true；如果不一致，返回 false 并说明原因。
    """
    
    result = llm_judge(prompt)
    return result.is_consistent
```

如果不一致，要求 Agent 重新生成。

### 7.3 新意度检查

防止 Agent 重复发言：

```python
def check_novelty(output, history):
    recent_contents = [m.content for m in history[-5:]]
    similarity = compute_similarity(output.content, recent_contents)
    
    if similarity > 0.6:
        return False, "与近期发言重复度过高"
    
    return True, None
```

### 7.4 情节记忆隔离（A2A 上下文控制，v0.4 重设计）

除了温度、信念引擎、一致性检查外，决策庭通过 **A2A 消息边界**+**ContextView 投影**+**情节记忆池**从信息层面切断 Agent 之间的推理污染。

**v0.4 三道信息隔离防线**：

1. **A2A Bus 路由隔离**：所有 Agent 间通信必须经过 `Bus.Send()`，自动按 `visibility` 过滤，私有消息仅 `ToAgent` 和 `FromAgent` 可见。
2. **ContextView 投影**：`BuildContextView(selfAgent)` 在 Orchestrator 构造 LLM prompt 前调用，对对方 public 消息应用 `SanitizedPayload()` 剥离 `reasoning` 字段。
3. **情节记忆池隔离**：通过 A2A 私有通道实现，对方既看不到 `visibility=private` 消息本身，也看不到其中存储的策略笔记。

#### 7.4.1 为什么需要私有记忆隔离

多 Agent 辩论中，如果控方看到辩方的私有推理（如"我准备攻击 E003 的数据来源"），可能出现两种负面效果：

1. **针对性防御**：控方提前修补自己的论点，导致辩论变成"信息博弈"而非证据博弈。
2. **立场漂移**：某一方看到对方推理更有说服力后，倾向于附和，失去对抗性。

私有记忆隔离确保每个 Agent 只能基于**公开信息 + 自己的私有策略**发言。

#### 7.4.2 记忆注入规则

Orchestrator 在构造 prompt 时，按以下优先级注入内容：

```
1. System Prompt（角色、信念度、规则）
2. 当前任务描述（action / target）
3. 公共证据板（双方可见）
4. 庭审历史：双方公开发言内容（已剥离 reasoning）
5. 该 Agent 的私有记忆（按 recency / 相关性排序）
6. 输出格式要求
```

**禁止注入**：

- 其他 Agent 的 `payload.reasoning`。
- 其他 Agent 的 `private_memory` 内容。
- 书记员生成判决书时不读取任何私有记忆。

#### 7.4.3 记忆生成示例

控方在收到证据 E003 后，生成私有记忆：

```json
{
  "memory_id": "pm_pro_003",
  "session_id": "court_demo_001",
  "agent_id": "prosecutor_1",
  "round": 2,
  "type": "evidence_eval",
  "content": "E003 声称互联网大厂有 35 岁危机，但来源是匿名论坛，可信度低。下一轮应主动质疑其来源。",
  "linked_evidence_ids": ["E003"],
  "linked_messages": [],
  "created_at": "2026-06-25T12:03:00Z"
}
```

这条记忆只会在 Orchestrator 调用控方时注入控方 prompt，辩方永远不会收到。

---

## 8. 输出格式规范

所有 Agent 输出必须严格 JSON 化，便于后端解析。

### 8.1 Agent 原始输出（v0.4 ReAct 强制版）

> v0.4 修订：原 v0.3 的 "MVP 强制版" 输出格式已演进为 ReAct 协议——LLM 输出多步决策，每步是 `thought` / `tool_call` / `reflect` / `speak` 之一。**只有 `speak` 步骤包含最终发言内容**；其他步骤的 `content` 字段可为空。

每一步 ReAct 输出：

```json
{
  "action": "thought | tool_call | reflect | speak",
  "reasoning": "当前这一步的推理（仅 Orchestrator 与该 Agent 可见）",
  // 当 action=tool_call 时必填：
  "tool": "investigator_search",
  "tool_input": { "query": "..." },
  // 当 action=reflect 时必填：
  "reflection": "对自己的下一步策略反思",
  // 当 action=speak 时必填：
  "content": "正式发言（公开，最多 200 字）",
  "evidence_refs": ["E001"],
  "confidence": 0.8,
  "stance": "pro_a",
  "memory_to_write": {
    "type": "strategy_note",
    "content": "本轮应继续攻击 E003 的来源可信度",
    "linked_evidence_ids": ["E003"]
  }
}
```

**MVP 强制校验规则**（每步独立校验）：
- `action` 必须是 `thought / tool_call / reflect / speak` 之一
- `reasoning` 不能为空字符串
- 当 `action=speak`：
  - `content` 不能为空字符串
  - `evidence_refs` 当前有证据时必须非空且全部存在；无证据时必须为 `[]`
  - `confidence` 必须是 `[0, 1]` 之间的数字
  - `stance` 必须是 `pro_a / pro_b / challenge / neutral` 之一
- 当 `action=tool_call`：
  - `tool` 必须是已注册的工具名（当前仅 `investigator_search`）
  - `tool_input` 必须是 tool 接受的 schema（见 `internal/agent/tools/`）

**校验失败处理**：
- 第一次失败：将错误信息反馈给 LLM，要求按格式重试
- 第二次失败：当前步 fallback（如 speak 失败 → 兜底发言）
- 记录到 `llm_calls` 表的 `status` 和 `error_msg` 字段

**LLM 流式**：
- `speak` 步骤会走 `llm.Client.StreamComplete` 流式生成
- 每个 chunk 通过 `RunnerConfig.OnSpeakChunk` 回调广播为 `agent.speak_chunk` 事件
- 后端 hub.Broadcast sleep 30ms 保证 chunks 间隔，前端 flushSync 强制同步 commit
- 流式失败/超时 → fallback 到 JSON 里的 `content` 字段占位

### 8.2 A2A 消息封装

Orchestrator 将 Agent 原始输出封装为 A2A 消息后广播或转发：

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
    "reasoning": "推理过程摘要",
    "content": "正式发言内容",
    "evidence_refs": ["E001"],
    "confidence": 0.8
  },
  "memory_references": ["pm_pro_002"],
  "created_at": "2026-06-25T12:00:00Z"
}
```

### 8.3 后端解析规则（v0.4 ReAct）

按当前步的 `action` 分发：

| action | 处理 |
|---|---|
| `thought` | 推 `agent.cot_step(kind=thought)`；继续 ReAct 循环 |
| `tool_call` | 调 `agent.tools.InvestigatorSearch.Execute(tool_input)`；返回 `observation` 字符串；推 `agent.cot_step(kind=tool_call, tool, tool_input, observation)` |
| `reflect` | 推 `agent.cot_step(kind=reflect)`；继续 ReAct 循环（受 MaxReflects=3 cap） |
| `speak` | 1) 走 `llm.Client.StreamComplete` 流式生成 `content`，每 chunk 推 `agent.speak_chunk`；2) 提取 `content` 推 `agent.speak`；3) 提取 `reasoning` 写入该 Agent 私有记忆或审计日志，**不展示给其他 Agent**；4) 提取 `evidence_refs` 做可视化连线；5) 如果存在 `memory_to_write`，则写入该 Agent 的私有记忆池 |

**ReAct runner 钩子**：
- `OnIterStart(iter int)` 在每轮 iter 起始时触发 → 服务端推送 `agent.thinking_started`（仅 iter=0 推，让云朵只在最开始出现一次）
- `OnSpeakChunk(chunk, accumulated)` 在每个流式 chunk 到达时触发 → 推送 `agent.speak_chunk`

---

## 9. 异常处理

| 异常 | 处理策略 |
|---|---|
| LLM 输出非 JSON | 重试 2 次，仍失败则返回兜底文案 |
| LLM 调用超时 | speak 阶段 30s 软超时；超时后 fallback 到 JSON `content` 占位 |
| LLM 流式异常 | chunkCb 失败立即结束流，fallback 到非流式 Complete 拿完整 content |
| Agent 立场不一致 | 要求重生成，最多重试 3 次 |
| Agent 发言重复 | 要求换角度，最多重试 3 次 |
| ReAct 超过 max iterations | 返回 "react: max iterations (N) exceeded without speak" 错误，庭审可继续下一阶段 |
| Search provider 失败 | DispatchInvestigator 用 defer 保证 `search.completed` 仍发出，payload `success=false` + `error` 字段 |
| 信念度异常 |  clamp 到 [0.05, 0.95] |
| 私有记忆越权访问 | Orchestrator 拒绝注入，记录审计日志 |
| A2A 消息路由错误 | 返回 500，要求 Orchestrator 重新构造消息 |

---

## 10. 当前状态与下一步

> 文档版本：v0.5（v0.5 增补 Episodic Memory via A2A 私有通道 + ContextView 投影）

### 10.1 已落地到文档的设计

- ✅ A2A 消息协议：消息格式、总线时序、上下文隔离规则
- ✅ 私有记忆模块：记忆结构、读写规则、注入优先级、防越权处理
- ✅ 防止附和机制：温度差异 + 信念引擎 + 立场一致性 + 新意度 + 私有记忆隔离
- ✅ ReAct 协议：action / tool_call / reflect / speak 多步决策
- ✅ LLM 流式：OnSpeakChunk 钩子 + hub.Broadcast sleep + 前端 flushSync
- ✅ 调查发现独立表：与 evidence 严格分离，通过 /investigations 端点暴露

### 10.2 已完成（v0.4 增量）

| 设计点 | 代码实现 |
|---|---|
| ReAct 协议 | `internal/agent/react_runner.go` + `OnIterStart` / `OnSpeakChunk` 钩子 + `ActionReflect` |
| ReAct 输出校验 | `internal/agent/orchestrator.go` 校验 action / reasoning / tool / content / stance |
| LLM 流式 | `internal/llm/client.go` StreamComplete + 渐进 JSON content 提取 |
| 后端 ws 帧间隔 | `internal/api/hub.go` Broadcast sleep 30ms |
| 前端流式渲染 | `frontend/store/courtroomStore.ts` applySpeakChunk 用 `flushSync` 强制 commit |
| 调查员派遣 | `internal/investigation/` + `investigation_findings` 表 + InvestigationFinding 端点 |
| 调查员视觉 | `InvestigatorPanel.tsx` + Avatar isSearching 状态 + 状态机 dispatch→report 升级 |
| Bubbles 优先级 | AgentAvatar streaming > thinking 互斥守卫 |
| Bocha 搜索 | `internal/search/bocha.go` + Mock Provider |
| ReAct 集成测试 | `internal/courtroom/service_react_helpers_test.go` + dispatch_investigator_events_test.go |

### 10.3 尚未在代码中实现（后续迭代）

| 设计点 | 代码现状 | 下一步 |
|---|---|---|
| 智能收敛 | 信念度初始化 + snapshot 已做，未基于 delta 提前结束庭审 | 加 `is_converged` 检查 + `trial.converged=true` 提前进入 closing |
| 信念引擎动态更新 | 初始化 + snapshot 记录已做，未基于证据实时更新 | `internal/belief/engine.go` 加 update_belief(ev) 函数 |
| ReAct 多轮反思优化 | 当前 LLM 自带反思，MaxReflects=3 cap 已生效 | 根据实际庭审数据调优 cap + 增加更细粒度的反思 prompt |
| 调查发现引用图谱 | 当前 InvestigatorPanel 按时间顺序展示 | 加 result 排序 + 相关性评分 + 时间线视图 |
| Agent Gateway | 未实现 | 模型路由 + Prompt 压缩 + Token 预算 + 调用审计 |

### 10.4 下一步

1. **运行端到端验证**：确保信念更新、智能收敛、立场一致性检查在真实庭审中工作正常。
2. **代码实现 A2A 模块**：定义消息结构、路由函数、访问控制。
3. **代码实现私有记忆模块**：数据表 + CRUD + 注入逻辑。
4. **改造 Orchestrator**：使用 A2A 消息驱动 Agent 调用，替换直接 LLM 调用。
5. **端到端验证**：确保前端无需改动即可继续使用现有 WebSocket 事件。

---

## 11. 集成测试

### 11.1 测试文件

- `backend/internal/courtroom/service_integration_test.go`
- 输出目录：`backend/test-output/`

### 11.2 测试用例

| 用例 | 覆盖场景 |
|---|---|
| `TestStandardModeFullFlow` | 标准模式、无证据、自动连续跑完所有轮次 |
| `TestStandardModeForcedFullRounds` | 提交强证据，验证不会过早收敛，跑满 3 轮 |
| `TestSubmitEvidenceDuringCrossExam` | 质证中提交证据，验证 belief 更新、Agent 回应、无虚假引用 |
| `TestDirectVerdictEarlyTermination` | 直接判决，验证庭审提前结束且只生成一份 verdict |

### 11.3 关键断言

- 最终阶段为 `deliberation`，不自动跳转到 `verdict`
- 消息数量与阶段/轮次匹配
- 相邻 Agent 不会返回完全相同的内容
- 同一 Agent 在不同轮次不会高度重复（>80% 相似度）
- 不会引用不存在的证据 ID
- 提交证据后 belief 会更新
- verdict 字段完整且分数合法

### 11.4 运行方式

```bash
cd backend
go test ./internal/courtroom -count=1 -timeout 300s -v
```

**注意**：测试使用真实 DeepSeek API key，运行一次约 100-120 秒，会产生 API 调用费用。

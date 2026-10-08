# ADR 0045：投影隔离硬化（公共消息剥离路径收口 + 载荷白名单化）

| | |
|---|---|
| **状态** | ✅ Accepted（2026-10-08 实施） |
| **日期** | 2026-10-08 |
| **来源** | [`docs/todo/deferred-items-2026-10-07.md`](../todo/deferred-items-2026-10-07.md) §D13（**AGENTS.md §2.1 范畴**） |
| **授权** | 用户 2026-10-08 明确授权"按建议全做"（方案见 `.trae/documents/d13-projection-isolation-hardening-proposal.md`） |
| **关联** | ADR 0003（投影决策，含已记录的弱点）· ADR 0002（私有通道）· ADR 0044（同类"静默失效"根因） |

---

## 1. 背景：两处隔离保证，一处是巧合

`docs/decisioncourt-agent-design.md` 描述了三道信息隔离防线。D13 核对后发现其中"控辩双方
互不可见对方推理链"这件事，**当时有两重保险，但一重是结构性巧合，另一重是没有消费者的旁路**：

| 事实 | 证据（v2.11 核对时） |
|---|---|
| A1. `BuildContextView` 会把「他人发的公共消息」剥离 `reasoning` 后放进 `WorkingMemory` | `internal/a2a/context_view.go` |
| A2. **`WorkingMemory` 没有生产消费者** —— 编排层只读 `PrivateMemory` | `internal/agent/orchestrator_context.go`（`buildEpisodicMemoryBlock` 只遍历 `view.PrivateMemory`） |
| A3. 另一处投影 `Message.SanitizedPayload()` 也没有生产调用点 | `internal/a2a/types.go`（仅测试引用） |
| A4. 对手的实际上下文来源是 `model.Message` 转写，且**只渲染 `Content`** | `internal/agent/prompts.go`、`internal/agent/orchestrator.go`（`speak` 里的历史构造） |
| A5. 剥离逻辑是**单键黑名单**（只删 `reasoning`） | `a2a/types.go` + `a2a/context_view.go` |

**结论**：隔离成立，但 A4 的"恰好只渲染 Content"是**巧合**而非契约（推理链就存在
`model.Message.Metadata` 里，谁读它谁就泄漏）；A5 是"记得删"而不是"默认安全"。

---

## 2. 决策

### 2.1 A-1：把转写 → 提示词的不变量显式化（**零行为变更**）

抽出**唯一出口**并在那里声明白名单：

- `agent.renderTranscriptLine(m model.Message, maxLen int)` —— 全仓唯一的"庭审转写 →
  提示词文本"渲染点（5 个 prompt 构造器统一走它）。注释显式声明：只允许
  `ActionType` / `Content` 过桥，**绝不读 `Metadata`**。
- `agent.buildLLMHistory(messages, agentMap)` —— **对手 LLM 实际看到的对话历史**的
  唯一构造点（从 `speak` 里抽出）。其 `Metadata` 是**重新构造**的标签 map
  （只有 `agent_type` / `evidence_id`，给网关压缩器评分用），不是从 `m.Metadata` 拷贝。

**为什么不改成"让提示词消费 `WorkingMemory`"**（D13 方案里的 A-2）：那会改变 agent 实际
看到的上下文（A2A 消息与 messages 表的字段/粒度/排序都不同）→ 属于**行为变更**，可能改变
庭审走向与判决结果。D13 的语义是"硬化"不是"改行为"，A-1 能用零行为变更拿到同等保证。

### 2.2 B：载荷剥离改**正向白名单**

`a2a.ProjectPayloadForOpponent(msgType, payload)` 取代"删 `reasoning` 一个键"：

| MessageType | 允许进对方视图的键 |
|---|---|
| `speech` | `content` / `stance` / `confidence` / `evidence_refs` |
| `dispatch` | `query` / `dispatched_by` |
| `report` | `query` / `dispatched_by` / `finding_id` / `result_count` / `summary` / `source` |
| **未登记的 MessageType** | 仅 `content`（保留"对方说了什么"的信封语义） |

`Message.SanitizedPayload()` 与 `sanitizeMessageRow()` 都委托给这一个实现。

**方向反转的意义**：未登记的键**默认不进入**对方视图。新增敏感字段（`planned_next_move`、
内部置信度、工具参数……）不再需要"人工记得加进剥离逻辑" —— 漏登记只会让对方少看到合法
信息（**可见**的降级），而不是静默泄漏（**不可见**的风险）。

### 2.3 为什么不动用户/审计视角

`MemoryAuditPanel` 同时看到双方私有笔记是**有意的产品设计**（用户是法官）。白名单**只**作用于
"投影给对方 agent"这一步：DB 行、WS `a2a.message` 广播、私有记忆都不变
（前端 `MemoryTimeline` 依赖 `reasoning` 渲染结构化卡片）。

---

## 3. 不做的事（明确边界）

- ❌ 不改用户/审计视角的可见性（同时看双方私有笔记是有意设计）
- ❌ 不改 `ListVisibleTo` 的 SQL 语义（私有记忆的硬保证在这里，不要动）
- ❌ 不新增记忆类型 / 事件类型
- ❌ 不做"LLM 语义级"推理泄漏检测（成本高、不确定）
- ❌ 不改控辩双方**当前实际可见的信息量**（A-1 是把它钉死；B 是让它默认安全）

---

## 4. 验证

| 验收项 | 测试 |
|---|---|
| 注入虚构敏感字段（`planned_next_move` / `internal_confidence`）不出现在对手提示词里 | `TestTranscriptIsolation_NoMetadataLeakInPromptBuilders`（5 个 prompt 构造器逐个覆盖） |
| **零行为变更**（"改造前后提示词逐字节相同"的可执行版本） | `TestTranscriptIsolation_MetadataChangeDoesNotAlterPrompt`（只改 Metadata → 提示词文本必须逐字节相同） |
| 对手 LLM 看到的历史不含 DB Metadata | `TestTranscriptIsolation_SpeakHistoryOmitsDBMetadata`（`buildLLMHistory` 的 Metadata 只能含 `agent_type`/`evidence_id`） |
| 白名单丢弃未登记键 | `TestMessage_SanitizedPayload_DropsUnregisteredKeys` |
| 未登记 MessageType 只放行 `content` | `TestMessage_SanitizedPayload_UnknownTypeKeepsOnlyContent` |
| 白名单完整性（新增类型必须显式分类） | `TestPublicPayloadWhitelist_CoversAllTypes` / `_PublicProducersRegistered` / `_PrivateTypesNeverWhitelisted` |
| 投影不改入参、缺失键不塞 nil | `TestProjectPayloadForOpponent_DoesNotMutateInput` / `_MissingKeysOmitted` |

既有隔离测试（`context_view_test.go` / `bus_test.go`）**全部保留**；唯一改动是
`TestMessage_SanitizedPayload_StripsReasoning` 显式给出 `MessageType: speech`
（白名单按类型生效，此前不设类型也能过）—— 断言意图不变，**不是简化测试**。

`go test ./...` 22 包全绿。

---

## 5. 遗留（本 ADR 明确不改）

- `BuildContextView.WorkingMemory` 与 `Message.SanitizedPayload()` 仍然**没有生产消费者**。
  本次保留了它们（`context_view.go` 的文档引用 `SanitizedPayload`），并把"当前无调用点"
  写进注释。将来若要收口成"提示词直接消费投影"，那是**行为变更**，需要另开 ADR。
- `a2a` 公共 payload 的生产者目前只有 3 个（speech / dispatch / report）。新增公共事件类型
  时必须在 `publicPayloadWhitelist` 登记（有测试护栏），否则对方只能看到 `content`。

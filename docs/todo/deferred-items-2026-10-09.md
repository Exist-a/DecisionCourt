# Deferred Items（2026-10-09 新增 · 记忆分层 / 投影隔离审计）

| | |
|---|---|
| **生成日期** | 2026-10-09 |
| **状态** | **2026-10-11 收口更新**：✅ **M1 已接线**（用户拍板方案①，投影产物 WorkingMemory 现已真正被提示词消费）／ ✅ **M2 已收口**（legacy `private_memories` 表删除，Expand-Contract 走完）／ ✅ **M3 已真填充**（`LLMContext.Beliefs` 现由信念引擎产物填充并被提示词消费）／ ✅ **M4 已删**（两个孤儿投影 API 删除）／ 🟡 **M5 记录性复核项**（随 M1 联动评估：新区块只读投影后的 WorkingMemory，未破隔离）／ ✅ **M6 已更正 + 已加固**（数字改正 + eval floor 护栏）／ 🔴 **M7 已采到真实数据，但结论反转**（见下方 M6/M7 状态更新与新增 **M8**） |
| **2026-10-11 新增** | **M8：压缩在真实庭审里省 0% + 会话预算被顶穿** —— 见 §M8。**已处置**：预算默认 200k→**300k**（已落地）；压缩**本轮不修**（用户定"system 压缩最低优先级"），**⚠️ 结论已于 2026-10-11 复核更正**（"永远 0%" 取样不全 → 实为 **0%～22%**，且省幅主要来自 system 兜底上限；见 §M8 与 [`docs/COMPRESSION-REDESIGN-PROPOSAL.md`](../COMPRESSION-REDESIGN-PROPOSAL.md)）；P2 已出方案文档。 |
| **触发** | 以面试题「设计分层 Agent 记忆系统，基于投影在生成对方提示词前剥离私有字段，按可见性维护各 Agent 私有策略池，从协议层硬保证控辩双方互不可见对方私有推理链」为题，**逐条对照真实代码**（不采信文档，因文档可能漂移）核对结论 |
| **关联 PR** | 无（本批为新增发现，M1 起编号） |
| **核对基线** | `main` @ `1515bbb`（v2.13 + 第二次上线文档收尾之后） |
| **前序关联** | [D13 / ADR 0045](../adr/0045-projection-isolation-hardening.md)（投影隔离硬化的上一轮：转写出口显式化 + 载荷正向白名单，commit `b10b477`） |

---

## 背景

面试题描述的架构，`DecisionCourt` 里**骨架全部存在、且有测试钉住**：
分层记忆分了 4 层、投影白名单是正向 fail-closed、私有池有可见性校验、SQL 层有过滤。

但逐条核对真实调用点后，发现**最关键的一环——"投影剥离后进入对方提示词"——在生产链路上是空转的**：

- `ProjectPayloadForOpponent` 每次律师发言都会跑，但它的输出（`LLMContext.WorkingMemory`）**没有任何消费者**；
- 真正拦住"对方推理链"的，是**庭审转写渲染的字段白名单**（`renderTranscriptLine` 只取 `ActionType + Content`）和 **SQL 可见性过滤**，不是投影层；
- 私有策略池是**双写**（A2A 私有消息 + legacy `private_memory` 表），而 legacy 那张表**只写不读**。

这不是造假，是**半成品 / 未接线的安全底座**——每一处都能说清"当时为什么停在这里"（D13 明确评估过、主动否决了把投影接进提示词，因为那属行为变更）。但如果不落文档，面试深挖和长期维护时都会暴露，且未来若有人误以为"投影层在保护提示词"，会做出错误的安全判断。

> **本批与 D13 的关系**：D13 已发现并记录了「公共消息剥离路径无消费者」这一现象，并将其作为"两重保险中一重是旁路"接受（选择零行为变更的硬化路径）。M1 不重复该发现，而是**把"是否接线"作为独立决策项重新摊开**（D13 当时因"行为变更"否决，本轮给出三选一方案供拍板）；M2–M5 是核对中**新发现**的、D13 未覆盖的另一半缺口（legacy 表读出口、Beliefs 占位符、投影入口孤儿、明文载荷端到端依赖）。

---

## 缺口总表

| 编号 | 标题 | 影响 | 优先级 | 需用户决策 |
|---|---|---|---|---|
| M1 | 投影剥离层产物 `LLMContext.WorkingMemory` 无消费者 —— 「基于投影剥离私有字段」在提示词链路上空转 | 隔离保证实际由转写白名单承担；投影层是未接线的冗余（非漏洞，但易被误认为在生效） | P1 | **是**（接线 = 行为变更，D13 曾否决） |
| M2 | legacy `private_memory` 表**只写不读** —— 带 `viewerRole` 权限校验的 `Repository.List` 零生产调用点 | 每场庭审多写一份无人读取的数据；代码注释自诺的"PR 4.5 drop"（[orchestrator.go:542](../../backend/internal/agent/orchestrator.go)）未执行；两套私有池语义漂移 | P2 | 否（收口方向明确，但删表需授权） |
| M3 | `LLMContext.Beliefs` 是占位符，**从不被填充** | 信念（belief）未进入记忆分层；注释承诺的 "v0.5 PR 3 填充"从未兑现 | P3 | 否 |
| M4 | `SanitizeForViewer` / `Message.SanitizedPayload` **零调用点** | 4 个投影入口只有半个链路在跑；单行投影 API 属孤儿代码 | P3 | 否 |
| M5 | 公开 A2A `speech` payload **明文存 `reasoning`** 且经 WS 广播明文下发 | 端到端隔离完全依赖"消费端不读"；WS 仅软 owner 校验（UUID-as-credential） | P3（记录/复核） | 否（属既定产品设计，仅记录） |
| M6 | 压缩节省率宣称值（`31.4%` / 保留 `14` 条）**来自合成测试 fixture、生产从未触发过压缩**（真实庭审 `compressed=false`），且已**漂移**为实测 `34.1%` / `13` 条；eval **不钉数字**；引用源 `.trae/documents/compression-eval-baseline.md` **已不存在** | 亮点引用的数字不是真实庭审数据；引用链断裂；回归无护栏（掉到 5% 也能过） | P2 | 否 |
| M7 | **真实庭审压缩数值采集** —— 跑一场会触发压缩的庭审，取真实节省率/保留条数，替换掉所有 fixture 数字 | `PRD` / `tech-spec` 的"≥30% 节省率"指标目前**无法验收**；亮点数字无生产依据 | P2 | 否（但需用户手动跑一场 + 时间） |

---

## M1（P1）投影剥离层产物 `WorkingMemory` 无消费者

### 现状（代码事实）

`BuildContextView` 把可见消息投影成两半：公共消息（剥离后进 `WorkingMemory`）+ 本人私有（`PrivateMemory`）。

- 投影实现：`ProjectPayloadForOpponent`（[context_view.go:271](../../backend/internal/a2a/context_view.go)）＋ 白名单（[context_view.go:219](../../backend/internal/a2a/context_view.go)）＋ 行级 `sanitizeMessageRow`（[context_view.go:291](../../backend/internal/a2a/context_view.go)）。
- 公共行剥离逻辑确实在跑（[context_view.go:129](../../backend/internal/a2a/context_view.go)），结果写进 `out.WorkingMemory`（[:148](../../backend/internal/a2a/context_view.go)/[:151](../../backend/internal/a2a/context_view.go)）。

**但 `BuildContextView` 全仓只有一个调用者**——`buildEpisodicMemoryBlock`（[orchestrator_context.go:39](../../backend/internal/agent/orchestrator_context.go)，由 [orchestrator.go:238](../../backend/internal/agent/orchestrator.go) 在 `lawyerSpeakReAct` 中触发），而它**只读 `view.PrivateMemory`**（[orchestrator_context.go:45](../../backend/internal/agent/orchestrator_context.go)/[:52](../../backend/internal/agent/orchestrator_context.go)）。

`grep -rn "WorkingMemory" backend/ --include=*.go` 的**全部命中都在 `context_view.go` 自身**：没有任何提示词构造器读它。

### 真正在扛隔离的是什么

| 机制 | 位置 | 是否生产生效 |
|---|---|---|
| 转写渲染字段白名单（只取 `ActionType + Content`） | [prompts.go:308](../../backend/internal/agent/prompts.go) `renderTranscriptLine` | ✅ **主力**（全仓唯一「转写 → 提示词」出口，5 个 prompt 构造器统一走它） |
| 对手对话历史重建（重建 Metadata，不拷贝 `model.Message.Metadata`） | [orchestrator.go:868](../../backend/internal/agent/orchestrator.go) `buildLLMHistory` | ✅ |
| "对方刚刚说"摘要只取 `.Content` | [orchestrator.go:408](../../backend/internal/agent/orchestrator.go) `withArgumentSummaryText` | ✅ |
| SQL 可见性过滤（私有行在行集层就不出现） | [bus.go:90-105](../../backend/internal/a2a/bus.go) `ListVisibleTo` | ✅（私有池那半边） |
| **投影剥离 `WorkingMemory`** | [context_view.go:98](../../backend/internal/a2a/context_view.go) | ❌ **无消费者** |

### 影响

- **不是漏洞**：当前对手提示词确实拿不到推理链（上面 4 条真的在生效，且有 D13 注入式回归测试钉住）。
- **但是风险**：投影层被计算、被排序、被测试，**却不影响任何提示词字节**。任何据此推断"投影在保护提示词"的安全判断都是错的。一旦未来有人新增一条读 `WorkingMemory` 的提示词路径并**顺手删掉/绕过转写白名单**，就会真的泄漏——而测试断言的是"投影输出正确"，不是"提示词实际消费了投影"。

### D13 当时的决策（不是遗漏，是选择）

`b10b477` commit message 原文记录了两种方案：

- **A-2（接线）**：让提示词直接消费 `WorkingMemory`。**否决**，理由：会改变 Agent 实际看到的上下文 → 属行为变更，可能改变庭审走向；而 D13 的语义是"硬化"不是"改行为"。
- **A-1（选定）**：零行为变更地把转写出口显式化为白名单契约。

### 建议动作（三选一，需用户拍板）

| 方案 | 做法 | 代价 |
|---|---|---|
| **① 接线（推荐评估）** | 让 `lawyerSpeakReAct` 用 `WorkingMemory` 渲染"对方的公共发言"区段，替换/补充 `withArgumentSummaryText`；同步补一条"提示词必须消费投影"的测试 | **行为变更**：需跑真实庭审对比走向，且要过 AGENTS.md §11 docker 验证 |
| **② 显式降级为内部库** | 保留投影层但**明确注释**为"未接线的安全底座 + 为什么留着"，并把 `WorkingMemory` 改名为 `WorkingMemoryUnused` 或加 `//nolint` 标注，防止误读 | 零行为变更；纯文档/命名，但消不掉"冗余"本身 |
| **③ 删除投影的公共半边** | 只保留 `PrivateMemory` 路由，删掉 `WorkingMemory`＋白名单＋相关测试 | 减少 ~150 行维护面；但会**丢掉未来 A2A 直连通道的现成底座**（白名单 fail-closed 设计有价值） |

> 倾向：**短期 ②、中期评估 ①**。②零风险地消除"误认为在生效"的认知风险；①只有在确实需要"Agent 直接收到对方消息的语义版本"时才值得做。

### ✅ 决策记录（2026-10-10，用户拍板）：**选 ① 接线**

用户 2026-10-10 明确选择**方案 ①**——让提示词真正消费投影产物 `WorkingMemory`。

- **归属轮次**：第 4 轮（隔离收口），与 M3 / M4 / M5 一并处理。
- **为什么不是 ②**：②只是"消除误读"，投影层仍然是空转的冗余；用户要的是让它**真的生效**。
- **实施时的硬要求**（行为变更，按 AGENTS.md §11.2 / §2.1 纪律）：
  1. 需**跑真实庭审对比走向**（接线前后同题对跑），不能只跑单测；
  2. 必须过 AGENTS.md §11 docker 业务验证 + 浏览器实测；
  3. 同步补一条"**提示词必须消费投影**"的测试——否则下次有人绕过它（回到转写白名单）不会被发现；
  4. ⚠️ **与 M5 联动**：一旦有了"读原始 payload"的新消费方，公开 payload 里明文存 `reasoning`
     这件事就会从"审计友好"变成"泄漏源"，实施时必须确认新消费方**只读投影后的 `WorkingMemory`**。

### ✅ 实施记录（2026-10-11）

- `buildEpisodicMemoryBlock` 扩展为 **`buildA2AContextBlock`**：一次 `BuildContextView`
  消费三部分 —— 对方的公开发言（来自已投影的 `WorkingMemory`）、本方私有策略笔记
  （`PrivateMemory`）、当前信念状态（`Beliefs`）。
- 三个律师路径（ReAct + 非 ReAct 的 `ProsecutorSpeak`/`DefenderSpeak`）都追加该区块；
  发言步素材（`buildSpeakMaterials`）也共用同一份 → **think 与 speak 两个 prompt 都消费投影**。
- ✅ **M5 联动已满足**：新区块只读投影后的 `WorkingMemory` 行（`from_agent != self` 且
  `message_type == speech`），**不重读**对手原始 payload；对手 `reasoning` 未进入提示词（有回归测试）。
- 回归：`TestBuildA2AContextBlock_ConsumesProjectedOpponent_NotReasoning`（对手公开结论/引用可见、
  reasoning 不可见）+ 原 episodic memory 三条集成测试（`ProsecutorSpeakWithReAct_*`）继续通过。
- 验证：dev compose quick 庭审实跑干净（75,267 token、零 ERROR/WARN），判决由 LLM 正常生成。

---

## M2（P2）legacy `private_memory` 表只写不读

> ✅ **已修复（2026-10-10，用户授权后）**：按记录在案的决策**走完 Expand-Contract 的 Contract 阶段** ——
> ① **删写**：`recordSideEffects` 的 legacy `memoryRepo.Append` 移除，A2A 私有消息成为私有记忆的**唯一权威源**；
> ② **删包**：`backend/internal/private_memory/` 整个删除（含 9 项隔离测试）；
> ③ **删模型**：`model.PrivateMemory` 删除并移出 AutoMigrate；
> ④ **删表**：dev 库 `private_memories` 已 `DROP TABLE`（实删 **106 行**）。
>
> **根因顺带查清**：表名实际是 **`private_memories`**（GORM 对 `PrivateMemory` 的默认复数化），而文档一直写单数 `private_memory` —— 又一个文档/实现命名漂移（与 R20 同族），已在 PRD §4.5.5 / tech-spec 更正。
>
> **测试处理（AGENTS.md §3，非简化）**：原 **6 处断言 legacy 池隔离**的用例（`memRepo.List` 的 owner/orchestrator 权限、私有笔记内容）**改钉到存活的 A2A 通道**（`a2aRepo.ListVisibleTo` + private strategy_note 断言）—— 同一保证换实现；仅删除**契约已不存在**的用例（`nil memRepo` 必须 panic）。`go build`/`vet`/`go test ./...` **22 包全绿**。
>
> **docker 业务验证**：删表后实跑一场带证据庭审 → 零 ERROR、`verdicts` 落 1 行、终态 `verdict/completed`、A2A 私有笔记照常（`strategy_note`×5 + `opponent_weakness` + `self_correction`）。
>
> ⚠️ **生产库尚未删**：线上跑的是**旧代码**（仍在写这张表）。**必须先部署新代码，再**执行 `DROP TABLE private_memories;` —— 顺序反了会让线上每次发言写库报错。

### 现状（代码事实）

私有策略池是**双写**（[orchestrator.go:542-560](../../backend/internal/agent/orchestrator.go)）：

1. **A2A 私有消息**：`visibility=private` / `to=自己` / `message_type=strategy_note` → 前端 MemoryAuditPanel 数据源（读路径 [courtroom/service.go:2138](../../backend/internal/courtroom/service.go) → [bus.go:112](../../backend/internal/a2a/bus.go) `ListPrivateMemory` → API [handler.go:938](../../backend/internal/api/handler.go)）。
2. **legacy `private_memory` 表**：`o.memoryRepo.Append(...)`。

而 legacy 那半的**读接口**`Repository.List(sessionID, agentID, viewerRole)`（[private_memory/repository.go:51](../../backend/internal/private_memory/repository.go)，GORM 实现 [:124](../../backend/internal/private_memory/repository.go)）带着完整权限模型（`"orchestrator"` 可读全部 / `agent:<uuid>` 只能读自己 / 否则 `ErrNotOwned`），**但在整个后端非测试代码里没有任何调用点**：

```
$ grep -rn "\.List(" backend/ --include=*.go | grep -v _test
（空）
$ grep -rn "memRepo\|memoryRepo" backend/ --include=*.go | grep -v _test
main.go:258   memRepo := private_memory.NewGormRepository(model.DB)
main.go:310   agent.NewOrchestrator(gatewayClient, bus, memRepo, nil, nil)
orchestrator.go:558   o.memoryRepo.Append(...)   ← 只有 Append
```

### 影响

- 每场庭审往 `private_memory` 表多写一份**没人读**的数据（含 `立场= 置信度= reasoning=` 全文）。
- 注释承诺"双写过渡期，PR 4.5 drop"（[orchestrator.go:542](../../backend/internal/agent/orchestrator.go)）**未执行**，过渡期无限期延长。
- `private_memory.Repository.List` 的权限测试只在单测里跑，**它保护的表没有任何生产读者**——"按可见性维护私有池"这句话，真实生效的只有 A2A 那条路。

### 建议动作

不涉及裁决逻辑，方向明确：**删写 + 删表 + 删包**（或保留包但删 `List` 与其权限测试）。⚠️ **删表属破坏性操作**，需用户授权（AGENTS.md §9.4 / §11.4 同类纪律）。若不删，至少把注释从"PR 4.5 drop"改成"长期保留（原因）"，消除虚假承诺。

---

## M3（P3）`LLMContext.Beliefs` 占位符从不填充

### 现状

```go
// context_view.go:41
// Beliefs is a placeholder for the belief engine snapshot; v0.5 PR 3 will
// populate it from internal/belief/engine.go.
Beliefs map[string]float64
```

- 唯一写入点：`BuildContextView` 里初始化成**空 map**（[context_view.go:115](../../backend/internal/a2a/context_view.go)）。
- 唯一读取方：**无**（含 `HasContent()` 也不看它）。

真实的信念状态是**每 Agent 一行数值**（`agent.BeliefA/BeliefB`），直接拼进角色提示词（[prompts.go](../../backend/internal/agent/prompts.go) `ProsecutorPrompt` 里的 `agent.BeliefA`），**不经过**记忆分层。

### 影响

面试题里"分层记忆"如果被追问"信念在不在记忆层"，答案是**不在**：belief 是独立数值状态，`LLMContext.Beliefs` 只是留了个从没通电的口子。属文档/注释承诺未兑现。

### 建议动作

二选一：① 删字段（承认信念不进 context view）；② 真的填充（把 `belief` engine 快照注入，需评估 prompt 变化）。倾向 ① —— 避免留占位符误导。

### ✅ 实施记录（2026-10-11，用户选"真填充"，范围限定"只填本方信念"）

- `buildA2AContextBlock` 在调用 `BuildContextView` 后，把**信念引擎的产物**
  （`agent.BeliefA`）写入 `view.Beliefs[selfType]`，并从中渲染
  「## 当前信念状态」区段 —— `LLMContext.Beliefs` 不再是从未填充的占位符。
- 同步**移除** `ProsecutorPrompt` / `DefenderPrompt` 里直接读 `model.Agent` 的标量
  「## 当前信念度」行，使信念**单一来源于投影层**（信息等价，最小行为变更）。
- 三个律师路径（ReAct + 非 ReAct）都追加该区段，避免非 ReAct 路径丢信念信息。
- 测试：`TestBuildA2AContextBlock_NoMemoryOrOpponent_OnlyBeliefSection` 断言信念区段
  来自投影层且值为传入信念。

---

## M4（P3）`SanitizeForViewer` / `Message.SanitizedPayload` 零调用点

### 现状

投影层实际有 4 个入口，但生产只有半个链路在跑：

| 入口 | 位置 | 生产调用点 |
|---|---|---|
| `BuildContextView`（整体视图） | [context_view.go:98](../../backend/internal/a2a/context_view.go) | ✅ 1 处（`buildEpisodicMemoryBlock`），但只读私有半边（见 M1） |
| `sanitizeMessageRow`（行级，内部） | [context_view.go:291](../../backend/internal/a2a/context_view.go) | ✅ 仅被 `BuildContextView` 调用（结果又无人读） |
| `SanitizeForViewer`（行级，导出） | [context_view.go:183](../../backend/internal/a2a/context_view.go) | ❌ **0**（注释自述"暴露给 Orchestrator 按需单行投影"） |
| `Message.SanitizedPayload`（值级） | [types.go:108](../../backend/internal/a2a/types.go) | ❌ **0**（注释自述"当前生产代码没有调用点"） |

### 影响

孤儿 API 本身无害，但**叠加 M1** 后形成一种危险组合：**整个投影层在生产提示词链路上没有一个"真的在保护什么"的活跃入口**。任何代码漫游者看到 4 个投影函数 + 12 项测试，很容易得出"隔离由投影保证"的错误结论。

### 建议动作

随 M1 的方案一并决定：选 ① 接线则这些入口会被用起来；选 ③ 删除则一并清理。**不建议单独保留**（除非同时完成 M1 的 ② 显式降级注释）。

### ✅ 实施记录（2026-10-11，用户选"删除"）

按 M1 方案①接线后，这两个入口仍无生产调用点，故删除：

- 删 `Bus.SanitizeForViewer`、随其无用的 `ErrNotVisible`、以及 `Message.SanitizedPayload`；
- 三条断言 D13-B 白名单契约的用例（reasoning 剥离 / 未登记键丢弃 / 未知类型只留 content）
  **原样平移到存活的 `ProjectPayloadForOpponent`** —— 同一保证换实现，不简化覆盖面；
- 删 `TestSanitizeForViewer_Rules`（其契约已不存在，同 M2 先例）。

结果：投影层生产路径收敛为 `BuildContextView → sanitizeMessageRow →
ProjectPayloadForOpponent` 一条，消除"4 个入口有多个在跑"的误读。

---

## M5（P3，记录性复核项）公开 A2A payload 明文含推理链 + 广播明文下发

### 现状（代码事实）

- `recordSideEffects` 把**公开** `speech` 消息的 payload 写成 `content + reasoning + stance + confidence + evidence_refs`（[orchestrator.go:499-505](../../backend/internal/agent/orchestrator.go)），注释明说"so the bus has a faithful audit record"。
- `Bus.Send` 广播时，**public 消息的 payload 原样下发**（[bus.go:198-200](../../backend/internal/a2a/bus.go)）。
- 私有 episodic memory（4 种 type）的 payload 也**原样广播**——这是有意的产品设计（用户看双方"内心戏"，[bus.go:29-34](../../backend/internal/a2a/bus.go) 注释）。
- WS 接入需 **JWT**，但 owner 不匹配只是**软校验**（v0.9.2 "UUID-as-credential" 策略，[websocket.go:165-179](../../backend/internal/api/websocket.go)）：**持有 session UUID + 任意有效 JWT 即可加入房间并收到含 reasoning 的广播**。

### 影响 / 定性

- **这是既定产品设计，不是新漏洞**：用户就是要看双方推理链（差异化卖点），Real-Courthouse 的"打码"只是 **UI 层**开关（后端始终返回全量，[bus.go:29-34](../../backend/internal/a2a/bus.go) 说明是为了防止切换开关导致两侧状态脱同步）。
- **但与 M1 是同一枚硬币两面**：因为"提示词不吃 A2A payload"，所以才敢在公开 payload 里明文写 reasoning。**一旦 M1 选了方案 ① 且有别的消费方读原始 payload，这条"明文"就会从"审计友好"变成"泄漏源"**。
- 因此列为**复核项**：不是要改，而是要在 M1 决策时**联动评估**。

### 建议动作

无独立动作。M1 若选 ①，需同时确认"新消费方只读投影后的 `WorkingMemory`，绝不读原始 payload"。

---

## M6（P2）压缩节省率基线数字漂移 + 无护栏 + 引用源缺失

> **状态更新（2026-10-11，真实庭审实测 + 同日复核更正）**：本项的"问题 0（生产从未触发压缩）"已被实测解决 —— 压缩**真的触发了**。**⚠️ 但"省 0%"的初版结论取样不全，已更正**：完整日志 12 条压缩记录中，前 7 条（session `1e241ab4`）确为 **0.0%**，后 5 条（session `78e68370`）**省 17.7–22.1%**。且省幅来源**不是四步流水线，而是 system 消息的兜底上限**（`compressMaxSystemMsgLen=24000` → 截到 20000）被触发。逐条数据、算术证明与其余 4 项异常（含根因）见 [`docs/COMPRESSION-REDESIGN-PROPOSAL.md`](../COMPRESSION-REDESIGN-PROPOSAL.md)。
>
> **状态更新（2026-10-10，第 1 轮止血）**：**问题 1（数字漂移）/ 问题 2（引用源不存在）已于 2026-10-09 修复** ——
> `decisioncourt-prd.md` §9.4、`decisioncourt-tech-spec.md`（Token 节省率行）、`ADR 0006` 顶部状态更新均已改为
> **34.1% / 13 条** + 加"合成 fixture 微基准，不是生产实测"限定词 + 死链改指可复现源
> `backend/internal/agent_gateway/compression_eval_test.go`。**问题 3（eval 无护栏）已于 2026-10-10 修复** ——
> `TestCompressionEval_StrategyComparison` 新增 floor 断言：smart 节省率 **≥30%**、保留条数 **≥12**
> （此前只断言方向，掉到 5% 仍 PASS）。**问题 0（生产从未触发压缩）仍未解** —— 需在真实庭审上采一次压缩数据，
> 随第 2 轮（素材管道放量后压缩更可能触发）或第 3 轮进行。

### 现状（代码事实 —— 功能层面**已实现**）

面试叙述「多步 Token 压缩：多维评分 → 原子组识别 → 贪心打包 → 兜底摘要 四步流水线，强制保留最近轮次与高价值证据/推理锚点」**在代码里是完整的**，且已接进生产路径、默认开启：

| 步 | 实现 | 位置 |
|---|---|---|
| 1 多维评分 | `ScoreMessages`（role 权重 + 首末位置 + evidence/@角色引用 + tool_call + recency decay α=0.3） | [prompt_scorer.go:59](../../backend/internal/agent_gateway/prompt_scorer.go) |
| 2 原子组识别 | `BuildAtomicGroups`（`tool_call_id` 组 + `evidence_id` 组，同生共死） | [prompt_atomic.go:38](../../backend/internal/agent_gateway/prompt_atomic.go) |
| 3 贪心打包 | `GreedyPack`（按组均分降序 + `ScoreThreshold` 预过滤 + `keepRatioForStatus` 0.7/0.4/0.2） | [prompt_greedy.go:42](../../backend/internal/agent_gateway/prompt_greedy.go) |
| 4 兜底摘要 | `BuildEarlierSummary` / `BuildAbstractiveSummary`（丢弃 > `SummaryInsertThreshold` 时插 1 条锚点摘要） | [prompt_summary.go:26](../../backend/internal/agent_gateway/prompt_summary.go) |
| 强制保留最近 N | `CompressScored` 内 `KeepRecentForcedN`（默认 3）硬兜底 | [prompt_compressor.go:254-268](../../backend/internal/agent_gateway/prompt_compressor.go) |

- 接线：`NewPromptCompressor` 构造于 [gateway.go:73](../../backend/internal/agent_gateway/gateway.go)，`Compress` 在 `Complete`（[:214](../../backend/internal/agent_gateway/gateway.go)）与 `StreamComplete`（[:396](../../backend/internal/agent_gateway/gateway.go)）各调一次。
- 默认开启：`.env.example` `AGENT_GATEWAY_PROMPT_COMPRESSION=true` + `AGENT_GATEWAY_SMART_COMPRESSION=true`；config 走 `resolveGatewayChild`（[:331](../../backend/internal/config/config.go)）。
- 触发条件：仅在预算状态 ∈ {compress, throttle, exhausted} 时压缩（[prompt_compressor.go:151](../../backend/internal/agent_gateway/prompt_compressor.go)），normal 档不压缩。

### 问题 0（最根本）：这组数字来自**合成 fixture**，而**生产从未触发过压缩**

`31.4% / 14 条` 乃至下面的 `34.1% / 13 条`，输入都不是真实庭审 —— 是测试文件里手写的 `fixedTrialTranscript()`：19 条消息由 `strings.Repeat("[fact-a] ", 30)`、`noise(N)` 这类**重复填充文本**拼成，"证据 E1 / tool_call=tc-1 / judge assess" 全是虚构的（[compression_eval_test.go:27-76](../../backend/internal/agent_gateway/compression_eval_test.go)）。

更关键：**在唯一一场有生产数据的真实庭审上，压缩根本没触发**。[OBSERVABILITY.md §8.6](../OBSERVABILITY.md) 记录的 2026-10-09 真实庭审（41 次 LLM 调用）显示 —— 预算峰值 `138,790 / 200,000 = 69.4%`，**全程 `compressed=false`**；压缩触发线是预算的 70%，**刚好压在线下**（不是压缩坏了，是设计如此）。

所以：

- 「31.4% 节省」是**fixture 级微基准**，不是产品指标 —— 没有一场真实庭审的数据支撑它；
- fixture 是高度重复的填充文本（真实中文法律推理的压缩率不会相同），且省率用 `chars/4` 估算（[compression_eval_test.go:80](../../backend/internal/agent_gateway/compression_eval_test.go)），而算法内部用 CJK 感知的 `EstimateTokens` —— **两把尺子**；
- 真实庭审里这套四步流水线**一次都没跑过**，也就没有"生产环境节省多少"的观测。

### 问题 1：数字漂移（`31.4%`/`14 条` → 实测 `34.1%`/`13 条`）

实跑 `go test ./internal/agent_gateway/ -run TestCompressionEval_StrategyComparison -v`（2026-10-09）：

```
Transcript baseline: 19 messages, 4763 chars ≈ 1190 tokens
| disabled | false | 19 | 19 | 4763 | 4763 |   0 |  0.0% |           |  0 | 0 | 0 |
| legacy   | true  | 19 |  6 | 4763 | 1415 | 837 | 70.3% | legacy    |  0 | 0 | 0 |
| smart    | true  | 19 | 13 | 4763 | 3138 | 406 | 34.1% | scored    | 17 | 9 | 1 |
```

**保留 13 条（不是 14），节省 34.1%（不是 31.4%）**。`31.4% / 14 条` 是 [ADR 0006](../adr/0006-smart-prompt-compression.md) 的 **2026-07-01 基线**，早于 v2.10 [ADR 0044](../adr/0044-token-compression-strategy-review.md) 的改动 —— 测试注释自述：token 预算单位从"字符数"改成内容感知 `EstimateTokens` 后，Smart 保留组数由 10 变 9。数字没跟着更新。

### 问题 2：引用源不存在

`31.4%` 的引用源 `.trae/documents/compression-eval-baseline.md` **已不在**（本机 `.trae/documents/` 目录不存在；`.trae/` 被 gitignore，属本地稿）。而 [decisioncourt-prd.md](../decisioncourt-prd.md)（§9.4 上方 benchmark 段）与 [decisioncourt-tech-spec.md](../decisioncourt-tech-spec.md)（Token 节省率行）都把它当权威出处，tech-spec 里的相对链接 `../.trae/...` 从 `docs/` 出发也解析不到。

### 问题 3：eval 测试不钉数字（回归无护栏）

`TestCompressionEval_StrategyComparison` 只断言**下限**：legacy ≥25%、smart >0、`atomicKept ≥ 9`、smart `msgsAfter >` legacy。它**不**断言 31.4%、不钉 14 条、也不钉 tech-spec 声明的 `≥30%` 目标 —— 节省率掉到 5% 仍会 PASS。

### 问题 4（话术风险）：数字方向被讲反了

Smart 的节省率**低于** Legacy（34.1% vs 70.3%）是**设计取舍**（留更多高价值上下文换事实链完整）。把「31.4% 节省」当亮点正面陈述，会被一句"那为什么不用节省 70.3% 的 Legacy"问倒。真正站得住的说法是：**牺牲节省率换取证据链/推理锚点不丢**（13 条 vs 6 条，信息密度相当），而不是"节省了 31.4%"。

### 建议动作

0. **先在真实庭审上采一次压缩数据**（最根本）：当前"节省率"永远只能是 fixture 数字 —— 要么把真实庭审跑到触发线以上（`budget_used` > 预算 70%，见 R19：预算用量目前无指标，得先能看见），要么临时下调 `CompressionThreshold` 跑一场观测。**没有这一步，任何节省率都不该写进亮点。**
1. **复测统一数字**：以实跑为准更新表述（并可写"约 31–34%"以容纳计量口径差异）。⚠️ **只改数字不改定性 = 把 fixture 数字包装成"实测"** —— 加 "2026-10-09 复测" 反而更可信、更误导。改数字前必须先做第 0 步。
2. **修文档**：[decisioncourt-prd.md](../decisioncourt-prd.md) + [decisioncourt-tech-spec.md](../decisioncourt-tech-spec.md) 的失效数字与死链（改指向 `compression_eval_test.go` 这个可复现源）。
3. **加护栏**：eval 测试补"smart 节省率 ≥ 某下限 / 保留条数 ≥ 某下限"断言，让数字回归可被 CI 发现。
4. **ADR 0006 加"状态更新"**：注明 2026-10-09 复测值，保留 2026-07-01 基线作为历史（ADR 属时点记录，不改原文）。
5. **改口径**：PRD / tech-spec / 亮点里给数字加限定词 —— 写"**合成 fixture 微基准**"，不要写成产品实测值。

> **与 M1 的区别**：M1 是"投影层没消费者"（能力空转）；M6 是"能力在跑、但对外宣称的数字已经不准且没人守着"。两者共同点是**文档/叙述与代码现状脱节**。

---

## M7（P2，待跑）真实庭审压缩数值采集

### 为什么

当前**所有**"节省率"数字（`31.4%` / `34.1%`）都是合成 fixture 微基准；唯一有生产数据的真实庭审 `compressed=false`（预算峰值 69.4% < 70% 触发线，见 M6 / [OBSERVABILITY.md §8.6](../OBSERVABILITY.md)）。所以**没有任何真实庭审的压缩数值** —— `PRD` / `tech-spec` 写的「Token 节省率 ≥ 30%」这个验收目标**无法验收**。本项就是去把它采成真的。

### 前置

- **先落 M6 的定性纠正**（文档侧已完成：PRD / tech-spec 已标注"合成微基准 / 尚无生产数据"）；本项是把数字补成真的，不是把它包装成真的。
- 相关：[R19](deferred-items-2026-10-07.md)（预算用量没有指标）—— 触发与否目前只能翻 FileLogger，采样前最好先能"看见压缩有没有触发"。

### 怎么做（怎么让压缩**真的触发**）

触发线 = `AGENT_GATEWAY_BUDGET_PER_SESSION`（默认 `200000`）× `AGENT_GATEWAY_COMPRESSION_THRESHOLD`（默认 `0.7`）= 140k token。两条路：

1. **跑一场足够长/多轮的庭审**（轮次 + 证据更多 → 预算消耗更大，自然越线）。
2. **临时下调触发线**（用 **env 临时覆盖**，不改 `.env` 文件 —— AGENTS.md §8 红线）：把 `AGENT_GATEWAY_BUDGET_PER_SESSION` 调小（如 `20000`），或把 `AGENT_GATEWAY_COMPRESSION_THRESHOLD` 调低（如 `0.3`），让压缩必然触发。

### 采什么数

- FileLogger JSON 字段：`compression_before_length` / `compression_after_length` / `compression_before_count` / `compression_after_count` / `compression_strategy` / `compression_atomic_groups` / `compression_atomic_groups_kept` / `compression_recent_forced_kept` / `compression_summarized`。
- **用上游真实 token 计量**（对比压缩前后调用的 `llm_calls.prompt_tokens`），**不要再用 `chars/4` 估** —— 这正是当前 fixture 数字的失真来源之一。
- 需 `AGENT_GATEWAY_FILE_LOGGER=true`；若要看正文还需 `FILE_LOGGER_PROMPTS=full`（默认 `metadata`，ADR 0042 的隐私 opt-in）。

### 验收

得到一组**真实触发过**的压缩前后（token / 条数 / 原子组）数值 → 据此更新 `PRD` / `tech-spec` 的指标行与 [ADR 0006](../adr/0006-smart-prompt-compression.md)，让「≥30%」可被真实数据判定。

### 备注

- 属**用户手动跑一场**的任务（非纯代码改动）：按 AGENTS.md §11 用 dev compose 跑，或按 §9 在服务器上跑。
- 跑完顺带补 M6 的「数字护栏」（eval 测试加下限断言），防止再次静默漂移。

---

## M8（P1，2026-10-11 新增）压缩省幅 0%～22% + 会话预算被顶穿 → 判决走 fallback

### 怎么发现的

做 Module A（发言步「全量带」）的真实庭审验证时，跑一场 **standard 3 轮 + 1 条证据**的
庭审，判决阶段日志出现：

```
[GenerateVerdict] LLM error: agent_gateway: token budget exhausted (session=1e241ab4-… ratio=1.05)
GenerateVerdict failed, using fallback: agent_gateway: token budget exhausted
```

即：**会话预算被打满，最终判决被拒，降级为"依据法官信念度直接裁决"的 fallback**
（判决书里用户可见地写着"LLM 生成失败"）。

### 实测数据（session `1e241ab4-a336-4d05-8056-18508be55c26`）

| 指标 | 实测 |
|---|---|
| 本场总 token | **209,018** > 预算 200,000 → ratio 1.045（日志四舍五入 1.05） |
| `react_think` | 19 次，输入均值 **8,309**，合计 **162,109（77%）** |
| `react_think_retry` | 3 次，均值 9,602，合计 29,252 |
| `react_speak_stream` | 6 次，均值 1,670，合计 11,802（**5.6%**，Module A 的增量来源） |
| 既往同类标准庭审 `079793a2` | **197,788（98.9%）**——早已贴线 |
| 既往标准庭审 `8b9558d4` | 147,073 |

**结论一（预算）**：200k 的默认预算对"标准 3 轮 + 证据"已经不够用；Module A 只加了
~5.6%，是"压线的最后一根稻草"，主因是既有的 `react_think` 输入膨胀（Round 2 把证据
正文放开到全文 + ReAct 历史放宽）。

### 压缩：初版"7 次全 0.0%"（取样不全），复核后为 12 次 · 0%～22%

从 FileLogger（`logs/agent_gateway_2026-10-11.log`）取到的 7 条 `compressed=true`：

```
task=react_think        msgs 5->5  chars 22815->22815  saved=0.0%  ratio=0.703
task=react_think_retry  msgs 6->6  chars 22923->22923  saved=0.0%  ratio=0.754
task=react_think        msgs 1->1  chars 21826->21826  saved=0.0%  ratio=0.804
task=react_think        msgs 3->3  chars 22579->22579  saved=0.0%  ratio=0.853
task=react_think_retry  msgs 4->4  chars 22687->22687  saved=0.0%  ratio=0.903
task=react_think        msgs 1->1  chars 20742->20742  saved=0.0%  ratio=0.953
task=react_think_retry  msgs 2->2  chars 20850->20850  saved=0.0%  ratio=0.999
```

`before_count == after_count` **且** `before_length == after_length`：压缩是**空转**。

**结论二（压缩）—— 初版说"空转"，同日复核后更正**：完整日志 12 条压缩记录中，**前 7 条 0.0%、后 5 条 17.7–22.1%**。
因而**不是"结构性永远 0%"**。更正后的机制：`CompressScored` 只对**非 system** 消息评分/丢弃，system 只受
**兜底上限**约束（`compressMaxSystemMsgLen=24000` → `compressSystemTargetLen=20000`）——
- system < 24,000 且非 system 列表 ≤ ~5 条 → 什么都不丢 → **0%**（session `1e241ab4`，system 20.7–22.9k）；
- system > 24,000 → 触发兜底截断 → **17–22%**，且 `after ≈ 20000 + 保留的非 system 尾巴`（算术逐条吻合）。
即：**省幅主要来自那条兜底上限，而不是四步流水线**（流水线只在列表 ≥ ~9 条时才开始丢消息）。
逐条数据 + 算术证明见 [`docs/COMPRESSION-REDESIGN-PROPOSAL.md`](../COMPRESSION-REDESIGN-PROPOSAL.md) §1.2。

### 影响

- 长标准庭审的**最终判决会降级**（belief-only fallback），用户可见质量下降；
- PRD / tech-spec 的「Token 节省率 ≥ 30%」**在真实输入上不成立**（实测 0%（短）～22%（长），
  且省幅主要来自 system 兜底上限；见 §1.2 更正）；
- 这解释了为什么"预算快烧完"却指望不上压缩兜底（R19 的隐忧在此坐实）；
- **同日审计另查出 4 项真实异常**（逐项根因已读代码定位）：
  ① 限流把 `max_tokens` 砍到 100 → ReAct 决策 JSON 被截断 → 重试（最坏 `return error` 打断整轮），
  实测限流后 7 次调用 3 次重试、重试平均输入 9,108 > 正常 8,479，**疑似净负**；
  ② 兜底截断保留**头部**、丢弃**尾部** → 被砍的恰是**最新庭审历史**；
  ③ 预算拒绝只写 DB、**不进 FileLogger**（DB 35 行 vs 文件 33 条）；
  ④ 兜底摘要条件 `droppedCount > 5` 与 `MaxIterations=4`（非 system ≤ ~8、强制保留 3）不匹配 → **分支不可达**。
  （原列的"quick 波动 2.6 倍"经复核**已撤回** —— 是我自己误点「直接判决」跳过质证造成的取样失误。）
  详见 [`docs/COMPRESSION-REDESIGN-PROPOSAL.md`](../COMPRESSION-REDESIGN-PROPOSAL.md) §3。

### ✅ 决策与落地（2026-10-11）

用户拍板：

1. **预算**：**上调到 300000**（已落地）—— `config.DefaultBudgetPerSession` 与
   `agent_gateway.DefaultBudgetPerSession` 两处常量 + `.env.example` + 4 条测试同步；
   跨包一致性护栏 `TestBudgetDefault_ConfigAndGatewayAgree` 通过。⚠️ 服务器 `.env` 若显式写了
   该变量，上调默认值**不会覆盖**，上线时需手工同步（`.env` 属敏感文件，Agent 不代改）。
2. **压缩**：**本轮不修**。用户指出 **"系统提示词的压缩应放最低优先级"** —— 与代码现状一致
   （`CompressScored` 对 system 强制保留；且 v2.10 实测过"压 system → 销毁指令 → 模型编造证据"）。
   重构方案（把 system 拆成"契约块 + 素材块"）+ 附带修（兜底截断改为"保最近、砍最旧"）
   已写入 [`docs/COMPRESSION-REDESIGN-PROPOSAL.md`](../COMPRESSION-REDESIGN-PROPOSAL.md)，**未实施**。
   PRD/tech-spec/ADR 0006 的「≥30%」已按实测更正（0%～22%，见 ADR 0006 状态更新）。
3. **P2 可见面**：**先出方案文档** —— 见 [`docs/P2-VISIBILITY-PROPOSAL.md`](../P2-VISIBILITY-PROPOSAL.md)
   （澄清了"论证其实已可见、P2 真正问的是要不要给推理链"，推荐维持现状）。

### ✅ 2026-10-11 第 1 轮修复落地（M8-2 A1–A5）

用户 2026-10-11 追加拍板：**压缩重构 M8-2 立项做完整重构**（第 2 轮），**先修 4 项附带异常 + 补度量**（本轮）。本轮 **5 项全部落地并通过单测 + 真实 docker/浏览器链路验证**：

| 编号 | 修复 | 关键改动 | 回归测试 |
|---|---|---|---|
| **A1** | 兜底截断方向：由"砍尾部"改为"**保最近、砍最旧**"（保留契约头 + 最新历史） | `prompt_compressor.go` 新增 `truncateSystemContent` + `TranscriptHistoryMarker`（单一来源）；`react_runner.go` 引用该常量 | `prompt_truncate_direction_test.go`（5 例）、`react_runner_history_marker_test.go` |
| **A2** | 限流不再砍 JSON 输出：`react_think/reflect` 决策 JSON 纳入"关键输出"豁免（保留 max_tokens，仅降 temperature） | `throttler.go` 新增 `opts.JSONMode` 豁免分支 + `exempt()` 抽取 | `throttler_json_exempt_a2_test.go` |
| **A3** | 预算拒绝补文件日志（**含流式分支**，此前流式连 DB 都不写） | `gateway.go` Complete / StreamComplete 两个拒绝分支各补 `writeFileLog`（流式另补 `recorder.Record`） | `gateway_reject_filelog_test.go` |
| **A4** | 兜底摘要死分支：默认阈值 **5 → 3**（ReAct 实测 droppable 上限 4，原阈值永不触发） | `config.go` + `gateway_config.go` + `prompt_compressor.go` 三处默认值 + `.env.example` | `prompt_summary_reachability_a4_test.go` |
| **A5** | 新增"压缩实际节省率"指标 `prompt_compression_saved_ratio`（= 1 − after/before，0 = 没省） | `observability/metrics.go` + `prompt_compressor.go` 埋点 | `prompt_compressor_saved_ratio_a5_test.go` |

**真实链路验证证据**（dev compose + IAB 浏览器跑标准庭审；临时小预算 override 逼出各档，验证后已删、未入仓）：

- **A3**：预算耗尽时文件日志出现 `"status":"error"` + `error_msg="agent_gateway: token budget exhausted … ratio=1.44"`（修复前仅 DB 有、文件无）。
- **A2**：`react_think`（ratio 0.617）与 `react_speak_stream`（ratio 0.975）日志显示 `throttle_exempted=true` + `throttle_exempt_reason="json_output: truncated JSON is unusable"`，`max_tokens` 保留 500/1000 不被砍；`llm_throttle_exempted_total` 计数出现。
- **A1/A5**：`compressed=true, strategy=scored` 正常执行、无异常；`/metrics` 出现 `prompt_compression_saved_ratio`（本次短调用节省率 ≈ 0，与审计结论一致）。
- **A4**：默认阈值 3 已生效（`AGENT_GATEWAY_SUMMARY_INSERT_THRESHOLD=3`），replacement 可达性由单测护栏钉住。
- 业务链路无回归：标准庭审开场陈述正常生成、零 ERROR、`retry_count=0`；`budget_total=300000` 确认 M8-1 默认值生效。

**仍待第 2 轮**：M8-2 主重构（system 拆「契约块 + 素材块」，§2.2）—— 属行为变更，需 A/B 同题对跑 + 注入式回归 + §11 docker/浏览器实测。

### 🚧 2026-10-11 第 2 轮（M8-2 主重构）—— 代码完成，**真实链路验证未完成**

**已落地（本地提交，未 push）**：

- `message_section.go`：新增区块标记常量（`MessageSectionContract` / `MessageSectionMaterial`）+ `MarkContract/MarkMaterial/IsContractSystemMessage/IsMaterialMessage`。未标记的旧式 system 一律视作契约（保持"system 强制保留"旧行为）。
- `react_runner.go::buildInitialMessages`：system 拆为「契约块（baseRules + 工具协议，不可压）」+「素材块（历史区段标记 + 逐条历史行，可压）」多条消息。
- `prompt_compressor.go`：`CompressScored` 分离三态 —— 契约永不评分/丢弃；素材与普通消息同台参与评分/原子组/贪心；新增 `MaterialKeepRecentN`（默认 6）素材下限，保证最新 N 条历史不丢、旧素材可丢弃省 token。`CompressLegacy` 改为保留**全部** system（旧实现只留第一条，拆分后会误删素材）。
- `prompt_scorer.go`：素材块给中性权重 1.0（否则沿用 system 的 2.0 永不被丢弃，"旧素材可丢"失效）。
- 配置：`AGENT_GATEWAY_MATERIAL_KEEP_RECENT_N`（默认 6）贯通 config → gateway_config → compressor。
- 回归测试：`compression_section_split_test.go`（契约永不丢 / 素材下限保最近弃最旧 / legacy 保留全部 system）。

**验证状态**：`go build` + `go test -count=1 ./...` 全绿；真实庭审 docker + IAB 实测**进行到一半被用户叫停**（庭审已启动，未及分析日志/指标）。**第 2 轮的真实链路验证与 A/B 同题对跑尚未完成**，明天继续。**行为变更，在 A/B 完成前不应上线。**

### 建议动作（需用户决策）

1. **预算**：把 `AGENT_GATEWAY_BUDGET_PER_SESSION` 默认值上调（如 200000 → 300000），
   或按庭审模式（quick/standard/deep）分档；至少要让 standard 不再必然踩线。
2. **压缩**：让压缩真正作用于 system prompt（token 大头），或**明确限定"压缩只对
   message 列表生效"并下调对外宣称**——不能让"≥30% 节省"继续挂在文档里。
   → **2026-10-11 第 1 轮已完成附带修（A1–A4）；主重构（system 拆分）第 2 轮立项实施中。**
3. **度量**：补一条"压缩实际节省率"指标（现有 `prompt_compression_ratio` 是
   after/before **长度比**，0% 节省时值为 1.0，不直观）。→ **2026-10-11 第 1 轮 A5 已完成**
   （`prompt_compression_saved_ratio`）。

### 关联

- M6 / M7（本项是它们"问题 0 / 待跑"的**实测答案**，且结论反转）；
- [R19](deferred-items-2026-10-07.md)（预算用量没有指标 —— 已由第 3 轮 `budget_ratio` 解决，
  正因如此本场才能看到 ratio=1.05）；
- `docs/AGENT-GATEWAY-BUDGET-PLAN.md`（Phase 3 预留式预算用户已决定**不做**）。

---

## M9（发现即修，2026-10-11）调查员搜索失败在后端**无日志**（静默缺口）

### 怎么发现的

第 2 轮真实验证时，用户在庭审页面看到顶部 banner「调查员搜索失败: <query>…」。但后端
`logs` 里**完全没有对应记录** —— 无法判断是网络超时、限流、还是 key 失效。

### 根因（读码定位）

- `investigation.RecordFinding`（`backend/internal/investigation/service.go:73-76`）在**第一步
  就执行搜索**，失败即 `return nil, err` —— 此时**尚未**发 A2A dispatch/report，故 `a2a_messages`
  与 `investigation_findings` 都**不留痕**。
- `courtroom.DispatchInvestigator`（`service.go`）的 defer 把失败转成 WS
  `search.completed{success:false,error}` 发给前端（D2-L2 的 banner 就是它），但**不写后端日志**。
- ReAct 侧该错误只作为 `[tool_error] …` 回灌给 LLM（`react_runner.go:371`），也不落日志。
- ⇒ 失败只对「LLM 的下一轮输入」和「前端 UI」可见，**运维视角是黑洞**（AGENTS.md §6.2e 规则 3：
  沉默的 return 是长期隐患）。

### 现场取证

- 直接复现两条 query（中文 + 英文）对 bocha 均 **HTTP 200** → 该次失败是**瞬时**的
  （超时/取消/限流），而非查询本身非法。
- 强制失败验证修法：把 `BOCHA_API_KEY` 置为**非空但无效**的值（置空会命中启动 fail-fast
  `SEARCH_PROVIDER=bocha requires BOCHA_API_KEY`），跑庭审后日志出现：

  ```
  WARN investigator search failed  session_uuid=… dispatcher=prosecutor
       query="…" err="investigation: search: bocha: status 401: {\"message\":\"Invalid API KEY\"…}"
  ```

### 处置（已落地）

`courtroom/service.go` 的 `DispatchInvestigator` defer 在 `success=false` 分支补一条
`slog.Warn("investigator search failed", session_uuid, dispatcher, query, err)`。
下次同类失败可直接从日志定位原因。**不改搜索本身逻辑**（瞬时失败属正常，由前端 retry 兜底）。

---

## 核对方法（可复现）

本批结论全部来自真实代码 grep / read，不采信文档：

```bash
# M1：投影产物无消费者
grep -rn "WorkingMemory" backend/ --include=*.go          # 命中全在 context_view.go 自身
grep -rn "BuildContextView" backend/ --include=*.go | grep -v _test
                                                          # 唯一调用者 orchestrator_context.go:39

# M2：legacy 私有池只写不读
grep -rn "\.List(" backend/ --include=*.go | grep -v _test      # 空
grep -rn "memoryRepo\|memRepo" backend/ --include=*.go | grep -v _test   # 只有 Append

# M3：Beliefs 占位符
grep -rn "Beliefs" backend/ --include=*.go | grep -v _test

# M4：投影入口孤儿
grep -rn "SanitizeForViewer\|SanitizedPayload" backend/ --include=*.go | grep -v _test
                                                          # 只有定义 + 注释

# M5：公开 payload 明文 + 广播
sed -n '471,561p' backend/internal/agent/orchestrator.go
sed -n '180,212p' backend/internal/a2a/bus.go

# M6：四步压缩流水线的真实执行点 + 实跑复现数字
grep -rn "NewPromptCompressor\|\.Compress(" backend/ --include=*.go | grep -v _test
cd backend && go test ./internal/agent_gateway/ -run TestCompressionEval_StrategyComparison -v
ls -la .trae/documents/     # 确认 baseline 引用源是否存在（当前：不存在）
```

---

## 关联文档

- [ADR 0045 投影隔离硬化](../adr/0045-projection-isolation-hardening.md) —— D13 的决策与实施（本批 M1/M4 的直接前序）
- [deferred-items-2026-10-07.md](deferred-items-2026-10-07.md) §D13 —— 上一轮发现与"已知冗余"定性
- `backend/internal/agent/prompts_transcript_isolation_d13_test.go` —— 转写出口的注入式隔离测试（真正在生效的那重保险）
- `backend/internal/a2a/payload_whitelist_d13_test.go` —— 白名单完整性测试（保护的是未接线的投影层）

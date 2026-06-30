# 记忆系统 & A2A 重设计执行计划

> 版本：v1.1
> 状态：**v0.5 全部 PR 已完成（2026-06-30）；v0.5+ 修补完成**
> 目标：把私有记忆与 A2A Bus 统一为单一"MemoryBus"基础设施，提升对抗性保证 + 记忆可观测性。
> 预计周期：4 周（4 个 PR）

---

## v1.1 增量日志（2026-06-30）

### v0.5 全部 PR 完成

| PR | 内容 | 状态 |
|---|---|---|
| PR 1 | `internal/a2a/context_view.go` BuildContextView + 4 个 MessageType 常量 + 12 项测试 | ✅ |
| PR 2 | `reflect_classifier.go` MemoryHook + ReAct reflect 步骤自动写记忆 + 14 项测试 | ✅ |
| PR 3 | Orchestrator 注入 ContextView 到 system prompt（剥离对方 reasoning） | ✅ |
| PR 4 | 前端 `MemoryAuditPanel` + `MemoryTimeline` + 真实法庭 toggle + verdict 页幕后视角 | ✅ |

### v0.5+ 修补期（用户报告"策略笔记不出现"后端到端排查）

| # | 修复 | 文件 | 测试守门 |
|---|---|---|---|
| A | **SessionUUID vs SessionID 房间钥匙 bug**：`Message` 加 `SessionUUID` 字段；`Bus.Send` 优先用 `SessionUUID` 当 room key，否则 fallback `SessionID.String()` + WARN | [a2a/types.go](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/backend/internal/a2a/types.go)、[a2a/bus.go](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/backend/internal/a2a/bus.go) | `TestBus_Send_BroadcastRoomKey_UsesSessionUUIDNotSessionID` 显式 `NotEqual(SessionID.String())` |
| B | **前端 envelope 字段名错配**：前端读 `p.from_agent` 后端是 `p.from` —— 错配导致全部 entry 落到 `prosecutor` 然后被 sort/dedup 折叠。新增 `mapFromToAgentType()` 归一化函数 | [store/courtroomStore.ts](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/store/courtroomStore.ts) | （前端无测试基础设施，靠 schema freeze + 浏览器复测） |
| C | **recordSideEffects 双写 A2A Bus + 老表**：speak 完成后立即发一条 private `strategy_note` 到 A2A Bus（前端可见），不再依赖 LLM 主动填 memory 字段 | [agent/orchestrator.go](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/backend/internal/agent/orchestrator.go) | `TestOrchestrator_ProsecutorSpeak_PublishesA2AMessage` |
| D | **MemoryEntry 结构化字段**：`stance` / `confidence` / `reasoning` 独立字段；后端 payload 拆结构化字段，前端 `MemoryTimeline` 渲染成结构化卡片（立场 chip + 置信度条 + 推理段） | [types/index.ts](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/types/index.ts)、[MemoryTimeline.tsx](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/components/courtroom/MemoryTimeline.tsx) | （前端无单测；视觉复测） |

### v0.5+ UX 改进（用户报告"看不出策略笔记有什么用"后）

| # | 改进 | 文件 |
|---|---|---|
| U1 | **MemoryTimeline 4 种 kind 配色**：blue / rose / amber / emerald 左边框 + chip 区分 | [MemoryTimeline.tsx](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/components/courtroom/MemoryTimeline.tsx) |
| U2 | **置信度条**：≥80 深青 / 50-80 中性 / <50 灰；数字 + 进度条并行 | 同上 |
| U3 | **MemoryAuditPanel header 精简**：去掉冗余 ×N（sidebar tab 已有 badge） | [MemoryAuditPanel.tsx](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/components/courtroom/MemoryAuditPanel.tsx) |
| U4 | **Footer 改 kind 分布 chip**：去掉总计数 chip，改成 `{kind} × N` 的 kind-by-kind summary | 同上 |
| U5 | **AgentAvatar 思考脉冲**：CSS `@keyframes ring-think-pulse` 1.6s 呼吸，控方绛红 / 辩方深青（之前 ring 是 Tailwind 默认灰色无动画） | [app/globals.css](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/app/globals.css)、[AgentAvatar.tsx](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/components/courtroom/AgentAvatar.tsx) |
| U6 | **顶部 ThinkingBubble 撤掉**：用户明确反馈顶部那个气泡不想要 | [MessageHistory.tsx](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/frontend/components/courtroom/MessageHistory.tsx) |

### 已发现但**未做**（留给 v0.6）

- **证据 ID 显示成 UUID 根因**：entry.linkedEvidenceIds 拿到的是数据库主键 UUID，不是 E001 格式 display_id。需要后端 recordSideEffects 加一层 `evidence_id → display_id` JOIN 映射。前端先显示 UUID fallback。

---

（v1.0 原内容继续在下方 ↓）

---

## 0. 背景与动机

### 0.1 当前实装盘点（2026-06-29）

| 组件 | 位置 | 状态 |
|---|---|---|
| A2A Message Bus | `internal/a2a/bus.go` | ✅ 完整（12 项测试） |
| A2A SanitizedPayload | `a2a/types.go#L70-L79` | ⚠️ **方法存在但无人调用** |
| Private Memory 写入 | `internal/agent/orchestrator.go#L241-L254` | ⚠️ **被动 dump strategy_note** |
| Private Memory 读取 | — | ❌ **完全断链** |
| LLM 调用审计 | `model.LLMCall` 表 | ❌ **表存在，零写入** |
| MemoryType（evidence_eval 等） | `internal/private_memory/repository.go` | ❌ **枚举定义但无自动写入** |

### 0.2 三大差距

1. **私有记忆写入 100%，读取 0%** — LLM 看不到自己之前的策略笔记
2. **A2A SanitizedPayload 未生效** — LLM 仍能拿到对方的 reasoning（虽未注入，但 Bus 路径上有隐患）
3. **private_memories 与 a2a_messages 是两套重复基础设施** — 隔离/审计/路由分散在两个表里

### 0.3 业界调研结论

参考来源（节选）：
- [Microsoft Multi-Agent Reference Architecture](http://microsoft.github.io/multi-agent-reference-architecture/print.html)：4 种多 Agent 共享记忆模式（supervisor-mediated / shared block / cross-thread / blackboard）
- [LLM Agent Memory Survey 2026（arXiv:2603.07670）](https://blog.csdn.net/qcx23/article/details/161904173)：三维分类法（认知载体 × 形式 × 触发）+ Write-Manage-Read 闭环
- [MetaReflection（Microsoft 2024）](https://www.aclanthology.org/2024.emnlp-main.477.pdf)：LLM 显式输出 type 比关键词分类准确度高 4-16.82%
- [Futureaiit Expand-Contract](https://futureaiit.com/blog/zero-downtime-migrations) + [Zylos Schema Migration](https://zylos.ai/research/2026-02-27-schema-migration-strategies-ai-agent-systems)：schema 迁移必须 3 阶段零停机

---

## 1. 已确认的 5 个核心决策

| # | 决策点 | 选择 | 一句话理由 |
|---|---|---|---|
| 1 | 私有记忆底层 | **改用 A2A private 消息** | A2A 已提供隔离/审计/路由，MemoryBus 统一 |
| 2 | 注入策略 | **全文注入** | 50 条上限远低于 128K，零摘要损失 |
| 3 | memory_type 分类 | **LLM 显式输出 type（嵌入现有 reflect）** | 零额外 LLM 调用，准确度高 |
| 4 | 旧表迁移 | **双写过渡 1 个版本周期后废弃** | 零风险 + 可回滚 |
| 5 | 法官可见性 | **默认显示，toggle 屏蔽** | 差异化卖点 + 教育价值 |

### 决策 #1 详细论证（私有记忆底层用 A2A）

**业界映射**：Microsoft Reference Architecture 把 Supervisor-Mediated Channel 列为最常用模式，A2A Bus 实质就是 Supervisor 模式 + MemoryBus 的统一体。

**对比表**：

| 维度 | A. 改用 A2A private | B. 保留独立表 + 视图层 | C. 双写 |
|---|---|---|---|
| 隔离/审计基础设施 | ✅ 复用 | ❌ 重复实现 | ⚠️ 双倍维护 |
| 前端可视化 | ✅ a2a.message 已推送 | ❌ 额外 WS 事件 | ⚠️ 双源不一致风险 |
| 隔离测试 | ✅ 12 项已覆盖 | ✅ 9 项已覆盖 | ✅ 合并 |
| 迁移成本 | ⚠️ 需写迁移脚本 | ❌ 0 | ⚠️ 持续成本 |
| **结论** | **✅ 推荐** | ❌ 短期主义 | ❌ 长期债务 |

### 决策 #2 详细论证（注入策略）

**数据**：50 条 strategy_note × ~200 token = **10K tokens 上限**，远低于 128K context window（仅 5% 利用率）。

**业界 4 策略对比**：

| 策略 | Token 控制 | 信息保留 | 决策Court 适用性 |
|---|---|---|---|
| 全量注入 | ❌ 易爆 | ✅ 100% | ✅ **完全匹配** |
| 固定窗口 | ✅ | ❌ 永久丢失 | ❌ |
| 滚动+摘要 | ✅ | ⚠️ 远期压缩 | 🟡 第二阶段 |
| 语义检索 | ✅ | ⚠️ 检索误差 | ❌ |

### 决策 #3 详细论证（分类机制）

**业界 3 类方法**：

| 方法 | 代表 | 准确度 | 延迟 |
|---|---|---|---|
| 关键词规则 | 早期 Reflexion | ❌ 脆 | ✅ 零 |
| LLM 显式输出 type | MetaReflection | ✅ 高 | ⚠️ 多 1 次 LLM |
| LLM 主动 reflect | RMM / MemGPT | ✅✅ | ❌ 反射调用 |

**改良方案**：在现有 `reflect` 步骤的 JSON schema 增 `memory_type` + `memory_note` 字段，**零额外 LLM 调用**。

### 决策 #4 详细论证（表迁移）

**业界 Expand-Contract 3 阶段**：

```
Week 1-2: Phase 1 Expand    — 双写（memoryRepo.Append + a2aBus.Send(private)）
Week 2:   Phase 2 Migrate   — 后台脚本复制历史数据
Week 3:   Phase 2.5 Shadow  — 对比新旧表一致性
Week 4:   Phase 3 Contract  — 切读新表 → drop 旧表
```

### 决策 #5 详细论证（法官可见性）

**真实法庭调研**（legalclarity.org / thelawtoknow.com）：
- 律师庭审准备笔记：**不公开**
- 陪审员审议室对话：no-impeachment rule，**不公开**
- 但 DecisionCourt 不是真实法庭，是 **AI 决策可视化平台**

**产品定位**：差异化卖点 → 用户能看到律师"内心戏"。

**实现**：默认显示 + "真实法庭模式" toggle + 庭审结束后自动解锁"幕后视角"。

---

## 2. 4 周实施计划

### PR 1：A2A ContextView 投影 + MessageType 扩展

**目标**：实现私有记忆的基础设施层，为后续 PR 打地基。

**交付物**：
1. 新增 4 个 MessageType：`strategy_note` / `opponent_weakness` / `self_correction` / `evidence_eval`
2. 新增 `internal/a2a/context_view.go`：`BuildContextView(sessionID, selfAgent)` 方法
3. 新增测试：`context_view_test.go`（≥ 8 项）

**改动文件**：
- `internal/a2a/types.go`（+12 行：4 个常量）
- `internal/a2a/context_view.go`（+150 行：新增文件）
- `internal/a2a/context_view_test.go`（+200 行：新增测试）

**验收标准**：
- [ ] 单测：控方收到辩方 public speech → reasoning 字段为空
- [ ] 单测：控方收到自己 private strategy_note → 完整内容
- [ ] 单测：辩方无法 ListVisibleTo 控方 private 消息
- [ ] 单测：orchestrator 能看到全部
- [ ] `go test ./internal/a2a/...` 全部通过

**风险**：低（纯新增，不破坏现有 12 项测试）

**详细规划** 见 `todolist1-pr1-contextview.md`（本文档末尾附 6）

---

### PR 2：ReAct reflect schema 扩展 + 自动写记忆

**目标**：让 LLM 在 reflect 步骤直接产出 type + memory_note，自动写入 A2A private 通道。

**交付物**：
1. 扩展 `ReflectAction` JSON schema：`memory_type` + `memory_note` 字段
2. `internal/agent/reflect_classifier.go`：解析后自动 `a2aBus.Send(MemoryTypePrivate)`
3. 集成测试：3 轮 cross-exam 验证 strategy_note 类型多样

**改动文件**：
- `internal/agent/types.go`（+15 行：扩展 struct）
- `internal/agent/react_runner.go`（+30 行：reflect 分支处理）
- `internal/agent/reflect_classifier.go`（+100 行：新增）
- `internal/agent/reflect_classifier_test.go`（+150 行：新增）

**验收标准**：
- [ ] LLM 在 reflect 时填 `memory_type` + `memory_note` → 自动写入 a2a_messages
- [ ] 集成测试：3 轮 cross-exam，至少产出 3 种不同 type 的私有记忆
- [ ] LLM 不填字段时，旧行为不变（向后兼容）
- [ ] `go test ./internal/agent/...` 全部通过

**风险**：低（新增字段，向后兼容）

---

### PR 3：Orchestrator 接入 ContextView + Prompt 注入

**目标**：让 LLM 在 speak 前看到自己的私有策略笔记历史 + 对方（仅 public + sanitized）的发言历史。

**交付物**：
1. `internal/agent/orchestrator.go`：ProsecutorSpeakWithReAct / DefenderSpeakWithReAct 调 `BuildContextView`
2. Prompt 注入：system prompt 增加 `## 你之前的策略笔记` 段落
3. Integration 测试：验证记忆确实影响后续发言（不同 seed 产出不同策略）

**改动文件**：
- `internal/agent/orchestrator.go`（+60 行）
- `internal/agent/prompts.go`（+25 行：注入段落模板）
- `internal/courtroom/service_react_test.go`（+50 行：新增断言）

**验收标准**：
- [ ] LLM 调用时 system prompt 包含 50 条 strategy_note（注入全文）
- [ ] 同一 trial，第 1 轮 vs 第 3 轮 LLM 输出明显不同（记忆起作用）
- [ ] 控方 prompt 不含辩方 reasoning（即使 sanitized 也不应包含）
- [ ] 单场庭审 token 增加 ≤ 5%
- [ ] `go test ./...` 全部通过

**风险**：中（改动 hot path，需性能验证）

---

### PR 4：前端 MemoryAuditPanel + 文档同步

**目标**：用户能看到双方私有策略笔记的演化过程（差异化卖点）。

**交付物**：
1. 前端新组件：`components/courtroom/MemoryAuditPanel.tsx`
2. "真实法庭模式" toggle（默认 off → 显示）
3. 庭审结束后解锁"幕后视角"详情页
4. 文档同步：`docs/` 下 5 个 .md 文件全部更新

**改动文件**：
- `frontend/components/courtroom/MemoryAuditPanel.tsx`（+200 行：新增）
- `frontend/app/court/[id]/page.tsx`（+30 行：集成）
- `frontend/store/courtroomStore.ts`（+50 行：状态）
- `docs/*.md`（更新版本号 + 新增章节）

**验收标准**：
- [ ] 前端能按 type 过滤 a2a.message 事件
- [ ] toggle 隐藏时，仅显示 type 和数量
- [ ] 庭审结束后"幕后视角"页可访问
- [ ] 5 个 .md 文档全部更新，版本号 v0.5
- [ ] `pnpm build` + `go build ./...` 通过

**风险**：低（纯前端增量）

---

## 3. 双写迁移时间线（PR 4 完成后启动）

```
Week 1 (PR 4 完成时)
  └─ recordSideEffects 同时调 memoryRepo.Append + a2aBus.Send(private)

Week 2-3（观察期）
  ├─ 对比 memoryRepo.List(sessionID, agentID) vs a2a.ListVisibleTo(sessionID, agentID)
  ├─ 验证两者数量、内容一致
  └─ CI 加断言：每场庭审 strategy_note 数量两表相同

Week 4（废弃期）
  ├─ 全量切读到 a2a_messages
  ├─ 旧表停止写入
  └─ 1 周观察无问题 → drop 旧表 + 旧测试
```

---

## 4. 验证矩阵

| 维度 | 测试类型 | 指标 |
|---|---|---|
| **隔离性** | 单测 | 控辩双方互相看不到对方 private；A2A bus_test.go 12 项全绿 |
| **记忆注入** | 单测 + 集成 | system prompt 含 50 条 strategy_note；同 trial 不同轮输出有差异 |
| **记忆多样性** | 集成 | 3 轮 cross-exam 至少产出 3 种 type |
| **Token 成本** | 性能 | 单场庭审 token 增加 ≤ 5% |
| **Prompt Caching** | 性能 | system prompt 命中 provider cache（前缀稳定） |
| **可审计** | E2E | 前端 MemoryAuditPanel 完整回放 |
| **向后兼容** | 回归 | go test ./... 全部通过；frontend build 通过 |

---

## 5. 风险登记

| 风险 | 等级 | 缓解措施 |
|---|---|---|
| 注入 strategy_note 导致 token 超限 | 中 | PR 3 加 token 计数保护，超限降级到 top-30 |
| LLM 不填 memory_type 字段 | 低 | PR 2 兜底：未填时按默认值 strategy_note |
| 双写期数据不一致 | 低 | PR 4 完成后立即停旧表写，仅读 1 周 |
| A2A 表查询性能下降 | 低 | 已建索引 (session_id, created_at) |
| 前端渲染 50 条笔记卡顿 | 低 | 虚拟滚动 + type 过滤 |
| SanitizedPayload 未生效的回归 | 中 | 加回归测试：sanitize 必包含 reasoning 删除 |

---

## 6. 关联文档更新清单

执行本文档时，需同步更新：

| 文件 | 更新内容 |
|---|---|
| `docs/decisioncourt-prd.md` | §4.5 私有记忆 → §4.5 Episodic Memory via A2A；§4.6 法官可见性 toggle |
| `docs/decisioncourt-agent-design.md` | §3.3 时序图加 ContextView 注入点；§4.4 MessageType 扩展 4 个 |
| `docs/decisioncourt-api-design.md` | §5 WS 事件 a2a.message schema 加 memory_type 字段 |
| `docs/decisioncourt-tech-spec.md` | §9.2 私有记忆架构改为 a2a.PrivateChannel；新增 §9.3 迁移策略 |
| `AGENTS.md` | §6 文档清单加 memory-a2a-redesign.md |

---

## 7. 后续可探索（不阻塞 MVP）

- **PR 5**：Prompt Caching 接入（Anthropic 90% / OpenAI 50% off）
- **PR 6**：LLM 调用审计接通（`model.LLMCall` 表已有 schema）
- **PR 7**：跨庭审合并（同一用户多次使用时，合并 strategy_note）
- **PR 8**：长 session 摘要（超过 50 条时启用滚动窗口+摘要）

---

## 附录 A：业界引用

1. Microsoft Multi-Agent Reference Architecture — <http://microsoft.github.io/multi-agent-reference-architecture/print.html>
2. LLM Agent 记忆系统综述（arXiv:2603.07670）— <https://blog.csdn.net/qcx23/article/details/161904173>
3. MetaReflection (Microsoft 2024) — <https://www.aclanthology.org/2024.emnlp-main.477.pdf>
4. Reflective Memory Management (RMM) — <https://www.themoonlight.io/en/review/in-prospect-and-retrospect-reflective-memory-management-for-long-term-personalized-dialogue-agents>
5. Futureaiit Zero-Downtime Migrations — <https://futureaiit.com/blog/zero-downtime-migrations>
6. Zylos Schema Migration Strategies — <https://zylos.ai/research/2026-02-27-schema-migration-strategies-ai-agent-systems>
7. Gliding Horse Agent Harness Memory Architecture — <https://blog.csdn.net/2604_96270735/article/details/162292441>
8. Context Window Management for Long-Running Agents — <https://inductivee.com/blog/context-window-management-production>
9. Agent Harness 上下文管理四种策略 — <https://blog.51cto.com/u_16099251/14580853>
10. Multi-Agent Shared Memory Patterns — <https://jatinbansal.com/ai-engineering/multi-agent-shared-memory/>
11. Jury Deliberation Legal Foundations — <https://thelawtoknow.com/2025/12/03/jury-deliberation/>
12. Database Migrations at Scale — <https://sujeet.pro/articles/database-migrations-at-scale>
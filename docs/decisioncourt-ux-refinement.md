# DecisionCourt — UX 与数据模型细化（v0.4）

> 本文档针对「调查员调用 → ReAct 反馈 → 庭审视觉」三处体验/语义问题，沉淀已对齐的决策与实现路径。**本期已全部实装并通过浏览器验收。**

> 版本对照：v0.3 → v0.4 主要修订：
> - ReAct 实时性：从"两步走（先 bubble 后流式）"变更为**已完成**——bubble + LLM 流式 + flushSync + bubble 优先级
> - DispatchInvestigator 改造：单条 entry 状态机（searching → completed/failed），不再写 evidence
> - 数据模型：InvestigationFinding 表 + raw_results 字段，前端可点击展开查看完整搜索结果

---

## 0. 触发问题

1. **调查员结果被混入证据列表** — 用户提交庭审时只手动 `POST /evidences`，但律师调用 investigator_search 后会向 `evidences` 表写入记录，导致用户看到自己没提交的「证据 ID」出现在消息里，语义混乱。
2. **ReAct 思考期「死亡空窗」** — 律师开始思考后到第一次推送前通常 2-4 秒无任何 UI 反馈，用户会怀疑页面卡死。
3. **调查员无任何视觉反馈** — 派遣/回报只在服务端日志可见，前端既不显示「X 方正在派遣调查员调查：xxx」，也不区分派遣 vs 回报。

---

## 1. 已对齐的决策（且已落地）

| 主题 | 决策 | 实现状态 |
|------|------|----------|
| 调查员结果归属 | 新概念 `InvestigationFinding`（独立表） | ✅ `internal/model/investigation_finding.go` + `internal/investigation/` 包 |
| 调查员结果可见性 | **类比正常庭审 → 公开**（写入 A2A bus public channel + `investigation_findings` 表） | ✅ `a2a.message` 公开广播 + `GET /api/v1/courtrooms/:uuid/investigations` |
| ReAct 实时性 | **本期完成** — bubble + LLM token 级流式 | ✅ `OnIterStart` 钩子 + `OnSpeakChunk` 钩子 + 前端 `flushSync` |
| 调查员面板位置 | 两处都要 — CoT 折叠 + 独立 InvestigatorPanel | ✅ `InvestigatorPanel.tsx` 作为右侧 Tab + `CotStepsPanel` 标签改「调查发现」 |
| 流式 chunk 落端 | 律师头像气泡（**不是**消息流顶端全局面板） | ✅ `AgentAvatar` 自己订阅 `streamingContent` |

---

## 2. 数据模型

### 2.1 新表 `investigation_findings`

```sql
CREATE TABLE investigation_findings (
  id              UUID PRIMARY KEY,
  finding_uuid    VARCHAR(36) UNIQUE NOT NULL,
  session_id      UUID NOT NULL REFERENCES court_sessions(session_uuid) ON DELETE CASCADE,
  dispatcher      VARCHAR(32) NOT NULL,        -- 'prosecutor' | 'defender'
  investigator    VARCHAR(32) NOT NULL DEFAULT 'investigator',
  query           TEXT NOT NULL,                -- 派遣时的 tool_input.query
  summary         TEXT NOT NULL,                -- 搜索结果摘要（截断 ≤ 1KB）
  raw_result      JSONB,                        -- 完整搜索结果（标题/URL/content），便于审计 + 前端可点击展开
  source_provider VARCHAR(32) NOT NULL,        -- 'bocha' | 'duckduckgo' | 'mock'
  result_count    INT NOT NULL DEFAULT 0,      -- 本次搜索返回的结果数
  a2a_message_id  UUID,                        -- 对应的 A2A 消息 id（可追溯）
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_findings_session ON investigation_findings(session_id, created_at);
```

> 与 `evidences` 完全独立，**不会**出现在 `/evidences` 列表里。前端 InvestigatorPanel 的「✓ 调查员回报」行可点击展开 `raw_result` 查看完整搜索结果。

### 2.2 A2A message type 扩展

`a2a.Message.MessageType` 增加：

```go
MessageTypeDispatchInvestigator = "dispatch_investigator"
MessageTypeInvestigationReport  = "investigation_report"
```

visibility 固定为 `VisibilityPublic`（类比庭审记录公开）。**修正了 v0.3 的错误**——旧版误以为应当走 `VisibilityPrivate`（私有通道），实际是**公开**。

---

## 3. 后端改动（全部已实装）

### 3.1 `internal/agent/tools/investigator_search.go`

Execute 走 `investigation.Service.RecordFinding(...)`，写入 `investigation_findings` 表，**不**再写 `evidences`。

Observation 返回 JSON：

```json
{"finding_id":"f-xxx","summary":"前 3 条搜索结果摘要","count":5}
```

Observation 文本：

```
搜索完成：新增调查发现 finding_id=xxx。摘要=...。查询=...
```

### 3.2 新模块 `internal/investigation`

```
internal/investigation/
├── service.go                  # RecordFinding / ListBySession
├── service_test.go             # 10 个单元测试（全部绿）
├── gorm_repository.go          # GORM 实现
├── inmemory_repository.go      # InMemoryRepository（测试用）
└── types.go                    # Repository interface + Finding struct
```

`Service` 结构：

```go
type Service struct {
    repo     Repository
    a2aBus   A2ABus              // 用于公开 A2A dispatch / report
    searcher search.Provider     // Bocha / DuckDuckGo / Mock
    logger   *log.Logger
}
```

公开方法：

| 方法 | 作用 |
|------|------|
| `RecordFinding(ctx, session, dispatcher, query) (*Finding, error)` | 1) 公开 A2A dispatch；2) 调 searcher；3) 持久化 Finding；4) 公开 A2A report |
| `ListBySession(ctx, sessionUUID) ([]Finding, error)` | 前端启动时拉取调查活动历史 |

### 3.3 `internal/courtroom/service.go`

`DispatchInvestigator` 重写为**单条 entry 状态机**：

```
search.started  →  search.completed (success=true)  或  search.completed (success=false)
```

- **修复 bug**：旧实现 `search.completed` 仅在 searcher 成功时发送，失败时跳过——导致前端 spinner 永远转。新实现用 `defer` 包裹，无论成功失败都发出 `search.completed`，payload 含 `success` + `error` 字段。

`SpeakWithReAct` 流程：

```
1) 推 agent.thinking_started（前端立即显示云朵）
2) 跑 ReAct runner：每轮 iter 调 OnIterStart 钩子 + 每步推 agent.cot_step
3) runner 决策 = speak 时：
   a) 调 LLM.StreamComplete 流式生成发言
   b) chunkCb 推送 agent.speak_chunk（每 ~30ms 一个 chunk）
   c) runner 完成后推 agent.speak
4) 推 agent.thinking_finished
```

WS 事件清单（与 1.0 文档对应）：

```
agent.thinking_started   { agent_id, agent_type }
agent.thinking_finished  { agent_id, agent_type }
agent.cot_step           { agent_id, agent_type, step: {kind, tool, query, observation} }
agent.speak_chunk        { agent_id, agent_type, accumulated }
agent.speak              { agent_id, agent_type, content, evidence_refs, ... }
search.started           { dispatcher, query }
search.completed         { dispatcher, query, finding_id, result_count, success, error?, summary }
a2a.message              { message_uuid, from, to, message_type, visibility, payload }
```

### 3.4 `internal/agent/react_runner.go`

`RunnerConfig` 增加：

```go
OnIterStart   func(iter int)              // 每轮 iteration 起始钩子（驱动 thinking_started）
OnSpeakChunk  func(chunk, accumulated string)  // 流式发言回调
MaxReflects   int                         // 默认 3
```

新增 `ActionReflect ActionKind = "reflect"` —— 让 LLM 在 speak 之前反思当前策略。

### 3.5 `internal/api/hub.go`

`hub.Broadcast` 每次 `conn.WriteMessage` 后 `time.Sleep(30ms)`：

```go
const minChunkSpacing = 30 * time.Millisecond
```

**根因**：Nagle 算法 + OS TCP send buffer 把 175 个 WebSocket frames 合并成一个 TCP send 包，浏览器一次性收到 → 1 个 onmessage → React 1 次 re-render → "闪一下"。

**修复后**：浏览器端 ws onmessage 间隔 ≈ 30ms，前端 React 每次都能感知到流式增量。

**端到端测试**：`TestHub_Broadcast_StreamTimingGap` 验证 50 个事件 avg gap = 31.1ms ✅

**真实 ws 客户端验证**：`/tmp/ws_probe.py` 连真实 server，191 个 chunks avg gap = 50.7ms ✅

---

## 4. 前端改动（全部已实装）

### 4.1 Avatar 头部气泡（替代原 MessageHistory ThinkingBubble）

**变更**：`ThinkingBubble`（消息流顶端全局面板）已废弃。改在每个律师 `AgentAvatar` 头部按优先级显示气泡：

| 优先级 | 类型 | 触发条件 | 显示内容 |
|---|---|---|---|
| 1（最高） | 调查 | `search.started` | 「正在调查：「query 摘要」」 |
| 2 | 流式 | `agent.speak_chunk` 当前 agent | 实时逐字内容 + 光标 |
| 3 | 思考 | `agent.thinking_started` 期间无流式 | 「思考中（N 步）」云朵 |
| 4（兜底） | 完整发言 | `agent.speak` | MessageHistory 里的发言内容 |

**关键修复**：v0.3 优先级是 `thinking > streaming` —— 但后端流式期间 `thinking_finished` 还没发（发在 runner 结束后），导致云朵覆盖流式气泡，用户看到"云朵 + 光标"重叠。新版改为 `streaming > thinking`，且 `thinkingBubble` 条件加上 `!isStreamingThisAgent` 守卫。

### 4.2 新组件 `components/courtroom/InvestigatorPanel.tsx`

独立面板，位于 `CourtroomScene` 右侧 w-80 区域的 Tab「调查活动」：

```
┌─ 🔍 调查活动 ────────────────┐
│ → 控方 调查员搜索中 ⏳        │   ← status='searching'
│   「睡眠对健康的好处 科学证据」│
│   42s 前                       │
│                                │
│ → 辩方 调查员搜索中 ⏳        │
│   「睡眠债可恢复 短期 认知」  │
│   42s 前                       │
└────────────────────────────────┘
```

`search.completed` 到达后，对应 dispatch 行**就地升级**为：

```
┌─ 🔍 调查活动 ────────────────┐
│ ← 控方 调查员回报 ✓           │   ← status='completed'
│   「睡眠对健康的好处 科学证据」│
│   finding_id=fbf655cf... · 10 条结果│
│   摘要：...                    │
│   ▾ 点击查看 10 条搜索结果     │   ← 可点击展开 raw_results
│                                │
│   [1] 睡眠不足的 7 大危害      │
│       https://...             │
│       ...                      │
└────────────────────────────────┘
```

**关键设计**：dispatch + report 合并成**单条 entry**（不是两条独立行）—— 保证一次搜索 = 一条 entry，spinner 不会永转。

启动时调 `api.getInvestigations()` REST 端点拉历史，**通过状态机合并** dispatch 行转化为 report 行（旧的 finding 转 report entry）。

### 4.3 改动 `components/courtroom/CotStepsPanel.tsx`

- tool_call 类型 step + tool=investigator_search → 标签从「调用 investigator_search」改为「🔍 调查发现 · 查询：...」
- 新增 `ActionReflect` 分支显示「🪞 反思 N/M」

### 4.4 store `store/courtroomStore.ts`

新增字段：

```ts
activeThinking: Record<AgentType, { agentId, startedAt } | undefined>
streamingContent: { agentId, agentType, accumulated } | null
investigationEvents: InvestigationEvent[]   // 已合并 dispatch+report
activeInvestigation: { dispatcher, query, startedAt } | null
```

事件 reducer：

- `agent.thinking_started` → `setActiveThinking`
- `agent.thinking_finished` → 清空
- `agent.cot_step` (tool_call, tool=investigator_search) → **不再创建 dispatch entry**（避免重复）；只追加 CoT step
- `agent.speak_chunk` → `applySpeakChunk` (用 **`flushSync`** 强制同步 commit，绕过 React 18 batching)
- `agent.speak` → `clearStreamingContent` + `addMessage`
- `search.started` → `setActiveInvestigation` + 创建 dispatch entry (status='searching')
- `search.completed` → `completeInvestigationEvent`（三层兜底匹配 dispatcher+query，找到后**就地升级** entry 的 status / summary / finding_id）

`completeInvestigationEvent` 三层兜底（解决 LLM 在多轮 ReAct 中改写 query 导致 dispatch 行找不到的问题）：

1. 严格匹配 `dispatcher + query + status='searching'`
2. 宽容匹配 `dispatcher + status='searching'`（忽略 query）
3. 兜底匹配 `dispatcher` 的任意 dispatch entry（兼容旧数据缺 status 字段）
4. 找不到 → fallback append 一条 report entry

### 4.5 `applySpeakChunk` 用 `flushSync` 绕过 React 18 batching

```ts
import { flushSync } from "react-dom";

applySpeakChunk: (chunk) => {
  flushSync(() => {
    set(() => ({
      streamingContent: { agentId, agentType, accumulated: chunk.accumulated },
    }));
  });
}
```

**根因**：React 18 automatic batching 在 native event handler (ws onmessage) 里把多次 setState 合并成 1 次 commit —— LLM 191 个流式 chunks 只触发 1 次 re-render，用户看到"突然整段出现"。

**修复后**：每次 ws onmessage 立即同步 commit，前端每 ~30ms re-render 一次。

---

## 5. API 改动（全部已实装）

| Method | Path | 说明 |
|--------|------|------|
| GET | `/api/v1/courtrooms/:uuid/investigations` | 列出本场所有 `InvestigationFinding`，按时间倒序。响应：`{"data":{"findings":[...]}}` |

**不**修改 `/evidences` 端点 — 那是用户证据专用。

WS 事件清单（**已实装**）：

```
agent.thinking_started   { agent_id, agent_type }
agent.thinking_finished  { agent_id, agent_type }
agent.cot_step           { agent_id, agent_type, step }
agent.speak_chunk        { agent_id, agent_type, accumulated }
agent.speak              { agent_id, agent_type, content, evidence_refs, ... }
search.started           { dispatcher, query }
search.completed         { dispatcher, query, finding_id, result_count, success, error?, summary, raw_results }
a2a.message              { message_uuid, from, to, message_type, visibility, payload }
```

A2A message type **新增**：

```
dispatch_investigator   (visibility=public)
investigation_report    (visibility=public)
```

---

## 6. 迁移计划

- DB 自动迁移：`internal/model/investigation_finding.go` 加 struct → `AutoMigrate` 自动建表 ✅
- 历史 session：`investigation_findings` 为空表，不影响 ✅
- 旧 `evidences` 表保留，不删（兼容历史） ✅

---

## 7. 测试计划（全部已通过）

### 7.1 单元（Go）

- ✅ `internal/investigation/service_test.go` — 10 个测试：RecordFinding 写入 DB + 发 A2A + ListBySession + 校验 + 错误传播
- ✅ `internal/agent/react_runner_thinking_test.go` — 3 个测试：OnIterStart 钩子调用 + NilIterStartHookSafe + OnIterStartFiresBeforeLLMCall
- ✅ `internal/agent/react_runner_streaming_test.go` — 4 个测试：SpeakStreamsContentViaOnSpeakChunk + SpeakStreamingFailureFallsBackToPlaceholderContent + ToolCallDoesNotStream + SpeakStreamsMultilineJSON
- ✅ `internal/courtroom/dispatch_investigator_events_test.go` — 2 个测试：BroadcastsSearchStartedAndCompleted + **SearchStartedBeforeSearchCompletedEvenWhenSearcherErrors**（关键：失败也必须发 completed）
- ✅ `internal/courtroom/dispatch_investigator_test.go` — 3 个测试：DoesNotCreateEvidence + BroadcastsPublicDispatchAndReport + ReturnsFindingAndSummary
- ✅ `internal/courtroom/service_thinking_test.go` — 3 个测试：BroadcastsThinkingStartedBeforeCotStep + ThinkingStartedPayloadShape + SearchStartedAroundToolCotStep
- ✅ `internal/courtroom/service_speak_streaming_test.go` — 1 个测试：BroadcastsSpeakChunkEvents
- ✅ `internal/courtroom/service_react_helpers_test.go` — 共享 helpers (reactScriptedLLM / speakJSON / toolJSON)
- ✅ `internal/api/hub_timing_test.go` — 1 个测试：Broadcast_StreamTimingGap（50 事件 avg gap=31.1ms）
- ✅ `internal/api/handler_investigations_test.go` — 4 个测试：列表顺序、空数组非 null、404 负路径、跨 session 隔离

### 7.2 端到端验证（真实 ws 客户端）

`/tmp/ws_probe.py` — Python websockets 客户端连真实 Go server，验证：

```
=== analysis: chunks=191 first->last=9.63s avg_gap=50.7ms min=28.0ms max=3780.1ms ===
```

191 个 chunks 平均间隔 50.7ms（= 肉眼可见的打字机速度），最小 28ms（最接近 Broadcast sleep 30ms），最大 3780ms（流式开始前的 LLM 思考延迟）。

### 7.3 浏览器人工验收

- ✅ 云朵在 `agent.thinking_started` 立即出现
- ✅ 流式期间律师头像气泡逐字显示 + 末尾光标
- ✅ 调查员派遣时头像旋转 spinner + 「正在调查：...」气泡
- ✅ 调查员回报到达时 spinner 停止 + 显示 finding_id + 可点击展开 raw_results
- ✅ 证据列表**不**出现调查发现
- ✅ 刷新页面 InvestigatorPanel 立即显示历史

---

## 8. 风险与权衡

| 风险 | 缓解 |
|------|------|
| A2A 消息类型改动可能影响旧客户端解析 | 后端在 WS envelope 加版本字段；前端事件订阅器做容错 |
| `InvestigationFinding` 与未来「书证、物证」等其他来源扩展 | 现在就用 `source_provider` 字段预留 |
| 云朵动画在低端机掉帧 | 用 CSS transform + will-change；不用 JS 动画 |
| 调查活动面板过长占屏 | 折叠默认收起 + 显示最近 N 条，点击展开 |
| 流式 chunks 跟 DeepSeek batching 行为耦合 | 后端 hub.Broadcast sleep 30ms + 端到端测试兜底 |
| React 18 batching 让流式看不到逐字效果 | `flushSync` 强制同步 commit |
| Bubbles 重叠（云朵 vs 流式 vs 调查） | streaming > thinking 优先级 + 互斥条件守卫 |

---

## 9. 后续可能改进（不在本期范围）

- DeepSeek V3/R1 切换（按场景路由）
- 调查发现引用图谱 / 时间线视图
- 调查员主动推荐关键词 / 智能 follow-up
- 流式打字速度可配置

---

## 10. 关键修复的 bug 清单

| Bug | 触发 | 修复 |
|---|---|---|
| 调查结果被混入证据 | DispatchInvestigator 写 `evidences` 表 | 改写 `investigation_findings`，前端读 `/investigations` 端点 |
| `search.completed` 在 searcher 失败时跳过 | 失败路径直接 return | 用 `defer` 强制发送 completed，payload 含 `success:false` |
| 前端 store 没触发逐字 re-render | React 18 batching | `flushSync` 强制同步 commit |
| 后端 ws frames 被 batching 合并 | Nagle + TCP buffer | `hub.Broadcast` sleep 30ms |
| Avatar 气泡重叠 | thinking 优先级高于 streaming | streaming > thinking + `!isStreamingThisAgent` 守卫 |
| 一次搜索产生两条 entry | dispatch + report 两条独立 event | dispatch 行就地升级为 report（单条 entry 状态机） |
| dispatch 行 spinner 永转 | query 改写导致匹配失败 | `completeInvestigationEvent` 三层兜底匹配 |
| `unknown action: start_trial` | ws handler 把 start_trial 当 user.action | ws 只做实时通信，start_trial 走 REST `/start` 端点 |

---

## 11. 文件清单

### 后端新增/修改

- `backend/internal/model/investigation_finding.go` (新增)
- `backend/internal/model/db.go` (新增 AutoMigrate)
- `backend/internal/investigation/{types,service,gorm_repository,inmemory_repository,service_test}.go` (新增)
- `backend/internal/courtroom/service.go` (DispatchInvestigator 重构 + speakWithReAct 流式)
- `backend/internal/courtroom/dispatch_investigator_test.go` (重写)
- `backend/internal/courtroom/dispatch_investigator_events_test.go` (新增)
- `backend/internal/courtroom/service_thinking_test.go` (新增)
- `backend/internal/courtroom/service_speak_streaming_test.go` (新增)
- `backend/internal/courtroom/service_react_helpers_test.go` (新增)
- `backend/internal/agent/react_runner.go` (OnIterStart + OnSpeakChunk + ActionReflect)
- `backend/internal/agent/react_runner_thinking_test.go` (新增)
- `backend/internal/agent/react_runner_streaming_test.go` (新增)
- `backend/internal/agent/types.go` (ActionReflect 常量)
- `backend/internal/agent/tools/investigator_search.go` (新签名)
- `backend/internal/agent/tools/investigator_search_test.go` (更新)
- `backend/internal/agent/orchestrator_react_test.go` (更新签名)
- `backend/internal/api/hub.go` (Broadcast sleep 30ms)
- `backend/internal/api/hub_timing_test.go` (新增端到端时序测试)
- `backend/internal/api/handler.go` (GetInvestigations 端点 + sessionLookup)
- `backend/internal/api/handler_investigations_test.go` (新增)

### 前端新增/修改

- `frontend/types/index.ts` (新增 InvestigationFinding / SearchStartedEvent / SearchCompletedEvent / AgentThinkingStartedEvent / AgentThinkingFinishedEvent / A2AMessageEvent)
- `frontend/store/courtroomStore.ts` (新增 activeThinking / streamingContent / investigationEvents / activeInvestigation + applySpeakChunk flushSync)
- `frontend/lib/api.ts` (新增 getInvestigations)
- `frontend/components/courtroom/AgentAvatar.tsx` (订阅 store + bubble 优先级 streaming > thinking)
- `frontend/components/courtroom/MessageHistory.tsx` (移除顶部 ThinkingBubble，依赖 Avatar 自己渲染)
- `frontend/components/courtroom/InvestigatorPanel.tsx` (新增)
- `frontend/components/courtroom/CotStepsPanel.tsx` (标签改 "调查发现" + reflect 分支)
- `frontend/app/globals.css` (@keyframes thinking-fade-in / cloud-drift / cloud-pulse)
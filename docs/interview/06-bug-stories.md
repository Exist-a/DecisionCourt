# 06 · 真实 Bug 故事（5 个）—— 体现"工程化让 AI 不再神秘"

> **目标**：用 5 个真实 bug 故事，展示 DecisionCourt 在 v0.8 白盒化中发现的隐藏问题。**这些故事是面试的"杀手锏"**—— 你能讲出"具体某天我做了什么发现什么"会让面试官觉得你不是"包装项目"。
> **配套**：[`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)（完整审计 + 时间线 + 修复 + 启示）

---

## 0. 一句话总结

> v0.8 + v0.8.3 一共暴露 **5 个隐藏 bug**——全部由**真实庭审回归**而非单元测试发现。**最深刻的是 bug 4**："信念轨迹只显示 1 条"—— 数据库 / API / 后端 / 前端**每一层单独看都对**，但**数据在某个中间环节丢了**。

---

## 5 个 bug 总览

| # | 名称 | 严重度 | 暴露路径 | 关键代码 |
|---|---|---|---|---|
| 1 | llm_calls 外键约束失败 | 🔴 P1 | v0.8 demo 当天 stdout ERROR | `agent_gateway/gorm_store.go` |
| 2 | A2A SessionID fallback WARN | 🟡 P2 | v0.8 demo 当天 stdout WARN | `a2a/bus.go:170-174` |
| 3 | a2a_message_throughput 计数缺失 | 🟢 P3 | v0.8 demo 查 /metrics 端点 | `api/hub.go:Broadcast` |
| 4 | **信念轨迹只显示 1 条（应是 16 条）** | 🟡 P2 | **v0.8.3 真实庭审回归** | `belief/engine_v06.go:97` |
| 5 | 判决书"AI 可视化"按钮无响应 | 🟢 P3 UX | v0.8.3 真实庭审回归 | `BehindTheScenesPanel.tsx` |

---

## Bug 1：🔴 P1 · llm_calls 外键约束失败（业务跑得欢但审计表 0 行）

### 现象

v0.8 白盒化 demo 当天（2026-07-02 18:55），stdout 立刻抛错误：

```
2026/07/02 18:55:11 backend/internal/agent_gateway/gorm_store.go:47 
ERROR: insert or update on table "llm_calls" violates foreign key constraint 
"fk_court_sessions_llm_calls" (SQLSTATE 23503)

INSERT INTO "llm_calls" ("session_id",...) 
VALUES ('6dc16a5f-884e-4657-b12c-359190f78719',NULL,...)
```

### 根因（v0.5 之前就存在）

`backend/internal/agent_gateway/gorm_store.go:30`（修复前）：

```go
// 错误代码：把业务 key 当主键
var sessionID uuid.UUID
if r.SessionUUID != "" {
    if u, err := uuid.Parse(r.SessionUUID); err == nil {
        sessionID = u  // ❌ 错把 session_uuid 当 session_id（DB 主键）写入
    }
}
```

**业务层困惑**：注释写"需要 lookup 主键 session_id（FK）"（**意图对**），但代码只做了 `uuid.Parse`（**实现错**）。

**意图与实现脱节 = bug 经典形态**。

### 影响（白盒化后才发现）

| 维度 | 影响 |
|---|---|
| 业务功能 | **完全无感** —— 网关写审计失败仅 log |
| token 成本 | **完全无法统计** —— llm_calls 表 0 行 |
| 调用次数 | **完全无法统计** |
| 失败率 | **完全无法统计** |
| 故障定位 | **完全无法做** —— "这次为什么慢"无法关联 |

**本质**：业务跑得欢但**关键审计表 0 行** —— 这种 bug 在没白盒化的项目里**几周到几个月都发现不了**。

### 修复（5 行）

```go
var sessionID uuid.UUID
if r.SessionUUID != "" {
    var session model.CourtSession
    if err := model.DB.Select("id").
        Where("session_uuid = ?", r.SessionUUID).
        First(&session).Error; err != nil {
        slog.Warn("...", "request_id", r.RequestID, "session_uuid", r.SessionUUID)
        return nil
    }
    sessionID = session.ID  // ← 真做 lookup
}
```

**新增 4 项单元测试**（`gorm_store_test.go`）：NilDB / EmptySessionUUID / SessionNotFound / NewGORMStore。

### 面试故事模板（90 秒）

> "我最自豪的真实 bug：v0.8 demo 当天，**stdout JSON 日志立刻抛出 'fk_court_sessions_llm_calls' 违反外键**。
>
> **根因**：recorder 把 `session_uuid`（业务 key）当成 `session_id`（DB 主键）写数据库，**外键失败**。但**业务完全没受影响**——网关 fail-soft，LLM 响应正常。
>
> **可怕之处**：业务跑得欢，但 `llm_calls` 表 **0 行** —— **token 成本 / 调用次数 / 失败率全部无法统计**。如果没白盒化，这个 bug 可能隐藏到月底做 token 对账时才发现，**那时候已经超支都不知道**。
>
> **修复**：5 行代码（uuid.Parse → DB lookup）+ 4 项单元测试。**定位仅 5 分钟**——因为 v0.8 的白盒化让 ERROR 立刻从 stdout 抛出来。

---

## Bug 2：🟡 P2 · A2A SessionID fallback WARN（数据库主键 vs 业务 key 混用）

### 现象

v0.8 demo 当天，stdout WARN：

```
[a2a] WARN: a2a.message broadcast using SessionID.String() fallback 
— caller should set Message.SessionUUID to match hub room key 
(got sessionID=4c21d466-9cd1-4944-a10d-6d07c298d7a2)
```

### 根因

`backend/internal/a2a/bus.go:170-174`：

```go
sessionUUID := msg.SessionUUID
if sessionUUID == "" {
    sessionUUID = uuid.UUID(msg.SessionID).String()  // fallback
    log.Printf("[a2a] WARN: ... fallback ...")
}
```

**含义**：调用方没填 `Message.SessionUUID`，bus 退而求其次用 `SessionID.String()`（**DB 主键**）当房间 key。

**风险**：hub room key 应该是业务 key（`6dc16a5f-...`），fallback 用了 DB 主键（`4c21d466-...`），**WS 推送可能进错房间**。

### 当前处理：文档化，不修

WARN 是"调用方提醒"，fallback 行为安全（数据库主键刚好兼容 hub 当前实现）。**v0.8.1 后续修复**：找到没填 `Message.SessionUUID` 的调用方统一填上。

### 面试洞察

> "**这个 bug 教会我**：外部依赖（这里是 Agent 间的消息总线）的**契约不严**会导致 fallback 逻辑累积。**契约严 = 接口必填字段必须明示**。**Slog WARN 是提醒，但本质是接口设计问题**。v0.8.1 我会做'接口必填检查 + lint 工具 + 调用方统一修复'。"

---

## Bug 3：🟢 P3 · a2a_message_throughput 计数缺失（指标 vs 业务的脱节）

### 现象

`GET /metrics` 返回的 `counters` 里**没有** `a2a_message_throughput_total`，但 A2A 业务明明在跑（stdout 看到 `[a2a] prosecutor → investigator`）。

### 根因

`backend/internal/api/hub.go` 修复前：

```go
func (h *Hub) Broadcast(sessionUUID string, event courtroom.Event) {
    h.mu.RLock()
    clients, ok := h.rooms[sessionUUID]
    h.mu.RUnlock()

    if !ok {  // ⚠️ 没人订阅 WS 时直接 return
        return
    }
    if h.metrics != nil {
        h.metrics.IncCounter(...)  // ⚠️ 在 if !ok 之后
    }
}
```

**问题**：没人订阅时 `!ok` → 直接 return → metrics 不执行 → "业务上 A2A 真的发了"但"metrics 显示 0"。

### 修复（7 行）

```go
func (h *Hub) Broadcast(...) {
    // v0.8.1 修复：metrics 必须在订阅检查之前
    if h.metrics != nil {
        h.metrics.IncCounter(observability.MetricA2AThroughputTotal, ...)
    }
    
    h.mu.RLock()
    clients, ok := h.rooms[sessionUUID]
    h.mu.RUnlock()
    if !ok { return }
    // ...
}
```

### 面试洞察

> "**这个 bug 教会我**：**指标必须在分支检查之前执行**——因为指标是'观测行为'，不是'业务行为'。哪怕没人订阅，**业务上'广播'这个动作发生了，metrics 就该记录**。**指标脱钩业务逻辑** 是 observability 的设计纪律。**这一行位置错误，让 prod 监控一晚上是 0，运维看不到任何流量增加**。"

---

## Bug 4：🟡 P2 · 信念轨迹只显示 1 条（应是 16 条）—— **本次面试杀手锏**

### 现象

v0.8.3 真实庭审回归，用户反馈：
> "我在这场添加了四个证据，但是只有一次是放入信念轨迹中"

### 白盒化系统化审计

| 数据源 | 状态 | 数值 |
|---|---|---|
| 数据库 `belief_diffs` 表 | ✅ 16 行 | 4 evidence × 4 agent 全部写入 |
| API `GET /belief-diffs` | ✅ 返回 16 条 | count=16, distinct id=16 |
| 后端 stdout `belief.diff` WS 事件 | ⚠️ 16 个事件但 **ID 全部为 uuid.Nil** | 内存零值 |
| 前端 `store.appendBeliefDiff` 幂等检查 | ❌ 第 2-16 条被去重 | 用户看到 1 条 |

### 根因

`backend/internal/belief/engine_v06.go:97` 修复前：

```go
diff := model.BeliefDiff{
    // ❌ 没有 ID 字段！
    SessionID: sessionID,
    Round:     round,
    // ...
}
```

`engine_v06.go` 创建 diff 时未分配 ID，依赖 `gorm_repository.go:24-26` 内 fallback：

```go
if diff.ID == uuid.Nil {
    diff.ID = uuid.New()
}
```

**Insert 数据库时补 ID → DB 16 行有不同 ID ✅**（正确）

**但 broadcast 时用内存 `d.ID` = uuid.Nil → WS 16 个 belief.diff 事件 ID 全是零值**。

**前端 store idempotency**：
```ts
appendBeliefDiff: (diff) => set((state) => {
    if (state.beliefDiffs.some((d) => d.id === diff.id)) {
        return state;  // ← 第 2-16 条全 skip
    }
    // ...
})
```

### 链路解读（最能说明问题的图）

```
backend belief engine        repo.Insert              service.broadcast
            │                       │                          │
  创建 diff (ID=Nil)  ───►  检测到 ID==Nil            ◄── 用内存 d.ID=Nil
                             生成真 ID 写入 DB ◄──────   广播 WS 16 个 belief.diff
                                   │                    id 全是 uuid.Nil 零值
                                   ▼                          │
                           DB 16 行有不同 ID                   ▼
                                                       前端 store.appendBeliefDiff
                                                       幂等检查 some(d.id === diff.id)
                                                       因为 ID 相同 → 后 15 条 skip
                                                       ▼
                                                       用户看到 1 条 BeliefDiffCard
```

### 修复（5 行）

```go
diff := model.BeliefDiff{
    // v0.8.3 白盒化回归发现：必须显式分配 ID，让 service.broadcastEvent
    // 转发出的 belief.diff WS 事件携带 distinct id。否则前端 store 的
    // idempotency 逻辑会把后 15 条全部去重 → 用户看到 1 条而非 16 条。
    ID:               uuid.New(),  // ← 加这一行
    SessionID:        sessionID,
    Round:            round,
    // ...
}
```

### 30 秒面试故事

> "我印象最深的 bug：v0.8.3 跑了一次真实庭审，**信念轨迹只显示 1 条**，**应该是 16 条**（4 evidence × 4 agent）。
>
> **可怕之处**：每一层（数据库 / API / 后端 / 前端）**单独看都对**，但数据在某个中间环节丢了。数据库正确写入 16 行；API 返回 16 条不同 ID；但后端 WebSocket 广播时，16 个 belief.diff 事件的 ID **全是零值（uuid.Nil）**，前端 store 的幂等检查把后 15 条去重掉了。
>
> **根因是 engine_v06.go 创建 belief_diff 时未显式分配 ID**，依赖 `repo.Insert` fallback 才补 ID。fallback 修补数据库写入路径，但 broadcast 路径直接用了**内存零值**。
>
> **核心启示**：**数据层正确 ≠ 链路正确**，必须端到端真实跑业务 + 每层数据自洽才能发现。**这种 bug 是 sneaky production bug**——单元测试完全测不出来。**修了一行 `uuid.New()`**。"

---

## Bug 5：🟢 P3 · 判决书"AI 可视化"按钮无响应（设计意图错实现）

### 现象

v0.8.3 真实庭审，用户反馈：
> "最后判决书的策略笔记的可视化按钮不能点击"

### 根因（设计意图 vs 实现的脱节）

`frontend/components/courtroom/BehindTheScenesPanel.tsx` 修复前：

```tsx
<MemoryAuditPanel
  entries={entries}
  redactedMode={false}                                    // ← 永远 false
  onToggleRedacted={() => {
    /* no-op: post-trial redaction is intentionally disabled */  // ← 空函数
  }}
/>
```

**设计意图**（v0.5 决定）：post-trial 强制公开全部策略笔记（"幕后视角"是完成庭审的奖励）。**但没隐藏按钮**，UX 看起来像 bug。

### 修复（4 个 props 透传）

```tsx
// verdict page 加 state
const [redacted, setRedacted] = useState(false);

// 传给 BehindTheScenesPanel
<BehindTheScenesPanel
  entries={memoryEntries}
  redactedMode={redacted}
  onToggleRedacted={() => setRedacted(!redacted)}
/>

// BehindTheScenesPanel 接受 + 透传
export function BehindTheScenesPanel({ entries, redactedMode, onToggleRedacted }) {
  return (
    <MemoryAuditPanel
      entries={entries}
      redactedMode={redactedMode}
      onToggleRedacted={onToggleRedacted}
    />
  );
}
```

### 面试洞察

> "**这个 bug 教会我**：**UX bug 也是真 bug**。
>
> 后端数据完全正确（策略笔记都入库），但用户觉得'按钮坏了'。**一旦用户对产品失去信任，后续 feature 加再多用户也会先怀疑**。
>
> **设计意图 vs 实现的脱节**：注释写'no-op: post-trial redaction is intentionally disabled'，**意图对**，但**实现 = 把按钮留着 = 看起来像 bug**。**bug 的本质是'意图与用户感知的脱节'**。
>
> **修复**：让 verdict 页面真可切换 redactedMode，35 行代码（4 个 props）。**核心教训**：**'意图没实现' 也是 bug** —— 即使数据完整呈现、即使 backend 正确、即使用户能看见策略笔记，**'按钮看着能用但不能点'就是 bug**。"

---

## 【总结】5 个 bug 的 4 个共同启示

### 启示 1：**业务跑得欢 ≠ 系统健康**（bug 1 / bug 3）
> 没有白盒化的系统 = 不知道自己 token 花了多少钱 / 失败率多高 / 流量有没有起来。**业务正确 ≠ 系统健康**。

### 启示 2：**意图与实现脱节 = bug 经典形态**（bug 1 / bug 5）
> bug 1 的注释"需要 lookup 主键 session_id（FK）"写对了意图但代码错。bug 5 的注释"no-op: post-trial redaction is intentionally disabled"写对了意图但 UX 错。**意图在注释里 + 实现不在 = bug 仓库**。

### 启示 3：**数据层正确 ≠ 链路正确**（bug 4，最深刻）
> 每一层单独看都对（数据库 / API / 后端 / 前端），但数据在某个中间环节丢了。**必须端到端真实跑业务 + 每层数据自洽**才能发现。**这种 bug 单元测试完全测不出来**。

### 启示 4：**白盒化 ROI = "持续回归 → 持续暴露 → 持续修复"**
> 5 个 bug 全部由**真实庭审回归**而非单元测试发现。**单元测试覆盖率 ≠ 系统健康度**。**真实业务跑 = 终极测试**。

---

## 【为什么这是面试杀手锏】

1. **真实具体**：你讲"2026-07-02 18:55 我做了一个 v0.8 demo stdout 立刻暴露 llm_calls 外键失败"——比"我们很重视代码质量"**有说服力 100 倍**。
2. **业务理解深**：你能讲出"用户跑了 4 个证据的庭审，为什么 belief 只显示 1 条"——证明你**真的理解每一层数据的语义**。
3. **工程化思考**：你能讲出"指标必须在分支检查之前"——证明你**有系统级思考**，不只是写代码。
4. **诚实面对**：你能讲出"我没做到 L3 Prometheus，是我有意 L2 完整"——证明你**有 trade-off 意识**。

**这 5 个 bug 故事，可以让面试官相信你是一个"实战过、能讲清、有思考"的候选人**。

---

## 名词速查

| 名词 | 含义 |
|---|---|
| sneaky production bug | 上线前发现不了、上线后业务跑得欢、1% 路径出错的 bug |
| Intent-implementation gap | 意图与实现的脱节 |
| End-to-end trace 自洽 | 端到端 trace 链条上每层数据一致 |
| Whitebox 回归 | 白盒化驱动的真实业务回归测试 |
| L2 / L3 maturity | observability 5 级模型的第 2 / 3 级 |

---

**下一步**：
- [`07-key-terms.md`](07-key-terms.md) —— 全部技术名词解释
- [`08-faq-30-questions.md`](08-faq-30-questions.md) —— 30 个面试高频问题
- [`09-data-snapshot.md`](09-data-snapshot.md) —— 项目真实数据快照（让数据替你说话）


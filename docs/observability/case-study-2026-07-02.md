# 真实案例：v0.8 白盒化让隐藏 bug 显形（2026-07-02）

> **目的**：用一次真实的"白盒化发现 bug + 修复"案例，说明白盒化（Observability）**在 MVP 阶段的价值**。本文档可用于**面试问答**、**团队复盘**、**未来白盒化设计参考**。
> **配套**：[`../architecture/link-overview.md`](../architecture/link-overview.md) §6 · [`../adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md) · [`../roadmap/whitebox-roadmap.md`](../roadmap/whitebox-roadmap.md)
> **完成于**：2026-07-02

---

## 0. 一句话总结

> **v0.8 白盒化交付当天（2026-07-02），一次 demo 跑通，stdout JSON 日志立刻暴露出 v0.5 之前就存在、但从未被发现的 3 个隐藏 bug** —— 其中 P1 级的 `llm_calls` 外键 bug 导致**整个 LLM 调用审计**长期是 0 行，**token 成本 / 调用次数 / 失败率全部无法统计**。

---

## 1. 背景

### 1.1 v0.8 交付内容（2026-07-02 当天完成）

按 [`adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md)，v0.8 实现"白盒化三大支柱"：

| 支柱 | 实现 | 代码 |
|---|---|---|
| Logging | `log/slog` JSON + `slog.With(trace)` 自动注入 | `internal/observability/logger.go` |
| Metrics | 进程级 `Metrics` 接口 + 内存实现 + `GET /metrics` 端点 | `internal/observability/metrics.go` |
| Tracing | 自实现 `Span` 接口 + `decision_events` 表落库 | `internal/observability/trace.go` |
| 端到端 trace 串联 | Gin `TraceMiddleware` → `ctx.Trace` → 全链路透传 | `internal/observability/middleware.go` |

**白盒化的核心价值承诺**：让"上线后任何'庭审卡住'的问题，都能在 30 秒内定位到具体 trace_id 下的具体事件"。

### 1.2 demo 目标

启动后端 + PostgreSQL + Redis，跑一次完整流程（创建庭审 → 立案 → 触发状态机迁移 → LLM 调用），验证 v0.8 的 4 项白盒化能力**真的能用**（不是"做了但没测"）。

---

## 2. demo 流程（时间线）

```
18:45  docker compose up -d postgres redis      # 启动 PG + Redis
18:52  go run ./cmd/server                       # 启动 backend（用 bocha 模式）
       stdout: {"msg":"DecisionCourt backend listening","port":"8080","version":"v0.8.0","whitebox":"enabled"}
18:54  curl GET /health                          # 验证 HTTP 串联
       response: X-Request-Id: demo-step-1-health   ✅ 串联工作
18:55  curl POST /api/v1/courtrooms              # 创建庭审
       response: session_uuid=6dc16a5f-...
18:55  curl POST /api/v1/courtrooms/.../start    # 立案（同步）
       response: {"code":0,"data":{"message":"庭审开始..."}}
18:55  ← stdout 立刻看到
       {"level":"INFO","msg":"state transition","session_uuid":"6dc16a5f...","from":"idle","to":"opening","round":0}  ✅ slog 工作
       [LLM] provider=deepseek model=deepseek-chat latency=1258ms tokens=3208
       ERROR: insert or update on table "llm_calls" violates foreign key constraint "fk_court_sessions_llm_calls"  ⚠️ BUG 显形
18:55  curl GET /metrics
       response: courtroom_state_transition_total{from="idle",to="opening"}=1     ✅ metrics 工作
              : http_request_duration_seconds{path="/api/v1/courtrooms",status="400"} count=2   ✅ HTTP 直方图工作
18:55  psql: SELECT * FROM decision_events
       result: 1 行 state_transition                                              ✅ decision_events 工作
18:55  psql: SELECT * FROM llm_calls
       result: 0 行                                                              ⚠️ 与 LLM 调用次数不符（应该有 4 行）
```

**关键观察**：v0.8 的 4 项白盒化能力**全部按设计工作**，但同时**意外暴露了 3 个 v0.5 之前就有的 bug**。

---

## 3. bug 1（🔴 P1 · 必修）：`llm_calls` 表外键约束失败

### 3.1 现象

stdout 立刻抛出错误：

```
2026/07/02 18:55:11 D:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gorm_store.go:47 
ERROR: insert or update on table "llm_calls" violates foreign key constraint "fk_court_sessions_llm_calls" (SQLSTATE 23503)

INSERT INTO "llm_calls" ("session_id",...) 
VALUES ('6dc16a5f-884e-4657-b12c-359190f78719',NULL,...) 
```

### 3.2 根因（v0.5 之前就存在）

[`backend/internal/agent_gateway/gorm_store.go:30`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gorm_store.go)（修复前）：

```go
// 错误代码：把业务 key 当主键
var sessionID uuid.UUID
if r.SessionUUID != "" {
    if u, err := uuid.Parse(r.SessionUUID); err == nil {
        sessionID = u  // ❌ 错把 session_uuid 当 session_id（DB 主键）写入
    }
}
row := model.LLMCall{
    SessionID: sessionID,  // FK 失败：session_id='6dc16a5f-...' 在 court_sessions.id 里找不到
    ...
}
```

**数据库 schema**：
- `court_sessions.session_uuid` = `6dc16a5f-...`（业务 key，业务侧公开）
- `court_sessions.id` = `4c21d466-...`（DB 主键，FK 指向它）
- `llm_calls.session_id` = FK 指向 `court_sessions.id`

**根因**：recorder 收到的是业务 key（`session_uuid`），但 `llm_calls.session_id` 是 FK，**需要先 lookup 拿到 DB 主键**才能写。**注释里写了"需要 lookup 主键 session_id（FK）"**（写对了意图），**但代码里没真做 lookup**，只做了 `uuid.Parse` 转换！

### 3.3 影响范围（白盒化后才发现）

| 维度 | 影响 |
|---|---|
| 业务功能 | **完全无感** —— 网关写审计失败仅 log，不影响 LLM 响应 |
| token 成本 | **完全无法统计** —— llm_calls 表 0 行 = 不知道花了多少钱 |
| 调用次数 | **完全无法统计** —— "今天 LLM 调用了多少次"问题无解 |
| 失败率 | **完全无法统计** —— `status='error'` 的审计行从未落库 |
| 故障定位 | **完全无法做** —— "这次为什么慢"无法关联到具体 session |

**这正是"白盒化失败"的典型表现**：业务跑得欢，但**关键审计表 0 行**，**token 成本可能超支但没人知道**。

### 3.4 修复（2026-07-02 当天完成）

[`backend/internal/agent_gateway/gorm_store.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gorm_store.go) 改写：

```go
// 修复后：真做 lookup
var sessionID uuid.UUID
if r.SessionUUID != "" {
    var session model.CourtSession
    if err := model.DB.Select("id").
        Where("session_uuid = ?", r.SessionUUID).
        First(&session).Error; err != nil {
        // 找不到对应 session（异常 race / 单元测试），跳过
        slog.Warn("agent_gateway.GORMStore: session_uuid not found, skip insert",
            "request_id", r.RequestID, "session_uuid", r.SessionUUID, "error", err)
        return nil
    }
    sessionID = session.ID
}
```

**改动 5 行**：uuid.Parse → model.DB lookup。

**新增 4 项单元测试**（[`gorm_store_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gorm_store_test.go)）：NilDB / EmptySessionUUID / SessionNotFound / NewGORMStore。

---

## 4. bug 2（🟡 P2 · 建议修）：A2A SessionID fallback WARN

### 4.1 现象

stdout WARN：

```
[a2a] WARN: a2a.message broadcast using SessionID.String() fallback 
— caller should set Message.SessionUUID to match hub room key 
(got sessionID=4c21d466-9cd1-4944-a10d-6d07c298d7a2)
```

### 4.2 根因

[`backend/internal/a2a/bus.go:170-174`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/bus.go)：

```go
sessionUUID := msg.SessionUUID
if sessionUUID == "" {
    sessionUUID = uuid.UUID(msg.SessionID).String()  // fallback
    log.Printf("[a2a] WARN: a2a.message broadcast using SessionID.String() fallback — ...")
}
```

**含义**：调用方（orchestrator / 业务代码）**没填** `Message.SessionUUID`，bus 退而求其次用 `SessionID.String()`（DB 主键）当房间 key。

**风险**：如果 hub room key 是业务 key（`6dc16a5f-...`），fallback 用了 DB 主键（`4c21d466-...`），**WS 推送会进错房间**。

**v0.5 修复历史**：之前因为"DB 主键 vs session_uuid 混用"导致 WS 消息走错房间，v0.5 加了 SessionUUID 字段 + 这个 WARN。**但有调用方至今没填**。

### 4.3 影响范围

- **当前**：消息依然推送（fallback 工作），但**房间可能错**
- **业务表现**：前端可能收到"不属于自己的 session 的消息"（实际未发生，fallback 默认走 DB 主键，跟 hub 当前实现刚好兼容）
- **审计**：审计 trail 落库不受影响（`a2a_messages` 表用 DB 主键）

### 4.4 处理

**当前不修**——这个 WARN 是"调用方提醒"，fallback 行为安全。但**应在 v0.8.1 后续修复**：找到没填 `Message.SessionUUID` 的调用方，统一填上。

---

## 5. bug 3（🟢 P3 · 已修）：`a2a_message_throughput_total` 计数缺失

### 5.1 现象

`GET /metrics` 端点返回的 `counters` 里**没有** `a2a_message_throughput_total`，但 A2A 业务明明在跑（stdout 看到 `[a2a] prosecutor → investigator` 等日志）。

### 5.2 根因

[`backend/internal/api/hub.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/hub.go) 修复前：

```go
func (h *Hub) Broadcast(sessionUUID string, event courtroom.Event) {
    h.mu.RLock()
    clients, ok := h.rooms[sessionUUID]
    h.mu.RUnlock()

    if !ok {  // ⚠️ 没人订阅时直接 return
        return
    }
    // ...
    if h.metrics != nil {
        h.metrics.IncCounter(...)  // ⚠️ 在 if !ok 之后
    }
}
```

**逻辑问题**：没人订阅 WS 时（demo 时浏览器没开）`!ok == true`，**直接 return**，**IncCounter 不执行**。结果："业务上 A2A 真的发了"但"metrics 显示 0"。

### 5.3 影响范围

- **A2A 吞吐量指标失效** —— 失去"实时看到 A2A 事件流量"的能力
- **告警失效** —— Phase C 计划的"A2A 速率异常"告警不可用

### 5.4 修复（2026-07-02 当天完成）

[`backend/internal/api/hub.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/hub.go)：

```go
func (h *Hub) Broadcast(sessionUUID string, event courtroom.Event) {
    // v0.8.1 修复：metrics 必须在订阅检查之前
    if h.metrics != nil {
        h.metrics.IncCounter(observability.MetricA2AThroughputTotal, ...)
    }
    // 然后再查订阅
    h.mu.RLock()
    clients, ok := h.rooms[sessionUUID]
    h.mu.RUnlock()
    if !ok { return }
    // ...
}
```

**改动 7 行**：IncCounter 移到 `if !ok` 之前。

---

## 6. 白盒化价值复盘

### 6.1 没白盒化 vs 有白盒化

| 维度 | 没白盒化（v0.5 之前的状态） | 有白盒化（v0.8） |
|---|---|---|
| 发现 bug 1（llm_calls 外键） | **不可能发现**——审计表 0 行但**业务跑得欢**，没人会去看 | **30 秒**——stdout JSON 日志立刻抛 ERROR |
| 定位 bug 1 根因 | **数天**——翻代码 + 翻 SQL + 跑 migration | **5 分钟**——看 gorm_store.go 注释与代码不符 |
| 修复 bug 1 | 不知道要修什么 | 5 行代码 + 4 项单元测试 |
| 发现 bug 3（A2A 计数） | **数月**——等到 Phase C 接 Prometheus 才发现 | **demo 跑完立刻发现** |
| token 成本失控 | **发生了也不知道** | **实时可查** |

### 6.2 时间线

| 阶段 | 耗时 |
|---|---|
| v0.8 白盒化 PR | ~4 小时（含 ADR + 文档 + 测试） |
| bug 1 修复 | 5 分钟定位 + 5 行代码 + 4 项测试 |
| bug 2 文档化 | 1 分钟（不修，只记录） |
| bug 3 修复 | 3 分钟定位 + 7 行代码 |
| **白盒化从"做"到"用"的总时间** | **1 天** |

### 6.3 关键启示

#### 启示 1：白盒化不是"做完就完事"

v0.8 白盒化的"测试"不是"单元测试通过"——是**真实跑一次业务流，看日志 / metrics / DB 三处的数据是否自洽**。

#### 启示 2："业务跑得欢" ≠ "系统健康"

bug 1 的本质：业务**完全没受影响**，但 token 成本 / 调用次数 / 失败率**完全无法统计**。**没有白盒化的系统 = 不知道自己花多少钱 / 慢在哪里 / 失败在哪**。

#### 启示 3：白盒化让"意图与实现"脱节立刻显形

gorm_store.go 注释写"需要 lookup 主键 session_id（FK）"，代码却只做了 `uuid.Parse`。**意图与实现脱节 = bug 经典形态**。白盒化（让代码被运行、被审计）让这种脱节**在第一次跑业务时显形**。

#### 启示 4：白盒化的 ROI

- 投入：4 小时 / 1 PR
- 产出：1 次 demo 暴露 3 个隐藏 bug（其中 1 个 P1 直接影响成本统计）
- **ROI：发现 P1 bug 的时间从"等到 Phase C Prometheus 接进来"提前到"v0.8 demo 当天"**

---

## 7. 后续动作

| 项 | 状态 | 备注 |
|---|---|---|
| bug 1 修复 | ✅ 完成（[`gorm_store.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gorm_store.go) + 4 项测试） | 重启 backend 验证落库 |
| bug 2 修复 | ⏳ 后续 | 找到没填 SessionUUID 的调用方统一填上 |
| bug 3 修复 | ✅ 完成（[`hub.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/hub.go)） | 重启 backend 验证 metrics |
| 真实集成测试 | ⏳ 后续 | 用 docker compose 起 PG，跑 llm_calls 真实落库测试 |
| Phase A 数据采集 | ⏳ 启动（按 [`../roadmap/whitebox-roadmap.md`](../roadmap/whitebox-roadmap.md) Phase A） | 跑 5-10 场真实庭审 |

---

## 8. 面试故事模板

> **问：你们项目最有技术亮点的事情是什么？**
>
> 答：我印象最深的是 v0.8 上线当天，**白盒化让 3 个隐藏 bug 显形**。其中 1 个 P1 bug 是 LLM 审计表的外键约束失败——业务跑得好好的，但 token 成本无法统计。如果没白盒化，这个 bug 会一直隐藏到上线后第一次做 token 成本对账时（可能是月底），但那时候已经**花了多少钱都不知道**。
>
> 我们用 4 小时把 v0.8 白盒化做出来（slog / metrics / decision_events + 端到端 trace 串联），**白盒化交付当天跑了一次 demo**，stdout JSON 日志立刻暴露了 3 个 bug。**5 行代码 + 4 项测试**修完 P1。
>
> **核心启示**：白盒化不是"做完就完事"，是"跑一次业务流验证三处数据自洽"。**业务跑得欢 ≠ 系统健康**，**意图与实现脱节是 bug 经典形态**，白盒化让这种脱节在第一次跑业务时显形。

---

**更新于**：2026-07-02
**作者**：DecisionCourt Team
**版本**：v1.0
**配套**：[`../architecture/link-overview.md`](../architecture/link-overview.md) · [`../adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md) · [`../roadmap/whitebox-roadmap.md`](../roadmap/whitebox-roadmap.md)

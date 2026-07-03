# 05 · 白盒化（v0.8）—— 让 AI 系统"自己知道自己在干什么"

> **目标**：用第一人称 + 工程视角，讲清楚 v0.8 白盒化（observability）**为什么是面试杀手锏**、**业内怎么定位**、**DecisionCourt 做到什么程度**、**如何在面试中讲出来**。
> **配套**：[`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md) · [`../adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md) · [`../roadmap/whitebox-roadmap.md`](../roadmap/whitebox-roadmap.md)
> **更新于**：2026-07-02
> **版本**：v1.0

---

## 0. 一句话总结

> "AI 系统能不能被 debug / 被审计 / 被复盘"= 这个项目的工程成熟度。**v0.8 我做了完整可观测性（slog + metrics + decision_events + 端到端 trace），核心价值是 v0.8.3 一次真实庭审暴露 5 个隐藏 bug（包括 1 个"每一层都对但链路 ID 错配"的隐蔽 bug）** —— 业务跑得欢 ≠ 系统健康，**白盒化让"业务正确性"和"系统健康度"两件事都能查**。

---

## 1. 业内成熟度模型（5 级）

```
L0  黑盒:   printf 散落、SSH 翻日志、靠"猜"排查
L1  基础:   结构化日志 + /metrics 端点 + trace_id
L2  标准:   三大支柱完整串联 + 业务 event 独立落库    ← v0.8 完成
L3  高级:   OTLP + Jaeger + Prometheus + Grafana + SLO
L4  智能:   AIOps 异常检测 / eBPF 无侵入埋点 / 持续剖析
```

**我对 DecisionCourt 的定位**：**L2 完整 + L3 接口预留**。

### 1.1 L2 完整 = MVP 阶段够用

- 三大支柱（slog / metrics / decision_events）**全链路打通**
- 端到端 trace_id 串联（HTTP X-Request-ID → ctx.Trace → 业务事件 → 数据库）
- 业务级 event 单独存表（`decision_events` / `belief_diffs` / `a2a_messages`）

### 1.2 L3 预留 = 未来不返工

- `Metrics` 接口设计预留 Prometheus exporter（**不改业务代码**）
- `Span` 接口设计预留 OTel exporter
- 关键埋点（`transitionPhase`）已埋

### 1.3 为什么不做 L3

**业内教训**：很多项目 L0 → L3 一上来接 Prometheus + Jaeger + ELK，**花费数周**配置 K8s / exporters / dashboards，**业务上线前半年都在搭监控**。**对 MVP 阶段用户量百级是过度工程化**。

**我的策略**：**L2 让"业务跑得欢 + 系统被监控"**，L3 留给商业化阶段（v0.9+）。

---

## 2. 三大支柱逐个讲

### 2.1 Logging（slog JSON）

[`backend/internal/observability/logger.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/observability/logger.go)：

```go
// 全局 logger 配置：JSON handler + 自动注入 trace_id
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})))
```

**业务调用只需要写 message**：
```go
slog.Info("state transition", 
    "session_uuid", sessionUUID, 
    "from", from, "to", to, "round", round)
```

**trace_id 自动注入**：
```go
func WithTrace(ctx, "submit_evidence") (context, func()) {
    ctx, end := observability.WithSpan(ctx, "submit_evidence")
    defer end()
    
    // 后续 log 自动带 trace_id
    slog.InfoContext(ctx, "evidence added", "id", evidenceID)
    // → {"time":"...", "level":"INFO", "msg":"evidence added",
    //     "trace_id":"abc-123", "span_id":"def-456", "id":"..."}
}
```

**业内经验**：text log = 没 log（grep 不到 / 聚合不到）。**JSON log + trace_id 是下限**。

### 2.2 Metrics（11 类业务指标）

[`backend/internal/observability/metrics.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/observability/metrics.go)：

```go
type Metrics interface {
    IncCounter(name string, labels map[string]string)
    ObserveHistogram(name string, value float64, labels map[string]string)
    SetGauge(name string, value float64, labels map[string]string)
}
```

**v0.8 的 11 类指标**（精选业务级）：

| 指标 | 类型 | Labels |
|---|---|---|
| `courtroom_state_transition_total` | counter | from, to |
| `http_request_duration_seconds` | histogram | path, method, status |
| `a2a_message_throughput_total` | counter | event_type |
| `llm_call_total` | counter | agent_type, status |
| `belief_diff_total` | counter | agent_type, direction |
| `evidence_total` | counter | source, type |
| `memory_entry_total` | counter | agent_type, type |
| `investigation_finding_total` | counter | source_provider |
| `circuit_breaker_state_change_total` | counter | provider, from, to |
| `compression_token_saved_total` | counter | agent_type |
| `session_active_count` | gauge | (无) |

**3 个设计纪律**：
1. **label 用有限集合**（agent_type / status / phase），不用 session_uuid（cardinality 爆炸 → Prometheus 挂）
2. **业务指标，不只是技术指标**（不是 CPU / 内存 / GC，是"每个庭审花了多少 token"）
3. **`GET /metrics` 端点暴露**（v0.8 是 JSON 格式，未来切 Prometheus text）

**业内关键经验**：**cardinality 控制 = metric 设计的第一原则**。用 session_uuid 当 label = 性能灾难。

### 2.3 Tracing（自实现 Span + OTLP-ready）

[`backend/internal/observability/trace.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/observability/trace.go)：

```go
type Span interface {
    ID() string
    TraceID() string
    SetAttribute(key, value string)
    End()
}

type Tracer interface {
    StartSpan(ctx, name string) (context.Context, Span)
}

// 内存实现（v0.8），未来切 OTLP 不改业务
type memTracer struct { spans sync.Map }
```

**端到端 trace 串联**：

```
HTTP request (X-Request-ID)
    ↓
gin.TraceMiddleware → ctx.Trace
    ↓
business code → span "submit_evidence" → span "llm_call"
    ↓
broadcast WS event with trace_id
    ↓
frontend receives trace_id → 业务事件落 decision_events 都有 trace_id
    ↓
数据库可以用 trace_id 查出整条链路所有事件
```

**业务价值**：**庭审卡住 = 给 user 一个 trace_id，工程师用 `SELECT * FROM decision_events WHERE trace_id = 'abc-123'` 看到所有事件**。

### 2.4 业务 Events（decision_events 表）

`decision_events` 表 —— v0.8 新增的核心表。

```sql
CREATE TABLE decision_events (
    id uuid PRIMARY KEY,
    session_uuid uuid,
    trace_id varchar(64),
    span_id varchar(64),
    event_type varchar(50),  -- state_transition / span.X / business.X
    status varchar(20),      -- ok / error
    payload jsonb,           -- 业务级 span attributes
    created_at timestamptz
);
```

**写入位置**：
- `transitionPhase()` 状态机迁移时 → `event_type='state_transition'`
- 未来业务级 span → `event_type='span.X'`

**关键设计**：**业务事件独立表 vs 普通日志**：

| 维度 | 普通 Log | 业务 Event |
|---|---|---|
| 目的 | 排错 | 审计 / 庭审回放 / BI |
| Schema | 不固定 | 固定（每类 event 有 schema） |
| 存储 | 文本 + 全文检索 | 结构化表 + 索引 |
| 保留期 | 几天～几周 | 几个月～永久 |
| 谁读 | 开发者 / SRE | 业务方 / 法官 / 用户 |

---

## 3. 4 个 sink 的设计哲学（面试亮点）

白盒化的数据落到 **4 个不同位置**：

| 维度 | 数据 | 位置 | 持久化 |
|---|---|---|---|
| 实时排错 | slog JSON | **stdout** | ❌（接 ELK/Loki 才持久化） |
| 实时指标 | 11 类业务指标 | **内存**（`GET /metrics` 返回） | ❌（接 Prometheus 才持久化） |
| 业务审计 | decision_events | **PostgreSQL** | ✅ 持久化 |
| LLM 审计 | llm_calls | **PostgreSQL** + **文件 JSON Lines** | ✅ 双写 |
| 消息 | a2a_messages | **PostgreSQL** | ✅ 持久化 |

**设计原则**：
1. **结构化数据（业务事件）→ 永远进数据库**（可 SQL 查询、可索引）
2. **实时排错 → 走 stdout**（不持久化避免磁盘爆炸）
3. **实时指标 → 走内存**（不持久化避免数据库爆写）
4. **备份（LLM 调用副本）→ 走文件**（数据库出问题时还能查）

**未来 L3 接入**：stdout 接 ELK，内存接 Prometheus，文件继续保留。**接口已预留，不改业务代码**。

---

## 4. v0.8.3 bug 故事（面试杀手锏，必背）

详见 [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)，这里只记核心。

### 4.1 我印象最深的 bug（bug 4：信念轨迹只显示 1 条）

**用户跑了一次真实庭审**，反馈"4 个证据只有 1 个放入信念轨迹"。

**白盒化让我系统化审计**：

| 数据源 | 状态 | 数值 |
|---|---|---|
| 数据库 `belief_diffs` 表 | ✅ 16 行 | 4 evidence × 4 agent 全部写入 |
| API `GET /belief-diffs` | ✅ 返回 16 条 | count=16, distinct id=16 |
| 后端 stdout `belief.diff` WS 事件 | ⚠️ 16 个事件但 **ID 全部为 uuid.Nil** | 内存零值 |
| 前端 `store.appendBeliefDiff` 幂等检查 | ❌ 第 2-16 条被去重 | 用户看到 1 条 |

**根因**：`backend/internal/belief/engine_v06.go:97` 创建 `model.BeliefDiff{}` **没有设置 ID 字段**。依赖 `repo.Insert` 内 fallback (`if diff.ID == uuid.Nil { diff.ID = uuid.New() }`) 在写入 DB 时补 ID —— 但**内存里的 diff 仍然是零值**。service.broadcastEvent 直接用内存对象 → WS 推送的 16 个事件 ID 全相同 → 前端 store idempotency dedupe。

**可怕之处**：**每一层（数据库 / API / 后端 / 前端）单独看都对**，但**数据在某个环节丢失**。

**修复**：1 行 `ID: uuid.New()` + 4 行注释。

**核心启示**：**数据层正确 ≠ 链路正确**，必须**端到端真实跑业务 + 每层数据自洽**才能发现这类 bug。

### 4.2 其他 4 个 bug（30 秒）

v0.8 + v0.8.3 一共暴露 **5 个 bug**：

| Bug | 严重度 | 暴露路径 |
|---|---|---|
| `llm_calls` 外键约束失败 | 🔴 P1 | v0.8 demo 当天 stdout ERROR |
| SessionID fallback WARN | 🟡 P2 | v0.8 demo 当天 stdout WARN |
| `a2a_message_throughput_total` 计数缺失 | 🟢 P3 | v0.8 demo 查 /metrics 端点 |
| **信念轨迹只显示 1 条** | 🟡 P2 | **v0.8.3 真实庭审回归** |
| 判决书按钮无响应 | 🟢 P3 UX | v0.8.3 真实庭审回归 |

**5 个 bug 全部由白盒化（demo 跑业务 / 真实庭审）暴露**，**没有任何一个被单元测试发现**。

---

## 5. 关键代码位置（面试可指）

| 模块 | 文件 | 行数 |
|---|---|---|
| 三大支柱概览 | `backend/internal/observability/` | ~700 |
| logger.go | JSON handler + trace 注入 | ~80 |
| metrics.go | Metrics interface + 内存实现 | ~150 |
| trace.go | Span / Tracer interface | ~120 |
| middleware.go | gin 4 个 middleware | ~180 |
| event_recorder.go | decision_events 表写入 | ~90 |
| model/decision_event.go | DB model | ~50 |
| business_spans.go | 业务级 span 名常量 | ~40 |
| main.go 装配 | | ~20 行 |
| Case-study 配套文档 | `docs/observability/case-study-2026-07-02.md` | 350 行 |

---

## 6. 防质疑思考（面试常问）

### Q1: "为什么不直接接 Prometheus / Jaeger？"

> **业内 trade-off**：
> - 接 Prometheus：需要在 K8s / exporters / dashboards 上花数周
> - 接 Jaeger：需要额外服务（trace 后端）+ agent + collector
> - MVP 阶段用户量小，**L3 性价比低**
>
> **我的策略**：
> 1. **L2 完整**（slog + metrics + decision_events + 端到端 trace）够 debug / 够审计
> 2. **接口预留**（`Metrics` / `Span` 是 interface，未来加 exporter 不改业务）
> 3. **v0.9+ 接 Prometheus 不返工**（已经验证业务层数据正确）
>
> 这是**"先做对，再做全"** 的工程哲学 —— **不预先做大平台的工程师**。

### Q2: "白盒化值不值得？4 小时这么多工作量？"

> **ROI 数字**：
> - 投入：4 小时 / 1 PR（包括 ADR + 文档 + 50+ 测试）
> - 产出：1 次 demo 暴露 3 个隐藏 bug + 1 次真实庭审暴露 2 个隐藏 bug
> - 节省：~2 周的"上线后发现 → 排查 → 修复 → 复测"循环
>
> **业内经验**：**没有 observability 的 AI 系统 = 黑盒**。**调试 LLM 行为平均 30 分钟/问题**。**有 observability = 5 分钟/问题**（按 trace_id 查 event）。**回报 = 数量级的差距**。

### Q3: "业务事件跟日志有什么区别？"

> 详见 §2.4 表格。**核心区别**：业务事件**目的明确（审计 / 回放 / BI）+ schema 固定 + 可长期持久化**；日志**目的模糊（debug 兼审计）+ schema 不固定 + 短期保留**。
>
> **业务事件 ≠ 日志**。**DecisionCourt 同时用日志和业务事件** —— **不同目的不同取舍**。**白盒化不是单选，是分场景选 sink**。

### Q4: "trace_id 在跨服务调用时怎么传？"

> v0.8 我的项目是**单 backend 进程**，HTTP / WebSocket / RPC 都在同一进程 → `ctx.Trace` 直接传。
>
> 未来**跨服务调用**（比如 LLM gateway 独立成微服务）需要**OTel W3C Trace Context** 标准 —— 它定义 `traceparent` header 在 HTTP / gRPC / Kafka 等协议间传递 trace_id。
>
> 这是 v0.9+ L3 阶段的事。我的 `Span` 接口已经预留**OTel 兼容的属性命名**（`trace_id` / `span_id` / `parent_span_id`），未来切 OTel SDK 不用改业务。

---

## 7. 面试话术（这是 5 个亮点章节里**最让我自信的**）

### 30 秒版（推荐讲）

> "我印象最深的是 [项目名] 的 v0.8 白盒化系统。**核心论点：AI 系统能不能被 debug = 它的工程成熟度**。
>
> v0.8 我做了完整可观测性：**slog JSON + 11 类业务指标 metrics + 端到端 trace 串联 + decision_events 业务事件表**。**核心价值**：跑一次真实庭审，stdout + metrics + DB 三处数据**自洽**。
>
> **最有说服力的故事**：v0.8.3 我跑了一次真实庭审，**4 个证据触发了 16 条 belief_diff**，**每一层（数据库 / API / 后端 / 前端）单独看都对**，但用户看到只 1 条。**根因是 `belief_diffs` 表的 ID 在创建时是 uuid.Nil 零值，依赖 `repo.Insert` 内部 fallback 才补 ID，但 broadcast 时用的是内存零值 ID，前端 store 的幂等去重把后 15 条全 skip 了**。**修了一行 `uuid.New()`**。
>
> **核心洞察**：**业务跑得欢 ≠ 系统健康**。**白盒化让两件事都能查**。

### 3 分钟版

> 我们调研了 AI 系统的可观测性业内做法。**业内分 5 级**：L0 黑盒（printf）→ L1 基础（结构化日志）→ L2 标准（三大支柱 + 业务 event）→ L3 高级（Prometheus + Grafana + Jaeger + SLO）→ L4 智能（AIOps）。
>
> 我做 [项目名] 时，**MVP 阶段选 L2 完整 + L3 接口预留**，拒绝 L3 一上来接 Prometheus。**理由**：百级用户，搭 Prometheus + Grafana 数周投入，性价比太低。
>
> **v0.8 我具体做了什么**：
> 1. **Logging**：Go 1.21+ `log/slog` JSON handler，自动注入 trace_id 到每条 log
> 2. **Metrics**：11 类业务指标（不是 CPU/内存，是"每个庭审花了多少 token"、"消息有多少"），`GET /metrics` 端点暴露 JSON，**label 用有限集合**（cardinality 控制）
> 3. **Tracing**：自实现 `Span` / `Tracer` 接口，内存实现，**OTel 兼容属性命名**为未来预留
> 4. **Business events**：`decision_events` 表，**业务级事件独立落库**（区别于普通日志：审计 / 庭审回放 / BI）
> 5. **4 个 sink 设计**：实时 log → stdout；实时 metric → 内存；业务 event → 数据库；LLM 审计 → 数据库 + 文件
>
> **最有说服力的真实案例**：v0.8 + v0.8.3 一共暴露 **5 个隐藏 bug**，没有一个被单元测试发现。**最深刻的一个**：信念轨迹只显示 1 条（应该是 16 条）。**可怕之处**：数据库正确写了 16 行；API 返回了 16 条不同 ID；但后端 WebSocket 广播时，16 个 belief.diff 事件 ID 全是零值（uuid.Nil），前端 store 的幂等逻辑把后 15 条去重掉了。**每层单独看都对，链路错配**。
>
> **核心洞察**：
> 1. **数据层正确 ≠ 链路正确**（必须端到端真实跑 + 每层数据自洽）
> 2. **业务跑得欢 ≠ 系统健康**（没白盒化可能 token 失控）  
> 3. **白盒化 ROI 不是"做完就完"，是"持续回归 → 持续暴露 → 持续修复"**
> 4. **接口预留 = 未来不返工**（v0.9 接 Prometheus 不用改业务）

### 10 分钟深度版（深入面追问用）

> 详细讲 DecisionCourt v0.8 白盒化的实现 + trade-off + 真实案例：
> - **三大支柱的具体实装**（200+ 行代码细节）
> - **业务事件 vs 日志的取舍**（schema / 保留期 / 谁读）
> - **cardinality 控制**（label 设计的 3 条戒律）
> - **接口预留策略**（`Metrics` / `Span` interface 怎么设计方便未来切 OTel）
> - **5 个 bug 的复盘**（每一个怎么发生 / 怎么暴露 / 怎么修复 / 教训）
> - **v0.9+ L3 接入的 Roadmap**（Phase A → Phase B → Phase C）

---

## 8. 【反思】

### 反思 1：**白盒化不是"做完就完"，是"持续投入"**

业内很多人以为"做了 = 完成"。**错**。白盒化是**持续运营成本**——每加一个业务功能就要补埋点、每发现一个新 bug 就要加 metric、每次升级都要验证 trace 串联。

**我的 5 级成熟度模型 = 持续投入的方向感**。**L2 是"持续回归 + 持续埋点"的循环**，**L3 是"接入 + dashboard + SLO"的运营**，**L4 是"AIOps + 告警降噪"的智能化**。

### 反思 2：**白盒化是 AI 系统的"工程化起点"**

AI 系统最大的风险是"**决策过程不可解释**"。**白盒化 = 让 AI 系统能解释自己**。

我做的 decision_events 表 + belief_diffs 表 + a2a_messages 表 = **3 个审计 trail**——这一场庭审的所有决策、所有信念变化、所有 Agent 通信**完整入库**，**未来任何"为什么"都能查**。

**这是 AI 系统区别于传统软件系统的核心**——传统软件 debug 看 stack trace，**AI 系统 debug 看 "这段 prompt 让 LLM 输出 X 的逻辑"**。**白盒化就是 AI 系统的 stack trace**。

### 反思 3：**真实庭审回归 > 单元测试 + e2e 测试**

v0.8.3 bug 4 这种"端到端 ID 错配"bug，**单元测试完全测不出来**（每个组件单独测都对），**e2e 测试也大概率测不出来**（mock LLM 时 belief engine 行为不对）。**只有真实 LLM + 真实庭审 + 用户真实使用 + 白盒化数据自洽**才能发现。

**这种 bug 业内叫 "**sneaky production bug**"——上线前发现不了，上线后业务跑得欢，但 1% 路径出错**。**白盒化回归 = 提前抓到 1% 路径 bug 的唯一办法**。

### 反思 4：**卡片化业务事件 = AIOps 的前置**

decision_events 表里 16 条 belief_diff 都是结构化的（`event_type` / `status` / `payload` / `trace_id`）。未来接 AIOps：

| 分析 | 用 |
|---|---|
| 异常检测 | "这一场的 belief_diff variance 比平均高 3 倍 → 异常庭审" |
| 故障归因 | "所有 LLM 调用失败的 session 都有相同的 trace_id" |
| 用户洞察 | "80% 用户在 cross_exam round 2 后表达观点收敛" |

**结构化业务事件 = AIOps 的训练数据**。**不是事后整理，是事前结构化**。

---

## 9. 名词速查

| 名词 | 含义 |
|---|---|
| Observability | 可观测性（Logs + Metrics + Traces） |
| Whitebox | 白盒化（同 observability，工程化叫法） |
| Cardinality | label 组合数（控制 metric 内存） |
| OpenTelemetry | CNCF 标准的 observability 协议 |
| OTLP | OpenTelemetry Protocol |
| CNCF | Cloud Native Computing Foundation |
| SLO | Service Level Objective（4 个 9 = 99.99% 等） |
| JWT | JSON Web Token（不在本文范围，但工程化常用） |
| L4 maturity | AIOps level（持续剖析 / 异常检测） |

---

**下一步**：
- [`06-bug-stories.md`](06-bug-stories.md) —— 上面提到的好几个 bug 真实故事
- [`07-key-terms.md`](07-key-terms.md) —— 技术名词解释
- [`08-faq-30-questions.md`](08-faq-30-questions.md) —— 30 个面试高频问题


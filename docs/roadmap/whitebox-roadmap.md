# v0.8+ 白盒化完善计划（Whitebox Observability Roadmap）

> **版本**：v0.1（2026-07-02 制定）
> **目标**：基于"使用数据驱动"思路，分阶段完善 v0.8 白盒化基础设施
> **当前状态**：v0.8 基础设施层完成（slog + 11 类业务指标 + Span 接口 + decision_events 表 + `GET /metrics` 端点 + 端到端 trace_id 串联）
> **关联 ADR**：[`../adr/0010-whitebox-observability.md`](../adr/0010-whitebox-observability.md)
> **关联主 roadmap**：[`../decisioncourt-roadmap.md`](../decisioncourt-roadmap.md) §6 持续可观测性

---

## 0. 设计原则（最重要的部分，先看）

**1. 数据驱动，不凭直觉**
> v0.8 故意**不全埋**业务级 span。半年后看 `decision_events` 表的 `event_type` 分布，
> 哪些 `event_type` 出现频次高、哪类问题最常被查询，**再增量埋**。
>
> 业内经验：事先全埋 → 30-50% 是死代码；数据驱动 → 埋得准 + 维护成本低。

**2. 不返工原则**
> v0.8 的 [`Metrics`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/observability/metrics.go) 和 [`Span`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/observability/trace.go) 都是**接口**。
> Phase C / D 加 Prometheus / OTLP exporter **不改业务代码**，只加 adapter。

**3. 优先级 = 排查频率 × 业务关键性**
> - **状态机迁移**（`transitionPhase`）：已埋 ✅
> - **跨轮 ReAct 循环**（`RunCrossExamRound`）：下一步最高优先级（庭审最常卡的位置）
> - **单次 LLM 调用**：LLM Gateway 层有 `llm_calls` 表支撑，不必再埋业务 span
> - **信念变化**：`belief_diffs` 表支撑（v0.6 已实装），不重复

---

## Phase A — 数据采集期（v0.8.1，预计 1-2 周）

### A.1 跑真实庭审 5-10 场
- 让真实用户（或内部测试）跑 5-10 场完整庭审
- 故意制造异常：网络断流 / LLM timeout / 状态机卡某轮 / 用户反复点击

### A.2 跑 SQL 统计
```sql
-- 哪些 event_type 出现频次最高？
SELECT event_type, COUNT(*)
FROM decision_events
WHERE created_at > NOW() - INTERVAL '2 weeks'
GROUP BY event_type
ORDER BY 2 DESC
LIMIT 20;

-- 哪些 session 的 decision_event 数异常多（= 卡住）？
SELECT session_uuid, COUNT(*) AS ev_count
FROM decision_events
WHERE created_at > NOW() - INTERVAL '2 weeks'
GROUP BY session_uuid
ORDER BY 2 DESC
LIMIT 10;

-- 错误事件分布
SELECT event_type, COUNT(*)
FROM decision_events
WHERE status = 'error'
  AND created_at > NOW() - INTERVAL '2 weeks'
GROUP BY event_type
ORDER BY 2 DESC;
```

### A.3 触发下一阶段的决策
| 统计结果 | 触发动作 |
|---|---|
| `state_transition` 占 80% 以上 → 状态机查询高频 | 优先埋 Phase B.1 跨轮 span |
| `span.X` 类（已有但少）查询频繁 | 补 Phase B.2 `RunCrossExamRound` |
| 大量 `status='error'` 集中某类 | 加告警规则（Phase C.2） |
| HTTP 延迟 P99 > 5s 集中某路径 | 加 Phase B.4 HTTP 详细 span |

### A.4 交付物
- 一份**统计报告**（在 ADR 0010 末尾追加 §A "Phase A 统计"）
- **增量埋点清单**（Phase B 具体埋哪些 span 的输入）

---

## Phase B — 业务级 span 增量埋点（v0.8.x，预计 1 个月）

> **不一次性全埋**。Phase A 跑出数据后再**按频次排**。

### B.1 优先级 P0（必埋）

| Span 名 | 位置 | 触发 Phase A 的依据 |
|---|---|---|
| `RunCrossExamRound` | `courtroom/service.go` `runCrossExamRound` | 庭审最常卡的循环 |
| `DispatchInvestigator` | `agent/orchestrator.go` 调查员 dispatch | 外部 API 慢点 |
| `GenerateVerdict` | `courtroom/service.go` 法官判决 | 业务最关键路径 |

### B.2 优先级 P1（看数据决定）

| Span 名 | 位置 | 触发条件 |
|---|---|---|
| `BeliefUpdate` | `belief/engine_v06.go` 引擎入口 | `belief_diff` 查询频次 > 100/天 |
| `ConvergenceCheck` | `belief/convergence.go` 收敛判断 | 状态机卡 `cross_exam` 频次 > 5 |
| `ReAct.Speak` | `agent/react_runner.go` speak 步骤 | Agent 行为异常反馈 > 3 次 |

### B.3 优先级 P2（视情况）

| Span 名 | 位置 |
|---|---|
| `CompressionTrigger` | `agent_gateway/prompt_compress.go` |
| `BudgetExhausted` | `agent_gateway/token_budget.go` |
| `HubBroadcast` | `api/hub.go`（用于 A2A 投递耗时分析） |

### B.4 HTTP 详细 span（可选）

为关键路径加细粒度 span：
- `POST /api/v1/courtrooms` → `StartTrial` span
- `POST /api/v1/courtrooms/:uuid/evidences` → `SubmitEvidence` span
- `WS /ws/courtrooms/:uuid` → 整个连接生命周期 span

### B.5 实施模板（每次新增 span 必做）

```go
// 1. 在 business_spans.go 加常量
const SpanName_RunCrossExamRound = "RunCrossExamRound"

// 2. 在 service.go 用 defer 模式埋点
func (s *Service) runCrossExamRound(ctx context.Context, ...) error {
    span, ctx := observability.TracerFromContext(ctx, s.recorder, s.metrics,
        observability.SpanName_RunCrossExamRound)
    defer span.End()

    span.SetAttr("session_uuid", sessionUUID)
    span.SetAttr("round", round)
    // ... 业务逻辑
    return nil
}

// 3. 加测试
func TestRunCrossExamRound_SpanRecorded(t *testing.T) {
    // 验证 span 关闭后写入 decision_events
}
```

### B.6 交付物
- B.1 必埋的 3 个 span 全部上线
- B.2 / B.3 按 Phase A 数据决定
- decision_events 表 `event_type` 索引更新
- 单元测试覆盖

---

## Phase C — Metrics 升级到 Prometheus（v0.9.0，预计 3 个月）

> **触发条件**：用户量 > 100 / 单日 LLM 调用 > 1000 次

### C.1 Prometheus 文本格式 exporter

新增 `internal/observability/prometheus_exporter.go`：
```go
// 实现 encoding/text expvar-like 接口
func (m *MemMetrics) WritePrometheusFormat(w io.Writer) error {
    // 遍历所有 counter / gauge / histogram
    // 输出 text/plain; version=0.0.4
}
```

修改 `handler.go`：
```go
if c.Query("format") == "prometheus" {
    c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
    metrics.WritePrometheusFormat(c.Writer)
    return
}
```

### C.2 Grafana 仪表盘 JSON

放 `docs/observability/grafana-dashboard.json`：
- **面板 1：庭审状态机流量**（state transition 桑基图）
- **面板 2：LLM 调用 P50/P95/P99 延迟**（按 agent_type / model）
- **面板 3：活跃 session 数**（按 state）
- **面板 4：Token 消耗速率**（counter rate）
- **面板 5：业务事件分布**（decision_events event_type 饼图）
- **面板 6：HTTP 错误率**（按 path / status）

### C.3 告警规则

放 `docs/observability/prometheus-alerts.yml`：
- LLM P99 > 10s 持续 5 分钟
- 状态机卡在 `cross_exam` 超过 30 分钟
- `decision_event` 写入失败率 > 5%
- HTTP 5xx 比率 > 1%
- 活跃 session 数 > 100（容量预警）

### C.4 交付物
- Prometheus exporter + 1 项测试
- Grafana 仪表盘 JSON
- 告警规则 yml
- 一篇 "如何接入" 文档（`docs/observability/QUICKSTART.md`）

---

## Phase D — Tracing 升级到 OTLP（v1.0.0，预计 6 个月）

> **触发条件**：多实例 backend 部署 / 跨服务调用（LLM gateway 独立 / WebSocket 跨节点）

### D.1 OTLP exporter 接入

```go
// internal/observability/otel_exporter.go
import "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"

func NewOTelTracer(cfg OTelConfig) (Tracer, error) {
    exporter, _ := otlptracehttp.New(ctx,
        otlptracehttp.WithEndpoint(cfg.Endpoint),
        otlptracehttp.WithInsecure(),
    )
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(resource.NewWithAttributes(...)),
    )
    return &otelTracer{tp: tp}, nil
}
```

### D.2 改造 Span 实现

把现在的 `spanImpl` 替换为 `otelTracer.Span`：
- 业务代码**不变**（接口兼容）
- 关闭 span 时**双写**：`decision_events` 表（保留）+ OTLP 导出
- 关闭旧的 `MemTracer`（保留为测试用）

### D.3 Jaeger / Tempo 部署

- 选型 Jaeger（成熟，社区大）或 Tempo（成本低，云原生）
- 部署到 K8s（如果届时已上 K8s）
- 验证：一次庭审能在 Jaeger 看到完整 span 树

### D.4 交付物
- OTel SDK 接入
- `decision_events` 双写（兼容历史）
- Jaeger / Tempo 部署文档

---

## Phase E — 业务 Events 进数据仓库（v1.x，看商业化阶段）

> **触发条件**：商业化启动 / 需要 BI 分析 / 法官审计需求

### E.1 ClickHouse / 阿里云 AnalyticDB

- `decision_events` 实时同步到 ClickHouse
- `llm_calls` / `belief_diffs` 同步
- 保留期：1 年（PostgreSQL） + 5 年（ClickHouse）

### E.2 庭审回放系统

- 基于 `decision_events` 表 + `a2a_messages` + `llm_calls` + `belief_diffs`
- 输入 `session_uuid` → 输出**可交互式时间线**
- 关键节点：状态机迁移 / Agent 发言 / 调查员发现 / 法官判决

### E.3 BI 仪表盘

- 日均庭审数 / 平均轮次 / 收案胜率 / 法官平均打分
- Agent 表现分析（哪个 agent 信念最稳定 / 哪个 agent 最常被攻破）

### E.4 交付物
- 数据仓库 ETL 脚本
- 庭审回放前端页面
- BI 仪表盘

---

## 阶段触发决策表（运营检查清单）

每 1 个月跑一次：

| 检查项 | 触发条件 | 进入下一阶段 |
|---|---|---|
| 真实庭审数 | < 5 场 | 继续跑（Phase A 延长） |
| 真实庭审数 | 5-10 场 | 进入 Phase B 决策 |
| 真实庭审数 | > 10 场 | 加速 Phase B |
| 日均 LLM 调用 | < 100 | 继续 Phase B |
| 日均 LLM 调用 | 100-1000 | 进入 Phase C |
| 日均 LLM 调用 | > 1000 | 加速 Phase C |
| 后端实例数 | 1 个 | 继续 Phase C |
| 后端实例数 | ≥ 2 个 | 进入 Phase D |
| 月活用户 | < 100 | 暂缓 Phase E |
| 月活用户 | 100-1000 | 评估 Phase E |
| 月活用户 | > 1000 | 启动 Phase E |

---

## 不做的事（避免过度工程化）

| 项 | 不做的理由 |
|---|---|
| 启动即全埋所有 span | 30-50% 是死代码，浪费 |
| 启动即接 Prometheus | 用户量小，JSON `/metrics` 够用 |
| 启动即接 OTLP | 单实例 + 内存实现已够 |
| 启动即建数据仓库 | 商业化前没需求 |
| eBPF / 持续剖析 | 阶段 4，>10k 用户才考虑 |
| 告警降噪 / AIOps | 阶段 4，告警规则稳定后才考虑 |

---

## 版本与里程碑

| 版本 | 阶段 | 工作量估算 | 触发条件 |
|---|---|---|---|
| **v0.8** | 当前 | ✅ 已完成 | — |
| v0.8.1 | Phase A | 1-2 周（数据采集） | 跑 5-10 场 |
| v0.8.x | Phase B | 2-3 周（增量埋点） | Phase A 数据 |
| v0.9.0 | Phase C | 2-3 周（Prometheus） | 日均 LLM > 100 |
| v1.0.0 | Phase D | 3-4 周（OTLP） | 多实例部署 |
| v1.x | Phase E | 4-6 周（数据仓库） | 商业化启动 |

---

## 维护守则

1. **每次新增业务级 span**：
   - 在 `business_spans.go` 加常量
   - 在 service 入口用 `defer span.End()` 模式
   - 至少 1 项单元测试验证 `decision_events` 落库
   - 同步更新本文档"埋点清单"
2. **每次新增 metric**：
   - 严格遵守"label 是有限集合"原则（防 cardinality 爆炸）
   - 至少 1 项测试
3. **每次新增 dashboard 面板**：
   - JSON 入库 `docs/observability/`
   - 在本文档"交付物"段链接

---

**最后更新**：2026-07-02
**下次评审**：2026-08-02（Phase A 数据采集完成时）

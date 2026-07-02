# ADR 0010: 后端白盒化（Observability 三支柱落地）

> **状态**：✅ Accepted (2026-07-02)
> **决策日期**：2026-07-02
> **影响范围**：全后端 + `internal/observability/` 新模块 + 5 份主文档

## 背景

MVP 阶段后端"白盒子集"已落地（[`docs/decisioncourt-tech-spec.md` §6.4](../decisioncourt-tech-spec.md)），但**白盒化**（Observability）严重不足：

| 痛点 | 现状 |
|---|---|
| 查问题靠翻日志拼接 | 多个 `log.Printf` 散落在 `hub.go` / `recorder.go` / `state.go`，无统一字段 |
| "这次庭审调了几次 LLM？" | `llm_calls` 表只能按 session_uuid 聚合，无 round / phase / agent_type 分维度 |
| "庭审卡在某轮不动了" | 无活跃轮次 watchdog，无法判断是 LLM hang / 状态机 bug / 用户发呆 |
| "为什么这次相信 A 而不是 B" | `belief_diffs` 有 trail，但和 LLM 调用没有 trace 关联 |
| "GPU API 突然变慢了" | 无 P50/P99 延迟指标，只能翻 latency_ms 字段手算 |

**已有能力**：
- `internal/agent_gateway/trace.go` —— ctx 传递 `Trace{RequestID, SessionUUID, AgentType, TaskType}`
- `internal/agent_gateway/recorder.go` —— 每次 LLM 调用写 `llm_calls` 表
- `internal/agent_gateway/file_logger.go` —— 每次 LLM 调用写结构化 JSON

**缺失**：
- HTTP 入口 / WebSocket 入口 / 状态机迁移 / A2A 消息 —— 全部不参与 trace 串联
- 无 Prometheus exporter
- 无结构化日志（slog）
- 无业务事件审计（仅 `belief_diffs` 和 `llm_calls` 两类）

## 选项对比

| 维度 | 选项 A | 选项 B | 选项 C |
|---|---|---|---|
| Trace 工具 | 自建 trace_id + ctx | **OpenTelemetry SDK + OTLP** ✅ | 接入 Jaeger SDK（vendor lock-in） |
| Metrics 工具 | 仅系统指标 | **Prometheus + 业务指标** ✅ | DataDog（vendor lock-in） |
| Logging 方式 | 保留 log.Printf | **slog 化** ✅ | zap（多依赖） |
| Trace 串联范围 | 仅 HTTP → LLM | + WebSocket → A2A | **+ 业务事件（状态机 / 信念）** ✅ |
| 实施方式 | 单 PR 全做 | **单 PR** ✅ | 分 3 PR（log / metric / trace） |

## 决策

采用 **方案 B + slog + Prometheus + OTel + D3 串联 + 单 PR**。

### 关键决策点

1. **OpenTelemetry SDK + OTLP exporter** —— 业界标准，未来可对接 Jaeger / Tempo / 阿里 ARMS
2. **Prometheus 业务指标** —— 不只 `go_*`，必须包含业务核心（活跃庭审数 / LLM 调用次数 / 延迟直方图 / budget 触达率 / 信念变化频次 / A2A 吞吐）
3. **slog（Go 1.21+ 标准库）** —— 不引入 zap/logrus
4. **D3 串联范围** —— HTTP / WebSocket / A2A Bus / 状态机迁移 / 信念变化 全部带同一 trace_id
5. **新增 `decision_events` 表** —— 业务事件审计（状态机迁移 / 调查员 dispatch 完成 / 信念收敛触发等）
6. **不破坏现有 `agent_gateway.Trace`** —— 保留向后兼容，OTel Tracer 在 Trace 之上叠加

### 关键理由

- **白盒化是后续高可用 / 并发防护的度量基础** —— 没有 trace，分布式锁调试无从下手
- **OTel 是业界事实标准** —— 避免 vendor lock-in，未来换 backend 零代码改动
- **Prometheus 是云原生标准** —— 与 K8s 部署天然契合
- **单 PR** —— 反复改 ctx 路径风险大，一次到位
- **decision_events 单独建表** —— 不复用 `llm_calls`（语义不同：业务事件 vs LLM 调用事件）

## 后果

### 收益

- ✅ 一次请求的完整 trace 可在 Jaeger 中可视化（HTTP → handler → Orchestrator → ReAct → LLM Gateway）
- ✅ Prometheus 暴露 11 类业务指标 + 4 类系统指标
- ✅ 所有 log 带 session_uuid / trace_id 字段，可按 session 过滤
- ✅ `decision_events` 表让业务事件可审计 / 可回放
- ✅ HTTP/WS/状态机/A2A/信念 全链路带同一 trace_id

### 代价

- ⚠️ 新增 `go.opentelemetry.io/otel` + `prometheus/client_golang` 依赖（约 30MB 编译产物）
- ⚠️ ctx 路径上多 2-3 个值（trace / baggage），单次 LLM 调用多 < 1ms 开销
- ⚠️ `decision_events` 表每次状态机迁移写一行（约 5-10 行 / 庭审）
- ⚠️ 需要补充 trace_id 串联的集成测试（≥ 5 项）

## 实施清单

### 1. 新增模块 `internal/observability/`

```
internal/observability/
├── logger.go              # slog 包装，支持 trace_id / session_uuid 字段注入
├── metrics.go             # Prometheus 注册 + 11 类业务指标定义
├── metrics_middleware.go  # Gin middleware (http_request_duration_seconds)
├── trace.go               # OTel setup + TracerProvider + OTLP exporter
├── trace_middleware.go    # HTTP middleware (X-Request-ID → ctx)
├── trace_ctx.go           # ctx propagation helpers
├── business_spans.go      # 业务级 span helpers (RunCrossExamRound / A2A / ReAct / StateTransition)
└── *_test.go
```

### 2. 代码修改

- `cmd/server/main.go` —— 装配 observability（TracerProvider + Prometheus Handler + slog）
- `internal/api/handler.go` —— trace_middleware + metrics_middleware
- `internal/api/websocket.go` —— WS 消息带 trace_id
- `internal/api/hub.go` —— `log.Printf` → `slog.Info`
- `internal/courtroom/service.go` —— 状态机迁移打 span + 写 decision_events
- `internal/courtroom/statemachine.go` —— 迁移打 `business_spans.StateTransition`
- `internal/agent/orchestrator.go` —— 顶层 span `RunCrossExamRound`
- `internal/agent_gateway/recorder.go` —— `log.Printf` → `slog`

### 3. 新增表

- `decision_events` —— 业务事件审计表
  - `id` (uuid, pk)
  - `session_uuid` (uuid, indexed)
  - `request_id` (string, indexed)
  - `event_type` (string: state_transition / belief_diff / convergence_trigger / investigator_dispatched / ...)
  - `agent_type` (string, nullable)
  - `payload` (jsonb)
  - `created_at` (timestamp)

### 4. 新增 Prometheus 指标

| 指标名 | 类型 | 标签 |
|---|---|---|
| `courtroom_active_sessions` | gauge | `state` |
| `courtroom_state_transition_total` | counter | `from`, `to` |
| `llm_call_total` | counter | `agent_type`, `model`, `status` |
| `llm_call_duration_seconds` | histogram | `agent_type`, `model` |
| `llm_call_tokens_total` | counter | `agent_type`, `direction` |
| `budget_ratio` | gauge | `session_uuid` |
| `budget_rejected_total` | counter | `agent_type` |
| `a2a_message_throughput_total` | counter | `from`, `to`, `message_type`, `visibility` |
| `belief_diff_total` | counter | `agent_type`, `sign` |
| `compression_applied_total` | counter | `strategy` |
| `http_request_duration_seconds` | histogram | `path`, `method`, `status` |

### 5. 文档更新

- [`docs/decisioncourt-tech-spec.md`](../decisioncourt-tech-spec.md) —— 新增 §7 可观测性章节
- [`docs/decisioncourt-api-design.md`](../decisioncourt-api-design.md) —— 新增 §X `GET /metrics` 端点
- [`docs/decisioncourt-db-design.md`](../decisioncourt-db-design.md) —— 新增 `decision_events` 表 schema
- [`docs/decisioncourt-prd.md`](../decisioncourt-prd.md) —— §15.2 状态追加 v0.8 白盒化
- [`docs/decisioncourt-roadmap.md`](../decisioncourt-roadmap.md) —— 状态矩阵更新
- [`docs/README.md`](../README.md) —— 实装状态矩阵更新

### 6. 测试

- `internal/observability/logger_test.go` —— 5 项
- `internal/observability/metrics_test.go` —— 5 项
- `internal/observability/trace_test.go` —— 5 项
- `internal/observability/middleware_test.go` —— 5 项
- `internal/courtroom/service_trace_test.go` —— 3 项（trace 串联集成）
- `internal/api/handler_trace_test.go` —— 3 项（X-Request-ID 串联）

## 关联

- 主文档：[`../decisioncourt-tech-spec.md` §6.4 + 新增 §7](../decisioncourt-tech-spec.md)
- 后续 ADR：[`0011-distlock-redis.md`](./0011-distlock-redis.md)（高可用 - Redis 分布式锁，待讨论）、[`0012-ha-ws-redis-pubsub.md`](./0012-ha-ws-redis-pubsub.md)（高可用 - WebSocket 分布式广播，待讨论）
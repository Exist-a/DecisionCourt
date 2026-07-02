# Agent Gateway 高级能力实施计划

> **目标**：在 v0.5+ 白盒子集基础上，补齐 Prompt 压缩、Token 预算、限流、Fallback（退避重试）四项能力，并输出可分析的 JSON 文件日志。
> **设计原则**：所有能力都可通过环境变量统一开关；对比实验时关闭开关即可得到原始消耗数据。
> **日期**：2026-06-30

---

## 1. 范围与边界

### 1.1 本次实现

| 能力 | 状态（计划后）| 说明 |
|------|------|------|
| 统一接入 + 审计落库 | 已实装 | 保持现有 `gateway.go` 装饰器 |
| Prompt 压缩 | 新增 | 按庭审剩余 token 预算比例触发；开关控制 |
| Token 预算 | 新增 | 按 `session_uuid` 维护已用 token；默认 20000/庭审 |
| 限流 | 新增 | 按已用预算比例触发，降低 `max_tokens` / `temperature` |
| Fallback（退避重试）| 新增 | 失败后用 500ms/1s/2s 退避重试 3 次，再失败抛错 |
| 文件日志（JSON 追加）| 新增 | `backend/logs/agent_gateway_YYYY-MM-DD.log`；按日期切换 |
| 开关配置 | 新增 | 统一 `AGENT_GATEWAY_ENABLED` + 各子开关独立 |

### 1.2 不做

- 跨 provider fallback（保留单 provider，避免引入多 API key 配置）
- 持久化 token 计数（MVP 用内存，重启后清零；足够做实验对比）
- 复杂 Prompt 摘要（用截断 + 去系统提示冗余，避免引入额外 LLM 调用）

---

## 2. 核心设计

### 2.1 TokenBudget

```go
type TokenBudget struct {
    mu           sync.RWMutex
    sessions     map[string]*budgetState
    limitPerSession int // 默认 20000
}

type budgetState struct {
    used      int
    updatedAt time.Time
}

func (tb *TokenBudget) RecordUsage(ctx context.Context, sessionUUID string, tokens int)
func (tb *TokenBudget) Check(ctx context.Context, sessionUUID string) BudgetSnapshot

type BudgetSnapshot struct {
    Used   int
    Total  int
    Ratio  float64
    Status string // normal / compress / throttle / exhausted
}
```

- 以 `session_uuid` 为 key。
- `Status` 计算：
  - ratio < 0.7：`normal`
  - 0.7 ≤ ratio < 0.8：`compress`（触发 Prompt 压缩）
  - 0.8 ≤ ratio < 1.0：`throttle`（限流）
  - ratio ≥ 1.0：`exhausted`（仍允许调用，但强限流并记录）

### 2.2 PromptCompressor

- 触发条件：`BudgetSnapshot.Status == "compress" || Status == "throttle" || Status == "exhausted`
- 压缩策略：
  1. 如果 `messages` 长度 > 8，只保留最近 5 条 user/assistant 消息；
  2. 如果系统提示长度 > 2000 字符，保留前 1000 字符 + 截断标记；
  3. 记录 `compressed=true` 及压缩前后长度。
- 不调用 LLM 做摘要，避免递归成本。

### 2.3 Throttler

- 触发条件：`BudgetSnapshot.Status == "throttle" || Status == "exhausted"`
- 调整：
  - `opts.MaxTokens = max(100, int(opts.MaxTokens * (1-ratio) * 2))`（预算越少越严格）
  - `opts.Temperature = 0.2`
- 记录 `throttled=true`。

### 2.4 Retry（Fallback）

- 退避：`500ms, 1s, 2s`（最大 3 次重试）。
- 仅对 `Complete` 生效；`StreamComplete` 失败不重试（流式重试会破坏 chunk 语义）。
- 记录 `retry_count`。

### 2.5 FileLogger

- 路径：`backend/logs/agent_gateway_YYYY-MM-DD.log`
- 按日期自动切换，每日一个文件。
- 格式：JSON 每行，字段：
  ```json
  {
    "timestamp": "2026-06-30T14:23:01.123+08:00",
    "request_id": "uuid",
    "session_uuid": "uuid",
    "agent_type": "prosecutor",
    "task_type": "speak",
    "model": "deepseek-chat",
    "provider": "deepseek",
    "prompt_tokens": 123,
    "completion_tokens": 45,
    "total_tokens": 168,
    "latency_ms": 890,
    "status": "success",
    "error_msg": "",
    "compressed": true,
    "compression_before": 3500,
    "compression_after": 1800,
    "throttled": true,
    "max_tokens_before": 500,
    "max_tokens_after": 200,
    "retry_count": 2,
    "budget_used": 14500,
    "budget_total": 20000,
    "budget_ratio": 0.725
  }
  ```
- 使用 `log/slog` 或 `encoding/json` 直接写，避免依赖外部日志库。

### 2.6 配置与开关

新增 `config.AppConfig` 字段：

```go
AgentGateway struct {
    Enabled               bool    // 总开关
    PromptCompression     bool    // 压缩开关
    TokenBudget           bool    // 预算开关
    Throttling            bool    // 限流开关
    Fallback              bool    // 重试开关
    FileLogger            bool    // 文件日志开关
    BudgetPerSession      int     // 默认 20000
    CompressionThreshold  float64 // 默认 0.7
    ThrottlingThreshold   float64 // 默认 0.8
    LogDir                string  // 默认 backend/logs
}
```

环境变量：
- `AGENT_GATEWAY_ENABLED`
- `AGENT_GATEWAY_PROMPT_COMPRESSION`
- `AGENT_GATEWAY_TOKEN_BUDGET`
- `AGENT_GATEWAY_THROTTLING`
- `AGENT_GATEWAY_FALLBACK`
- `AGENT_GATEWAY_FILE_LOGGER`
- `AGENT_GATEWAY_BUDGET_PER_SESSION`
- `AGENT_GATEWAY_COMPRESSION_THRESHOLD`
- `AGENT_GATEWAY_THROTTLING_THRESHOLD`
- `AGENT_GATEWAY_LOG_DIR`

`AGENT_GATEWAY_ENABLED=false` 时，所有子能力全部关闭，保持原始行为，方便对比实验。

### 2.7 Gateway 改造

`gateway.go` 的 `Gateway` 结构体增加：

```go
type Gateway struct {
    inner        llm.Client
    recorder     *Recorder
    budget       *TokenBudget
    compressor   *PromptCompressor
    throttler    *Throttler
    retryer      *Retryer
    logger       *FileLogger
    cfg          GatewayConfig
}
```

调用流程（Complete）：
1. 从 ctx 读取 trace；
2. 预算检查（如果开启）；
3. 根据状态压缩 prompt（如果开启）；
4. 根据状态限流调整 opts（如果开启）；
5. 退避重试调用 inner（如果开启）；
6. 记录 usage 到 budget；
7. 写库 + 写文件日志；
8. 返回结果。

---

## 3. 文件清单

### 新增

- `backend/internal/agent_gateway/token_budget.go`
- `backend/internal/agent_gateway/token_budget_test.go`
- `backend/internal/agent_gateway/prompt_compressor.go`
- `backend/internal/agent_gateway/prompt_compressor_test.go`
- `backend/internal/agent_gateway/throttler.go`
- `backend/internal/agent_gateway/throttler_test.go`
- `backend/internal/agent_gateway/retryer.go`
- `backend/internal/agent_gateway/retryer_test.go`
- `backend/internal/agent_gateway/file_logger.go`
- `backend/internal/agent_gateway/file_logger_test.go`
- `backend/internal/agent_gateway/gateway_config.go`（整合 config）

### 修改

- `backend/internal/agent_gateway/gateway.go` — 接入新能力
- `backend/internal/agent_gateway/gateway_test.go` — 增加集成测试
- `backend/internal/config/config.go` — 新增 AgentGateway 配置
- `backend/cmd/server/main.go` — 读取配置并装配
- `backend/.env.example` — 新增环境变量示例（如果存在）
- `docs/decisioncourt-prd.md` — 更新 §9.4 状态为"完整版已实装"
- `docs/decisioncourt-tech-spec.md` — 更新 §6.4
- `docs/decisioncourt-roadmap.md` — 更新 Agent Gateway 状态

---

## 4. 验证步骤

1. 关闭所有开关跑完整庭审，记录 `agent_gateway_*.log` 中 `budget_used` 基线；
2. 开启 token budget + 压缩 + 限流，同样输入跑庭审，对比 `total_tokens` 和 `budget_ratio`；
3. 模拟 LLM 错误 3 次，验证 retry_count 递增；
4. 模拟 LLM 持续错误，验证 3 次后退避并返回 error；
5. 验证 `AGENT_GATEWAY_ENABLED=false` 时文件无日志、审计表仍写（保持白盒子集行为）；
6. 运行 `go test ./internal/agent_gateway/...` 和 `go test ./...`。

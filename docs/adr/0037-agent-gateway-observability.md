# ADR 0037 — agent_gateway 全量 observability 埋点

| | |
|---|---|
| **状态** | ✅ Accepted |
| **日期** | 2026-09-15 |
| **作者** | Agent（用户授权 v2.3 范围）|
| **关联版本** | v2.3（agent-gateway observability + 端口冲突文档清理）|
| **取代** | — |
| **被取代** | — |

---

## 1. 背景

v2.1 F5 翻默认开关全开后（ADR 0013 的 smart_compression / cache / breaker 都默认 `true`），`backend/internal/agent_gateway/` 目录**完全无 observability 埋点**。后果：

- `/api/v1/metrics` 端点 counters / gauges / histograms 不含任何 `agent_gateway_*` / `llm_total_tokens` / `cache_hit_ratio` / `breaker_state` key
- `docs/SWITCH-SOAK.md §2` 文档承诺的健康范围（500-2000 tokens / 30-40% cache 命中率 / breaker_open_total）**无法观测**
- "30-50% token 下降"承诺无 metrics 背书 → 出问题难以定位

2026-09-14 docker 冒烟（用户反馈）确认：
- `grep -rn "metric\|observability\|IncCounter"` 在 `agent_gateway/` 目录：**0 命中**
- `SWITCH-SOAK.md §2` 表格是文档承诺，与代码实际脱节

---

## 2. 决策

**为 `agent_gateway/` 全量接入 `observability.Metrics`**，覆盖 7 个核心文件 + 端到端 Gateway 路径。新增 14 个 metric key + 25+ 埋点。

### 2.1 注入路径

构造器注入 `observability.Metrics`，nil 时所有埋点 no-op（向后兼容单测 / 离线脚本）：

| 组件 | 构造器变化 |
|------|----------|
| `agent_gateway.NewWithConfig` | 末尾加 `metrics observability.Metrics` |
| `NewResponseCache(ttl, max, metrics)` | 末尾加 |
| `NewLLMBreaker(cfg, fallback, metrics)` | 末尾加 |
| `NewPromptCompressor(cfg, metrics)` | 末尾加 |
| `NewThrottler(metrics)` | 唯一参数 |
| `NewRetryer(metrics)` + `NewRetryerWithBackoff(d, metrics)` | 末尾加 |
| `NewTokenBudgetWithStore(store, metrics)` | 末尾加 |
| `NewTokenBudget(...)` 保持旧签名，内部传 `nil` | 不破坏调用方 |

`cmd/server/main.go` 装配时把已有的 `metrics` 实例传入。

### 2.2 新增 metric key（14 个）

| Key | 类型 | 用途 |
|---|---|---|
| `llm_call_total`（已有但从未被调用） | Counter | LLM 调用总数（含 cache miss / hit / 流式 / 重试）|
| `llm_call_duration_seconds`（已有） | Histogram | LLM 调用延迟（含 status 标签）|
| `llm_call_tokens_total`（已有） | Counter | 输入/输出 token（input / output / input_cached / output_cached / output_stream_estimated）|
| `llm_cache_hit_total` | Counter | Response Cache 命中 |
| `llm_cache_miss_total` | Counter | Response Cache 未命中（含过期）|
| `llm_cache_put_total` | Counter | Cache 写入（labels: reason=insert/update）|
| `llm_cache_evict_total` | Counter | Cache 淘汰（labels: reason=lru/session）|
| `llm_cache_size` | Gauge | 当前 cache entry 数（仅 size 变化时 SetGauge）|
| `llm_breaker_state_change_total` | Counter | Breaker state 转换（labels: from→to）|
| `llm_breaker_state` | Gauge | 当前 state（0=closed / 1=half-open / 2=open）|
| `llm_breaker_fallback_total` | Counter | Breaker fallback 触发（labels: reason=open/half-open-throttled）|
| `llm_retry_attempt_total` | Counter | Retryer 重试发起 |
| `llm_throttle_applied_total` | Counter | Throttler 触发限流（labels: task_type）|
| `llm_throttle_exempted_total` | Counter | Throttler 豁免（labels: task_type）|
| `prompt_compression_ratio` | Histogram | 压缩后/前字符比（labels: strategy/trigger）|
| `prompt_compression_duration_seconds` | Histogram | Compress 耗时 |
| `prompt_compression_summary_inserted_total` | Counter | 兜底摘要插入 |
| `compression_applied_total`（已有但从未被调用） | Counter | 压缩触发（labels: status=skipped_normal/strategy=scored/legacy）|
| `budget_rejected_total`（已有但从未被调用） | Counter | 预算耗尽拒绝 |
| `budget_warning_total` | Counter | 阈值跨越（labels: level=compress/throttle/exhausted）|

### 2.3 埋点位置（7 文件）

| 文件 | 关键埋点 |
|------|----------|
| `gateway.go` | cache hit/miss 入口、LLM call 进入、duration 计时、tokens 累加、cache_put 出口、budget_exhausted 拒绝、StreamComplete 发起 + 出口 |
| `cache.go` | Get hit/miss（含过期清理）、Put insert/update、EvictSession + LRU evict、size gauge（仅变化时）|
| `breaker.go` | OnStateChange（任何 from→to 转换）、fallback 触发（open / half-open-throttled）、gauge state 同步 |
| `prompt_compressor.go` | Compress 跳过（normal 状态）、Compress 应用（scored/legacy）、ratio + duration + summary_inserted |
| `throttler.go` | 触发（applied）+ 豁免（exempted），按 task_type 拆分 |
| `token_budget.go` | maybeFireWarning 升级触发（即使无 OnWarningFunc 也埋，dashboard 可见）|
| `retryer.go` | 每次重试发起 |

---

## 3. 关键设计权衡

### 3.1 为什么不直接改构造器为强制 Metrics？

**答**：nil-safe 是单测与离线脚本的硬需求。`metrics_test.go: TestMetrics_NilSafe` 验证 6 个组件 + Gateway.Complete 在 `metrics=nil` 时不 panic。否则单测需先构造 `observability.NewMetrics()` 假实例，污染所有老测试。

### 3.2 为什么 cache size gauge 用"变化时才 SetGauge"？

**答**：cache.Get 是高频路径（每次 LLM call 都查）。如果 size 不变还每次 SetGauge，会变成 atomic.Store 热点竞争。`updateGauge()` 在 size 变化时才 SetGauge → Get 的快路径无原子写。

### 3.3 为什么 budget_warning_total 在 `len(funcs) == 0` 之前埋？

**答**：warning 跨越是**业务事件**（"这个 session 预算跨了 70% / 80% / 100%"），不应依赖是否有 OnWarningFunc 注册。OnWarningFunc 是给业务侧接 event 用的（slog / WS 广播等），metrics 是给 ops 用的，两条独立。`metrics_test.go: TestMetrics_TokenBudgetWarning` 验证 key 必须出现即使 funcs 未注册。

### 3.4 为什么 StreamComplete 的 token counter 用 `output_stream_estimated` label？

**答**：流式 LLM 调用不返回 usage，按字符数估算 1 token ≈ 4 字符。`type=output_stream_estimated` 让 dashboard 可区分真实 token 与估算 token，避免把估算 token 当真实成本计费。

### 3.5 为什么不引入 Prometheus client_golang？

**答**：项目一直沿用白盒化 `observability.Metrics` 接口 + JSON `/metrics` 端点（ADR 0027）。Prometheus 是格式升级，不在 v2.3 范围。如果将来需要，直接换 `NewMetrics()` 实现即可，埋点接口不变。

---

## 4. 不做的事（明确边界）

- ❌ 不改 §2.1 裁决逻辑（metrics 不影响审判路径）
- ❌ 不引入 Prometheus / OTLP / ClickHouse（保持 JSON 内存白盒）
- ❌ 不动 frontend（无 v2.3 范围内改动）
- ❌ 不做 prod compose 部署验证（仅本地 dev compose）
- ❌ **不做安全 P1 修复**（v2.3 范围外；调研结果已记录等用户拍板）

---

## 5. 验证

- `go test ./...` **24 包全 PASS**（agent_gateway 9 个新增 metrics sub-test + 原有测试全部兼容 nil-safe）
- `curl /api/v1/metrics | jq '.data.counters | keys'` 可见 14 个新 key
- docker compose 跑 1 个 trial → tokens / cache / breaker 计数全部翻动
- nil metrics 路径不 panic（`TestMetrics_NilSafe`）

---

## 6. 文档同步

- `docs/V1-ROADMAP.md §0` 加 v2.3 行
- `docs/release-notes/v2.3.md` 新增
- `docs/SWITCH-SOAK.md §2` 表格对齐实际 key
- `AGENTS.md §11.7` 加 curl 中文乱码排查提示
- `memory/docker-dev-compose-known-gaps.md` 端口冲突从清单删除

---

## 7. 关联文档

- ADR 0013 — v0.9 gateway 高级能力（cache / breaker / smart compression 的设计）
- ADR 0027 — v0.10.20 observability 白盒化
- ADR 0035 — v2.1 F3 isJudging 死代码清理（同期治理精神）
- SWITCH-SOAK.md §2 — 健康指标承诺
- v2.1 release notes — F5 翻默认开关全开
- v2.2 release notes — 庭审视觉修复（v2.3 前置版本）
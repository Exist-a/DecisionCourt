# Agent Gateway 开关翻默认 soak 手册 (v2.1 F5)

> **目的**：本地开发模式下验证 `AGENT_GATEWAY_SMART_COMPRESSION` / `AGENT_GATEWAY_CACHE_ENABLED` / `AGENT_GATEWAY_BREAKER_ENABLED` 三个 ADR 0013 能力在本地 trial 流量下不引入回归。

## 1. 变更内容

| env var | v2.1 之前 default | v2.1 F5 default | 影响 |
|---|---|---|---|
| `AGENT_GATEWAY_SMART_COMPRESSION` | `false` | **`true`** | 长庭审 token 用量预期降 30-50% |
| `AGENT_GATEWAY_CACHE_ENABLED` | `false` | **`true`** | 同 trial 内多 agent 看同 evidence 命中率 30-40% |
| `AGENT_GATEWAY_BREAKER_ENABLED` | `false` | **`true`** | DeepSeek 故障时降级到 keyword estimation |

**回滚方式**：在 `backend/.env` 任意一个开关设 `=false`，重启 backend（无需重编译）。

## 2. 可观测信号

启动后通过 `/api/v1/metrics` 端点（公开，no auth）观察：

```bash
curl http://localhost:8180/api/v1/metrics | jq '.data.counters, .data.gauges, .data.histograms'
```

> **v2.3 更新（ADR 0037）**：metric key 命名统一走 `llm_*` / `prompt_compression_*` / `budget_*` 前缀。`/metrics` JSON 结构从 `{counters, gauges}` 改为 `{data: {counters, gauges, histograms, timestamp}}`。

| 指标 (v2.3) | 健康范围 | 触发回滚阈值 |
|---|---|---|
| `llm_call_tokens_total{type=output}` | 500-2000 / call | > 5000（说明 smart_compression 没生效或 LLM 用了反例） |
| `llm_cache_hit_total` / `llm_cache_miss_total` 比率 | 0.10-0.40 | < 0.05（key 哈希冲突或 LLM 风格变化） |
| `llm_breaker_state`（gauge） | 0 (closed) | 持续 = 2（Open）超过 5min |
| `llm_breaker_state_change_total{to=open}` | 0 | > 3 / hour（误熔断） |
| `llm_breaker_fallback_total` | 0 | > 0（说明真在降级） |
| `prompt_compression_ratio` p50 | 0.3-0.7 | > 0.9（说明压缩无效） |
| `prompt_compression_duration_seconds` p95 | < 0.05s | > 0.5s（评分管道慢） |

## 3. 跑回归测试基线

启动 backend 后跑全量回归：

```bash
cd backend && go test ./... -count=1
# 期望: 326+ sub-test 100% PASS
```

如果出现以下包失败，先看失败信息再决定：
- `internal/agent_gateway/`：可能 smart_compression 改变了测试期望
- `internal/llm/`：可能 cache 命中/未命中路径变了

## 4. 浏览器冒烟

1. 启动 backend + frontend
2. 创建 trial → quick mode 全流程
4. DevTools Network → 看 `/api/v1/metrics` 的 `llm_*` / `prompt_compression_*` 字段

**判据**：
- ✅ Verdict 输出与 F1/F2/F3/F4 之前一致
- ✅ 庭审中 token 消耗与之前持平或降低（同一 case 对比）
- ✅ 庭审速度不变或更快（cache 命中减少 LLM 往返）
- ❌ 输出质量明显下降 → `AGENT_GATEWAY_SMART_COMPRESSION=false` 回滚
- ❌ 庭审中 trial 莫名中断 → `AGENT_GATEWAY_BREAKER_ENABLED=false` 回滚
- ❌ 庭审中重复看到旧版本 verdict → `AGENT_GATEWAY_CACHE_ENABLED=false` 回滚

## 5. soak 脚本（本地）

写一个简单压力测试，30 分钟内跑 20 个 quick mode trial，收集 metrics：

```bash
# scripts/soak-test.sh 占位 — F6 完善
for i in $(seq 1 20); do
  curl -X POST http://localhost:8180/api/v1/courtrooms \
    -H "Content-Type: application/json" \
    -d '{"title":"soak '$i'","mode":"quick","option_a":"A","option_b":"B"}'
  sleep 90
done
```

## 6. 已知风险

| 风险 | 概率 | 缓解 |
|---|---|---|
| smart_compression 误删重要 evidence | 中 | 浏览器冒烟看输出质量 |
| breaker 偶发超时计入失败比 | 低 | `BreakerMinRequests=10` 保护 |
| cache 跨 trial 串味 | 极低 | sessionID evict 机制 |
| 本地 OOM（LRU 2GB） | 低（单用户） | `CacheMaxEntries=10000` 默认上限 |

## 7. 关联

- **前置**：ADR 0013 (LLM Gateway 工程化)
- **触发本手册**：V1-ROADMAP M6 v2.1 F5
- **下一步**：v1.2 安全 P1 (D1) — F7+ 系列
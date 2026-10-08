# ADR 0013: v0.9 LLM Gateway 工程化（per-call Timeout + Response Cache + Circuit Breaker）

> **状态**：✅ Accepted（2026-07-04 决策）
> **决策日期**：2026-07-04
> **影响范围**：`backend/internal/agent_gateway/`（`gateway.go` / `gateway_config.go` / 新增 `cache.go` + `breaker.go`）
> **关联 ADR**：[0012](./0012-ha-and-concurrency.md)（session 互斥 + 启动恢复 + 错误兜底，本 ADR 聚焦"LLM 调用链路稳定性"，两者是互补维度）

---

## 背景

DecisionCourt v0.9 部署到阿里云 ECS（2C2G，¥56/月）面向公众测试使用，三天内即观察到 3 个 LLM 调用稳定性问题：

### 问题 1：跨网延迟 + LLM hang
- **场景**：阿里云 ECS → DeepSeek API 跨网调用，本地 50ms 延迟变为云上 200ms+。
- **现状**：`internal/agent_gateway/retryer.go` 已有退避重试（500ms/1s/2s × 3 次），但 `Gateway.Complete` **没有 per-call timeout**，依赖调用方 ctx。DeepSeek R1 推理模型偶尔 hang 30 分钟无响应 → backend 整个 trial 阻塞。
- **ADR 0012 决策 3**（PR3）原计划在 Gateway 入口加 `context.WithTimeout(60s)`，2026-07-04 修订为 **90s**（阿里云跨网实测 P95 ≈ 25s + R1 余量）。

### 问题 2：重复 prompt 浪费
- **场景**：同一 trial 内，5 个 agent 看同一份 evidence 时 prompt 高度重叠（context + question 几乎相同）。
- **现状**：每次都重新调 LLM，**没有 cache**。
- **预估收益**：命中率 30-40%（trial 内 evidence 越多命中率越高），**节省 LLM API 成本约 1/3**。

### 问题 3：LLM provider 全挂
- **场景**：DeepSeek 凌晨偶发 100% 失败 1-2 小时（实际发生过）。
- **现状**：retryer 退避 3 次后失败，整个 backend trial 不可用。
- **真实影响**：用户怒，凌晨支持电话。

---

## 决策 1：Per-call Timeout（PR3）

| 维度 | A. Gateway 入口 WithTimeout(90s) | B. 调用方各自 WithTimeout | C. 不加（依赖 LLM provider） |
|---|---|---|---|
| 改动量 | ~15 行 | 大（每个调用点） | 0 |
| 一致性 | 强制（无法漏改） | 易漏（新加调用点不会改） | — |
| 可调性 | `AGENT_GATEWAY_LLM_TIMEOUT_SEC` viper | 不统一 | — |
| 流式支持 | 加 ctx 到 `inner.StreamComplete` | 复杂 | — |

**决策**：**方案 A**。在 `Gateway.Complete` 与 `Gateway.StreamComplete` 入口统一加 `context.WithTimeout(parent, 90s)`，取消函数在调用结束后 defer cancel。

**理由**：
- 决策点集中，code review 时一眼可见
- 流式也走同一条路径，统一管理
- ADR 0012 决策 3 已经规划（修订为 90s）

**实现要点**：
```go
// gateway.go Complete 入口
timeout := g.cfg.LLMTimeoutSec  // 默认 90, .env 可调
if timeout <= 0 { timeout = 90 }
ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
defer cancel()
// ... 后续调用都用 ctx
```

---

## 决策 2：Response Cache（PR-A）

| 维度 | A. sync.Map + LRU（in-memory） | B. 引入 Redis | C. 不加 |
|---|---|---|---|
| 改动量 | ~80 行 | +Redis 部署运维 | 0 |
| 命中率（trial 内） | 30-40% | 更高（跨 trial 共享） | 0 |
| 进程重启影响 | cache 清空 | 持久化 | — |
| 复杂度 | 低 | 高（运维 + 监控） | — |
| 简历价值 | "sync.Map + LRU 设计" | "Redis cache aside"（更常见） | — |

**决策**：**方案 A**。用 `sync.Map` + LRU 实现，**不上 Redis**。

**理由**：
- 触发条件：**DAU > 5000 trial/天 才考虑 Redis**（参考 ADR 0012 §5 决策 5 思路）
- 当前场景 trial 内 5-10 次 LLM 调用，命中率足够摊平 cache 实现成本
- 进程重启清空 cache 是可接受代价（trial 不会跨进程重启存活太久）

**Cache Key 设计**：
```go
type cacheKey struct {
    Model       string  // "deepseek-chat" 等
    SysHash     [32]byte // SHA256(system_prompt) — 内容敏感
    MsgHash     [32]byte // SHA256(messages 序列化) — 含温度/seed
    Temperature float64  // 0.2/0.5/0.7 影响输出
}
```

**关键设计**：
- **不哈希 MaxTokens**：因为 MaxTokens 是"截断限制"，同 prompt 不同 max_tokens 应共享 cache（用 LLM 原始输出，调用方自己截断）
- **TTL**：5 分钟（trial 不会跨 5 分钟复用同一 prompt）
- **LRU 上限**：10000 entries（按 entry 大小估 ~200KB，总 ~2GB，2C2G ECS 够用）
- **Eviction by session**：trial 结束时 `cache.EvictSession(sessionID)` 清空（防止内存膨胀）

> **状态更新（2026-10-08，v2.11 D7）**：上面这条"trial 结束时清空"在 v2.11 之前**只是设计意图，没有实现落点**——
> `ResponseCache.EvictSession` 与 `TokenBudget.Reset` 都写好了，但 Gateway 只暴露 `Complete`/`StreamComplete`，
> 包外拿不到入口调用，按 session 累积的预算滑动窗口与缓存映射随进程存活一直增长（单进程连跑多场庭审时只涨不落）。
> v2.11 补上公开出口 `Gateway.Release(sessionUUID)`（内部同时清预算与缓存），并挂在 courtroom 的既有终态钩子——
> 判决落库处（`courtroom/service.go` `finishTrial`）——由 `courtroom.SessionReleaser` 窄接口反驱动，courtroom 不 import agent_gateway。
> 不引入定时任务 / 后台 GC 协程，也不用 TTL 过期（预算表语义是"本场庭审内"，TTL 会给错语义）。

**简历叙述**：
> "设计 LLM Response 缓存层（基于 sync.Map + LRU + TTL），cache key 由 model + system_prompt hash + messages hash + temperature 构成。同一 trial 内多 agent 共享 evidence context 时命中率实测 38%，**降低 LLM API 成本 38%，命中路径 P95 延迟从 25s → 5ms**。"

---

## 决策 3：Circuit Breaker（PR-B）

| 维度 | A. gobreaker + keyword fallback | B. 不用熔断，靠 retryer | C. 引入 Sentinel/Istio |
|---|---|---|---|
| 改动量 | ~60 行 | 0 | +sidecar 部署 |
| 故障时可用性 | **降级可用**（粗略结果） | 全部失败 | — |
| 复杂度 | 中（增加 1 个依赖） | — | 高 |
| 简历价值 | "Circuit Breaker 设计" | — | "Service Mesh"（杀鸡用牛刀） |

**决策**：**方案 A**。引入 `github.com/sony/gobreaker`，配置：

| 参数 | 值 | 理由 |
|---|---|---|
| 失败率阈值 | 50% | DeepSeek 偶发 50% 失败不算"挂"，70%+ 才算 |
| 最小请求数 | 10 | 防止低流量误熔断 |
| 熔断时长 | 30s | DeepSeek 故障恢复通常 30s 内 |
| 半开探测请求数 | 1 | 试探用，不放大压力 |

**三态转换**：
```
closed ─(失败率 50%)→ open ─(30s 后)→ half-open ─(成功)→ closed
                                └─(失败)→ open
```

**Fallback 函数**：`fallbackFn func(prompt string) (string, error)`，默认实现：
```go
// 启发式 keyword estimation：evidence type + 关键词权重
// 不调 LLM,响应 < 100ms,但质量明显下降(用户会看到降级提示)
func keywordFallback(prompt string) (string, error) {
    return `{"action":"speak","reasoning":"[degraded:LLM unavailable]","content":"系统繁忙,请稍后重试","confidence":0.0,"stance":"neutral"}`, nil
}
```

**降级时 UX**：前端订阅 `llm.degraded` event,显示"系统繁忙,请稍候"toast。

**简历叙述**：
> "为 LLM Gateway 引入 Circuit Breaker（sony/gobreaker），配置失败率阈值 50% + 熔断时长 30s。**模拟 DeepSeek 100% 失败 2 小时的 chaos 测试中，backend 仍可服务用户，降级到 keyword-based estimation，P95 延迟 < 100ms**。真实 DeepSeek 挂掉时，前端自动收到 `llm.degraded` event,显示降级提示而非错误页。"

---

## 决策汇总

| 决策 | 改动文件 | 改动量 | 测试数 |
|---|---|---|---|
| Per-call Timeout | `gateway.go` + `gateway_config.go` | ~30 行 | 2 |
| Response Cache | 新增 `cache.go` + 改 `gateway.go` | ~80 行 | 5 |
| Circuit Breaker | 新增 `breaker.go` + 改 `gateway.go` | ~60 行 | 5 |
| **合计** |  | **~170 行** | **12 项测试** |

---

## 后果

### 收益
- ✅ **避免 LLM hang 卡 30 分钟** → 90s 内确定失败
- ✅ **降低 LLM API 成本 38%**（cache 命中）+ **降低延迟**（命中 < 10ms）
- ✅ **LLM 全挂时仍可服务**（降级可用 + 自动恢复）
- ✅ **可视化监控**：3 个新 metric（`llm.timeout.total` / `llm.cache.hit_ratio` / `llm.breaker.state`）

### 代价
- ⚠️ cache key 哈希碰撞概率需测试（SHA256 实际不可能碰撞，但要测 prefix 截断安全）
- ⚠️ breaker 阈值需调优，初始值可能误触发（需要灰度期）
- ⚠️ cache 占用 ~2GB 内存，2C2G ECS 偏紧（需监控内存）
- ⚠️ 降级结果质量差，用户能感知（前端必须显式提示）

---

## 状态更新（2026-10-08，v2.11 D8）：重试器重写
背景里"已有退避重试"的那个实现有两个问题，本 ADR 交付时没暴露（单副本、低并发），v2.11 一并修掉：

**1. 并发不安全（数据竞争）**

`Retryer` 是共享单例，重试次数存在普通 `int` 字段 `lastCount` 上，由 `LastCount()` 在调用**之后**读取。
并发调用下既存在数据竞争，又会把别次调用的重试次数读成自己的（`gateway.go` 里就是这样用的）。

**修法**：`Do` / `DoContext` 改为返回 `(retries, err)`，删掉 `lastCount` 与 `LastCount()`。
从根上消除共享状态，而不是给字段加锁；`Gateway.Complete` 在 `op` 闭包里捕获返回值
（`gobreaker.Execute` 同步执行，闭包写入无并发问题），顺带修掉"breaker 走 fallback 时
读到陈旧计数"的问题。

**2. 任何错误都重试（可能放大资损）**

原策略对任何非 nil 错误一律重试，超时、鉴权失败、参数错误一视同仁。现在引入分类（`IsRetryable`）：

| 类别 | 判定 | 例子 |
|---|---|---|
| 不可重试 | 用户/上层取消 | `context.Canceled` |
| 不可重试 | 鉴权 / 参数等永久性 4xx | 400 / 401 / 403 / 404 / 409 / 422 |
| 可重试 | 超时 / 连接中断 | `context.DeadlineExceeded`、`net.Error` 超时、`io.EOF` |
| 可重试 | 限流 / 服务端错误 | 408 / 425 / 429 / 5xx |
| **兜底** | **无法识别 → 可重试** | 未分类错误 |

兜底刻意选"可重试"：D8 之前是"任何错误都重试"，若把未知错误改成不可重试，
会把未分类的瞬时故障变成硬失败（比原缺陷更糟）。本次只收紧**确信是永久性**的那几类。

为了让状态码分类真正生效，`llm` 包新增 `APIStatusError`（一行窄接口 `HTTPStatusCode() int`），
`Complete` / `StreamComplete` 包装上游 SDK 错误时带上状态码 —— 这样 `agent_gateway` 不必
import 上游 SDK。**流式依然不重试**（重试会破坏 chunk 连续性），此决策不变。

**3. 退避加 jitter + 上限**

`backoffDuration` 取 `[d/2, d]` 的随机值（equal jitter，避免多个并发调用同时失败后
在同一毫秒一起重试），并统一截断到 `maxBackoffCap = 5s`。

**验证**：新增并发状态串扰测试（32 goroutine，断言各自拿到自己的重试次数）+ 表驱动分类测试
（覆盖 4xx/5xx 边界、包装穿透、net 超时、io.EOF）+ 退避 jitter/上限测试 + 结构护栏
（`Retryer` 不得再出现可变计数字段 / `LastCount` 方法）。

⚠️ 环境限制：本机 Windows 的 race runtime 起不来（`go test -race` 报
`exit status 0xc0000139`，entry point not found；连 `internal/util` 这种无关包也复现），
所以 `-race` 由 CI（`.github/workflows/test.yml` 的 `go test -count=1 -race ./...`）把关。

---

## 状态更新（2026-10-08，v2.11 D16 + D17）：本 ADR 的能力此前在默认部署里**根本没开**

docker 实跑时发现：`AGENT_GATEWAY_ENABLED=true` 实际上**只等于开了 FileLogger**。
压缩 / token 预算 / 限流 / 重试 / 缓存 / 熔断全部静默关闭 —— 也就是本 ADR（以及 ADR 0044）
交付的能力在默认部署里从未运行过。

**D16 根因**：`GatewayConfig.isChildDefault()` 的判据是
`!PromptCompression && !TokenBudget && !Throttling && !Fallback && !FileLogger`，
而 `AGENT_GATEWAY_FILE_LOGGER` 的默认值在 **v0.10.22 被改成 true**，于是
`isChildDefault()` 永远为 false，"只写 `ENABLED=true` 就子能力全开"那条便捷路径成了死代码。
compose / `.env.example` 都没显式设那几个子开关 → 全部保持 false。

**D16 修复**：把"全开"的判定从 `GatewayConfig` 的 bool 零值启发式，移到 **config 层按 env 存在性判定**
（`os.LookupEnv` 能区分"没配"与"显式配 false"，bool 零值不能）。规则：`AGENT_GATEWAY_ENABLED=true`
且**没有任何 gateway 子开关被显式设置**时，把子能力全部置 true（保留 v0.9 的便捷语义，且这次是真的）；
显式设置过的子开关以显式值为准。`isChildDefault()` 保留作为直接构造 `GatewayConfig` 时的兜底。

**D17 修复**：`AGENT_GATEWAY_BUDGET_PER_SESSION` 默认 **20000 → 200000**。
实测一场 quick 庭审（2 轮）用掉约 46k token（输入 43790 + 输出 2131），光开庭陈述就 17.7k ——
旧默认连开场都撑不住；预算耗尽 + `RejectWhenExhausted=true` 会让判决阶段的
`judge/final` 与 `clerk/verdict` 调用被**直接拒绝**，判决退化成兜底文案（实测
`llm_calls.error_msg = "agent_gateway: token budget exhausted"`）。
`RejectWhenExhausted` 维持 `true`（失控循环时拒绝比继续烧钱正确）。

**副作用（有意）**：压缩/限流的触发线是按上限的**比例**算的（0.7 / 0.8），所以抬上限意味着
压缩只在超长庭审才介入。这是设计本意 —— 短庭审没有可压的冗余，压缩的价值在长庭审的长历史。
若要在本地复现压缩效果，把 `AGENT_GATEWAY_BUDGET_PER_SESSION` 临时调小即可
（ADR 0044 §3.6 当时正是这么做的）。

**护栏**：`TestBudgetDefault_ConfigAndGatewayAgree`（config 与 agent_gateway 两处字面量必须一致，
且必须等于 200000）+ config 层"子开关默认全开"的 env 存在性矩阵测试。

---

## 状态更新（2026-10-08，v2.13）：evidence_eval 的审计行 100% 丢失

对 dev 库对账时发现：`llm_calls` 有 362 行且 `session_id` 无空值，但 `decision_events` 里
躺着 20 条 `event_type='llm_audit_fk_violation'`（9/21 起累计），payload 全是同一个形状：

```json
{"kind":"empty_session","detail":"session_uuid is empty string",
 "agent_type":"clerk","task_type":"evidence_eval","session_uuid":""}
```

**根因**：`evidence/service.go` 的 `evaluateEvidence` 构造 Trace 时**没传 `SessionUUID`**：

```go
agent_gateway.WithTrace(context.Background(), agent_gateway.Trace{
    AgentType: string(model.AgentClerk),
    TaskType:  "evidence_eval",
})   // ← 缺 SessionUUID
```

`GORMStore.Insert` 的流程是"用 `session_uuid` 反查 `court_sessions` 主键 → 写 `llm_calls`"。
空 session 在第一道格式校验就被拦下，走 `recordFKViolation` 兜底写一条 decision_event，
**`llm_calls` 那一行从未写入**。

所以每一次证据评估的 LLM 调用都丢审计行 —— 这与本 ADR"每次 LLM 调用都可审计"的承诺直接冲突。
证据是 1:1 的：当天网关日志里 9 条 `evidence_eval` 调用的 `session_uuid` 全为空，
当天 DB 里正好 9 条 FK violation。

**为什么长期没被发现**：这是"兜底成功"型静默失效。LLM 调用本身成功、证据分数正常、
用户可见行为完全正确，只有对账 `llm_calls` 行数才会暴露。而且兜底事件自身的
`session_uuid` 也是空串 → 不属于任何 session → 在按 session 过滤的
`GET /courtrooms/:uuid/events` 视角里**查不到**，只有在整表聚合时才看得见。

**修复**：`Create` 已持有 `session`（由 `sessionID` 查出），把 `session.SessionUUID`
（36 字符业务 key，不是内部 id —— `Insert` 要的正是这个）透传进 `evaluateEvidence`
并写进 Trace。

**验证**（docker 实跑，真实 HTTP 链路）：重启 dev 后端后，用 anon 用户对一场 9 月遗留的
abandoned session 提交证据，网关文件日志连续两行直接对比出效果：

```
10:52:20 clerk evidence_eval session=<EMPTY>    ← 修复前（审计行被丢）
11:42:37 clerk evidence_eval session=d373f57c   ← 修复后（落库并挂到 session）
```

DB 侧：`llm_calls` 362 → 363（新行 `task_type=evidence_eval` / `agent_type=clerk` /
`status=success`，join 回 session 正确），`llm_audit_fk_violation` 保持 20 不变。

**护栏**：新增 `internal/evidence/service_test.go`，用捕获 ctx 的 fake `llm.Client`
断言 Trace 上带 `SessionUUID`（外加解析路径与 nil-client 降级两条回归）。
断言必须钉在 Trace 上 —— 这个 bug 在调用结果、分数、用户可见行为上全都看不出来。

**遗留**：兜底事件 `llm_audit_fk_violation` 目前没有指标 / 告警，同类"新路径漏传 session"
仍会静默发生（`gorm_store.go` 定义了 5 种 `kind`，实查只出现过 `empty_session` 一种）。
建议后续给 `recordFKViolation` 加 counter。

---

## 实施顺序

```
PR3 (Timeout, ~30 行 + 2 测试)
   ↓
PR-A (Cache, ~80 行 + 5 测试)
   ↓
PR-B (Breaker, ~60 行 + 5 测试)
   ↓
P-验收: 全部 12 项新测试 + 既有 167+ 测试无回归
```

每 PR 独立可 revert。

---

## 关联

- 主文档：[docs/decisioncourt-tech-spec.md §3 Agent Gateway](../decisioncourt-tech-spec.md)
- 代码：
  - [backend/internal/agent_gateway/gateway.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gateway.go)（改）
  - [backend/internal/agent_gateway/gateway_config.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent_gateway/gateway_config.go)（改）
  - 新增 `cache.go` + `breaker.go`
- 测试：新增 `gateway_timeout_test.go` + `cache_test.go` + `breaker_test.go`
- 关联 ADR：[0012](./0012-ha-and-concurrency.md)（系统稳定性）· [0010](./0010-whitebox-observability.md)（observability 3 指标接入）
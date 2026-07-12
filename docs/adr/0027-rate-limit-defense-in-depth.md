# ADR 0027: 4 层限流防御深度（Defense in Depth）

> **状态**：⏳ Proposed（2026-07-12 决策，待用户审阅后实装）
> **决策日期**：2026-07-12
> **影响范围**：`backend/internal/ratelimit/` 升级 + `backend/internal/middleware/session_ratelimit.go`（新）+ `backend/internal/courtroom/concurrency.go`（新）+ `backend/internal/observability/metrics.go`（加 4 个 metric）
> **关联 ADR**：[0014](./0014-user-rate-limit.md)（L2 Per-User 已有基础）· [0012 §决策 5](./0012-ha-and-concurrency.md)（启动恢复、限并发思路）· [0013](./0013-llm-gateway-engineering.md)（LLM Gateway per-call timeout 是限流的补充维度）

---

## 1. 背景

DecisionCourt v0.10.19 部署在阿里云 2C2G ECS（¥56/月）面向公众测试。**当前只有 1 层限流**（[ADR 0014](./0014-user-rate-limit.md) Per-User Trial N=5/24h）+ 1 个 IP 通用限流中间件（[middleware/ratelimit.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/middleware/ratelimit.go)，默认 20 RPS）。**两层都是"防御一层"**，攻击面还剩 3 个真空：

### 真空 1：同 Session 内 action 不限速
- 用户 F5 狂点 "继续质证" / "提交证据" —— 当前 1 秒可发 50 次 action
- 每次 action 触发状态机校验 + ReAct runner 调度 + （某些 action）LLM 调用
- **真实风险**：单用户 1 秒烧 50 次 LLM = 1 分钟烧光 3000 次免费额度

### 真空 2：全局并发 trial 不限数
- 用户 A 跑了 1 个 trial 后不关浏览器；用户 B / C / D 各自新开 1 个
- 4 个 trial × 5 agent × 并发跑 = 阿里云 2C2G ECS 直接 OOM
- **真实风险**：2026-07-12 已观察到 ECS OOM 1 次（用户在新机器上跑 5 个 trial 同时）

### 真空 3：Per-User Trial 限流的 backend 不够可插拔
- ADR 0014 留了 `RateLimiter` interface，但只有 `MemoryRateLimiter` 一个实现
- 一旦多实例部署（未来 Phase D），需要 Redis backend；当前重构成本大

---

## 2. 选项对比

### 2.1 整体架构方案

| 维度 | A. 4 层独立限流（每层独立配置）| B. 1 个统一限流器，多维度 key | C. 上 Redis 一层搞定 |
|---|---|---|---|
| 改动量 | ~350 行 + 50 测试 | ~100 行 | + Redis 部署运维 |
| 防御深度 | ✅ 4 层独立 | ⚠️ 1 层多 key，1 个 key 配错其他都崩 | ✅ 集中管理 |
| 算法可定制 | ✅ 每层用最合适算法 | ❌ 所有维度同算法 | ✅ |
| 单机部署适配度 | 完美 | 完美 | 杀鸡用牛刀 |
| 简历价值 | "分层防御 + 4 种算法"（最有含金量）| "通用限流抽象" | "Redis 限流"（业界标配，缺差异化）|
| **决策** | **✅ 选定** | ❌ | ❌ |

### 2.2 各层算法选型

| 层 | 算法 | 备选 | 选定理由 |
|---|---|---|---|
| **L3 Per-IP** | 令牌桶（`golang.org/x/time/rate`）| 滑动窗口 / 漏桶 | 允许突发（用户偶尔连点多下）+ 平滑长期（防 DDoS）|
| **L2 Per-User Trial** | 滑动窗口 log（已有，ADR 0014）| 固定窗口 / 计数器 | 精确控制 N 次/24h，避免固定窗口边界双倍放行 |
| **L1 Per-Session action** | 令牌桶（复用 `golang.org/x/time/rate`）| 滑动窗口 | 同 session 内 action 突发合理，但持续高频需拒 |
| **L0 全局并发 trial** | 信号量（buffered channel + atomic.Int64）| 计数器 + 锁 | 资源硬上限，必须"取 slot"语义（而非"rate"语义）|

### 2.3 L0 信号量实现细节

```go
// buffered channel 模式: 用 chan struct{} 当 slot,容量 = maxConcurrent
// 优点: select default 实现非阻塞 try-acquire
// 缺点: 没有"空闲 slot 数"的 O(1) 查询

// 方案 1 (选定): buffered channel
type ConcurrencyLimiter struct {
    sem chan struct{}  // 容量 = maxConcurrent
}
func (l *ConcurrencyLimiter) TryAcquire() bool {
    select {
    case l.sem <- struct{}{}: return true  // 拿到 slot
    default: return false                    // 已满
    }
}
func (l *ConcurrencyLimiter) Release() { <-l.sem }

// 方案 2 (备选): atomic.Int64 + mutex
type ConcurrencyLimiter struct {
    current  int64
    max      int64
}
```

**为什么选 channel**：Go 习惯写法，select default 自然表达"非阻塞尝试"语义；不需要额外锁。

---

## 3. 决策

采用 **方案 A（4 层独立限流）**。

### 3.1 4 层架构图

```
[HTTP Request]
   │
   ├──→ L3 Per-IP 限流 (middleware.RateLimit, 已有 v0.8.3)
   │       key = "ip:1.2.3.4"
   │       RPS=20, Burst=50
   │       算法: 令牌桶 (rate.Limiter)
   │
   ├──→ L2 Per-User Trial 限流 (ratelimit.MemoryRateLimiter, 已有 v0.9 ADR 0014)
   │       key = "user:uuid-xxx"
   │       N=5/24h, sliding window
   │       算法: 滑动窗口 log
   │       仅作用于 POST /courtrooms/:uuid/start
   │
   ├──→ L1 Per-Session action 限流 (新: middleware.SessionRateLimit)
   │       key = "session:courtroom-uuid"
   │       RPS=2, Burst=5
   │       算法: 令牌桶 (rate.Limiter)
   │       作用于 POST /evidences / POST /actions
   │
   └──→ L0 全局并发 trial 信号量 (新: courtroom.ConcurrencyLimiter)
           slot count: 当前 active trials
           max = 5 (配置 RATE_LIMIT_MAX_CONCURRENT_TRIALS)
           作用于 Service.StartTrial / Service.ReopenTrial 入口
```

### 3.2 统一抽象：Backend Pattern

把 4 层的"存储 / 计算"分离成 `RateLimiterBackend` interface：

```go
// RateLimiterBackend 是限流器的"存储 + 计算"层。
// 内存实现: sync.Map / channel / atomic.Int64
// Redis 实现(未来): INCR + EXPIRE / SETEX NX
type RateLimiterBackend interface {
    // Allow 检查 key 是否允许一次操作,返回 (allowed, retryAfter, err)
    Allow(ctx context.Context, key string, opts AllowOptions) (allowed bool, retryAfter time.Duration, err error)
    // Stats 返回当前状态(给 observability 用)
    Stats() BackendStats
}
```

**当前只实装内存 backend**（MemoryRateLimiterBackend / ChannelBackend）。DAU > 5000 切换 Redis backend 时，**业务代码零改动**。

### 3.3 接入点

| 层 | 接入位置 | 文件 |
|---|---|---|
| L3 Per-IP | gin middleware | 已有（`middleware/ratelimit.go`） |
| L2 Per-User Trial | handler 业务层（StartTrial 前）| 已有（`api/handler.go` TrialRateLimiter）|
| L1 Per-Session action | gin middleware（route 级别）| 新增（`middleware/session_ratelimit.go`）|
| L0 全局并发 trial | service 业务层（RunTrial / ReopenTrial 前）| 新增（`courtroom/concurrency.go`）|

---

## 4. 关键设计

### 4.1 L1 Per-Session action 限流

```go
// middleware/session_ratelimit.go
package middleware

import (
    "net/http"
    "sync"
    "time"
    "github.com/gin-gonic/gin"
    "golang.org/x/time/rate"
)

type SessionConfig struct {
    RPS   float64
    Burst int
}

var DefaultSessionConfig = SessionConfig{
    RPS:   2,    // 1 session 1 秒最多 2 次 action
    Burst: 5,    // 但允许 1 秒内瞬时 5 次（F5 狂点最多 5 次连续）
}

type sessionLimiter struct {
    mu      sync.Mutex
    limiters map[string]*rate.Limiter
}

func SessionRateLimit(cfg SessionConfig) gin.HandlerFunc {
    if cfg.RPS <= 0 { cfg = DefaultSessionConfig }
    sl := &sessionLimiter{limiters: make(map[string]*rate.Limiter)}

    return func(c *gin.Context) {
        sessionUUID := c.Param("session_uuid")
        if sessionUUID == "" {
            c.Next()
            return
        }

        sl.mu.Lock()
        lim, ok := sl.limiters[sessionUUID]
        if !ok {
            lim = rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst)
            sl.limiters[sessionUUID] = lim
        }
        sl.mu.Unlock()

        if !lim.Allow() {
            c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
                "code":    1429,
                "message": "session action rate limit exceeded, please slow down",
                "session_uuid": sessionUUID,
            })
            return
        }
        c.Next()
    }
}
```

**接入方式**（route 级别）：

```go
// api/handler.go router setup
v1.POST("/courtrooms/:session_uuid/evidences",
    middleware.SessionRateLimit(middleware.DefaultSessionConfig),  // ← 新增
    h.SubmitEvidence)
v1.POST("/courtrooms/:session_uuid/actions",
    middleware.SessionRateLimit(middleware.DefaultSessionConfig),  // ← 新增
    h.ProcessUserAction)
```

### 4.2 L0 全局并发 trial 信号量

```go
// courtroom/concurrency.go
package courtroom

import (
    "errors"
    "sync/atomic"
)

var ErrConcurrencyLimitExceeded = errors.New("concurrent trial limit reached")

type ConcurrencyLimiter struct {
    max      int64
    current  atomic.Int64
}

func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
    if max <= 0 { max = 5 }
    return &ConcurrencyLimiter{max: int64(max)}
}

// TryAcquire 非阻塞尝试拿 slot;成功返回 true,失败返回 false
func (l *ConcurrencyLimiter) TryAcquire() bool {
    for {
        cur := l.current.Load()
        if cur >= l.max { return false }
        if l.current.CompareAndSwap(cur, cur+1) { return true }
    }
}

func (l *ConcurrencyLimiter) Release() {
    l.current.Add(-1)
}

func (l *ConcurrencyLimiter) Stats() (current, max int64) {
    return l.current.Load(), l.max
}
```

**接入方式**（service 业务层）：

```go
// courtroom/service.go (伪代码, 实际改 StartTrial / ReopenTrial)
func (s *Service) StartTrial(...) error {
    if !s.concurrencyLimiter.TryAcquire() {
        return courtroom.ErrConcurrencyLimitExceeded
    }
    defer s.concurrencyLimiter.Release()
    // ... 原有 StartTrial 逻辑
}
```

### 4.3 配置项

```go
// backend/internal/config/config.go 加 4 个配置
type RateLimitConfig struct {
    // L1 Per-Session action 限流
    SessionActionRPS    float64 `mapstructure:"RATE_LIMIT_SESSION_ACTION_RPS"`     // 默认 2
    SessionActionBurst  int     `mapstructure:"RATE_LIMIT_SESSION_ACTION_BURST"`  // 默认 5

    // L0 全局并发 trial 信号量
    MaxConcurrentTrials int     `mapstructure:"RATE_LIMIT_MAX_CONCURRENT_TRIALS"`  // 默认 5
}
```

`.env.example` 同步：

```bash
# v0.10.20 (ADR 0027) 限流配置
RATE_LIMIT_SESSION_ACTION_RPS=2
RATE_LIMIT_SESSION_ACTION_BURST=5
RATE_LIMIT_MAX_CONCURRENT_TRIALS=5
```

### 4.4 可观测性（4 个新 metric）

```go
// observability/metrics.go 加 4 个 counter/gauge
session_rate_limit_rejected_total       // counter: L1 拒绝次数
global_concurrency_rejected_total       // counter: L0 拒绝次数
global_concurrency_current              // gauge:   当前 active trials
global_concurrency_max                  // gauge:   max 配置值
```

接入 Prometheus exporter 后可在 Grafana 看 dashboard。

---

## 5. 测试计划

### 5.1 L1 Per-Session 单测

| Case | 验证 |
|---|---|
| `TestSessionRateLimit_BasicAllowDeny` | limit=2/s，前 2 次通过，第 3 次拒绝 |
| `TestSessionRateLimit_BurstThenRefill` | burst=5，瞬时 5 次通过；6 次拒绝；空闲 1s 后恢复 |
| `TestSessionRateLimit_DifferentSessionsIndependent` | session A 满不影响 session B |
| `TestSessionRateLimit_ConcurrentSafe` | 50 goroutine 并发，race detector 不报警 |
| `TestSessionRateLimit_EmptySessionUUID` | session_uuid 为空时不拦截（兜底）|

### 5.2 L0 Concurrency 单测

| Case | 验证 |
|---|---|
| `TestConcurrencyLimiter_BasicAcquireRelease` | max=2，前 2 次 TryAcquire 返回 true，第 3 次 false；Release 1 次后可再拿到 |
| `TestConcurrencyLimiter_ConcurrentSafe` | 100 goroutine × 1 acquire/release，最终 current=0（无泄漏）|
| `TestConcurrencyLimiter_Stats` | current/max 正确 |
| `TestConcurrencyLimiter_ZeroMax` | max=0 → 用默认 5 |

### 5.3 集成测

```go
// courtroom/integration_concurrency_test.go
func TestService_StartTrial_RespectsConcurrencyLimit(t *testing.T) {
    // 启动 5 个 trial 成功,第 6 个返回 ErrConcurrencyLimitExceeded
    // Release 1 个后第 6 个 trial 启动成功
}
```

### 5.4 压测（可选）

用 `vegeta` 或 `hey` 跑：
- 单 IP 100 RPS × 10s → 验证 L3 拒绝率 80%+
- 单 user 10 trial/秒 × 60s → 验证 L2 拒绝率约 95%
- 单 session 20 action/秒 × 60s → 验证 L1 拒绝率 80%+
- 6 个并发 trial → 验证 L0 第 6 个被拒

---

## 6. 决策汇总

| 子项 | 改动文件 | 改动量 | 测试数 |
|---|---|---|---|
| L1 Per-Session middleware | `backend/internal/middleware/session_ratelimit.go`（新）+ `session_ratelimit_test.go` | ~120 行 | 5 |
| L0 Concurrency limiter | `backend/internal/courtroom/concurrency.go`（新）+ `concurrency_test.go` | ~80 行 | 4 |
| 配置项 | `backend/internal/config/config.go` + `.env.example` | +3 字段 | 0 |
| middleware 接入 | `backend/internal/api/handler.go` router setup | +2 行 | 1 |
| service 接入 | `backend/internal/courtroom/service.go` StartTrial/ReopenTrial | +6 行 | 1 (集成) |
| 4 个 metric | `backend/internal/observability/metrics.go` | +4 字段 | 0 |
| **合计** |  | **~215 行** | **11 项测试** |

---

## 7. 后果

### 7.1 收益

- ✅ **L0 防 ECS OOM**：最多 5 个 trial 同时跑，资源硬上限
- ✅ **L1 防 LLM 配额失控**：单 session 1 秒最多 2 次 action，烧 LLM 上限 2 N/s
- ✅ **L1 防 F5 狂点**：用户体验也更好（看不到失败 toast 多次）
- ✅ **可观测**：4 个新 metric + Grafana dashboard
- ✅ **架构可扩展**：未来多实例部署 + Redis backend 切换零成本

### 7.2 代价

- ⚠️ L1 默认值 RPS=2 / Burst=5 需要根据真实流量调优（10 用户跑 1 周才能定型）
- ⚠️ L0=5 trial 并发可能在高峰时段不够（可调到 10，但 2C2G 顶不住 10 个 trial 同时跑 LLM）
- ⚠️ 4 层限流会让初次部署调试复杂（用户报"请求被拒"时可能不知道是哪一层拒的）—— 解决方案：每个 429 响应带 `code`（如 1429=IP / 1428=session / 1427=concurrency），前端 ErrorBus 按 code 区分

### 7.3 不在本 ADR 范围

- ❌ Redis backend 实装（DAU > 5000 再说）
- ❌ 4 层限流 dashboard / Grafana JSON（Phase C Prometheus 时再做）
- ❌ Per-Agent breaker（prosecutor breaker 打开时 defender 不受影响）—— 留到 v0.11+

---

## 7A. 实施后决策修正（2026-07-12 补记）

实施 v0.10.20 时发现 3 处与原设计差异，记录如下：

### 7A.1 L0 接入位置：handler 层 TryAcquire 已撤销

**原设计**（§3.3 接入点）：
```
L0 作用于 Service.StartTrial / Service.ReopenTrial 入口
```

**实施时**：
- 第一版在 `handler.StartTrial` 入口直接 TryAcquire（用户早期返回 429）
- 集成测试 `TestService_WithConcurrencyLimiter_Limits` 失败：
  ```
  expected current=1 (handler 占的还在), got current=2 (handler + service 各占 1)
  ```

**根因**：handler 层 TryAcquire 占 1 slot + service.withCancel 也 TryAcquire 占 1 slot = **同 1 个 trial 占 2 个 slot**。max=5 实际只能开 2-3 个 trial，严重违反 L0 语义。

**修复**：撤销 handler 层 TryAcquire，**L0 只在 `service.withCancel` 一处管理**。

**UX 影响**：
- handler.StartTrial HTTP 200 返回（"trial 已开始"）
- 后台 goroutine 内 `RunOpeningSpeeches → withCancel` 失败
- `service.RunOpeningSpeeches` 返回 `ErrConcurrencyLimitExceeded`
- handler goroutine 已有 `if err != nil { broadcastUserFacingError }` 路径
- 前端通过 WS 收到 `CodeConcurrentTrialLimit` → 显示"系统繁忙"Toast

**设计原则**：
- **HTTP 层限流（防单用户滥用）必须快路径立即拒绝**（L1）
- **业务层限流（防全局资源耗尽）发现时往往 trial 已开始，必须走业务级错误通道**（L0）

**代码记录**：[`backend/internal/api/handler.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L358-L372) 仅注释（无 TryAcquire），[`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go) `withCancel` 内 TryAcquire。

### 7A.2 Release 行为：select default 非阻塞（不是 `<-l.sem` 阻塞）

**原设计**（§4.2）：
```go
func (l *ConcurrencyLimiter) Release() {
    <-l.sem
}
```

**实施时**：
- 单元测试 `TestConcurrencyLimiter_NoAcquireRelease_Panics`（预期 panic）实际 hang 30s 超时
- 根因：空 channel 上 `<-l.sem` 是 receive 操作，**永久阻塞**

**修复**：Release 改为 `select default` 非阻塞 + log warning：
```go
func (l *ConcurrencyLimiter) Release() {
    select {
    case <-l.sem:
        // 正常释放
    default:
        slog.Warn("ConcurrencyLimiter.Release without matching TryAcquire")
    }
}
```

**为什么宽容而非 panic**：
- 生产环境优先避免进程卡死 30s
- 一个 bug（defer Release 多了、Release 没匹配 Acquire）不应让整个 service 卡死
- log warning 已足够暴露问题（监控告警捕获）

### 7A.3 Metric 接入：仅加常量，未在 service 写 IncCounter/SetGauge

**原设计**（§4.4）：
```
4 个 metric：session_rate_limit_rejected_total / global_concurrency_rejected_total / global_concurrency_current / global_concurrency_max
```

**实施时**：
- ✅ 4 个 metric 名常量加到 `observability/metrics.go`
- ❌ service.withCancel 失败时没调 `metrics.IncCounter(MetricGlobalConcurrencyRejectedTotal)`
- ❌ service 拿 ConcurrencyStats 时没调 `metrics.SetGauge(MetricGlobalConcurrencyCurrent)`

**原因**：v0.10.20 时间紧张（CI 反复失败），先把限流器接入 + 测试做扎实，metric 写入留到后续 PR。

**影响**：/metrics 端点看不到这 4 个 metric 的实时值。修复方法（未实装）：
```go
// service.withCancel 失败时
s.metrics.IncCounter(observability.MetricGlobalConcurrencyRejectedTotal, nil)

// service.withCancel 成功时
cur, _, _ := s.concurrencyLimiter.Stats()
s.metrics.SetGauge(observability.MetricGlobalConcurrencyCurrent, nil, float64(cur))
s.metrics.SetGauge(observability.MetricGlobalConcurrencyMax, nil, float64(max))
```

**追踪**：见 [interview/12 §13 后续工作](file:///d:/源码/FullStack/DecisionCourt/docs/interview/12-rate-limit-defense-in-depth.md)。

### 7A.4 ADR 0027 vs 实际实施的最终对照

| 项 | ADR 原文 | 实际实施 | 差异原因 |
|---|---|---|---|
| L0 接入位置 | "Service.StartTrial / Service.ReopenTrial 入口" | **service.withCancel 一处** | handler + service 双重 acquire bug 教训后调整 |
| Release 行为 | "defer Release 必须配对" | **select default 非阻塞 + log warning** | 防进程被空 channel 卡死 30s |
| Metric | "4 个新 metric" | **加 4 个 metric 名常量，未接入 service** | v0.10.20 时间紧张，metric 写入留后续 PR |
| handler.SessionRateLimit 字段命名 | ADR 没说 | **新增 handler.SessionRateLimit 字段** | L1 独立字段（与 L3 IP 限流区分） |

---

## 8. 实施步骤

```
PR 1: L1 Per-Session middleware + 5 单测 + 1 集成测 + middleware 接入
PR 2: L0 Concurrency limiter + 4 单测 + 1 集成测 + service 接入
PR 3: 4 个 metric + config 字段 + .env.example 同步
PR 4: 文档同步（ADR 0027 + interview/12 + tech-spec + release-notes）
```

每个 PR 独立可 revert。

---

## 9. 关联

- **ADR 0014**：L2 Per-User Trial 已实装，本 ADR 补齐 L0 + L1 + 升级 L2 的可插拔 backend
- **ADR 0012 §决策 5**：启动扫描 + 限并发 ≤ 5 是本 ADR L0 的姊妹（启动恢复是"过去的事"，L0 是"现在的事"）
- **ADR 0013**：LLM Gateway 的 per-call timeout 是"单次请求"维度限流，本 ADR 是"频率 / 并发"维度限流，两者互补
- **代码**：所有现有 rate limit 文件 + 4 个新文件
- **测试**：11 项新单测 + 1 项集成测
- **配套 interview 文档**：[interview/12-rate-limit-defense-in-depth.md](../interview/12-rate-limit-defense-in-depth.md)
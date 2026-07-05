# ADR 0014: v0.9 用户级 Trial 限流（每用户每天 N 次）

> **状态**：✅ Accepted（2026-07-04 决策）
> **决策日期**：2026-07-04
> **影响范围**：`backend/internal/ratelimit/`（新包）+ `backend/internal/api/handler.go` 接入 + `backend/internal/config/config.go` 配置
> **关联 ADR**：[0012 §决策 6](./0012-ha-and-concurrency.md)（"用户级限流"规划，本 ADR 落地）+ [0013](./0013-llm-gateway-engineering.md)（LLM Gateway 工程化）

---

## 背景

DecisionCourt v0.9 计划部署到阿里云单 ECS（2C2G，¥56/月）面向公众测试使用（PRD §14 已确认）。但当前 backend 没有任何用户级限流，存在 3 个 P0 风险：

### 风险 1：恶意脚本刷 trial
- 单用户可写脚本每秒点 50 次 "StartTrial"
- 每次 trial = 5 个 agent × 10 次 LLM 调用 = 50 次 DeepSeek API 调用
- 50 trials/秒 × 50 调用 = **2500 次 LLM/秒**，1 小时烧光免费额度（DeepSeek 免费版 60 RPM）

### 风险 2：LLM 配额失控
- 阿里云单 ECS 部署后无运维值班
- 配额异常需第二天人工看账单才能发现

### 风险 3：用户体验不公
- 一个用户独占 trial 资源，其他用户排队
- 阿里云 2C2G 资源紧张时优先让"早用的人"挤掉"新人"

### 现有保护不足
- [v0.8.3 安全加固 §7](./decisioncourt-tech-spec.md) 有 IP 级 fail2ban（防爆破），但**没有用户级业务限流**
- [ADR 0013 §决策 1](./0013-llm-gateway-engineering.md) 有 per-call timeout（防止 hang），但**没有请求级频率控制**

---

## 决策

| 维度 | A. sync.Map 内存计数 | B. Redis 滑动窗口 | C. 数据库计数（每日重置） |
|---|---|---|---|
| 改动量 | ~80 行 | +Redis 部署运维 | +DB schema + 每日 cron |
| 进程重启影响 | 计数清空 | 持久化 | 持久化 |
| 复杂度 | 低 | 中 | 高 |
| 简历价值 | "sync.Map 设计 + 滑动窗口" | "Redis cache aside"（更常见） | "DB 事务 + 每日重置" |
| 单机部署适配度 | **完美** | 杀鸡用牛刀 | 杀鸡用牛刀 |

**决策**：**方案 A**。sync.Map + 滑动窗口实现。预留 Redis interface，DAU > 5000 触发切换。

---

## 限流规则

### 配额
- **每用户每天（UTC）最多 N 次 StartTrial**
- **默认 N = 5**（测试阶段保守值；生产可调到 20，v0.10 上线再调）
- **续 cross-exam / submit evidence 不计配额**（用户已经在 trial 内）
- **失败 trial 计配额**（防"失败重试绕过限流"）

### 时间窗
- **UTC 日界**（每日 00:00:00 重置）—— 简单可预测，避免时区问题
- 滑动窗口长度 = 24 小时
- 内存中保留 7 天历史（防 burst），过期 entry 自动清理

### 触发位置
- **`POST /courtrooms/:session_uuid/start`** —— StartTrial 入口前
- **其他端点不限流**（get/export/continue 都是 trial 内的合法操作）

### 错误响应
```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 3600

{
  "error": "rate_limit_exceeded",
  "message": "已达今日 trial 配额上限（5 次）。明天 00:00 UTC 重置。",
  "limit": 5,
  "used": 5,
  "resets_at": "2026-07-05T00:00:00Z"
}
```

---

## 关键设计

### 滑动窗口算法
```go
type userCounter struct {
    timestamps []int64  // unix nano, sorted
}

func (c *userCounter) allow(now int64, window time.Duration, limit int) (bool, int64) {
    // 1. 清掉 window 外的
    cutoff := now - window.Nanoseconds()
    i := 0
    for ; i < len(c.timestamps); i++ {
        if c.timestamps[i] > cutoff { break }
    }
    c.timestamps = c.timestamps[i:]
    
    // 2. 检查是否超限
    if len(c.timestamps) >= limit {
        oldest := c.timestamps[0]
        retryAfter := time.Duration(oldest+window.Nanoseconds()-now) * time.Nanosecond
        return false, retryAfter
    }
    
    // 3. 记录本次
    c.timestamps = append(c.timestamps, now)
    return true, 0
}
```

### 后台清理
- 启动时跑一次清理（清 7 天前 entry）
- 每 1 小时跑一次清理 goroutine（防止内存膨胀）

### 接口预留（DAU > 5000 切换）
```go
type RateLimiter interface {
    Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)
}

// 默认实现
type MemoryRateLimiter struct { ... }

// 未来 Redis 实现（DAU > 5000 时切换）
// type RedisRateLimiter struct { client *redis.Client }
```

---

## 决策汇总

| 决策 | 改动文件 | 改动量 | 测试数 |
|---|---|---|---|
| sync.Map RateLimiter 实现 | `backend/internal/ratelimit/` 新包 | ~120 行 | 4 |
| 接入 StartTrial | `backend/internal/api/handler.go` | ~25 行 | 1 |
| 配置项 + viper.SetDefault | `backend/internal/config/config.go` | +3 字段 | 0 |
| **合计** |  | **~150 行** | **5 项测试** |

---

## 后果

### 收益
- ✅ **防止 LLM 配额被刷光** —— 单用户最多 5 trial/天 = 5 × 50 = 250 LLM 调用上限
- ✅ **公平分配资源** —— 每用户都有机会
- ✅ **可视化计数** —— 429 响应告诉用户"还剩多少"
- ✅ **可插拔** —— `RateLimiter` interface 预留 Redis 实现

### 代价
- ⚠️ sync.Map 单机部署，DAU > 5000 需切换 Redis（届时重构 RateLimiter 实现即可，业务代码不动）
- ⚠️ 进程重启计数清空（可接受：用户多刷 5 次 = 多烧 250 LLM = 不致命）
- ⚠️ 测试阶段 N=5 偏紧，可能误伤正常用户（生产可调到 20）

### 风险
- 🟡 **时区误判**：UTC 日界对中国用户不友好（中国已是 8:00 UTC = 16:00 北京），可改为本地时区 → 决定 UTC，简化逻辑

---

## 关联

- 主文档：[docs/decisioncourt-prd.md §14](../decisioncourt-prd.md)（"测试阶段"已确认）
- 代码：
  - 新增 `backend/internal/ratelimit/ratelimit.go`
  - 修改 [backend/internal/api/handler.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go)
  - 修改 [backend/internal/config/config.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/config/config.go)
- 测试：新增 `backend/internal/ratelimit/ratelimit_test.go` + `backend/internal/api/handler_ratelimit_test.go`
- 关联 ADR：[0012 §决策 6](./0012-ha-and-concurrency.md) · [0013](./0013-llm-gateway-engineering.md)
package middleware

// v0.10.20 (ADR 0027 §决策 3) L1 Per-Session action 限流中间件。
//
// 业务背景:
//   - L3 Per-IP (本文件同目录 ratelimit.go) 已经按 IP 防 DDoS, 20 RPS + burst 50
//   - L2 Per-User Trial 限流 (internal/ratelimit) 已经按 user 限 trial 数 (5/24h)
//   - **L1 Per-Session 限流缺失** → 用户在 1 个 session 内 F5 狂点 action (continue_cross_exam / submit_evidence),
//     1 秒内可发 30 次 action, 触发 session lock 排队, 把正常用户拖慢 30 倍
//
// 解决方案: 每个 session 独立的 rate.Limiter, 令牌桶 RPS=2 / Burst=5
//   - RPS=2: 1 session 1 秒最多 2 次 action (持续高频拒)
//   - Burst=5: 允许用户 F5 狂点 5 次连续 (UX 友好)
//
// 与 L3 Per-IP 的关系:
//   - L3 是 IP 维度全局限流 (防 DDoS)
//   - L1 是 session 维度细粒度限流 (防 F5 狂点)
//   - 两者串联: 先 L3 通过 → 进 L1 → 进 service 层
//
// 接入方式 (handler.go RegisterAPIRoutes):
//   llmGroup := api.Group("/")
//   llmGroup.Use(h.LLMRateLimit)          // LLM 端点限流 5 req/s/user
//   llmGroup.Use(h.SessionRateLimit)      // L1 Per-Session 2 req/s/session  ← 新增
//   llmGroup.POST("/courtrooms/:session_uuid/evidences", h.SubmitEvidence)
//   llmGroup.POST("/courtrooms/:session_uuid/actions", h.UserAction)

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// SessionConfig 描述 session 限流中间件的配置。
//
//   - RPS: 每秒允许的请求数 (token bucket refill rate)
//   - Burst: token bucket 容量 (瞬时可超过 RPS 的量, 即 F5 狂点最大允许次数)
//   - OnReject: 拒绝时调用的回调 (nil-safe, 用于记录 metric / 写 audit log)。
//     典型用法: main.go 注入 metrics.IncCounter(MetricSessionRateLimitRejectedTotal)
//     解耦设计: middleware 包不直接依赖 observability 包, 由调用方决定如何处理。
type SessionConfig struct {
	RPS      float64
	Burst    int
	OnReject func()
}

// DefaultSessionConfig: RPS=2 + burst=5。按 ADR 0027 §6.1 决策, 经用户 2026-07-12 确认。
//   - RPS=2: 持续高频拒 (1 秒最多 2 次, 防脚本)
//   - Burst=5: 允许 F5 狂点 5 次连续 (UX 友好)
var DefaultSessionConfig = SessionConfig{
	RPS:   2,
	Burst: 5,
}

// sessionLimiter 维护一个 map[sessionUUID]*rate.Limiter + 定期清理。
// 与通用 RateLimit 不同, 这里 key 永远是 session UUID, 不需要按 IP/user 区分。
type sessionLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	cfg      SessionConfig
}

func newSessionLimiter(cfg SessionConfig) *sessionLimiter {
	return &sessionLimiter{
		limiters: make(map[string]*rate.Limiter),
		cfg:      cfg,
	}
}

// get 拿 session 的 rate.Limiter, 没有就新建。线程安全。
func (sl *sessionLimiter) get(sessionUUID string) *rate.Limiter {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if lim, ok := sl.limiters[sessionUUID]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Limit(sl.cfg.RPS), sl.cfg.Burst)
	sl.limiters[sessionUUID] = lim
	return lim
}

// SessionRateLimit 返回一个 gin 中间件, 按 session UUID 限流。
//
// 行为:
//   - 从 gin URL param "session_uuid" 取 session 标识
//   - 拿该 session 的 rate.Limiter, 检查 Allow()
//   - 不通过 → 429 (HTTP 429 + code=1427 + user_facing_error envelope)
//   - 通过 → c.Next()
//
// 兜底:
//   - session_uuid 为空 (路由配置错误) → 不拦截, 放行 (让上层逻辑处理)
//
// 拒绝时的响应格式 (与 v0.10.17 silent-error-fix 对齐):
//   {
//     "code": 1427,
//     "message": "session action rate limit exceeded, please slow down",
//     "session_uuid": "xxx",
//     "user_facing_error": {
//       "class": "transient",
//       "code": "SESSION_RATE_LIMITED",
//       "message": "操作太频繁, 请稍后再试",
//       "retry_after_seconds": 1
//     }
//   }
func SessionRateLimit(cfg SessionConfig) gin.HandlerFunc {
	if cfg.RPS <= 0 {
		cfg = DefaultSessionConfig
	}
	if cfg.Burst <= 0 {
		cfg.Burst = int(cfg.RPS * 2)
		if cfg.Burst < 1 {
			cfg.Burst = 1
		}
	}
	sl := newSessionLimiter(cfg)

	return func(c *gin.Context) {
		sessionUUID := c.Param("session_uuid")
		if sessionUUID == "" {
			// 兜底: 路由配置错误, 放行不拦截
			c.Next()
			return
		}

		lim := sl.get(sessionUUID)
		if !lim.Allow() {
			// v0.10.20 (ADR 0027 §4.4) 拒绝时调 OnReject 回调 (nil-safe)。
			// main.go 注入: cfg.OnReject = func() { metrics.IncCounter(MetricSessionRateLimitRejectedTotal, nil) }
			if cfg.OnReject != nil {
				cfg.OnReject()
			}
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":        1427,
				"message":     "session action rate limit exceeded, please slow down",
				"session_uuid": sessionUUID,
				"user_facing_error": gin.H{
					"class":                "transient",
					"code":                 "SESSION_RATE_LIMITED",
					"message":              "操作太频繁, 请稍后再试",
					"retry_after_seconds":  1,
				},
			})
			return
		}
		c.Next()
	}
}
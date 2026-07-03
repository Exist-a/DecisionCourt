// Package middleware 提供 gin 中间件集合(限流等)。
//
// v0.8.3 安全(P1-2):
//   - RateLimit: 按 IP 限流(token bucket 算法,golang.org/x/time/rate)
//   - 限流维度:IP(默认)或 user(viewer from ctx)
//   - 限流数字可由 env 覆盖(给生产调参)
//
// 设计权衡:
//   - 单进程内存限流(token-per-IP map),重启会丢计数——可接受(攻击者也得从 0 开始)
//   - 不接 Redis 限流:本项目单机部署,无需分布式协调
//   - LLM 类端点(/evidences /actions)用更严的限流,由调用方决定
package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Config 描述一个限流中间件的配置。
//
//   - RPS:每秒允许的请求数(token bucket refill rate)
//   - Burst:token bucket 容量(瞬时可超过 RPS 的量)
//   - By:"ip"(默认)或 "user"
//   - Cleanup:每多少分钟清一次过期的 limit(避免内存泄漏)
type Config struct {
	RPS     float64
	Burst   int
	By      string // "ip" or "user"
	Cleanup time.Duration
}

// DefaultConfig:20 req/s + burst 50 + 按 IP。够普通用户,不限制突然的页面加载。
var DefaultConfig = Config{
	RPS:     20,
	Burst:   50,
	By:      "ip",
	Cleanup: 5 * time.Minute,
}

// LLMConfig:5 req/s + burst 10。防"一秒 1000 次 dispatch_investigator"烧配额。
var LLMConfig = Config{
	RPS:     5,
	Burst:   10,
	By:      "user", // 按 user 限,防止单用户多 tab 合并打爆
	Cleanup: 5 * time.Minute,
}

// limiterEntry 保存一个 limit + 最后一次访问时间(用于清理)。
type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// rateLimiter 维护一个 map[key]*limiterEntry + 定期清理。
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	cfg     Config
}

func newRateLimiter(cfg Config) *rateLimiter {
	rl := &rateLimiter{
		entries: make(map[string]*limiterEntry),
		cfg:     cfg,
	}
	if cfg.Cleanup > 0 {
		go rl.cleanupLoop()
	}
	return rl
}

func (rl *rateLimiter) cleanupLoop() {
	t := time.NewTicker(rl.cfg.Cleanup)
	defer t.Stop()
	for range t.C {
		rl.mu.Lock()
		now := time.Now()
		for k, e := range rl.entries {
			if now.Sub(e.lastSeen) > rl.cfg.Cleanup*2 {
				delete(rl.entries, k)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) get(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if e, ok := rl.entries[key]; ok {
		e.lastSeen = time.Now()
		return e.lim
	}
	lim := rate.NewLimiter(rate.Limit(rl.cfg.RPS), rl.cfg.Burst)
	rl.entries[key] = &limiterEntry{lim: lim, lastSeen: time.Now()}
	return lim
}

// RateLimit 返回一个 gin 中间件,按 IP 或 user 限流。
//
// 优先级链 /api/v1/courtrooms/:session_uuid/evidences 这种贵资源端点,
// 在路由层叠 LLMConfig 即可(更严的 limit 在前置中间件)。
func RateLimit(cfg Config) gin.HandlerFunc {
	if cfg.RPS <= 0 {
		cfg = DefaultConfig
	}
	if cfg.Burst <= 0 {
		cfg.Burst = int(cfg.RPS * 2)
	}
	rl := newRateLimiter(cfg)

	keyFn := func(c *gin.Context) string {
		switch cfg.By {
		case "user":
			if u := auth.ViewerFromContext(c); u != "" {
				return "u:" + u
			}
		}
		return "ip:" + c.ClientIP()
	}

	return func(c *gin.Context) {
		key := keyFn(c)
		lim := rl.get(key)
		if !lim.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    1429,
				"message": "rate limit exceeded, please slow down",
			})
			return
		}
		c.Next()
	}
}

package middleware

// v0.10.20 (ADR 0027 §5.1) L1 Per-Session 限流测试。
//
// 验证 5 个核心契约:
//   1. 基础限流: 前 N 次通过, 第 N+1 次拒绝
//   2. 突发 + 恢复: burst 期内允许 N 个, 空闲后 token 恢复
//   3. 不同 session 隔离: session A 满不影响 session B
//   4. 并发安全: 50 goroutine 并发, race detector 不报警
//   5. 空 session_uuid 兜底: 路由配置错误时放行, 不拦截

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	// 静默 gin debug 日志, 测试输出更干净
	gin.SetMode(gin.TestMode)
}

// newTestRouter 构造一个 gin engine + 1 个 mock handler, 用于测试 middleware 行为。
// session_uuid 通过路径参数传入, 模拟真实路由 /courtrooms/:session_uuid/...
func newTestRouter(cfg SessionConfig) *gin.Engine {
	r := gin.New()
	r.POST("/test/:session_uuid/action", SessionRateLimit(cfg), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	// 兜底路由: session_uuid 不在路径里, 模拟路由配置错误场景
	r.POST("/test/no-session/action", SessionRateLimit(cfg), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// TestSessionRateLimit_BasicAllowDeny 验证: RPS=2, 前 2 次 allowed,
// 第 3 次 allowed=false (token 用完, refill 没跟上)。
// 这是限流器最基础的契约。
func TestSessionRateLimit_BasicAllowDeny(t *testing.T) {
	t.Parallel()

	// RPS=2 (slow refill), Burst=2 — 让 token 耗尽后短时间内难以恢复
	r := newTestRouter(SessionConfig{RPS: 2, Burst: 2})

	// 前 2 次: 消耗 burst token → 通过
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("call %d: expected 200, got %d (body=%s)", i, w.Code, w.Body.String())
		}
	}

	// 第 3 次: token 已耗尽, refill 在 1/2s 内, 立即再发会拒
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("call 3: expected 429, got %d (body=%s)", w.Code, w.Body.String())
	}

	// 验证响应格式 (与 v0.10.17 silent-error-fix 对齐)
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v (body=%s)", err, w.Body.String())
	}
	if code, _ := resp["code"].(float64); code != 1427 {
		t.Errorf("expected code=1427 (session rate limit), got %v", resp["code"])
	}
	// 验证 user_facing_error envelope (v0.10.17 新增, 前端 ErrorBus 用)
	ufErr, ok := resp["user_facing_error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected user_facing_error envelope, got %v", resp["user_facing_error"])
	}
	if cls, _ := ufErr["class"].(string); cls != "transient" {
		t.Errorf("expected class=transient, got %v", ufErr["class"])
	}
	if code, _ := ufErr["code"].(string); code != "SESSION_RATE_LIMITED" {
		t.Errorf("expected code=SESSION_RATE_LIMITED, got %v", ufErr["code"])
	}
}

// TestSessionRateLimit_BurstThenRefill 验证: Burst=5, 瞬时 5 次全部通过,
// 第 6 次拒绝; 空闲 1 秒后 token 恢复, 再次允许。
// 这是令牌桶 vs 漏桶的关键差异 (允许突发)。
func TestSessionRateLimit_BurstThenRefill(t *testing.T) {
	t.Parallel()

	// RPS=2 (slow refill), Burst=5 — 5 个 token 瞬时可用, 第 6 个要等
	r := newTestRouter(SessionConfig{RPS: 2, Burst: 5})

	// 瞬时 5 次: 全通过
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("burst call %d: expected 200, got %d", i, w.Code)
		}
	}

	// 第 6 次: token 已耗尽, 立即拒绝
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("call 6: expected 429, got %d (body=%s)", w.Code, w.Body.String())
	}

	// 等 1 秒: RPS=2 意味着 1 秒加 2 个 token, 应该足够通过 1 次
	time.Sleep(1100 * time.Millisecond)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("after 1s wait: expected 200 (token refilled), got %d (body=%s)",
			w.Code, w.Body.String())
	}
}

// TestSessionRateLimit_DifferentSessionsIndependent 验证: session A 用完 token
// 不影响 session B。这是限流器"按 session 隔离"的契约。
func TestSessionRateLimit_DifferentSessionsIndependent(t *testing.T) {
	t.Parallel()

	r := newTestRouter(SessionConfig{RPS: 2, Burst: 2})

	// session-A 用满 2 个 token
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("session-A call %d: expected 200, got %d", i, w.Code)
		}
	}

	// session-A 第 3 次: 拒绝
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("session-A call 3: expected 429, got %d", w.Code)
	}

	// session-B: 不受 session-A 影响, 仍可通过
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test/session-B/action", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("session-B call %d: expected 200 (different from session-A), got %d",
				i, w.Code)
		}
	}
}

// TestSessionRateLimit_ConcurrentSafe 验证: 50 goroutine 并发对同一 session 发请求,
// 用 -race 跑这个测试, 任何 data race 都会被 catch。
// 这个测试主要验证 sync.Mutex 保护 map[string]*rate.Limiter 没有 race。
// burst=500 是为了"全部通过", 让测试聚焦在并发安全而非限流能力。
func TestSessionRateLimit_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	r := newTestRouter(SessionConfig{RPS: 1000, Burst: 500}) // burst 大于总请求数, 全通过

	const goroutines = 50
	const callsPerGoroutine = 10

	var okCount int64
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/test/session-X/action", nil)
				r.ServeHTTP(w, req)
				if w.Code == http.StatusOK {
					atomic.AddInt64(&okCount, 1)
				}
			}
		}()
	}
	wg.Wait()

	// RPS=1000 + Burst=500 → 所有 500 次都能通过 (burst 大于总请求数)
	if got := atomic.LoadInt64(&okCount); got != 500 {
		t.Errorf("expected 500 ok (RPS=1000, Burst=500), got %d", got)
	}
}

// TestSessionRateLimit_EmptySessionUUID 验证: session_uuid 为空时 (路由配置错误),
// 中间件放行, 不拦截。这是兜底, 让上层逻辑处理 (而不是 500)。
func TestSessionRateLimit_EmptySessionUUID(t *testing.T) {
	t.Parallel()

	r := newTestRouter(SessionConfig{RPS: 1, Burst: 1}) // 即使限流很严, 空 UUID 也不应触发

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test/no-session/action", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("empty session_uuid call %d: expected 200 (fallback), got %d",
				i, w.Code)
		}
	}
}

// TestSessionRateLimit_DefaultConfig 验证: cfg.RPS <= 0 时用默认配置。
// 这是 "配置缺失 → 默认值兜底" 契约。
func TestSessionRateLimit_DefaultConfig(t *testing.T) {
	t.Parallel()

	// RPS=0 → 用 DefaultSessionConfig (RPS=2, Burst=5)
	cfg := SessionConfig{RPS: 0, Burst: 0}
	middleware := SessionRateLimit(cfg)

	if middleware == nil {
		t.Fatal("expected non-nil middleware")
	}

	// 实测默认值生效: 用 DefaultSessionConfig 的 RPS/Burst
	// burst=5 瞬时通过 5 次, 第 6 次拒
	r := gin.New()
	r.POST("/test/:session_uuid/action", middleware, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("default config call %d: expected 200, got %d", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/session-A/action", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("default config call 6: expected 429 (burst exhausted), got %d", w.Code)
	}
}
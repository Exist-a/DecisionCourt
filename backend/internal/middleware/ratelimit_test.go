package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestRateLimit_AllowsBelowThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(Config{RPS: 10, Burst: 5, By: "ip"}))
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}
}

func TestRateLimit_RejectsAboveThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(Config{RPS: 1, Burst: 2, By: "ip"}))
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	// 头 2 个 OK(burst),第 3 个 429(rate = 1/s)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}
	req := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code, "expected 429 after burst exhausted")
}

// v2.11 (deferred D11): per-IP 层拒绝必须带 Retry-After 响应头 + 正整数的
// retry_after_seconds，让被限流的客户端有明确退避依据。此前该层只有
// 429 + code 1429，与已有 Retry-After 的试用配额层契约不一致。
func TestRateLimit_429HasRetryAfterHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(Config{RPS: 1, Burst: 1, By: "ip"}))
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	// burst=1 → 第 1 个 200，第 2 个 429
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))

	require.Equal(t, 429, w.Code, "expected 429 after burst exhausted")

	hdr := w.Header().Get("Retry-After")
	require.NotEmpty(t, hdr, "429 必须带 Retry-After 头")
	secs, err := strconv.Atoi(hdr)
	require.NoError(t, err, "Retry-After 必须是整数秒, got %q", hdr)
	assert.Greater(t, secs, 0, "Retry-After 必须是正整数")

	// 响应体同样暴露 retry_after_seconds，与 header 一致；code 保持 1429。
	var body struct {
		Code              int `json:"code"`
		RetryAfterSeconds int `json:"retry_after_seconds"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 1429, body.Code, "拒绝码必须保持 1429(前端 ErrorBus 依赖)")
	assert.Equal(t, secs, body.RetryAfterSeconds)
}

// TestRetryAfterSeconds_EstimatesFromBucket 表驱动覆盖估算函数：
// 等待秒数按"缺口 token / refill rate"向上取整，且下限为 1s。
func TestRetryAfterSeconds_EstimatesFromBucket(t *testing.T) {
	cases := []struct {
		name    string
		rps     float64
		burst   int
		wantMin int
	}{
		{"0.1 rps 缺口 1 token => 至少 10s", 0.1, 1, 10},
		{"1 rps => 至少 1s", 1, 1, 1},
		{"100 rps 也至少 1s(不鼓励立即重试)", 100, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lim := rate.NewLimiter(rate.Limit(c.rps), c.burst)
			for lim.Allow() { // 耗尽桶
			}
			assert.GreaterOrEqual(t, retryAfterSeconds(lim), c.wantMin)
		})
	}

	assert.Equal(t, 1, retryAfterSeconds(nil), "nil limiter 兜底为 1s")
}

func TestRateLimit_RecoversAfterRefill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(Config{RPS: 100, Burst: 1, By: "ip"}))
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	// burst 1 → 第二个被拒
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	req2 := httptest.NewRequest("GET", "/x", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)

	// 10ms 后,RPS=100 应当 refill 至少 1 个 token
	time.Sleep(20 * time.Millisecond)
	req3 := httptest.NewRequest("GET", "/x", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code, "should refill after sleep")
}

func TestRateLimit_ByIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(Config{RPS: 100, Burst: 2, By: "ip"}))
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	// 不同 IP 互不影响
	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/x", nil)
			req.Header.Set("X-Forwarded-For", ip)
			req.RemoteAddr = ip + ":1234"
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, 200, w.Code, "ip=%s i=%d", ip, i)
		}
	}
}

func TestRateLimit_ConcurrentSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var ok, throttled int64
	// RPS=0.1 意味着 10s 才补 1 个 token;测试在 0.01s 内跑完,
	// burst=10 全部消耗后,后续 90 个都应被拒。
	r.Use(RateLimit(Config{RPS: 0.1, Burst: 10, By: "ip"}))
	r.GET("/x", func(c *gin.Context) {
		c.String(200, "ok")
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/x", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code == 200 {
				atomic.AddInt64(&ok, 1)
			} else {
				atomic.AddInt64(&throttled, 1)
			}
		}()
	}
	wg.Wait()
	okN := atomic.LoadInt64(&ok)
	thrN := atomic.LoadInt64(&throttled)
	// 100 个并发请求,burst=10 + 几乎无 refill;实际行为依赖 token bucket
	// 内部计时(测试运行时间会补充少量 token)。断言:总和不超 100,限流量>0。
	assert.Equal(t, int64(100), okN+thrN)
	assert.True(t, okN >= 10, "expected at least burst=10 ok, got ok=%d thr=%d", okN, thrN)
	assert.True(t, thrN > 0, "expected some throttled, got ok=%d thr=%d", okN, thrN)
}

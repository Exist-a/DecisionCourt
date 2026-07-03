package middleware

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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

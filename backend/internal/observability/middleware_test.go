package observability

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceMiddleware_GeneratesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TraceMiddleware())

	var observed Trace
	r.GET("/x", func(c *gin.Context) {
		observed = TraceFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.NotEmpty(t, observed.RequestID)
	// 长度与 uuidLike 输出一致（36）
	assert.Len(t, observed.RequestID, 36)
	assert.Equal(t, observed.RequestID, w.Header().Get("X-Request-ID"))
}

func TestTraceMiddleware_PrefersIncomingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TraceMiddleware())

	var observed Trace
	r.GET("/x", func(c *gin.Context) {
		observed = TraceFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-ID", "client-supplied-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, "client-supplied-123", observed.RequestID)
	assert.Equal(t, "client-supplied-123", w.Header().Get("X-Request-ID"))
}

func TestTraceMiddleware_ExtractsSessionUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TraceMiddleware())

	var observed Trace
	r.GET("/sessions/:session_uuid/x", func(c *gin.Context) {
		observed = TraceFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/sessions/abc-123/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, "abc-123", observed.SessionUUID)
}

func TestMetricsMiddleware_RecordsDuration(t *testing.T) {
	m := NewMetrics()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware(m))

	r.GET("/sessions/:session_uuid/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/err", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/sessions/abc/x", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
	req := httptest.NewRequest("GET", "/err", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	snap := m.Snapshot()
	require.Contains(t, snap.Histograms, MetricHTTPRequestDuration)
	// 找到 /sessions/:session_uuid/x + GET + 200 的样本
	var ok200, err500 *MetricSample
	for i, s := range snap.Histograms[MetricHTTPRequestDuration] {
		if s.Labels["path"] == "/sessions/:session_uuid/x" && s.Labels["status"] == strconv.Itoa(http.StatusOK) {
			ok200 = &snap.Histograms[MetricHTTPRequestDuration][i]
		}
		if s.Labels["path"] == "/err" && s.Labels["status"] == strconv.Itoa(http.StatusInternalServerError) {
			err500 = &snap.Histograms[MetricHTTPRequestDuration][i]
		}
	}
	require.NotNil(t, ok200)
	assert.Equal(t, int64(5), ok200.Count)
	require.NotNil(t, err500)
	assert.Equal(t, int64(1), err500.Count)
}

func TestMetricsMiddleware_NilMeterIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware(nil))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	// 不应 panic
	assert.NotPanics(t, func() { r.ServeHTTP(w, req) })
}

func TestRecoveryMiddleware_RecoversFromPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := NewMetrics()
	r := gin.New()
	r.Use(RecoveryMiddleware(m))
	r.GET("/panic", func(c *gin.Context) { panic("oops") })

	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	assert.NotPanics(t, func() { r.ServeHTTP(w, req) })
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	snap := m.Snapshot()
	require.Contains(t, snap.Counters, "http_panic_total")
	assert.Equal(t, float64(1), snap.Counters["http_panic_total"][0].Value)
}
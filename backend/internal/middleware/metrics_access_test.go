package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 2026-10-10 第 3 轮：/metrics 访问控制。四态都要钉住，否则很容易退化成
// "看起来配了 token、其实还是全公开"（或反过来把自己锁死）。
func newMetricsRouter(cfg MetricsAccessConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/metrics", MetricsAccess(cfg), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func doGet(r *gin.Engine, headers map[string]string) int {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestMetricsAccess_PublicAllowsWithoutCredentials(t *testing.T) {
	r := newMetricsRouter(MetricsAccessConfig{Public: true})
	require.Equal(t, http.StatusOK, doGet(r, nil))
}

func TestMetricsAccess_ClosedWithoutChannelReturns401(t *testing.T) {
	r := newMetricsRouter(MetricsAccessConfig{})
	require.Equal(t, http.StatusUnauthorized, doGet(r, nil), "无 Public 也无 Token 时必须 401，而不是静默放行")
}

func TestMetricsAccess_TokenAcceptsBearerAndXHeader(t *testing.T) {
	r := newMetricsRouter(MetricsAccessConfig{Token: "s3cr3t"})
	require.Equal(t, http.StatusOK, doGet(r, map[string]string{"Authorization": "Bearer s3cr3t"}))
	require.Equal(t, http.StatusOK, doGet(r, map[string]string{"X-Metrics-Token": "s3cr3t"}))
}

func TestMetricsAccess_TokenRejectsWrongOrMissing(t *testing.T) {
	r := newMetricsRouter(MetricsAccessConfig{Token: "s3cr3t"})
	require.Equal(t, http.StatusUnauthorized, doGet(r, nil))
	require.Equal(t, http.StatusUnauthorized, doGet(r, map[string]string{"Authorization": "Bearer wrong"}))
	require.Equal(t, http.StatusUnauthorized, doGet(r, map[string]string{"X-Metrics-Token": "wrong"}))
	// 常见错误：把 Basic / 裸 token 当 Bearer 传
	require.Equal(t, http.StatusUnauthorized, doGet(r, map[string]string{"Authorization": "s3cr3t"}))
}

// Token 与 Public 同时配置时，token 路径仍可用，public 仍然全开（两条通道独立）。
func TestMetricsAccess_TokenAndPublicCoexist(t *testing.T) {
	r := newMetricsRouter(MetricsAccessConfig{Public: true, Token: "s3cr3t"})
	require.Equal(t, http.StatusOK, doGet(r, nil))
	require.Equal(t, http.StatusOK, doGet(r, map[string]string{"X-Metrics-Token": "s3cr3t"}))
}

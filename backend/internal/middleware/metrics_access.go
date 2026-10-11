package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// MetricsAccessConfig 控制 `/metrics` 的可见性（2026-10-10 第 3 轮）。
type MetricsAccessConfig struct {
	// Public=true → 无需凭证直接可读（dev 便利 / 外部拨测）。
	Public bool
	// Token 非空时，带对凭证的请求放行（给 Prometheus scraper 的专用口子）。
	Token string
}

// MetricsAccess 保护 `/metrics` 端点。
//
// 背景（先说清"为什么不是简单塞进 auth 组"）：`/metrics` 的消费方是 **Prometheus scraper**，
// 它不会带用户 JWT。若直接挂 auth.Middleware，scraper 永远 401；若继续公开，等于把
// 调用量/错误率/预算水位暴露给任何人。所以给两条受控通道：
//
//	1) Bearer / X-Metrics-Token 匹配 OPS_METRICS_TOKEN → 放行（scraper 用）
//	2) OPS_METRICS_PUBLIC=true → 放行（dev / 拨测用）
//	否则 401（JSON，带可操作提示，不静默）。
//
// Token 用常数时间比较，避免时序侧信道。
func MetricsAccess(cfg MetricsAccessConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Public {
			c.Next()
			return
		}
		if cfg.Token != "" && tokenMatches(cfg.Token, extractToken(c)) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"code":    1401,
			"message": "metrics 端点需要凭证：设置 OPS_METRICS_TOKEN 并在请求头带 " +
				"Authorization: Bearer <token>（或 X-Metrics-Token），或设 OPS_METRICS_PUBLIC=true 开放",
		})
	}
}

// extractToken 从 Authorization: Bearer 或 X-Metrics-Token 取凭证。
func extractToken(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); h != "" {
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
	}
	return strings.TrimSpace(c.GetHeader("X-Metrics-Token"))
}

func tokenMatches(want, got string) bool {
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// TraceMiddleware 从 HTTP header 中提取或生成 trace_id，注入 ctx。
//
// 行为：
//   - 优先读 X-Request-ID header（前端可在请求时带，方便端到端关联）
//   - 缺失时生成新的 uuid 字符串
//   - 同时通过 c.Header("X-Request-ID", ...) 回写，方便前端 / curl 调试
//
// 用法：
//   r.Use(observability.TraceMiddleware())
func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Request-ID")
		if traceID == "" {
			traceID = newRequestID()
		}
		c.Header("X-Request-ID", traceID)

		// 提取 path param :session_uuid 作为 ctx session_uuid
		sessionUUID := c.Param("session_uuid")

		tr := Trace{
			RequestID:   traceID,
			SessionUUID: sessionUUID,
		}
		ctx := WithTrace(c.Request.Context(), tr)
		ctx = WithLogger(ctx, Logger(ctx)) // 注入带 trace 字段的 logger

		c.Request = c.Request.WithContext(ctx)
		c.Set("trace_id", traceID)
		c.Set("session_uuid", sessionUUID)

		c.Next()
	}
}

// MetricsMiddleware 记录 HTTP 请求的耗时直方图。
//
// 标签：
//   - method: HTTP method
//   - path:   路由模板（含 :param 替换为 *），避免高基数
//   - status: HTTP status code（"200"/"404"/"500"）
//
// 用法：
//   r.Use(observability.MetricsMiddleware(metrics))
func MetricsMiddleware(meter Metrics) gin.HandlerFunc {
	if meter == nil {
		// nil meter 时返回 noop，避免每次访问 metrics 时 nil-check
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		// 路由模板（含 :session_uuid 等参数占位符），避免每次请求不同的 UUID 把指标基数打爆
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		dur := time.Since(start)
		meter.ObserveHistogram(MetricHTTPRequestDuration, map[string]string{
			"path":   path,
			"method": method,
			"status": status,
		}, dur.Seconds())
	}
}

// RecoveryMiddleware panic 恢复 + 记 metric + 写日志。
// 仅当默认 gin.Recovery 不可用时使用；当前 main.go 使用 gin.Default() 已内置 Recovery。
func RecoveryMiddleware(meter Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				tr := TraceFromContext(c.Request.Context())
				Logger(c.Request.Context()).Error("panic recovered",
					"panic", rec,
					"path", c.FullPath(),
					"request_id", tr.RequestID,
				)
				if meter != nil {
					meter.IncCounter("http_panic_total", map[string]string{
						"path": c.FullPath(),
					})
				}
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

// newRequestID 生成新的 request_id。调用方传入空字符串时使用。
// 与 agent_gateway.Trace.WithTrace 行为一致：uuid.NewString()。
func newRequestID() string {
	return uuidLike()
}

// uuidLike 生成伪 uuid 用于 request_id（不导入 google/uuid，避免 observability 包依赖过重）。
// 仅用于 trace_id，不参与 DB 主键。
func uuidLike() string {
	// 使用 time.Now().UnixNano() 编码为 36 字符固定长度的伪 uuid
	n := time.Now().UnixNano()
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	for i := 0; i < 36; i++ {
		switch i {
		case 8, 13, 18, 23:
			out[i] = '-'
		default:
			out[i] = hex[int(n)%16]
			n = n*1103515245 + 12345 // 简单 LCG 摊平分布
			if n < 0 {
				n = -n
			}
		}
	}
	return string(out)
}
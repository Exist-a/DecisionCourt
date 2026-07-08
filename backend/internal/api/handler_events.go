package api

// v0.10 前端埋点 (ADR 0020):POST /api/v1/courtrooms/:session_uuid/events
//
// 设计要点：
//   - 复用 observability.EventRecorder,直接落 decision_events 表,
//     EventType 以 fe. 前缀与后端 span.X 区分。
//   - 鉴权复用 checkSessionAccess(必须是 session owner,防他人灌垃圾事件)。
//   - 埋点失败(recorder nil / Record 返错) → 仍然 200,失败仅 slog 警告。
//     埋点是 best-effort,绝不能阻塞前端主流程。
//   - EventType 长度上限 50(DB schema varchar(50) 约束,handler 层前置校验
//     让 400 在到达 ORM 前拦截,避免浪费一次 DB round-trip)。

import (
	"log/slog"
	"net/http"

	"github.com/decisioncourt/backend/internal/observability"
	"github.com/gin-gonic/gin"
)

// frontendEventRequest 是 POST /events 的请求体。
// Payload 用 map[string]interface{} 接收 —— 前端事件维度差异很大,
// schema-less 更适合快速迭代(详见 ADR 0020 决策 #5)。
type frontendEventRequest struct {
	EventType  string                 `json:"event_type" binding:"required,max=50"`
	Payload    map[string]interface{} `json:"payload"`
	DurationMs int64                  `json:"duration_ms"`
	Status     string                 `json:"status"`
	ErrorMsg   string                 `json:"error_msg"`
}

// maxFrontendEventTypeLen 来自 model.DecisionEvent.EventType 的 varchar(50)。
// 这里复制一份常量避免循环引用(handler_events.go 不应该 import model)。
const maxFrontendEventTypeLen = 50

// PostFrontendEvent 处理前端埋点上报。
// ADR 0020 决策 #2：埋点失败绝不阻塞前端 → recorder nil / Record 报错都返 200。
// ADR 0020 决策 #4：EventType 长度在 handler 层前置校验,避免 DB 写入后再 500。
func (h *Handler) PostFrontendEvent(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	// 鉴权:复用 checkSessionAccess(必须 owner)。这一步会自己写 404/403,
	// 不通过时直接 return。
	if _, ok := h.checkSessionAccess(c, sessionUUID); !ok {
		return
	}

	var req frontendEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "invalid event payload",
		})
		return
	}

	// 显式长度校验:ShouldBindJSON 的 max=50 也会触发,但这里补一刀让错误
	// 消息更明确(DB schema 约束的细节不暴露给客户端,只说 "invalid event_type")。
	if len(req.EventType) > maxFrontendEventTypeLen {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "invalid event_type",
		})
		return
	}

	// status 缺省时填 "ok"(DB schema NOT NULL)。
	status := req.Status
	if status == "" {
		status = "ok"
	}

	// recorder nil 时降级 noop(测试场景 + 生产装配失败兼容)。
	if h.eventRecorder == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"recorded": false}})
		return
	}

	rec := observability.DecisionEventRecord{
		SessionUUID: sessionUUID,
		EventType:   req.EventType,
		Payload:     req.Payload,
		DurationMs:  req.DurationMs,
		Status:      status,
		ErrorMsg:    req.ErrorMsg,
	}
	if err := h.eventRecorder.Record(c.Request.Context(), rec); err != nil {
		// ADR 0020 决策 #2:埋点失败不阻塞前端,仅记 slog。
		slog.Warn("frontend event record failed",
			"event_type", req.EventType,
			"session_uuid", sessionUUID,
			"error", err)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"recorded": true}})
}
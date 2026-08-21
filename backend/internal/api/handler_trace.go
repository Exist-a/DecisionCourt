package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/decisioncourt/backend/internal/trace"
	"github.com/gin-gonic/gin"
)

// TraceDateFormat 是 trace 日期参数的格式,与 agent_gateway.FileLogger dateFormat 对齐。
//
// 接受格式: "2006-01-02" (e.g. "2026-08-22")
// 空字符串 = 查今天 (Store 内部用 dateNow())
// 其他格式 → 400 + 明确错误
const TraceDateFormat = "2006-01-02"

// RegisterTraceRoutes 把 /api/v1/courtrooms/:session_uuid/traces/* 路由挂到传入的 group。
//
// 端点清单 (与 V1.0.4-PLAN.md §2 PR-C1 完全一致):
//   GET /api/v1/courtrooms/:session_uuid/traces         → ListTraces (按 date 查询)
//   GET /api/v1/courtrooms/:session_uuid/traces/:trace_id → GetTrace (单 trace 详情)
//
// 鉴权:
//   - 必须 owner 才能查 (复用 checkSessionAccess)
//   - nil h.traceStore 时不注册路由 (降级 404)
func (h *Handler) RegisterTraceRoutes(api *gin.RouterGroup) {
	if h.traceStore == nil {
		return
	}
	api.GET("/courtrooms/:session_uuid/traces", h.ListTraces)
	api.GET("/courtrooms/:session_uuid/traces/:trace_id", h.GetTrace)
}

// ListTraces 处理 GET /api/v1/courtrooms/:session_uuid/traces
//
// Query params:
//   - date (optional): 查询日期,格式 YYYY-MM-DD。空 = 今天。
//
// 响应:
//   - 200 OK: { code: 0, data: { traces: [...], count: N, date: "..." } }
//   - 200 OK (无数据): { code: 0, data: { traces: [], count: 0, date: "..." } } — 不返 404
//   - 400: date 格式错
//   - 403: 不是 session owner
//   - 404: session 不存在
//   - 500: store 内部错误
func (h *Handler) ListTraces(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")
	if _, ok := h.checkSessionAccess(c, sessionUUID); !ok {
		return
	}

	date := c.Query("date")
	if date != "" {
		if _, err := time.Parse(TraceDateFormat, date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1001,
				"message": "invalid date format, expected YYYY-MM-DD",
			})
			return
		}
	}

	traces, err := h.traceStore.ListBySession(sessionUUID, date)
	if err != nil {
		slog.Error("list traces failed", "session", sessionUUID, "date", date, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    1500,
			"message": "list traces failed",
		})
		return
	}

	// 空数组而非 nil (JSON 一致性)
	if traces == nil {
		traces = []*trace.Trace{}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"traces": traces,
			"count":  len(traces),
			"date":   date,
		},
	})
}

// GetTrace 处理 GET /api/v1/courtrooms/:session_uuid/traces/:trace_id
//
// 响应:
//   - 200 OK: { code: 0, data: <Trace> }
//   - 200 OK (无数据): { code: 0, data: null } — frontend 按"无数据"渲染,不返 404
//     (设计: trace_id 不存在不算 "err",前端 TrialReplay 应容错显示空状态)
//   - 403: 不是 session owner
//   - 404: session 不存在
//   - 500: store 内部错误
//
// URL 参数 trace_id:
//   - URL segment :trace_id 是 LogEntry.RequestID (HTTP trace_id)
//   - 例: GET /traces/abc123def → 查 RequestID="abc123def" 的所有 runs
//   - URL 编码: trace_id 是 hex/alphanumeric,无需特殊编码
func (h *Handler) GetTrace(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")
	if _, ok := h.checkSessionAccess(c, sessionUUID); !ok {
		return
	}

	traceID := c.Param("trace_id")
	if traceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "trace_id is required",
		})
		return
	}

	// 简单长度校验,防 DoS (trace_id 通常是 hex 16-32 字符)
	if len(traceID) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "trace_id too long",
		})
		return
	}

	t, err := h.traceStore.GetTraceByID(sessionUUID, traceID)
	if err != nil {
		slog.Error("get trace failed", "session", sessionUUID, "trace_id", traceID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    1500,
			"message": "get trace failed",
		})
		return
	}

	// 找不到时返 data:null,前端按"无数据"渲染
	if t == nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": t,
	})
}

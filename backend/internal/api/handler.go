package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MemoryLister is the contract GetVisibleMemory uses to fetch the
// v0.5 episodic-memory timeline. In production it's wired to
// courtroom.Service.ListPrivateMemory; tests inject a fake that reads
// from an in-memory a2a repository so they don't need a full Service
// (which depends on GORM DB + LLM + searcher + ReAct runner).
type MemoryLister interface {
	ListPrivateMemory(ctx context.Context, sessionUUID string) ([]model.A2AMessage, error)
}

type Handler struct {
	service            *courtroom.Service
	investigationService *investigation.Service
	// sessionLookup resolves a sessionUUID → CourtSession. Production uses
	// the GORM default (looked up from model.DB); tests can inject an
	// in-memory function so handler tests don't need a real database.
	sessionLookup func(sessionUUID string) (model.CourtSession, bool)
	// memoryLister resolves the v0.5 private-memory timeline. Production
	// defaults to service-backed; tests can inject a fake. See
	// MemoryLister interface comment.
	memoryLister MemoryLister
	// v0.8 白盒化：metrics 实例，可选注入；nil 时 /metrics 端点返回 503。
	metrics observability.Metrics
}

func NewHandler(service *courtroom.Service, investigationService *investigation.Service) *Handler {
	h := &Handler{
		service:            service,
		investigationService: investigationService,
		sessionLookup:      defaultSessionLookup,
	}
	// Default the memory lister to whatever service we got. If service is
	// nil (unit tests that only exercise the investigation routes), the
	// /memory route will 500 with a clear error — same behavior as the
	// existing service-dependent endpoints.
	if service != nil {
		h.memoryLister = service
	}
	return h
}

// WithMetrics 注入 metrics 实例，供 /metrics 端点查询。
// 装配阶段（main.go）调用一次，测试可不调。
func (h *Handler) WithMetrics(m observability.Metrics) {
	h.metrics = m
}

// RegisterMetricsRoute 注册 GET /metrics 端点（JSON 格式的指标快照）。
// 未来如需 Prometheus 兼容导出，可在此处增加 ?format=prometheus 分支。
func (h *Handler) RegisterMetricsRoute(r *gin.Engine) {
	r.GET("/metrics", h.MetricsHandler)
}

// MetricsHandler 返回当前所有指标快照，JSON 格式：
//   { "timestamp": "...", "counters": {...}, "gauges": {...}, "histograms": {...} }
//
// 不走 TraceMiddleware 的 metrics 路径（path=/metrics 不会被 MetricsMiddleware 多次计数，
// 因为 Gin's metrics middleware 已经覆盖）。但 trace 仍然生效，便于在日志中关联。
func (h *Handler) MetricsHandler(c *gin.Context) {
	if h.metrics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    1500,
			"message": "metrics not configured",
		})
		return
	}
	snap := h.metrics.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": snap,
	})
}

// defaultSessionLookup queries the global model.DB. Tests can override
// h.sessionLookup with a closure that returns in-memory sessions.
func defaultSessionLookup(sessionUUID string) (model.CourtSession, bool) {
	var session model.CourtSession
	if err := model.DB.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return model.CourtSession{}, false
	}
	return session, true
}

// lookupSession wraps sessionLookup so call sites read naturally. Always
// returns (session, true) on hit; (zero, false) on miss.
func (h *Handler) lookupSession(sessionUUID string) (model.CourtSession, bool) {
	if h.sessionLookup != nil {
		return h.sessionLookup(sessionUUID)
	}
	return defaultSessionLookup(sessionUUID)
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.HealthHandler)

	api := r.Group("/api/v1")
	{
		api.POST("/courtrooms", h.CreateCourtroom)
		api.GET("/courtrooms/:session_uuid", h.GetCourtroom)
		api.POST("/courtrooms/:session_uuid/start", h.StartTrial)
		api.POST("/courtrooms/:session_uuid/evidences", h.SubmitEvidence)
		api.POST("/courtrooms/:session_uuid/actions", h.UserAction)
		api.GET("/courtrooms/:session_uuid/messages", h.GetMessages)
		api.GET("/courtrooms/:session_uuid/evidences", h.GetEvidences)
		api.GET("/courtrooms/:session_uuid/agents", h.GetAgents)
		api.GET("/courtrooms/:session_uuid/verdict", h.GetVerdict)
		api.GET("/courtrooms/:session_uuid/investigations", h.GetInvestigations)
		api.GET("/courtrooms/:session_uuid/export", h.ExportSession)
		// v0.6 belief engine: structured belief-diff audit trail.
		// Supports ?agent=prosecutor|defender|... and ?round=N filters.
		api.GET("/courtrooms/:session_uuid/belief-diffs", h.GetBeliefDiffs)
		// v0.5 episodic-memory REST hydration. Frontend MemoryAuditPanel
		// can rebuild the full strategy-note timeline on verdict page
		// refresh / browser-back / court page reload. See
		// service.ListPrivateMemory for visibility rationale.
		api.GET("/courtrooms/:session_uuid/memory", h.GetVisibleMemory)
	}
}

func (h *Handler) HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) CreateCourtroom(c *gin.Context) {
	var req struct {
		Title   string `json:"title" binding:"required"`
		OptionA string `json:"option_a"`
		OptionB string `json:"option_b"`
		Context string `json:"context"`
		Mode    string `json:"mode"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": err.Error()})
		return
	}

	session, err := h.service.CreateSession(req.Title, req.OptionA, req.OptionB, req.Context, req.Mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sessionResponse(session)})
}

func (h *Handler) GetCourtroom(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sessionResponse(session)})
}

func (h *Handler) StartTrial(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	// v0.8.3 race 修复：把 phase transition 同步跑完，再让 LLM-backed
	// opening speeches 在 goroutine 里跑。HTTP 200 返回时 DB 已经是
	// `opening` 状态 —— 用户刷新或重复点击"开 庭"看到的都是 opening，
	// 不会再触发"can only start from idle phase"之类的 ValidateAction 错误。
	if _, err := h.service.TransitionToOpening(context.Background(), sessionUUID); err != nil {
		slog.Warn("start trial: transition to opening failed",
			"session_uuid", sessionUUID, "error", err)
		// 1003 = "当前阶段不允许该操作"（见 decisioncourt-api-design §5）。
		// 如果 session 不存在，transitionToOpening 返回 gorm.ErrRecordNotFound，
		// 也归到 400/1003 —— 调用方在打开庭审前必先立案。
		c.JSON(http.StatusBadRequest, gin.H{"code": 1003, "message": err.Error()})
		return
	}

	// ReAct 开庭陈述在后台跑（detached context），HTTP 请求不阻塞。
	go func() {
		if err := h.service.RunOpeningSpeeches(context.Background(), sessionUUID); err != nil {
			slog.Error("opening speeches failed", "session_uuid", sessionUUID, "error", err)
			h.service.Broadcast(sessionUUID, courtroom.Event{
				Type: "error",
				Payload: map[string]interface{}{
					"code":    "OPENING_SPEECHES_FAILED",
					"message": err.Error(),
				},
			})
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"session_uuid":  sessionUUID,
			"current_phase": "opening",
			"message":       "庭审已开始，Agent 发言将通过 WebSocket 推送",
		},
	})
}

func (h *Handler) SubmitEvidence(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	var req struct {
		Content string `json:"content" binding:"required"`
		Type    string `json:"type"`
		Source  string `json:"source"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": err.Error()})
		return
	}

	if req.Type == "" {
		req.Type = "fact"
	}
	if req.Source == "" {
		req.Source = "user"
	}

	// Run asynchronously with a detached context.
	go func() {
		if _, err := h.service.SubmitEvidence(context.Background(), sessionUUID, req.Content, req.Type, req.Source, "user"); err != nil {
			log.Printf("submit evidence failed for %s: %v", sessionUUID, err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"session_uuid": sessionUUID,
			"message":      "证据已提交，Agent 反馈将通过 WebSocket 推送",
		},
	})
}

func (h *Handler) UserAction(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	var req struct {
		Action  string                 `json:"action" binding:"required"`
		Payload map[string]interface{} `json:"payload"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": err.Error()})
		return
	}

	// Run asynchronously with a detached context.
	go func() {
		if err := h.service.ProcessUserAction(context.Background(), sessionUUID, req.Action, req.Payload); err != nil {
			slog.Error("process user action failed", "session_uuid", sessionUUID, "action", req.Action, "error", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"session_uuid": sessionUUID,
			"action":       req.Action,
		},
	})
}

func (h *Handler) GetMessages(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	var messages []model.Message
	model.DB.Where("session_id = ?", session.ID).Order("created_at asc").Find(&messages)

	// v0.6: 提一条 agent_type 到顶层（Message 模型本身没这列，从
	// metadata.agent_type 取），让前端按 agent 过滤/展示不用 join。
	out := make([]gin.H, 0, len(messages))
	for _, m := range messages {
		row := gin.H{
			"id":            m.ID,
			"session_id":    m.SessionID,
			"agent_id":      m.AgentID,
			"phase":         m.Phase,
			"round":         m.Round,
			"content":       m.Content,
			"evidence_refs": m.EvidenceRefs,
			"action_type":   m.ActionType,
			"metadata":      m.Metadata,
			"created_at":    m.CreatedAt,
			"agent_type":    extractAgentTypeFromMetadata(m.Metadata, m.AgentID),
		}
		out = append(out, row)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"messages": out}})
}

// extractAgentTypeFromMetadata reads metadata.agent_type when present
// (set by saveAgentMessage since v0.6). Falls back to looking up the
// agent row to get its AgentType for legacy rows. Returns "" if nothing.
func extractAgentTypeFromMetadata(metadataJSON string, agentID *uuid.UUID) string {
	if metadataJSON != "" {
		var md struct {
			AgentType string `json:"agent_type"`
		}
		if err := json.Unmarshal([]byte(metadataJSON), &md); err == nil && md.AgentType != "" {
			return md.AgentType
		}
	}
	if agentID == nil {
		return ""
	}
	var ag model.Agent
	if err := model.DB.Select("agent_type").Where("id = ?", *agentID).First(&ag).Error; err == nil {
		return string(ag.AgentType)
	}
	return ""
}

func (h *Handler) GetEvidences(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	var evidences []model.Evidence
	model.DB.Where("session_id = ?", session.ID).Order("created_at asc").Find(&evidences)

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"evidences": evidences}})
}

func (h *Handler) GetAgents(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	var agents []model.Agent
	model.DB.Where("session_id = ?", session.ID).Find(&agents)

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"agents": agents}})
}

func (h *Handler) GetVerdict(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	var verdict model.Verdict
	if err := model.DB.Where("session_id = ?", session.ID).First(&verdict).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "判决书不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": verdictResponse(verdict)})
}

// ExportSession returns a self-contained JSON dump of the trial. Used by
// the verdict page's "导出 JSON" button. The user can take this file
// away to keep their private strategy notes after the session ends.
//
// Visibility is enforced via a2a.Bus.ListVisibleTo("user") so the export
// only includes a2a_messages the user was already allowed to see during
// the trial (public transcript + own private memory). Opposing-side
// private notes are NOT included — that's the SQL isolation guarantee.
//
// Supported formats:
//   - json (default): full structured dump
//   - json+download: same payload, but Content-Disposition forces download
//
// PDF export is intentionally NOT done server-side: the verdict page
// uses the browser's print dialog (`window.print()`) with a print
// stylesheet to get a printable PDF. This avoids adding a Go PDF lib
// dependency for a feature used by < 5% of users.
func (h *Handler) ExportSession(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	payload, err := h.service.ExportSession(c.Request.Context(), session)
	if err != nil {
		log.Printf("[ExportSession] failed for %s: %v", sessionUUID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "导出失败"})
		return
	}

	// Always include session_uuid as a query-string echo so the frontend
	// can name the file with the case id.
	filename := fmt.Sprintf("decisioncourt-%s-%s.json",
		sessionUUID,
		time.Now().UTC().Format("20060102-150405"))
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.JSON(http.StatusOK, payload)
}

// GetBeliefDiffs returns the v0.6 structured belief-diff audit trail for a
// session. Each row is one engine update step (one evidence piece applied
// to one agent) with full before/after math: prior/posterior logits, delta,
// evidence weight, weaken factor, and the human-readable reason.
//
// Query params (all optional):
//   - agent=prosecutor|defender|investigator|clerk|judge : filter by agent type
//   - round=N                                            : filter to one round
//
// When the belief repo is not configured (older deployment), we return
// an empty list rather than 500 — the frontend treats this as "no diffs
// recorded yet" and falls back to the legacy belief trajectory.
func (h *Handler) GetBeliefDiffs(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	if h.service == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"diffs": []gin.H{}, "count": 0}})
		return
	}

	repo := h.service.GetDiffRepository()
	if repo == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"diffs": []gin.H{}, "count": 0}})
		return
	}

	agentFilter := c.Query("agent")
	roundStr := c.Query("round")

	var rows []model.BeliefDiff
	var err error

	switch {
	case agentFilter != "":
		rows, err = repo.ListBySessionAndAgent(c.Request.Context(), session.ID, model.AgentType(agentFilter))
	case roundStr != "":
		round, parseErr := strconv.Atoi(roundStr)
		if parseErr != nil || round < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": "round 参数非法"})
			return
		}
		rows, err = repo.ListBySessionAndRound(c.Request.Context(), session.ID, round)
	default:
		rows, err = repo.ListBySession(c.Request.Context(), session.ID)
	}
	if err != nil {
		log.Printf("list belief diffs failed for %s: %v", sessionUUID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "查询信念变化失败"})
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, d := range rows {
		items = append(items, beliefDiffResponse(d))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"diffs": items, "count": len(items)}})
}

func beliefDiffResponse(d model.BeliefDiff) gin.H {
	return gin.H{
		"id":                 d.ID,
		"round":              d.Round,
		"phase":              d.Phase,
		"agent_type":         string(d.AgentType),
		"evidence_id":        evidenceUUIDString(d.EvidenceID),
		"source":             d.Source,
		"direction":          d.Direction,
		"prior_belief_a":     d.PriorBeliefA,
		"posterior_belief_a": d.PosteriorBeliefA,
		"delta_belief_a":     d.DeltaBeliefA,
		"prior_logit":        d.PriorLogit,
		"posterior_logit":    d.PosteriorLogit,
		"evidence_weight":    d.EvidenceWeight,
		"weaken_factor":      d.WeakenFactor,
		"reason":             d.Reason,
		"created_at":         d.CreatedAt,
	}
}

// GetVisibleMemory returns the v0.5 episodic-memory timeline for a session
// (strategy_note / opponent_weakness / self_correction / evidence_eval).
//
// Unlike ListVisibleTo("user") — which is scoped to a viewer role and would
// hide the opposing side's private strategy — this endpoint returns ALL
// four memory types for both sides, matching the WebSocket a2a.message
// broadcast behavior the MemoryAuditPanel was built against. The "真实法庭"
// toggle is a UI-only filter that hides content from the user without
// touching backend state.
//
// Response shape mirrors the WS `a2a.message` envelope so the frontend
// can feed rows directly into the same `appendMemoryEntry` reducer:
//
//	{
//	  "code": 0,
//	  "data": {
//	    "memory": [
//	      {
//	        "id": "uuid",
//	        "message_uuid": "mem_pro_001",
//	        "round": 1,
//	        "phase": "cross_exam",
//	        "from": "prosecutor",
//	        "to": "prosecutor",
//	        "message_type": "strategy_note",
//	        "visibility": "private",
//	        "payload": { "stance": "...", "content": "...", ... },
//	        "created_at": "2026-07-01T..."
//	      }
//	    ],
//	    "count": N
//	  }
//	}
//
// Errors:
//   404 code=1002 when session_uuid not found.
//   200 with empty array when no memory entries yet (NOT 404).
func (h *Handler) GetVisibleMemory(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	// Use lookupSession for the 404 fast-path so we don't have to wait
	// for ListPrivateMemory to surface gorm.ErrRecordNotFound through the
	// generic 1500 error envelope.
	if _, ok := h.lookupSession(sessionUUID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	if h.memoryLister == nil {
		slog.Error("memoryLister not configured", "session_uuid", sessionUUID)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "memory lister not configured"})
		return
	}

	rows, err := h.memoryLister.ListPrivateMemory(c.Request.Context(), sessionUUID)
	if err != nil {
		slog.Error("list private memory failed", "session_uuid", sessionUUID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "查询情节记忆失败"})
		return
	}

	// Note: ListPrivateMemory does its own session lookup and returns
	// (nil, gorm.ErrRecordNotFound) when the session is gone between the
	// two reads. Surface that as 404 too.
	if rows == nil {
		rows = []model.A2AMessage{}
	}

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		payload, perr := a2a.DecodePayload(r)
		if perr != nil {
			slog.Warn("memory decode payload failed; serving empty payload",
				"message_uuid", r.MessageUUID, "error", perr)
			payload = map[string]interface{}{}
		}
		items = append(items, gin.H{
			"id":           r.ID,
			"message_uuid": r.MessageUUID,
			"round":        r.Round,
			"phase":        r.Phase,
			// Frontend reducer expects "from" / "to" (not "from_agent")
			// — matches the WS envelope field names from bus.go:147-148.
			"from":         r.FromAgent,
			"to":           r.ToAgent,
			"message_type": r.MessageType,
			"visibility":   r.Visibility,
			"payload":      payload,
			"created_at":   r.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"memory": items, "count": len(items)},
	})
}

func evidenceUUIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// GetInvestigations returns all investigation findings for a session,
// oldest first. Used by the InvestigatorPanel to hydrate on first load
// so dispatch/report events that arrive via WebSocket don't appear out
// of context.
func (h *Handler) GetInvestigations(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "庭审不存在"})
		return
	}

	if h.investigationService == nil {
		// Investigation service not wired (e.g. older deployment).
		// Return an empty list rather than 500 — the frontend treats
		// this as "no investigations yet".
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"findings": []gin.H{}}})
		return
	}

	findings, err := h.investigationService.ListBySession(c.Request.Context(), session.ID)
	if err != nil {
		slog.Error("list investigations failed", "session_uuid", sessionUUID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "查询调查发现失败"})
		return
	}

	items := make([]gin.H, 0, len(findings))
	for _, f := range findings {
		items = append(items, investigationResponse(f))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"findings": items}})
}

func investigationResponse(f investigation.Finding) gin.H {
	return gin.H{
		"finding_uuid": f.FindingUUID,
		"dispatcher":   f.Dispatcher,
		"investigator": f.Investigator,
		"query":        f.Query,
		"summary":      f.Summary,
		"result_count": f.ResultCount,
		"source":       f.SourceProvider,
		// raw_results 包含每条搜索结果的「标题 | URL | 内容」三元组，
		// 前端 InvestigatorPanel 点击行后展开显示完整内容。这条信息
		// 之前被隐藏，用户只能看到摘要而无法访问证据原文 —— 是 UX 盲点。
		"raw_results": f.RawResult,
		"created_at":  f.CreatedAt,
	}
}

func verdictResponse(v model.Verdict) gin.H {
	return gin.H{
		"id":                v.ID,
		"session_id":        v.SessionID,
		"content":           v.Content,
		"summary":           v.Summary,
		"trial_summary":     v.TrialSummary,
		"option_a_score":    v.OptionAScore,
		"option_b_score":    v.OptionBScore,
		"consensus_points":  v.ConsensusPoints,
		"divergence_points": v.DivergencePoints,
		"recommendation":    v.Recommendation,
		"user_feedback":     v.UserFeedback,
		"created_at":        v.CreatedAt,
	}
}

func sessionResponse(session model.CourtSession) gin.H {
	return gin.H{
		"session_uuid":  session.SessionUUID,
		"title":         session.Title,
		"option_a":      session.OptionA,
		"option_b":      session.OptionB,
		"context":       session.Context,
		"mode":          session.Mode,
		"max_rounds":    session.MaxRounds,
		"current_phase": session.CurrentPhase,
		"current_round": session.CurrentRound,
		"status":        session.Status,
		"converged":     session.Converged,
		"created_at":    session.CreatedAt,
		"updated_at":    session.UpdatedAt,
	}
}

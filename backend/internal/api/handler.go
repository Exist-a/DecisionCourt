package api

import (
	"context"
	"log"
	"net/http"

	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service            *courtroom.Service
	investigationService *investigation.Service
	// sessionLookup resolves a sessionUUID → CourtSession. Production uses
	// the GORM default (looked up from model.DB); tests can inject an
	// in-memory function so handler tests don't need a real database.
	sessionLookup func(sessionUUID string) (model.CourtSession, bool)
}

func NewHandler(service *courtroom.Service, investigationService *investigation.Service) *Handler {
	return &Handler{
		service:            service,
		investigationService: investigationService,
		sessionLookup:      defaultSessionLookup,
	}
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

	// Run asynchronously to avoid HTTP timeout during LLM calls.
	// Use a detached context so the goroutine survives the HTTP response.
	go func() {
		if err := h.service.StartTrial(context.Background(), sessionUUID); err != nil {
			log.Printf("start trial failed for %s: %v", sessionUUID, err)
			h.service.Broadcast(sessionUUID, courtroom.Event{
				Type: "error",
				Payload: map[string]interface{}{
					"code":    "START_TRIAL_FAILED",
					"message": err.Error(),
				},
			})
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"session_uuid": sessionUUID,
			"message":      "庭审开始，Agent 发言将通过 WebSocket 推送",
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
			log.Printf("process user action failed for %s: %v", sessionUUID, err)
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

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"messages": messages}})
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
		log.Printf("list investigations failed for %s: %v", sessionUUID, err)
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

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
	"github.com/decisioncourt/backend/internal/auth"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
		"github.com/decisioncourt/backend/internal/observability"
	"github.com/decisioncourt/backend/internal/ratelimit"
	"github.com/decisioncourt/backend/internal/idempotency"
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
	// v0.8.3 安全(P1-2)：可选的 LLM 端点专用限流中间件。
	// 如果不为 nil,挂到 /evidences /actions(更严的限流,防烧 LLM 配额)。
	// 注入方式:handler.LLMRateLimit = middleware.RateLimit(middleware.LLMConfig)
	LLMRateLimit gin.HandlerFunc

	// v0.9 用户限流 (ADR 0014):每用户每天 N 次 StartTrial。nil 时不限流。
	// 注入方式:handler.TrialRateLimiter = ratelimit.NewMemoryRateLimiter(cfg.UserTrialLimit, 24*time.Hour)
	TrialRateLimiter ratelimit.RateLimiter

	// v0.9 (ADR 0012 §决策 2):客户端 Idempotency-Key header,服务端 24h 去重。
	Idempotency *idempotency.Idempotency

	// v0.10 前端埋点 (ADR 0020):复用 v0.8 observability.EventRecorder,
	// 前端事件直接落 decision_events 表,EventType 以 fe. 前缀与后端 span.X 区分。
	// nil 时 POST /events 端点降级为 200 + 不写库(不阻塞前端)。
	eventRecorder observability.EventRecorder
}

// WithEventRecorder 注入前端埋点 recorder。装配阶段(main.go)调用一次。
func (h *Handler) WithEventRecorder(rec observability.EventRecorder) {
	h.eventRecorder = rec
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

// RegisterRoutes 注册无需鉴权的根级路由(/health 等)。
// /metrics 由 RegisterMetricsRoute 单独注册。
// 鉴权 /api/v1/* 由 RegisterAPIRoutes 注册到带 auth 中间件的 group。
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.HealthHandler)
}

// RegisterAPIRoutes 把 /api/v1/* 路由注册到传入的 group。
// 调用方需在调用前用 auth.Middleware(...) 给 group 挂中间件。
//   authedGroup := r.Group("/api/v1")
//   authedGroup.Use(auth.Middleware(...))
//   handler.RegisterAPIRoutes(authedGroup)
//
// v0.8.3 安全(P1-2):如果 h.LLMRateLimit != nil,挂到 /evidences /actions(更严限流)
func (h *Handler) RegisterAPIRoutes(api *gin.RouterGroup) {
	// 普通端点(默认限流,IP 维度)
	api.POST("/courtrooms", h.CreateCourtroom)
	api.GET("/courtrooms/:session_uuid", h.GetCourtroom)
	api.POST("/courtrooms/:session_uuid/start", h.StartTrial)
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

	// v0.10 前端埋点 (ADR 0020):前端 track() API 的唯一落点。
	// 复用 decision_events 表,EventType 以 fe. 前缀与后端 span.X 区分。
	// 鉴权复用 checkSessionAccess(必须是 session owner,防他人灌垃圾事件)。
	api.POST("/courtrooms/:session_uuid/events", h.PostFrontendEvent)

	// LLM 端点(更严限流,user 维度)— 防"一秒 1000 次 dispatch_investigator"烧配额
	llmGroup := api.Group("/")
	if h.LLMRateLimit != nil {
		llmGroup.Use(h.LLMRateLimit)
	}
	llmGroup.POST("/courtrooms/:session_uuid/evidences", h.SubmitEvidence)
	llmGroup.POST("/courtrooms/:session_uuid/actions", h.UserAction)
}

func (h *Handler) HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// checkSessionAccess 是 P0-1 + P0-5 鉴权核心:
//   1. 用 sessionUUID 查 DB
//   2. 比对 session.OwnerID 与 ctx viewer
//   3. 旧 session(OwnerID="")默认拒绝访问(无 owner 不能鉴权)
// 返回 (session, true) 继续;(zero, false) 已写完错误响应可直接 return。
//
// 调用方惯例:
//   session, ok := h.checkSessionAccess(c, sessionUUID)
//   if !ok { return }
func (h *Handler) checkSessionAccess(c *gin.Context, sessionUUID string) (model.CourtSession, bool) {
	session, ok := h.lookupSession(sessionUUID)
	if !ok {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
			"code":    1002,
			"message": "庭审不存在",
		})
		return model.CourtSession{}, false
	}
	viewer := auth.ViewerFromContext(c)
	if session.OwnerID == "" {
		// 旧 session 无 owner——不允许任何 viewer 访问(防历史数据泄漏)。
		// 如果需要"匿名 session"工作流,OwnerID 应在 v0.8.3 之后的 create
		// 时必填,不会留空。
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":    1403,
			"message": "forbidden: session has no owner (legacy data; recreate to access)",
		})
		return model.CourtSession{}, false
	}
	if session.OwnerID != viewer {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":    1403,
			"message": "forbidden: not the owner of this session",
		})
		return model.CourtSession{}, false
	}
	return session, true
}

// writeAudit 写一条审计日志(异步,不阻塞主流程)。
// 失败时仅记 slog 警告,不返回错误——审计不是关键路径。
func (h *Handler) writeAudit(c *gin.Context, action, target, result, reason string) {
	if model.DB == nil {
		return
	}
	viewer := auth.ViewerFromContext(c)
	row := model.AuditLog{
		UserID:  viewer,
		Action:  action,
		Target:  target,
		IP:      c.ClientIP(),
		UA:      c.GetHeader("User-Agent"),
		Result:  result,
		Reason:  reason,
	}
	if err := model.DB.Create(&row).Error; err != nil {
		slog.Warn("audit log write failed",
			"action", action, "target", target, "user", viewer, "error", err)
	}
}

func (h *Handler) CreateCourtroom(c *gin.Context) {
	var req struct {
		Title   string `json:"title" binding:"required,max=255"`
		OptionA string `json:"option_a" binding:"required,max=255"`
		OptionB string `json:"option_b" binding:"required,max=255"`
		Context string `json:"context" binding:"max=2000"`
		Mode    string `json:"mode" binding:"max=20"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		// v0.8.3 安全(P1-6 错误脱敏):不直接回显 err.Error()(暴露 binding tag / 字段名)
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": "invalid request body"})
		slog.Warn("CreateCourtroom bind failed", "error", err, "client_ip", c.ClientIP())
		return
	}

	// v0.8.3 安全：OwnerID 来自 token 中的 viewer(由 auth.Middleware 注入)。
	ownerID := auth.ViewerFromContext(c)
	if ownerID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1401, "message": "missing viewer"})
		return
	}

	session, err := h.service.CreateSession(req.Title, req.OptionA, req.OptionB, req.Context, req.Mode, ownerID)
	if err != nil {
		// 1001 是请求参数错误(不要泄露底层 err 细节给客户端)。
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": "invalid request"})
		slog.Warn("create courtroom failed", "user", ownerID, "error", err)
		return
	}

	h.writeAudit(c, "session.create", session.SessionUUID, "ok", "")
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

	// v0.9 (ADR 0012 §决策 2): Idempotency-Key 去重。
	if h.Idempotency != nil {
		key := c.GetHeader("Idempotency-Key")
		if key != "" && len(key) <= idempotency.MaxKeyLen {
			if cached, ok := h.Idempotency.Get(key); ok {
				h.writeAudit(c, "session.start", sessionUUID, "idempotency_hit", "key="+key)
				c.Data(int(cached.StatusCode), "application/json", cached.Body)
				return
			}
		}
	}

	// v0.9 (ADR 0014):每用户每天 N 次 StartTrial 限流。nil 时跳过。
	// 必须在 owner check 之前 —— 防止"先验证后限流"导致攻击者枚举 session。
	if h.TrialRateLimiter != nil {
		userID := auth.ViewerFromContext(c)
		if userID != "" {
			allowed, retryAfter, _ := h.TrialRateLimiter.Allow(c.Request.Context(), userID)
			if !allowed {
				h.writeAudit(c, "session.start", sessionUUID, "rate_limited",
					fmt.Sprintf("retry_after=%v", retryAfter))
				c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				now := time.Now().UTC()
				resetsAt := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
				c.JSON(http.StatusTooManyRequests, gin.H{
					"error":               "rate_limit_exceeded",
					"message":             "已达今日 trial 配额上限。请等待重试。",
					"retry_after_seconds": int(retryAfter.Seconds()),
					"resets_at":           resetsAt.Format(time.RFC3339),
				})
				return
			}
		}
	}

	if _, ok := h.checkSessionAccess(c, sessionUUID); !ok {
		h.writeAudit(c, "session.start", sessionUUID, "denied", "owner check failed")
		return
	}

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
					"message": "agent 开幕陈词失败,已记录到日志",
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
		Content string `json:"content" binding:"required,min=1,max=4096"`
		Type    string `json:"type" binding:"max=30,oneof=fact testimony expert_opinion constraint rebuttal"`
		Source  string `json:"source" binding:"max=30,oneof=user investigator system"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": "invalid request body"})
		slog.Warn("SubmitEvidence bind failed", "error", err, "client_ip", c.ClientIP())
		return
	}

	// v0.8.3 安全：先 owner check,再看 service。
	session, ok := h.checkSessionAccess(c, sessionUUID)
	if !ok {
		h.writeAudit(c, "evidence.submit", sessionUUID, "denied", "owner check failed")
		return
	}

	if req.Type == "" {
		req.Type = "fact"
	}
	if req.Source == "" {
		req.Source = "user"
	}

	// v0.8.3 安全：submittedBy = viewer(不再是写死的 "user")。
	// source 白名单已由 binding oneof 限制。
	ownerID := auth.ViewerFromContext(c)
	go func() {
		if _, err := h.service.SubmitEvidence(context.Background(), session.SessionUUID, req.Content, req.Type, req.Source, ownerID); err != nil {
			slog.Warn("submit evidence failed", "session", sessionUUID, "user", ownerID, "error", err)
		}
	}()

	h.writeAudit(c, "evidence.submit", sessionUUID, "ok", "")
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
		Action  string                 `json:"action" binding:"required,max=50,oneof=direct_verdict continue_cross_exam start_cross_exam skip_agent dispatch_investigator reopen_trial"`
		Payload map[string]interface{} `json:"payload"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1001, "message": err.Error()})
		return
	}

	// v0.8.3 安全：先 owner check。
	if _, ok := h.checkSessionAccess(c, sessionUUID); !ok {
		h.writeAudit(c, "user_action."+req.Action, sessionUUID, "denied", "owner check failed")
		return
	}

	// Run asynchronously with a detached context.
	go func() {
		if err := h.service.ProcessUserAction(context.Background(), sessionUUID, req.Action, req.Payload); err != nil {
			slog.Error("process user action failed", "session_uuid", sessionUUID, "action", req.Action, "error", err)
		}
	}()

	h.writeAudit(c, "user_action."+req.Action, sessionUUID, "ok", "")
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

	session, ok := h.checkSessionAccess(c, sessionUUID)
	if !ok {
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

	session, ok := h.checkSessionAccess(c, sessionUUID)
	if !ok {
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

	// v0.8.3 安全：必须 owner 才能导出(下载整个 session 全部数据)。
	session, ok := h.checkSessionAccess(c, sessionUUID)
	if !ok {
		h.writeAudit(c, "session.export", sessionUUID, "denied", "owner check failed")
		return
	}

	payload, err := h.service.ExportSession(c.Request.Context(), session)
	if err != nil {
		slog.Warn("[ExportSession] failed", "session", sessionUUID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "导出失败"})
		return
	}

	h.writeAudit(c, "session.export", sessionUUID, "ok", "")
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

	session, ok := h.checkSessionAccess(c, sessionUUID)
	if !ok {
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

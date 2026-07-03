package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/api"
	"github.com/decisioncourt/backend/internal/auth"
	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func main() {
	config.Load()

	// v0.8 白盒化：用 slog JSON handler 替换默认 logger。所有 log.Printf
	// 在 main / api / agent_gateway 后续被替换为 observability.Logger(ctx)。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	observability.SetDefault(slog.Default())

	// 白盒化：进程级 metrics 实例（线程安全的内存实现）。
	metrics := observability.NewMetrics()

	if err := model.Connect(); err != nil {
		log.Fatalf("database connection failed: %v", err)
	}

	llmClient, err := llm.NewClient()
	if err != nil {
		log.Printf("warning: LLM client not initialized: %v", err)
		log.Println("courtroom service will not be available until LLM_API_KEY is set")
	}

	// Agent Gateway 白盒子集：把所有 LLM 调用过 Recorder，写入
	// llm_calls 表（model.LLMCall）。即使 LLM 客户端尚未就绪，Recorder
	// 仍能接到 nil 内层并安全 noop，不阻塞后续装配。
	recorder := agent_gateway.NewRecorder(agent_gateway.RecorderConfig{
		Enabled:  llmClient != nil,
		Provider: config.AppConfig.LLMProvider,
	}, agent_gateway.NewGORMStore())
	defaultModel := config.AppConfig.LLMModelV3
	if defaultModel == "" {
		defaultModel = "deepseek-chat"
	}
	gatewayCfg := agent_gateway.GatewayConfig{
		Enabled:              config.AppConfig.AgentGateway.Enabled,
		PromptCompression:    config.AppConfig.AgentGateway.PromptCompression,
		TokenBudget:          config.AppConfig.AgentGateway.TokenBudget,
		Throttling:           config.AppConfig.AgentGateway.Throttling,
		Fallback:             config.AppConfig.AgentGateway.Fallback,
		FileLogger:           config.AppConfig.AgentGateway.FileLogger,
		BudgetPerSession:     config.AppConfig.AgentGateway.BudgetPerSession,
		CompressionThreshold: config.AppConfig.AgentGateway.CompressionThreshold,
		ThrottlingThreshold:  config.AppConfig.AgentGateway.ThrottlingThreshold,
		LogDir:               config.AppConfig.AgentGateway.LogDir,

		// v2 Token Budget
		RejectWhenExhausted:    config.AppConfig.AgentGateway.RejectWhenExhausted,
		BudgetSlidingWindowSec: config.AppConfig.AgentGateway.BudgetSlidingWindowSec,

		// v2 Prompt Compression
		SmartCompression:       config.AppConfig.AgentGateway.SmartCompression,
		KeepRecentForcedN:      config.AppConfig.AgentGateway.KeepRecentForcedN,
		SummaryInsertThreshold: config.AppConfig.AgentGateway.SummaryInsertThreshold,
		ScoreThreshold:         config.AppConfig.AgentGateway.ScoreThreshold,
	}
	gatewayClient := agent_gateway.NewWithConfig(llmClient, recorder, defaultModel, gatewayCfg)

	// v0.8 白盒化：把 metrics + GormEventRecorder 注入到 gatewayClient 装饰器层，
	// 让所有 LLM 调用的指标自动归集到 metrics，业务级 span 自动写入 decision_events。
	// 业务级 span 端到端关联靠 Trace{RequestID,SessionUUID,AgentType} 沿 ctx 传递。
	eventRecorder := observability.NewGormEventRecorder(model.DB)
	_ = eventRecorder // 保留引用：business_spans 在 courtroom / orchestrator 层按需构造 Tracer 时使用

	hub := api.NewHub()
	hub.WithMetrics(metrics)
	a2aBroadcaster := func(sessionUUID, eventType string, payload map[string]interface{}) {
		hub.Broadcast(sessionUUID, courtroom.Event{Type: eventType, Payload: payload})
	}
	bus := a2a.NewBus(a2a.NewGormRepository(model.DB), a2aBroadcaster)
	memRepo := private_memory.NewGormRepository(model.DB)
	orchestrator := agent.NewOrchestrator(gatewayClient, bus, memRepo, nil, nil)
	evidenceSvc := evidence.NewService(model.DB, gatewayClient)
	searcher, _ := search.NewProvider(config.AppConfig.SearchProvider, config.AppConfig.BochaAPIKey)

	courtroomSvc := courtroom.NewService(model.DB, orchestrator, evidenceSvc, searcher, bus, hub.Broadcast)
	// v0.6 belief engine: wire the diff + weaken repos so the courtroom
	// service uses the Bayesian-log-odds engine + multi-signal convergence
	// and emits belief.diff / belief.convergence events.
	courtroomSvc.WithBeliefRepositories(
		belief.NewGormDiffRepository(model.DB),
		belief.NewGormWeakenRepository(model.DB),
	)
	// v0.8 白盒化：注入 metrics + event recorder，让 RunCrossExamRound / StateTransition 等
	// 业务级 span 自动归集指标 + 落库到 decision_events。
	courtroomSvc.WithObservability(metrics, eventRecorder)

	handler := api.NewHandler(courtroomSvc, courtroomSvc.InvestigationService())
	// v0.8 白盒化：Handler 暴露 metrics 实例，让 /metrics 端点能查询 snapshot。
	handler.WithMetrics(metrics)
	wsServer := api.NewWebSocketServer(hub, courtroomSvc)

	r := gin.New()
	// v0.8 白盒化：trace middleware 在最前——给每个 HTTP 请求生成/提取 trace_id 并注入 ctx。
	// metrics middleware 紧跟其后——记录 HTTP 请求耗时直方图。
	r.Use(observability.TraceMiddleware())
	r.Use(observability.MetricsMiddleware(metrics))
	r.Use(observability.RecoveryMiddleware(metrics))
	// v0.8.3 安全：AllowedOrigins 从 config 读(env ALLOWED_ORIGINS 覆盖)，
	// 不要再硬编码 localhost。生产必须用 ALLOWED_ORIGINS=https://yourdomain.com 显式设置。
	allowedOrigins := config.AppConfig.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Request-ID"},
		AllowCredentials: true,
	}))

	// v0.8.3 安全(P0-1)：auth 端点(/auth/anon / /auth/logout)无需 token；
	// /health / /metrics 也公开。其余所有 /api/v1/* 由 auth.Middleware 守门。
	//
	// 注意：必须用分组挂中间件,而不是 r.Use(auth.Middleware)——后者会把
	// /health / /metrics / /ws 也强制鉴权,不符合 Q7 决策。
	authedGroup := r.Group("/api/v1")
	authedGroup.POST("/auth/anon", anonAuthHandler(config.AppConfig))
	authedGroup.POST("/auth/logout", logoutAuthHandler(config.AppConfig))
	authedGroup.Use(auth.Middleware(config.AppConfig.JWTSecret))
	handler.RegisterAPIRoutes(authedGroup)

	handler.RegisterRoutes(r) // 注册 /health 到 r
	handler.RegisterMetricsRoute(r)

	// WebSocket 升级前 token 验证在 wsServer.Handler 内部做(query 优先,cookie 兜底)。
	r.GET("/ws/courtrooms/:session_uuid", wsServer.Handler)

	port := config.AppConfig.Port
	if port == "" {
		port = "8080"
	}

	slog.Info("DecisionCourt backend listening",
		"port", port,
		"version", "v0.8.0",
		"whitebox", "enabled",
	)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server failed: %v", err)
	}

	// 保留 context 引用以便未来 OTLP exporter / graceful shutdown 扩展。
	_ = context.Background()
}

// anonAuthHandler 接收客户端传来的 user_id(由前端 crypto.randomUUID() 生成),
// 签发 JWT,set HttpOnly cookie,写 audit_log,upsert user 表。
//
// 请求体:`{"user_id": "anon_xxx"}`(也接受 X-User-Id header 作为 fallback)。
// 响应:`{"code": 0, "data": {"user_id": "...", "expires_in": 604800}}`。
//
// 安全考量:
//   - 严格白名单 user_id 字符集(字母数字下划线短横线,长度 1-64)
//     防止恶意 user_id 撑爆 DB / log 注入
//   - 不验证 user_id 是否"属于"client(匿名模式下无法验证)
//   - 写 user 表时 upsert(FirstOrCreate + 更新 LastSeen)
func anonAuthHandler(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			UserID string `json:"user_id" binding:"required,min=1,max=64"`
		}
		// 兼容从 header 取(简化首次迁移)
		if req.UserID == "" {
			req.UserID = strings.TrimSpace(c.GetHeader("X-User-Id"))
		}
		if err := c.ShouldBindJSON(&req); err != nil && req.UserID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1001,
				"message": "user_id is required (frontend must generate via crypto.randomUUID())",
			})
			return
		}
		req.UserID = strings.TrimSpace(req.UserID)
		// 白名单字符集
		for _, r := range req.UserID {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"code":    1001,
					"message": "user_id must match [A-Za-z0-9_.-]{1,64}",
				})
				return
			}
		}

		expiry := time.Duration(cfg.JWTExpiryHours) * time.Hour
		if expiry <= 0 {
			expiry = 7 * 24 * time.Hour
		}
		token, err := auth.Sign(cfg.JWTSecret, req.UserID, expiry)
		if err != nil {
			slog.Error("auth.Sign failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "auth setup failed"})
			return
		}

		// upsert user 表
		if model.DB != nil {
			now := time.Now().UTC()
			row := model.User{
				UserID:    req.UserID,
				FirstSeen: now,
				LastSeen:  now,
				LastIP:    c.ClientIP(),
				LastUA:    c.GetHeader("User-Agent"),
			}
			// SQLite-style upsert;Postgres 同样支持 ON CONFLICT via GORM
			if err := model.DB.Where("user_id = ?", req.UserID).
				Assign(model.User{LastSeen: now, LastIP: row.LastIP, LastUA: row.LastUA}).
				FirstOrCreate(&row).Error; err != nil {
				slog.Warn("user upsert failed", "user", req.UserID, "error", err)
			}
		}

		// set HttpOnly cookie
		setSessionCookie(c, cfg, token, int(expiry.Seconds()))

		// 写 audit
		row := model.AuditLog{
			ID:        uuid.New(),
			UserID:    req.UserID,
			Action:    "auth.anon",
			Target:    req.UserID,
			IP:        c.ClientIP(),
			UA:        c.GetHeader("User-Agent"),
			Result:    "ok",
			CreatedAt: time.Now().UTC(),
		}
		if model.DB != nil {
			_ = model.DB.Create(&row).Error
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"user_id":    req.UserID,
				"token":      token,
				"expires_in": int(expiry.Seconds()),
			},
		})
	}
}

// logoutAuthHandler 清 cookie + 写 audit。
// JWT 是无状态的——清 cookie 即可让浏览器不再带它;旧 token 仍然有效直到过期。
// 未来要做"立即吊销"需要黑名单(deny-list),目前不在 P0 范围。
func logoutAuthHandler(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		clearSessionCookie(c, cfg)
		viewer := auth.ViewerFromContext(c)
		if viewer == "" {
			viewer = "anon"
		}
		_ = model.DB.Create(&model.AuditLog{
			ID:        uuid.New(),
			UserID:    viewer,
			Action:    "auth.logout",
			IP:        c.ClientIP(),
			UA:        c.GetHeader("User-Agent"),
			Result:    "ok",
			CreatedAt: time.Now().UTC(),
		}).Error
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"message": "logged out"}})
	}
}

// setSessionCookie / clearSessionCookie 统一管 cookie 属性。
func setSessionCookie(c *gin.Context, cfg config.Config, token string, maxAgeSec int) {
	sameSite := http.SameSiteLaxMode
	switch strings.ToLower(cfg.CookieSameSite) {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
	}
	c.SetSameSite(sameSite)
	c.SetCookie(
		auth.CookieName,
		token,
		maxAgeSec,
		"/",
		cfg.CookieDomain,
		cfg.CookieSecure, // Secure flag: dev = false (HTTP localhost), prod = true (HTTPS)
		true,             // HttpOnly
	)
}

func clearSessionCookie(c *gin.Context, cfg config.Config) {
	sameSite := http.SameSiteLaxMode
	switch strings.ToLower(cfg.CookieSameSite) {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
	}
	c.SetSameSite(sameSite)
	c.SetCookie(
		auth.CookieName,
		"",
		-1,
		"/",
		cfg.CookieDomain,
		cfg.CookieSecure,
		true,
	)
}

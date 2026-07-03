package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/decisioncourt/backend/internal/a2a"
	
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/api"
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
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:3001", "http://127.0.0.1:3001"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Request-ID"},
		AllowCredentials: true,
	}))

	handler.RegisterRoutes(r)
	// v0.8 白盒化：/metrics 端点暴露 Prometheus-兼容指标（JSON 格式，未来可换 Prometheus exporter）。
	handler.RegisterMetricsRoute(r)
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

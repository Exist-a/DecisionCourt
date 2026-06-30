package main

import (
	"log"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/api"
	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	config.Load()

	if err := model.Connect(); err != nil {
		log.Fatalf("database connection failed: %v", err)
	}

	llmClient, err := llm.NewClient()
	if err != nil {
		log.Printf("warning: LLM client not initialized: %v", err)
		log.Println("courtroom service will not be available until LLM_API_KEY is set")
	}

	hub := api.NewHub()
	a2aBroadcaster := func(sessionUUID, eventType string, payload map[string]interface{}) {
		hub.Broadcast(sessionUUID, courtroom.Event{Type: eventType, Payload: payload})
	}
	bus := a2a.NewBus(a2a.NewGormRepository(model.DB), a2aBroadcaster)
	memRepo := private_memory.NewGormRepository(model.DB)
	orchestrator := agent.NewOrchestrator(llmClient, bus, memRepo)
	evidenceSvc := evidence.NewService(model.DB, llmClient)
	searcher, _ := search.NewProvider(config.AppConfig.SearchProvider, config.AppConfig.BochaAPIKey)

	courtroomSvc := courtroom.NewService(model.DB, orchestrator, evidenceSvc, searcher, bus, hub.Broadcast)
	handler := api.NewHandler(courtroomSvc, courtroomSvc.InvestigationService())
	wsServer := api.NewWebSocketServer(hub, courtroomSvc)

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:3001", "http://127.0.0.1:3001"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	handler.RegisterRoutes(r)
	r.GET("/ws/courtrooms/:session_uuid", wsServer.Handler)

	port := config.AppConfig.Port
	if port == "" {
		port = "8080"
	}

	log.Printf("DecisionCourt backend listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

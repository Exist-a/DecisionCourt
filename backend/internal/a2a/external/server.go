package external

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ServerConfig 是 A2A external server 的配置。
type ServerConfig struct {
	Provider string              // Provider organization
	Version  string              // system version
	Cards    map[string]AgentCard // agent type → card
	Bridge   *Bridge             // 桥接到内部 A2A bus（用于 tasks/send）
}

// Server 是 A2A external server。
type Server struct {
	cfg ServerConfig
}

// NewServer 构造 Server。
func NewServer(cfg ServerConfig) *Server {
	return &Server{cfg: cfg}
}

// RegisterRoutes 注册 3 个端点到 Gin engine。
//
// 端点 1: GET /.well-known/agent-card.json
// 端点 2: GET /a2a/agents/:type/agent-card
// 端点 3: POST /a2a/tasks/send
func (s *Server) RegisterRoutes(r *gin.Engine) {
	r.GET("/.well-known/agent-card.json", s.handleDiscovery)
	r.GET("/a2a/agents/:type/agent-card", s.handleAgentCard)
	r.POST("/a2a/tasks/send", s.handleTaskSend)
}

// handleDiscovery 处理 GET /.well-known/agent-card.json。
func (s *Server) handleDiscovery(c *gin.Context) {
	doc := assembleDiscoveryDocument(s.cfg.Cards, s.cfg.Version)
	c.JSON(http.StatusOK, doc)
}

// handleAgentCard 处理 GET /a2a/agents/:type/agent-card。
func (s *Server) handleAgentCard(c *gin.Context) {
	agentType := c.Param("type")
	card, ok := s.cfg.Cards[agentType]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"jsonrpc": "2.0",
			"error": map[string]interface{}{
				"code":    -32004,
				"message": "agent type not found: " + agentType,
			},
		})
		return
	}
	c.JSON(http.StatusOK, card)
}

// handleTaskSend 处理 POST /a2a/tasks/send。
//
// 接收 JSON-RPC 2.0 格式的 task：
//
//	{"jsonrpc":"2.0","id":"req-1","method":"tasks/send",
//	 "params":{"id":"task-123","message":{...}}}
//
// 最小实现：解析 → 桥接 → 落库（如果 Bridge 非 nil）→ 返回 202 accepted。
func (s *Server) handleTaskSend(c *gin.Context) {
	var req struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      string                 `json:"id"`
		Method  string                 `json:"method"`
		Params  map[string]interface{} `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"id":      nil,
			"error": map[string]interface{}{
				"code":    -32700,
				"message": "parse error: " + err.Error(),
			},
		})
		return
	}
	if req.JSONRPC != "2.0" {
		c.JSON(http.StatusBadRequest, gin.H{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error": map[string]interface{}{
				"code":    -32600,
				"message": "invalid request: jsonrpc must be 2.0",
			},
		})
		return
	}
	// 解析 params
	taskID, _ := req.Params["id"].(string)
	msgData, _ := json.Marshal(req.Params["message"])
	var msg ExternalMessage
	_ = json.Unmarshal(msgData, &msg) // 容忍 message 格式不完整

	// 桥接到内部 bus（如果配置了）
	if s.cfg.Bridge != nil {
		sessionUUID, _ := req.Params["sessionId"].(string)
		_ = s.cfg.Bridge.PublishToInternalBus(c.Request.Context(), ExternalTask{
			ID:          taskID,
			Method:      req.Method,
			Message:     msg,
			SessionUUID: sessionUUID,
		})
	}

	// 返回 202 accepted（JSON-RPC 2.0 标准响应）
	c.JSON(http.StatusAccepted, gin.H{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result": map[string]interface{}{
			"id": taskID,
			"status": map[string]interface{}{
				"state":   "submitted",
				"message": "task queued for internal processing",
			},
			"artifacts": []interface{}{},
		},
	})
}

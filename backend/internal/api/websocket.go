package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WebSocketServer struct {
	hub     *Hub
	service *courtroom.Service
}

func NewWebSocketServer(hub *Hub, service *courtroom.Service) *WebSocketServer {
	return &WebSocketServer{
		hub:     hub,
		service: service,
	}
}

func (s *WebSocketServer) Handler(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	s.hub.Join(sessionUUID, conn)
	defer s.hub.Leave(sessionUUID, conn)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var event struct {
			Type    string                 `json:"type"`
			Payload map[string]interface{} `json:"payload"`
		}
		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		if event.Type == "user.action" {
			action := getString(event.Payload, "action")
			// v0.8 白盒化：从 WS 消息中提取 trace_id（如前端传来），注入 ctx。
			// 缺失时生成新 trace_id，保证每条 user.action 都有可关联的 trace。
			traceID := getString(event.Payload, "trace_id")
			tr := observability.Trace{
				RequestID:   traceID,
				SessionUUID: sessionUUID,
			}
			ctx := observability.WithTrace(context.Background(), tr)
			// Run in background to avoid blocking WebSocket read loop
			go func() {
				var err error
				switch action {
				case "submit_evidence":
					content := getString(event.Payload, "content")
					evType := getString(event.Payload, "type")
					if evType == "" {
						evType = "fact"
					}
					_, err = s.service.SubmitEvidence(ctx, sessionUUID, content, evType, "user", "user")
				case "interrupt":
					content := getString(event.Payload, "content")
					err = s.service.Interrupt(sessionUUID, content)
				default:
					err = s.service.ProcessUserAction(ctx, sessionUUID, action, event.Payload)
				}
				if err != nil {
					s.hub.Broadcast(sessionUUID, courtroom.Event{
						Type: "error",
						Payload: map[string]interface{}{
							"code":    "ACTION_FAILED",
							"message": err.Error(),
						},
					})
				}
			}()
		}
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

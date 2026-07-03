package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/decisioncourt/backend/internal/auth"
	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// upgrader 是 gorilla/websocket 的 Upgrade 配置。
//
// v0.8.3 安全(P1-1)：CheckOrigin 不再 return true,改为白名单(env 读
// ALLOWED_ORIGINS,默认 localhost:3000/3001)。生产部署必须覆盖。
//
//   curl -H "Origin: https://evil.com" ws://host/ws/...
//     → 拒绝(不在白名单)
//
//   curl -H "Origin: https://yourdomain.com" ws://host/ws/... (生产)
//     → 通过(若 ALLOWED_ORIGINS=https://yourdomain.com)
var upgrader = websocket.Upgrader{
	CheckOrigin: buildCheckOrigin(),
}

// buildCheckOrigin 返回一个 Origin 白名单检查函数。
// 行为:Origin 为空(非浏览器 client / 服务端调用)→ 通过;
// Origin 在白名单 → 通过;否则拒绝。
func buildCheckOrigin() func(r *http.Request) bool {
	allowed := config.AppConfig.AllowedOrigins
	if len(allowed) == 0 {
		allowed = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		allowedSet[strings.TrimRight(o, "/")] = true
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// 非浏览器调用(curl / native / 测试),不检查
			return true
		}
		return allowedSet[strings.TrimRight(origin, "/")]
	}
}

type WebSocketServer struct {
	hub     *Hub
	service *courtroom.Service
	secret  string
}

func NewWebSocketServer(hub *Hub, service *courtroom.Service) *WebSocketServer {
	return &WebSocketServer{
		hub:     hub,
		service: service,
		secret:  config.AppConfig.JWTSecret,
	}
}

// Handler 处理 WebSocket 升级 + 消息循环。
//
// v0.8.3 安全(P0-1 + P0-5)：
//   1. 升级前从 ?token=xxx / Cookie dc_session 提取 viewer_id
//   2. 验证失败 → 401(不升级)
//   3. 同时检查 session 存在(用 viewer_id 限制;其他人的 session 不升级)
//   4. 连接级 viewer_id 写入 conn 上下文;user.action 派发时带上,
//      service 用它做 audit + submittedBy
func (s *WebSocketServer) Handler(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	// 1. 提取 viewer_id
	viewer, err := extractWSViewer(c, s.secret)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"code":    1401,
			"message": "unauthorized: " + err.Error(),
		})
		return
	}

	// 2. 验证 session 存在 + 必须是 owner
	if model.DB == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code":    1500,
			"message": "database not ready",
		})
		return
	}
	var n int64
	if err := model.DB.Table("court_sessions").
		Where("session_uuid = ? AND owner_id = ?", sessionUUID, viewer).
		Count(&n).Error; err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code":    1500,
			"message": "session lookup failed",
		})
		return
	}
	if n == 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":    1403,
			"message": "forbidden: not the owner of this session",
		})
		return
	}

	// 3. 升级
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	s.hub.Join(sessionUUID, conn)
	defer s.hub.Leave(sessionUUID, conn)

	// 把 viewer_id 绑到 conn 的 custom key(用闭包捕获)
	connViewer := viewer

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

		// v0.8.3 心跳：客户端每 ~25s 发 {type:"ping"}，服务端立即回
		// {type:"pong"}。
		if event.Type == "ping" {
			s.hub.Broadcast(sessionUUID, courtroom.Event{
				Type:    "pong",
				Payload: map[string]interface{}{"ts": time.Now().UTC().Format(time.RFC3339)},
			})
			continue
		}

		if event.Type == "user.action" {
			action := getString(event.Payload, "action")
			traceID := getString(event.Payload, "trace_id")
			tr := observability.Trace{
				RequestID:   traceID,
				SessionUUID: sessionUUID,
				// 把 viewer 写入 trace,日志里能直接 grep
			}
			ctx := observability.WithTrace(context.Background(), tr)
			ctx = context.WithValue(ctx, viewerCtxKey{}, connViewer)

			go func() {
				var err error
				switch action {
				case "submit_evidence":
					content := getString(event.Payload, "content")
					evType := getString(event.Payload, "type")
					if evType == "" {
						evType = "fact"
					}
					source := getString(event.Payload, "source")
					if source == "" {
						source = "user"
					}
					// v0.8.3 安全：submittedBy = connViewer(不再是 "user")
					_, err = s.service.SubmitEvidence(ctx, sessionUUID, content, evType, source, connViewer)
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

// extractWSViewer 优先读 URL ?token=xxx(给 native client / 测试用),
// 兜底读 Cookie dc_session(浏览器同源时自动带)。
func extractWSViewer(c *gin.Context, secret string) (string, error) {
	if t := strings.TrimSpace(c.Query("token")); t != "" {
		return auth.ExtractFromQuery(secret, t)
	}
	if cookie, err := c.Cookie(auth.CookieName); err == nil && cookie != "" {
		claims, err := auth.Parse(secret, cookie)
		if err != nil {
			return "", err
		}
		return claims.UserID, nil
	}
	return "", errMissingToken
}

var errMissingToken = stringError("no token in query or cookie")

type stringError string

func (e stringError) Error() string { return string(e) }

// viewerCtxKey 是 ctx key 类型,防止碰撞。
type viewerCtxKey struct{}

// modelDB 返回 model.DB,做了 nil-check + 用函数封装以避免初始化顺序问题。
// 这里之所以用函数,是因为 handler / ws / auth 可能不依赖 model,但要用 DB。
func modelDB() interface {
	Table(name string) interface {
		Where(query interface{}, args ...interface{}) interface {
			Count(out *int64) error
		}
	}
} {
	return nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

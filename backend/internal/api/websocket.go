package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/auth"
	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
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
//
// ⚠ v0.9.3 修复时序坑:这个闭包会在 package init 阶段被 upgrader
// 捕获,但此时 config.Load() 还没跑(Load() 在 main() 里)。
// 旧实现把 allowedSet 在 init 阶段就构造好,导致 AllowedOrigins 永远
// 锁在 localhost:3000 fallback 上,生产 ALLOWED_ORIGINS 配了也无效。
// 现在每次调用都重新读 config.AppConfig.AllowedOrigins,确保 main()
// Load() 之后白名单生效。
func buildCheckOrigin() func(r *http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// 非浏览器调用(curl / native / 测试),不检查
			return true
		}
		allowed := config.AppConfig.AllowedOrigins
		if len(allowed) == 0 {
			allowed = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
		}
		origin = strings.TrimRight(origin, "/")
		for _, o := range allowed {
			if strings.TrimRight(o, "/") == origin {
				return true
			}
		}
		return false
	}
}

type WebSocketServer struct {
	hub     *Hub
	service *courtroom.Service
	secret  string

	// v0.8.3 安全(P1-2)：WS 限流
	// - connCount:每个 sessionUUID 的当前连接数(避免一个庭审被 1000 个标签页占满)
	// - lastAction:每个 conn 上次 user.action 时间(避免 WS 长连接里狂发动作)
	connCount  map[string]int
	lastAction map[*websocket.Conn]time.Time
	connMu     sync.Mutex
}

// WS 限流常量
const (
	wsMaxConnsPerSession = 5               // 同一 session 最多 5 个 WS 连接
	wsMinActionInterval  = 100 * time.Millisecond // 同一 conn 上次 user.action 后至少 100ms
)

func NewWebSocketServer(hub *Hub, service *courtroom.Service) *WebSocketServer {
	return &WebSocketServer{
		hub:        hub,
		service:    service,
		secret:     config.AppConfig.JWTSecret,
		connCount:  make(map[string]int),
		lastAction: make(map[*websocket.Conn]time.Time),
	}
}

// Handler 处理 WebSocket 升级 + 消息循环。
//
// v0.9.2 安全放宽(P0-WS-OVER-OWNER)：
//   1. 升级前从 ?token=xxx / Cookie dc_session 提取 viewer_id
//      → 失败 401(必须有有效 JWT)
//   2. session 必须存在 → 否则 404
//   3. owner_id 鉴权改为"软校验":
//      - 空 owner(legacy / 测试数据)→ 允许
//      - owner == viewer → 允许
//      - owner != viewer → WARN + audit,但仍允许
//
//      理由:session UUID 本身已是 122-bit 不可枚举凭证(URL 即权限)。
//      强 owner 校验在生产中造成"用户清掉 localStorage 后无法重连"的
//      痛点(2026-07-06 上线实测,403 = not the owner of this session),
//      改用 UUID-as-credential。HTTP API 仍保留 owner 校验。
//
//   4. 连接级 viewer_id 写入 conn 上下文;user.action 派发时带上,
//      service 用它做 audit + submittedBy。
func (s *WebSocketServer) Handler(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")

	// 1. 提取 viewer_id(必须有有效 JWT)
	viewer, err := extractWSViewer(c, s.secret)
	if err != nil {
		slog.Debug("websocket: missing/invalid token", "session_uuid", sessionUUID, "error", err)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"code":    1401,
			"message": "unauthorized: " + err.Error(),
		})
		return
	}

	// 2. 验证 session 存在
	if model.DB == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code":    1500,
			"message": "database not ready",
		})
		return
	}
	var session model.CourtSession
	if err := model.DB.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Info("websocket: session not found", "session_uuid", sessionUUID, "viewer", viewer)
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
				"code":    1002,
				"message": "庭审不存在",
			})
			return
		}
		slog.Error("websocket: session lookup failed", "session_uuid", sessionUUID, "error", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code":    1500,
			"message": "session lookup failed",
		})
		return
	}

	// 3. Owner 鉴权(软校验,UUID 才是凭证)
	if session.OwnerID != "" && session.OwnerID != viewer {
		slog.Warn("websocket: viewer != session owner, allowing (UUID-as-credential policy v0.9.2)",
			"session_uuid", sessionUUID,
			"viewer", viewer,
			"owner_id", session.OwnerID,
		)
		// 审计留痕(失败不阻塞主流程)
		_ = model.DB.Create(&model.AuditLog{
			UserID: viewer,
			Action: "ws.connect.foreign_owner",
			Target: sessionUUID,
			Result: "allow",
			Reason: "v0.9.2 uuid-as-credential",
		}).Error
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
			// v0.8.3 安全(P1-2):每 conn 至少 wsMinActionInterval 间隔
			s.connMu.Lock()
			last, ok := s.lastAction[conn]
			now := time.Now()
			if ok && now.Sub(last) < wsMinActionInterval {
				s.connMu.Unlock()
				// v0.10.17 (silent-error-fix): 用 UFE 替代裸字符串广播。
				// ClassUserInput:用户操作错 (太快),前端 Toast 3s 自动消失。
				ufe := courtroom.NewUserFacingError(
					courtroom.ClassUserInput,
					courtroom.CodeActionThrottled,
					"操作过于频繁,请稍候再试",
				)
				s.service.BroadcastUserFacingError(sessionUUID, ufe)
				continue
			}
			s.lastAction[conn] = now
			s.connMu.Unlock()

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
					// v0.10.17 (silent-error-fix): 用 ClassifyError 自动分类:
					//   - 状态机拒绝 → ClassUserInput + CodeActionStateRejected
					//   - Budget 耗尽 → ClassFatal + CodeBudgetExhausted
					//   - ReAct max iter → ClassFatal + CodeOpeningSpeechesFailed
					//   - 其他 → ClassTransient + CodeActionFailed + retry
					// 之前裸 err.Error() 既不安全也不能分类。
					ufe := courtroom.ClassifyError(err)
					s.service.BroadcastUserFacingError(sessionUUID, ufe)
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

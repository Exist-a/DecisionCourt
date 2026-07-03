package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Middleware 构造一个 gin 中间件,功能:
//   1. 从 Cookie 读 dc_session → 用 JWT_SECRET 验签
//   2. 兜底从 Authorization: Bearer xxx 读
//   3. 验证通过 → c.Set(ContextKey, userID)
//   4. 失败 → 401 + 停止后续处理
//
// secret 必传(从 config.JWTSecret 读);secure 控制 cookie 是否 Secure,
// 在 dev (HTTP localhost) 时由 config.CookieSecure=false 关闭 Secure flag,
// 实际 SetCookie 不在这里做 — SetCookie 在登录/登出端点里手动控制。
//
// 用法:
//   api := r.Group("/api/v1")
//   api.Use(auth.Middleware(config.AppConfig.JWTSecret))
func Middleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := extractUserID(c, secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    1401,
				"message": "unauthorized: " + err.Error(),
			})
			return
		}
		c.Set(ContextKey, userID)
		c.Next()
	}
}

// extractUserID 按优先级提取并验证 token:
//   1. Cookie (浏览器自动带,首选)
//   2. Authorization: Bearer (给 curl/SDK 用)
// 返回 user_id 或 error。
func extractUserID(c *gin.Context, secret string) (string, error) {
	// 1. Cookie
	if cookie, err := c.Cookie(CookieName); err == nil && cookie != "" {
		claims, err := Parse(secret, cookie)
		if err == nil {
			return claims.UserID, nil
		}
		// Cookie 存在但验签失败,继续尝试 Authorization 给兼容性一个机会
	}

	// 2. Authorization: Bearer
	authz := c.GetHeader("Authorization")
	if len(authz) > 7 && authz[:7] == "Bearer " {
		token := authz[7:]
		claims, err := Parse(secret, token)
		if err == nil {
			return claims.UserID, nil
		}
		return "", err
	}

	return "", errors.New("no valid auth token found")
}

// ExtractFromQuery 是给 WebSocket 用的:从 URL ?token=xxx 验签。
// WebSocket 升级时浏览器会带 cookie,但 query token 是兜底(给 native client / 测试脚本用)。
func ExtractFromQuery(secret, token string) (string, error) {
	claims, err := Parse(secret, token)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

// ViewerFromContext 返回 c.MustGet(ContextKey) 的字符串值。
// 调用方应保证已通过 Middleware 鉴权,否则 panic。
func ViewerFromContext(c *gin.Context) string {
	v, ok := c.Get(ContextKey)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

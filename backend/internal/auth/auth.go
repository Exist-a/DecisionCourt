// Package auth implements the v0.8.3 anonymous-JWT authentication scheme.
//
// 设计目标(P0-1 / 2.1 安全审计):
//   - 首次访问者,前端在 localStorage 生成一个随机 user_id(用 crypto.randomUUID())
//   - 前端调用 POST /api/v1/auth/anon 把 user_id 传上来;后端用 HS256 签发 JWT,设 cookie + 返回
//   - 后续请求自动带 cookie(JWT 写入 HttpOnly + Secure + SameSite=Lax 的 dc_session cookie)
//   - WebSocket 走 URL query ?token=xxx 兜底(同源时浏览器也会自动带 cookie,所以后端两个都试)
//   - 不做密码/邮箱/注册;服务端只"信任"客户端自报 user_id,匿名身份
//
// 安全边界:
//   - JWT_SECRET 必须从 env 读,缺失则 fail-fast(在 config.Load 里)
//   - 7 天过期;过期前端用同一 user_id 再调 /auth/anon 即可续期
//   - 不存 user 表的密码/敏感字段 — user_id 是公开的匿名标识
//   - HS256 单密钥,后端持有;若未来需要支持多端(后端微服务化),可改 RS256
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CookieName 是会话 cookie 的名字。前端不要在 localStorage 同步存这个(cookie 自身是 HttpOnly 的)。
const CookieName = "dc_session"

// ContextKey 是 gin.Context.Set/Get 用的 viewer key。
const ContextKey = "viewer"

// Claims 是 JWT payload 的结构。包含最小必要字段。
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// Sign 用 HS256 + JWT_SECRET 签发 token,有效期 7 天(可由 config.JWTExpiryHours 覆盖)。
// 失败原因:secret 为空(配置错误)→ 启动时就该 fail-fast,所以这里基本只会在单测里 fail。
func Sign(secret string, userID string, expiry time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("auth: JWT_SECRET is empty (this is a server misconfiguration)")
	}
	if userID == "" {
		return "", errors.New("auth: user_id is required")
	}
	now := time.Now()
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			Issuer:    "decisioncourt",
			Subject:   userID,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// Parse 验证签名 + 过期时间,返回 Claims。
// 注意:Parse 不检查 user_id 是否非空(由调用方决定);但本项目里 user_id 是必填的。
func Parse(secret, tokenStr string) (*Claims, error) {
	if secret == "" {
		return nil, errors.New("auth: JWT_SECRET is empty")
	}
	if tokenStr == "" {
		return nil, errors.New("auth: token is empty")
	}
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		// 防御性检查:阻止 alg=none 攻击(库默认会拒,这里再显式确认)
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("auth: token is invalid")
	}
	if claims.UserID == "" {
		return nil, errors.New("auth: token missing user_id")
	}
	return claims, nil
}

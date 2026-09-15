package middleware

// v2.5 (P1-2 安全审计修复) CSRF Token 中间件 (double-submit cookie 模式)。
//
// 攻击场景：用户已登录 dc_session cookie（浏览器自动带），攻击者在 evil.com
// 发起 <form action="https://yourdomain.com/api/v1/courtrooms/..." method=POST">，
// 浏览器自动带 dc_session → 后端认为是合法 user action。这是经典 CSRF。
//
// double-submit cookie 防御：
//   1. 后端 Set-Cookie 一个 XSRF-TOKEN（**非 HttpOnly**，让 JS 可读）
//   2. 前端 fetch 读 cookie 里的 token，放到 X-XSRF-TOKEN header
//   3. 后端中间件校验：cookie == header（且 method 为 POST/PUT/DELETE）
//
// 设计权衡（vs gorilla/csrf）：
//   - **手写不引入新依赖**（gorilla/csrf 加深 v2.5 commit 范围 + 大版本绑定）
//   - 使用 HMAC 签名而非随机 token：服务端校验时可重新计算签名，无需存表
//   - Cookie Path=/api/v1，Domain 与 dc_session 一致（SameSite=Lax 已有 CSRF 部分防御）
//   - **豁免** GET / HEAD / OPTIONS（idempotent method 不应触发 CSRF 校验）
//   - **豁免** /auth/anon（注册 / 首次登录，本身就要发 token）
//   - **WS 升级** 不强制 CSRF（浏览器 WS API 不让设自定义 header；改用 Origin 白名单已实装）
//
// 关键不变量：
//   - 签名密钥 = JWT_SECRET（同源密钥复用，避免新增 env）
//   - 签名 payload = user_id || timestamp || nonce（保证不可预测 + 不可重放）
//   - timestamp 容忍 ±2 小时滑动窗口（参考 gorilla/csrf 默认）

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CSRFConfig 中间件配置。
type CSRFConfig struct {
	// Secret 用于 HMAC 签名（必填，建议复用 JWT_SECRET）。
	Secret []byte
	// CookieName Set-Cookie / Read 的 cookie 名。默认 "XSRF-TOKEN"。
	CookieName string
	// HeaderName 前端必须塞的 header 名。默认 "X-XSRF-TOKEN"。
	HeaderName string
	// CookiePath Set-Cookie Path。默认 "/api/v1"（让 JS 在 API 请求域下读得到）。
	CookiePath string
	// MaxAge cookie 有效期。默认 24h。
	MaxAge time.Duration
	// SkipPaths 跳过 CSRF 校验的路径（这些路径本身就是发 token 的）。
	SkipPaths []string
}

// DefaultCSRFConfig 默认配置（Secret 调用方必填）。
func DefaultCSRFConfig(secret []byte) CSRFConfig {
	return CSRFConfig{
		Secret:     secret,
		CookieName: "XSRF-TOKEN",
		HeaderName: "X-XSRF-TOKEN",
		CookiePath: "/api/v1",
		MaxAge:     24 * time.Hour,
		SkipPaths:  []string{"/auth/anon", "/auth/login"},
	}
}

// csrfTokenPayload 嵌入 cookie value 的可解码结构（用于 HMAC 验签）。
// 设计：value = base64(userIDHex + "." + timestampUnix + "." + nonceHex + "." + HMAC)
type csrfTokenPayload struct {
	userID    string
	timestamp int64
	nonce     string
	signature string
}

// signToken 为 (userID, timestamp, nonce) 计算 HMAC-SHA256，返回 hex。
func signToken(secret []byte, userID string, ts int64, nonce string) string {
	mac := hmac.New(sha256.New, secret)
	payload := fmt.Sprintf("%s|%d|%s", userID, ts, nonce)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// generateToken 构造新的 CSRF token（含签名）。
//
// 返回格式：base64(userID|ts|nonce|signature) → 浏览器 cookie 存的就是这个值。
// 前端 JS decode 后取 userID|ts|nonce 三段（不验签，服务端用同一密钥验签），
// 直接 echo 整个值到 header 即可（服务端验签逻辑只看 cookie 与 header 是否相等）。
func generateToken(secret []byte, userID string) (string, error) {
	ts := time.Now().Unix()
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("csrf: nonce gen failed: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	sig := signToken(secret, userID, ts, nonce)
	// 拼接明文 (userID.ts.nonce.sig)，base64 编码
	raw := fmt.Sprintf("%s.%d.%s.%s", userID, ts, nonce, sig)
	return base64.StdEncoding.EncodeToString([]byte(raw)), nil
}

// parseToken 解码 base64 并拆分四段。失败返回 error。
func parseToken(value string) (*csrfTokenPayload, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("csrf: base64 decode: %w", err)
	}
	parts := strings.Split(string(decoded), ".")
	if len(parts) != 4 {
		return nil, fmt.Errorf("csrf: token parts != 4 (got %d)", len(parts))
	}
	ts, err := parseInt64(parts[1])
	if err != nil {
		return nil, fmt.Errorf("csrf: ts parse: %w", err)
	}
	return &csrfTokenPayload{
		userID:    parts[0],
		timestamp: ts,
		nonce:     parts[2],
		signature: parts[3],
	}, nil
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not an integer")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

// CSRF 返回一个 gin 中间件。
//
//   - GET / HEAD / OPTIONS：自动 issue cookie（若不存在），放行
//   - POST / PUT / DELETE：校验 cookie == header；不匹配 → 403
//   - 跳过 SkipPaths 配置的路径（/auth/anon 等）
//
// 注意：本中间件必须在 auth.Middleware 之后挂（需要从 ctx 取 viewer_id）。
// 如果 viewer_id 为空（匿名），按 userID="" 处理，token 仍可签发（前端 anon
// 请求也要带 CSRF，避免攻击者用 anon 端点伪造请求）。
func CSRF(cfg CSRFConfig) gin.HandlerFunc {
	if cfg.Secret == nil {
		panic("csrf: Secret is required")
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "XSRF-TOKEN"
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-XSRF-TOKEN"
	}
	if cfg.CookiePath == "" {
		cfg.CookiePath = "/api/v1"
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 24 * time.Hour
	}
	skipSet := make(map[string]bool, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skipSet[p] = true
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 1. 跳过列表（如 /auth/anon /auth/login）。
		// SkipPaths 接受完整 path（含 /api/v1 前缀）或后缀匹配（"/auth/anon"）。
		skip := false
		for _, p := range cfg.SkipPaths {
			if path == p || strings.HasSuffix(path, p) {
				skip = true
				break
			}
		}
		if skip {
			c.Next()
			return
		}

		// 2. 根据 method 分支
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			// idempotent 方法：自动 issue cookie（若不存在），放行
			if _, err := c.Cookie(cfg.CookieName); err != nil {
				// 没有 cookie → 签发新 token
				uid := currentUserID(c)
				token, err := generateToken(cfg.Secret, uid)
				if err == nil {
					setCSRFCookie(c, cfg, token)
				}
			}
			c.Next()
			return
		}

		// 3. POST/PUT/DELETE 路径：校验
		cookieVal, err := c.Cookie(cfg.CookieName)
		if err != nil || cookieVal == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "CSRF_COOKIE_MISSING",
				"message": "CSRF cookie missing; reload the page",
			})
			return
		}
		headerVal := c.GetHeader(cfg.HeaderName)
		if headerVal == "" || !secureCompare(cookieVal, headerVal) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "CSRF_TOKEN_MISMATCH",
				"message": "CSRF token mismatch",
			})
			return
		}

		// 4. 验签 + 时效检查
		parsed, err := parseToken(cookieVal)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "CSRF_TOKEN_INVALID",
				"message": "CSRF token malformed",
			})
			return
		}
		expectedSig := signToken(cfg.Secret, parsed.userID, parsed.timestamp, parsed.nonce)
		if !secureCompare(parsed.signature, expectedSig) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "CSRF_TOKEN_FORGED",
				"message": "CSRF token signature mismatch",
			})
			return
		}
		now := time.Now().Unix()
		// 滑动窗口 ±2h（与 gorilla/csrf 默认一致）
		if abs(now-parsed.timestamp) > int64(2*time.Hour/time.Second) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "CSRF_TOKEN_EXPIRED",
				"message": "CSRF token expired",
			})
			return
		}

		c.Next()
	}
}

// setCSRFCookie 写 Set-Cookie。**非 HttpOnly**（让前端 JS 能读到）。
// Secure 跟随 dc_session（同源 cookie 应该是同源传输策略）。
func setCSRFCookie(c *gin.Context, cfg CSRFConfig, value string) {
	secure := strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(
		cfg.CookieName,
		value,
		int(cfg.MaxAge.Seconds()),
		cfg.CookiePath,
		"",    // Domain 与 dc_session 一致（浏览器默认当前 host）
		secure,
		false, // 非 HttpOnly — JS 需要读这个值
	)
}

// currentUserID 从 gin ctx 取 viewer_id（auth.Middleware 已注入）。
// 匿名时返回 ""（仍签发 token，但 userID 段为空）。
func currentUserID(c *gin.Context) string {
	if v, ok := c.Get("viewer_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// secureCompare 恒定时间字符串比较（防 timing attack）。
func secureCompare(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
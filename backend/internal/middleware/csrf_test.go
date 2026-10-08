package middleware

// v2.5 (P1-2) CSRF middleware 双 cookie 模式测试。

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newTestServer(cfg CSRFConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(CSRF(cfg))
	// 模拟 auth 中间件：注入 viewer_id 到 ctx
	g.Use(func(c *gin.Context) {
		c.Set("viewer_id", "user-123")
		c.Next()
	})
	g.GET("/protected", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	g.POST("/protected", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	g.POST("/auth/anon", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	return r
}

func performReq(r *gin.Engine, method, path, cookieValue, headerValue string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: cookieValue})
	}
	if headerValue != "" {
		req.Header.Set("X-XSRF-TOKEN", headerValue)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// makeToken 手工构造 CSRF token（用于测试伪造场景）。
func makeToken(secret []byte, userID string, ts int64, nonce string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(fmt.Sprintf("%s|%d|%s", userID, ts, nonce)))
	sig := hex.EncodeToString(mac.Sum(nil))
	raw := fmt.Sprintf("%s.%d.%s.%s", userID, ts, nonce, sig)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// TestCSRF_GetIssuesCookie GET 自动 Set-Cookie XSRF-TOKEN。
func TestCSRF_GetIssuesCookie(t *testing.T) {
	t.Parallel()
	r := newTestServer(DefaultCSRFConfig([]byte("test-secret-32-chars-xxxxxxxxx")))
	w := performReq(r, "GET", "/api/v1/protected", "", "")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var found *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "XSRF-TOKEN" {
			found = ck
			break
		}
	}
	if found == nil {
		t.Fatal("expected Set-Cookie XSRF-TOKEN on GET")
	}
	if found.Value == "" {
		t.Error("XSRF-TOKEN cookie value should not be empty")
	}
	if found.HttpOnly {
		t.Error("XSRF-TOKEN 必须非 HttpOnly（前端 JS 读得到）")
	}
}

// TestCSRF_CookiePathMustBeReadableFromAppPages v2.11 (D19) 回归护栏。
//
// double-submit 依赖"前端 JS 读得到 cookie"这一步。而 `document.cookie` 只暴露
// **Path 是当前文档路径前缀**的 cookie：应用页面在 "/"、"/court/..."，所以
// cookie 的 Path 必须是 "/"（或至少是应用页面的前缀）。
//
// 历史 bug：Path 曾是 "/api/v1" → 前端 readCookie 恒为 null → 不发 X-XSRF-TOKEN 头
// → 后端对浏览器所有 POST/PUT/DELETE 回 403（立案/开庭/提交证据/action 全挂）。
// 这个缺陷单元测试测不出来（单测直接构造 cookie），只有实跑浏览器才会暴露，
// 所以这里把"Path 必须对 JS 可见"这件事本身钉成断言。
func TestCSRF_CookiePathMustBeReadableFromAppPages(t *testing.T) {
	t.Parallel()
	cfg := DefaultCSRFConfig([]byte("test-secret-32-chars-xxxxxxxxx"))
	if cfg.CookiePath != "/" {
		t.Fatalf("CookiePath 必须是 \"/\"，当前 %q —— 非根路径会让应用页面上的 "+
			"document.cookie 读不到 token，double-submit 整体失效（D19）", cfg.CookiePath)
	}

	r := newTestServer(cfg)
	w := performReq(r, "GET", "/api/v1/protected", "", "")
	var found *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "XSRF-TOKEN" {
			found = ck
			break
		}
	}
	if found == nil {
		t.Fatal("expected Set-Cookie XSRF-TOKEN on GET")
	}
	// 只有 Path 为 "/" 时，"/" 与 "/court/xxx" 这类应用页面才能读到它。
	if found.Path != "/" {
		t.Errorf("Set-Cookie 的 Path 必须为 \"/\"（应用页面在 / 与 /court/... 下），got %q", found.Path)
	}
}

// TestCSRF_UnescapedCookieValueRoundTrip v2.11 (D19) 配套护栏：
// 前端读 cookie 后是 `decodeURIComponent(value)`，再作为 header 回传。
//
// 这条链路之所以能对上，是因为 Go 在 **写** Set-Cookie 时会对值做 URL 转义
// （base64 的 `=` → `%3D`），在 **读** cookie 时又会 unescape —— 所以
// "前端 decodeURIComponent(cookie) == 服务端读到的 cookie"。
// 一旦有人改掉任意一端，这里就会失败，避免又变成静默 403。
func TestCSRF_UnescapedCookieValueRoundTrip(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-chars-xxxxxxxxx")
	r := newTestServer(DefaultCSRFConfig(secret))

	w := performReq(r, "GET", "/api/v1/protected", "", "")
	var raw string
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "XSRF-TOKEN" {
			raw = ck.Value
		}
	}
	if raw == "" {
		t.Fatal("no XSRF-TOKEN cookie issued")
	}

	// 前端视角：document.cookie 拿到的是**转义后**的值，readCookie 会 unescape。
	// httptest 的 Result().Cookies() 已经解析过，这里用 RawSetCookie 模拟原始串。
	var escaped string
	for _, line := range w.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(line, "XSRF-TOKEN=") {
			escaped = strings.TrimSuffix(strings.TrimPrefix(strings.Split(line, ";")[0], "XSRF-TOKEN="), "")
		}
	}
	if escaped == "" {
		t.Fatal("no raw Set-Cookie for XSRF-TOKEN")
	}
	decoded, err := url.QueryUnescape(escaped)
	if err != nil {
		t.Fatalf("unescape: %v", err)
	}

	// 前端把 decodeURIComponent 后的值放进 header；服务端读 cookie 时也会 unescape。
	// 两者必须相等 → 才能通过 secureCompare。用转义原值当 cookie 发送（浏览器行为）。
	w2 := performReq(r, "POST", "/api/v1/protected", escaped, decoded)
	if w2.Code != 200 {
		t.Fatalf("前端 decode 后的 header 必须能与 cookie 对上，got %d body=%s",
			w2.Code, w2.Body.String())
	}
}

// TestCSRF_PostRequiresHeader POST 必须带 X-XSRF-TOKEN header。
func TestCSRF_PostRequiresHeader(t *testing.T) {
	t.Parallel()
	r := newTestServer(DefaultCSRFConfig([]byte("test-secret-32-chars-xxxxxxxxx")))

	w := performReq(r, "POST", "/api/v1/protected", "", "")
	if w.Code != 403 {
		t.Errorf("no cookie+header: expected 403, got %d", w.Code)
	}
	w = performReq(r, "POST", "/api/v1/protected", "abc", "")
	if w.Code != 403 {
		t.Errorf("no header: expected 403, got %d", w.Code)
	}
}

// TestCSRF_PostSucceedsWithMatchingTokens cookie == header 通过。
func TestCSRF_PostSucceedsWithMatchingTokens(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-chars-xxxxxxxxx")
	r := newTestServer(DefaultCSRFConfig(secret))

	tok := makeToken(secret, "user-123", time.Now().Unix(), newNonce())
	w := performReq(r, "POST", "/api/v1/protected", tok, tok)
	if w.Code != 200 {
		t.Errorf("matching tokens: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestCSRF_PostRejectsMismatchedTokens cookie 与 header 不一致 → 403。
func TestCSRF_PostRejectsMismatchedTokens(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-chars-xxxxxxxxx")
	r := newTestServer(DefaultCSRFConfig(secret))

	tok := makeToken(secret, "user-123", time.Now().Unix(), newNonce())
	w := performReq(r, "POST", "/api/v1/protected", tok, "different-value")
	if w.Code != 403 {
		t.Errorf("mismatched tokens: expected 403, got %d", w.Code)
	}
}

// TestCSRF_PostRejectsForgedSignature 篡改 sig → 403。
func TestCSRF_PostRejectsForgedSignature(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-chars-xxxxxxxxx")
	otherSecret := []byte("different-secret-32-chars-yyyyy")
	r := newTestServer(DefaultCSRFConfig(secret))

	tok := makeToken(otherSecret, "user-123", time.Now().Unix(), newNonce())
	w := performReq(r, "POST", "/api/v1/protected", tok, tok)
	if w.Code != 403 {
		t.Errorf("forged sig: expected 403, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestCSRF_PostRejectsExpiredToken timestamp 超 ±2h 滑动窗口 → 403。
func TestCSRF_PostRejectsExpiredToken(t *testing.T) {
	t.Parallel()
	secret := []byte("test-secret-32-chars-xxxxxxxxx")
	r := newTestServer(DefaultCSRFConfig(secret))

	oldTs := time.Now().Unix() - 3*3600 // 3 小时前
	tok := makeToken(secret, "user-123", oldTs, newNonce())
	w := performReq(r, "POST", "/api/v1/protected", tok, tok)
	if w.Code != 403 {
		t.Errorf("expired token: expected 403, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestCSRF_SkipPathExempt /auth/anon 即使 POST 也不校验 CSRF。
func TestCSRF_SkipPathExempt(t *testing.T) {
	t.Parallel()
	r := newTestServer(DefaultCSRFConfig([]byte("test-secret-32-chars-xxxxxxxxx")))

	w := performReq(r, "POST", "/api/v1/auth/anon", "", "")
	if w.Code != 200 {
		t.Errorf("/auth/anon should skip CSRF, got %d", w.Code)
	}
}

// TestCSRF_PanicOnNilSecret 配置 nil secret 应该 panic。
func TestCSRF_PanicOnNilSecret(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil secret")
		}
	}()
	gin.SetMode(gin.TestMode)
	CSRF(CSRFConfig{Secret: nil})
}

// TestCSRF_RejectsMalformedTokenBase64 token 不是合法 base64 → 403。
func TestCSRF_RejectsMalformedTokenBase64(t *testing.T) {
	t.Parallel()
	r := newTestServer(DefaultCSRFConfig([]byte("test-secret-32-chars-xxxxxxxxx")))

	w := performReq(r, "POST", "/api/v1/protected", "not-base64-!!!", "not-base64-!!!")
	if w.Code != 403 {
		t.Errorf("malformed token: expected 403, got %d", w.Code)
	}
}

// TestCSRF_RejectsWrongPartCount base64 OK 但字段数 != 4 → 403。
func TestCSRF_RejectsWrongPartCount(t *testing.T) {
	t.Parallel()
	r := newTestServer(DefaultCSRFConfig([]byte("test-secret-32-chars-xxxxxxxxx")))

	bad := base64.StdEncoding.EncodeToString([]byte("only.two")) // 只有 2 段
	w := performReq(r, "POST", "/api/v1/protected", bad, bad)
	if w.Code != 403 {
		t.Errorf("wrong part count: expected 403, got %d", w.Code)
	}
}
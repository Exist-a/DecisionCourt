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
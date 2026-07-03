package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignAndParse_RoundTrip(t *testing.T) {
	const secret = "test-secret-1234567890"
	uid := "anon_test_abc"

	tok, err := Sign(secret, uid, time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	claims, err := Parse(secret, tok)
	require.NoError(t, err)
	assert.Equal(t, uid, claims.UserID)
}

func TestSign_EmptySecretFails(t *testing.T) {
	_, err := Sign("", "uid", time.Hour)
	assert.Error(t, err)
}

func TestSign_EmptyUserIDFails(t *testing.T) {
	_, err := Sign("secret", "", time.Hour)
	assert.Error(t, err)
}

func TestParse_EmptyTokenFails(t *testing.T) {
	_, err := Parse("secret", "")
	assert.Error(t, err)
}

func TestParse_WrongSecretFails(t *testing.T) {
	tok, _ := Sign("secret-A", "uid", time.Hour)
	_, err := Parse("secret-B", tok)
	assert.Error(t, err, "different secret must fail verification")
}

func TestParse_ExpiredTokenFails(t *testing.T) {
	tok, _ := Sign("secret", "uid", -time.Hour) // 1h in the past
	_, err := Parse("secret", tok)
	assert.Error(t, err, "expired token must fail")
}

func TestParse_AlgNoneRejected(t *testing.T) {
	// Manually craft an alg=none token to verify defense
	// (this is defense-in-depth; the library should already reject it)
	tok := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1c2VyX2lkIjoidWlkIn0."
	_, err := Parse("secret", tok)
	assert.Error(t, err, "alg=none must be rejected")
}

func TestMiddleware_RejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", Middleware("secret"), func(c *gin.Context) {
		c.String(200, ViewerFromContext(c))
	})

	req := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code, "no token must yield 401; body=%s", w.Body.String())
}

func TestMiddleware_AcceptsCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret"
	tok, _ := Sign(secret, "anon_xyz", time.Hour)

	r := gin.New()
	r.GET("/x", Middleware(secret), func(c *gin.Context) {
		c.String(200, ViewerFromContext(c))
	})

	req := httptest.NewRequest("GET", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "anon_xyz", w.Body.String())
}

func TestMiddleware_AcceptsBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret"
	tok, _ := Sign(secret, "anon_abc", time.Hour)

	r := gin.New()
	r.GET("/x", Middleware(secret), func(c *gin.Context) {
		c.String(200, ViewerFromContext(c))
	})

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "anon_abc", w.Body.String())
}

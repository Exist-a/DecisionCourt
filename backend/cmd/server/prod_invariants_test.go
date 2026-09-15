package main

// v2.4 (P1-7) APP_ENV=prod|staging 启动 invariant 测试。
//
// 验证 enforceProdInvariants 在 dev 配置漏到 prod 时能 fail-fast 拒绝。
// 这层保护防的是"用户把 .env 复制到 prod"或"忘了 APP_ENV=prod"导致
// dev-style 配置被误部署到公网。

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/config"
)

// withAppConfig 临时设置 config.AppConfig 用于单测。
func withAppConfig(t *testing.T, cfg config.Config) {
	t.Helper()
	original := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = original
	})
	config.AppConfig = cfg
}

func TestEnforceProdInvariants_OK(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{"https://yourdomain.com"},
		CookieSecure:   true,
		JWTSecret:      strings.Repeat("x", 32), // ≥ 32
		LLMAPIKey:      "sk-real-key-12345",
	})
	if err := enforceProdInvariants(); err != nil {
		t.Errorf("valid prod config should pass, got: %v", err)
	}
}

func TestEnforceProdInvariants_LocalhostOrigin(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{"http://localhost:3000"},
		CookieSecure:   true,
		JWTSecret:      strings.Repeat("x", 32),
		LLMAPIKey:      "sk-real-key",
	})
	err := enforceProdInvariants()
	if err == nil {
		t.Fatal("localhost origin should fail in prod")
	}
	if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
		t.Errorf("error should mention ALLOWED_ORIGINS, got: %v", err)
	}
}

func TestEnforceProdInvariants_LoopbackIP(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{"https://yourdomain.com", "http://127.0.0.1:3000"},
		CookieSecure:   true,
		JWTSecret:      strings.Repeat("x", 32),
		LLMAPIKey:      "sk-real-key",
	})
	err := enforceProdInvariants()
	if err == nil {
		t.Fatal("127.0.0.1 origin should fail in prod")
	}
}

func TestEnforceProdInvariants_EmptyOrigins(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{},
		CookieSecure:   true,
		JWTSecret:      strings.Repeat("x", 32),
		LLMAPIKey:      "sk-real-key",
	})
	err := enforceProdInvariants()
	if err == nil {
		t.Fatal("empty origins should fail in prod")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got: %v", err)
	}
}

func TestEnforceProdInvariants_CookieInsecure(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{"https://yourdomain.com"},
		CookieSecure:   false, // dev fallback
		JWTSecret:      strings.Repeat("x", 32),
		LLMAPIKey:      "sk-real-key",
	})
	err := enforceProdInvariants()
	if err == nil {
		t.Fatal("COOKIE_SECURE=false should fail in prod")
	}
	if !strings.Contains(err.Error(), "COOKIE_SECURE") {
		t.Errorf("error should mention COOKIE_SECURE, got: %v", err)
	}
}

func TestEnforceProdInvariants_ShortJWTSecret(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{"https://yourdomain.com"},
		CookieSecure:   true,
		JWTSecret:      "dev-secret", // 10 chars
		LLMAPIKey:      "sk-real-key",
	})
	err := enforceProdInvariants()
	if err == nil {
		t.Fatal("short JWT_SECRET should fail in prod")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error should mention JWT_SECRET, got: %v", err)
	}
}

func TestEnforceProdInvariants_EmptyLLMKey(t *testing.T) {
	t.Parallel()
	withAppConfig(t, config.Config{
		AllowedOrigins: []string{"https://yourdomain.com"},
		CookieSecure:   true,
		JWTSecret:      strings.Repeat("x", 32),
		LLMAPIKey:      "", // dev 空 key
	})
	err := enforceProdInvariants()
	if err == nil {
		t.Fatal("empty LLM_API_KEY should fail in prod")
	}
	if !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Errorf("error should mention LLM_API_KEY, got: %v", err)
	}
}
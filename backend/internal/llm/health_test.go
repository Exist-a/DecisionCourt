package llm

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/config"
)

// TestMaskKey 覆盖 maskKey 函数 5 case:
//   - 空 key
//   - 标准 11+ 字符 key → 返回 "sk-***f456" 掩码
//   - 1 字符 → "***" (太短整体掩码)
//   - 7 字符临界 → "***"
//   - 8 字符 (刚好够) → 返回前 3 + *** + 后 4 (注意可能前 3 == 后 4 重叠)
func TestMaskKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"empty key", "", "***"},
		{"1 char", "x", "***"},
		{"7 chars boundary", "sk-1234", "***"},
		{"11 chars standard", "sk-abc123def456", "sk-***f456"},
		{"24 chars long", "sk-prod-deepseek-2026-secret-key", "sk-***-key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := maskKey(c.key)
			if got != c.want {
				t.Errorf("maskKey(%q) = %q, want %q", c.key, got, c.want)
			}
		})
	}
}

// TestMaskKeyNeverReturnsFull 兜底测试: 无论输入什么, maskKey 输出永远不应包含完整 key。
// 这是 AGENTS.md §8 红线的最终防线: 即使有人误把 key 传给 maskKey, 也不应回显全值。
func TestMaskKeyNeverReturnsFull(t *testing.T) {
	secrets := []string{
		"sk-abc123def456",
		"sk-prod-deepseek-2026-secret-key",
		"sk-12345678",
		"sk-abcdef",
	}
	for _, s := range secrets {
		got := maskKey(s)
		if got == s {
			t.Errorf("maskKey(%q) returned full key (security violation)", s)
		}
		if strings.Contains(got, s) {
			t.Errorf("maskKey(%q) output %q contains full key (security violation)", s, got)
		}
	}
}

// TestGetHealthKeyPreviewNeverContainsFullKey 端到端测试:
// GetHealth() 返回的 JSON 永远不应包含 LLM_API_KEY 完整值。
//
// 配置 LLM_API_KEY 为测试值, 然后 marshal GetHealth() 检查所有字段拼接不含完整值。
func TestGetHealthKeyPreviewNeverContainsFullKey(t *testing.T) {
	const testKey = "sk-test-integration-secret-do-not-leak"
	config.AppConfig.LLMAPIKey = testKey
	config.AppConfig.LLMProvider = "deepseek"
	config.AppConfig.LLMModelV3 = "deepseek-v4-flash"

	h := GetHealth()
	allFields := h.Provider + h.Model + h.KeyPreview + boolToStr(h.Configured)

	if strings.Contains(allFields, testKey) {
		t.Errorf("GetHealth() output contains full key (security violation): %+v", h)
	}
	if !h.Configured {
		t.Errorf("expected Configured=true when key set")
	}
	// KeyPreview 应为 maskKey 输出: "sk-***eak"
	if h.KeyPreview == "" {
		t.Errorf("expected non-empty KeyPreview when key set")
	}
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestGetHealthEmptyKey 当 LLM_API_KEY 未设置时返回 Configured=false。
func TestGetHealthEmptyKey(t *testing.T) {
	config.AppConfig.LLMAPIKey = ""
	config.AppConfig.LLMProvider = "deepseek"
	config.AppConfig.LLMModelV3 = "deepseek-v4-flash"

	h := GetHealth()
	if h.Configured {
		t.Errorf("expected Configured=false when key empty")
	}
	if h.KeyPreview != "" {
		t.Errorf("expected empty KeyPreview when key empty, got %q", h.KeyPreview)
	}
}
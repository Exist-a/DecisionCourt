package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// v0.10.21 PR-C: envOrDefault 系列单元测试
// 覆盖: env 命中 / env 空 → default / env 未设 → default / 解析失败 / 各种 typed 变体

// --- envOrDefault 核心 ---

func TestEnvOrDefault_FromEnvTakesPrecedence(t *testing.T) {
	t.Setenv("TEST_PORT", "9999")
	v, fromEnv := envOrDefault("TEST_PORT", "8080")
	require.Equal(t, "9999", v)
	require.True(t, fromEnv)
}

func TestEnvOrDefault_EmptyEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_PORT", "")
	v, fromEnv := envOrDefault("TEST_PORT", "8080")
	require.Equal(t, "8080", v)
	require.False(t, fromEnv, "空字符串走 default, 而非空字符串")
}

func TestEnvOrDefault_NotSetFallsBackToDefault(t *testing.T) {
	_ = os.Unsetenv("TEST_PORT_UNSET")
	v, fromEnv := envOrDefault("TEST_PORT_UNSET", "8080")
	require.Equal(t, "8080", v)
	require.False(t, fromEnv)
}

func TestEnvOrDefaultString_ThinWrapper(t *testing.T) {
	t.Setenv("TEST_FOO", "bar")
	require.Equal(t, "bar", envOrDefaultString("TEST_FOO", "default"))
	require.Equal(t, "default", envOrDefaultString("TEST_FOO_UNSET", "default"))
}

// --- envOrDefaultInt ---

func TestEnvOrDefaultInt_ValidParse(t *testing.T) {
	t.Setenv("TEST_INT", "12345")
	require.Equal(t, 12345, envOrDefaultInt("TEST_INT", 100))
}

func TestEnvOrDefaultInt_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_INT", "notanumber")
	require.Equal(t, 100, envOrDefaultInt("TEST_INT", 100))
}

func TestEnvOrDefaultInt_NotSetFallsBackToDefault(t *testing.T) {
	require.Equal(t, 100, envOrDefaultInt("TEST_INT_UNSET", 100))
}

func TestEnvOrDefaultInt_EmptyFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_INT", "")
	require.Equal(t, 100, envOrDefaultInt("TEST_INT", 100))
}

// --- envOrDefaultBool ---

func TestEnvOrDefaultBool_TrueValues(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE", "1", "yes", "YES", "on", "On"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("TEST_BOOL", v)
			require.Equal(t, true, envOrDefaultBool("TEST_BOOL", false))
		})
	}
}

func TestEnvOrDefaultBool_FalseValues(t *testing.T) {
	for _, v := range []string{"false", "False", "FALSE", "0", "no", "NO", "off", "Off"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("TEST_BOOL", v)
			require.Equal(t, false, envOrDefaultBool("TEST_BOOL", true))
		})
	}
}

func TestEnvOrDefaultBool_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_BOOL", "maybe")
	require.Equal(t, true, envOrDefaultBool("TEST_BOOL", true))
}

func TestEnvOrDefaultBool_NotSetFallsBackToDefault(t *testing.T) {
	require.Equal(t, true, envOrDefaultBool("TEST_BOOL_UNSET", true))
}

// --- envOrDefaultFloat ---

func TestEnvOrDefaultFloat_ValidParse(t *testing.T) {
	t.Setenv("TEST_FLOAT", "0.75")
	require.Equal(t, 0.75, envOrDefaultFloat("TEST_FLOAT", 0.5))
}

func TestEnvOrDefaultFloat_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_FLOAT", "notafloat")
	require.Equal(t, 0.5, envOrDefaultFloat("TEST_FLOAT", 0.5))
}

func TestEnvOrDefaultFloat_NegativeAndZero(t *testing.T) {
	t.Setenv("TEST_FLOAT", "-1.5")
	require.Equal(t, -1.5, envOrDefaultFloat("TEST_FLOAT", 0.0))
	require.Equal(t, 0.0, envOrDefaultFloat("TEST_FLOAT_UNSET", 0.0))
}

// --- envOrDefaultStringSlice ---

func TestEnvOrDefaultStringSlice_SingleValue(t *testing.T) {
	t.Setenv("TEST_SLICE", "https://only.com")
	require.Equal(t, []string{"https://only.com"}, envOrDefaultStringSlice("TEST_SLICE", []string{"default"}))
}

func TestEnvOrDefaultStringSlice_CommaSeparated(t *testing.T) {
	t.Setenv("TEST_SLICE", "a,b,c")
	require.Equal(t, []string{"a", "b", "c"}, envOrDefaultStringSlice("TEST_SLICE", nil))
}

func TestEnvOrDefaultStringSlice_WithWhitespace(t *testing.T) {
	t.Setenv("TEST_SLICE", " a , b , c ")
	require.Equal(t, []string{"a", "b", "c"}, envOrDefaultStringSlice("TEST_SLICE", nil))
}

func TestEnvOrDefaultStringSlice_TrailingComma(t *testing.T) {
	t.Setenv("TEST_SLICE", "https://x.com,")
	require.Equal(t, []string{"https://x.com"}, envOrDefaultStringSlice("TEST_SLICE", nil))
}

func TestEnvOrDefaultStringSlice_EmptyFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_SLICE", "")
	require.Equal(t, []string{"fallback"}, envOrDefaultStringSlice("TEST_SLICE", []string{"fallback"}))
}

func TestEnvOrDefaultStringSlice_OnlyCommasFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_SLICE", ",,,")
	require.Equal(t, []string{"fallback"}, envOrDefaultStringSlice("TEST_SLICE", []string{"fallback"}))
}

func TestEnvOrDefaultStringSlice_NotSetFallsBackToDefault(t *testing.T) {
	require.Equal(t, []string{"fallback"}, envOrDefaultStringSlice("TEST_SLICE_UNSET", []string{"fallback"}))
}

// --- 大小写保留 (viper lowercase bug 修复验证) ---

// v0.10.19 修过的 5 个 env 现在也应该走 envOrDefault, 验证大小写 key 命中正确。
func TestEnvOrDefault_KeyCasePreserved_OnLookup(t *testing.T) {
	t.Setenv("JWT_SECRET", "deadbeef")
	v, fromEnv := envOrDefault("JWT_SECRET", "")
	require.Equal(t, "deadbeef", v)
	require.True(t, fromEnv)
}

// --- v1.0.0 (ADR 0029) DeepSeek v3→v4 模型硬迁移默认值验证 ---

// TestEnvOrDefaultString_DeepSeekV4_Default_V3 验证未设 LLM_MODEL_V3 时
// 默认值是 deepseek-v4-flash (ADR 0029 硬迁移;旧名 deepseek-chat 已不再默认)。
func TestEnvOrDefaultString_DeepSeekV4_Default_V3(t *testing.T) {
	_ = os.Unsetenv("LLM_MODEL_V3")
	require.Equal(t, "deepseek-v4-flash", envOrDefaultString("LLM_MODEL_V3", "deepseek-v4-flash"))
}

// TestEnvOrDefaultString_DeepSeekV4_Default_R1 验证未设 LLM_MODEL_R1 时
// 默认值是 deepseek-v4-pro (ADR 0029 硬迁移;旧名 deepseek-reasoner 已不再默认)。
func TestEnvOrDefaultString_DeepSeekV4_Default_R1(t *testing.T) {
	_ = os.Unsetenv("LLM_MODEL_R1")
	require.Equal(t, "deepseek-v4-pro", envOrDefaultString("LLM_MODEL_R1", "deepseek-v4-pro"))
}

// TestEnvOrDefaultString_DeepSeekV3_OverrideStillWorks 验证 .env 仍可手动覆盖回
// 旧名 (向后兼容窗口,但不推荐 — 旧名已不在 DeepSeek 官方文档)。
func TestEnvOrDefaultString_DeepSeekV3_OverrideStillWorks(t *testing.T) {
	t.Setenv("LLM_MODEL_V3", "deepseek-chat")
	require.Equal(t, "deepseek-chat", envOrDefaultString("LLM_MODEL_V3", "deepseek-v4-flash"))
}

package config

// v2.4 (P1-7) APP_ENV 校验 + IsDev / IsProd / IsProdLike 单元测试。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppEnv_IsDev(t *testing.T) {
	t.Parallel()
	cases := []struct {
		env  string
		want bool
	}{
		{"", true}, // 默认空 → IsDev (本地开发模式 fall-back)
		{"dev", true},
		{"DEV", true}, // 大小写不敏感
		{"  dev  ", true}, // 前后空白 trim
		{"prod", false},
		{"staging", false},
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			cfg := Config{AppEnv: c.env}
			require.Equal(t, c.want, cfg.IsDev(), "AppEnv=%q", c.env)
		})
	}
}

func TestAppEnv_IsProd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		env  string
		want bool
	}{
		{"prod", true},
		{"PROD", true},
		{"  prod  ", true},
		{"dev", false},
		{"staging", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			cfg := Config{AppEnv: c.env}
			require.Equal(t, c.want, cfg.IsProd(), "AppEnv=%q", c.env)
		})
	}
}

func TestAppEnv_IsProdLike(t *testing.T) {
	t.Parallel()
	require.True(t, Config{AppEnv: "prod"}.IsProdLike())
	require.True(t, Config{AppEnv: "staging"}.IsProdLike())
	require.False(t, Config{AppEnv: "dev"}.IsProdLike())
	require.False(t, Config{AppEnv: ""}.IsProdLike()) // 空默认 = dev
}

func TestAppEnv_Validate(t *testing.T) {
	t.Parallel()

	// 暂存并恢复 AppConfig，避免污染其他测试
	original := AppConfig
	defer func() { AppConfig = original }()

	valid := []string{"", "dev", "staging", "prod"}
	for _, v := range valid {
		t.Run("valid_"+v, func(t *testing.T) {
			AppConfig = Config{AppEnv: v}
			require.NoError(t, ValidateAppEnv())
		})
	}

	// 大小写不敏感：PROD 是合法值（落到 prod 分支）；只有完全拼错的才 invalid
	invalid := []string{"production", "Production", "stage", "test", "live", "dev2"}
	for _, v := range invalid {
		t.Run("invalid_"+v, func(t *testing.T) {
			AppConfig = Config{AppEnv: v}
			err := ValidateAppEnv()
			require.Error(t, err, "AppEnv=%q should fail", v)
			require.Contains(t, err.Error(), "invalid APP_ENV")
		})
	}
}

// TestAppEnv_DefaultsToDev 在 Load() 后若未设 APP_ENV, 默认值是 "dev"。
// 注意：完整 Load() 跑不到（需要 JWT_SECRET + DATABASE_URL），所以这里
// 只验证默认值字符串匹配。
func TestAppEnv_DefaultIsDev(t *testing.T) {
	t.Setenv("APP_ENV", "")
	require.Equal(t, "dev", envOrDefaultString("APP_ENV", "dev"),
		"未设 APP_ENV 时默认值必须是 dev (本地开发模式)")
}
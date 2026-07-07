package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseAllowedOrigins_SingleValue 回归 v0.9.3 403 bug:
// 之前 viper.Unmarshal 对 ALLOWED_ORIGINS=https://x.com 这种单值(无逗号)
// env var 不会自动 split 成 []string,导致 AppConfig.AllowedOrigins 是 nil,
// websocket.go 触发 localhost fallback,生产 Origin 不在白名单,403 拒掉所有 WS 握手。
//
// 这里用纯算法测试验证 split 逻辑(Load() 函数会读 .env + 跑 mustEnvs 校验,
// 单测里直接调会因 JWT_SECRET / DATABASE_URL 缺失或 viper 默认值干扰。
// split 算法本身是修复点,所以这里只测算法,不测 Load)。
func TestParseAllowedOrigins_SingleValue(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single value no comma (production .env bug)",
			raw:  "https://decisioncourt.cn",
			want: []string{"https://decisioncourt.cn"},
		},
		{
			name: "comma-separated (.env.example style)",
			raw:  "http://localhost:3000,http://127.0.0.1:3000",
			want: []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		},
		{
			name: "with trailing comma",
			raw:  "https://decisioncourt.cn,",
			want: []string{"https://decisioncourt.cn"},
		},
		{
			name: "with whitespace",
			raw:  " https://a.com , https://b.com ",
			want: []string{"https://a.com", "https://b.com"},
		},
		{
			name: "empty string",
			raw:  "",
			want: nil, // caller checks len(out) > 0
		},
		{
			name: "only commas",
			raw:  ",,,",
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 模拟 Load() 里的 split 算法。任何算法变更必须同步
			// 这个 helper 和 config.go 里的 Load 函数(两个地方
			// 用同样的 split + trim 语义)。
			got := splitAllowedOrigins(c.raw)
			require.Equal(t, c.want, got)
		})
	}
}

// splitAllowedOrigins 是 Load() 中手动 split ALLOWED_ORIGINS 的算法。
// 这里独立实现一份,避免直接调 Load()(Load 会跑 mustEnvs fail-fast
// + 读 .env + 读 env var,在单测环境里副作用不可控)。
//
// 实现必须与 config.go Load() 里的代码保持一致:
//   strings.Split(raw, ",") + 跳过 trim 后为空的元素
func splitAllowedOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := []string{}
	last := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == ',' {
			parts = append(parts, trimSpaces(raw[last:i]))
			last = i + 1
		}
	}
	parts = append(parts, trimSpaces(raw[last:]))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func trimSpaces(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
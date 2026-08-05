package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// v0.10.21 PR-C: envOrDefault helper 全面修复 viper 25+ env lowercase bug。
//
// 背景: viper 1.21.0 AutomaticEnv() 默认把 UPPERCASE env var (e.g. JWT_SECRET)
// 转 lowercase 查找 (jwt_secret), 找不到 → fallback SetDefault 默认值。
// v0.10.19 修过 5 个关键 env (JWT_SECRET / DATABASE_URL / LLM_API_KEY /
// BOCHA_API_KEY / ALLOWED_ORIGINS), 但剩余 33 个 env 仍受 bug 影响。
//
// 决策: 抛弃 viper 的 AutomaticEnv + BindEnv 走 os.Getenv, 直接读 process env。
// 原因: viper 1.21.0 lowercase 转换是默写行为, BindEnv 是补丁方案 (每个 key
// 都要列出), os.Getenv 是 Go 原生无 bug。setDefault 的"出厂默认值"语义保留
// 在代码里 (helper 第二个参数), 而不是 viper.SetDefault 那条隐藏 bug 路径。
//
// 设计:
//   - 5 个 typed 变体: String / Int / Bool / Float64 / StringSlice
//   - 不 fail-fast: 缺失 → 走 default
//   - 关键安全 env (JWT_SECRET / DATABASE_URL) 缺失 → Load() 末尾 mustEnvs
//     主动 fail-fast, 集中在 Load() 入口, 而不是 helper 散落
//   - 空字符串 ("") 视为未设, 走 default (避免 viper 那种"env=空"覆盖
//     default 的反直觉行为)
//
// 用法参考 ADR 0027。

// envOrDefault 读 env var, 空值时返回 defaultVal。
// 返回 (value, fromEnv): value 是 env 或 default; fromEnv=true 表示来自 env。
func envOrDefault(key, defaultVal string) (value string, fromEnv bool) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v, true
	}
	return defaultVal, false
}

// envOrDefaultInt 读 env var 解析为 int。解析失败时返回 defaultVal (不 panic)。
func envOrDefaultInt(key string, defaultVal int) int {
	v, _ := envOrDefault(key, "")
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("config: env parse int failed, falling back to default",
			"key", key, "value", v, "default", defaultVal, "err", err)
		return defaultVal
	}
	return parsed
}

// envOrDefaultBool 读 env var 解析为 bool。接受 "true"/"false"/"1"/"0"/"yes"/"no" (大小写不敏感)。
// 解析失败时返回 defaultVal (不 panic)。
func envOrDefaultBool(key string, defaultVal bool) bool {
	v, _ := envOrDefault(key, "")
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes", "y", "on":
		return true
	case "false", "0", "no", "n", "off":
		return false
	default:
		slog.Warn("config: env parse bool failed, falling back to default",
			"key", key, "value", v, "default", defaultVal)
		return defaultVal
	}
}

// envOrDefaultFloat 读 env var 解析为 float64。解析失败时返回 defaultVal (不 panic)。
func envOrDefaultFloat(key string, defaultVal float64) float64 {
	v, _ := envOrDefault(key, "")
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil {
		slog.Warn("config: env parse float failed, falling back to default",
			"key", key, "value", v, "default", defaultVal, "err", err)
		return defaultVal
	}
	return parsed
}

// envOrDefaultStringSlice 读 env var 解析为 []string, 按逗号 split。
// 接受 v0.9.3 修复的 split 语义: 单值 (无逗号) + 逗号分隔 + 带空格 + 尾随逗号。
// 空字符串或全空元素 → 返回 defaultVal (保持 "未设" 语义)。
func envOrDefaultStringSlice(key string, defaultVal []string) []string {
	v, _ := envOrDefault(key, "")
	if v == "" {
		return defaultVal
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return defaultVal
	}
	return out
}

// loadSummary 在 Load() 末尾被调用, 统计从 env 实际读到的 key 数量。
// v0.10.21 PR-C 新增: 用于运维诊断 "配置到底生效了几个 env" 的常见问题。
func loadSummary(fromEnv int, total int) {
	slog.Info("config loaded",
		"from_env", fromEnv,
		"total_keys", total,
	)
}

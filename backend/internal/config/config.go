package config

import (
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type AgentGatewayConfig struct {
	Enabled              bool    `mapstructure:"AGENT_GATEWAY_ENABLED"`
	PromptCompression    bool    `mapstructure:"AGENT_GATEWAY_PROMPT_COMPRESSION"`
	TokenBudget          bool    `mapstructure:"AGENT_GATEWAY_TOKEN_BUDGET"`
	Throttling           bool    `mapstructure:"AGENT_GATEWAY_THROTTLING"`
	Fallback             bool    `mapstructure:"AGENT_GATEWAY_FALLBACK"`
	FileLogger           bool    `mapstructure:"AGENT_GATEWAY_FILE_LOGGER"`
	BudgetPerSession     int     `mapstructure:"AGENT_GATEWAY_BUDGET_PER_SESSION"`
	CompressionThreshold float64 `mapstructure:"AGENT_GATEWAY_COMPRESSION_THRESHOLD"`
	ThrottlingThreshold  float64 `mapstructure:"AGENT_GATEWAY_THROTTLING_THRESHOLD"`
	LogDir               string  `mapstructure:"AGENT_GATEWAY_LOG_DIR"`

	// === Token Budget v2 ===
	RejectWhenExhausted     bool `mapstructure:"AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED"`
	BudgetSlidingWindowSec  int  `mapstructure:"AGENT_GATEWAY_BUDGET_SLIDING_WINDOW_SEC"`

	// === Prompt Compression v2 ===
	SmartCompression        bool    `mapstructure:"AGENT_GATEWAY_SMART_COMPRESSION"`
	KeepRecentForcedN       int     `mapstructure:"AGENT_GATEWAY_KEEP_RECENT_FORCED_N"`
	SummaryInsertThreshold  int     `mapstructure:"AGENT_GATEWAY_SUMMARY_INSERT_THRESHOLD"`
	ScoreThreshold          float64 `mapstructure:"AGENT_GATEWAY_SCORE_THRESHOLD"`

	// === v0.9 LLM Gateway 工程化 (ADR 0013 §决策 1) ===
	// LLMTimeoutSec: 每次 LLM 调用的硬超时（秒）。默认 90 —— 阿里云 ECS
	// → DeepSeek 跨网调用 P95 ≈ 25s + R1 推理模型余量。本地开发可调小到 30。
	LLMTimeoutSec           int     `mapstructure:"AGENT_GATEWAY_LLM_TIMEOUT_SEC"`

	// === v0.9 LLM Gateway 工程化 (ADR 0013 §决策 2) ===
	// CacheEnabled: 启用 LLM Response Cache（sync.Map + LRU + TTL）。
	// CacheTTLSec: 缓存 entry 过期时间（秒），0 → 300（5min）。
	// CacheMaxEntries: LRU 上限 entry 数,0 → 10000（约 2GB 内存）。
	CacheEnabled            bool    `mapstructure:"AGENT_GATEWAY_CACHE_ENABLED"`
	CacheTTLSec             int     `mapstructure:"AGENT_GATEWAY_CACHE_TTL_SEC"`
	CacheMaxEntries         int     `mapstructure:"AGENT_GATEWAY_CACHE_MAX_ENTRIES"`

	// === v0.9 LLM Gateway 工程化 (ADR 0013 §决策 3) ===
	// Circuit Breaker 配置。详见 ADR 0013 + breaker.go。
	BreakerEnabled             bool    `mapstructure:"AGENT_GATEWAY_BREAKER_ENABLED"`
	BreakerFailureRatio        float64 `mapstructure:"AGENT_GATEWAY_BREAKER_FAILURE_RATIO"`
	BreakerMinRequests         int     `mapstructure:"AGENT_GATEWAY_BREAKER_MIN_REQUESTS"`
	BreakerOpenTimeoutSec      int     `mapstructure:"AGENT_GATEWAY_BREAKER_OPEN_TIMEOUT_SEC"`
	BreakerHalfOpenMaxRequests int     `mapstructure:"AGENT_GATEWAY_BREAKER_HALF_OPEN_MAX_REQUESTS"`
}

type Config struct {
	Port        string `mapstructure:"PORT"`
	DatabaseURL string `mapstructure:"DATABASE_URL"`
	RedisURL    string `mapstructure:"REDIS_URL"`

	LLMProvider string `mapstructure:"LLM_PROVIDER"`
	LLMAPIKey   string `mapstructure:"LLM_API_KEY"`
	LLMBaseURL  string `mapstructure:"LLM_BASE_URL"`
	LLMModelV3  string `mapstructure:"LLM_MODEL_V3"`
	LLMModelR1  string `mapstructure:"LLM_MODEL_R1"`

	SearchProvider string `mapstructure:"SEARCH_PROVIDER"`
	TavilyAPIKey   string `mapstructure:"TAVILY_API_KEY"`
	BochaAPIKey    string `mapstructure:"BOCHA_API_KEY"`

	// v0.8.3 安全：JWTSecret 必填(无默认值,启动时校验)。JWTExpiryHours
	// 默认 168 = 7 天,CookieSecure 默认 true(生产 HTTPS);CookieDomain/SameSite
	// 用于跨域/iframe 场景,默认 SameSite=Lax。
	JWTSecret       string        `mapstructure:"JWT_SECRET"`
	JWTExpiryHours  int           `mapstructure:"JWT_EXPIRY_HOURS"`
	CookieSecure    bool          `mapstructure:"COOKIE_SECURE"`
	CookieSameSite  string        `mapstructure:"COOKIE_SAME_SITE"`
	CookieDomain    string        `mapstructure:"COOKIE_DOMAIN"`
	AllowedOrigins  []string      `mapstructure:"ALLOWED_ORIGINS"`

	// === v0.9 用户限流 (ADR 0014) ===
	// UserTrialLimit 每用户每天（UTC）最多 StartTrial 次数。
	//   - 默认 5（测试阶段保守值;生产可调到 20）
	//   - 0 → 禁用限流（紧急回滚用）
	UserTrialLimit int `mapstructure:"USER_TRIAL_LIMIT"`

	AgentGateway AgentGatewayConfig `mapstructure:",squash"`
}

var AppConfig Config

func Load() {
	// 1. 读 .env (导出到 process env, 然后 os.Getenv 即可读到)
	// v0.10.21 PR-C: 不再用 viper.Get/Set/Unmarshal 读 env, 改用 envOrDefault
	// 直接读 process env。viper 在这里只承担 "export .env 到 process env" 的角色。
	envPath := filepath.Join(getProjectRoot(), ".env")
	if _, err := os.Stat(envPath); err == nil {
		viper.SetConfigFile(envPath)
		if err := viper.ReadInConfig(); err != nil {
			slog.Warn("config: failed to read .env file", "err", err)
		} else {
			// viper 1.21.0: ReadInConfig 把 .env 内容 set 到 viper 内部 store,
			// 但不会自动 export 到 process env (viper.AutomaticEnv 默认 disable 这是 bug)。
			// 这里手动 export, 让 envOrDefault 后续能读到。失败不致命, 直接走 process env。
			viper.AutomaticEnv() // 触发 env key replacer setup
			for _, key := range viper.AllKeys() {
				val := viper.GetString(key)
				if val != "" {
					_ = os.Setenv(key, val)
				}
			}
		}
	}

	// 2. 完全抛弃 viper env lookup, 改用 envOrDefault 直接读 env
	// v0.10.21 PR-C: 33 env 全部走 envOrDefault (含 v0.10.19 修过的 5 个关键 env,
	// 因为现在所有 env 走同一条路径, 没必要单独 BindEnv 了)。
	// P0-2 安全: JWT_SECRET / DATABASE_URL 不设 default, 缺失由 mustEnvs fail-fast。
	AppConfig = Config{
		Port:           envOrDefaultString("PORT", "8080"),
		DatabaseURL:    envOrDefaultString("DATABASE_URL", ""),
		RedisURL:       envOrDefaultString("REDIS_URL", ""),

		LLMProvider:    envOrDefaultString("LLM_PROVIDER", "deepseek"),
		LLMAPIKey:      envOrDefaultString("LLM_API_KEY", ""),
		// Default to DeepSeek; set LLM_BASE_URL to https://api.moonshot.cn/v1 for Kimi
		LLMBaseURL:     envOrDefaultString("LLM_BASE_URL", "https://api.deepseek.com/v1"),
		// v1.0.0 (ADR 0029): DeepSeek 官方文档 2026-08-20 仅列 deepseek-v4-flash / deepseek-v4-pro,
		// 旧名 deepseek-chat / deepseek-reasoner 已不在文档;v4-flash 对应原 deepseek-chat 用法(常规对话),
		// v4-pro 对应原 deepseek-reasoner 用法(深度推理)。.env 仍可手动覆盖回旧名(短时兼容),但不建议。
		LLMModelV3:     envOrDefaultString("LLM_MODEL_V3", "deepseek-v4-flash"),
		LLMModelR1:     envOrDefaultString("LLM_MODEL_R1", "deepseek-v4-pro"),

		SearchProvider: envOrDefaultString("SEARCH_PROVIDER", "searxng"),
		TavilyAPIKey:   envOrDefaultString("TAVILY_API_KEY", ""),
		BochaAPIKey:    envOrDefaultString("BOCHA_API_KEY", ""),
		// SEARXNG_URL: dead config (无 mapstructure tag, 无人用 viper.Get), 跳。

		// JWT 关键配置
		JWTSecret:      envOrDefaultString("JWT_SECRET", ""),
		JWTExpiryHours: envOrDefaultInt("JWT_EXPIRY_HOURS", 168),
		CookieSecure:   envOrDefaultBool("COOKIE_SECURE", true),
		CookieSameSite: envOrDefaultString("COOKIE_SAME_SITE", "lax"),
		CookieDomain:   envOrDefaultString("COOKIE_DOMAIN", ""),

		// v0.9.3 修复: ALLOWED_ORIGINS 单值字符串, envOrDefaultStringSlice 内部 split。
		// 当 .env / env 都未设时, 退到 ["http://localhost:3000"] (dev fallback)。
		AllowedOrigins: envOrDefaultStringSlice("ALLOWED_ORIGINS", []string{"http://localhost:3000"}),

		// v0.9 用户限流 (ADR 0014): 0 → 禁用限流
		UserTrialLimit: envOrDefaultInt("USER_TRIAL_LIMIT", 5),

		// Agent Gateway 22 个 env
		AgentGateway: AgentGatewayConfig{
			Enabled:              envOrDefaultBool("AGENT_GATEWAY_ENABLED", false),
			PromptCompression:    envOrDefaultBool("AGENT_GATEWAY_PROMPT_COMPRESSION", false),
			TokenBudget:          envOrDefaultBool("AGENT_GATEWAY_TOKEN_BUDGET", false),
			Throttling:           envOrDefaultBool("AGENT_GATEWAY_THROTTLING", false),
			Fallback:             envOrDefaultBool("AGENT_GATEWAY_FALLBACK", false),
			FileLogger:           envOrDefaultBool("AGENT_GATEWAY_FILE_LOGGER", true),
			BudgetPerSession:     envOrDefaultInt("AGENT_GATEWAY_BUDGET_PER_SESSION", 20000),
			CompressionThreshold: envOrDefaultFloat("AGENT_GATEWAY_COMPRESSION_THRESHOLD", 0.7),
			ThrottlingThreshold:  envOrDefaultFloat("AGENT_GATEWAY_THROTTLING_THRESHOLD", 0.8),
			LogDir:               envOrDefaultString("AGENT_GATEWAY_LOG_DIR", "logs"),

			// Token Budget v2 — REJECT_WHEN_EXHAUSTED 默认 true (v0.10.18 改)
			RejectWhenExhausted:    envOrDefaultBool("AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED", true),
			BudgetSlidingWindowSec:  envOrDefaultInt("AGENT_GATEWAY_BUDGET_SLIDING_WINDOW_SEC", 300),

			// Prompt Compression v2
			SmartCompression:       envOrDefaultBool("AGENT_GATEWAY_SMART_COMPRESSION", false),
			KeepRecentForcedN:      envOrDefaultInt("AGENT_GATEWAY_KEEP_RECENT_FORCED_N", 3),
			SummaryInsertThreshold: envOrDefaultInt("AGENT_GATEWAY_SUMMARY_INSERT_THRESHOLD", 5),
			ScoreThreshold:         envOrDefaultFloat("AGENT_GATEWAY_SCORE_THRESHOLD", 0.3),

			// v0.9 三大新能力 (ADR 0013)
			LLMTimeoutSec:  envOrDefaultInt("AGENT_GATEWAY_LLM_TIMEOUT_SEC", 90),
			CacheEnabled:   envOrDefaultBool("AGENT_GATEWAY_CACHE_ENABLED", false),
			CacheTTLSec:    envOrDefaultInt("AGENT_GATEWAY_CACHE_TTL_SEC", 300),
			CacheMaxEntries: envOrDefaultInt("AGENT_GATEWAY_CACHE_MAX_ENTRIES", 10000),

			// Circuit Breaker
			BreakerEnabled:             envOrDefaultBool("AGENT_GATEWAY_BREAKER_ENABLED", false),
			BreakerFailureRatio:        envOrDefaultFloat("AGENT_GATEWAY_BREAKER_FAILURE_RATIO", 0.5),
			BreakerMinRequests:         envOrDefaultInt("AGENT_GATEWAY_BREAKER_MIN_REQUESTS", 10),
			BreakerOpenTimeoutSec:      envOrDefaultInt("AGENT_GATEWAY_BREAKER_OPEN_TIMEOUT_SEC", 30),
			BreakerHalfOpenMaxRequests: envOrDefaultInt("AGENT_GATEWAY_BREAKER_HALF_OPEN_MAX_REQUESTS", 1),
		},
	}

	// 3. mustEnvs fail-fast (P0-2 / P0-4 安全)
	jsonVal := func(k string) string {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
		return ""
	}
	mustEnvs := []struct {
		name  string
		value string
		help  string
	}{
		{"JWT_SECRET", jsonVal("JWT_SECRET"), "generate with: openssl rand -base64 48"},
		{"DATABASE_URL", jsonVal("DATABASE_URL"), "set in .env (e.g. postgres://user:pass@host:5432/db)"},
	}
	for _, e := range mustEnvs {
		if e.value == "" {
			slog.Error("config: required config is empty", "key", e.name, "help", e.help)
			log.Fatalf("FATAL: required config %s is empty — %s", e.name, e.help)
		}
	}
	// LLM_API_KEY 暂不强制（无 key 时 LLM client 返回 warning,程序继续跑），
	// 但如果 SEARCH_PROVIDER=bocha / tavily 则对应 key 必须有。
	if AppConfig.SearchProvider == "bocha" && AppConfig.BochaAPIKey == "" {
		log.Fatalf("FATAL: SEARCH_PROVIDER=bocha requires BOCHA_API_KEY")
	}
	if AppConfig.SearchProvider == "tavily" && AppConfig.TavilyAPIKey == "" {
		log.Fatalf("FATAL: SEARCH_PROVIDER=tavily requires TAVILY_API_KEY")
	}

	// 4. 末尾输出 summary (v0.10.21 PR-C: 诊断配置生效情况)
	// 统计 "实际从 env 读到" 的 key 数量, 排查 .env 没生效常见问题。
	fromEnv := 0
	for _, key := range []string{
		"PORT", "DATABASE_URL", "REDIS_URL",
		"LLM_PROVIDER", "LLM_API_KEY", "LLM_BASE_URL", "LLM_MODEL_V3", "LLM_MODEL_R1",
		"SEARCH_PROVIDER", "TAVILY_API_KEY", "BOCHA_API_KEY",
		"JWT_SECRET", "JWT_EXPIRY_HOURS", "COOKIE_SECURE", "COOKIE_SAME_SITE", "COOKIE_DOMAIN",
		"ALLOWED_ORIGINS", "USER_TRIAL_LIMIT",
		"AGENT_GATEWAY_ENABLED", "AGENT_GATEWAY_PROMPT_COMPRESSION", "AGENT_GATEWAY_TOKEN_BUDGET",
		"AGENT_GATEWAY_THROTTLING", "AGENT_GATEWAY_FALLBACK", "AGENT_GATEWAY_FILE_LOGGER",
		"AGENT_GATEWAY_BUDGET_PER_SESSION", "AGENT_GATEWAY_COMPRESSION_THRESHOLD",
		"AGENT_GATEWAY_THROTTLING_THRESHOLD", "AGENT_GATEWAY_LOG_DIR",
		"AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED", "AGENT_GATEWAY_BUDGET_SLIDING_WINDOW_SEC",
		"AGENT_GATEWAY_SMART_COMPRESSION", "AGENT_GATEWAY_KEEP_RECENT_FORCED_N",
		"AGENT_GATEWAY_SUMMARY_INSERT_THRESHOLD", "AGENT_GATEWAY_SCORE_THRESHOLD",
		"AGENT_GATEWAY_LLM_TIMEOUT_SEC", "AGENT_GATEWAY_CACHE_ENABLED",
		"AGENT_GATEWAY_CACHE_TTL_SEC", "AGENT_GATEWAY_CACHE_MAX_ENTRIES",
		"AGENT_GATEWAY_BREAKER_ENABLED", "AGENT_GATEWAY_BREAKER_FAILURE_RATIO",
		"AGENT_GATEWAY_BREAKER_MIN_REQUESTS", "AGENT_GATEWAY_BREAKER_OPEN_TIMEOUT_SEC",
		"AGENT_GATEWAY_BREAKER_HALF_OPEN_MAX_REQUESTS",
	} {
		if _, ok := os.LookupEnv(key); ok {
			fromEnv++
		}
	}
	loadSummary(fromEnv, 43)
}

// envOrDefaultString 是 envOrDefault 的 thin wrapper, 保留返回 string 单值签名以简化 Load() 写法。
func envOrDefaultString(key, defaultVal string) string {
	v, _ := envOrDefault(key, defaultVal)
	return v
}

// getProjectRoot returns the project root directory (parent of backend/)
func getProjectRoot() string {
	// Start from the backend directory and go up to find .env
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Dir(dir)
}

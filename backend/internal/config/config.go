package config

import (
	"log"
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
	// Try to read .env file from project root
	envPath := filepath.Join(getProjectRoot(), ".env")
	if _, err := os.Stat(envPath); err == nil {
		viper.SetConfigFile(envPath)
		if err := viper.ReadInConfig(); err != nil {
			log.Printf("warning: failed to read .env file: %v", err)
		}
	}

	viper.SetDefault("PORT", "8080")
	viper.SetDefault("DATABASE_URL", "")
	viper.SetDefault("REDIS_URL", "")
	viper.SetDefault("LLM_PROVIDER", "deepseek")
	viper.SetDefault("LLM_API_KEY", "")
	// Default to DeepSeek; set LLM_BASE_URL to https://api.moonshot.cn/v1 for Kimi
	viper.SetDefault("LLM_BASE_URL", "https://api.deepseek.com/v1")
	viper.SetDefault("LLM_MODEL_V3", "deepseek-chat")
	viper.SetDefault("LLM_MODEL_R1", "deepseek-reasoner")
	viper.SetDefault("SEARCH_PROVIDER", "searxng")
	viper.SetDefault("SEARXNG_URL", "http://searxng:8080")
	viper.SetDefault("TAVILY_API_KEY", "")
	viper.SetDefault("BOCHA_API_KEY", "")
	viper.SetDefault("JWT_SECRET", "decisioncourt-secret")

	viper.SetDefault("AGENT_GATEWAY_ENABLED", false)
	viper.SetDefault("AGENT_GATEWAY_PROMPT_COMPRESSION", false)
	viper.SetDefault("AGENT_GATEWAY_TOKEN_BUDGET", false)
	viper.SetDefault("AGENT_GATEWAY_THROTTLING", false)
	viper.SetDefault("AGENT_GATEWAY_FALLBACK", false)
	viper.SetDefault("AGENT_GATEWAY_FILE_LOGGER", false)
	viper.SetDefault("AGENT_GATEWAY_BUDGET_PER_SESSION", 20000)
	viper.SetDefault("AGENT_GATEWAY_COMPRESSION_THRESHOLD", 0.7)
	viper.SetDefault("AGENT_GATEWAY_THROTTLING_THRESHOLD", 0.8)
	viper.SetDefault("AGENT_GATEWAY_LOG_DIR", "logs")

	// v2 defaults
	// 2026-07-01 变更：默认从 false 改为 true。之前的 false 默认会让
	// budget 撞 100% 后 inner LLM 仍被调用、计费继续累加（审计看到
	// budget_ratio=1.46 但 status=success 的"隐性超额"）。改为 true 后，
	// gateway 在 budget 耗尽时直接返回 ErrBudgetExhausted 并写一条
	// status=error 的审计行。GatewayConfig.IsRejectWhenExhaustedEnabled
	// 也加了 child-default 同步逻辑，保持只设 ENABLED=true 也能开。
	viper.SetDefault("AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED", true)
	viper.SetDefault("AGENT_GATEWAY_BUDGET_SLIDING_WINDOW_SEC", 300)
	viper.SetDefault("AGENT_GATEWAY_SMART_COMPRESSION", false)
	viper.SetDefault("AGENT_GATEWAY_KEEP_RECENT_FORCED_N", 3)
	viper.SetDefault("AGENT_GATEWAY_SUMMARY_INSERT_THRESHOLD", 5)
	viper.SetDefault("AGENT_GATEWAY_SCORE_THRESHOLD", 0.3)

	// v0.9 LLM Gateway 工程化 (ADR 0013)
	viper.SetDefault("AGENT_GATEWAY_LLM_TIMEOUT_SEC", 90)
	viper.SetDefault("AGENT_GATEWAY_CACHE_ENABLED", false)
	viper.SetDefault("AGENT_GATEWAY_CACHE_TTL_SEC", 300)
	viper.SetDefault("AGENT_GATEWAY_CACHE_MAX_ENTRIES", 10000)
	viper.SetDefault("AGENT_GATEWAY_BREAKER_ENABLED", false)
	viper.SetDefault("AGENT_GATEWAY_BREAKER_FAILURE_RATIO", 0.5)
	viper.SetDefault("AGENT_GATEWAY_BREAKER_MIN_REQUESTS", 10)
	viper.SetDefault("AGENT_GATEWAY_BREAKER_OPEN_TIMEOUT_SEC", 30)
	viper.SetDefault("AGENT_GATEWAY_BREAKER_HALF_OPEN_MAX_REQUESTS", 1)

	// v0.9 用户级 Trial 限流 (ADR 0014):默认 5 次/24h,0 禁用。
	viper.SetDefault("USER_TRIAL_LIMIT", 5)

	viper.SetEnvPrefix("")
	viper.AutomaticEnv()

	if err := viper.Unmarshal(&AppConfig); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// P0-2 / P0-4 安全：关键密钥 / 连接串必须存在,缺失则 fail-fast。
	// 不要在这里给"软警告"——生产部署一旦用错密钥,所有用户都会受影响。
	mustEnvs := []struct {
		name  string
		value string
		help  string
	}{
		{"JWT_SECRET", AppConfig.JWTSecret, "generate with: openssl rand -base64 48"},
		{"DATABASE_URL", AppConfig.DatabaseURL, "set in .env (e.g. postgres://user:pass@host:5432/db)"},
	}
	for _, e := range mustEnvs {
		if e.value == "" {
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

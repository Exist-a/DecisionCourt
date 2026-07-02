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
	SearxNGURL     string `mapstructure:"SEARXNG_URL"`
	TavilyAPIKey   string `mapstructure:"TAVILY_API_KEY"`
	BochaAPIKey    string `mapstructure:"BOCHA_API_KEY"`

	JWTSecret string `mapstructure:"JWT_SECRET"`

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

	viper.SetEnvPrefix("")
	viper.AutomaticEnv()

	if err := viper.Unmarshal(&AppConfig); err != nil {
		log.Fatalf("failed to load config: %v", err)
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

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

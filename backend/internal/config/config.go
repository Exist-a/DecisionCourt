package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

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

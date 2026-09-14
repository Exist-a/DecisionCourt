package llm

import "github.com/decisioncourt/backend/internal/config"

// LLMHealth 是不暴露 key 值的健康状态结构。
// v2.1 F4: LLM 静默启动检测的对外结构。key_preview 永远不返回完整 key,
// 仅在 key 存在时返回 "sk-***xxxx" 掩码 (前 3 + 后 4 位)。
type LLMHealth struct {
	Configured bool   `json:"configured"`     // key 是否非空
	Provider   string `json:"provider"`       // "deepseek" / "openai"
	Model      string `json:"model"`          // "deepseek-v4-flash"
	KeyPreview string `json:"key_preview"`    // "sk-***abcd" 或 ""
}

// GetHealth 返回当前 LLM 健康状态, 不读完整 key 值, 仅判断是否非空 + 生成掩码。
//
// 设计纪律 (AGENTS.md §8 红线):
//   - 不回显 key 全值到任何调用方
//   - 不写入任何文件 / 日志
//   - 不通过 HTTP 响应返回完整 key (KeyPreview 字段是唯一输出通道)
func GetHealth() LLMHealth {
	key := config.AppConfig.LLMAPIKey // 只读用于判断非空, 立即传给 maskKey
	cfg := LLMHealth{
		Provider: config.AppConfig.LLMProvider,
		Model:    config.AppConfig.LLMModelV3,
	}
	if key == "" {
		cfg.Configured = false
		cfg.KeyPreview = ""
		return cfg
	}
	cfg.Configured = true
	cfg.KeyPreview = maskKey(key)
	return cfg
}

// maskKey 返回 "前3 + *** + 后4" 掩码。key 太短 (≤7 字符) 整体掩码,
// 避免暴露任何位。永远不返回完整 key。
func maskKey(k string) string {
	if len(k) <= 7 {
		return "***"
	}
	return k[:3] + "***" + k[len(k)-4:]
}
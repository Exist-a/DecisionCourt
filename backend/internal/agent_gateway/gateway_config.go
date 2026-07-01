package agent_gateway

// GatewayConfig 是 Agent Gateway 高级能力的统一开关与阈值配置。
// 所有 bool 开关默认 false；当 AGENT_GATEWAY_ENABLED 为 true 时，
// 各子开关若未显式设置，继承 Enabled 的值（即全开）。
type GatewayConfig struct {
	Enabled              bool
	PromptCompression    bool
	TokenBudget          bool
	Throttling           bool
	Fallback             bool
	FileLogger           bool
	BudgetPerSession     int
	CompressionThreshold float64
	ThrottlingThreshold  float64
	LogDir               string

	// === Token Budget v2 扩展 ===
	// RejectWhenExhausted 控制 budget 达到 100% 时 Gateway.Complete 的行为。
	//   true  → 返回 ErrBudgetExhausted（推荐；与业内网关一致，避免无谓账单）
	//   false → 现状：仍调用 inner，由 Throttler 强降 MaxTokens=100
	// 默认 false 以保留 v0.5+ 行为；上线时建议 true。
	RejectWhenExhausted bool
	// BudgetSlidingWindowSec budget sliding 时间窗（秒），0 → 默认 300。
	BudgetSlidingWindowSec int

	// === Prompt Compression v2 扩展 ===
	// SmartCompression 启用"评分 + 原子组 + 贪心打包 + 兜底摘要"四阶段。
	// 默认 false → 保持 v0.5+ 的 keep-5 旧策略。
	SmartCompression bool
	// KeepRecentForcedN 评分策略下，强制保留最近 N 条，不参与评分淘汰。
	KeepRecentForcedN int
	// SummaryInsertThreshold 丢弃数量超过该阈值时插入一条"earlier context"摘要。
	SummaryInsertThreshold int
	// ScoreThreshold 低于该分不进入保留集。
	ScoreThreshold float64
}

// IsPromptCompressionEnabled 返回压缩是否生效。
func (c GatewayConfig) IsPromptCompressionEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.PromptCompression || c.isChildDefault()
}

// IsTokenBudgetEnabled 返回预算是否生效。
func (c GatewayConfig) IsTokenBudgetEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.TokenBudget || c.isChildDefault()
}

// IsThrottlingEnabled 返回限流是否生效。
func (c GatewayConfig) IsThrottlingEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.Throttling || c.isChildDefault()
}

// IsFallbackEnabled 返回重试是否生效。
func (c GatewayConfig) IsFallbackEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.Fallback || c.isChildDefault()
}

// IsFileLoggerEnabled 返回文件日志是否生效。
func (c GatewayConfig) IsFileLoggerEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.FileLogger || c.isChildDefault()
}

// isChildDefault 当 AGENT_GATEWAY_ENABLED=true 且没有任何子开关被显式
// 设置为 true 时，视为全开。这样默认环境变量可以只写 ENABLED=true。
func (c GatewayConfig) isChildDefault() bool {
	return !c.PromptCompression && !c.TokenBudget && !c.Throttling && !c.Fallback && !c.FileLogger
}

// Normalize 把配置中的默认值补全。
func (c GatewayConfig) Normalize() GatewayConfig {
	out := c
	if out.BudgetPerSession <= 0 {
		out.BudgetPerSession = 20000
	}
	if out.CompressionThreshold <= 0 || out.CompressionThreshold >= 1 {
		out.CompressionThreshold = 0.7
	}
	if out.ThrottlingThreshold <= 0 || out.ThrottlingThreshold >= 1 {
		out.ThrottlingThreshold = 0.8
	}
	if out.LogDir == "" {
		out.LogDir = "logs"
	}
	// v2 默认值
	if out.BudgetSlidingWindowSec <= 0 {
		out.BudgetSlidingWindowSec = 300 // 5min
	}
	if out.KeepRecentForcedN <= 0 {
		out.KeepRecentForcedN = 3
	}
	if out.SummaryInsertThreshold <= 0 {
		out.SummaryInsertThreshold = 5
	}
	if out.ScoreThreshold <= 0 {
		out.ScoreThreshold = 0.3
	}
	return out
}

// IsSmartCompressionEnabled 决定是否启用"评分压缩"v2。
// SmartCompression 必须显式 true；不回退到 Enable / childDefault（破坏性升级，
// 需用户在 .env 显式开启）。
func (c GatewayConfig) IsSmartCompressionEnabled() bool {
	return c.Enabled && c.SmartCompression
}

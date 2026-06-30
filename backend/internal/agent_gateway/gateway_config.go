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
	return out
}

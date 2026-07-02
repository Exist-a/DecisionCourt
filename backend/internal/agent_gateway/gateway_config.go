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
	//   false → 仍调用 inner，由 Throttler 强降 MaxTokens=100（兼容 v0.5+
	//           行为；当用户希望"超 budget 也跑完"时手动关闭）
	//
	// 默认值由 config.Load() 的 viper.SetDefault 设 true（2026-07-01
	// 改为 true，之前的 false 默认会让 budget_ratio 超 1.0 时 inner 仍
	// 被调用、账单超支但审计看不出来 —— 用户在日志里看到 budget_ratio=1.46
	// 但 status=success 的"隐性超额"）。详见 IsRejectWhenExhaustedEnabled。
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

// IsRejectWhenExhaustedEnabled 返回 budget 耗尽时是否拒绝新请求。
//
// 行为规则：
//   - AGENT_GATEWAY_ENABLED=false → false（gateway 整体未启用）
//   - AGENT_GATEWAY_ENABLED=true + RejectWhenExhausted=true → true
//   - AGENT_GATEWAY_ENABLED=true + RejectWhenExhausted=false → false（用户显式关）
//
// 默认值由 config.Load() 的 viper.SetDefault("AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED", true)
// 提供；通过 .env 设 AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED=false 即可恢复
// v0.5+ "超 budget 也跑" 兼容行为。
//
// 设计理由：之前的"默认 false"会让 budget_ratio 超 1.0 时还在调 inner，
// 账单超支但审计看不出来（用户在日志里看到 budget_ratio=1.46 但
// status=success 的"隐性超额"）。2026-07-01 起把 viper 默认改为 true。
//
// 注：Go bool 零值无法区分"用户显式 false"和"未设置"，所以这个方法
// 不引入 child-default 规则；其它子开关（PromptCompression 等）继续走
// isChildDefault()。
func (c GatewayConfig) IsRejectWhenExhaustedEnabled() bool {
	return c.Enabled && c.RejectWhenExhausted
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

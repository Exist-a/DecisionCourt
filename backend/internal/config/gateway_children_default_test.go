package config

import (
	"testing"
)

// v2.11 (deferred D16): gateway 子开关「显式 env 优先，未设置则继承总开关」。
//
// 背景：docker 实跑发现 AGENT_GATEWAY_ENABLED=true 实际只等于开了 FileLogger ——
// 压缩 / 预算 / 限流 / 重试 / 缓存 / 熔断全部静默关闭。根因是旧实现把「子开关继承
// 总开关」交给了 GatewayConfig.isChildDefault() 的 bool 零值启发式，而
// AGENT_GATEWAY_FILE_LOGGER 的默认值是 true → 该启发式恒为 false → 「全开」是死代码。
//
// 这组测试钉住新规则。注意 buildAgentGatewayConfig 是可直接调用的（Load 有副作用），
// 所以这些断言覆盖的是**生产路径真正用到的那段逻辑**。

// gatewayChildEnvKeys 是需要「继承总开关」的能力型子开关清单。
// 新增子开关时必须加进来 —— 否则它在默认部署里又会静默关闭（D16 同类问题）。
var gatewayChildEnvKeys = []string{
	"AGENT_GATEWAY_PROMPT_COMPRESSION",
	"AGENT_GATEWAY_TOKEN_BUDGET",
	"AGENT_GATEWAY_THROTTLING",
	"AGENT_GATEWAY_FALLBACK",
	"AGENT_GATEWAY_FILE_LOGGER",
	"AGENT_GATEWAY_SMART_COMPRESSION",
	"AGENT_GATEWAY_CACHE_ENABLED",
	"AGENT_GATEWAY_BREAKER_ENABLED",
}

// clearGatewayChildEnvs 把清单里的 env 全部置空（t.Setenv 的 cleanup 会恢复原值）。
func clearGatewayChildEnvs(t *testing.T) {
	t.Helper()
	for _, k := range gatewayChildEnvKeys {
		t.Setenv(k, "")
	}
}

// TestGatewayChildren_InheritWhenUnset 核心契约：总开关开 + 子开关都没配 → 全开。
func TestGatewayChildren_InheritWhenUnset(t *testing.T) {
	clearGatewayChildEnvs(t)
	t.Setenv("AGENT_GATEWAY_ENABLED", "true")

	gw := buildAgentGatewayConfig()

	if !gw.Enabled {
		t.Fatal("Enabled 应为 true")
	}
	checks := map[string]bool{
		"PromptCompression": gw.PromptCompression,
		"TokenBudget":       gw.TokenBudget,
		"Throttling":        gw.Throttling,
		"Fallback(重试)":      gw.Fallback,
		"FileLogger":        gw.FileLogger,
		"SmartCompression":  gw.SmartCompression,
		"CacheEnabled":      gw.CacheEnabled,
		"BreakerEnabled":    gw.BreakerEnabled,
	}
	for name, on := range checks {
		if !on {
			t.Errorf("%s 未设置时应继承总开关(true) → 应为 true，实际 false —— "+
				"这正是 D16 的缺陷形态（默认部署里子能力静默关闭）", name)
		}
	}
	// ADR 0044 #6 例外：abstractive 摘要是成本 opt-in，不继承。
	if gw.SmartCompressionAbstractiveSummary {
		t.Error("SmartCompressionAbstractiveSummary 不应继承总开关（ADR 0044 #6 成本 opt-in）")
	}
}

// TestGatewayChildren_ExplicitFalseWins 显式 false 必须生效，且**只影响自己**。
func TestGatewayChildren_ExplicitFalseWins(t *testing.T) {
	clearGatewayChildEnvs(t)
	t.Setenv("AGENT_GATEWAY_ENABLED", "true")
	t.Setenv("AGENT_GATEWAY_CACHE_ENABLED", "false")
	t.Setenv("AGENT_GATEWAY_BREAKER_ENABLED", "false")

	gw := buildAgentGatewayConfig()

	if gw.CacheEnabled {
		t.Error("显式 CACHE_ENABLED=false 应生效")
	}
	if gw.BreakerEnabled {
		t.Error("显式 BREAKER_ENABLED=false 应生效")
	}
	// 关键：显式关掉一个子开关，不得连带关掉其他子开关
	// （旧 isChildDefault() 是"全有或全无"，一个子开关就能击穿全部）。
	if !gw.PromptCompression || !gw.TokenBudget || !gw.Throttling || !gw.Fallback {
		t.Error("显式关闭 cache/breaker 不得影响其他子开关 —— "+
			"「全有或全无」正是 D16 的根因形态")
	}
	if !gw.FileLogger {
		t.Error("FileLogger 未显式设置，应继承 true")
	}
}

// TestGatewayChildren_GatewayDisabledTurnsAllOff 总开关关 → 子开关全关（未显式设置时）。
func TestGatewayChildren_GatewayDisabledTurnsAllOff(t *testing.T) {
	clearGatewayChildEnvs(t)
	t.Setenv("AGENT_GATEWAY_ENABLED", "false")

	gw := buildAgentGatewayConfig()
	for _, name := range []string{"PromptCompression", "TokenBudget", "Throttling", "Fallback", "FileLogger"} {
		_ = name
	}
	if gw.Enabled {
		t.Fatal("Enabled 应为 false")
	}
	if gw.PromptCompression || gw.TokenBudget || gw.Throttling || gw.Fallback || gw.FileLogger ||
		gw.SmartCompression || gw.CacheEnabled || gw.BreakerEnabled {
		t.Errorf("总开关 false 且子开关未配置时，子能力应全关，实际: pc=%v tb=%v th=%v fb=%v fl=%v sc=%v cache=%v br=%v",
			gw.PromptCompression, gw.TokenBudget, gw.Throttling, gw.Fallback, gw.FileLogger,
			gw.SmartCompression, gw.CacheEnabled, gw.BreakerEnabled)
	}
}

// TestGatewayChildren_EnabledUnsetDefaultsOff 总开关本身未配置 → 默认 false（D14 的语义保留）。
func TestGatewayChildren_EnabledUnsetDefaultsOff(t *testing.T) {
	clearGatewayChildEnvs(t)
	t.Setenv("AGENT_GATEWAY_ENABLED", "")

	gw := buildAgentGatewayConfig()
	if gw.Enabled {
		t.Error("AGENT_GATEWAY_ENABLED 未设置时默认应为 false（D14 保留该默认值）")
	}
	if gw.PromptCompression || gw.TokenBudget {
		t.Error("总开关关着时子能力不应为 true")
	}
}

// TestGatewayChildren_EachKeyInherits 逐个键验证继承（防止新增子开关时漏接规则）。
func TestGatewayChildren_EachKeyInherits(t *testing.T) {
	for _, key := range gatewayChildEnvKeys {
		t.Run(key, func(t *testing.T) {
			clearGatewayChildEnvs(t)
			t.Setenv("AGENT_GATEWAY_ENABLED", "true")
			if !resolveGatewayChild(key, true) {
				t.Errorf("%s 未设置时应继承 true", key)
			}
			// 显式 false 覆盖
			t.Setenv(key, "false")
			if resolveGatewayChild(key, true) {
				t.Errorf("%s 显式 false 时应为 false", key)
			}
			// 显式 true 覆盖（即使总开关是 false）
			t.Setenv(key, "true")
			if !resolveGatewayChild(key, false) {
				t.Errorf("%s 显式 true 时应为 true", key)
			}
		})
	}
}

// TestGatewayChildren_BlankEnvTreatedAsUnset 空白串按"未配置"处理（与 envOrDefault 语义一致）。
func TestGatewayChildren_BlankEnvTreatedAsUnset(t *testing.T) {
	t.Setenv("AGENT_GATEWAY_TOKEN_BUDGET", "   ")
	if !resolveGatewayChild("AGENT_GATEWAY_TOKEN_BUDGET", true) {
		t.Error("空白串应视为未配置 → 继承 true")
	}
	if resolveGatewayChild("AGENT_GATEWAY_TOKEN_BUDGET", false) {
		t.Error("空白串应视为未配置 → 继承 false")
	}
}

// TestGatewayChildren_BudgetDefaultIsRealistic 钉住 D17 的默认上限（与 D16 同批）。
func TestGatewayChildren_BudgetDefaultIsRealistic(t *testing.T) {
	t.Setenv("AGENT_GATEWAY_BUDGET_PER_SESSION", "")
	gw := buildAgentGatewayConfig()
	if gw.BudgetPerSession != DefaultBudgetPerSession {
		t.Errorf("未配置时预算上限应为 %d，got %d", DefaultBudgetPerSession, gw.BudgetPerSession)
	}
	if gw.BudgetPerSession != 200000 {
		t.Errorf("预算上限应为 200000（实测一场 quick 庭审约 46k token），got %d", gw.BudgetPerSession)
	}
	if !gw.RejectWhenExhausted {
		t.Error("RejectWhenExhausted 默认应为 true（失控循环时拒绝优于继续烧钱）")
	}
}

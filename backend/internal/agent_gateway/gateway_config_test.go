package agent_gateway

import (
	"testing"
)

func TestGatewayConfig_DisabledTurnsAllOff(t *testing.T) {
	c := GatewayConfig{Enabled: false, PromptCompression: true, TokenBudget: true}.Normalize()
	if c.IsPromptCompressionEnabled() {
		t.Error("should be off when disabled")
	}
	if c.IsTokenBudgetEnabled() {
		t.Error("budget should be off when disabled")
	}
	if c.IsFileLoggerEnabled() {
		t.Error("logger should be off when disabled")
	}
}

func TestGatewayConfig_EnabledDefaultsToAllOn(t *testing.T) {
	c := GatewayConfig{Enabled: true}.Normalize()
	if !c.IsPromptCompressionEnabled() {
		t.Error("compression should default on")
	}
	if !c.IsTokenBudgetEnabled() {
		t.Error("budget should default on")
	}
	if !c.IsThrottlingEnabled() {
		t.Error("throttling should default on")
	}
	if !c.IsFallbackEnabled() {
		t.Error("fallback should default on")
	}
	if !c.IsFileLoggerEnabled() {
		t.Error("file logger should default on")
	}
	// v2.10 ADR 0044 #1: SmartCompression 也应走 isChildDefault()
	if !c.IsSmartCompressionEnabled() {
		t.Error("smart compression should default on")
	}
}

func TestGatewayConfig_SubSwitchesOverride(t *testing.T) {
	c := GatewayConfig{Enabled: true, PromptCompression: false, TokenBudget: true, Throttling: false, Fallback: false, FileLogger: false}.Normalize()
	if c.IsPromptCompressionEnabled() {
		t.Error("compression should be off")
	}
	if !c.IsTokenBudgetEnabled() {
		t.Error("budget should be on")
	}
	if c.IsThrottlingEnabled() {
		t.Error("throttling should be off")
	}
	if c.IsFallbackEnabled() {
		t.Error("fallback should be off")
	}
	if c.IsFileLoggerEnabled() {
		t.Error("file logger should be off")
	}
}

// v2.10 ADR 0044 #1: SmartCompression 配置 footgun 修复验证。
// 当 Enabled=true 且 SmartCompression 显式 false，但有其他子开关开启时，
// SmartCompression 应为 false（isChildDefault 返回 false）。
func TestGatewayConfig_SmartCompressionExplicitOff(t *testing.T) {
	c := GatewayConfig{Enabled: true, SmartCompression: false, TokenBudget: true}.Normalize()
	if c.IsSmartCompressionEnabled() {
		t.Error("smart compression explicit off should be respected when other sub-switch is on")
	}
}

// 当 Enabled=true 且无任何子开关时，SmartCompression 应走 isChildDefault() 自动开启。
func TestGatewayConfig_SmartCompressionDefaultsOn(t *testing.T) {
	c := GatewayConfig{Enabled: true}.Normalize()
	if !c.IsSmartCompressionEnabled() {
		t.Error("smart compression should default on via isChildDefault()")
	}
}

// 当 Enabled=false 时，SmartCompression 应为 false。
func TestGatewayConfig_SmartCompressionDisabled(t *testing.T) {
	c := GatewayConfig{Enabled: false, SmartCompression: true}.Normalize()
	if c.IsSmartCompressionEnabled() {
		t.Error("smart compression should be off when gateway disabled")
	}
}

func TestGatewayConfig_NormalizeDefaults(t *testing.T) {
	c := GatewayConfig{Enabled: true}.Normalize()
	if c.BudgetPerSession != 20000 {
		t.Errorf("budget: want 20000 got %d", c.BudgetPerSession)
	}
	if c.CompressionThreshold != 0.7 {
		t.Errorf("compress threshold: want 0.7 got %.2f", c.CompressionThreshold)
	}
	if c.ThrottlingThreshold != 0.8 {
		t.Errorf("throttle threshold: want 0.8 got %.2f", c.ThrottlingThreshold)
	}
	if c.LogDir != "logs" {
		t.Errorf("log dir: want logs got %q", c.LogDir)
	}
}

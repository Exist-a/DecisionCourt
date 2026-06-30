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

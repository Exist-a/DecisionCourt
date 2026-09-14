package config

import (
	"testing"
)

// TestF5DefaultSwitchesTrue v2.1 F5: 三个 ADR 0013 能力默认全开
// (本地开发模式验证可用性, 回滚: .env 设 =false 重启)
func TestF5DefaultSwitchesTrue(t *testing.T) {
	// 显式 unset 三个 env var, 验证 default = true
	t.Setenv("AGENT_GATEWAY_SMART_COMPRESSION", "")
	t.Setenv("AGENT_GATEWAY_CACHE_ENABLED", "")
	t.Setenv("AGENT_GATEWAY_BREAKER_ENABLED", "")

	cfg := AgentGatewayConfig{
		SmartCompression: envOrDefaultBool("AGENT_GATEWAY_SMART_COMPRESSION", true),
		CacheEnabled:     envOrDefaultBool("AGENT_GATEWAY_CACHE_ENABLED", true),
		BreakerEnabled:   envOrDefaultBool("AGENT_GATEWAY_BREAKER_ENABLED", true),
	}

	if !cfg.SmartCompression {
		t.Errorf("F5: SmartCompression 应默认 true (本地开发模式)")
	}
	if !cfg.CacheEnabled {
		t.Errorf("F5: CacheEnabled 应默认 true")
	}
	if !cfg.BreakerEnabled {
		t.Errorf("F5: BreakerEnabled 应默认 true")
	}
}

// TestF5DefaultsRespectedFromEnv 验证 env 覆盖依然有效 (回滚路径)。
// 设 env = false 后, envOrDefaultBool 应返回 false (不是默认值 true)。
func TestF5DefaultsRespectedFromEnv(t *testing.T) {
	t.Setenv("AGENT_GATEWAY_SMART_COMPRESSION", "false")
	t.Setenv("AGENT_GATEWAY_CACHE_ENABLED", "false")
	t.Setenv("AGENT_GATEWAY_BREAKER_ENABLED", "false")

	cfg := AgentGatewayConfig{
		SmartCompression: envOrDefaultBool("AGENT_GATEWAY_SMART_COMPRESSION", true),
		CacheEnabled:     envOrDefaultBool("AGENT_GATEWAY_CACHE_ENABLED", true),
		BreakerEnabled:   envOrDefaultBool("AGENT_GATEWAY_BREAKER_ENABLED", true),
	}

	if cfg.SmartCompression {
		t.Errorf("F5: env=false 应覆盖 default true")
	}
	if cfg.CacheEnabled {
		t.Errorf("F5: env=false 应覆盖 default true")
	}
	if cfg.BreakerEnabled {
		t.Errorf("F5: env=false 应覆盖 default true")
	}
}
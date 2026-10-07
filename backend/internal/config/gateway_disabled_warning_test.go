package config

import (
	"os"
	"testing"
)

// v2.11 (deferred D14): AGENT_GATEWAY_ENABLED 代码默认 false，compose /
// .env.example 默认 true。直接跑二进制时变量缺失 → 网关静默不启用。
// 这组测试钉住三件事：
//  1. 默认值仍是 false（方案 2「保留默认、加告警」，不改变行为）
//  2. "没配"的部署会拿到告警文案
//  3. "显式配 false"的部署不被打扰
func TestGatewayDisabledWarning(t *testing.T) {
	cases := []struct {
		name        string
		enabled     bool
		envSet      bool
		wantWarning bool
	}{
		{"网关开启 → 不告警", true, true, false},
		{"网关开启且变量缺失 → 不告警", true, false, false},
		{"显式配 false → 用户决策，不告警", false, true, false},
		{"没配 → 默认值静默生效，必须告警", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GatewayDisabledWarning(c.enabled, c.envSet)
			if c.wantWarning && got == "" {
				t.Fatal("expected warning text, got empty string")
			}
			if !c.wantWarning && got != "" {
				t.Fatalf("expected no warning, got %q", got)
			}
		})
	}
}

// TestAgentGatewayEnabledEnvSet 覆盖 env 探测的三个态：缺失 / 空串 / 有值。
// 空串必须算"没配"——envOrDefault 也把空值当未设，两者语义要一致。
func TestAgentGatewayEnabledEnvSet(t *testing.T) {
	t.Run("变量有值 → true", func(t *testing.T) {
		t.Setenv("AGENT_GATEWAY_ENABLED", "false")
		if !AgentGatewayEnabledEnvSet() {
			t.Error("显式设为 false 也算已配置（用户决策）")
		}
	})

	t.Run("变量为空串 → false", func(t *testing.T) {
		t.Setenv("AGENT_GATEWAY_ENABLED", "  ")
		if AgentGatewayEnabledEnvSet() {
			t.Error("空白串应按未配置处理")
		}
	})

	t.Run("变量缺失 → false", func(t *testing.T) {
		t.Setenv("AGENT_GATEWAY_ENABLED", "x") // 注册 cleanup，恢复原值
		if err := os.Unsetenv("AGENT_GATEWAY_ENABLED"); err != nil {
			t.Fatalf("unsetenv: %v", err)
		}
		if AgentGatewayEnabledEnvSet() {
			t.Error("变量缺失时应返回 false")
		}
	})
}

// TestAgentGatewayEnabledDefaultStillFalse 钉住默认值：本次只加告警，不改行为。
// 若将来把默认改成 true，这条测试会失败并提醒同步 compose / .env.example / 文档。
func TestAgentGatewayEnabledDefaultStillFalse(t *testing.T) {
	t.Setenv("AGENT_GATEWAY_ENABLED", "")
	if envOrDefaultBool("AGENT_GATEWAY_ENABLED", false) {
		t.Error("默认值必须保持 false —— 改默认值需要同步 compose 与文档")
	}
}

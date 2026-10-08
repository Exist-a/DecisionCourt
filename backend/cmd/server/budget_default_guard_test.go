package main

import (
	"testing"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/config"
)

// v2.11 (deferred D17) 跨包一致性护栏：token 预算的默认上限在 config 与
// agent_gateway 两个包各有一份字面量（config 不能反向 import agent_gateway，
// 否则经 llm → config 形成环）。两处语义相同，漂移会导致"走 config 路径"与
// "直接构造 GatewayConfig"得到不同的默认预算，且不会有任何编译错误。
//
// 这条测试把两者钉在一起：任一处改了而另一处没跟上，立刻失败。
func TestBudgetDefault_ConfigAndGatewayAgree(t *testing.T) {
	if config.DefaultBudgetPerSession != agent_gateway.DefaultBudgetPerSession {
		t.Fatalf("默认预算上限漂移：config=%d agent_gateway=%d —— 两处必须一致",
			config.DefaultBudgetPerSession, agent_gateway.DefaultBudgetPerSession)
	}
	// 顺手钉住具体数值：D17 实测一场 quick 庭审约 46k token，20000 会让判决被拒。
	// 改动这个数字必须是有意识的决策（同时更新 .env.example + ADR 0013 状态更新）。
	if config.DefaultBudgetPerSession != 200000 {
		t.Errorf("默认预算上限应为 200000（v2.11 D17 实测依据），got %d", config.DefaultBudgetPerSession)
	}
}

// TestBuildGatewayConfig_UnsetBudgetFallsBackToDefault 确认"配置没写"时走的是
// 新默认值，而不是某个历史遗留常量。
func TestBuildGatewayConfig_UnsetBudgetFallsBackToDefault(t *testing.T) {
	// config 侧未设 → 0；Normalize 应补成 DefaultBudgetPerSession。
	got := buildGatewayConfig(config.AgentGatewayConfig{}).Normalize()
	if got.BudgetPerSession != agent_gateway.DefaultBudgetPerSession {
		t.Errorf("未配置时 BudgetPerSession 应回落为 %d，got %d",
			agent_gateway.DefaultBudgetPerSession, got.BudgetPerSession)
	}
}

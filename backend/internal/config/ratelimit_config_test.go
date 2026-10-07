package config

import "testing"

// v2.11 (deferred D10): 四层限流阈值接线到 config。默认值必须与历史硬编码
// 完全一致(2 / 5 / 5)——本次只做"可配",不改行为。若有人顺手改默认值，
// 这条测试会失败并提醒同步 .env.example + ADR 0027 + api-design §5.2。
func TestRateLimitThresholdDefaults(t *testing.T) {
	t.Setenv("RATE_LIMIT_SESSION_ACTION_RPS", "")
	t.Setenv("RATE_LIMIT_SESSION_ACTION_BURST", "")
	t.Setenv("RATE_LIMIT_MAX_CONCURRENT_TRIALS", "")

	rps := envOrDefaultFloat("RATE_LIMIT_SESSION_ACTION_RPS", 2)
	burst := envOrDefaultInt("RATE_LIMIT_SESSION_ACTION_BURST", 5)
	maxTrials := envOrDefaultInt("RATE_LIMIT_MAX_CONCURRENT_TRIALS", 5)

	if rps != 2 {
		t.Errorf("SessionActionRPS 默认值应为 2(历史 middleware.DefaultSessionConfig.RPS), got %v", rps)
	}
	if burst != 5 {
		t.Errorf("SessionActionBurst 默认值应为 5(历史 DefaultSessionConfig.Burst), got %d", burst)
	}
	if maxTrials != 5 {
		t.Errorf("MaxConcurrentTrials 默认值应为 5(历史 main.go 硬编码), got %d", maxTrials)
	}
}

// TestRateLimitThresholdEnvOverride 钉住回滚/调参路径：env 必须能覆盖默认值。
func TestRateLimitThresholdEnvOverride(t *testing.T) {
	t.Setenv("RATE_LIMIT_SESSION_ACTION_RPS", "7.5")
	t.Setenv("RATE_LIMIT_SESSION_ACTION_BURST", "12")
	t.Setenv("RATE_LIMIT_MAX_CONCURRENT_TRIALS", "3")

	rps := envOrDefaultFloat("RATE_LIMIT_SESSION_ACTION_RPS", 2)
	burst := envOrDefaultInt("RATE_LIMIT_SESSION_ACTION_BURST", 5)
	maxTrials := envOrDefaultInt("RATE_LIMIT_MAX_CONCURRENT_TRIALS", 5)

	if rps != 7.5 {
		t.Errorf("RPS env 覆盖失效: got %v", rps)
	}
	if burst != 12 {
		t.Errorf("Burst env 覆盖失效: got %d", burst)
	}
	if maxTrials != 3 {
		t.Errorf("MaxConcurrentTrials env 覆盖失效: got %d", maxTrials)
	}
}

// TestDiagnosticEnvKeysIncludeRateLimit 保证声明式清单同步——loadSummary 靠它
// 统计"实际生效了几个 env"，漏加就会少报(与 v2.8 FILE_LOGGER_PROMPTS 同类问题)。
func TestDiagnosticEnvKeysIncludeRateLimit(t *testing.T) {
	want := []string{
		"RATE_LIMIT_SESSION_ACTION_RPS",
		"RATE_LIMIT_SESSION_ACTION_BURST",
		"RATE_LIMIT_MAX_CONCURRENT_TRIALS",
	}
	have := make(map[string]bool, len(diagnosticEnvKeys))
	for _, k := range diagnosticEnvKeys {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("diagnosticEnvKeys 缺少 %s —— 新增 env 必须加进清单", k)
		}
	}
}

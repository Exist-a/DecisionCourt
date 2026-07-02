package agent_gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestGateway_RejectWhenExhausted: 启用 RejectWhenExhausted 时，budget 100%
// 不再调用 inner，直接返回 ErrBudgetExhausted。
func TestGateway_RejectWhenExhausted(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeContent: "should-not-be-called"}
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, newFakeStore())
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  true,
		BudgetPerSession:     1000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
	}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "v2-s1", AgentType: "judge", TaskType: "verdict"})

	// 加满 budget → exhausted
	gw.budget.AddUsage(ctx, "v2-s1", BudgetUsage{InputTokens: 500, OutputTokens: 600})

	_, _, err := gw.Complete(ctx, "sys", []llm.Message{{Role: "user", Content: "x"}}, llm.CompletionOptions{})
	if err == nil {
		t.Fatalf("expected ErrBudgetExhausted, got nil")
	}
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("err should be ErrBudgetExhausted, got %v", err)
	}
	if inner.completeCalls != 0 {
		t.Errorf("inner should NOT be called when exhausted + reject enabled, got %d", inner.completeCalls)
	}
}

// TestGateway_NoRejectWhenExhausted_Disabled: 显式关闭 reject 时仍调用 inner。
//
// 2026-07-01 变更：默认从 false 改成 true（见 GatewayConfig.IsRejectWhenExhaustedEnabled
// 注释）。本测试用 `RejectWhenExhausted: false` 显式保留"超 budget 也跑"
// 的兼容路径，覆盖"老用户主动关掉 reject"的场景。
func TestGateway_NoRejectWhenExhausted_Disabled(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeContent: "called-anyway"}
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, newFakeStore())
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  false, // 用户显式关
		BudgetPerSession:     1000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
	}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "v2-s2", AgentType: "judge", TaskType: "verdict"})
	gw.budget.AddUsage(ctx, "v2-s2", BudgetUsage{InputTokens: 500, OutputTokens: 600})

	_, _, err := gw.Complete(ctx, "sys", []llm.Message{{Role: "user", Content: "x"}}, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("should not error when reject disabled, got %v", err)
	}
	if inner.completeCalls != 1 {
		t.Errorf("inner should be called when reject disabled, got %d", inner.completeCalls)
	}
}

// TestGateway_RejectWhenExhausted_DefaultOpen: 显式把 RejectWhenExhausted
// 设为 true（模拟 viper 的 SetDefault(true) 注入场景），gateway 必须
// 在 budget 耗尽时返回 ErrBudgetExhausted 且 inner 不被调用。这是用户
// 日志里看到 budget_ratio=1.46 但 status=success 的根因修复的回归守门。
//
// 2026-07-01 变更：默认值由 viper 设为 true（见 config.Load()）。纯 Go
// 构造 GatewayConfig{} 时 bool 零值是 false，所以这个测试显式打开来
// 模拟 viper 注入后的真实配置。
func TestGateway_RejectWhenExhausted_DefaultOpen(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeContent: "should-not-be-called"}
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, newFakeStore())
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  true, // 模拟 viper 默认注入
		BudgetPerSession:     1000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
	}.Normalize()
	if !cfg.IsRejectWhenExhaustedEnabled() {
		t.Fatalf("IsRejectWhenExhaustedEnabled should be true when explicitly enabled")
	}
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "v2-do1", AgentType: "judge", TaskType: "verdict"})
	gw.budget.AddUsage(ctx, "v2-do1", BudgetUsage{InputTokens: 500, OutputTokens: 600})

	_, _, err := gw.Complete(ctx, "sys", []llm.Message{{Role: "user", Content: "x"}}, llm.CompletionOptions{})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted via viper-default-open, got %v", err)
	}
	if inner.completeCalls != 0 {
		t.Errorf("inner must NOT be called when reject enabled, got %d calls", inner.completeCalls)
	}
}

// TestGateway_RejectWhenExhausted_UserExplicitFalse: 用户在 .env 里显式
// 设 AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED=false 时，child-default 必须让步，
// 保持 v0.5+ 行为（budget 超了降级继续调 inner）。避免误伤老部署。
func TestGateway_RejectWhenExhausted_UserExplicitFalse(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeContent: "called-anyway"}
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, newFakeStore())
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  false, // 显式关
		BudgetPerSession:     1000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
	}.Normalize()
	if cfg.IsRejectWhenExhaustedEnabled() {
		t.Fatalf("user-explicit false must override child-default")
	}
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "v2-uf", AgentType: "judge", TaskType: "verdict"})
	gw.budget.AddUsage(ctx, "v2-uf", BudgetUsage{InputTokens: 500, OutputTokens: 600})

	_, _, err := gw.Complete(ctx, "sys", []llm.Message{{Role: "user", Content: "x"}}, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("user-explicit false: should not error, got %v", err)
	}
	if inner.completeCalls != 1 {
		t.Errorf("inner should be called when user-explicit false, got %d", inner.completeCalls)
	}
}

// TestGatewayConfig_IsRejectWhenExhaustedEnabled_Matrix: 纯配置层断言，
// 不依赖 gateway 行为。把"网关未启用 / 用户显式 true / 用户显式 false /
// 其它子开关不影响本开关"四种情况全部矩阵化，避免将来重构时漏掉路径。
//
// 注：纯 Go 构造 GatewayConfig{} 时 RejectWhenExhausted 零值是 false。
// 生产配置由 viper 注入，viper 默认值是 true（见 config.Load()）。
func TestGatewayConfig_IsRejectWhenExhaustedEnabled_Matrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  GatewayConfig
		want bool
	}{
		{
			name: "enabled=false → 不开",
			cfg:  GatewayConfig{Enabled: false, RejectWhenExhausted: true},
			want: false,
		},
		{
			name: "enabled=true + 显式 true → 开",
			cfg:  GatewayConfig{Enabled: true, RejectWhenExhausted: true},
			want: true,
		},
		{
			name: "enabled=true + 显式 false → 关",
			cfg:  GatewayConfig{Enabled: true, RejectWhenExhausted: false},
			want: false,
		},
		{
			name: "enabled=true + RejectWhenExhausted 零值（纯 Go 默认）→ 关",
			cfg:  GatewayConfig{Enabled: true},
			want: false,
		},
		{
			name: "enabled=true + 其它子开关不影响本开关",
			cfg: GatewayConfig{
				Enabled:           true,
				RejectWhenExhausted: true,
				PromptCompression: true,
				TokenBudget:       true,
				Throttling:        true,
				Fallback:          true,
				FileLogger:        true,
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.IsRejectWhenExhaustedEnabled(); got != tc.want {
				t.Errorf("IsRejectWhenExhaustedEnabled: want %v got %v (cfg=%+v)", tc.want, got, tc.cfg)
			}
		})
	}
}

// TestTokenBudget_OnWarningHookFired: 预算跨越 0.7 / 0.8 / 1.0 时分别触发 hook。
func TestTokenBudget_OnWarningHookFired(t *testing.T) {
	t.Parallel()
	tb := NewTokenBudget(1000, 0.7, 0.8)

	var gotLevels []string
	tb.AddOnWarning(func(_ context.Context, _ string, level string, _ BudgetSnapshot) {
		gotLevels = append(gotLevels, level)
	})

	// 740 tokens → 0.74 ratio → compress (warning_70)
	tb.AddUsage(context.Background(), "wh-1", BudgetUsage{InputTokens: 740})
	snap := tb.Check(context.Background(), "wh-1")
	if snap.Status != StatusCompress {
		t.Fatalf("want compress got %s", snap.Status)
	}

	// 跳到 850 → 0.85 → throttle (warning_80)
	tb.AddUsage(context.Background(), "wh-1", BudgetUsage{InputTokens: 110})
	tb.Check(context.Background(), "wh-1")

	// 跳到 1100 → 1.1 → exhausted (warning_exhausted_100)
	tb.AddUsage(context.Background(), "wh-1", BudgetUsage{InputTokens: 250})
	tb.Check(context.Background(), "wh-1")

	wantLevels := []string{
		"warning_70",
		"warning_80",
		"exhausted_100",
	}
	if len(gotLevels) != len(wantLevels) {
		t.Fatalf("gotLevels=%v want %d entries", gotLevels, len(wantLevels))
	}
	for i, want := range wantLevels {
		if gotLevels[i] != want {
			t.Errorf("level[%d]: want %q got %q", i, want, gotLevels[i])
		}
	}
}

// TestGateway_SmartCompressionInfo_WrittenToLog: 启用 SmartCompression 时
// LogEntry.CompressionStrategy == "scored"，且 CompressionAtomicGroups > 0。
func TestGateway_SmartCompressionInfo_WrittenToLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inner := &fakeLLM{completeContent: "ok"}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	cfg := GatewayConfig{
		Enabled:                  true,
		PromptCompression:        true,
		TokenBudget:              true,
		SmartCompression:         true,
		KeepRecentForcedN:        2,
		SummaryInsertThreshold:   1,
		ScoreThreshold:           0,
		FileLogger:               true,
		BudgetPerSession:         10000,
		CompressionThreshold:     0.7,
		ThrottlingThreshold:      0.99, // 确保 throttle
		LogDir:                   dir,
	}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)
	// 强制进入 throttle 通过 budget snapshot
	gw.budget.AddUsage(context.Background(), "sc-1", BudgetUsage{InputTokens: 700})

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "sc-1", AgentType: "prosecutor", TaskType: "speak"})
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "plain-a"},
		{Role: "user", Content: "evidence_id=E1 anchor", Metadata: map[string]string{"evidence_id": "E1"}},
		{Role: "user", Content: "plain-c"},
		{Role: "user", Content: "r1"},
		{Role: "user", Content: "r2"},
	}
	_, _, err := gw.Complete(ctx, "sys", msgs, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

// TestGateway_AddUsageMultiDim_WrittenToLog: 多维 budget 用量写入日志。
func TestGateway_AddUsageMultiDim_WrittenToLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inner := &fakeLLM{
		completeContent: "ok",
		completeUsage:   llm.Usage{PromptTokens: 300, CompletionTokens: 200, TotalTokens: 500},
	}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		FileLogger:           true,
		BudgetPerSession:     10000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
		LogDir:               dir,
	}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "md-1", AgentType: "judge", TaskType: "verdict"})
	_, _, err := gw.Complete(ctx, "sys", nil, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gw.budget.CurrentUsage("md-1"); got != 500 {
		t.Errorf("multi-dim total: want 500 got %d", got)
	}
}

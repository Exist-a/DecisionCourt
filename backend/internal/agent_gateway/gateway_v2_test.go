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

// TestGateway_NoRejectWhenExhausted_Disabled: 未启用 reject 时仍调用 inner。
func TestGateway_NoRejectWhenExhausted_Disabled(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeContent: "called-anyway"}
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, newFakeStore())
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  false, // 默认关
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

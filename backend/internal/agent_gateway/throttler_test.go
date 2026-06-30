package agent_gateway

import (
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

func TestThrottler_NoOpWhenNormal(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusNormal}, "speak")
	if out.MaxTokens != 500 || out.Temperature != 0.7 {
		t.Errorf("should not throttle normal: %+v", out)
	}
	if info.Applied {
		t.Errorf("applied should be false")
	}
}

func TestThrottler_ReducesMaxTokensAndTemperature(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusThrottle, Ratio: 0.85}, "speak")
	if !info.Applied {
		t.Fatalf("applied should be true")
	}
	if info.MaxTokensBefore != 500 {
		t.Errorf("before: want 500 got %d", info.MaxTokensBefore)
	}
	if info.MaxTokensAfter <= 0 || info.MaxTokensAfter >= 500 {
		t.Errorf("max_tokens should be reduced, got %d", info.MaxTokensAfter)
	}
	if out.Temperature != 0.2 {
		t.Errorf("temperature: want 0.2 got %f", out.Temperature)
	}
	if out.MaxTokens != info.MaxTokensAfter {
		t.Errorf("output max_tokens mismatch")
	}
}

func TestThrottler_ExhaustedMinimum(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "speak")
	if out.MaxTokens != 100 {
		t.Errorf("exhausted max_tokens should be 100, got %d", out.MaxTokens)
	}
	if info.MaxTokensAfter != 100 {
		t.Errorf("info.MaxTokensAfter: want 100 got %d", info.MaxTokensAfter)
	}
}

func TestThrottler_ZeroMaxTokens(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 0, Temperature: 0.7}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "speak")
	if out.MaxTokens != 100 {
		t.Errorf("zero max_tokens should fall back to 100, got %d", out.MaxTokens)
	}
	if !info.Applied {
		t.Errorf("applied should be true")
	}
}

func TestThrottler_CompressStatus(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusCompress, Ratio: 0.75}, "speak")
	if !info.Applied {
		t.Errorf("compress status should also throttle")
	}
	if out.MaxTokens >= 500 || out.Temperature != 0.2 {
		t.Errorf("compress should reduce: %+v", out)
	}
}

// 关键任务豁免：verdict / final / summary / assess 即使在 exhausted 时
// 也必须保留 max_tokens，截断会破坏业务输出。
func TestThrottler_ExemptsVerdict(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 2000, Temperature: 0.3}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "verdict")
	if out.MaxTokens != 2000 {
		t.Errorf("verdict should keep max_tokens, got %d", out.MaxTokens)
	}
	if info.Applied {
		t.Errorf("applied should be false for exempt task")
	}
	if !info.Exempted {
		t.Errorf("exempted should be true")
	}
	if info.ExemptReason == "" {
		t.Errorf("exempt reason should be set")
	}
}

func TestThrottler_ExemptsFinal(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 800, Temperature: 0.2}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.5}, "final")
	if out.MaxTokens != 800 {
		t.Errorf("final should keep max_tokens, got %d", out.MaxTokens)
	}
	if !info.Exempted {
		t.Errorf("final should be exempt")
	}
}

func TestThrottler_ExemptsSummary(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.3}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusThrottle, Ratio: 0.9}, "summary")
	if out.MaxTokens != 500 {
		t.Errorf("summary should keep max_tokens, got %d", out.MaxTokens)
	}
	if !info.Exempted {
		t.Errorf("summary should be exempt")
	}
}

func TestThrottler_ExemptsAssess(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.3}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusThrottle, Ratio: 0.85}, "assess")
	if out.MaxTokens != 500 {
		t.Errorf("assess should keep max_tokens, got %d", out.MaxTokens)
	}
	if !info.Exempted {
		t.Errorf("assess should be exempt")
	}
}

// 豁免时仍降 temperature 以稳定格式
func TestThrottler_ExemptReducesTemperature(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 2000, Temperature: 0.3}
	out, _ := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "verdict")
	if out.Temperature != 0.2 {
		t.Errorf("exempt should still reduce temperature to 0.2, got %f", out.Temperature)
	}
}

// 非豁免任务正常被限流
func TestThrottler_NonExemptStillThrottled(t *testing.T) {
	th := NewThrottler()
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7}
	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "react_speak_stream")
	if out.MaxTokens == 500 {
		t.Errorf("speak should be throttled, but kept 500")
	}
	if info.Exempted {
		t.Errorf("speak should not be exempt")
	}
}

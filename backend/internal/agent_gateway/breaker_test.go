package agent_gateway

// v0.9 (ADR 0013 §决策 3) Circuit Breaker 测试。
//
// 验证 5 个核心契约:
//   1. closed 状态正常调用,成功计数 + 1
//   2. 连续失败触发 open,后续调用走 fallback
//   3. fallback 函数返回降级内容 + 累计 fallbacks 计数
//   4. open → half-open → closed 状态恢复
//   5. fallback=nil 时熔断返回 ErrBreakerOpen

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/sony/gobreaker"
)

// successOp 返回成功的 operation。
func successOp(content string) LLMO {
	return func(_ context.Context) (string, llm.Usage, error) {
		return content, llm.Usage{TotalTokens: 10}, nil
	}
}

// failingOp 返回失败的 operation(模拟 LLM 100% 失败)。
func failingOp(err error) LLMO {
	return func(_ context.Context) (string, llm.Usage, error) {
		return "", llm.Usage{}, err
	}
}

// TestBreaker_Closed_NormalOperation 验证:closed 状态正常调用,不走 fallback。
// 这是 breaker 启用但服务正常时的零开销契约。
func TestBreaker_Closed_NormalOperation(t *testing.T) {
	t.Parallel()

	fallbackCalled := false
	fallback := func(_ context.Context, _ string, _ []llm.Message) (string, llm.Usage, error) {
		fallbackCalled = true
		return "fallback", llm.Usage{}, nil
	}

	cfg := BreakerConfig{Enabled: true, FailureRatio: 0.5, MinRequests: 3, OpenTimeoutSec: 30}.Normalize()
	b := NewLLMBreaker(cfg, fallback)

	content, _, err := b.Execute(context.Background(), successOp("success"), struct {
		SystemPrompt string
		Messages     []llm.Message
	}{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if content != "success" {
		t.Errorf("expected 'success', got %q", content)
	}
	if fallbackCalled {
		t.Error("fallback should not be called in closed state")
	}
	if state := b.State(); state != gobreaker.StateClosed {
		t.Errorf("expected closed state, got %v", state)
	}
}

// TestBreaker_OpensAfterFailureRate 验证:失败率超过阈值时熔断。
// 配置:MinRequests=3,FailureRatio=0.5 → 至少 3 个请求 + 50% 失败才熔断。
func TestBreaker_OpensAfterFailureRate(t *testing.T) {
	t.Parallel()

	fallbackCalled := 0
	fallback := func(_ context.Context, _ string, _ []llm.Message) (string, llm.Usage, error) {
		fallbackCalled++
		return "fallback-content", llm.Usage{}, nil
	}

	cfg := BreakerConfig{Enabled: true, FailureRatio: 0.5, MinRequests: 3, OpenTimeoutSec: 30}.Normalize()
	b := NewLLMBreaker(cfg, fallback)
	ctx := context.Background()

	// 调 5 次全部失败 → 失败率 100% → 必然熔断
	for i := 0; i < 5; i++ {
		_, _, err := b.Execute(ctx, failingOp(errors.New("llm error")), struct {
			SystemPrompt string
			Messages     []llm.Message
		}{})
		t.Logf("call %d: state=%v err=%v", i, b.State(), err)
		if i < 3 && err == nil {
			t.Fatalf("call %d: expected error (LLM should be called), got nil (fallback triggered prematurely)", i)
		}
	}

	// 验证熔断已触发
	if state := b.State(); state != gobreaker.StateOpen {
		t.Errorf("expected open state, got %v", state)
	}

	// 后续调用应走 fallback,而不是 inner
	content, _, err := b.Execute(ctx, successOp("should-not-reach"), struct {
		SystemPrompt string
		Messages     []llm.Message
	}{})
	if err != nil {
		t.Fatalf("expected fallback to handle open state, got error: %v", err)
	}
	if content != "fallback-content" {
		t.Errorf("expected fallback content, got %q", content)
	}
	if fallbackCalled == 0 {
		t.Error("expected fallback to be called when breaker is open")
	}

	trips, fallbacks, _ := b.Stats()
	if trips == 0 {
		t.Error("expected trips >= 1")
	}
	if fallbacks == 0 {
		t.Error("expected fallbacks >= 1")
	}
}

// TestBreaker_FallbackReturnsDegradedContent 验证:fallback 返回的内容会被业务
// 视为降级。fallback 应该不调 LLM(防止递归熔断),响应 < 100ms。
func TestBreaker_FallbackReturnsDegradedContent(t *testing.T) {
	t.Parallel()

	fallbackCalls := 0
	fallback := func(_ context.Context, sysPrompt string, _ []llm.Message) (string, llm.Usage, error) {
		fallbackCalls++
		// 简单断言:确认 fallback 收到原始 systemPrompt(给业务层做 keyword 估算用)
		if sysPrompt != "test-system" {
			t.Errorf("fallback should receive original systemPrompt, got %q", sysPrompt)
		}
		return "[degraded]", llm.Usage{}, nil
	}

	cfg := BreakerConfig{Enabled: true, FailureRatio: 0.5, MinRequests: 2, OpenTimeoutSec: 30}.Normalize()
	b := NewLLMBreaker(cfg, fallback)
	ctx := context.Background()

	// 触发熔断
	for i := 0; i < 3; i++ {
		b.Execute(ctx, failingOp(errors.New("err")), struct {
			SystemPrompt string
			Messages     []llm.Message
		}{SystemPrompt: "test-system"})
	}

	// 熔断后调一次
	content, _, err := b.Execute(ctx, successOp("from-inner"), struct {
		SystemPrompt string
		Messages     []llm.Message
	}{SystemPrompt: "test-system"})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if content != "[degraded]" {
		t.Errorf("expected fallback content '[degraded]', got %q", content)
	}
	if fallbackCalls == 0 {
		t.Error("expected fallback to be called")
	}
}

// TestBreaker_RecoversViaHalfOpen 验证:open → half-open → closed 的状态恢复。
// 这是 chaos test 的关键 —— LLM 恢复后 breaker 能自动恢复服务。
func TestBreaker_RecoversViaHalfOpen(t *testing.T) {
	t.Parallel()

	cfg := BreakerConfig{
		Enabled:             true,
		FailureRatio:        0.5,
		MinRequests:         2,
		OpenTimeoutSec:      1, // 短超时方便测试
		HalfOpenMaxRequests: 1,
	}.Normalize()
	b := NewLLMBreaker(cfg, nil) // 无 fallback,熔断时直接返回 ErrBreakerOpen
	ctx := context.Background()

	// 触发熔断
	for i := 0; i < 3; i++ {
		_, _, _ = b.Execute(ctx, failingOp(errors.New("err")), struct {
			SystemPrompt string
			Messages     []llm.Message
		}{})
	}
	if state := b.State(); state != gobreaker.StateOpen {
		t.Fatalf("expected open state after failures, got %v", state)
	}

	// 等 OpenTimeoutSec 后 → half-open
	time.Sleep(1100 * time.Millisecond)

	// 半开探测成功 → closed
	content, _, err := b.Execute(ctx, successOp("recovered"), struct {
		SystemPrompt string
		Messages     []llm.Message
	}{})
	if err != nil {
		t.Fatalf("expected half-open success to return content, got error: %v", err)
	}
	if content != "recovered" {
		t.Errorf("expected 'recovered', got %q", content)
	}
	if state := b.State(); state != gobreaker.StateClosed {
		t.Errorf("expected closed state after recovery, got %v", state)
	}
}

// TestBreaker_NoFallback_ReturnsError 验证:无 fallback 时熔断返回 ErrBreakerOpen。
// 业务层应识别这个错误,决定是降级返回还是返回 503。
func TestBreaker_NoFallback_ReturnsError(t *testing.T) {
	t.Parallel()

	cfg := BreakerConfig{Enabled: true, FailureRatio: 0.5, MinRequests: 2, OpenTimeoutSec: 30}.Normalize()
	b := NewLLMBreaker(cfg, nil) // 无 fallback
	ctx := context.Background()

	// 触发熔断
	for i := 0; i < 3; i++ {
		_, _, _ = b.Execute(ctx, failingOp(errors.New("err")), struct {
			SystemPrompt string
			Messages     []llm.Message
		}{})
	}

	// 熔断时调用 → 返回 ErrBreakerOpen
	_, _, err := b.Execute(ctx, successOp("from-inner"), struct {
		SystemPrompt string
		Messages     []llm.Message
	}{})
	if err == nil {
		t.Fatal("expected error when breaker open and no fallback")
	}
	if !errors.Is(err, ErrBreakerOpen) {
		t.Errorf("expected ErrBreakerOpen, got %v", err)
	}
}

// TestBreaker_Disabled_PassesThrough 验证:Enabled=false 时 breaker 不参与,
// operation 直通。这是默认配置(breaker 关闭)的安全契约。
func TestBreaker_Disabled_PassesThrough(t *testing.T) {
	t.Parallel()

	cfg := BreakerConfig{Enabled: false}.Normalize()
	b := NewLLMBreaker(cfg, nil)

	content, _, err := b.Execute(context.Background(), successOp("direct"), struct {
		SystemPrompt string
		Messages     []llm.Message
	}{})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if content != "direct" {
		t.Errorf("expected 'direct', got %q", content)
	}
	if state := b.State(); state != gobreaker.StateClosed {
		t.Errorf("expected closed state when disabled, got %v", state)
	}
}

// TestDefaultKeywordFallback_NoLLMCall 验证:DefaultKeywordFallback 不调 LLM,
// 响应 deterministic。这是防止"fallback 递归触发熔断"的关键契约。
func TestDefaultKeywordFallback_NoLLMCall(t *testing.T) {
	t.Parallel()

	start := time.Now()
	content, usage, err := DefaultKeywordFallback(context.Background(), "any system", []llm.Message{{Role: "user", Content: "any"}})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if content == "" {
		t.Error("expected non-empty fallback content")
	}
	if usage.TotalTokens != 0 {
		t.Errorf("expected zero token usage in fallback, got %d", usage.TotalTokens)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("fallback took %v, expected < 100ms (must not call LLM)", elapsed)
	}
	// content 必须 deterministic(便于前端识别降级状态)
	content2, _, _ := DefaultKeywordFallback(context.Background(), "different", nil)
	if content != content2 {
		t.Error("DefaultKeywordFallback should return deterministic content")
	}
}
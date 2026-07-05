package agent_gateway

// v0.9 (ADR 0013 §决策 1) per-call Timeout 测试。
//
// 验证：Gateway.Complete / StreamComplete 在 inner hang 超过 LLMTimeoutSec
// 时,通过 context.WithTimeout 触发 ctx.Done(),立即返回 context.DeadlineExceeded
// 错误(或 stream Done+Err),不阻塞 trial。

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// slowFakeLLM 模拟 LLM hang:延迟 delay 后返回内容,但 ctx 取消时立即退出。
// 用于验证 per-call timeout 能在 hang 时及时终止。
type slowFakeLLM struct {
	mu        sync.Mutex
	delay     time.Duration
	callCount int
}

func (s *slowFakeLLM) Complete(ctx context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	s.mu.Lock()
	s.callCount++
	s.mu.Unlock()
	select {
	case <-time.After(s.delay):
		return "slow but success", llm.Usage{}, nil
	case <-ctx.Done():
		return "", llm.Usage{}, ctx.Err()
	}
}

func (s *slowFakeLLM) StreamComplete(ctx context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 2)
	go func() {
		defer close(ch)
		select {
		case <-time.After(s.delay):
			ch <- llm.StreamChunk{Content: "slow chunk", Done: true}
		case <-ctx.Done():
			ch <- llm.StreamChunk{Done: true, Err: ctx.Err()}
		}
	}()
	return ch
}

// TestGateway_Complete_HangTriggersTimeout 验证：inner hang 超过 LLMTimeoutSec
// 时,Gateway.Complete 在 ctx.Done() 触发时立即返回 context.DeadlineExceeded,
// 不阻塞 trial。这是 ADR 0013 §决策 1 的核心契约。
func TestGateway_Complete_HangTriggersTimeout(t *testing.T) {
	t.Parallel()

	// 延迟 10s 的"挂掉的 LLM",但 timeout 设为 1s,禁用 retryer → 应该 ~1s 后返回 deadline error。
	// 禁用 retry 是关键:retryer 退避 + 重试可能让 inner 真正返回,这样 timeout
	// 就测不出来(timeout 触发只是 ctx 取消,inner 实际还在运行)。
	inner := &slowFakeLLM{delay: 10 * time.Second}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:       true,
		Fallback:      false, // 禁用 retryer,纯测 timeout
		LLMTimeoutSec: 1,
	})

	start := time.Now()
	_, _, err := gw.Complete(context.Background(), "sys", []llm.Message{{Role: "user", Content: "hi"}}, llm.CompletionOptions{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from hung LLM, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	// 1s timeout 应该让 Complete 在 ~1s 内返回
	if elapsed > 3*time.Second {
		t.Errorf("Complete took %v, expected < 3s (timeout should kick in at ~1s)", elapsed)
	}
	t.Logf("Complete returned in %v with err=%v (tested with delay=10s, timeout=1s)", elapsed, err)
}

// TestGateway_Complete_FastCallNotAffected 验证：正常调用不被 timeout 干扰。
// 即使 LLMTimeoutSec 设了 1s,inner 在 100ms 内返回,无 timeout 报错。
func TestGateway_Complete_FastCallNotAffected(t *testing.T) {
	t.Parallel()

	inner := &slowFakeLLM{delay: 100 * time.Millisecond}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:       true,
		Fallback:      false,
		LLMTimeoutSec: 1, // 1s timeout,inner 100ms 完成
	})

	content, _, err := gw.Complete(context.Background(), "sys", []llm.Message{{Role: "user", Content: "hi"}}, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "slow but success" {
		t.Errorf("expected 'slow but success', got: %q", content)
	}
}

// TestGateway_Complete_DefaultTimeout90s 验证：未配置时,默认 90s timeout
// 生效（防止 .env 漏配导致 timeout=0 = 关闭）。
func TestGateway_Complete_DefaultTimeout90s(t *testing.T) {
	t.Parallel()

	inner := &slowFakeLLM{delay: 50 * time.Millisecond}
	// LLMTimeoutSec=0 → Normalize() 应补默认 90
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:       true,
		Fallback:      false,
		LLMTimeoutSec: 0, // 触发 Normalize 默认值
	})

	if gw.cfg.LLMTimeoutSec != 90 {
		t.Fatalf("expected default LLMTimeoutSec=90 after Normalize, got: %d", gw.cfg.LLMTimeoutSec)
	}

	// 快速调用应正常返回
	content, _, err := gw.Complete(context.Background(), "sys", []llm.Message{{Role: "user", Content: "hi"}}, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "slow but success" {
		t.Errorf("expected 'slow but success', got: %q", content)
	}
}

// TestGateway_StreamComplete_HangTriggersTimeout 验证：流式 hang 也受 timeout
// 约束,90s 内未结束则 ctx.Done() → stream 收到 Done+Err。
func TestGateway_StreamComplete_HangTriggersTimeout(t *testing.T) {
	t.Parallel()

	inner := &slowFakeLLM{delay: 5 * time.Second}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:       true,
		LLMTimeoutSec: 1,
	})

	start := time.Now()
	ch := gw.StreamComplete(context.Background(), "sys", []llm.Message{{Role: "user", Content: "hi"}}, llm.CompletionOptions{})

	var firstErr error
	for chunk := range ch {
		if chunk.Err != nil {
			firstErr = chunk.Err
			break
		}
	}
	elapsed := time.Since(start)

	if firstErr == nil {
		t.Fatal("expected stream chunk error from hung LLM, got nil")
	}
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", firstErr)
	}
	if elapsed > 3*time.Second {
		t.Errorf("StreamComplete took %v, expected < 3s (timeout should kick in at ~1s)", elapsed)
	}
	t.Logf("StreamComplete returned in %v with err=%v", elapsed, firstErr)
}
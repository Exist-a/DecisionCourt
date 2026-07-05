package agent_gateway

// v0.9 (ADR 0013 §决策 2) Gateway + Cache 集成测试。
//
// 验证:当 cache 启用时,Gateway.Complete 入口会先查 cache,命中短路返回。
// 这是 PR-A 的核心契约 —— 同 prompt 第二次调用 inner 不会被触发。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// countingFakeLLM 记录调用次数,用于验证 cache 命中时 inner 不被调用。
type countingFakeLLM struct {
	count int64
	value string
}

func (c *countingFakeLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	atomic.AddInt64(&c.count, 1)
	return c.value, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil
}

func (c *countingFakeLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Content: c.value, Done: true}
	close(ch)
	return ch
}

func TestGateway_Complete_CacheHitShortCircuits(t *testing.T) {
	t.Parallel()

	inner := &countingFakeLLM{value: "from-llm"}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:       true,
		Fallback:      false,
		CacheEnabled:  true,
		CacheTTLSec:   60,
		CacheMaxEntries: 100,
	})

	ctx := context.Background()
	messages := []llm.Message{{Role: "user", Content: "evidence-A"}}

	// 第一次:cache miss → 调 inner
	content1, _, err := gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
	if err != nil {
		t.Fatalf("first Complete failed: %v", err)
	}
	if content1 != "from-llm" {
		t.Errorf("first call: got %q want %q", content1, "from-llm")
	}
	if atomic.LoadInt64(&inner.count) != 1 {
		t.Fatalf("after first call: inner.count=%d, expected 1", inner.count)
	}

	// 第二次:同 prompt → cache hit,inner 不应再被调用
	content2, _, err := gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
	if err != nil {
		t.Fatalf("second Complete failed: %v", err)
	}
	if content2 != "from-llm" {
		t.Errorf("second call: got %q want %q", content2, "from-llm")
	}
	if atomic.LoadInt64(&inner.count) != 1 {
		t.Errorf("after second call: inner.count=%d, expected 1 (cache hit should short-circuit)", inner.count)
	}

	// 验证 cache stats
	if gw.cache == nil {
		t.Fatal("expected cache to be initialized")
	}
	hits, misses, size := gw.cache.Stats()
	if hits != 1 {
		t.Errorf("expected hits=1, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("expected misses=1, got %d", misses)
	}
	if size != 1 {
		t.Errorf("expected size=1, got %d", size)
	}
}

func TestGateway_Complete_CacheMissOnDifferentPrompt(t *testing.T) {
	t.Parallel()

	inner := &countingFakeLLM{value: "from-llm"}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:        true,
		Fallback:       false,
		CacheEnabled:   true,
		CacheTTLSec:    60,
		CacheMaxEntries: 100,
	})

	ctx := context.Background()

	// 调 3 次不同 prompt → 3 次 cache miss → inner 被调 3 次
	for i := 0; i < 3; i++ {
		messages := []llm.Message{{Role: "user", Content: string(rune('a' + i))}}
		_, _, err := gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	if got := atomic.LoadInt64(&inner.count); got != 3 {
		t.Errorf("expected inner.count=3 (all miss), got %d", got)
	}
}

func TestGateway_Complete_CacheDisabled_NoCaching(t *testing.T) {
	t.Parallel()

	inner := &countingFakeLLM{value: "from-llm"}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:      true,
		Fallback:     false,
		CacheEnabled: false, // 禁用
	})

	ctx := context.Background()
	messages := []llm.Message{{Role: "user", Content: "evidence-A"}}

	// 2 次同 prompt → cache 禁用,inner 调 2 次
	for i := 0; i < 2; i++ {
		_, _, _ = gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
	}
	if got := atomic.LoadInt64(&inner.count); got != 2 {
		t.Errorf("expected inner.count=2 (cache disabled), got %d", got)
	}
	if gw.cache != nil {
		t.Error("expected cache to be nil when CacheEnabled=false")
	}
}

func TestGateway_Complete_CacheRespectsTemperature(t *testing.T) {
	t.Parallel()

	inner := &countingFakeLLM{value: "from-llm"}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:        true,
		Fallback:       false,
		CacheEnabled:   true,
		CacheTTLSec:    60,
		CacheMaxEntries: 100,
	})

	ctx := context.Background()
	messages := []llm.Message{{Role: "user", Content: "evidence-A"}}

	// 同 prompt,不同 temperature → 视为不同 key → 2 次 cache miss → inner 调 2 次
	_, _, _ = gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.3})
	_, _, _ = gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})

	if got := atomic.LoadInt64(&inner.count); got != 2 {
		t.Errorf("expected inner.count=2 (different temp = different cache key), got %d", got)
	}
}

func TestGateway_Complete_CacheBoundedByLRU(t *testing.T) {
	t.Parallel()

	inner := &countingFakeLLM{value: "from-llm"}
	gw := NewWithConfig(inner, nil, "test-model", GatewayConfig{
		Enabled:        true,
		Fallback:       false,
		CacheEnabled:   true,
		CacheTTLSec:    60,
		CacheMaxEntries: 2, // 极小 LRU 方便测淘汰
	})

	ctx := context.Background()

	// 3 次不同 prompt → 第二次后超 LRU 上限,最早被淘汰 → inner 调 3 次
	for i := 0; i < 3; i++ {
		messages := []llm.Message{{Role: "user", Content: string(rune('a' + i))}}
		_, _, _ = gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
	}
	if got := atomic.LoadInt64(&inner.count); got != 3 {
		t.Errorf("expected inner.count=3, got %d", got)
	}
	// 第一个 prompt 重新调 → 应该 cache miss → inner 调第 4 次
	messages := []llm.Message{{Role: "user", Content: "a"}}
	_, _, _ = gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
	if got := atomic.LoadInt64(&inner.count); got != 4 {
		t.Errorf("expected inner.count=4 (LRU evicted first prompt), got %d", got)
	}
}

func TestGateway_Complete_CacheFailureNotCached(t *testing.T) {
	t.Parallel()

	// 失败 case 的 inner
	failingFake := &failingFakeLLM{err: context.DeadlineExceeded}
	gw := NewWithConfig(failingFake, nil, "test-model", GatewayConfig{
		Enabled:         true,
		Fallback:        true, // 显式开启,后面测试会检查
		PromptCompression: true, // 任意子开关开启 → 避免 isChildDefault 把 Fallback 默认开
		CacheEnabled:    true,
		CacheTTLSec:     60,
		CacheMaxEntries: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := []llm.Message{{Role: "user", Content: "evidence-A"}}

	// 调 2 次都失败 → cache 不应缓存错误 → 第二次仍然走 inner(retryer 启用
// 时 inner 被调 6 次,否则 2 次;但 cache size 应始终为 0)。
	for i := 0; i < 2; i++ {
		_, _, err := gw.Complete(ctx, "sys", messages, llm.CompletionOptions{Temperature: 0.7})
		if err == nil {
			t.Fatalf("call %d: expected error, got nil", i)
		}
	}
	// 关键 assertion:cache size == 0（错误永不缓存）,hits == 0
	hits, misses, size := gw.cache.Stats()
	if size != 0 {
		t.Errorf("expected cache size=0 (errors not cached), got %d", size)
	}
	if hits != 0 {
		t.Errorf("expected hits=0 (errors shouldn't count as hit), got %d", hits)
	}
	if inner_count := atomic.LoadInt64(&failingFake.count); inner_count == 0 {
		t.Errorf("expected inner to be called, but count=0")
	}
	_ = misses
}

type failingFakeLLM struct {
	count int64
	err   error
}

func (f *failingFakeLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	atomic.AddInt64(&f.count, 1)
	return "", llm.Usage{}, f.err
}

func (f *failingFakeLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true, Err: f.err}
	close(ch)
	return ch
}

// 防止 time 包未被使用的 import 警告
var _ = time.Second
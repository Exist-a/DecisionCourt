package agent_gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// fakeBudgetLLM 可以控制每次返回的 usage，用于预算测试。
// 为了匹配 v2 Token Budget 的多维计数（InputTokens / OutputTokens），这里把
// total 拆成 600/200 这样的真实比例（避免仅设 Total 时被认作 0）。
type fakeBudgetLLM struct {
	mu           sync.Mutex
	calls        int
	usagePerCall llm.Usage
}

func (f *fakeBudgetLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return "ok", f.usagePerCall, nil
}

func (f *fakeBudgetLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

func TestGateway_Advanced_BudgetCompressThrottleAndLog(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeBudgetLLM{usagePerCall: llm.Usage{PromptTokens: 600, CompletionTokens: 200, TotalTokens: 800}}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	cfg := GatewayConfig{
		Enabled:           true,
		PromptCompression: true,
		TokenBudget:       true,
		Throttling:        true,
		Fallback:          true,
		FileLogger:        true,
		BudgetPerSession:  1000,
		LogDir:            dir,
	}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "sess-1", AgentType: "prosecutor", TaskType: "speak"})
	msgs := make([]llm.Message, 12)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Content: strings.Repeat("a", 400)}
	}

	// 第一次：normal 状态，不压缩不限流
	gw.Complete(ctx, "sys", msgs, llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7})
	// 第二次：used=800，ratio=0.8 -> throttle
	gw.Complete(ctx, "sys", msgs, llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7})
	// 第三次：used=1600，ratio=1.6 -> exhausted
	gw.Complete(ctx, "sys", msgs, llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7})

	if inner.calls != 3 {
		t.Errorf("want 3 calls, got %d", inner.calls)
	}

	// 验证日志文件
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 log file, got %d", len(files))
	}
	data, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 log lines, got %d", len(lines))
	}

	var first, second, third LogEntry
	json.Unmarshal([]byte(lines[0]), &first)
	json.Unmarshal([]byte(lines[1]), &second)
	json.Unmarshal([]byte(lines[2]), &third)

	if first.Compressed || first.Throttled {
		t.Errorf("first should not be compressed/throttled: compressed=%v throttled=%v", first.Compressed, first.Throttled)
	}
	if !second.Throttled {
		t.Errorf("second should be throttled")
	}
	if !third.Throttled || !third.Compressed {
		t.Errorf("third should be both throttled and compressed")
	}
	if third.BudgetRatio <= 1.0 {
		t.Errorf("third budget ratio should exceed 1.0, got %.2f", third.BudgetRatio)
	}
}

func TestGateway_Advanced_FallbackRetry(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeRetryLLM{failures: 2}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	cfg := GatewayConfig{
		Enabled: true,
		Fallback: true,
		FileLogger: true,
		LogDir: dir,
	}.Normalize()
	// 用短退避加速测试
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)
	gw.retryer = NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond})

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "sess-2", AgentType: "judge", TaskType: "assess"})
	_, _, err := gw.Complete(ctx, "sys", nil, llm.CompletionOptions{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if inner.calls != 3 {
		t.Errorf("want 3 calls (1 + 2 retries), got %d", inner.calls)
	}

	files, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.RetryCount != 2 {
		t.Errorf("retry count: want 2 got %d", entry.RetryCount)
	}
}

func TestGateway_Advanced_FallbackExhausted(t *testing.T) {
	inner := &fakeRetryLLM{failures: 10}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	cfg := GatewayConfig{Enabled: true, Fallback: true}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)
	gw.retryer = NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond})

	ctx := WithTrace(context.Background(), Trace{})
	_, _, err := gw.Complete(ctx, "sys", nil, llm.CompletionOptions{})
	if err == nil {
		t.Fatal("expected err after max retries")
	}
	if inner.calls != 4 {
		t.Errorf("want 4 calls (1 + 3 retries), got %d", inner.calls)
	}
}

func TestGateway_Advanced_DisabledNoFileLog(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeBudgetLLM{usagePerCall: llm.Usage{TotalTokens: 10}}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	cfg := GatewayConfig{Enabled: false, FileLogger: true, LogDir: dir}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "sess-3", AgentType: "clerk", TaskType: "summary"})
	gw.Complete(ctx, "sys", nil, llm.CompletionOptions{})

	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Errorf("disabled gateway should not write file logs")
	}
}

// fakeRetryLLM 用于测试 fallback。
type fakeRetryLLM struct {
	mu       sync.Mutex
	calls    int
	failures int
}

func (f *fakeRetryLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failures {
		return "", llm.Usage{}, errors.New("transient")
	}
	return "ok", llm.Usage{TotalTokens: 10}, nil
}

func (f *fakeRetryLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

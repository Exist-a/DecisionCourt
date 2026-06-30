package agent_gateway

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// fakeLLM 模拟 llm.Client，可由测试控制每次调用的内容/错误/usage/延迟。
type fakeLLM struct {
	mu sync.Mutex

	completeContent string
	completeUsage   llm.Usage
	completeErr     error
	completeCalls   int

	streamChunks []string
	streamErr    error
	streamCalls  int
}

func (f *fakeLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls++
	return f.completeContent, f.completeUsage, f.completeErr
}

func (f *fakeLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, len(f.streamChunks)+1)
	f.mu.Lock()
	f.streamCalls++
	f.mu.Unlock()
	go func() {
		defer close(ch)
		for _, c := range f.streamChunks {
			ch <- llm.StreamChunk{Content: c}
		}
		if f.streamErr != nil {
			ch <- llm.StreamChunk{Done: true, Err: f.streamErr}
		} else {
			ch <- llm.StreamChunk{Done: true}
		}
	}()
	return ch
}

// 验证：Complete 成功路径 → 写一条 status=success 记录，字段映射正确。
func TestGateway_Complete_Success(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{
		completeContent: "hello",
		completeUsage:   llm.Usage{PromptTokens: 7, CompletionTokens: 11, TotalTokens: 18},
	}
	store := newFakeStore()
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)

	gw := Wrap(inner, rec, "deepseek-chat")
	gwClient := gw.(llm.Client)

	ctx := WithTrace(context.Background(), Trace{
		SessionUUID: "s1",
		AgentType:   "prosecutor",
		TaskType:    "speak",
	})
	content, usage, err := gwClient.Complete(ctx, "sys", nil, llm.CompletionOptions{Model: "deepseek-chat"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if content != "hello" {
		t.Errorf("content: %q", content)
	}
	if usage.TotalTokens != 18 {
		t.Errorf("usage.TotalTokens: %d", usage.TotalTokens)
	}
	if inner.completeCalls != 1 {
		t.Errorf("inner.calls: %d", inner.completeCalls)
	}
	if got := len(store.rows); got != 1 {
		t.Fatalf("expected 1 row, got %d", got)
	}
	row := store.rows[0]
	if row.Status != StatusSuccess {
		t.Errorf("status: %q", row.Status)
	}
	if row.Model != "deepseek-chat" {
		t.Errorf("model: %q", row.Model)
	}
	if row.PromptTokens != 7 || row.CompletionTokens != 11 {
		t.Errorf("tokens: %+v", row)
	}
	if row.SessionUUID != "s1" || row.AgentType != "prosecutor" || row.TaskType != "speak" {
		t.Errorf("trace: %+v", row)
	}
	if row.ErrorMsg != "" {
		t.Errorf("ErrorMsg should be empty: %q", row.ErrorMsg)
	}
}

// 验证：Complete 失败 → status=error + err msg 写入，但调用方仍拿到 err。
func TestGateway_Complete_Error(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeErr: errors.New("upstream 502")}
	store := newFakeStore()
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)
	gwClient := Wrap(inner, rec, "deepseek-chat").(llm.Client)

	_, _, err := gwClient.Complete(context.Background(), "sys", nil, llm.CompletionOptions{Model: "deepseek-chat"})
	if err == nil {
		t.Fatal("expected err to bubble up")
	}
	if !strings.Contains(err.Error(), "upstream 502") {
		t.Errorf("err should contain upstream 502, got %v", err)
	}
	if got := len(store.rows); got != 1 {
		t.Fatalf("want 1 row, got %d", got)
	}
	row := store.rows[0]
	if row.Status != StatusError {
		t.Errorf("status: %q", row.Status)
	}
	if row.ErrorMsg != "upstream 502" {
		t.Errorf("ErrorMsg: %q", row.ErrorMsg)
	}
}

// 验证：Recorder 关闭时 Gateway 仍能跑（noop），且不写库。
func TestGateway_Complete_RecorderDisabled(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{completeContent: "ok"}
	rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
	gwClient := Wrap(inner, rec, "deepseek-chat").(llm.Client)

	content, _, err := gwClient.Complete(context.Background(), "sys", nil, llm.CompletionOptions{Model: "deepseek-chat"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if content != "ok" {
		t.Errorf("content: %q", content)
	}
}

// 验证：StreamComplete 把 chunk 透传给调用方，并在结束后埋点。
func TestGateway_StreamComplete_PassesThrough(t *testing.T) {
	t.Parallel()
	inner := &fakeLLM{streamChunks: []string{"a", "b", "c"}}
	store := newFakeStore()
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)
	gwClient := Wrap(inner, rec, "deepseek-chat").(llm.Client)

	ch := gwClient.StreamComplete(context.Background(), "sys", nil, llm.CompletionOptions{Model: "deepseek-chat"})
	var got []string
	for c := range ch {
		got = append(got, c.Content)
	}
	if strings.Join(got, "") != "abc" {
		t.Errorf("chunks: %v", got)
	}
	if got := len(store.rows); got != 1 {
		t.Fatalf("want 1 row, got %d", got)
	}
	if store.rows[0].Status != StatusSuccess {
		t.Errorf("status: %q", store.rows[0].Status)
	}
}

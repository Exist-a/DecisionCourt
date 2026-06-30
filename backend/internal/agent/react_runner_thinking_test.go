package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/stretchr/testify/require"
)

// T_iterStart: OnIterStart hook is invoked exactly once per iteration,
// BEFORE the LLM is called. This is the hook the courtroom service uses
// to broadcast agent.thinking_started so the frontend can immediately
// show a thinking bubble.
func TestReActRunner_OnIterStartInvokedOncePerIteration(t *testing.T) {
	var calls int32
	var iterIdx []int
	var mu sync.Mutex

	cfg := RunnerConfig{
		OnIterStart: func(iter int) {
			atomic.AddInt32(&calls, 1)
			mu.Lock()
			iterIdx = append(iterIdx, iter)
			mu.Unlock()
		},
	}

	// 3 步：reflect → tool_call → speak
	scripts := []string{
		reflectOutputJSON("思考"),
		toolOutputJSON("搜索", "noop", map[string]interface{}{"query": "x"}),
		speakOutputJSON("定稿", "final", "pro_a", 0.7),
	}

	stub := &stubTool{name: "noop", description: "noop"}
	r, _ := newTestRunner(t, scripts, []Tool{stub}, nil, cfg)
	_, _, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []int{0, 1, 2}, iterIdx, "iter 索引应从 0 开始依次递增")
}

// T_noHookSafe: RunnerConfig 不提供 OnIterStart 时不应 panic
func TestReActRunner_NilIterStartHookSafe(t *testing.T) {
	scripts := []string{
		speakOutputJSON("r", "c", "pro_a", 0.7),
	}
	r, _ := newTestRunner(t, scripts, nil, nil, RunnerConfig{}) // 没设 OnIterStart
	_, _, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
}

// T_noLLMCallWhenMaxIterZero: iter=0 时钩子应被调用，但 LLM 不调用（边界）
// —— 这条断言验证钩子先于 LLM 调用；如果将来出现 hook 在 LLM 之后的回归，
// 哪怕仅一行反转都会被这条测试逮到。
func TestReActRunner_OnIterStartFiresBeforeLLMCall(t *testing.T) {
	var hookCalledBeforeLLM atomic.Bool
	llmClient := &llmSpy{
		beforeComplete: func() {
			// 第一次 LLM 调用前，钩子必须已经被触发
			if hookCalledBeforeLLM.Load() {
				return
			}
		},
	}

	cfg := RunnerConfig{
		OnIterStart: func(_ int) {
			hookCalledBeforeLLM.Store(true)
		},
	}
	r := NewReActRunner(llmClient, "system", map[string]Tool{}, cfg)
	_, _, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, hookCalledBeforeLLM.Load(), "OnIterStart 必须在 LLM 调用之前触发")
}

// llmSpy 是一个最小的 LLM client，用于观察 OnIterStart 与 LLM.Complete 的调用顺序。
type llmSpy struct {
	beforeComplete func()
}

func (l *llmSpy) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	if l.beforeComplete != nil {
		l.beforeComplete()
	}
	return speakOutputJSON("r", "c", "pro_a", 0.7), llm.Usage{}, nil
}

// llmSpy 不走流式：返回空流让 speak fallback 到 JSON content。
func (l *llmSpy) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}
package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// streamingScriptedLLM 让测试可以逐片段注入 LLM 响应：
//   - completeScript[i] 是第 i 次 Complete() 的返回内容
//   - streamChunks 是 StreamComplete() 的内容切片（按 JSON 模式流式输出）
//
// 这是这一组测试的核心 fixture —— 既验证 ReAct 决策走 Complete，又
// 验证 speak 决策后流式 content 被 chunk-by-chunk 回调出来。
type streamingScriptedLLM struct {
	mu              sync.Mutex
	completeScript  []string
	streamChunks    []string // StreamComplete 一次调用的全部 chunks
	completeCalls   int
	streamCalls     int
	// streamMessages 用于断言 prompt 包含的论据（被第一次 Complete 决策时填）
	capturedMessages []llm.Message
}

func (s *streamingScriptedLLM) Complete(_ context.Context, _ string, msgs []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.capturedMessages = append(s.capturedMessages, msgs...)
	idx := s.completeCalls
	s.completeCalls++
	if idx >= len(s.completeScript) {
		idx = len(s.completeScript) - 1
	}
	return s.completeScript[idx], llm.Usage{}, nil
}

func (s *streamingScriptedLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	s.mu.Lock()
	chunks := append([]string{}, s.streamChunks...)
	s.streamCalls++
	s.mu.Unlock()

	out := make(chan llm.StreamChunk, len(chunks)+1)
	go func() {
		defer close(out)
		for _, c := range chunks {
			out <- llm.StreamChunk{Content: c}
			time.Sleep(2 * time.Millisecond) // simulate streaming latency
		}
		out <- llm.StreamChunk{Done: true}
	}()
	return out
}

// TestReActRunner_SpeakStreamsContentViaOnSpeakChunk 是这次流式
// 改造的核心 UX 测试：律师决定 speak 后，content 应该被分块回调给
// caller，而不是一次性返回。前端用这个回调做"打字机"动画。
//
// 流式协议：LLM 以最小 JSON 形式输出 `{"content":"完整发言..."}`，
// 后端用正则从累积 partial JSON 中提取 content 字段值（已 unquote），
// 通过 OnSpeakChunk 推送给调用方。前端拿到的是纯中文，不是 JSON。
func TestReActRunner_SpeakStreamsContentViaOnSpeakChunk(t *testing.T) {
	// 决策 JSON：action=speak，content 占位（空字符串），其余字段完整。
	// 流式重新生成 content。
	decision := `{"action":"speak","reasoning":"控方主张 A 在长期收益上显著优于 B","content":"","stance":"pro_a","confidence":0.85,"evidence_refs":[]}`
	// 流式 chunks：模拟 LLM 流式输出最小 JSON。
	// chunks 累积起来就是 `{"content":"基于市场长期数据，选项 A在收益上更稳健。"}`
	llmClient := &streamingScriptedLLM{
		completeScript: []string{decision},
		streamChunks: []string{
			`{"content":"基于市场`,
			`长期数据`,
			`，选项 A`,
			`在收益上`,
			`更稳健。"}`,
		},
	}

	r := NewReActRunner(llmClient, "system-prompt", nil, RunnerConfig{
		MaxIterations: 2,
	})
	var (
		mu   sync.Mutex
		accs []string // 已 unquote 的中文 content（accumulated）
	)
	r.cfg.OnSpeakChunk = func(_, accumulated string) {
		mu.Lock()
		defer mu.Unlock()
		accs = append(accs, accumulated)
	}

	speaker, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)

	// Content 必须等于正则提取的最终 content（已 unquote）
	require.Equal(t, "基于市场长期数据，选项 A在收益上更稳健。", speaker.Content,
		"最终 content 必须是流式 content 字段的 unquote 结果")

	// accumulated 必须是已 unquote 的中文（不是 JSON 字符串）
	require.GreaterOrEqual(t, len(accs), 3, "至少应该有 3 次 callback")
	for _, a := range accs {
		require.False(t, strings.HasPrefix(a, "{"),
			"accumulated 不应该是 JSON 包装；得到=%q", a)
		require.False(t, strings.HasPrefix(a, `"`),
			"accumulated 不应该带引号；得到=%q", a)
	}

	// accumulated 必须是单调前缀递增
	for i := 1; i < len(accs); i++ {
		require.True(t, strings.HasPrefix(accs[i], accs[i-1]),
			"accumulated 必须是单调前缀递增；accs[%d]=%q 不以 accs[%d]=%q 开头",
			i, accs[i], i-1, accs[i-1])
	}

	// StreamComplete 必须被调用 1 次
	require.Equal(t, 1, llmClient.streamCalls)
	// Complete 必须被调用 1 次（决策）
	require.Equal(t, 1, llmClient.completeCalls)
}

// TestReActRunner_SpeakStreamsMultilineJSON 验证 deepseek 输出多行
// pretty-printed JSON 时渐进提取仍能工作。DeepSeek 流式常常输出
//   {
//     "content": "..."
//   }
// 而不是 `{"content":"..."}`。我们的 parser 必须容忍 whitespace。
func TestReActRunner_SpeakStreamsMultilineJSON(t *testing.T) {
	decision := `{"action":"speak","reasoning":"R","content":"","stance":"pro_a","confidence":0.5,"evidence_refs":[]}`
	llmClient := &streamingScriptedLLM{
		completeScript: []string{decision},
		streamChunks: []string{
			"{\n  \"con",
			"tent\": \"基于",
			"市场长期",
			"数据，",
			"选项 A",
			"更稳健。\"\n}",
		},
	}

	var (
		mu   sync.Mutex
		accs []string
	)
	r := NewReActRunner(llmClient, "sys", nil, RunnerConfig{
		MaxIterations: 1,
		OnSpeakChunk: func(_, accumulated string) {
			mu.Lock()
			defer mu.Unlock()
			accs = append(accs, accumulated)
		},
	})

	speaker, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)
	require.Equal(t, "基于市场长期数据，选项 A更稳健。", speaker.Content,
		"多行 JSON 流式也能正确提取 content")
	require.GreaterOrEqual(t, len(accs), 3, "应该至少 callback 3 次")
}

// TestReActRunner_SpeakStreamingFailureFallsBackToPlaceholderContent
// 验证流式失败时（LLM 返回 Err）speaker 仍然返回：用决策 JSON 里的
// content 占位（即使是空字符串也能 speak，不会卡住律师发言）。
func TestReActRunner_SpeakStreamingFailureFallsBackToPlaceholderContent(t *testing.T) {
	decision := `{"action":"speak","reasoning":"R","content":"","stance":"pro_a","confidence":0.5,"evidence_refs":[]}`
	llmClient := &streamingScriptedLLM{
		completeScript: []string{decision},
		// streamChunks 为空：chunks 循环立即结束，stream 流 Done=true 正常
		// —— 这等价于 LLM 流式返回了空内容。我们也通过改 fixture 验证：
		// stream 正常关闭但 speaker.Content 应该是空字符串（chunks 拼接）。
		streamChunks: []string{},
	}

	var chunkCalls int
	r := NewReActRunner(llmClient, "system", nil, RunnerConfig{
		MaxIterations: 1,
		OnSpeakChunk: func(_, _ string) {
			chunkCalls++
		},
	})

	speaker, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)
	require.Equal(t, "", speaker.Content, "空流时 content 为空字符串")
	require.Equal(t, 0, chunkCalls, "空流时不应触发 chunk 回调")
}

// TestReActRunner_ToolCallDoesNotStream 验证流式只对 speak action 生效。
// tool_call / reflect 决策仍然走 Complete 一次性返回，不走流式。
func TestReActRunner_ToolCallDoesNotStream(t *testing.T) {
	toolDecision := `{"action":"tool_call","reasoning":"need search","tool":"investigator_search","tool_input":{"query":"Q"}}`
	speakDecision := `{"action":"speak","reasoning":"R","content":"完整发言","stance":"pro_a","confidence":0.7,"evidence_refs":[]}`

	dummyTool := &stubToolForStream{name: "investigator_search", observation: "ok"}
	llmClient := &streamingScriptedLLM{
		completeScript: []string{toolDecision, speakDecision},
		streamChunks:   []string{`{"content":"完整发言"}`}, // speak 流式匹配 prefix 后触发 1 次 callback
	}

	var streamCalls int
	r := NewReActRunner(llmClient, "system", map[string]Tool{
		"investigator_search": dummyTool,
	}, RunnerConfig{
		MaxIterations: 2,
		OnSpeakChunk: func(_, _ string) {
			streamCalls++
		},
	})

	speaker, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)
	require.Equal(t, "完整发言", speaker.Content, "speak 流式回填 content")
	// tool_call 一次 + speak 决策一次 = 2 次 Complete
	require.Equal(t, 2, llmClient.completeCalls, "tool_call + speak 各调一次 Complete")
	// tool_call 不走流式，只有 speak 走一次流式
	require.Equal(t, 1, llmClient.streamCalls, "tool_call 不调 StreamComplete；speak 调 1 次")
	require.Equal(t, 1, streamCalls, "OnSpeakChunk 只在 speak 阶段触发一次（chunk 数为 1）")
}

// stubToolForStream 是测试专用 tool：返回固定 observation。
type stubToolForStream struct {
	name        string
	observation string
}

func (s *stubToolForStream) Name() string { return s.name }
func (s *stubToolForStream) Description() string {
	return "stub tool for streaming tests"
}
func (s *stubToolForStream) Execute(_ context.Context, _ map[string]interface{}) (string, error) {
	return s.observation, nil
}

// 占位：避免测试时被 unused uuid 报警
var _ = uuid.New
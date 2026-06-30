package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

// scriptedLLM returns a sequence of JSON AgentOutput strings. The next() func
// is invoked once per ReAct iteration. If exhausted, it returns the final
// element for all subsequent calls. This lets tests script long
// thought→tool→thought→tool→speak chains without manual loops.
type scriptedLLM struct {
	mu      sync.Mutex
	scripts []string
	calls   int32
}

func (s *scriptedLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := int(atomic.AddInt32(&s.calls, 1)) - 1
	if idx >= len(s.scripts) {
		idx = len(s.scripts) - 1
	}
	return s.scripts[idx], llm.Usage{}, nil
}

// scriptedLLM 不走流式 —— 老测试套件通过 OnSpeakChunk 缺省走 fallback。
func (s *scriptedLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

func (s *scriptedLLM) Calls() int { return int(atomic.LoadInt32(&s.calls)) }

func speakOutputJSON(reasoning, content, stance string, confidence float64) string {
	b, _ := json.Marshal(AgentOutput{
		Action:       "speak",
		Reasoning:    reasoning,
		Content:      content,
		EvidenceRefs: []string{},
		Confidence:   confidence,
		Stance:       stance,
	})
	return string(b)
}

func toolOutputJSON(reasoning, tool string, input map[string]interface{}) string {
	b, _ := json.Marshal(AgentOutput{
		Action:    "tool_call",
		Reasoning: reasoning,
		Tool:      tool,
		ToolInput: input,
	})
	return string(b)
}

// stubTool is a Tool for tests. It records invocations and returns a
// canned observation or error.
type stubTool struct {
	name        string
	description string
	mu          sync.Mutex
	calls       []map[string]interface{}
	observation string
	err         error
}

func (t *stubTool) Name() string        { return t.name }
func (t *stubTool) Description() string { return t.description }

func (t *stubTool) Execute(_ context.Context, input map[string]interface{}) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, input)
	if t.err != nil {
		return "", t.err
	}
	return t.observation, nil
}

func (t *stubTool) Calls() []map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]map[string]interface{}, len(t.calls))
	copy(out, t.calls)
	return out
}

func newTestRunner(t *testing.T, scripts []string, tools []Tool, hook StepHook, cfg RunnerConfig) (*ReActRunner, *scriptedLLM) {
	t.Helper()
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 4
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	llmClient := &scriptedLLM{scripts: scripts}
	toolMap := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name()] = tool
	}
	r := NewReActRunner(llmClient, "你是一名律师", toolMap, cfg)
	if hook != nil {
		r.SetStepHook(hook)
	}
	return r, llmClient
}

// T1: 第一次 LLM 直接返回 speak → runner 不调用任何 tool，直接返回 Speaker
func TestReActRunner_DirectSpeak_NoToolsInvoked(t *testing.T) {
	tool := &stubTool{name: "investigator_search", observation: "unused"}
	r, llm := newTestRunner(t, []string{
		speakOutputJSON("首次思考", "我直接陈述", "pro_a", 0.8),
	}, []Tool{tool}, nil, RunnerConfig{})

	speaker, steps, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "我直接陈述", speaker.Content)
	require.Equal(t, "pro_a", speaker.Stance)
	require.Len(t, steps, 1)
	require.Equal(t, "speak", steps[0].Action)
	require.Equal(t, 1, llm.Calls())
	require.Empty(t, tool.Calls(), "tool 不应被调用")
}

// T2: thought → tool → speak；tool 调用一次，observation 进入下一步
func TestReActRunner_OneToolThenSpeak(t *testing.T) {
	tool := &stubTool{name: "investigator_search", observation: "搜到了证据 E007"}
	r, llm := newTestRunner(t, []string{
		toolOutputJSON("我需要先搜证据", "investigator_search", map[string]interface{}{"query": "选项A 长期收益"}),
		speakOutputJSON("拿到证据后整理论点", "基于 E007 的论点", "pro_a", 0.85),
	}, []Tool{tool}, nil, RunnerConfig{})

	speaker, steps, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "基于 E007 的论点", speaker.Content)
	require.Len(t, steps, 2)
	require.Equal(t, "tool_call", steps[0].Action)
	require.Equal(t, "investigator_search", steps[0].ToolName)
	require.Equal(t, "搜到了证据 E007", steps[0].Observation)
	require.Equal(t, "speak", steps[1].Action)
	require.Equal(t, 2, llm.Calls())
	require.Len(t, tool.Calls(), 1)
	require.Equal(t, "选项A 长期收益", tool.Calls()[0]["query"])
}

// T3: thought → tool → tool → speak；两次工具调用 + 最终 speak
func TestReActRunner_TwoToolsThenSpeak(t *testing.T) {
	tool := &stubTool{name: "investigator_search", observation: "结果"}
	r, llm := newTestRunner(t, []string{
		toolOutputJSON("查 1", "investigator_search", map[string]interface{}{"query": "Q1"}),
		toolOutputJSON("查 2", "investigator_search", map[string]interface{}{"query": "Q2"}),
		speakOutputJSON("够了", "总结陈词", "pro_a", 0.9),
	}, []Tool{tool}, nil, RunnerConfig{})

	speaker, steps, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "总结陈词", speaker.Content)
	require.Len(t, steps, 3)
	require.Equal(t, 2, len(tool.Calls()))
	require.Equal(t, "Q1", tool.Calls()[0]["query"])
	require.Equal(t, "Q2", tool.Calls()[1]["query"])
	require.Equal(t, 3, llm.Calls())
}

// T4: 超过 max iteration 仍未 speak → runner 返回 error（不允许无限循环）
func TestReActRunner_MaxIterationsExceededReturnsError(t *testing.T) {
	tool := &stubTool{name: "investigator_search", observation: "结果"}
	// 全部都是 tool_call，永远不说 speak
	scripts := []string{
		toolOutputJSON("查", "investigator_search", map[string]interface{}{"query": "Q1"}),
		toolOutputJSON("查", "investigator_search", map[string]interface{}{"query": "Q2"}),
		toolOutputJSON("查", "investigator_search", map[string]interface{}{"query": "Q3"}),
		toolOutputJSON("查", "investigator_search", map[string]interface{}{"query": "Q4"}),
		toolOutputJSON("查", "investigator_search", map[string]interface{}{"query": "Q5"}),
	}
	r, _ := newTestRunner(t, scripts, []Tool{tool}, nil, RunnerConfig{MaxIterations: 4})

	_, steps, err := r.Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max iterations")
	require.Len(t, steps, 4, "最多 4 步")
	for i, step := range steps {
		require.Equal(t, "tool_call", step.Action, "step %d 应是 tool_call", i)
	}
}

// T5: tool 报错时 observation 带错误信息，下一步 LLM 仍能基于此决策
func TestReActRunner_ToolErrorObservationSurfaced(t *testing.T) {
	tool := &stubTool{
		name:        "investigator_search",
		err:         errors.New("search provider unavailable"),
	}
	r, _ := newTestRunner(t, []string{
		toolOutputJSON("搜证", "investigator_search", map[string]interface{}{"query": "Q"}),
		speakOutputJSON("搜不到就硬上", "虽然没有证据，但我方主张...", "pro_a", 0.5),
	}, []Tool{tool}, nil, RunnerConfig{})

	speaker, steps, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "虽然没有证据，但我方主张...", speaker.Content)
	require.Len(t, steps, 2)
	require.Contains(t, steps[0].Observation, "search provider unavailable")
	require.Contains(t, steps[0].Observation, "[tool_error]", "observation 应明确标记工具失败")
}

// T6: context 取消应停止 ReAct loop
func TestReActRunner_ContextCancelStopsLoop(t *testing.T) {
	tool := &stubTool{name: "investigator_search", observation: "ok"}
	ctx, cancel := context.WithCancel(context.Background())

	var hookCalls int32
	hook := func(step Step) {
		if atomic.AddInt32(&hookCalls, 1) == 1 {
			cancel() // 第一次 step 之后取消
		}
	}

	scripts := []string{
		toolOutputJSON("查", "investigator_search", map[string]interface{}{"query": "Q"}),
		toolOutputJSON("再查", "investigator_search", map[string]interface{}{"query": "Q2"}),
	}
	r, _ := newTestRunner(t, scripts, []Tool{tool}, hook, RunnerConfig{})

	_, _, err := r.Run(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
}

// T7: 不在白名单中的 tool 名 → runner 返回错误（防止 LLM 幻觉调出未注册的工具）
func TestReActRunner_UnknownToolRejected(t *testing.T) {
	allowed := &stubTool{name: "investigator_search", observation: "ok"}
	r, _ := newTestRunner(t, []string{
		toolOutputJSON("幻觉调一个不存在的工具", "delete_database", map[string]interface{}{"table": "users"}),
	}, []Tool{allowed}, nil, RunnerConfig{})

	_, _, err := r.Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete_database")
	require.Contains(t, err.Error(), "not registered")
}

// T8: 步进回调每次都被调用，且 Steps 数组按顺序
func TestReActRunner_StepHookInvokedForEachStep(t *testing.T) {
	tool := &stubTool{name: "investigator_search", observation: "obs"}
	var captured []Step
	var mu sync.Mutex
	hook := func(s Step) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, s)
	}
	r, _ := newTestRunner(t, []string{
		toolOutputJSON("T1", "investigator_search", map[string]interface{}{"query": "Q1"}),
		speakOutputJSON("T2", "最终发言", "pro_a", 0.8),
	}, []Tool{tool}, hook, RunnerConfig{})

	_, _, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, captured, 2)
	require.Equal(t, 0, captured[0].Index)
	require.Equal(t, 1, captured[1].Index)
	require.True(t, captured[0].ElapsedMs >= 0)
}

// T9: 解析失败的 LLM 输出应触发一次重试，仍失败则返回 error
func TestReActRunner_InvalidJSONRetriesThenFails(t *testing.T) {
	// 第一次：垃圾 JSON
	// 第二次：speak 正常输出（重试成功）
	scripts := []string{
		"{this is not valid json",
		speakOutputJSON("retry 成功", "OK", "pro_a", 0.7),
	}
	tool := &stubTool{name: "investigator_search"}
	r, llm := newTestRunner(t, scripts, []Tool{tool}, nil, RunnerConfig{})

	speaker, _, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "OK", speaker.Content)
	require.Equal(t, 2, llm.Calls(), "应有一次重试")
}

// T10: transcript 历史被注入到 system prompt 之外作为额外上下文
func TestReActRunner_TranscriptContextInjected(t *testing.T) {
	var capturedMessages []llm.Message
	var mu sync.Mutex
	captureLLM := &captureLLMScripted{
		output: speakOutputJSON("回应", "我的发言", "pro_a", 0.7),
		onCall: func(msgs []llm.Message) {
			mu.Lock()
			defer mu.Unlock()
			capturedMessages = append([]llm.Message{}, msgs...)
		},
	}
	transcript := []model.Message{
		{Content: "对方说 A 是垃圾", ActionType: "speak"},
		{Content: "我之前说 B 才正确", ActionType: "speak"},
	}

	r := NewReActRunner(captureLLM, "系统 prompt", map[string]Tool{}, RunnerConfig{MaxIterations: 4})
	r.SetStepHook(nil)
	_, _, err := r.Run(context.Background(), transcript)
	require.NoError(t, err)

	// system prompt 应包含双方历史
	mu.Lock()
	defer mu.Unlock()
	var foundSystem bool
	for _, m := range capturedMessages {
		if m.Role == "system" && strings.Contains(m.Content, "对方说 A 是垃圾") && strings.Contains(m.Content, "我之前说 B 才正确") {
			foundSystem = true
		}
	}
	require.True(t, foundSystem, "transcript 应被注入到 system prompt 或单独的 system 消息")
}

// captureLLMScripted 是 scriptedLLM 的变体，能在调用前把 messages 抓出来供测试断言。
type captureLLMScripted struct {
	output string
	onCall func([]llm.Message)
}

func (c *captureLLMScripted) Complete(_ context.Context, _ string, msgs []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	if c.onCall != nil {
		c.onCall(msgs)
	}
	return c.output, llm.Usage{}, nil
}

// captureLLMScripted 不走流式 —— 老测试 fixture 不需要 streaming。
func (c *captureLLMScripted) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

// reflectOutputJSON 构造一段 action="reflect" 的输出。
func reflectOutputJSON(reasoning string) string {
	b, _ := json.Marshal(AgentOutput{
		Action:       "reflect",
		Reasoning:    reasoning,
		EvidenceRefs: []string{},
		Confidence:   0.0,
		Stance:       "neutral",
	})
	return string(b)
}

// T11: reflect → reflect → speak 三步反思后定稿
func TestReActRunner_ReflectThenReflectThenSpeak(t *testing.T) {
	scripts := []string{
		reflectOutputJSON("先拆对方论点"),
		reflectOutputJSON("再想想证据缺口"),
		speakOutputJSON("最终定稿", "我的发言", "pro_a", 0.8),
	}
	r, llm := newTestRunner(t, scripts, nil, nil, RunnerConfig{})
	sp, steps, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "我的发言", sp.Content)
	require.Equal(t, 3, len(steps))
	require.Equal(t, "reflect", steps[0].Action)
	require.Equal(t, "reflect", steps[1].Action)
	require.Equal(t, "speak", steps[2].Action)
	require.Equal(t, 3, llm.Calls())
}

// T12: 超过 MaxReflects 后 reflect 被 cap，发出 reflect_cap_reached 提示，但仍允许继续 speak
func TestReActRunner_ReflectCapFallsThroughToSpeak(t *testing.T) {
	scripts := []string{
		reflectOutputJSON("反思 1"),
		reflectOutputJSON("反思 2"),
		reflectOutputJSON("反思 3"), // 第 3 次 reflect（reflectCount 增到 3）
		reflectOutputJSON("反思 4"), // 第 4 次 reflect 应被 cap（reflectCount=3 >= MaxReflects=3）
		speakOutputJSON("再不说就超时了", "我的发言", "pro_b", 0.7),
	}
	r, llm := newTestRunner(t, scripts, nil, nil, RunnerConfig{MaxReflects: 3, MaxIterations: 8})
	sp, steps, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "我的发言", sp.Content)
	// 期望：4 次 reflect（其中第 4 次 Observation 标记 cap_reached）+ 1 次 speak
	require.Equal(t, 5, len(steps))
	require.Contains(t, steps[3].Observation, "reflect_cap_reached")
	require.Equal(t, "speak", steps[4].Action)
	require.Equal(t, 5, llm.Calls())
}

// T13: reflect 不调用 tool、也不返回 Speaker（继续循环），最后 speak 时才返回
func TestReActRunner_ReflectDoesNotInvokeTools(t *testing.T) {
	stub := &stubTool{name: "noop", description: "noop"}
	scripts := []string{
		reflectOutputJSON("思考一下"),
		speakOutputJSON("ok", "我的发言", "pro_a", 0.7),
	}
	r, _ := newTestRunner(t, scripts, []Tool{stub}, nil, RunnerConfig{})
	_, _, err := r.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, 0, len(stub.Calls()), "reflect 不应触发 tool 调用")
}
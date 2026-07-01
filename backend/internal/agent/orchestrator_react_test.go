package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent/tools"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// reactScriptedLLM extends scriptedLLM but is defined separately so the
// ReAct tests can use a distinct scripts sequence that exercises tool
// dispatch without colliding with the basic runner tests.
type reactScriptedLLM struct {
	mu      sync.Mutex
	scripts []string
	calls   int32
}

func (r *reactScriptedLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := int(atomic.AddInt32(&r.calls, 1)) - 1
	if idx >= len(r.scripts) {
		idx = len(r.scripts) - 1
	}
	return r.scripts[idx], llm.Usage{}, nil
}

// reactScriptedLLM 不走流式 —— 老测试 fixture 不需要 streaming。
func (r *reactScriptedLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

// reactDispatch records the calls ProsecutorSpeakWithReAct makes through
// the tools.DispatchFn closure so tests can assert the LLM-driven search
// reached the courtroom service. The signature mirrors the post-ux-refinement
// DispatchFn contract: returns (findingID, summary, error).
type reactDispatch struct {
	mu       sync.Mutex
	calls    int32
	lastQ    string
	lastSess string
	lastSelf string
	fid      string
	summary  string
	err      error
}

func (r *reactDispatch) Dispatch(_ context.Context, sessionUUID, dispatcher, query string) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	atomic.AddInt32(&r.calls, 1)
	r.lastQ = query
	r.lastSess = sessionUUID
	r.lastSelf = dispatcher
	if r.err != nil {
		return "", "", r.err
	}
	if r.fid == "" {
		r.fid = "finding-" + dispatcher + "-" + query
	}
	return r.fid, r.summary, nil
}

// captureHook collects every Step emitted by the runner.
type captureHook struct {
	mu    sync.Mutex
	steps []Step
}

func (c *captureHook) Hook(s Step) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, s)
}

func (c *captureHook) Steps() []Step {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Step, len(c.steps))
	copy(out, c.steps)
	return out
}

// T1: 整个 ReAct 流程——LLM 决定 search → 拿到 evidence → 最终 speak
func TestProsecutorSpeakWithReAct_DispatchesSearchThenSpeaks(t *testing.T) {
	llmClient := &reactScriptedLLM{scripts: []string{
		toolOutputJSON("需要搜证据", "investigator_search", map[string]interface{}{"query": "选项A 长期收益 数据"}),
		speakOutputJSON("搜完了，整理发言", "基于证据的论点", "pro_a", 0.85),
	}}
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	orch := NewOrchestratorLegacy(llmClient, bus, memRepo)

	dispatch := &reactDispatch{fid: "finding-fake-1", summary: "[1] A; [2] B"}
	hook := &captureHook{}

	ag := model.Agent{
		ID:        uuid.New(),
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-react-1",
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 2,
	}

	speaker, _, err := orch.ProsecutorSpeakWithReAct(
		context.Background(),
		ag, session, []model.Evidence{}, nil,
		dispatch.Dispatch, hook.Hook, nil,
	)
	require.NoError(t, err)
	require.Equal(t, "基于证据的论点", speaker.Content)
	require.Equal(t, "pro_a", speaker.Stance)

	// LLM 被调用 2 次（1 tool_call + 1 speak）
	require.Equal(t, int32(2), atomic.LoadInt32(&llmClient.calls))

	// 调查员被派遣 1 次，参数绑定正确（dispatcher=self, session 来自绑定）
	require.Equal(t, int32(1), atomic.LoadInt32(&dispatch.calls))
	require.Equal(t, "选项A 长期收益 数据", dispatch.lastQ)
	require.Equal(t, "sess-react-1", dispatch.lastSess)
	require.Equal(t, string(model.AgentProsecutor), dispatch.lastSelf)

	// step hook 收到 2 步
	captured := hook.Steps()
	require.Len(t, captured, 2)
	require.Equal(t, "tool_call", captured[0].Action)
	require.Equal(t, "investigator_search", captured[0].ToolName)
	require.Contains(t, captured[0].Observation, "finding-fake-1", "observation 应含 finding_id")
	require.Contains(t, captured[0].Observation, "调查发现", "observation 应标记为调查发现而非证据")
	require.Equal(t, "speak", captured[1].Action)

	// 最终发言应该产生 1 条 A2A 公共消息（与单次调用等价）
	rows, err := a2aRepo.ListVisibleTo(context.Background(), session.ID, a2a.AddressOrchestrator)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1, "至少应有 1 条 A2A speech 消息")
	var speechCount int
	for _, r := range rows {
		if r.MessageType == string(a2a.MessageTypeSpeech) {
			speechCount++
		}
	}
	require.Equal(t, 1, speechCount)
}

// T2: 单次 speak 不调工具时，整套 side effects 应与原 ProsecutorSpeak 等价
func TestProsecutorSpeakWithReAct_DirectSpeakProducesSameSideEffects(t *testing.T) {
	llmClient := &reactScriptedLLM{scripts: []string{
		speakOutputJSON("思考", "直接陈述", "pro_a", 0.8),
	}}
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	orch := NewOrchestratorLegacy(llmClient, bus, memRepo)
	ag := model.Agent{ID: uuid.New(), AgentType: model.AgentProsecutor, BeliefA: 0.75}
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-direct", OptionA: "A", OptionB: "B"}

	dispatch := &reactDispatch{} // 不应被调用
	hook := &captureHook{}

	speaker, steps, err := orch.ProsecutorSpeakWithReAct(
		context.Background(), ag, session, nil, nil,
		dispatch.Dispatch, hook.Hook, nil,
	)
	require.NoError(t, err)
	require.Equal(t, "直接陈述", speaker.Content)
	require.Equal(t, int32(0), atomic.LoadInt32(&dispatch.calls), "未触发工具")
	require.Len(t, steps, 1)
	require.Equal(t, "speak", steps[0].Action)
}

// T3: dispatchFn 为 nil 时应优雅退化为「无工具」单步 speak
func TestProsecutorSpeakWithReAct_NilDispatchRunsWithoutTool(t *testing.T) {
	llmClient := &reactScriptedLLM{scripts: []string{
		speakOutputJSON("思考", "直接发言", "pro_a", 0.8),
	}}
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	orch := NewOrchestratorLegacy(llmClient, bus, memRepo)
	ag := model.Agent{ID: uuid.New(), AgentType: model.AgentDefender, BeliefA: 0.25, BeliefB: 0.75}
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-nil", OptionA: "A", OptionB: "B"}

	speaker, _, err := orch.DefenderSpeakWithReAct(
		context.Background(), ag, session, nil, nil,
		nil, nil, nil, // dispatchFn=nil, hook=nil, chunkCb=nil 都允许
	)
	require.NoError(t, err)
	require.Equal(t, "直接发言", speaker.Content)
}

// T4: dispatchFn 报错时 ReAct 应记录错误 observation 并允许 LLM 继续决策
func TestProsecutorSpeakWithReAct_DispatchErrorDoesNotAbort(t *testing.T) {
	llmClient := &reactScriptedLLM{scripts: []string{
		toolOutputJSON("搜", "investigator_search", map[string]interface{}{"query": "Q"}),
		speakOutputJSON("搜不到就硬上", "虽然没有新证据...", "pro_a", 0.5),
	}}
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	orch := NewOrchestratorLegacy(llmClient, bus, memRepo)
	dispatch := &reactDispatch{err: assertError("search provider unavailable")}
	hook := &captureHook{}

	ag := model.Agent{ID: uuid.New(), AgentType: model.AgentProsecutor, BeliefA: 0.7}
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-err", OptionA: "A", OptionB: "B"}

	speaker, steps, err := orch.ProsecutorSpeakWithReAct(
		context.Background(), ag, session, nil, nil,
		dispatch.Dispatch, hook.Hook, nil,
	)
	require.NoError(t, err)
	require.Equal(t, "虽然没有新证据...", speaker.Content)
	require.Len(t, steps, 2)
	require.Contains(t, steps[0].Observation, "[tool_error]")
	require.Contains(t, steps[0].Observation, "search provider unavailable")
}

// T5: 私有策略笔记应在最终发言时写入，与单次调用一致
func TestProsecutorSpeakWithReAct_WritesPrivateStrategyNoteAfterSpeak(t *testing.T) {
	llmClient := &reactScriptedLLM{scripts: []string{
		speakOutputJSON("思考", "论点", "pro_a", 0.8),
	}}
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2a.NewInMemoryRepository(nil), nil)
	orch := NewOrchestratorLegacy(llmClient, bus, memRepo)

	agID := uuid.New()
	ag := model.Agent{ID: agID, AgentType: model.AgentDefender, BeliefA: 0.25}
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "s", OptionA: "A", OptionB: "B"}

	_, _, err := orch.DefenderSpeakWithReAct(
		context.Background(), ag, session, nil, nil,
		nil, nil, nil,
	)
	require.NoError(t, err)

	rows, err := memRepo.List(context.Background(), session.ID, agID, "agent:"+agID.String())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 1)
	require.Equal(t, string(private_memory.TypeStrategyNote), rows[0].Type)
}

// assertError is a tiny helper to build a typed error without importing
// errors in every test signature.
type assertError string

func (e assertError) Error() string { return string(e) }

// T6: 验证 runner 在 ReAct 模式下仍遵守 30s timeout（注入极短超时让它立即超时）
// 这是一个 integration-flavored 校验，跑通证明 timeout 配置生效。
func TestProsecutorSpeakWithReAct_RespectsTimeout(t *testing.T) {
	llmClient := &reactScriptedLLM{scripts: []string{
		toolOutputJSON("永远查", "investigator_search", map[string]interface{}{"query": "Q"}),
		toolOutputJSON("永远查 2", "investigator_search", map[string]interface{}{"query": "Q2"}),
		toolOutputJSON("永远查 3", "investigator_search", map[string]interface{}{"query": "Q3"}),
		toolOutputJSON("永远查 4", "investigator_search", map[string]interface{}{"query": "Q4"}),
		toolOutputJSON("永远查 5", "investigator_search", map[string]interface{}{"query": "Q5"}),
	}}
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	_ = NewOrchestratorLegacy(llmClient, bus, memRepo)
	dispatch := &reactDispatch{fid: "finding-fake-1"}
	hook := &captureHook{}

	ag := model.Agent{ID: uuid.New(), AgentType: model.AgentProsecutor, BeliefA: 0.7}
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "s", OptionA: "A", OptionB: "B"}

	// 直接调内部 runner，把 timeout 调到 1ms 强制超时
	tool := tools.NewInvestigatorSearchTool(session.SessionUUID, string(ag.AgentType), dispatch.Dispatch)
	runner := NewReActRunner(llmClient, "sys", map[string]Tool{tool.Name(): tool}, RunnerConfig{
		MaxIterations: 4,
		Timeout:       1 * time.Millisecond,
	})
	runner.SetStepHook(hook.Hook)

	_, _, err := runner.Run(context.Background(), nil)
	require.Error(t, err)
}

// T7: serializing AgentOutput that includes Tool / ToolInput round-trips
// (defensive: the LLM may emit tool_call fields even when action=speak).
func TestAgentOutput_ToolCallFieldsRoundTrip(t *testing.T) {
	src := AgentOutput{
		Action:    ActionToolCall,
		Reasoning: "需要先查",
		Tool:      "investigator_search",
		ToolInput: map[string]interface{}{"query": "Q"},
	}
	b, err := json.Marshal(src)
	require.NoError(t, err)
	var out AgentOutput
	require.NoError(t, json.Unmarshal(b, &out))
	out.NormalizeAction()
	require.Equal(t, ActionToolCall, out.Action)
	require.Equal(t, "investigator_search", out.Tool)
	require.Equal(t, "Q", out.ToolInput["query"])
}
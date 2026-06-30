package courtroom

import (
	"context"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/agent/tools"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// reactScriptedLLM / speakJSON / toolJSON / stubSearcher 已迁移到
// fakes_test.go，本文件仅保留业务断言。

// TestServiceSpeakWithReAct_FullFlow wires Service.speakWithReAct end to
// end with in-memory repositories and a scripted LLM, then asserts:
//
//  1. The ReAct loop walks thought → tool_call → speak
//  2. broadcastAgentCotStep is invoked once per step with the expected
//     payload shape
//  3. A2A persists a single public speech message
//  4. Private memory writes one strategy_note for the lawyer
//
// Note: speakWithReAct itself does NOT broadcast agent.speak — that
// remains the caller's responsibility (see Service.broadcastAgentSpeak),
// so we assert the orchestrator-side side effects instead.
func TestServiceSpeakWithReAct_FullFlow(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	llmClient := &reactScriptedLLM{scripts: []string{
		toolJSON(tools.InvestigatorSearchToolName, map[string]interface{}{"query": "选项A 长期收益 数据"}),
		speakJSON("基于搜索证据的论点", "pro_a", 0.85),
	}}

	orch := agent.NewOrchestrator(llmClient, bus, memRepo)
	searcher := &stubSearcher{results: nil} // 0 results keeps DB-less path clean
	evidenceSvc := evidence.NewService(nil, llmClient)

	svc := &Service{
		db:           nil, // dispatchFnFor will short-circuit; that's fine
		stateMachine: NewStateMachine(),
		orchestrator: orch,
		evidenceSvc:  evidenceSvc,
		beliefEngine: nil,
		searcher:     searcher,
		a2aBus:       bus,
		broadcaster:  func(string, Event) {}, // updated below
		activeCalls:  map[string]context.CancelFunc{},
		sessionLocks: map[string]*sync.Mutex{},
	}

	var mu sync.Mutex
	var cotSteps []Event
	svc.broadcaster = func(_ string, e Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Type == "agent.cot_step" {
			cotSteps = append(cotSteps, e)
		}
	}

	agentID := uuid.New()
	ag := model.Agent{
		ID:        agentID,
		AgentUUID: "agent_prosecutor_1",
		AgentType: model.AgentProsecutor,
		Name:      "选项A代表",
		BeliefA:   0.75,
		BeliefB:   0.25,
	}
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-react-integ-1",
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
	}

	speaker, err := svc.speakWithReAct(context.Background(), ag, session, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "基于搜索证据的论点", speaker.Content)

	mu.Lock()
	defer mu.Unlock()

	// 1) cot_step 应被广播两次：一次 tool_call，一次 speak 收尾
	require.Len(t, cotSteps, 2, "cot_step 应该和 LLM 调用次数相同")

	// 2) cot_step payload 结构
	first := cotSteps[0].Payload
	require.Equal(t, "agent_prosecutor_1", first["agent_id"])
	require.Equal(t, model.AgentProsecutor, first["agent_type"])
	step, ok := first["step"].(agent.Step)
	require.True(t, ok, "step 应为 agent.Step, got %T", first["step"])
	require.Equal(t, "tool_call", step.Action)
	require.Equal(t, tools.InvestigatorSearchToolName, step.ToolName)

	// 3) LLM 被调用了两次
	require.Equal(t, 2, llmClient.calls)

	// 4) A2A 持久化：orchestrator 只在最终发言时写一条 public 消息
	rows, err := a2aRepo.ListVisibleTo(context.Background(), session.ID, a2a.AddressOrchestrator)
	require.NoError(t, err)
	var speech int
	for _, r := range rows {
		if r.MessageType == string(a2a.MessageTypeSpeech) {
			speech++
		}
	}
	require.Equal(t, 1, speech)

	// 5) Private memory 写入了一条 strategy_note
	notes, err := memRepo.List(context.Background(), session.ID, agentID, "agent:"+agentID.String())
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, string(private_memory.TypeStrategyNote), notes[0].Type)
}

// TestServiceSpeakWithReAct_NilDispatchNeverCallsInvestigator verifies that
// when the dispatchFn path is unavailable (nil DB), the runner still
// completes via direct speak without panicking.
func TestServiceSpeakWithReAct_NilDispatchNeverCallsInvestigator(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	llmClient := &reactScriptedLLM{scripts: []string{
		speakJSON("直接陈述", "pro_a", 0.7),
	}}
	orch := agent.NewOrchestrator(llmClient, bus, memRepo)

	svc := &Service{
		db:           nil,
		stateMachine: NewStateMachine(),
		orchestrator: orch,
		evidenceSvc:  nil,
		searcher:     nil,
		a2aBus:       bus,
		broadcaster:  func(string, Event) {},
		activeCalls:  map[string]context.CancelFunc{},
		sessionLocks: map[string]*sync.Mutex{},
	}

	ag := model.Agent{
		ID:        uuid.New(),
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-nil-dispatch",
		CurrentPhase: model.PhaseOpening,
		CurrentRound: 0,
	}

	speaker, err := svc.speakWithReAct(context.Background(), ag, session, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "直接陈述", speaker.Content)
	require.Equal(t, 1, llmClient.calls)
}
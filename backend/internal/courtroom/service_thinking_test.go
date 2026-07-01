package courtroom

import (
	"context"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/agent/tools"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// T_thinkingStartedBeforeCotStep: speakWithReAct 必须在第一个 cot_step 之
// 前推送 agent.thinking_started，让前端能立刻渲染云朵，避免死亡空窗。
func TestServiceSpeakWithReAct_BroadcastsThinkingStartedBeforeCotStep(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	llmClient := &reactScriptedLLM{scripts: []string{
		toolJSON(tools.InvestigatorSearchToolName, map[string]interface{}{"query": "x"}),
		speakJSON("final", "pro_a", 0.7),
	}}
	orch := agent.NewOrchestratorLegacy(llmClient, bus, memRepo)

	searcher := &stubSearcher{results: nil}
	invRepo := investigation.NewInMemoryRepository(nil)
	invSvc := investigation.NewService(invRepo, bus, searcher)
	evidenceSvc := evidence.NewService(nil, llmClient)

	svc := &Service{
		db:               nil,
		stateMachine:     NewStateMachine(),
		orchestrator:     orch,
		evidenceSvc:      evidenceSvc,
		investigationSvc: invSvc,
		beliefEngine:     nil,
		searcher:         searcher,
		a2aBus:           bus,
		broadcaster:      func(string, Event) {}, // 下面覆盖
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}

	var mu sync.Mutex
	var events []Event
	svc.broadcaster = func(_ string, e Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	ag := model.Agent{
		ID:        uuid.New(),
		AgentUUID: "agent_prosecutor_1",
		AgentType: model.AgentProsecutor,
		Name:      "选项A代表",
		BeliefA:   0.75,
		BeliefB:   0.25,
	}
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-thinking-1",
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
	}

	_, err := svc.speakWithReAct(context.Background(), ag, session, nil, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	// 找到第一个 thinking_started / agent.cot_step 的位置
	var startedIdx, firstCotIdx, finishedIdx int = -1, -1, -1
	for i, e := range events {
		switch e.Type {
		case "agent.thinking_started":
			if startedIdx == -1 {
				startedIdx = i
			}
		case "agent.cot_step":
			if firstCotIdx == -1 {
				firstCotIdx = i
			}
		case "agent.thinking_finished":
			if finishedIdx == -1 {
				finishedIdx = i
			}
		}
	}

	require.NotEqual(t, -1, startedIdx, "必须至少推送一次 agent.thinking_started")
	require.NotEqual(t, -1, firstCotIdx, "必须至少推送一次 agent.cot_step")
	require.NotEqual(t, -1, finishedIdx, "必须至少推送一次 agent.thinking_finished")
	require.Less(t, startedIdx, firstCotIdx,
		"agent.thinking_started 必须在第一个 agent.cot_step 之前 (started=%d cot=%d)",
		startedIdx, firstCotIdx)
	require.Less(t, firstCotIdx, finishedIdx,
		"agent.cot_step 必须全部出现于 thinking_finished 之前")
}

// T_thinkingStartedPayloadShape: 验证事件 payload 的字段结构稳定，前端
// 据此渲染气泡。
func TestServiceSpeakWithReAct_ThinkingStartedPayloadShape(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	llmClient := &reactScriptedLLM{scripts: []string{
		speakJSON("直接说话", "pro_a", 0.8),
	}}
	orch := agent.NewOrchestratorLegacy(llmClient, bus, memRepo)

	svc := &Service{
		db:               nil,
		stateMachine:     NewStateMachine(),
		orchestrator:     orch,
		evidenceSvc:      evidence.NewService(nil, llmClient),
		beliefEngine:     nil,
		searcher:         &stubSearcher{},
		a2aBus:           bus,
		broadcaster:      func(string, Event) {},
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}

	var mu sync.Mutex
	var started Event
	var finished Event
	svc.broadcaster = func(_ string, e Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Type == "agent.thinking_started" && started.Type == "" {
			started = e
		}
		if e.Type == "agent.thinking_finished" && finished.Type == "" {
			finished = e
		}
	}

	ag := model.Agent{
		ID:        uuid.New(),
		AgentUUID: "agent_prosecutor_xyz",
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-shape-1",
		CurrentPhase: model.PhaseOpening,
		CurrentRound: 0,
	}

	_, err := svc.speakWithReAct(context.Background(), ag, session, nil, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "agent.thinking_started", started.Type)
	require.Equal(t, "agent_prosecutor_xyz", started.Payload["agent_id"])
	require.Equal(t, model.AgentProsecutor, started.Payload["agent_type"])

	require.Equal(t, "agent.thinking_finished", finished.Type)
	require.Equal(t, "agent_prosecutor_xyz", finished.Payload["agent_id"])
}

// T_searchStartedBeforeToolCot: search.started/completed 由 DispatchInvestigator
// 内部广播，应该出现在 tool_call step 的 cot_step 事件附近，给前端显示
// 调查员的旋转动画。
func TestServiceSpeakWithReAct_SearchStartedAroundToolCotStep(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	llmClient := &reactScriptedLLM{scripts: []string{
		toolJSON(tools.InvestigatorSearchToolName, map[string]interface{}{"query": "市场行情"}),
		speakJSON("基于搜索的论点", "pro_a", 0.8),
	}}
	orch := agent.NewOrchestratorLegacy(llmClient, bus, memRepo)

	results := []search.Result{{Title: "数据", URL: "u", Content: "c"}}
	searcher := &stubSearcher{results: results}
	invRepo := investigation.NewInMemoryRepository(nil)
	invSvc := investigation.NewService(invRepo, bus, searcher)

	svc := &Service{
		db:               nil,
		stateMachine:     NewStateMachine(),
		orchestrator:     orch,
		evidenceSvc:      evidence.NewService(nil, llmClient),
		investigationSvc: invSvc,
		beliefEngine:     nil,
		searcher:         searcher,
		a2aBus:           bus,
		broadcaster:      func(string, Event) {},
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}

	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-search-events",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
	}
	svc.SessionLoader = func(_ string) (model.CourtSession, error) {
		return session, nil
	}

	var mu sync.Mutex
	var events []Event
	svc.broadcaster = func(_ string, e Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}

	ag := model.Agent{
		ID:        uuid.New(),
		AgentUUID: "agent_prosecutor_search",
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}

	_, err := svc.speakWithReAct(context.Background(), ag, session, nil, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	// 找到 search.started 和 tool_call cot_step 的索引
	var searchStartedIdx, toolCotIdx int = -1, -1
	for i, e := range events {
		if e.Type == "search.started" && searchStartedIdx == -1 {
			searchStartedIdx = i
		}
		if e.Type == "agent.cot_step" {
			if step, ok := e.Payload["step"].(agent.Step); ok && step.Action == "tool_call" {
				if toolCotIdx == -1 {
					toolCotIdx = i
				}
			}
		}
	}

	require.NotEqual(t, -1, searchStartedIdx, "必须推送 search.started")
	require.NotEqual(t, -1, toolCotIdx, "必须推送 tool_call 的 cot_step")
	// search.started 应在 tool_call cot_step 附近（同一次 ReAct iteration）
	require.Less(t, searchStartedIdx, toolCotIdx,
		"search.started (%d) 应在 tool_call cot_step (%d) 之前",
		searchStartedIdx, toolCotIdx)
}
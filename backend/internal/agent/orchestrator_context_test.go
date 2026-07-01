package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// --- buildEpisodicMemoryBlock unit tests --------------------------------

func TestBuildEpisodicMemoryBlock_NoPriorMemory_ReturnsEmpty(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	orch := NewOrchestratorLegacy(&reactScriptedLLM{}, bus, memRepo)

	block := orch.buildEpisodicMemoryBlock(context.Background(), uuid.New(), "prosecutor")
	require.Equal(t, "", block, "no memory should yield empty block, not an empty heading")
}

func TestBuildEpisodicMemoryBlock_NilInputs_ReturnsEmpty(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	orch := NewOrchestratorLegacy(&reactScriptedLLM{}, bus, memRepo)

	// nil/empty inputs to buildEpisodicMemoryBlock must short-circuit
	require.Equal(t, "", orch.buildEpisodicMemoryBlock(context.Background(), uuid.Nil, ""))
	require.Equal(t, "", orch.buildEpisodicMemoryBlock(context.Background(), uuid.New(), ""))
}

// helper: write a single private memory row directly to the bus.
func writePrivateMemo(t *testing.T, bus *a2a.Bus, sessionID uuid.UUID, agent, memType string, round int, content string) {
	t.Helper()
	_, err := bus.Send(context.Background(), a2a.Message{
		SessionID:   sessionID,
		Round:       round,
		Phase:       "cross_exam",
		From:        agent,
		To:          agent,
		MessageType: a2a.MessageType(memoryTypeForBus(memType)),
		Visibility:  a2a.VisibilityPrivate,
		Payload: map[string]interface{}{
			"memory_type": memType,
			"content":     content,
			"round":       round,
		},
	})
	require.NoError(t, err)
}

// memoryTypeForBus maps the 4 private kind strings to their a2a.MessageType
// constant. We do it via a map to keep the helper compact and explicit.
func memoryTypeForBus(kind string) string {
	switch kind {
	case "strategy_note":
		return string(a2a.MessageTypeStrategyNote)
	case "opponent_weakness":
		return string(a2a.MessageTypeOpponentWeakness)
	case "self_correction":
		return string(a2a.MessageTypeSelfCorrection)
	case "evidence_eval":
		return string(a2a.MessageTypeEvidenceEval)
	default:
		return string(a2a.MessageTypeStrategyNote)
	}
}

func TestBuildEpisodicMemoryBlock_FormatsAllRowsChronologically(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	orch := NewOrchestratorLegacy(&reactScriptedLLM{}, bus, memRepo)

	sessionID := uuid.New()
	// Insert out of order to confirm sort
	writePrivateMemo(t, bus, sessionID, "prosecutor", "self_correction", 3, "我之前论证 X 有误")
	writePrivateMemo(t, bus, sessionID, "prosecutor", "strategy_note", 1, "下一轮攻击 E001 数据来源")
	writePrivateMemo(t, bus, sessionID, "prosecutor", "opponent_weakness", 2, "辩方没反驳 E002")

	block := orch.buildEpisodicMemoryBlock(context.Background(), sessionID, "prosecutor")
	require.NotEmpty(t, block)
	require.Contains(t, block, "你之前的策略笔记", "heading must be present")
	require.Contains(t, block, "strategy_note")
	require.Contains(t, block, "opponent_weakness")
	require.Contains(t, block, "self_correction")
	// ordering: round 1 must appear before round 3
	r1 := strings.Index(block, "round=1")
	r3 := strings.Index(block, "round=3")
	require.Greater(t, r3, r1, "round ordering must be ascending")
	// content snippets preserved
	require.Contains(t, block, "下一轮攻击 E001")
	require.Contains(t, block, "辩方没反驳 E002")
	require.Contains(t, block, "我之前论证 X 有误")
}

func TestBuildEpisodicMemoryBlock_OtherAgentsMemoryNotVisible(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	orch := NewOrchestratorLegacy(&reactScriptedLLM{}, bus, memRepo)

	sessionID := uuid.New()
	writePrivateMemo(t, bus, sessionID, "defender", "strategy_note", 1, "DEFENDER SECRET MEMO")
	writePrivateMemo(t, bus, sessionID, "prosecutor", "strategy_note", 1, "PROSECUTOR MEMO")

	block := orch.buildEpisodicMemoryBlock(context.Background(), sessionID, "prosecutor")
	require.Contains(t, block, "PROSECUTOR MEMO")
	require.NotContains(t, block, "DEFENDER SECRET MEMO", "opposing-side private memory must NOT leak")
}

func TestBuildEpisodicMemoryBlock_MalformedPayloadSkipped(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	orch := NewOrchestratorLegacy(&reactScriptedLLM{}, bus, memRepo)

	sessionID := uuid.New()
	// Good row
	writePrivateMemo(t, bus, sessionID, "prosecutor", "strategy_note", 1, "good memory")
	// Manually inject a malformed payload via direct repo insert
	_, err := a2aRepo.Insert(context.Background(), model.A2AMessage{
		ID:          uuid.New(),
		SessionID:   sessionID,
		MessageUUID: uuid.New().String(),
		Round:       2,
		Phase:       "cross_exam",
		FromAgent:   "prosecutor",
		ToAgent:     "prosecutor",
		MessageType: string(a2a.MessageTypeStrategyNote),
		Payload:     "not-valid-json{",
		Visibility:  string(a2a.VisibilityPrivate),
		CreatedAt:   time.Date(2026, 6, 29, 12, 2, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	block := orch.buildEpisodicMemoryBlock(context.Background(), sessionID, "prosecutor")
	require.Contains(t, block, "good memory")
	require.NotContains(t, block, "not-valid-json")
	// still only one bullet row
	require.Equal(t, 1, strings.Count(block, "round="))
}

func TestBuildEpisodicMemoryBlock_LongContentTruncated(t *testing.T) {
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	orch := NewOrchestratorLegacy(&reactScriptedLLM{}, bus, memRepo)

	sessionID := uuid.New()
	longContent := strings.Repeat("这是很长的策略笔记。", 100) // 1000+ chars
	writePrivateMemo(t, bus, sessionID, "prosecutor", "strategy_note", 1, longContent)

	block := orch.buildEpisodicMemoryBlock(context.Background(), sessionID, "prosecutor")
	require.Contains(t, block, "...")
	// truncation should keep total block size bounded
	require.Less(t, len(block), 600, "block must be bounded even with huge content")
}

func TestExtractMemoryPayload_NormalAndMalformed(t *testing.T) {
	good, _ := json.Marshal(map[string]interface{}{
		"memory_type": "strategy_note",
		"content":     "hi",
	})
	c, mt := extractMemoryPayload(string(good))
	require.Equal(t, "hi", c)
	require.Equal(t, "strategy_note", mt)

	require.Equal(t, "", mustExtractContent(t, "not-json"), "malformed payload must return empty content")
	c, mt = extractMemoryPayload("")
	require.Equal(t, "", c)
	require.Equal(t, "", mt)
}

// mustExtractContent is a small wrapper so test assertions can use the
// single-value form (extractMemoryPayload returns two values).
func mustExtractContent(t *testing.T, payload string) string {
	t.Helper()
	c, _ := extractMemoryPayload(payload)
	return c
}

// --- Integration: ProsecutorSpeakWithReAct injects memory into prompt ---

// capturingLLM is a scriptedLLM variant that records every system prompt
// the runner passes in. We use this to assert buildEpisodicMemoryBlock's
// output really reaches the LLM, end-to-end.
type capturingLLM struct {
	mu               sync.Mutex
	scripts          []string
	calls            int32
	systemPrompts    []string
	lastSystemPrompt string
}

func (c *capturingLLM) Complete(_ context.Context, sys string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := int(atomic.AddInt32(&c.calls, 1)) - 1
	if idx >= len(c.scripts) {
		idx = len(c.scripts) - 1
	}
	c.systemPrompts = append(c.systemPrompts, sys)
	c.lastSystemPrompt = sys
	return c.scripts[idx], llm.Usage{}, nil
}

func (c *capturingLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

func (c *capturingLLM) Calls() int { return int(atomic.LoadInt32(&c.calls)) }

func TestProsecutorSpeakWithReAct_PriorMemoryInjectedIntoSystemPrompt(t *testing.T) {
	// Setup: bus + memRepo + orchestrator
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	orch := NewOrchestratorLegacy(&capturingLLM{}, bus, memRepo)

	// Seed 3 private strategy notes into the bus BEFORE the LLM call
	sessionID := uuid.New()
	writePrivateMemo(t, bus, sessionID, "prosecutor", "strategy_note", 1, "第一轮策略：强调 E001 来源")
	writePrivateMemo(t, bus, sessionID, "prosecutor", "opponent_weakness", 2, "对方没反驳 E002 是弱点")
	writePrivateMemo(t, bus, sessionID, "prosecutor", "self_correction", 3, "我之前论证 X 有误")

	// Configure capturing LLM to speak immediately
	llmClient := &capturingLLM{scripts: []string{
		speakOutputJSON("基于历史策略", "继续按 plan 推进", "pro_a", 0.85),
	}}
	// swap the orchestrator's client to our capturing one
	orch.llmClient = llmClient

	// Drive ProsecutorSpeakWithReAct
	ag := model.Agent{
		ID:        uuid.New(),
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}
	session := model.CourtSession{
		ID:           sessionID,
		SessionUUID:  "sess-pr3-1",
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 4,
	}
	speaker, _, err := orch.ProsecutorSpeakWithReAct(
		context.Background(),
		ag, session, []model.Evidence{}, nil,
		nil, nil, nil,
	)
	require.NoError(t, err)
	require.Equal(t, "继续按 plan 推进", speaker.Content)

	// Assert: the system prompt captured by the LLM contains ALL THREE memories
	require.GreaterOrEqual(t, llmClient.Calls(), 1, "LLM should have been called at least once")
	sys := llmClient.lastSystemPrompt
	require.Contains(t, sys, "你之前的策略笔记", "episodic memory heading must be injected")
	require.Contains(t, sys, "第一轮策略：强调 E001 来源")
	require.Contains(t, sys, "对方没反驳 E002 是弱点")
	require.Contains(t, sys, "我之前论证 X 有误")
	// round ordering: round=1 must appear before round=3
	r1 := strings.Index(sys, "round=1")
	r3 := strings.Index(sys, "round=3")
	require.Greater(t, r3, r1, "memory must be chronologically ordered in prompt")
}

func TestDefenderSpeakWithReAct_OwnMemoryInjected_OpponentsExcluded(t *testing.T) {
	// Setup: bus + memRepo + orchestrator
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	orch := NewOrchestratorLegacy(&capturingLLM{}, bus, memRepo)

	sessionID := uuid.New()
	// Defender's own memory (should appear)
	writePrivateMemo(t, bus, sessionID, "defender", "strategy_note", 1, "DEFENDER OWN STRATEGY")
	// Prosecutor's memory (must NOT appear)
	writePrivateMemo(t, bus, sessionID, "prosecutor", "strategy_note", 1, "PROSECUTOR SECRET PLAN")

	llmClient := &capturingLLM{scripts: []string{
		speakOutputJSON("thinking", "the defense speech", "pro_b", 0.7),
	}}
	orch.llmClient = llmClient

	ag := model.Agent{ID: uuid.New(), AgentType: model.AgentDefender, BeliefB: 0.75}
	session := model.CourtSession{
		ID:           sessionID,
		SessionUUID:  "sess-pr3-def",
		OptionA:      "A",
		OptionB:      "B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 2,
	}
	_, _, err := orch.DefenderSpeakWithReAct(
		context.Background(),
		ag, session, []model.Evidence{}, nil,
		nil, nil, nil,
	)
	require.NoError(t, err)
	sys := llmClient.lastSystemPrompt
	require.Contains(t, sys, "DEFENDER OWN STRATEGY")
	require.NotContains(t, sys, "PROSECUTOR SECRET PLAN", "defender must NOT see prosecutor's private memo")
}

func TestProsecutorSpeakWithReAct_NoPriorMemory_BlockAbsent(t *testing.T) {
	// Setup empty bus
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	orch := NewOrchestratorLegacy(&capturingLLM{}, bus, memRepo)

	llmClient := &capturingLLM{scripts: []string{
		speakOutputJSON("first time", "开场陈述", "pro_a", 0.7),
	}}
	orch.llmClient = llmClient

	ag := model.Agent{ID: uuid.New(), AgentType: model.AgentProsecutor, BeliefA: 0.75}
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-empty",
		OptionA:      "A",
		OptionB:      "B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
	}
	_, _, err := orch.ProsecutorSpeakWithReAct(
		context.Background(),
		ag, session, []model.Evidence{}, nil,
		nil, nil, nil,
	)
	require.NoError(t, err)
	require.NotContains(t, llmClient.lastSystemPrompt, "你之前的策略笔记",
		"empty memory must NOT produce an empty heading in prompt")
}

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// stubLLM returns a canned JSON AgentOutput that satisfies parseOutput.
type stubLLM struct {
	output string
}

func (s *stubLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	return s.output, llm.Usage{}, nil
}

// stubLLM 不走流式 —— 老测试套件不需要 streaming 行为。
func (s *stubLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

// validOutputJSON returns JSON shaped like AgentOutput so parseOutput succeeds.
func validOutputJSON() string {
	out := AgentOutput{
		Reasoning:    "对方对 E001 的反驳站不住脚",
		Content:      "选项 A 在长期收益上明显占优",
		EvidenceRefs: []string{"E001"},
		Confidence:   0.8,
		Stance:       "pro_a",
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func newTestOrchestrator(t *testing.T) (*Orchestrator, *a2a.InMemoryRepository, *private_memory.InMemoryRepository) {
	t.Helper()
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	llmClient := &stubLLM{output: validOutputJSON()}
	return NewOrchestratorLegacy(llmClient, bus, memRepo), a2aRepo, memRepo
}

func TestOrchestrator_ProsecutorSpeak_PublishesA2AMessage(t *testing.T) {
	orch, a2aRepo, _ := newTestOrchestrator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	agentID := uuid.New()
	ag := model.Agent{
		ID:        agentID,
		AgentType: model.AgentProsecutor,
		Name:      "选项A代表",
		BeliefA:   0.75,
		BeliefB:   0.25,
	}
	session := model.CourtSession{
		ID:           sessionID,
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 2,
	}

	speaker, err := orch.ProsecutorSpeak(ctx, ag, session, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "选项 A 在长期收益上明显占优", speaker.Content)

	rows, err := a2aRepo.ListVisibleTo(ctx, sessionID, a2a.AddressOrchestrator)
	require.NoError(t, err)
	// v0.5 升级：recordSideEffects 现在写两条 A2A 消息 —— 1 条 public
	// speech（用户可见发言）+ 1 条 private strategy_note（情景记忆）。
	// 这是 v0.5 plan 决策 #1：私有记忆走 A2A 通道，前端 MemoryAuditPanel
	// hydrate 成 MemoryEntry。
	require.Len(t, rows, 2, "expected 1 public speech + 1 private strategy_note")

	// 排序保证 public 在前
	publicIdx, privateIdx := -1, -1
	for i, row := range rows {
		switch row.Visibility {
		case string(a2a.VisibilityPublic):
			publicIdx = i
		case string(a2a.VisibilityPrivate):
			privateIdx = i
		}
	}
	require.NotEqual(t, -1, publicIdx, "public speech row must exist")
	require.NotEqual(t, -1, privateIdx, "private strategy_note row must exist")

	// 1) Public speech
	publicRow := rows[publicIdx]
	require.Equal(t, string(a2a.MessageTypeSpeech), publicRow.MessageType)
	require.Equal(t, "prosecutor", publicRow.FromAgent)
	require.Equal(t, "defender", publicRow.ToAgent)
	require.Equal(t, string(a2a.VisibilityPublic), publicRow.Visibility)
	require.Equal(t, 2, publicRow.Round)
	payload, err := a2a.DecodePayload(publicRow)
	require.NoError(t, err)
	require.Equal(t, speaker.Content, payload["content"])
	require.Equal(t, speaker.Reasoning, payload["reasoning"])

	// 2) Private strategy_note（MemoryAuditPanel 渲染的唯一来源）
	privateRow := rows[privateIdx]
	require.Equal(t, string(a2a.MessageTypeStrategyNote), privateRow.MessageType,
		"private message MUST be a strategy_note (not strategy_note + something else)")
	require.Equal(t, "prosecutor", privateRow.FromAgent,
		"private memory must come from self")
	require.Equal(t, "prosecutor", privateRow.ToAgent,
		"private memory is self→self for episodic memory")
	require.Equal(t, string(a2a.VisibilityPrivate), privateRow.Visibility,
		"this is the private channel — orchestrator+self can see it; defender cannot")
	privatePayload, err := a2a.DecodePayload(privateRow)
	require.NoError(t, err)
	require.Equal(t, "strategy_note", privatePayload["memory_type"])
	require.NotEmpty(t, privatePayload["content"])
	// linked_evidence_ids 应该是空数组（speaker.EvidenceRefs 是 nil），
	// 不允许是 nil —— 这是 MemoryEntry 在前端 hydrate 的安全契约。
	require.NotNil(t, privatePayload["linked_evidence_ids"])

	// v0.5 防御性断言：defender 不能读这条 private strategy_note
	defenderRows, err := a2aRepo.ListVisibleTo(ctx, sessionID, "defender")
	require.NoError(t, err)
	require.Len(t, defenderRows, 1, "defender must only see the public speech, not the private strategy_note")
	require.Equal(t, string(a2a.VisibilityPublic), defenderRows[0].Visibility,
		"defender visibility filter must hide private strategy_note from opponent")
}

func TestOrchestrator_ProsecutorSpeak_WritesPrivateStrategyNote(t *testing.T) {
	orch, _, memRepo := newTestOrchestrator(t)
	ctx := context.Background()
	sessionID := uuid.New()
	agentID := uuid.New()

	ag := model.Agent{
		ID:        agentID,
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}
	session := model.CourtSession{ID: sessionID, OptionA: "A", OptionB: "B"}

	speaker, err := orch.ProsecutorSpeak(ctx, ag, session, nil, nil)
	require.NoError(t, err)

	// Orchestrator 应只让本人可见自己的私有记忆
	memRows, err := memRepo.List(ctx, sessionID, agentID, "agent:"+agentID.String())
	require.NoError(t, err)
	require.Len(t, memRows, 1)
	require.Equal(t, string(private_memory.TypeStrategyNote), memRows[0].Type)
	require.Contains(t, memRows[0].Content, "立场=pro_a")
	require.Contains(t, memRows[0].Content, speaker.Reasoning)
}

func TestOrchestrator_DefenderSpeak_OtherAgentCannotReadStrategyNote(t *testing.T) {
	orch, _, memRepo := newTestOrchestrator(t)
	ctx := context.Background()
	sessionID := uuid.New()
	defenderID := uuid.New()
	otherAgentID := uuid.New()

	def := model.Agent{
		ID:        defenderID,
		AgentType: model.AgentDefender,
		BeliefA:   0.25,
	}
	session := model.CourtSession{ID: sessionID, OptionA: "A", OptionB: "B"}

	_, err := orch.DefenderSpeak(ctx, def, session, nil, nil)
	require.NoError(t, err)

	// 辩方的私有 memory，控方 (otherAgentID) 不能读
	_, err = memRepo.List(ctx, sessionID, defenderID, "agent:"+otherAgentID.String())
	require.ErrorIs(t, err, private_memory.ErrNotOwned)

	// 辩方自己可以读
	rows, err := memRepo.List(ctx, sessionID, defenderID, "agent:"+defenderID.String())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, string(private_memory.TypeStrategyNote), rows[0].Type)
}

// uuidOutputJSON 让 stubLLM 返回 LLM "误把 DB UUID 当 evidence_refs" 的
// 场景 —— 这正是 v0.6 evidence_id 显示成 UUID 的根因。测试目的是确认
// 后端 recordSideEffects 用 NormalizeEvidenceRefs 把 UUID 映射回
// display_id，再写入 A2A 消息和 private_memory 表。
func uuidOutputJSON(uuidStr string) string {
	out := AgentOutput{
		Reasoning:    "对方对 E001 的反驳站不住脚",
		Content:      "选项 A 在长期收益上明显占优",
		EvidenceRefs: []string{uuidStr},
		Confidence:   0.8,
		Stance:       "pro_a",
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// TestOrchestrator_ProsecutorSpeak_NormalizesUUIDRefs 是 v0.6 evidence_id
// 显示成 UUID 根因 bug 的端到端回归测试。LLM 返回 DB UUID 时，speak 路径
// 写出的 public speech A2A 消息 + private strategy_note + private_memory
// 表，三处 linked_evidence_ids / evidence_refs / LinkedEvidenceIDs 都必须
// 是 display_id（"E001"），不能是裸 UUID。
func TestOrchestrator_ProsecutorSpeak_NormalizesUUIDRefs(t *testing.T) {
	evidenceUUID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	llmClient := &stubLLM{output: uuidOutputJSON(evidenceUUID.String())}
	orch := NewOrchestratorLegacy(llmClient, bus, memRepo)

	sessionID := uuid.New()
	agentID := uuid.New()
	ag := model.Agent{
		ID:        agentID,
		AgentType: model.AgentProsecutor,
		BeliefA:   0.75,
	}
	session := model.CourtSession{
		ID:           sessionID,
		SessionUUID:  "test-session-uuid",
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 2,
	}
	evidences := []model.Evidence{
		{ID: evidenceUUID, EvidenceID: "E001"},
	}

	_, err := orch.ProsecutorSpeak(context.Background(), ag, session, evidences, nil)
	require.NoError(t, err)

	// 1) A2A 仓库：两条消息（public + private），evidence_refs /
	// linked_evidence_ids 都必须是 display_id。
	rows, err := a2aRepo.ListVisibleTo(context.Background(), sessionID, a2a.AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	for _, row := range rows {
		payload, err := a2a.DecodePayload(row)
		require.NoError(t, err)
		switch row.MessageType {
		case string(a2a.MessageTypeSpeech):
			require.Equal(t, []interface{}{"E001"}, payload["evidence_refs"],
				"public speech evidence_refs must be display_id, not UUID")
		case string(a2a.MessageTypeStrategyNote):
			require.Equal(t, []interface{}{"E001"}, payload["linked_evidence_ids"],
				"private strategy_note linked_evidence_ids must be display_id, not UUID")
		}
	}

	// 2) private_memory 表：LinkedEvidenceIDs 也必须是 display_id。
	memRows, err := memRepo.List(context.Background(), sessionID, agentID, "agent:"+agentID.String())
	require.NoError(t, err)
	require.Len(t, memRows, 1)
	require.Equal(t, model.StringSlice{"E001"}, memRows[0].LinkedEvidenceIDs,
		"private_memory LinkedEvidenceIDs must be display_id, not UUID")
}

func TestOrchestrator_NewOrchestrator_PanicsWithoutBus(t *testing.T) {
	require.Panics(t, func() {
		NewOrchestratorLegacy(&stubLLM{}, nil, private_memory.NewInMemoryRepository(nil))
	})
}

func TestOrchestrator_NewOrchestrator_PanicsWithoutMemory(t *testing.T) {
	require.Panics(t, func() {
		bus := a2a.NewBus(a2a.NewInMemoryRepository(nil), nil)
		NewOrchestratorLegacy(&stubLLM{}, bus, nil)
	})
}
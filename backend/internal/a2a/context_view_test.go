package a2a

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// helper: turn a payload map into the JSONB string the model.A2AMessage.Payload
// column expects. Keeping this in one place avoids drift between tests.
func mustJSON(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(b)
}

// helper: build a public speech message authored by `from` with the given
// payload. We bypass Bus.Send for speed — BuildContextView is a read-only
// projection and does not need to re-persist messages.
func publicSpeechRow(t *testing.T, sessionID uuid.UUID, from string, round int, payload map[string]interface{}) model.A2AMessage {
	t.Helper()
	// Prosecutor speaks at the top of the round's minute; defender's row uses
	// an earlier anchor so within-round ordering is deterministic when rows
	// share the same minute. We only assert ordering, not exact timestamps.
	if from == "defender" {
		return model.A2AMessage{
			ID:          uuid.New(),
			SessionID:   sessionID,
			MessageUUID: uuid.New().String(),
			Round:       round,
			Phase:       "cross_exam",
			FromAgent:   from,
			ToAgent:     "all",
			MessageType: string(MessageTypeSpeech),
			Payload:     mustJSON(t, payload),
			Visibility:  string(VisibilityPublic),
			CreatedAt:   time.Date(2026, 6, 29, 11, 59, 50, 0, time.UTC).Add(time.Duration(round) * time.Minute),
		}
	}
	return model.A2AMessage{
		ID:          uuid.New(),
		SessionID:   sessionID,
		MessageUUID: uuid.New().String(),
		Round:       round,
		Phase:       "cross_exam",
		FromAgent:   from,
		ToAgent:     "all",
		MessageType: string(MessageTypeSpeech),
		Payload:     mustJSON(t, payload),
		Visibility:  string(VisibilityPublic),
		CreatedAt:   time.Date(2026, 6, 29, 12, round, 0, 0, time.UTC),
	}
}

// helper: build a private episodic-memory message authored by `agent` for itself.
func privateMemoRow(t *testing.T, sessionID uuid.UUID, agent string, mtype MessageType, round int, payload map[string]interface{}) model.A2AMessage {
	t.Helper()
	return model.A2AMessage{
		ID:          uuid.New(),
		SessionID:   sessionID,
		MessageUUID: uuid.New().String(),
		Round:       round,
		Phase:       "cross_exam",
		FromAgent:   agent,
		ToAgent:     agent,
		MessageType: string(mtype),
		Payload:     mustJSON(t, payload),
		Visibility:  string(VisibilityPrivate),
		CreatedAt:   time.Date(2026, 6, 29, 12, round, 1, 0, time.UTC),
	}
}

// Test 1: BuildContextView strips reasoning from opposing-side public speech.
func TestContextView_Sanitize_PublicFromOtherSide_StripsReasoning(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := repo.Insert(ctx, publicSpeechRow(t, sessionID, "defender", 1, map[string]interface{}{
		"content":   "选项 B 更稳妥",
		"reasoning": "我看到对方没考虑汇率风险",
	}))
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, view.WorkingMemory, 1, "prosecutor should see defender's public speech")

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(view.WorkingMemory[0].Payload), &payload))
	require.Equal(t, "选项 B 更稳妥", payload["content"], "public content preserved")
	_, hasReasoning := payload["reasoning"]
	require.False(t, hasReasoning, "opposing-side reasoning must be stripped")
}

// Test 2: BuildContextView keeps reasoning on the agent's own public messages.
func TestContextView_Sanitize_PublicFromSelf_KeepsReasoning(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := repo.Insert(ctx, publicSpeechRow(t, sessionID, "prosecutor", 1, map[string]interface{}{
		"content":   "选项 A 优势明显",
		"reasoning": "我要强调 E001 的数据来源",
	}))
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, view.WorkingMemory, 1)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(view.WorkingMemory[0].Payload), &payload))
	require.Equal(t, "我要强调 E001 的数据来源", payload["reasoning"], "own reasoning preserved for self-reflection")
}

// Test 3: Private memory written by selfAgent is visible to selfAgent.
func TestContextView_PrivateOnlySelfSees(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := repo.Insert(ctx, privateMemoRow(t, sessionID, "prosecutor", MessageTypeStrategyNote, 1, map[string]interface{}{
		"memory_type": "strategy_note",
		"content":     "下一轮攻击 E001 数据来源",
	}))
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, view.PrivateMemory, 1, "prosecutor sees own private memory")
	require.Equal(t, string(MessageTypeStrategyNote), view.PrivateMemory[0].MessageType)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(view.PrivateMemory[0].Payload), &payload))
	require.Equal(t, "下一轮攻击 E001 数据来源", payload["content"])
}

// Test 4: Private memory written by another agent is NOT visible.
func TestContextView_PrivateFromOtherSideHidden(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := repo.Insert(ctx, privateMemoRow(t, sessionID, "defender", MessageTypeOpponentWeakness, 1, map[string]interface{}{
		"memory_type": "opponent_weakness",
		"content":     "控方没反驳 E002",
	}))
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Empty(t, view.PrivateMemory, "prosecutor must NOT see defender's private memory")
	// defender's public speech (none here) would still appear in WorkingMemory, but private is forbidden.
	require.Empty(t, view.WorkingMemory)
}

// Test 5: The orchestrator address sees everything (full audit view).
func TestContextView_OrchestratorSeesAll(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := repo.Insert(ctx, publicSpeechRow(t, sessionID, "defender", 1, map[string]interface{}{
		"content":   "X",
		"reasoning": "secret defender thought",
	}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, privateMemoRow(t, sessionID, "defender", MessageTypeStrategyNote, 1, map[string]interface{}{
		"memory_type": "strategy_note",
		"content":     "defender private",
	}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, privateMemoRow(t, sessionID, "prosecutor", MessageTypeOpponentWeakness, 1, map[string]interface{}{
		"memory_type": "opponent_weakness",
		"content":     "prosecutor private",
	}))
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, AddressOrchestrator)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(view.WorkingMemory)+len(view.PrivateMemory), 3,
		"orchestrator should see public + private across all agents")

	// orchestrator also gets un-sanitized reasoning on public messages
	var defenderPub model.A2AMessage
	for _, m := range view.WorkingMemory {
		if m.FromAgent == "defender" && m.Visibility == string(VisibilityPublic) {
			defenderPub = m
			break
		}
	}
	require.NotZero(t, defenderPub.ID, "orchestrator sees defender's public speech")
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(defenderPub.Payload), &payload))
	require.Equal(t, "secret defender thought", payload["reasoning"],
		"orchestrator audit view keeps full payload (no sanitization)")
}

// Test 6: Round ordering is stable (round asc, then created_at asc).
func TestContextView_RoundOrdering(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	// Insert in deliberately wrong order: round 2 first, then round 1, then round 2 again.
	_, err := repo.Insert(ctx, publicSpeechRow(t, sessionID, "prosecutor", 2, map[string]interface{}{
		"content": "second round later message",
	}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, publicSpeechRow(t, sessionID, "prosecutor", 1, map[string]interface{}{
		"content": "first round",
	}))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, publicSpeechRow(t, sessionID, "defender", 2, map[string]interface{}{
		"content": "second round earlier message",
	}))
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, view.WorkingMemory, 3)
	require.Equal(t, 1, view.WorkingMemory[0].Round)
	require.Equal(t, 2, view.WorkingMemory[1].Round)
	require.Equal(t, 2, view.WorkingMemory[2].Round)
	// within round 2: defender's "earlier" timestamp (12:02:30) must come before prosecutor's "later" (12:02:00 of the next minute)
	require.Equal(t, "defender", view.WorkingMemory[1].FromAgent)
	require.Equal(t, "prosecutor", view.WorkingMemory[2].FromAgent)
}

// Test 7: All four private MessageTypes route into PrivateMemory.
func TestContextView_PrivateMemoryMessageTypes_AllVisible(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	types := []MessageType{
		MessageTypeStrategyNote,
		MessageTypeOpponentWeakness,
		MessageTypeSelfCorrection,
		MessageTypeEvidenceEval,
	}
	for i, mt := range types {
		_, err := repo.Insert(ctx, privateMemoRow(t, sessionID, "prosecutor", mt, i+1, map[string]interface{}{
			"memory_type": string(mt),
			"content":     "test",
		}))
		require.NoError(t, err)
	}

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, view.PrivateMemory, 4)

	seen := map[string]bool{}
	for _, m := range view.PrivateMemory {
		require.Equal(t, string(VisibilityPrivate), m.Visibility)
		seen[m.MessageType] = true
	}
	for _, mt := range types {
		require.True(t, seen[string(mt)], "private memory type %s missing from view", mt)
	}
}

// Test 8: An empty session returns an empty LLMContext with no error.
func TestContextView_EmptySession(t *testing.T) {
	bus, _, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.NotNil(t, view)
	require.False(t, view.HasContent())
	require.Empty(t, view.WorkingMemory)
	require.Empty(t, view.PrivateMemory)
	require.NotNil(t, view.Beliefs, "Beliefs map must be non-nil even when empty so callers can range over it")
}

// Test 9 (bonus, ensures SanitizeForViewer single-row API works correctly).
func TestSanitizeForViewer_Rules(t *testing.T) {
	bus, _, _ := newTestBus(t)
	row := publicSpeechRow(t, uuid.New(), "defender", 1, map[string]interface{}{
		"content":   "X",
		"reasoning": "secret",
	})

	// orchestrator → unchanged
	got, err := bus.SanitizeForViewer(row, AddressOrchestrator)
	require.NoError(t, err)
	require.Equal(t, row.Payload, got.Payload)

	// from-agent itself → unchanged
	got, err = bus.SanitizeForViewer(row, "defender")
	require.NoError(t, err)
	require.Equal(t, row.Payload, got.Payload)

	// opposing agent → reasoning stripped
	got, err = bus.SanitizeForViewer(row, "prosecutor")
	require.NoError(t, err)
	var p map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got.Payload), &p))
	_, hasR := p["reasoning"]
	require.False(t, hasR)

	// private row + opposing viewer → ErrNotVisible
	privRow := privateMemoRow(t, uuid.New(), "defender", MessageTypeStrategyNote, 1, map[string]interface{}{})
	_, err = bus.SanitizeForViewer(privRow, "prosecutor")
	require.ErrorIs(t, err, ErrNotVisible)

	// empty viewer rejected
	_, err = bus.SanitizeForViewer(row, "")
	require.Error(t, err)
}

// Test 10 (bonus, malformed payload does not crash BuildContextView).
func TestContextView_MalformedPayload_SkippedButEnvelopeKept(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := repo.Insert(ctx, model.A2AMessage{
		ID:          uuid.New(),
		SessionID:   sessionID,
		MessageUUID: uuid.New().String(),
		Round:       1,
		Phase:       "cross_exam",
		FromAgent:   "defender",
		ToAgent:     "all",
		MessageType: string(MessageTypeSpeech),
		Payload:     "not-valid-json{", // malformed
		Visibility:  string(VisibilityPublic),
		CreatedAt:   time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	view, err := bus.BuildContextView(ctx, sessionID, "prosecutor")
	require.NoError(t, err, "malformed payload must not fail the view")
	require.Len(t, view.WorkingMemory, 1, "envelope kept, even if payload was un-decodable")
	require.Equal(t, "{}", view.WorkingMemory[0].Payload, "fallback payload")
}

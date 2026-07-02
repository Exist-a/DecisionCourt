package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// recordingEmitter captures every Send call so tests can assert the runner
// emitted the right envelope to the A2A bus. We use a real in-memory
// A2A Bus so the message visibility / persistence rules are honored end-
// to-end (this catches e.g. accidentally setting visibility=public).
type recordingEmitter struct {
	mu      sync.Mutex
	sent    []a2a.Message
	sendErr error // when non-nil, Send returns this error
	busInst *a2a.Bus
}

func newRecordingEmitter(t *testing.T) *recordingEmitter {
	t.Helper()
	repo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(repo, nil)
	return &recordingEmitter{busInst: bus}
}

func (r *recordingEmitter) Send(ctx context.Context, msg a2a.Message) (a2a.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendErr != nil {
		return a2a.Message{}, r.sendErr
	}
	r.sent = append(r.sent, msg)
	return r.busInst.Send(ctx, msg)
}

func (r *recordingEmitter) Sent() []a2a.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]a2a.Message, len(r.sent))
	copy(out, r.sent)
	return out
}

// reflectOutputJSONWithMemory returns an action="reflect" AgentOutput JSON
// string with the optional memory_type + memory_note fields populated. The
// suffix "WithMemory" keeps it distinct from react_runner_test.go's plain
// reflectOutputJSON helper (single-arg variant).
func reflectOutputJSONWithMemory(reasoning, mtype, mnote string) string {
	b, _ := json.Marshal(AgentOutput{
		Action:     ActionReflect,
		Reasoning:  reasoning,
		MemoryType: MemoryKind(mtype),
		MemoryNote: mnote,
	})
	return string(b)
}

// --- IsKnownMemoryKind --------------------------------------------------

func TestIsKnownMemoryKind_AllFour(t *testing.T) {
	for _, k := range []MemoryKind{
		MemoryKindStrategyNote,
		MemoryKindOpponentWeakness,
		MemoryKindSelfCorrection,
		MemoryKindEvidenceEval,
	} {
		require.True(t, IsKnownMemoryKind(k), "expected %s to be known", k)
	}
}

func TestIsKnownMemoryKind_RejectsUnknown(t *testing.T) {
	for _, k := range []MemoryKind{"", "strategy", "STRATEGY_NOTE", "evidence", "all"} {
		require.False(t, IsKnownMemoryKind(k), "expected %q to be unknown", k)
	}
}

// --- ToA2AMemoryMessageType ---------------------------------------------

func TestToA2AMemoryMessageType_KnownAndUnknown(t *testing.T) {
	require.Equal(t, a2a.MessageTypeStrategyNote, ToA2AMemoryMessageType(MemoryKindStrategyNote))
	require.Equal(t, a2a.MessageTypeOpponentWeakness, ToA2AMemoryMessageType(MemoryKindOpponentWeakness))
	require.Equal(t, a2a.MessageTypeSelfCorrection, ToA2AMemoryMessageType(MemoryKindSelfCorrection))
	require.Equal(t, a2a.MessageTypeEvidenceEval, ToA2AMemoryMessageType(MemoryKindEvidenceEval))
	require.Equal(t, a2a.MessageTypeStrategyNote, ToA2AMemoryMessageType("garbage"))
}

// --- AgentOutput.HasMemory ----------------------------------------------

func TestAgentOutput_HasMemory(t *testing.T) {
	cases := []struct {
		name string
		out  AgentOutput
		want bool
	}{
		{"both filled", AgentOutput{MemoryType: MemoryKindStrategyNote, MemoryNote: "x"}, true},
		{"only type", AgentOutput{MemoryType: MemoryKindStrategyNote}, false},
		{"only note", AgentOutput{MemoryNote: "x"}, false},
		{"unknown type", AgentOutput{MemoryType: "made_up", MemoryNote: "x"}, false},
		{"empty", AgentOutput{}, false},
		{"whitespace note is empty after trim", AgentOutput{MemoryType: MemoryKindStrategyNote, MemoryNote: "   "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.out.HasMemory())
		})
	}
}

// --- EmitMemoryFromOutput -----------------------------------------------

func TestEmitMemoryFromOutput_NilEmitter_NoCrash(t *testing.T) {
	out := AgentOutput{MemoryType: MemoryKindStrategyNote, MemoryNote: "x"}
	require.NoError(t, EmitMemoryFromOutput(context.Background(), nil, MemoryMeta{
		SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam",
	}, out))
}

func TestEmitMemoryFromOutput_NoMemory_NoSend(t *testing.T) {
	em := newRecordingEmitter(t)
	err := EmitMemoryFromOutput(context.Background(), em, MemoryMeta{
		SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam",
	}, AgentOutput{Action: "reflect", Reasoning: "no memory attached"})
	require.NoError(t, err)
	require.Empty(t, em.Sent())
}

func TestEmitMemoryFromOutput_AllFourTypesEnvelopesCorrect(t *testing.T) {
	em := newRecordingEmitter(t)
	meta := MemoryMeta{
		SessionID: uuid.New(), AgentType: "prosecutor", Round: 2, Phase: "cross_exam",
	}
	cases := []struct {
		kind MemoryKind
		want a2a.MessageType
	}{
		{MemoryKindStrategyNote, a2a.MessageTypeStrategyNote},
		{MemoryKindOpponentWeakness, a2a.MessageTypeOpponentWeakness},
		{MemoryKindSelfCorrection, a2a.MessageTypeSelfCorrection},
		{MemoryKindEvidenceEval, a2a.MessageTypeEvidenceEval},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			err := EmitMemoryFromOutput(context.Background(), em, meta, AgentOutput{
				Action:       "reflect",
				MemoryType:   c.kind,
				MemoryNote:   "test note for " + string(c.kind),
				EvidenceRefs: []string{"E001"},
			})
			require.NoError(t, err)
		})
	}
	sent := em.Sent()
	require.Len(t, sent, 4, "all four kinds must each emit one message")

	for i, m := range sent {
		require.Equal(t, meta.SessionID, m.SessionID)
		require.Equal(t, meta.Round, m.Round)
		require.Equal(t, meta.Phase, m.Phase)
		require.Equal(t, "prosecutor", m.From, "from must be self")
		require.Equal(t, "prosecutor", m.To, "to must be self (self-to-self)")
		require.Equal(t, a2a.VisibilityPrivate, m.Visibility, "must be private")
		require.Equal(t, cases[i].want, m.MessageType)
	}
}

func TestEmitMemoryFromOutput_EmitterErrorPropagates(t *testing.T) {
	em := newRecordingEmitter(t)
	em.sendErr = errors.New("DB down")
	err := EmitMemoryFromOutput(context.Background(), em, MemoryMeta{
		SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam",
	}, AgentOutput{MemoryType: MemoryKindStrategyNote, MemoryNote: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "DB down")
}

func TestBuildPrivateMemoryMessage_PayloadShape(t *testing.T) {
	out := AgentOutput{
		Action:       "reflect",
		MemoryType:   MemoryKindOpponentWeakness,
		MemoryNote:   " 辩方没反驳 E001 ",
		EvidenceRefs: []string{"E001", "E002"},
	}
	meta := MemoryMeta{
		SessionID: uuid.New(), AgentType: "defender", Round: 3, Phase: "cross_exam",
	}
	msg := buildPrivateMemoryMessage(meta, out)

	require.Equal(t, a2a.VisibilityPrivate, msg.Visibility)
	require.Equal(t, "defender", msg.From)
	require.Equal(t, "defender", msg.To)
	require.Equal(t, meta.Round, msg.Round)
	require.Equal(t, meta.Phase, msg.Phase)
	require.Equal(t, a2a.MessageTypeOpponentWeakness, msg.MessageType)

	// payload check
	p := msg.Payload
	require.Equal(t, "opponent_weakness", p["memory_type"])
	require.Equal(t, "辩方没反驳 E001", p["content"], "note must be trimmed")
	require.Equal(t, meta.Round, p["round"])
	require.Equal(t, []string{"E001", "E002"}, p["linked_evidence_ids"])
}

// TestBuildPrivateMemoryMessage_NormalizesUUIDs 是 v0.6 evidence_id 显示成
// UUID 根因 bug 的回归测试：reflect 步骤触发的 strategy_note 也必须走
// NormalizeEvidenceRefs，把 LLM 偶尔返回的 DB UUID 映射回 display_id。
// 详见 .trae/documents/memory-a2a-redesign.md §"已发现但未做"。
func TestBuildPrivateMemoryMessage_NormalizesUUIDs(t *testing.T) {
	evidences := []model.Evidence{
		{ID: uuidE001, EvidenceID: "E001"},
		{ID: uuidE002, EvidenceID: "E002"},
	}
	out := AgentOutput{
		Action:       "reflect",
		MemoryType:   MemoryKindStrategyNote,
		MemoryNote:   "E001 数据来源已核实",
		// LLM 错误返回了 UUID 字符串。
		EvidenceRefs: []string{uuidE001.String(), uuidE002.String()},
	}
	meta := MemoryMeta{
		SessionID: uuid.New(),
		AgentType: "prosecutor",
		Round:     2,
		Phase:     "cross_exam",
		Evidences: evidences,
	}

	msg := buildPrivateMemoryMessage(meta, out)

	p := msg.Payload
	require.Equal(t, []string{"E001", "E002"}, p["linked_evidence_ids"],
		"reflect path must NormalizeEvidenceRefs before writing linked_evidence_ids")
}

// TestBuildPrivateMemoryMessage_NilEvidences_LegacyPath 测试旧调用方不传
// Evidences 时不会引入回归 —— linked_evidence_ids 保留 LLM 原样（display_id）。
func TestBuildPrivateMemoryMessage_NilEvidences_LegacyPath(t *testing.T) {
	out := AgentOutput{
		Action:       "reflect",
		MemoryType:   MemoryKindStrategyNote,
		MemoryNote:   "x",
		EvidenceRefs: []string{"E001", "E002"},
	}
	meta := MemoryMeta{
		SessionID: uuid.New(),
		AgentType: "prosecutor",
		Round:     1,
		// Evidences 缺省（nil）—— 模拟旧调用方不传。
	}

	msg := buildPrivateMemoryMessage(meta, out)

	p := msg.Payload
	require.Equal(t, []string{"E001", "E002"}, p["linked_evidence_ids"],
		"legacy nil-Evidences path must pass through display_id unchanged")
}

// --- Runner integration -------------------------------------------------

func TestRunner_ReflectWithMemory_FiresHook(t *testing.T) {
	em := newRecordingEmitter(t)
	// Step 1: reflect with memory entry
	// Step 2: speak (must NOT emit memory)
	scripts := []string{
		reflectOutputJSONWithMemory("我意识到对方没反驳 E001", "opponent_weakness", "辩方没反驳 E001"),
		speakOutputJSON("基于上述观察", "我方立场成立", "pro_a", 0.8),
	}
	llmClient := &scriptedLLM{scripts: scripts}
	runner := NewReActRunner(llmClient, "sys", nil, RunnerConfig{
		MaxIterations: 4,
		MemoryHook: func(ctx context.Context, out AgentOutput, meta MemoryMeta) error {
			return EmitMemoryFromOutput(ctx, em, meta, out)
		},
		MemoryMeta: MemoryMeta{
			SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam",
		},
	})
	speaker, _, err := runner.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "我方立场成立", speaker.Content)

	sent := em.Sent()
	require.Len(t, sent, 1, "only reflect step emits, speak step does NOT")
	require.Equal(t, a2a.MessageTypeOpponentWeakness, sent[0].MessageType)
	require.Equal(t, a2a.VisibilityPrivate, sent[0].Visibility)
}

func TestRunner_ReflectWithoutMemory_DoesNotFireHook(t *testing.T) {
	em := newRecordingEmitter(t)
	scripts := []string{
		reflectOutputJSONWithMemory("just thinking, no memory", "", ""),
		speakOutputJSON("done thinking", "conclusion", "pro_a", 0.8),
	}
	runner := NewReActRunner(&scriptedLLM{scripts: scripts}, "sys", nil, RunnerConfig{
		MaxIterations: 4,
		MemoryHook: func(ctx context.Context, out AgentOutput, meta MemoryMeta) error {
			return EmitMemoryFromOutput(ctx, em, meta, out)
		},
		MemoryMeta: MemoryMeta{SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam"},
	})
	_, _, err := runner.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, em.Sent(), "empty memory must not fire hook")
}

func TestRunner_NilHook_StillCompletesNormally(t *testing.T) {
	scripts := []string{
		reflectOutputJSONWithMemory("thinking", "strategy_note", "next round plan"),
		speakOutputJSON("ready", "speech", "pro_a", 0.8),
	}
	runner := NewReActRunner(&scriptedLLM{scripts: scripts}, "sys", nil, RunnerConfig{
		MaxIterations: 4,
		// MemoryHook is nil
		MemoryMeta: MemoryMeta{SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam"},
	})
	speaker, _, err := runner.Run(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "speech", speaker.Content)
}

func TestRunner_ThreeRoundMemoryDiversity(t *testing.T) {
	// Integration-style test: simulate a 3-round cross-exam where the LLM
	// emits all 4 memory kinds across multiple reflect steps. Verify that
	// the final state in the bus contains one of each kind.
	em := newRecordingEmitter(t)
	scripts := []string{
		// round 1 reflect: strategy_note
		reflectOutputJSONWithMemory("round 1 strategy", "strategy_note", "强调 E001 数据来源"),
		reflectOutputJSONWithMemory("deeper", "opponent_weakness", "辩方没反驳 E002"),
		// round 2 reflect: self_correction
		reflectOutputJSONWithMemory("correct", "self_correction", "我之前论证 X 有误，下轮修正"),
		// round 2 reflect: evidence_eval
		reflectOutputJSONWithMemory("eval", "evidence_eval", "E003 对 option_a 强度 0.7"),
		speakOutputJSON("ready", "final speech", "pro_a", 0.85),
	}
	runner := NewReActRunner(&scriptedLLM{scripts: scripts}, "sys", nil, RunnerConfig{
		MaxIterations: 8,
		MemoryHook: func(ctx context.Context, out AgentOutput, meta MemoryMeta) error {
			return EmitMemoryFromOutput(ctx, em, meta, out)
		},
		MemoryMeta: MemoryMeta{SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam"},
	})
	_, _, err := runner.Run(context.Background(), nil)
	require.NoError(t, err)

	sent := em.Sent()
	require.Len(t, sent, 4, "all four kinds must be emitted")
	seen := map[a2a.MessageType]bool{}
	for _, m := range sent {
		seen[m.MessageType] = true
	}
	for _, want := range []a2a.MessageType{
		a2a.MessageTypeStrategyNote,
		a2a.MessageTypeOpponentWeakness,
		a2a.MessageTypeSelfCorrection,
		a2a.MessageTypeEvidenceEval,
	} {
		require.True(t, seen[want], "missing kind %s in emitted memory", want)
	}
}

func TestRunner_HookError_DoesNotAbortTrial(t *testing.T) {
	// Runner must keep going even when the memory hook errors out.
	scripts := []string{
		reflectOutputJSONWithMemory("thinking", "strategy_note", "plan"),
		speakOutputJSON("ready", "speech content", "pro_a", 0.8),
	}
	runner := NewReActRunner(&scriptedLLM{scripts: scripts}, "sys", nil, RunnerConfig{
		MaxIterations: 4,
		MemoryHook: func(ctx context.Context, out AgentOutput, meta MemoryMeta) error {
			return errors.New("simulated failure")
		},
		MemoryMeta: MemoryMeta{SessionID: uuid.New(), AgentType: "prosecutor", Round: 1, Phase: "cross_exam"},
	})
	speaker, _, err := runner.Run(context.Background(), nil)
	require.NoError(t, err, "memory hook failure must NOT fail the trial")
	require.Equal(t, "speech content", speaker.Content)
}

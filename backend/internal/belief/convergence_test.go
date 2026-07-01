package belief

import (
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// helpers --------------------------------------------------------------------

// mkSnap returns a BeliefSnapshot at a fixed time for a given agent_id +
// belief_a. Round is set explicitly so the test can build prev/curr snapshots
// without time-based ordering surprises.
func mkSnap(agentID uuid.UUID, round int, beliefA float64) model.BeliefSnapshot {
	return model.BeliefSnapshot{
		ID:        uuid.New(),
		AgentID:   agentID,
		Round:     round,
		BeliefA:   beliefA,
		BeliefB:   1 - beliefA,
		CreatedAt: time.Date(2026, 7, 1, 12, round, 0, 0, time.UTC),
	}
}

// mkMsg returns a model.Message with the supplied content / agent.
func mkMsg(agentID uuid.UUID, content string) model.Message {
	return model.Message{
		ID:        uuid.New(),
		AgentID:   &agentID,
		ActionType: "speak",
		Content:   content,
	}
}

// agentTypeMap is a tiny helper to build the agentTypes lookup map with the
// canonical four-agent layout that the production service uses.
func agentTypeMap(pros, def, inv, clerk uuid.UUID) map[uuid.UUID]model.AgentType {
	return map[uuid.UUID]model.AgentType{
		pros:  model.AgentProsecutor,
		def:   model.AgentDefender,
		inv:   model.AgentInvestigator,
		clerk: model.AgentClerk,
	}
}

// tests ----------------------------------------------------------------------

// TestCheckConvergence_DefaultContinuation exercises the "happy path": a
// round with normal movement, no consensus, no oscillation, no
// drift-low, no max-rounds. Must return Continue.
func TestCheckConvergence_DefaultContinuation(t *testing.T) {
	e := NewEngine()
	pros, def, inv, clerk := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, inv, clerk)

	prev := []model.BeliefSnapshot{mkSnap(pros, 1, 0.70), mkSnap(def, 1, 0.30)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 2, 0.72), mkSnap(def, 2, 0.28)}
	msgs := []model.Message{
		mkMsg(pros, "我方坚持认为目标公司的期权价值应当被充分重视"),
		mkMsg(def, "我方认为期权价值存在估值过高的风险，无法作为主要论据"),
	}

	d := e.CheckConvergence(2, prev, curr, msgs, at, 0, DefaultConvergenceConfig())
	if d.IsConverged() {
		t.Fatalf("expected Continue, got %+v", d)
	}
}

// TestCheckConvergence_OscillationTrumpsAll locks down the priority order:
// oscillation wins even when consensus is also present.
func TestCheckConvergence_OscillationTrumpsAll(t *testing.T) {
	e := NewEngine()
	pros, def, inv, clerk := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, inv, clerk)

	prev := []model.BeliefSnapshot{mkSnap(pros, 1, 0.85), mkSnap(def, 1, 0.85)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 2, 0.87), mkSnap(def, 2, 0.87)}

	// Two messages with >0.6 Jaccard similarity.
	msgs := []model.Message{
		mkMsg(pros, "估值过高 估值过高 估值过高 估值过高 估值过高"),
		mkMsg(def, "估值过高 估值过高 估值过高 估值过高 估值过高"),
	}

	d := e.CheckConvergence(2, prev, curr, msgs, at, 0, DefaultConvergenceConfig())
	if !d.IsConverged() {
		t.Fatalf("expected converge, got %+v", d)
	}
	if d.Reason != "reasoning_oscillation" {
		t.Fatalf("expected reasoning_oscillation, got %s", d.Reason)
	}
}

// TestCheckConvergence_Consensus verifies both-extreme-on-A path.
func TestCheckConvergence_Consensus(t *testing.T) {
	e := NewEngine()
	pros, def, inv, clerk := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, inv, clerk)

	prev := []model.BeliefSnapshot{mkSnap(pros, 1, 0.7), mkSnap(def, 1, 0.7)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 2, 0.90), mkSnap(def, 2, 0.90)}
	// Different content — otherwise oscillation fires before consensus.
	msgs := []model.Message{
		mkMsg(pros, "控方补充新证据 E005 显示目标公司市场份额上升"),
		mkMsg(def, "辩方承认 E005 数据 但强调波动性更高"),
	}

	d := e.CheckConvergence(2, prev, curr, msgs, at, 0, DefaultConvergenceConfig())
	if !d.IsConverged() || d.Reason != "consensus" {
		t.Fatalf("expected consensus, got %+v", d)
	}
}

// TestCheckConvergence_ConsensusBothOnB verifies both-extreme-on-B (low
// belief_a) is also detected as consensus.
func TestCheckConvergence_ConsensusBothOnB(t *testing.T) {
	e := NewEngine()
	pros, def, inv, clerk := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, inv, clerk)

	prev := []model.BeliefSnapshot{mkSnap(pros, 1, 0.3), mkSnap(def, 1, 0.3)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 2, 0.10), mkSnap(def, 2, 0.10)}
	msgs := []model.Message{
		mkMsg(pros, "控方承认 B 路径风险 但寻求缓解策略"),
		mkMsg(def, "辩方坚持 B 路径不可行 因为法规约束"),
	}

	d := e.CheckConvergence(2, prev, curr, msgs, at, 0, DefaultConvergenceConfig())
	if !d.IsConverged() || d.Reason != "consensus" {
		t.Fatalf("expected consensus, got %+v", d)
	}
}

// TestCheckConvergence_DriftLowRequiresConsecutive verifies the
// drift-low signal only fires once stableRounds has reached threshold.
// One round with low drift is not enough.
func TestCheckConvergence_DriftLowRequiresConsecutive(t *testing.T) {
	e := NewEngine()
	pros, def, inv, clerk := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, inv, clerk)

	prev := []model.BeliefSnapshot{mkSnap(pros, 1, 0.70), mkSnap(def, 1, 0.30)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 2, 0.71), mkSnap(def, 2, 0.29)} // Δ = 0.01
	msgs := []model.Message{
		mkMsg(pros, "新鲜论点 A 关于市场风险的对冲"),
		mkMsg(def, "新鲜论点 B 关于风险偏好的分歧"),
	}

	// stableRounds = 1 → not yet.
	if d := e.CheckConvergence(2, prev, curr, msgs, at, 1, DefaultConvergenceConfig()); d.IsConverged() {
		t.Fatalf("drift-low should NOT fire when stableRounds=1: %+v", d)
	}
	// stableRounds = 2 → fire.
	if d := e.CheckConvergence(2, prev, curr, msgs, at, 2, DefaultConvergenceConfig()); !d.IsConverged() || d.Reason != "belief_stable" {
		t.Fatalf("drift-low SHOULD fire when stableRounds=2: %+v", d)
	}
}

// TestCheckConvergence_MaxRoundsFallback verifies the safety net.
func TestCheckConvergence_MaxRoundsFallback(t *testing.T) {
	e := NewEngine()
	pros, def, inv, clerk := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, inv, clerk)

	prev := []model.BeliefSnapshot{mkSnap(pros, 4, 0.7), mkSnap(def, 4, 0.3)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 5, 0.65), mkSnap(def, 5, 0.35)}
	msgs := []model.Message{
		mkMsg(pros, "Alpha 论点 论证链 A 充分"),
		mkMsg(def, "Beta 论点 论证链 B 充分"),
	}

	d := e.CheckConvergence(5, prev, curr, msgs, at, 0, DefaultConvergenceConfig())
	if !d.IsConverged() || d.Reason != "max_rounds" {
		t.Fatalf("expected max_rounds fallback, got %+v", d)
	}
}

// TestCheckConvergence_NoRecentMessagesDegradesOscillation: when there's
// not enough signal (e.g. only one agent has spoken), oscillation must
// NOT spuriously fire. This protects against single-speaker trials.
func TestCheckConvergence_NoRecentMessagesDegradesOscillation(t *testing.T) {
	e := NewEngine()
	pros, def, _, _ := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := agentTypeMap(pros, def, uuid.New(), uuid.New())

	prev := []model.BeliefSnapshot{mkSnap(pros, 1, 0.7), mkSnap(def, 1, 0.3)}
	curr := []model.BeliefSnapshot{mkSnap(pros, 2, 0.71), mkSnap(def, 2, 0.29)}
	// Only one agent has spoken — oscillation must not trigger.
	msgs := []model.Message{mkMsg(pros, "孤独的发言")}

	if d := e.CheckConvergence(2, prev, curr, msgs, at, 0, DefaultConvergenceConfig()); d.IsConverged() {
		t.Fatalf("single-speaker trial must not converge on oscillation: %+v", d)
	}
}

// TestBagOfWords_ChineseAndEnglish mixes CJK + ASCII tokens to make sure
// the tokenizer handles both. Jaccard must work across scripts.
func TestBagOfWords_ChineseAndEnglish(t *testing.T) {
	a := bagOfWords("Hello world 你好世界")
	b := bagOfWords("Hello world 完全不同")
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("bagOfWords returned empty: a=%v b=%v", a, b)
	}
	// Sanity: at least one common Latin token.
	if _, ok := a["hello"]; !ok {
		t.Fatalf("expected 'hello' token in a: %v", a)
	}
	if _, ok := b["hello"]; !ok {
		t.Fatalf("expected 'hello' token in b: %v", b)
	}
}

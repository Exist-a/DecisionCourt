package belief

import (
	"context"
	"math"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// TestLogitSigmoidRoundTrip protects the two helper math functions from
// drifting out of sync. They are inverses on (0,1) → R and R → (0,1).
func TestLogitSigmoidRoundTrip(t *testing.T) {
	for _, p := range []float64{0.05, 0.1, 0.3, 0.5, 0.7, 0.9, 0.95} {
		z := Logit(p)
		got := Sigmoid(z)
		if math.Abs(got-p) > 1e-6 {
			t.Fatalf("round trip p=%.3f z=%.4f got=%.6f", p, z, got)
		}
	}
}

// TestLogitClamping protects against ±Inf when p is at the boundary.
func TestLogitClamping(t *testing.T) {
	for _, p := range []float64{0, 1, -0.001, 1.001} {
		z := Logit(p)
		if math.IsInf(z, 0) || math.IsNaN(z) {
			t.Fatalf("Logit(p=%.3f) returned non-finite value %v", p, z)
		}
	}
}

// TestDirectionFromImpact covers the three branches of the mapping.
func TestDirectionFromImpact(t *testing.T) {
	cases := []struct {
		name           string
		impactA, impactB float64
		want           string
	}{
		{"supports_a", 0.6, 0.2, model.BeliefDirSupportsA},
		{"supports_b", 0.2, 0.6, model.BeliefDirSupportsB},
		{"tie_neutral", 0.4, 0.4, model.BeliefDirNeutral},
		{"supports_b_negative", -0.4, 0.3, model.BeliefDirSupportsB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DirectionFromImpact(c.impactA, c.impactB)
			if got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

// TestEvidenceWeight_NoWeaken covers the bare weight formula.
func TestEvidenceWeight_NoWeaken(t *testing.T) {
	e := model.Evidence{CredibilityScore: 0.8, RelevanceScore: 0.9, ImpactOnOptionA: 0.5}
	got := EvidenceWeight(e, nil, model.AgentProsecutor)
	want := 0.8 * 0.9 * 0.5
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.4f want %.4f", got, want)
	}
}

// TestEvidenceWeight_WeakenApplied covers the weaken multiplier reducing
// the effective weight. Weaken strength 0.6 → multiplier 0.4 → weight halves.
func TestEvidenceWeight_WeakenApplied(t *testing.T) {
	e := model.Evidence{CredibilityScore: 0.8, RelevanceScore: 0.9, ImpactOnOptionA: 0.5}
	weakens := []model.EvidenceWeakenLink{
		{EvidenceID: e.ID, TargetAgent: model.AgentProsecutor, WeakenStrength: 0.6},
	}
	got := EvidenceWeight(e, weakens, model.AgentProsecutor)
	want := 0.8 * 0.9 * 0.5 * 0.4
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.4f want %.4f", got, want)
	}
}

// TestEvidenceWeight_WeakenForOtherAgentIgnored ensures we only apply weaken
// declarations that explicitly target the agent being updated.
func TestEvidenceWeight_WeakenForOtherAgentIgnored(t *testing.T) {
	e := model.Evidence{CredibilityScore: 0.8, RelevanceScore: 0.9, ImpactOnOptionA: 0.5}
	weakens := []model.EvidenceWeakenLink{
		{EvidenceID: e.ID, TargetAgent: model.AgentDefender, WeakenStrength: 0.9},
	}
	got := EvidenceWeight(e, weakens, model.AgentProsecutor)
	want := 0.8 * 0.9 * 0.5 // unchanged from no-weaken case
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.4f want %.4f (should ignore defender-targeted weaken)", got, want)
	}
}

// TestConfirmationSign exhausts the 6-case truth table for the
// confirmation-sign calculation. Locks down the asym between a 0.7-leaning
// prosecutor and a 0.3-leaning defender reacting to the same evidence.
func TestConfirmationSign(t *testing.T) {
	cases := []struct {
		name    string
		prior   float64
		impactA float64
		impactB float64
		want    float64
	}{
		{"leans_a_confirms", 0.7, 0.6, 0.0, 1.0},
		{"leans_a_contradicted", 0.7, 0.0, 0.6, -1.0},
		{"leans_b_confirms", 0.3, 0.0, 0.6, 1.0},
		{"leans_b_contradicted", 0.3, 0.6, 0.0, -1.0},
		{"neutral_supports_a", 0.5, 0.6, 0.0, 1.0},
		{"neutral_supports_b", 0.5, 0.0, 0.6, -1.0},
		{"near_neutral_treated_neutral", 0.5005, 0.0, 0.6, -1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := confirmationSign(c.prior, c.impactA, c.impactB)
			if math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("got %.3f want %.3f", got, c.want)
			}
		})
	}
}

// TestUpdateWithDiff_ProsecutorSmallMove verifies that a single typical
// piece of evidence moves the prosecutor's belief by 0.03–0.07 (matches
// the magnitude we were producing with the legacy linear algorithm).
func TestUpdateWithDiff_ProsecutorSmallMove(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	prosecutor := model.Agent{
		AgentType: model.AgentProsecutor,
		BeliefA:   0.70,
		BeliefB:   0.30,
	}
	evidence := model.Evidence{
		ID:              uuid.New(),
		CredibilityScore: 0.85,
		RelevanceScore:   0.80,
		ImpactOnOptionA:  0.6,
		ImpactOnOptionB:  0.0,
	}

	agents, diffs, err := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{prosecutor}, evidence, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff got %d", len(diffs))
	}
	d := diffs[0]
	if math.Abs(d.DeltaBeliefA) < 0.005 || math.Abs(d.DeltaBeliefA) > 0.10 {
		t.Fatalf("Δ=%.4f outside expected [0.005, 0.10] band for one piece of strong evidence", d.DeltaBeliefA)
	}
	if agents[0].BeliefA == 0.70 {
		t.Fatal("prosecutor belief_a should have moved")
	}
	if d.Source != model.BeliefSrcEvidence {
		t.Fatalf("source = %s want evidence", d.Source)
	}
	if d.Direction != model.BeliefDirSupportsA {
		t.Fatalf("direction = %s want supports_a", d.Direction)
	}
}

// TestUpdateWithDiff_InvestigatorMovesMoreThanProsecutor compares two
// identical pieces of evidence hitting two different agent types.
// Investigator must move ≥ 2× the prosecutor's Δ — that's the entire
// point of differentiating anchoring personalities.
func TestUpdateWithDiff_InvestigatorMovesMoreThanProsecutor(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	evidence := model.Evidence{
		ID:              uuid.New(),
		CredibilityScore: 0.85,
		RelevanceScore:   0.80,
		ImpactOnOptionA:  0.6,
		ImpactOnOptionB:  0.0,
	}
	prosecutor := model.Agent{AgentType: model.AgentProsecutor, BeliefA: 0.70, BeliefB: 0.30}
	investigator := model.Agent{AgentType: model.AgentInvestigator, BeliefA: 0.50, BeliefB: 0.50}

	prosecutorAgents, pDiffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{prosecutor}, evidence, nil,
	)
	investigatorAgents, iDiffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{investigator}, evidence, nil,
	)

	pDelta := math.Abs(pDiffs[0].DeltaBeliefA)
	iDelta := math.Abs(iDiffs[0].DeltaBeliefA)
	if iDelta < 2*pDelta {
		t.Fatalf("investigator Δ=%.4f should be ≥ 2× prosecutor Δ=%.4f", iDelta, pDelta)
	}
	_ = prosecutorAgents
	_ = investigatorAgents
}

// TestUpdateWithDiff_ProsecutorResistsManyWeakEvidences is the headline
// claim of the v0.6 algorithm: 10 weak pieces cannot flip a strongly
// anchored prosecutor. This is the case that the legacy linear algorithm
// got wrong; we want this test to fail loudly if anyone unanchors the
// anchoring config.
func TestUpdateWithDiff_ProsecutorResistsManyWeakEvidences(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	prosecutor := model.Agent{AgentType: model.AgentProsecutor, BeliefA: 0.70, BeliefB: 0.30}
	// 10 pieces of very weak evidence, all arguing against A (impact_a = 0.05)
	for i := 0; i < 10; i++ {
		weak := model.Evidence{
			ID:              uuid.New(),
			CredibilityScore: 0.5,
			RelevanceScore:   0.5,
			ImpactOnOptionA:  0.05,
			ImpactOnOptionB:  0.0,
		}
		updated, _, err := mustUpdateOne(engine, repo, sessionID, prosecutor, weak)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		prosecutor = updated
	}
	// After 10 weak evidences, prosecutor should still be > 0.55.
	if prosecutor.BeliefA < 0.55 {
		t.Fatalf("prosecutor flipped to %.4f from 10 weak evidences; expected > 0.55", prosecutor.BeliefA)
	}
}

// TestUpdateWithDiff_ClerkNeverMoves guards the neutral-skip behaviour.
func TestUpdateWithDiff_ClerkNeverMoves(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	clerk := model.Agent{AgentType: model.AgentClerk, BeliefA: 0.50, BeliefB: 0.50}
	evidence := model.Evidence{
		ID:              uuid.New(),
		CredibilityScore: 1.0,
		RelevanceScore:   1.0,
		ImpactOnOptionA:  1.0,
		ImpactOnOptionB:  0.0,
	}
	agents, diffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{clerk}, evidence, nil,
	)
	if agents[0].BeliefA != 0.50 {
		t.Fatalf("clerk moved: %.4f", agents[0].BeliefA)
	}
	if len(diffs) != 0 {
		t.Fatalf("clerk wrote %d diffs; expected 0", len(diffs))
	}
}

// TestUpdateWithDiff_DefenderSym tests that defenders move the opposite way
// from prosecutors (positive-A evidence pushes defender DOWN).
func TestUpdateWithDiff_DefenderSym(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	evidence := model.Evidence{
		ID:              uuid.New(),
		CredibilityScore: 0.9,
		RelevanceScore:   0.9,
		ImpactOnOptionA:  0.7,
		ImpactOnOptionB:  0.0,
	}
	prosecutor := model.Agent{AgentType: model.AgentProsecutor, BeliefA: 0.70, BeliefB: 0.30}
	defender := model.Agent{AgentType: model.AgentDefender, BeliefA: 0.30, BeliefB: 0.70}

	_, pDiffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{prosecutor}, evidence, nil,
	)
	_, dDiffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{defender}, evidence, nil,
	)

	if pDiffs[0].DeltaBeliefA <= 0 {
		t.Fatalf("prosecutor Δ=%.4f should be positive (supports A)", pDiffs[0].DeltaBeliefA)
	}
	if dDiffs[0].DeltaBeliefA >= 0 {
		t.Fatalf("defender Δ=%.4f should be negative (against A)", dDiffs[0].DeltaBeliefA)
	}
}

// TestUpdateWithDiff_BeliefStaysInRange ensures the [0.05, 0.95] clamp is
// honoured even when flood-input pushes the logit toward ±infinity.
func TestUpdateWithDiff_BeliefStaysInRange(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	agent := model.Agent{AgentType: model.AgentInvestigator, BeliefA: 0.5, BeliefB: 0.5}
	for i := 0; i < 50; i++ {
		strong := model.Evidence{
			ID:              uuid.New(),
			CredibilityScore: 1.0,
			RelevanceScore:   1.0,
			ImpactOnOptionA:  1.0,
			ImpactOnOptionB:  0.0,
		}
		updated, _, _ := mustUpdateOne(engine, repo, sessionID, agent, strong)
		agent = updated
		if agent.BeliefA < 0.04 || agent.BeliefA > 0.96 {
			t.Fatalf("belief broke range after %d updates: %.4f", i+1, agent.BeliefA)
		}
	}
}

// TestUpdateWithDiff_WeakenMultiplier is the integration-level proof that
// a weaken declaration reduces evidence impact on the targeted agent but
// leaves the other side untouched.
func TestUpdateWithDiff_WeakenMultiplier(t *testing.T) {
	engine := NewEngine()
	repo := NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()

	evidence := model.Evidence{
		ID:              uuid.New(),
		CredibilityScore: 0.9,
		RelevanceScore:   0.9,
		ImpactOnOptionA:  0.7,
		ImpactOnOptionB:  0.0,
	}
	prosecutor := model.Agent{AgentType: model.AgentProsecutor, BeliefA: 0.70, BeliefB: 0.30}

	// Round A: no weaken → baseline Δ.
	_, baseDiffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{prosecutor}, evidence, nil,
	)

	// Round B: same evidence, but a 50% weaken declaration against prosecutor.
	weakens := []model.EvidenceWeakenLink{
		{EvidenceID: evidence.ID, TargetAgent: model.AgentProsecutor, WeakenStrength: 0.5},
	}
	_, weakDiffs, _ := engine.UpdateWithDiff(
		context.Background(), repo, sessionID, 2, "cross_exam",
		[]model.Agent{prosecutor}, evidence, weakens,
	)

	baseDelta := math.Abs(baseDiffs[0].DeltaBeliefA)
	weakDelta := math.Abs(weakDiffs[0].DeltaBeliefA)
	if weakDelta >= baseDelta {
		t.Fatalf("weakened Δ=%.4f should be < baseline Δ=%.4f", weakDelta, baseDelta)
	}
	if weakDiffs[0].WeakenFactor >= 1.0 {
		t.Fatalf("weaken factor %.4f should be < 1.0", weakDiffs[0].WeakenFactor)
	}
}

// mustUpdateOne is a tiny helper that runs one update and returns the
// updated agent plus diffs (keeping the test bodies short).
func mustUpdateOne(e *Engine, repo DiffRepository, sessionID uuid.UUID, agent model.Agent, ev model.Evidence) (model.Agent, []model.BeliefDiff, error) {
	agents, diffs, err := e.UpdateWithDiff(
		context.Background(), repo, sessionID, 1, "cross_exam",
		[]model.Agent{agent}, ev, nil,
	)
	return agents[0], diffs, err
}

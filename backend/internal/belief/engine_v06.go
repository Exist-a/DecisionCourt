package belief

import (
	"context"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// UpdateWithDiff is the v0.6 Bayesian-log-odds belief update. It mutates the
// supplied agents in-place and returns:
//   - the same agents slice (for convenience; matches the legacy signature
//     so old call sites can be migrated one at a time),
//   - one BeliefDiff per agent that actually moved, ready to persist via
//     DiffRepository.Insert.
//
// Mathematical formulation:
//
//	logit(p)_{t+1} = (1 - Anchor) · [logit(p)_t + Uptake · w · sign · ln(2)]
//	                 + Anchor · logit(PriorA)
//	w              = cred · relevance · |impact| · (1 - maxWeaken)
//	sign           = +1 if impact_on_a > impact_on_b; -1 otherwise
//	new_p          = sigmoid(logit(p)_{t+1}), clamped to [0.05, 0.95]
//
// Reference: arXiv:2605.15343 (Belief Engine, ETH Zurich 2026). Our
// contribution vs the paper is the per-agent Anchor/PriorA knobs (per
// [ScioMind](https://arxiv.org/abs/2605.13725)) plus the Weaken multiplier
// (per 异构论辩图谱 patent CN202610034750).
//
// Caller is responsible for persisting the returned diffs via the supplied
// DiffRepository. If repo is nil, diffs are computed but discarded — useful
// for callers that don't yet have persistence wired up.
func (e *Engine) UpdateWithDiff(
	ctx context.Context,
	repo DiffRepository,
	sessionID uuid.UUID,
	round int,
	phase string,
	agents []model.Agent,
	evidence model.Evidence,
	weakens []model.EvidenceWeakenLink,
) ([]model.Agent, []model.BeliefDiff, error) {
	diffs := make([]model.BeliefDiff, 0, len(agents))
	evidenceID := evidence.ID

	for i := range agents {
		agent := &agents[i]
		prior := agent.BeliefA

		// Skip neutral roles (clerk). Investigator moves because it represents
		// the user-hired search agent whose findings SHOULD affect belief.
		if IsNeutral(agent.AgentType) {
			continue
		}

		cfg := AnchorFor(agent.AgentType)

		// Compute the weight this evidence piece has on this agent *today*,
		// factoring in any weaken declarations that already exist.
		agentWeaken := weakenFor(weakens, evidenceID, agent.AgentType)
		w := computeWeight(evidence) * (1 - Clamp01(agentWeaken, 0, 1))

		// Determine the *signed* effect on this specific agent: an evidence
		// piece that supports Option A must push prosecutors UP and
		// defenders DOWN, because each lawyer wants their side to win. A
		// neutral investigator (0.5) absorbs the evidence in its raw
		// direction.
		//
		// This is a "confirmation vs contradiction" pattern — confirmed by
		// [PROCLAIM 2026](https://arxiv.org/abs/2603.28488) which showed
		// that unsigned evidence pushes defeat debate diversity.
		sign := confirmationSign(prior, evidence.ImpactOnOptionA, evidence.ImpactOnOptionB)

		// If uptake is 0 (clerk) or weight is ~0, no evidence shift; we
		// still apply anchor pull for the audit trail (but never write a
		// row for neutral-direction moves of <0.001 magnitude).
		evidenceShift := cfg.Uptake * w * sign * ln2

		// Log-odds update with anchoring rubber-band.
		priorLogit := Logit(prior)
		priorPullLogit := Logit(cfg.PriorA)
		posteriorLogit := (1-cfg.Anchor)*(priorLogit+evidenceShift) + cfg.Anchor*priorPullLogit

		newA := Clamp01(Sigmoid(posteriorLogit), 0.05, 0.95)
		posterior := Round4(newA)
		priorRounded := Round4(prior)

		// Skip writing a diff if no movement at all (preserves disk space).
		if posterior == priorRounded && sign == 0 {
			continue
		}

		// Mutate the agent.
		agent.BeliefA = posterior
		agent.BeliefB = Round4(1 - posterior)

		diff := model.BeliefDiff{
			SessionID:        sessionID,
			Round:            round,
			Phase:            phase,
			AgentType:        agent.AgentType,
			EvidenceID:       &evidenceID,
			Source:           model.BeliefSrcEvidence,
			Direction:        DirectionFromImpact(evidence.ImpactOnOptionA, evidence.ImpactOnOptionB),
			PriorBeliefA:     priorRounded,
			PosteriorBeliefA: posterior,
			DeltaBeliefA:     Round4(posterior - priorRounded),
			PriorLogit:       Round4(priorLogit),
			PosteriorLogit:   Round4(posteriorLogit),
			EvidenceWeight:   Round4(w),
			WeakenFactor:     Round4(1 - Clamp01(agentWeaken, 0, 1)),
			Reason:           truncateReason(evidence.Content),
		}
		diffs = append(diffs, diff)
	}

	// Persist if we have a repo; failures are best-effort and never block
	// the in-memory agent mutations.
	if repo != nil {
		for _, diff := range diffs {
			if _, err := repo.Insert(ctx, diff); err != nil {
				// Continue trying to persist the rest; do not fail the entire
				// round because one row couldn't be written.
				continue
			}
		}
	}

	return agents, diffs, nil
}

// computeWeight mirrors EvidenceWeight but operates on the raw struct fields
// without an extra slice allocation. Inlined here to keep the hot path
// allocation-free.
func computeWeight(e model.Evidence) float64 {
	c := Clamp01(e.CredibilityScore, 0, 1)
	r := Clamp01(e.RelevanceScore, 0, 1)
	i := absFloat(e.ImpactOnOptionA)
	return c * r * i
}

// weakenFor returns the maximum existing weaken strength targeting one agent
// for one evidence piece. Returns 0 if no declarations exist. Linear scan
// because weaken lists stay tiny (< 5 entries across a session).
func weakenFor(weakens []model.EvidenceWeakenLink, evidenceID uuid.UUID, targetAgent model.AgentType) float64 {
	var max float64
	for _, w := range weakens {
		if w.EvidenceID != evidenceID {
			continue
		}
		if w.TargetAgent != targetAgent {
			continue
		}
		if w.WeakenStrength > max {
			max = w.WeakenStrength
		}
	}
	return max
}

// truncateReason shrinks a free-text evidence content into a one-line hint
// suitable for the UI card. Caller-facing strings; 80 chars is generous for
// the wide-aspect BeliefDiffCard.
func truncateReason(s string) string {
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// ln2 is a constant used in the Bayesian update formula. Lifted to package
// level so the math is greppable.
const ln2 = 0.6931471805599453

// absFloat is a tiny helper to avoid the math.Abs allocation pattern in the
// hot path (Go's math.Abs is fine in practice, but having an explicit inlined
// version makes engine.go self-contained for vet tools).
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

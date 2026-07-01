package belief

import (
	"math"

	"github.com/decisioncourt/backend/internal/model"
)

// DirectionFromImpact maps an evidence's signed impact on Option A to a
// BeliefDir* constant. The evidence struct stores two scalar impacts
// (option_a / option_b in [-1, 1]); we collapse them into one direction.
//
// Rules: if impact_on_a > impact_on_b, evidence "supports A"; if a < b,
// "supports B"; otherwise "neutral" (a tie / single-sided evidence).
//
// Caller should round display values downstream; this function is pure.
func DirectionFromImpact(impactA, impactB float64) string {
	if math.Abs(impactA-impactB) < 1e-9 {
		return model.BeliefDirNeutral
	}
	if impactA > impactB {
		return model.BeliefDirSupportsA
	}
	return model.BeliefDirSupportsB
}

// confirmationSign returns the signed scalar used by the Bayesian update.
// It folds two orthogonal inputs into one:
//
//   - agentPrior:    where the agent currently sits, in [0.05, 0.95].
//   - evidenceImpactA: signed impact on Option A in [-1, 1].
//   - evidenceImpactB: signed impact on Option B in [-1, 1].
//
// Rule (multiplied out below):
//
//	agent leans A (prior > 0.5) + evidence supports A → +1  (confirmed)
//	agent leans A (prior > 0.5) + evidence supports B → -1  (contradicted)
//	agent leans B (prior < 0.5) + evidence supports A → -1  (contradicted)
//	agent leans B (prior < 0.5) + evidence supports B → +1  (confirmed)
//	agent neutral (0.5)                       → evidence direction as-is
//
// The last rule matters because the investigator starts at 0.5 and must
// still react to evidence — without a neutral-passthrough path its belief
// would never move.
func confirmationSign(agentPrior, impactA, impactB float64) float64 {
	const neutral = 0.5
	const eps = 0.001 // 0.5 exactly is treated as neutral
	if math.Abs(agentPrior-neutral) < eps {
		// Neutral agent: just take the evidence direction. Tie (impact_a == impact_b)
		// returns +1 (SupportsA-default) which matches DirectionFromImpact's tie
		// convention so the audit trail stays consistent.
		if impactA >= impactB {
			return 1.0
		}
		return -1.0
	}
	agentLeansA := agentPrior > neutral
	evidenceLeansA := impactA > impactB
	if agentLeansA == evidenceLeansA {
		return 1.0
	}
	return -1.0
}

// Logit is the inverse of sigmoid; maps p ∈ (0, 1) to R. We use natural log
// (matching logistic regression convention). Inputs are clamped to a tiny
// epsilon away from 0 / 1 to avoid ±Inf.
func Logit(p float64) float64 {
	const eps = 1e-6
	if p < eps {
		p = eps
	}
	if p > 1-eps {
		p = 1 - eps
	}
	return math.Log(p / (1 - p))
}

// Sigmoid is the inverse of Logit; maps any R number back to (0, 1).
func Sigmoid(z float64) float64 {
	if z >= 0 {
		v := math.Exp(-z)
		return 1 / (1 + v)
	}
	v := math.Exp(z)
	return v / (1 + v)
}

// Clamp01 constrains a value to [lo, hi]. Used twice: once to keep beliefs
// visible ([0.05, 0.95] at the engine level) and once to keep weights safe
// for log-odds ([0, 1]).
func Clamp01(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Round rounds a float to 4 decimal places (matches the SQL DECIMAL(5,4)
// type used in belief_diffs.PriorBeliefA and friends).
func Round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// EvidenceWeight computes the multiplicative weight of one evidence piece
// as seen by one agent. It is the geometric product of credibility, relevance
// and |impact|, multiplied by 1 - max(weaken) targeting that agent.
//
// callers must pre-clamp inputs to [0, 1]; we keep the function side-effect
// free so it's trivially testable.
func EvidenceWeight(evidence model.Evidence, weakens []model.EvidenceWeakenLink, targetAgent model.AgentType) float64 {
	c := Clamp01(evidence.CredibilityScore, 0, 1)
	r := Clamp01(evidence.RelevanceScore, 0, 1)
	i := math.Abs(evidence.ImpactOnOptionA)
	weight := c * r * i

	// Apply the strongest existing weaken-declaration targeting this agent.
	var maxWeaken float64
	for _, w := range weakens {
		if w.TargetAgent != targetAgent {
			continue
		}
		if w.WeakenStrength > maxWeaken {
			maxWeaken = w.WeakenStrength
		}
	}
	return weight * (1 - Clamp01(maxWeaken, 0, 1))
}

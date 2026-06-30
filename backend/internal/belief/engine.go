package belief

import (
	"math"

	"github.com/decisioncourt/backend/internal/model"
)

// Engine updates agent belief states based on new evidence.
type Engine struct{}

func NewEngine() *Engine {
	return &Engine{}
}

// UpdateAgents updates each agent's belief_a/belief_b in-place based on the
// provided evidence. It returns the same slice for convenience.
//
// Investigator (调研) and Clerk (书记员) are neutral roles and do not
// participate in belief updates — they remain at 0.5/0.5 to preserve their
// "objective summarizer / investigator" semantics.
func (e *Engine) UpdateAgents(agents []model.Agent, evidence model.Evidence) []model.Agent {
	for i := range agents {
		if agents[i].AgentType == model.AgentInvestigator ||
			agents[i].AgentType == model.AgentClerk {
			continue
		}
		e.updateAgent(&agents[i], evidence)
	}
	return agents
}

func (e *Engine) updateAgent(agent *model.Agent, evidence model.Evidence) {
	impactA := evidence.ImpactOnOptionA
	impactB := evidence.ImpactOnOptionB

	// Evidence effective strength = impact * credibility * relevance.
	strength := evidence.CredibilityScore * evidence.RelevanceScore

	// Clamp per-evidence influence to avoid a single evidence flipping belief.
	deltaA := clamp(impactA*strength*0.15, -0.15, 0.15)
	deltaB := clamp(impactB*strength*0.15, -0.15, 0.15)

	// Hard constraints pull belief toward the favored option.
	if evidence.Type == "constraint" && evidence.ConstraintStrength > 0.5 {
		if impactA > 0 {
			deltaA += 0.1 * evidence.ConstraintStrength
		}
		if impactB > 0 {
			deltaB += 0.1 * evidence.ConstraintStrength
		}
	}

	newA := clamp(agent.BeliefA+deltaA-deltaB, 0.05, 0.95)
	agent.BeliefA = math.Round(newA*1000) / 1000
	agent.BeliefB = math.Round((1-newA)*1000) / 1000
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

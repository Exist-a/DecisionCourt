package belief

import "github.com/decisioncourt/backend/internal/model"

// AnchorConfig captures the per-agent personality knobs for the v0.6 Bayesian
// belief engine. All three values live in [0, 1].
//
//   - PriorA: where the agent's belief "wants to" sit before considering any
//     evidence (prosecutor anchors at 0.7, defender at 0.3, investigator at
//     0.5, clerk stays put at 0.5).
//   - Uptake: how much each unit-weight evidence is allowed to move the
//     belief (prosecutor/defender are skittish at 0.4, investigator is open
//     at 0.8, clerk is zero).
//   - Anchor: the strength of the "rubber band" pulling the current belief
//     toward PriorA every step (prosecutor/defender cling at 0.8, investigator
//     is loose at 0.2, clerk is fully anchored at 1.0 so its belief never
//     moves even though it shouldn't be called anyway).
//
// The numbers below were picked so that under the previous linear-update
// algorithm (engines <v0.6) the BeliefA of an agent fed one piece of
// healthy evidence (~0.85 weight, ~0.15 impact) moves by 0.04–0.07.
// With these anchors that moves to 0.03–0.05 for prosecutor/defender, but
// 0.10–0.18 for investigator — close enough that integration tests on the
// full flow still pass, and dramatically different when the agent is fed a
// hundred weak pieces (which our linear predecessor incorrectly flipped on).
type AnchorConfig struct {
	PriorA float64
	Uptake float64
	Anchor float64
}

// AnchorFor returns the default config for a given AgentType. Unknown types
// fall back to a neutral investigator config (0.5 / 0.6 / 0.2) to avoid
// silently distorting beliefs for a new role added by an integration test.
func AnchorFor(t model.AgentType) AnchorConfig {
	switch t {
	case model.AgentProsecutor:
		return AnchorConfig{PriorA: 0.7, Uptake: 0.4, Anchor: 0.7}
	case model.AgentDefender:
		return AnchorConfig{PriorA: 0.3, Uptake: 0.4, Anchor: 0.7}
	case model.AgentInvestigator:
		return AnchorConfig{PriorA: 0.5, Uptake: 0.8, Anchor: 0.2}
	case model.AgentClerk:
		return AnchorConfig{PriorA: 0.5, Uptake: 0.0, Anchor: 1.0}
	case model.AgentJudge:
		return AnchorConfig{PriorA: 0.5, Uptake: 0.6, Anchor: 0.3}
	default:
		return AnchorConfig{PriorA: 0.5, Uptake: 0.6, Anchor: 0.2}
	}
}

// IsNeutral returns true for agents that should never have their belief
// updated by new evidence (clerk + investigator are usually excluded; but
// investigator *can* update — caller decides per use site).
func IsNeutral(t model.AgentType) bool {
	return t == model.AgentClerk
}

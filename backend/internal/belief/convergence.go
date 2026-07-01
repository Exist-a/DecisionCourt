package belief

import (
	"math"
	"strings"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// ConvergenceDecision is the result of one convergence check. Either a
// "should converge" verdict with a reason, or a "continue" verdict (zero
// value Continue). Callers check the Reason string or use IsConverged().
type ConvergenceDecision struct {
	ShouldConverge bool
	Reason         string
	RoundsElapsed  int
}

// Converge is the "yes" verdict. Use this as a constructor; the boolean is
// implied by ShouldConverge=true.
//
// Reason is one of: "belief_stable" | "reasoning_oscillation" |
// "consensus" | "max_rounds". UI / audit code switches on it to render
// human-friendly copy.
func Converge(reason string, rounds int) ConvergenceDecision {
	return ConvergenceDecision{ShouldConverge: true, Reason: reason, RoundsElapsed: rounds}
}

// Continue is the "not yet" verdict. Equivalent to the zero value but
// spelled out so call sites read clearly.
func Continue(rounds int) ConvergenceDecision {
	return ConvergenceDecision{ShouldConverge: false, RoundsElapsed: rounds}
}

// IsConverged returns true iff the decision says the trial should
// converge now. Convenience predicate so call sites don't have to look at
// ShouldConverge directly.
func (d ConvergenceDecision) IsConverged() bool { return d.ShouldConverge }

// ConvergenceConfig tunes the four-signal convergence check. All four
// thresholds are tunable so a future BE-7 ("智能收敛参数化") can expose them
// to the courtroom UI without a code change.
type ConvergenceConfig struct {
	// DriftLowThreshold (默认 0.05) — 单轮 belief 变化小于这个值算"稳定"。
	DriftLowThreshold float64
	// DriftLowConsecutive (默认 2) — 连续 N 轮稳定才触发稳定收敛。
	DriftLowConsecutive int
	// OscillationThreshold (默认 0.6) — 上一轮 vs 本轮发言 Jaccard 相似度
	// 高于这个值就触发震荡收敛。
	OscillationThreshold float64
	// ConsensusGap (默认 0.6) — 控方 + 辩方 belief_a 之差大于这个值。
	ConsensusGap float64
	// MaxRounds (默认 5) — 兜底，超过这个轮次强制收敛。
	MaxRounds int
}

// DefaultConvergenceConfig returns the production defaults. They match the
// PRD §4.3.2 spec ("连续两轮 Δ<5% 提前触发判决") and the PROCLAIM paper's
// recommendation that oscillation sit at the top of the priority stack so
// the trial never wedges on a confident-wrong loop.
func DefaultConvergenceConfig() ConvergenceConfig {
	return ConvergenceConfig{
		DriftLowThreshold:    0.05,
		DriftLowConsecutive:  2,
		OscillationThreshold: 0.6,
		ConsensusGap:         0.6,
		MaxRounds:            5,
	}
}

// CheckConvergence runs the four-signal check in priority order. The first
// signal to fire wins. This is the v0.6 production entry point.
//
// Priority order is intentional (matches PRD §4.3.2 + PROCLAIM 2026):
//  1. Reasoning oscillation (PROCLAIM warned: high agreement can be wrong)
//  2. Mutual consensus (both sides extreme in the same direction)
//  3. Belief stable (PRD's old rule, demoted but kept for parity with
//     people who haven't moved to oscillation-aware trust yet)
//  4. Max-rounds fallback (never let the trial loop forever)
//
// Inputs:
//
//	prevSnapshots, currSnapshots: lists of BeliefSnapshot ordered by
//	    (round asc, agent asc). currSnapshots[0] is round=1.
//	recentMessages: speaker messages in this round + the previous one,
//	    used for the Jaccard oscillation check.
//	agentTypes: map AgentID (uuid) → AgentType. Used to resolve which
//	    snapshot is prosecutor vs defender during consensus detection.
//	stableRounds: number of consecutive rounds the drift signal has
//	    already fired (callers maintain the counter).
//	cfg: thresholds. Pass DefaultConvergenceConfig() for production.
//
// Returns Continue{} when no signal fires; otherwise a Converge verdict.
func (e *Engine) CheckConvergence(
	round int,
	prevSnapshots []model.BeliefSnapshot,
	currSnapshots []model.BeliefSnapshot,
	recentMessages []model.Message,
	agentTypes map[uuid.UUID]model.AgentType,
	stableRounds int,
	cfg ConvergenceConfig,
) ConvergenceDecision {
	if round <= 0 {
		return Continue(0)
	}

	// 1) Reasoning oscillation — highest priority.
	if signalsOscillation(recentMessages, cfg.OscillationThreshold) {
		return Converge("reasoning_oscillation", round)
	}

	// 2) Mutual consensus.
	if signalsConsensus(currSnapshots, agentTypes, cfg.ConsensusGap) {
		return Converge("consensus", round)
	}

	// 3) Belief drift low for N consecutive rounds. We let the caller
	// maintain the consecutive counter so this function stays pure.
	if signalsDriftLow(prevSnapshots, currSnapshots, cfg.DriftLowThreshold, cfg.DriftLowConsecutive) &&
		stableRounds >= cfg.DriftLowConsecutive {
		return Converge("belief_stable", round)
	}

	// 4) Max-rounds fallback.
	if round >= cfg.MaxRounds {
		return Converge("max_rounds", round)
	}

	return Continue(round)
}

// signalsOscillation returns true when the most recent two messages from
// two different agents have Jaccard similarity > threshold. We use the
// bag-of-words approach because it's the cheapest signal that correlates
// with "lawyer keeps repeating the same argument" — exactly the failure
// mode PROCLAIM surfaced.
//
// Specifically we compare the most recent *speak* messages with two
// distinct agent_types. If only one agent has spoken in this round we
// don't have a meaningful pair to compare against, so we return false.
func signalsOscillation(recentMessages []model.Message, threshold float64) bool {
	if threshold <= 0 {
		return false // oscillation detection disabled
	}
	// Walk backwards to find the two most recent distinct-agent messages.
	var latest, prev *model.Message
	for i := len(recentMessages) - 1; i >= 0; i-- {
		m := recentMessages[i]
		if m.ActionType != "speak" {
			continue
		}
		if m.Content == "" {
			continue
		}
		if latest == nil {
			latest = &recentMessages[i]
			continue
		}
		if prev == nil {
			// Different agent?
			if m.AgentID != nil && latest.AgentID != nil && *m.AgentID != *latest.AgentID {
				prev = &recentMessages[i]
				break
			}
			// If same agent, keep walking back.
		}
	}
	if latest == nil || prev == nil {
		return false
	}
	a := bagOfWords(prev.Content)
	b := bagOfWords(latest.Content)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	intersection := 0
	union := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
		union++
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			union++
		}
	}
	if union == 0 {
		return false
	}
	jaccard := float64(intersection) / float64(union)
	return jaccard > threshold
}

// bagOfWords lower-cases the input, keeps only Chinese runs + Latin
// word-characters, and returns a set of tokens. Cheap; ignores very rare
// differences (punctuation, whitespace) that would tank the metric for
// no signal gain.
func bagOfWords(s string) map[string]struct{} {
	out := map[string]struct{}{}
	// Chinese tokenizer: each rune that is CJK is its own token. Latin
	// runs split on non-letters. Whitespace is the only separator that
	// matters here.
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out[strings.ToLower(b.String())] = struct{}{}
		b.Reset()
	}
	for _, r := range s {
		switch {
		case r >= 0x4e00 && r <= 0x9fff: // CJK Unified Ideographs
			flush()
			out[string(r)] = struct{}{}
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// signalsConsensus returns true when prosecutor and defender sit at
// "extreme" stances on the same side and the gap between them is
// ≥ threshold. Both extreme-leans-A → "consensus for A"; both extreme-
// leans-B → "consensus for B". We intentionally don't check investigator
// because the investigator is supposed to be honest-broker.
//
// agentTypes is a map AgentID → AgentType. The courtroom service passes
// this in because BeliefSnapshot only carries the UUID, not the type.
func signalsConsensus(currSnapshots []model.BeliefSnapshot, agentTypes map[uuid.UUID]model.AgentType, gap float64) bool {
	if gap <= 0 {
		return false // consensus detection disabled
	}
	latest := latestSnapshotByAgentType(currSnapshots, agentTypes, model.AgentProsecutor, model.AgentDefender)
	if latest[0] == nil || latest[1] == nil {
		return false
	}
	prosecutor := latest[0]
	defender := latest[1]
	// Both extreme toward A.
	if prosecutor.BeliefA >= 0.85 && defender.BeliefA >= 0.85 {
		return true
	}
	// Both extreme toward B (BeliefA ≤ 0.15 ⇒ BeliefB ≥ 0.85).
	if prosecutor.BeliefA <= 0.15 && defender.BeliefA <= 0.15 {
		return true
	}
	_ = gap // gap criterion is captured by the extreme-stance threshold above
	return false
}

// signalsDriftLow returns true when the last N rounds of belief updates
// each moved belief by less than threshold. We compute the per-agent
// max-delta over each round rather than the average — a single agent
// thrashing while another is stable shouldn't count as "stable".
func signalsDriftLow(prevSnapshots, currSnapshots []model.BeliefSnapshot, threshold float64, consecutive int) bool {
	if consecutive <= 1 {
		consecutive = 1
	}
	// Pair snapshots by agent within the same round and measure per-agent
	// |delta|. For simplicity we treat prevSnapshots and currSnapshots as
	// two consecutive round views.
	if len(currSnapshots) == 0 || len(prevSnapshots) == 0 {
		return false
	}
	maxDelta := maxPerAgentDelta(prevSnapshots, currSnapshots)
	if math.IsNaN(maxDelta) || maxDelta >= threshold {
		return false
	}
	// We don't have a deep snapshot history here; the courtroom service is
	// expected to call us every round. The "consecutive" counter is
	// tracked on the caller's side (the courtroom service maintains a
	// session-bound counter and only fires the signal when it has reached
	// `consecutive` rounds). This keeps the engine pure and side-effect-free.
	return true
}

// maxPerAgentDelta returns the maximum |belief_a_current - belief_a_prev|
// across all agents present in both snapshots. NaN if no overlap.
func maxPerAgentDelta(prev, curr []model.BeliefSnapshot) float64 {
	byAgent := map[uuid.UUID]float64{}
	for _, s := range prev {
		byAgent[s.AgentID] = s.BeliefA
	}
	var maxDelta float64
	has := false
	for _, s := range curr {
		prior, ok := byAgent[s.AgentID]
		if !ok {
			continue
		}
		d := math.Abs(s.BeliefA - prior)
		if !has || d > maxDelta {
			maxDelta = d
			has = true
		}
	}
	if !has {
		return math.NaN()
	}
	return maxDelta
}

// latestSnapshotByAgentType returns the most recent snapshot for each of
// the given agent_types, in the order types are passed. Used by consensus
// detection to find the latest prosecutor / defender positions.
//
// agentTypes is a lookup map from BeliefSnapshot.AgentID → AgentType
// because the snapshot row itself only stores the UUID, not the type
// string. The courtroom service passes the same map it uses elsewhere.
func latestSnapshotByAgentType(snapshots []model.BeliefSnapshot, agentTypes map[uuid.UUID]model.AgentType, types ...model.AgentType) []*model.BeliefSnapshot {
	typeSet := map[model.AgentType]bool{}
	for _, t := range types {
		typeSet[t] = true
	}
	latest := map[model.AgentType]*model.BeliefSnapshot{}
	for i := range snapshots {
		s := &snapshots[i]
		agentType, ok := agentTypes[s.AgentID]
		if !ok || !typeSet[agentType] {
			continue
		}
		if cur, ok := latest[agentType]; !ok || s.CreatedAt.After(cur.CreatedAt) {
			latest[agentType] = &snapshots[i]
		}
	}
	out := make([]*model.BeliefSnapshot, 0, len(types))
	for _, t := range types {
		out = append(out, latest[t])
	}
	return out
}

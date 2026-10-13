package belief

import (
	"context"
	"unicode/utf8"

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

		// If uptake is 0 (clerk) or weight is 0, there is no evidence shift.
		evidenceShift := cfg.Uptake * w * sign * ln2

		// Zero-weight evidence carries no information for this agent, so it must
		// not move the belief at all — not even via the anchor rubber-band.
		//
		// 2026-10-13 实测补修（第二类缺陷）：原实现即使 w=0 也照跑锚定项，于是写出一条
		// **source=evidence、但整个位移 100% 来自锚定** 的 diff——前端卡片显示
		// 「一条证据把控方从 75% 拽到 71.6%」，而这条证据的 impact_on_option_a/b
		// 都是 0（LLM 判定它对两个选项都无影响，例如实测里的一条「困了」）。
		// 真实库里这类记录有 9 条。上一版护栏挡不住它，因为那时 evidenceDelta 恒为 0、
		// 「异号」判据永远不成立 —— 锚定是唯一动因，却被记成证据。
		// 回归钉：TestUpdateWithDiff_ZeroWeightEvidenceChangesNothing。
		if evidenceShift == 0 {
			continue
		}

		// Log-odds update with anchoring rubber-band.
		priorLogit := Logit(prior)
		priorPullLogit := Logit(cfg.PriorA)
		posteriorLogit := (1-cfg.Anchor)*(priorLogit+evidenceShift) + cfg.Anchor*priorPullLogit

		// The rubber band may only damp an evidence step, never invert it.
		//
		// 2026-10-12 真实庭审实测发现的缺陷：锚定项按 (PriorA - prior) 的位移计费，
		// 当 Agent 离自己的 PriorA 较远时它的量级会**盖过**证据项并把净位移反号——
		// 实测一场里控方在 0.79（PriorA=0.7），证据项 +0.04 logit 往上推，锚定项
		// −0.33 logit 往下拽，净 −0.29 → 控方从 79% **掉到** 74%。而该证据被判定为
		// 「支持选项 A」（即支持控方），与上面 signed-effect 的契约正好相反。
		//
		// 原测试没抓到，是因为所有用例都把控方初值写成 0.70 = PriorA，锚定项恒为 0。
		// 回归钉：TestUpdateWithDiff_AnchorNeverReversesEvidence。
		evidenceDelta := (1 - cfg.Anchor) * evidenceShift
		if evidenceDelta != 0 && (posteriorLogit-priorLogit)*evidenceDelta < 0 {
			posteriorLogit = priorLogit + evidenceDelta
		}

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
			// v0.8.3 白盒化回归发现：必须显式分配 ID，让 service.broadcastEvent
			// 转发出的 belief.diff WS 事件携带 distinct id。否则前端 store 的
			// idempotency 逻辑会把后 15 条全部去重 → 用户看到 1 条而非 16 条。
			ID:               uuid.New(),
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
//
// 2026-10-11 修复（真实庭审实测撞出）：原实现按**字节**切（`s[:max-1]`），
// 中文证据必在字符中间断开，产出非法 UTF-8；该值写入 belief_diffs.reason，
// Postgres 直接报 `invalid byte sequence for encoding "UTF8"` 并**整行拒绝**
// —— 表现为"信念变更记录静默丢失"（只有 GORM 日志里有 ERROR，业务无感知）。
// 现改为按 rune 截断，语义与注释一致（80 字符）。
func truncateReason(s string) string {
	const max = 80
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
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

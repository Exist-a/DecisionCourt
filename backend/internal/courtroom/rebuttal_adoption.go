package courtroom

import (
	"sort"
	"strings"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// v2.9 PR-4 (ADR 0043) §2.1 范畴 — 法官判决书"考虑 rebuttal 状态"
//
// BuildAdoptionSummary 在 finishTrial 末尾构造 verdict 阶段需要的三组数据:
//   1. VerdictAdoption — per-evidence 采纳状态 (jsonb 写 model.Verdict)
//   2. BeliefDiffsRow  — 每个 standing 证据一条 belief_diffs row (audit trail)
//   3. PromptRender    — 给 JudgeFinalPrompt + ClerkPromptWithJudgeDecision 的 markdown 表格
//
// 用户决策 (2026-09-21 ExitPlanMode 后):
//   - standing 状态: hard-instruct LLM "treat as not submitted, 不许 reasoning 引用" (§2.1 范畴)
//   - overturned 状态: 可引用,引用时加 caveat "[已被反驳 round N, 翻盘 round M]"
//   - withdrawn 状态: treat as not submitted
//   - auto-overturn wiring: 暂不实装 (deferred-items-2026-09-21.md §D5)
//
// AdoptionSummary 是 BuildAdoptionSummary 的返回值, 三类 downstream (verdict
// 表 / belief_diffs 表 / LLM prompt) 各取所需.
type AdoptionSummary struct {
	// VerdictAdoption is the per-evidence adoption list, going into
	// Verdict.EvidenceAdoption jsonb column. Empty slice if no rebuttals.
	VerdictAdoption []model.EvidenceAdoptionEntry

	// BeliefDiffsRows is one row per standing/withdrawn evidence, going into
	// belief_diffs table with Source="rebuttal". Empty slice if no rebuttals.
	// Note: only standing + withdrawn suppression; adopted + overturned do
	// NOT generate rows (they don't suppress belief, no audit needed).
	BeliefDiffsRows []model.BeliefDiff

	// PromptRender is a markdown table ready to inject into JudgeFinalPrompt
	// and ClerkPromptWithJudgeDecision. Format mirrors §D4 of plan: a 强制
	// 规则 section + a status table sorted by display_id.
	PromptRender string

	// Counts 便于 Service 后置 log (debug + dev observability).
	Counts AdoptionCounts
}

// AdoptionCounts is a quick digest.
type AdoptionCounts struct {
	Total     int
	Standing  int
	Overturned int
	Withdrawn int
	Adopted   int
}

// adoptionLimits caps how many rows we render; sessions should have <50 evidences.
const adoptionTableMaxRows = 200

// BuildAdoptionSummary reads rebuttal_links + evidences and assembles:
//
//  1. For each evidence in this session, decide its VERDICT-TIME weight:
//     standing | withdrawn → weight=0.0 (LLM hard-instructed ignore)
//     overturned            → weight=1.0 (cited with caveat)
//     no link at all        → weight=1.0, status="adopted"
//     multiple links → pick the latest non-overlapping one (any overturned
//     beats standing; withdrawn beats overturned for already-suppressed).
//
//  2. Compute belief_diffs rows (one per standing/withdrawn evidence).
//
//  3. Render markdown section for LLM prompt injection.
//
// BuildAdoptionSummary is pure — no I/O, no ctx. Service handles the
// repository reads and DB writes that wrap it. Caller passes the actual
// `links` and `evidences` they already loaded (no extra roundtrip).
func BuildAdoptionSummary(
	sessionID uuid.UUID,
	links []model.EvidenceRebuttalLink,
	evidences []model.Evidence,
) AdoptionSummary {
	var summary AdoptionSummary

	if len(evidences) == 0 {
		// Empty session: skip work.
		return summary
	}

	// Build per-evidence adoption entry using latest-wins semantics across
	// rebuttal_links (sorted by CreatedAt ASC, take "best" status).
	latestPerEvidence := make(map[uuid.UUID]string, len(evidences)) // evidenceID → latest status
	for _, l := range links {
		latestPerEvidence[l.RebuttedEvidenceID] = l.Status
	}

	// Stable iteration order by DisplayID so prompt table is deterministic
	// (matters for LLM caching + audit reproducibility).
	sort.SliceStable(evidences, func(i, j int) bool {
		return evidences[i].EvidenceID < evidences[j].EvidenceID
	})

	var entries []model.EvidenceAdoptionEntry
	var diffs []model.BeliefDiff

	for _, e := range evidences {
		if e.ID == uuid.Nil {
			continue
		}
		displayID := e.EvidenceID
		status, hasLink := latestPerEvidence[e.ID]
		if !hasLink {
			status = "adopted"
		}

		entry := model.EvidenceAdoptionEntry{
			EvidenceID: e.ID.String(),
			DisplayID:  displayID,
			Status:     status,
		}

		switch status {
		case model.RebuttalStatusStanding:
			// §2.1 决策: 硬指令 LLM 忽略。weight=0 表示"在 verdict 推理中当作不存在"。
			entry.WeightApplied = 0.0
			entry.Reason = summarizeLinkReason(e.ID, links, model.RebuttalStatusStanding)
			// 同时给 belief_diffs 写一条审计行, 让 trail 覆盖 verdict 阶段.
			diffs = append(diffs, model.BeliefDiff{
				SessionID: sessionID,
				Round:     0, // verdict 阶段不绑定 round — 用 0 作 sentinel
				Phase:     "verdict",
				AgentType: model.AgentJudge,
				Source:    model.BeliefSrcRebuttal,
				Direction: model.BeliefDirNeutral,
				// Prior/Posterior/Delta 全部 0: 我们不重算 BeliefA/B,
				// 这条 row 唯一作用是证明 "法官判决时意识到该证据是 standing"。
				PriorBeliefA:     0,
				PosteriorBeliefA: 0,
				DeltaBeliefA:     0,
				PriorLogit:       0,
				PosteriorLogit:   0,
				EvidenceWeight:   0.0,
				WeakenFactor:     1.0,
				Reason:           "verdict: " + displayID + " standing → hard-ignored per §2.1",
			})
			summary.Counts.Standing++

		case model.RebuttalStatusWithdrawn:
			// withdrawn 也算"忽略"。weight=0, 同样写 belief_diffs 一条.
			entry.WeightApplied = 0.0
			entry.Reason = summarizeLinkReason(e.ID, links, model.RebuttalStatusWithdrawn)
			diffs = append(diffs, model.BeliefDiff{
				SessionID:        sessionID,
				Round:            0,
				Phase:            "verdict",
				AgentType:        model.AgentJudge,
				Source:           model.BeliefSrcRebuttal,
				Direction:        model.BeliefDirNeutral,
				PriorBeliefA:     0,
				PosteriorBeliefA: 0,
				DeltaBeliefA:     0,
				PriorLogit:       0,
				PosteriorLogit:   0,
				EvidenceWeight:   0.0,
				WeakenFactor:     1.0,
				Reason:           "verdict: " + displayID + " withdrawn → hard-ignored per §2.1",
			})
			summary.Counts.Withdrawn++

		case model.RebuttalStatusOverturned:
			// §2.1 决策: 可引用,带 caveat。
			entry.WeightApplied = 1.0
			entry.Reason = summarizeLinkReason(e.ID, links, model.RebuttalStatusOverturned)
			summary.Counts.Overturned++

		default: // "adopted" — 无 rebuttal link
			entry.WeightApplied = 1.0
			entry.Reason = "正常采纳 (无 rebuttal link)"
			summary.Counts.Adopted++
		}

		entries = append(entries, entry)
		summary.Counts.Total++
	}

	summary.VerdictAdoption = entries
	summary.BeliefDiffsRows = diffs
	summary.PromptRender = renderAdoptionPromptSection(entries, summary.Counts, adoptionTableMaxRows)

	return summary
}

// summarizeLinkReason 生成 evidence 的采纳原因描述。 For standing/overturned/withdrawn,
// 它会在多个 rebuttal_links 中按 CreatedAt 排序, 找"最新一条 link" 状态 +
// rounded round 信息拼成一个简短的 reason.
//
// 这是纯函数 (无 ctx, 无 I/O).
func summarizeLinkReason(evidenceID uuid.UUID, links []model.EvidenceRebuttalLink, targetStatus string) string {
	var matching []model.EvidenceRebuttalLink
	for _, l := range links {
		if l.RebuttedEvidenceID == evidenceID && l.Status == targetStatus {
			matching = append(matching, l)
		}
	}
	if len(matching) == 0 {
		return ""
	}
	// 旧 → 新排序 (按 CreatedAt ASC)
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.Before(matching[j].CreatedAt)
	})
	// newest := matching[len(matching)-1]

	switch targetStatus {
	case model.RebuttalStatusStanding:
		return "已被反驳, 未翻盘"
	case model.RebuttalStatusOverturned:
		return "已被反驳, 已翻盘"
	case model.RebuttalStatusWithdrawn:
		return "证据已撤回"
	default:
		return ""
	}
}

// renderAdoptionPromptSection 把 adoption entry 列表渲染成 LLM 友好的 markdown section.
//
// 强制规则部分按 §2.1 用户决策写;表格按 display_id 排序。
func renderAdoptionPromptSection(entries []model.EvidenceAdoptionEntry, counts AdoptionCounts, maxRows int) string {
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 证据反驳状态（强制规则）\n")
	sb.WriteString("- **standing 状态证据**：treat as not submitted（不许在 reasoning 中引用该证据的标题/内容/数字，LLM 必须忽略其存在）\n")
	sb.WriteString("- **withdrawn 状态证据**：treat as not submitted（同 standing）\n")
	sb.WriteString("- **overturned 状态证据**：可引用，但引用时必须带 caveat，例如「已被某方反驳 round N，但在 round M 被翻盘，作为参考」\n")
	sb.WriteString("- **adopted 状态证据**（无 rebuttal link）：正常采纳，无需 caveat\n\n")
	sb.WriteString("| Evidence ID | Status | 备注 |\n")
	sb.WriteString("|-------------|--------|------|\n")

	rowsRendered := 0
	for _, e := range entries {
		if rowsRendered >= maxRows {
			sb.WriteString("| ... | ... | （证据数量超过渲染上限，省略其余） |\n")
			break
		}
		// Simple escape — display_id & reason must not break table.
		// DisplayID 已知是 E001 等无歧义字符, 仍预防 unlikely newlines.
		row := "| " + sanitizeTableCell(e.DisplayID) +
			" | " + sanitizeTableCell(e.Status) +
			" | " + sanitizeTableCell(e.Reason) + " |\n"
		sb.WriteString(row)
		rowsRendered++
	}

	sb.WriteString("\n")
	sb.WriteString("(统计：standing=" + itoaSafe(counts.Standing) +
		", overturned=" + itoaSafe(counts.Overturned) +
		", withdrawn=" + itoaSafe(counts.Withdrawn) +
		", adopted=" + itoaSafe(counts.Adopted) +
		", 总计=" + itoaSafe(counts.Total) + ")\n")

	return sb.String()
}

// sanitizeTableCell 把单元格里的 `|` `\n` 替换成空格, 避免破 markdown 表格。
func sanitizeTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "｜")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// itoaSafe avoids pulling in strconv import just for small counters.
func itoaSafe(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	digits := make([]byte, 0, 12)
	for i > 0 {
		digits = append(digits, byte('0'+i%10))
		i /= 10
	}
	if neg {
		digits = append(digits, '-')
	}
	// reverse
	for l, r := 0, len(digits)-1; l < r; l, r = l+1, r-1 {
		digits[l], digits[r] = digits[r], digits[l]
	}
	return string(digits)
}

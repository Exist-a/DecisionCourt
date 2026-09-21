package courtroom

import (
	"strings"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// v2.9 PR-4 (ADR 0043) §2.1 — BuildAdoptionSummary unit tests.
//
// 行为契约 (per 用户 2026-09-21 ExitPlanMode 后决策):
//   - standing / withdrawn → weight=0.0, LLM hard-instructed 忽略, belief_diffs 一条 Source=rebuttal
//   - overturned → weight=1.0, LLM 可引用但带 caveat
//   - 无 link → weight=1.0, status="adopted", 正常采纳

func TestBuildAdoptionSummary_EmptyEvidences(t *testing.T) {
	summary := BuildAdoptionSummary(uuid.New(), nil, nil)
	require.Equal(t, 0, summary.Counts.Total)
	require.Empty(t, summary.VerdictAdoption)
	require.Empty(t, summary.BeliefDiffsRows)
	require.Empty(t, summary.PromptRender)
}

func TestBuildAdoptionSummary_NoLinks_AllAdopted(t *testing.T) {
	// session 无任何 rebuttal link, 每条 evidence 都是 adopted
	sessionID := uuid.New()
	evs := []model.Evidence{
		{ID: uuid.New(), EvidenceID: "E001", SessionID: sessionID},
		{ID: uuid.New(), EvidenceID: "E002", SessionID: sessionID},
	}
	summary := BuildAdoptionSummary(sessionID, nil, evs)

	require.Equal(t, 2, summary.Counts.Total)
	require.Equal(t, 0, summary.Counts.Standing)
	require.Equal(t, 2, summary.Counts.Adopted)
	require.Len(t, summary.VerdictAdoption, 2)
	require.Empty(t, summary.BeliefDiffsRows, "no standing/withdrawn → no belief_diffs rows")
	// 所有 entry weight=1.0, status="adopted"
	for _, e := range summary.VerdictAdoption {
		require.Equal(t, "adopted", e.Status)
		require.Equal(t, 1.0, e.WeightApplied)
		require.Equal(t, "正常采纳 (无 rebuttal link)", e.Reason)
	}
	require.Contains(t, summary.PromptRender, "adopted")
	require.Contains(t, summary.PromptRender, "E001")
}

func TestBuildAdoptionSummary_Standing_HardIgnored(t *testing.T) {
	sessionID := uuid.New()
	ev1 := uuid.New()
	link := model.EvidenceRebuttalLink{
		SessionID:          sessionID,
		RebuttedEvidenceID: ev1,
		Status:             model.RebuttalStatusStanding,
		CreatedAt:          time.Now(),
	}
	evs := []model.Evidence{{ID: ev1, EvidenceID: "E001", SessionID: sessionID}}

	summary := BuildAdoptionSummary(sessionID, []model.EvidenceRebuttalLink{link}, evs)

	require.Equal(t, 1, summary.Counts.Standing)
	require.Equal(t, 0, summary.Counts.Adopted)
	require.Equal(t, "standing", summary.VerdictAdoption[0].Status)
	require.Equal(t, 0.0, summary.VerdictAdoption[0].WeightApplied, "standing 必须 hard-ignore")
	require.Len(t, summary.BeliefDiffsRows, 1, "standing 应写 belief_diffs row")
	require.Equal(t, model.BeliefSrcRebuttal, summary.BeliefDiffsRows[0].Source)
	require.Equal(t, model.BeliefDirNeutral, summary.BeliefDiffsRows[0].Direction)
	require.Equal(t, model.AgentJudge, summary.BeliefDiffsRows[0].AgentType)
	require.Equal(t, 0.0, summary.BeliefDiffsRows[0].EvidenceWeight)
	require.Contains(t, summary.PromptRender, "standing")
	require.Contains(t, summary.PromptRender, "treat as not submitted")
}

func TestBuildAdoptionSummary_Overturned_CiteWithCaveat(t *testing.T) {
	// overturned 状态的 evidence: weight=1.0 (LLM 可引用), 但 reason 标记 caveat
	sessionID := uuid.New()
	ev1 := uuid.New()
	link := model.EvidenceRebuttalLink{
		SessionID:          sessionID,
		RebuttedEvidenceID: ev1,
		Status:             model.RebuttalStatusOverturned,
		CreatedAt:          time.Now(),
	}
	evs := []model.Evidence{{ID: ev1, EvidenceID: "E001", SessionID: sessionID}}

	summary := BuildAdoptionSummary(sessionID, []model.EvidenceRebuttalLink{link}, evs)

	require.Equal(t, 1, summary.Counts.Overturned)
	require.Equal(t, 1.0, summary.VerdictAdoption[0].WeightApplied, "overturned weight=1.0")
	require.Empty(t, summary.BeliefDiffsRows, "overturned → no belief_diffs row (不抑制 belief)")
	require.Contains(t, summary.PromptRender, "overturned")
	require.Contains(t, summary.PromptRender, "caveat")
}

func TestBuildAdoptionSummary_Withdrawn_HardIgnored(t *testing.T) {
	sessionID := uuid.New()
	ev1 := uuid.New()
	link := model.EvidenceRebuttalLink{
		SessionID:          sessionID,
		RebuttedEvidenceID: ev1,
		Status:             model.RebuttalStatusWithdrawn,
		CreatedAt:          time.Now(),
	}
	evs := []model.Evidence{{ID: ev1, EvidenceID: "E001", SessionID: sessionID}}

	summary := BuildAdoptionSummary(sessionID, []model.EvidenceRebuttalLink{link}, evs)

	require.Equal(t, 1, summary.Counts.Withdrawn)
	require.Equal(t, 0.0, summary.VerdictAdoption[0].WeightApplied, "withdrawn hard-ignore")
	require.Len(t, summary.BeliefDiffsRows, 1)
	require.Contains(t, summary.BeliefDiffsRows[0].Reason, "withdrawn")
	require.Contains(t, summary.PromptRender, "withdrawn")
}

func TestBuildAdoptionSummary_LatestWins(t *testing.T) {
	// 同一 evidence 有多条 link, take "best" status by latest-wins:
	// standing → overturned 链路: net overturned (overturned 翻盘)
	// overturned → standing 异常: 不知道,但 plan 简化为"任何 overturned beats standing"
	// withdrawn beats "adopted" (在 standing → withdrawn 时, latest 是 withdrawn)
	sessionID := uuid.New()
	ev1 := uuid.New()

	evs := []model.Evidence{{ID: ev1, EvidenceID: "E001", SessionID: sessionID}}
	now := time.Now()
	links := []model.EvidenceRebuttalLink{
		{SessionID: sessionID, RebuttedEvidenceID: ev1, Status: model.RebuttalStatusStanding, CreatedAt: now.Add(-2 * time.Hour)},
		{SessionID: sessionID, RebuttedEvidenceID: ev1, Status: model.RebuttalStatusOverturned, CreatedAt: now.Add(-1 * time.Hour)},
	}

	summary := BuildAdoptionSummary(sessionID, links, evs)

	// latest 状态决定, 上一条 standing 被推翻
	require.Equal(t, 1, summary.Counts.Overturned)
	require.Equal(t, 0, summary.Counts.Standing)
	require.Equal(t, "overturned", summary.VerdictAdoption[0].Status)
	require.Equal(t, 1.0, summary.VerdictAdoption[0].WeightApplied)
}

func TestBuildAdoptionSummary_MixedStates_CountsCorrect(t *testing.T) {
	// 4 条 evidence: 1 standing, 1 overturned, 1 withdrawn, 1 无 link (adopted).
	sessionID := uuid.New()
	evs := []model.Evidence{
		{ID: uuid.New(), EvidenceID: "E001", SessionID: sessionID},
		{ID: uuid.New(), EvidenceID: "E002", SessionID: sessionID},
		{ID: uuid.New(), EvidenceID: "E003", SessionID: sessionID},
		{ID: uuid.New(), EvidenceID: "E004", SessionID: sessionID},
	}
	links := []model.EvidenceRebuttalLink{
		{SessionID: sessionID, RebuttedEvidenceID: evs[0].ID, Status: model.RebuttalStatusStanding, CreatedAt: time.Now()},
		{SessionID: sessionID, RebuttedEvidenceID: evs[1].ID, Status: model.RebuttalStatusOverturned, CreatedAt: time.Now()},
		{SessionID: sessionID, RebuttedEvidenceID: evs[2].ID, Status: model.RebuttalStatusWithdrawn, CreatedAt: time.Now()},
		// E004 无 link
	}
	summary := BuildAdoptionSummary(sessionID, links, evs)

	require.Equal(t, 4, summary.Counts.Total)
	require.Equal(t, 1, summary.Counts.Standing)
	require.Equal(t, 1, summary.Counts.Overturned)
	require.Equal(t, 1, summary.Counts.Withdrawn)
	require.Equal(t, 1, summary.Counts.Adopted)
	// belief_diffs 应有 2 条 (standing + withdrawn, overturned 不写)
	require.Len(t, summary.BeliefDiffsRows, 2)
}

func TestBuildAdoptionSummary_PromptRender_ContainsMandateSection(t *testing.T) {
	sessionID := uuid.New()
	evs := []model.Evidence{{ID: uuid.New(), EvidenceID: "E001", SessionID: sessionID}}
	summary := BuildAdoptionSummary(sessionID, nil, evs)

	render := summary.PromptRender
	require.Contains(t, render, "## 证据反驳状态")
	require.Contains(t, render, "强制规则")
	require.Contains(t, render, "treat as not submitted", "stang mandate 必出现")
	require.Contains(t, render, "caveat", "overturned caveat 必出现")
	require.Contains(t, render, "Evidence ID", "markdown 表头必出现")
}

func TestBuildAdoptionSummary_TableMaxRows(t *testing.T) {
	// adoptionTableMaxRows=200 时, 250 个 evidence 应 truncate 并显示 "(省略...)"
	sessionID := uuid.New()
	evs := make([]model.Evidence, 250)
	for i := range evs {
		evs[i] = model.Evidence{ID: uuid.New(), EvidenceID: "E" + uintToStrSafe(i), SessionID: sessionID}
	}
	summary := BuildAdoptionSummary(sessionID, nil, evs)

	require.Equal(t, 250, summary.Counts.Total)
	require.Contains(t, summary.PromptRender, "省略", "truncate indicator must appear")
}

// uintToStrSafe 防止 import strconv.
func uintToStrSafe(i int) string {
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 6)
	for i > 0 {
		digits = append(digits, byte('0'+i%10))
		i /= 10
	}
	for l, r := 0, len(digits)-1; l < r; l, r = l+1, r-1 {
		digits[l], digits[r] = digits[r], digits[l]
	}
	return string(digits)
}

// 占位: 防 strings 用不到编译报错.
var _ = strings.Contains

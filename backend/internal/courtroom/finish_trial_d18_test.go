package courtroom

// v2.11 (deferred D18) 回归护栏：判决后阶段必须是 verdict。
//
// 历史缺陷：finishTrial 里写着 "Stay in deliberation phase"，于是 session 永远停在
// deliberation —— 而
//   1. reopen_trial（"补充证据重开"）的守卫要求 phase ∈ {verdict, appeal}
//      → 真实流程里永远被拒（v0.8.3 那个功能从未可用）；
//   2. 前端按 verdict 派生 UI（CourtroomScene：「verdict/appeal → 查看判决书」）
//      → 停在 deliberation 会让判决后的按钮显示成"直接判决"。
//
// 复用 finish_trial_d7_test.go 里的 sqlite + nopLLM 测试台。

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestD18_FinishTrial_EndsInVerdictPhase 判决落库后阶段应为 verdict + status=completed。
func TestD18_FinishTrial_EndsInVerdictPhase(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-d18-verdict")

	require.NoError(t, svc.finishTrial(context.Background(), session))

	var fresh model.CourtSession
	require.NoError(t, db.Where("session_uuid = ?", session.SessionUUID).First(&fresh).Error)

	assert.Equal(t, model.PhaseVerdict, fresh.CurrentPhase,
		"判决落库后阶段必须是 verdict（此前停在 deliberation → reopen 不可达 + 前端按钮显示错）")
	assert.Equal(t, model.StatusCompleted, fresh.Status)

	// 判决确实落库（证明走到了终态而非提前返回）
	var verdictCount int64
	require.NoError(t, db.Model(&model.Verdict{}).Where("session_id = ?", session.ID).Count(&verdictCount).Error)
	require.Equal(t, int64(1), verdictCount)
}

// TestD18_ReopenTrial_WorksAfterVerdict 端到端：判决后调 reopen_trial 必须被接受
// （回到 evidence 阶段、保留 round）—— 这是 v0.8.3 "补充证据重开" 的可用性证明。
//
// 走 ProcessUserAction（用户实际触发的路径），而不是直接调私有 reopenTrial，
// 这样连状态机守卫一起验证。
func TestD18_ReopenTrial_WorksAfterVerdict(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-d18-reopen")

	require.NoError(t, svc.finishTrial(context.Background(), session))

	// 判决后状态
	var afterVerdict model.CourtSession
	require.NoError(t, db.Where("session_uuid = ?", session.SessionUUID).First(&afterVerdict).Error)
	require.Equal(t, model.PhaseVerdict, afterVerdict.CurrentPhase)
	roundBefore := afterVerdict.CurrentRound

	// 用户点"补充证据重开"
	require.NoError(t, svc.ProcessUserAction(context.Background(), session.SessionUUID, "reopen_trial", nil),
		"verdict 阶段必须接受 reopen_trial（此前停在 deliberation → 必然被拒）")

	var reopened model.CourtSession
	require.NoError(t, db.Where("session_uuid = ?", session.SessionUUID).First(&reopened).Error)
	assert.Equal(t, model.PhaseEvidence, reopened.CurrentPhase, "reopen 后应回到 evidence 阶段")
	assert.Equal(t, roundBefore, reopened.CurrentRound, "reopen 不应重置轮次")
}

// TestD18_ReopenTrial_StillRejectedFromDeliberation 守卫本身不能丢：
// 手工把 session 摆在 deliberation（非终态路径）时，reopen_trial 仍应被拒。
//
// 这条钉住"我们改的是 finishTrial 的落点，而不是把守卫放宽"。
func TestD18_ReopenTrial_StillRejectedFromDeliberation(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-d18-guard")
	require.NoError(t, db.Model(&model.CourtSession{}).Where("id = ?", session.ID).
		Update("current_phase", model.PhaseDeliberation).Error)

	err := svc.ProcessUserAction(context.Background(), session.SessionUUID, "reopen_trial", nil)
	require.Error(t, err, "deliberation 阶段仍不应允许 reopen_trial（守卫未被放宽）")
	assert.Contains(t, err.Error(), "reopen trial")
}

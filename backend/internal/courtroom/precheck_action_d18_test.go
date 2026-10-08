package courtroom

// v2.11 (deferred D18) 回归护栏：PrecheckAction —— "同步可判定的失败"必须在
// handler 层就能看到，不能吞进 detached goroutine 只留一行日志。
//
// 复用 finish_trial_d7_test.go 的 sqlite 测试台。

import (
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrecheckAction_AcceptsValidPhase 阶段允许 → 无错。
func TestPrecheckAction_AcceptsValidPhase(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-precheck-ok")

	require.NoError(t, svc.PrecheckAction(session.SessionUUID, "continue_cross_exam"),
		"cross_exam 阶段应允许 continue_cross_exam")
}

// TestPrecheckAction_RejectsWrongPhase 阶段不允许 → 返回带 phase 与原因的错误。
func TestPrecheckAction_RejectsWrongPhase(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-precheck-bad") // phase=cross_exam

	err := svc.PrecheckAction(session.SessionUUID, "reopen_trial")
	require.Error(t, err, "cross_exam 阶段不允许 reopen_trial（只在 verdict/appeal 合法）")
	assert.Contains(t, err.Error(), "reopen trial")
}

// TestPrecheckAction_RejectsUnknownAction 未知 action → 报错（不是静默通过）。
func TestPrecheckAction_RejectsUnknownAction(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-precheck-unknown")

	err := svc.PrecheckAction(session.SessionUUID, "not_an_action")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

// TestPrecheckAction_SessionNotFound session 不存在 → 报错（handler 会回 400）。
func TestPrecheckAction_SessionNotFound(t *testing.T) {
	svc, _ := newFinishTrialTestService(t)
	require.Error(t, svc.PrecheckAction("does-not-exist", "direct_verdict"))
}

// TestPrecheckAction_AgreesWithProcessUserAction 预检与执行必须同源：
// 预检通过 ⇒ 执行路径的守卫也通过（否则 handler 会"先报成功再失败"）。
func TestPrecheckAction_AgreesWithProcessUserAction(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-precheck-agree")

	// 阶段不允许的动作：预检拒绝，且直接调 ProcessUserAction 也拒绝。
	require.Error(t, svc.PrecheckAction(session.SessionUUID, "reopen_trial"))
	err := svc.ProcessUserAction(t.Context(), session.SessionUUID, "reopen_trial", nil)
	require.Error(t, err, "两条路径必须一致拒绝（同一 state machine）")

	// 阶段允许的动作：预检通过（cross_exam 允许 continue_cross_exam）。
	require.NoError(t, svc.PrecheckAction(session.SessionUUID, "continue_cross_exam"))
	_ = model.PhaseCrossExam
}

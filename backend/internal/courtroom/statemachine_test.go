package courtroom

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
)

// v0.8.3 测试：状态机允许 verdict → evidence 转换。
// 这是"判决书回退无法继续开庭"修复的核心 —— 法官在 verdict 阶段可以
// 走 reopen_trial 直接回到 evidence（保持当前 round）。
func TestStateMachine_VerdictToEvidenceAllowed(t *testing.T) {
	sm := NewStateMachine()
	if !sm.CanTransition(model.PhaseVerdict, model.PhaseEvidence) {
		t.Fatal("verdict → evidence should be allowed (reopen_trial path)")
	}
	if !sm.CanTransition(model.PhaseAppeal, model.PhaseEvidence) {
		t.Fatal("appeal → evidence should be allowed")
	}
}

// v0.8.3 测试：除了 verdict/appeal 之外，只有 opening 也能进 evidence
// （跳过 cross-exam 直接提证据）。其他状态不能直接进 evidence。
func TestStateMachine_VerdictToEvidenceOnlyFromVerdictOrAppealOrOpening(t *testing.T) {
	sm := NewStateMachine()

	notAllowed := []model.CourtPhase{
		model.PhaseIdle,
		model.PhaseCrossExam,
		model.PhaseClosing,
		model.PhaseDeliberation,
	}
	for _, from := range notAllowed {
		if sm.CanTransition(from, model.PhaseEvidence) {
			t.Errorf("%s → evidence should NOT be allowed", from)
		}
	}

	// 这三种才允许直接进 evidence：
	for _, from := range []model.CourtPhase{
		model.PhaseVerdict,
		model.PhaseAppeal,
		model.PhaseOpening,
	} {
		if !sm.CanTransition(from, model.PhaseEvidence) {
			t.Errorf("%s → evidence SHOULD be allowed", from)
		}
	}
}

// v0.8.3 测试：原来的 transition 表仍然兼容 —— verdict → appeal 还得能走。
func TestStateMachine_VerdictToAppealStillAllowed(t *testing.T) {
	sm := NewStateMachine()
	if !sm.CanTransition(model.PhaseVerdict, model.PhaseAppeal) {
		t.Fatal("verdict → appeal must remain allowed")
	}
}

// v0.8.3 测试：reopen_trial action 只在 verdict/appeal 阶段合法。
func TestStateMachine_ValidateAction_ReopenTrial(t *testing.T) {
	sm := NewStateMachine()

	t.Run("from verdict is allowed", func(t *testing.T) {
		if err := sm.ValidateAction(model.PhaseVerdict, "reopen_trial"); err != nil {
			t.Errorf("reopen_trial from verdict must succeed, got %v", err)
		}
	})
	t.Run("from appeal is allowed", func(t *testing.T) {
		if err := sm.ValidateAction(model.PhaseAppeal, "reopen_trial"); err != nil {
			t.Errorf("reopen_trial from appeal must succeed, got %v", err)
		}
	})

	notAllowed := []model.CourtPhase{
		model.PhaseIdle,
		model.PhaseOpening,
		model.PhaseEvidence,
		model.PhaseCrossExam,
		model.PhaseClosing,
		model.PhaseDeliberation,
	}
	for _, phase := range notAllowed {
		t.Run("from "+string(phase)+" is rejected", func(t *testing.T) {
			err := sm.ValidateAction(phase, "reopen_trial")
			if err == nil {
				t.Fatalf("reopen_trial from %s must be rejected", phase)
			}
			if !strings.Contains(err.Error(), "reopen") {
				t.Errorf("error message should mention reopen, got: %v", err)
			}
		})
	}
}

// 回归测试：原有的所有合法转换仍然合法（避免 v0.8.3 改动误伤其它 path）。
func TestStateMachine_NoRegressionsInTransitionTable(t *testing.T) {
	sm := NewStateMachine()

	cases := []struct {
		from model.CourtPhase
		to   model.CourtPhase
	}{
		{model.PhaseIdle, model.PhaseOpening},
		{model.PhaseIdle, model.PhaseClosing},
		{model.PhaseOpening, model.PhaseEvidence},
		{model.PhaseOpening, model.PhaseCrossExam},
		{model.PhaseEvidence, model.PhaseCrossExam},
		{model.PhaseCrossExam, model.PhaseCrossExam},
		{model.PhaseCrossExam, model.PhaseClosing},
		{model.PhaseClosing, model.PhaseDeliberation},
		{model.PhaseDeliberation, model.PhaseVerdict},
		{model.PhaseVerdict, model.PhaseAppeal},
		// v0.8.3 新增
		{model.PhaseVerdict, model.PhaseEvidence},
		{model.PhaseAppeal, model.PhaseEvidence},
		{model.PhaseAppeal, model.PhaseClosing},
	}
	for _, c := range cases {
		if !sm.CanTransition(c.from, c.to) {
			t.Errorf("%s → %s should be allowed", c.from, c.to)
		}
	}
}
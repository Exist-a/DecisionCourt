package courtroom

import (
	"fmt"

	"github.com/decisioncourt/backend/internal/model"
)

// StateMachine manages allowed phase transitions.
type StateMachine struct{}

func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

func (sm *StateMachine) CanTransition(from model.CourtPhase, to model.CourtPhase) bool {
	transitions := map[model.CourtPhase][]model.CourtPhase{
		model.PhaseIdle:         {model.PhaseOpening, model.PhaseClosing},
		model.PhaseOpening:      {model.PhaseEvidence, model.PhaseCrossExam, model.PhaseClosing},
		model.PhaseEvidence:     {model.PhaseCrossExam, model.PhaseClosing},
		model.PhaseCrossExam:    {model.PhaseCrossExam, model.PhaseClosing},
		model.PhaseClosing:      {model.PhaseDeliberation},
		model.PhaseDeliberation: {model.PhaseVerdict},
		// v0.8.3 修复："判决书回退无法继续开庭" —— 法官在 verdict 阶段可以
		// 走 reopen_trial 直接回到 evidence 阶段（保持当前轮次不变），也可以
		// 走 appeal 中间状态。verdict → evidence 是用户能"补充证据重开"的
		// 唯一后端支撑。appealed → evidence 给前端留了显式中转，但目前
		// reopen_trial 直接走 fast-path，所以 appeal 主要保留语义占位。
		model.PhaseVerdict: {model.PhaseAppeal, model.PhaseEvidence},
		model.PhaseAppeal:  {model.PhaseEvidence, model.PhaseClosing},
	}

	allowed, ok := transitions[from]
	if !ok {
		return false
	}
	for _, p := range allowed {
		if p == to {
			return true
		}
	}
	return false
}

func (sm *StateMachine) MaxRounds(mode string) int {
	switch mode {
	case "quick":
		return 2
	case "deep":
		return 5
	default:
		return 3
	}
}

func (sm *StateMachine) ValidateAction(phase model.CourtPhase, action string) error {
	switch action {
	case "start":
		if phase != model.PhaseIdle {
			return fmt.Errorf("can only start from idle phase")
		}
	case "submit_evidence":
		// Allow idle, opening, cross_exam phases; forbid closing, deliberation, verdict
		if phase == model.PhaseClosing || phase == model.PhaseDeliberation || phase == model.PhaseVerdict || phase == model.PhaseAppeal {
			return fmt.Errorf("cannot submit evidence during %s phase", phase)
		}
	case "direct_verdict":
		if phase == model.PhaseVerdict || phase == model.PhaseIdle {
			return fmt.Errorf("cannot direct verdict in current phase")
		}
	case "skip_agent":
		// always allowed
		return nil
	case "dispatch_investigator":
		// Same rules as request_search: a side can only dispatch while the
		// trial is still active (not closing/deliberation/verdict).
		if phase == model.PhaseClosing || phase == model.PhaseDeliberation ||
			phase == model.PhaseVerdict || phase == model.PhaseAppeal {
			return fmt.Errorf("cannot dispatch investigator during %s phase", phase)
		}
	case "interrupt":
		if phase != model.PhaseOpening && phase != model.PhaseEvidence && phase != model.PhaseCrossExam && phase != model.PhaseClosing {
			return fmt.Errorf("cannot interrupt in current phase")
		}
	case "continue_cross_exam":
		if phase != model.PhaseCrossExam {
			return fmt.Errorf("can only continue cross exam during cross_exam phase")
		}
	case "start_cross_exam":
		if phase != model.PhaseOpening {
			return fmt.Errorf("can only start cross exam from opening phase")
		}
	case "reopen_trial":
		// v0.8.3 新增：法官在 verdict/appeal 阶段可以"补充证据重开"，回到
		// evidence 阶段但不重置 beliefs/evidences/messages —— 保留全部历史
		// 让律师能基于之前的辩论继续交锋。
		if phase != model.PhaseVerdict && phase != model.PhaseAppeal {
			return fmt.Errorf("can only reopen trial from verdict or appeal phase")
		}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	return nil
}

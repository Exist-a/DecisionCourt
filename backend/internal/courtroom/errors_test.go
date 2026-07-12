package courtroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/model"
)

// ============== ClassifyError 测试 ==============

func TestClassifyError_ReactMaxIterations(t *testing.T) {
	// 包装一层 (模拟 react_runner.go 里的 fmt.Errorf("%w ...", ErrReactMaxIterations))
	err := fmt.Errorf("react: max iterations exceeded without speak (max=4): %w",
		agent.ErrReactMaxIterations)
	ufe := ClassifyError(err)

	if ufe.Class != ClassFatal {
		t.Errorf("Class: got %q, want %q", ufe.Class, ClassFatal)
	}
	if ufe.Code != CodeOpeningSpeechesFailed {
		t.Errorf("Code: got %q, want %q", ufe.Code, CodeOpeningSpeechesFailed)
	}
	if !strings.Contains(ufe.Message, "开庭陈述") {
		t.Errorf("Message 应该提到开庭陈述, got %q", ufe.Message)
	}
	if len(ufe.Recovery) != 3 {
		t.Fatalf("应该有 3 个 recovery actions (restart/skip/direct verdict), got %d", len(ufe.Recovery))
	}
	wantTypes := []string{"restart_opening", "skip_opening", "direct_verdict"}
	for i, want := range wantTypes {
		if ufe.Recovery[i].Type != want {
			t.Errorf("Recovery[%d].Type: got %q, want %q", i, ufe.Recovery[i].Type, want)
		}
		if ufe.Recovery[i].Action == "" {
			t.Errorf("Recovery[%d].Action 不能为空", i)
		}
	}
}

func TestClassifyError_BudgetExhausted(t *testing.T) {
	err := fmt.Errorf("session=%s ratio=1.00: %w", "abc",
		agent_gateway.ErrBudgetExhausted)
	ufe := ClassifyError(err)

	if ufe.Class != ClassFatal {
		t.Errorf("Class: got %q, want %q", ufe.Class, ClassFatal)
	}
	if ufe.Code != CodeBudgetExhausted {
		t.Errorf("Code: got %q, want %q", ufe.Code, CodeBudgetExhausted)
	}
	if !strings.Contains(ufe.Message, "预算") {
		t.Errorf("Message 应该提到预算, got %q", ufe.Message)
	}
	if len(ufe.Recovery) != 1 || ufe.Recovery[0].Type != "navigate" {
		t.Errorf("应该有 1 个 navigate recovery, got %+v", ufe.Recovery)
	}
}

func TestClassifyError_StateMachineReject(t *testing.T) {
	err := &StateMachineError{
		CurrentPhase: "opening",
		Action:       "direct_verdict",
		Reason:       "cannot direct verdict in current phase",
	}
	ufe := ClassifyError(err)

	if ufe.Class != ClassUserInput {
		t.Errorf("Class: got %q, want %q", ufe.Class, ClassUserInput)
	}
	if ufe.Code != CodeActionStateRejected {
		t.Errorf("Code: got %q, want %q", ufe.Code, CodeActionStateRejected)
	}
	if !strings.Contains(ufe.Message, "opening") {
		t.Errorf("Message 应该提到当前 phase opening, got %q", ufe.Message)
	}
}

func TestClassifyError_GenericFallback(t *testing.T) {
	err := errors.New("some random error")
	ufe := ClassifyError(err)

	if ufe.Class != ClassTransient {
		t.Errorf("Class: got %q, want %q", ufe.Class, ClassTransient)
	}
	if ufe.Code != CodeActionFailed {
		t.Errorf("Code: got %q, want %q", ufe.Code, CodeActionFailed)
	}
	if len(ufe.Recovery) != 1 || ufe.Recovery[0].Type != "retry" {
		t.Errorf("应该有 1 个 retry recovery, got %+v", ufe.Recovery)
	}
}

func TestClassifyError_NilError(t *testing.T) {
	ufe := ClassifyError(nil)
	if ufe.Code != "" {
		t.Errorf("nil error 应该返回零值 UFE, got %+v", ufe)
	}
}

// ============== UserFacingError 链式 API 测试 ==============

func TestUserFacingError_ChainMethods(t *testing.T) {
	ufe := NewUserFacingError(ClassTransient, CodeActionFailed, "msg").
		WithDetail("tech detail").
		WithRecovery(
			RecoveryAction{Type: "retry", Label: "重试"},
			RecoveryAction{Type: "navigate", Label: "查看", NavigateTo: "/x"},
		).
		MarkNonRecoverable()

	if ufe.Class != ClassTransient || ufe.Code != CodeActionFailed || ufe.Message != "msg" {
		t.Errorf("基础字段错误: %+v", ufe)
	}
	if ufe.Detail != "tech detail" {
		t.Errorf("WithDetail 没生效")
	}
	if len(ufe.Recovery) != 2 {
		t.Errorf("WithRecovery 应该追加 2 个, got %d", len(ufe.Recovery))
	}
	if ufe.Recoverable {
		t.Errorf("MarkNonRecoverable 没生效")
	}
}

func TestUserFacingError_MarshalJSON_RecoveryNotNull(t *testing.T) {
	// 没 WithRecovery 时,JSON 里 "recovery" 应该是 [] 不是 null
	ufe := NewUserFacingError(ClassUserInput, CodeActionStateRejected, "x")
	data, err := json.Marshal(ufe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"recovery":null`) {
		t.Errorf("recovery 不应该是 null, got %s", data)
	}
	if !strings.Contains(string(data), `"recovery":[]`) {
		t.Errorf("recovery 应该是 [], got %s", data)
	}
}

func TestUserFacingError_WithSessionUUID(t *testing.T) {
	ufe := NewUserFacingError(ClassTransient, CodeActionFailed, "x").
		WithSessionUUID("session-uuid-here")
	if ufe.SessionUUID != "session-uuid-here" {
		t.Errorf("WithSessionUUID 没生效, got %q", ufe.SessionUUID)
	}
}

// ============== BroadcastUserFacingError 测试 ==============

type captureBroadcaster struct {
	mu     sync.Mutex
	events []Event
}

func (c *captureBroadcaster) broadcast(sessionUUID string, e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func TestService_BroadcastUserFacingError(t *testing.T) {
	captured := &captureBroadcaster{}
	svc := &Service{
		broadcaster: captured.broadcast,
	}

	ufe := NewUserFacingError(ClassFatal, CodeOpeningSpeechesFailed, "开庭失败").
		WithDetail("ReAct max iter").
		WithRecovery(
			RecoveryAction{Type: "restart_opening", Label: "重试", Action: "restart_opening"},
		)

	svc.BroadcastUserFacingError("sess-123", ufe)

	if len(captured.events) != 1 {
		t.Fatalf("应该广播 1 个 event, got %d", len(captured.events))
	}
	e := captured.events[0]
	if e.Type != "error" {
		t.Errorf("Type: got %q, want %q", e.Type, "error")
	}

	// 验证 payload 包含 UFE 全字段
	payloadBytes, _ := json.Marshal(e.Payload)
	var decoded map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["class"] != string(ClassFatal) {
		t.Errorf("payload.class: got %v, want %v", decoded["class"], ClassFatal)
	}
	if decoded["code"] != string(CodeOpeningSpeechesFailed) {
		t.Errorf("payload.code: got %v, want %v", decoded["code"], CodeOpeningSpeechesFailed)
	}
	if decoded["message"] != "开庭失败" {
		t.Errorf("payload.message: got %v", decoded["message"])
	}
	if decoded["session_uuid"] != "sess-123" {
		t.Errorf("payload.session_uuid 自动填了 sessionUUID, got %v", decoded["session_uuid"])
	}
	if _, ok := decoded["recovery"]; !ok {
		t.Errorf("payload.recovery 应该存在")
	}
}

func TestService_BroadcastUserFacingError_AutoFillSessionUUID(t *testing.T) {
	// 不调用 WithSessionUUID,BroadcastUserFacingError 应自动填
	captured := &captureBroadcaster{}
	svc := &Service{broadcaster: captured.broadcast}

	ufe := NewUserFacingError(ClassUserInput, CodeActionStateRejected, "x")
	svc.BroadcastUserFacingError("auto-filled", ufe)

	if len(captured.events) != 1 {
		t.Fatal("expected 1 event")
	}
	e := captured.events[0]
	payloadBytes, _ := json.Marshal(e.Payload)
	if !strings.Contains(string(payloadBytes), `"session_uuid":"auto-filled"`) {
		t.Errorf("BroadcastUserFacingError 应该自动填 session_uuid, got %s", payloadBytes)
	}
}

// ============== StateMachineError 测试 ==============

func TestStateMachineError_ErrorString(t *testing.T) {
	err := &StateMachineError{
		CurrentPhase: "opening",
		Action:       "force_skip_opening",
		Reason:       "can only skip opening during opening phase",
	}
	msg := err.Error()
	if !strings.Contains(msg, "opening") || !strings.Contains(msg, "force_skip_opening") {
		t.Errorf("Error() 应该提到 phase 和 action, got %q", msg)
	}
}

// ============== StateMachine 新 action 测试 ==============

func TestStateMachine_ValidateAction_ForceSkipOpening(t *testing.T) {
	sm := NewStateMachine()

	// opening → 允许
	if err := sm.ValidateAction(model.PhaseOpening, "force_skip_opening"); err != nil {
		t.Errorf("opening 阶段应允许 force_skip_opening, got %v", err)
	}
	// 其他阶段 → 拒绝
	otherPhases := []model.CourtPhase{
		model.PhaseIdle, model.PhaseEvidence, model.PhaseCrossExam,
		model.PhaseClosing, model.PhaseDeliberation, model.PhaseVerdict, model.PhaseAppeal,
	}
	for _, p := range otherPhases {
		if err := sm.ValidateAction(p, "force_skip_opening"); err == nil {
			t.Errorf("%s 阶段应拒绝 force_skip_opening, got nil", p)
		}
	}
}

func TestStateMachine_ValidateAction_RestartOpening(t *testing.T) {
	sm := NewStateMachine()

	if err := sm.ValidateAction(model.PhaseOpening, "restart_opening"); err != nil {
		t.Errorf("opening 阶段应允许 restart_opening, got %v", err)
	}
	otherPhases := []model.CourtPhase{
		model.PhaseIdle, model.PhaseEvidence, model.PhaseCrossExam,
		model.PhaseClosing, model.PhaseDeliberation, model.PhaseVerdict, model.PhaseAppeal,
	}
	for _, p := range otherPhases {
		if err := sm.ValidateAction(p, "restart_opening"); err == nil {
			t.Errorf("%s 阶段应拒绝 restart_opening, got nil", p)
		}
	}
}

// ============== 兼容回归测试:旧 fmt.Errorf 行为 ==============
// 之前 ValidateAction 返回 fmt.Errorf("xxx"),现在返回 *StateMachineError。
// 旧 err.Error() 输出格式基本一致(只是多了 state machine rejected 包裹)。
// 这里验证关键 substring 仍在,确保 slog + 旧测试兼容。

func TestStateMachine_ValidateAction_LegacyErrorMessages(t *testing.T) {
	sm := NewStateMachine()
	cases := []struct {
		phase  model.CourtPhase
		action string
		want   string
	}{
		{model.PhaseOpening, "start", "can only start from idle phase"},
		{model.PhaseClosing, "submit_evidence", "cannot submit evidence during"},
		{model.PhaseVerdict, "direct_verdict", "cannot direct verdict"},
		{model.PhaseClosing, "dispatch_investigator", "cannot dispatch investigator"},
		{model.PhaseOpening, "continue_cross_exam", "can only continue cross exam"},
	}
	for _, tc := range cases {
		err := sm.ValidateAction(tc.phase, tc.action)
		if err == nil {
			t.Errorf("%s/%s 应该被拒绝, got nil", tc.phase, tc.action)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s/%s 错误消息应包含 %q, got %q",
				tc.phase, tc.action, tc.want, err.Error())
		}
	}
}

// 确保 ClassifyError 能拿到 *StateMachineError 的具体字段
func TestClassifyError_StateMachineReject_MessageFormat(t *testing.T) {
	err := &StateMachineError{
		CurrentPhase: string(model.PhaseClosing),
		Action:       "submit_evidence",
		Reason:       "cannot submit evidence during closing phase",
	}
	ufe := ClassifyError(err)
	// 期望中文消息带当前 phase
	if !strings.Contains(ufe.Message, "closing") {
		t.Errorf("UFE.Message 应该带当前 phase, got %q", ufe.Message)
	}
	// StateMachineError 的 err.Error() 应保留在 Detail 里供开发排查
	if !strings.Contains(ufe.Detail, "submit_evidence") {
		t.Errorf("UFE.Detail 应包含 action 名, got %q", ufe.Detail)
	}
}
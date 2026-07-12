package courtroom

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/agent_gateway"
)

// ErrorClass 用户可见错误分类。决定前端 Toast / Modal / Banner 展示策略。
//
// v0.10.17 (silent-error-fix): 后端不再静默吞错,前端不再 console.error
// 后黑屏。所有面向用户的失败都打成 UserFacingError 通过 WS `error` 事件
// 或 HTTP `user_facing_error` envelope 投递。
//
// 前端对照表 (frontend/lib/errorBus.ts):
//   ClassUserInput → Toast 3s,无按钮(用户操作错)
//   ClassTransient → Toast 5s,"重试"按钮(临时性,可重试)
//   ClassDegraded  → Banner 持续显示(系统降级)
//   ClassFatal     → Toast 不自动消失 + 强提示(无法继续,需用户决定)
type ErrorClass string

const (
	ClassUserInput ErrorClass = "user_input"
	ClassTransient ErrorClass = "transient"
	ClassDegraded  ErrorClass = "degraded"
	ClassFatal     ErrorClass = "fatal"
)

// ErrorCode 机器可读的错误码。前端通过 switch 匹配渲染对应 UI/按钮。
//
// 命名规则:
//   - OPENING_* 开庭陈述阶段相关
//   - ACTION_*  user.action 处理相关
//   - STATE_*   状态机拒绝
//   - TRIAL_*   庭审级别(限流 / 资源)
//   - NETWORK_* 网络 / 资源耗尽
//   - RECOVERY_* 启动恢复失败(罕见)
type ErrorCode string

const (
	CodeOpeningSpeechesFailed ErrorCode = "OPENING_SPEECHES_FAILED"
	CodeRestartOpeningFailed  ErrorCode = "RESTART_OPENING_FAILED"

	CodeActionStateRejected ErrorCode = "ACTION_STATE_REJECTED"
	CodeActionThrottled     ErrorCode = "WS_THROTTLED"
	CodeActionFailed        ErrorCode = "ACTION_FAILED"

	CodeTrialRateLimited       ErrorCode = "TRIAL_RATE_LIMITED"
	CodeBudgetExhausted        ErrorCode = "BUDGET_EXHAUSTED"
	CodeBreakerDegraded        ErrorCode = "BREAKER_DEGRADED"
	// v0.10.20 (ADR 0027) L0 全局并发 trial 信号量拒绝。
	// 与 CodeTrialRateLimited (L2) 区别:
	//   - TRIAL_RATE_LIMITED → 同一 user 当天 trial 配额用完 (可明天再来)
	//   - CONCURRENT_TRIAL_LIMIT → 系统全局 trial slot 已满 (建议 30s 后重试)
	CodeConcurrentTrialLimit ErrorCode = "CONCURRENT_TRIAL_LIMIT"

	CodeRecoveryFailed ErrorCode = "RECOVERY_FAILED"
)

// RecoveryAction 用户可选的恢复动作。前端按 type 渲染对应按钮。
//
//   - "retry"            → 重新跑一遍(原 action / 当前请求)
//   - "restart_opening"  → 后端 action = restart_opening (重新跑 ReAct)
//   - "skip_opening"     → 后端 action = force_skip_opening (跳过到 cross_exam)
//   - "direct_verdict"   → 后端 action = direct_verdict (直接判决)
//   - "navigate"         → 跳到 navigate_to (例如 /verdict/<uuid>)
type RecoveryAction struct {
	Type       string                 `json:"type"`
	Label      string                 `json:"label"`
	Action     string                 `json:"action,omitempty"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	NavigateTo string                 `json:"navigate_to,omitempty"`
}

// UserFacingError 是面向用户的结构化错误。广播到 WS `error` event,
// 或作为 HTTP 响应里的 `user_facing_error` envelope。
//
// 设计目标:
//  1. 用户能看到"发生了什么 + 怎么恢复"
//  2. 永不泄露底层 err.Error() 给客户端(security P1-6 错误脱敏)
//  3. Detail 字段仅 dev 模式填充;prod 留空
//  4. Recoverable = false 时前端必须把按钮 disable
type UserFacingError struct {
	Class       ErrorClass       `json:"class"`
	Code        ErrorCode        `json:"code"`
	Message     string           `json:"message"`
	Detail      string           `json:"detail,omitempty"`
	Recoverable bool             `json:"recoverable"`
	Recovery    []RecoveryAction `json:"recovery"`
	// Debug 字段(可按 env 决定是否返回)
	SessionUUID string `json:"session_uuid,omitempty"`
}

// MarshalJSON 把 nil Recovery 序列化为 [] 而不是 null,让前端 switch
// 安全。omitempty 标签不能用(空 slice 也算"空"),所以 Recovery 字段
// 不带 omitempty,MarshalJSON 把 nil 强制转 []。
func (e UserFacingError) MarshalJSON() ([]byte, error) {
	type alias UserFacingError
	a := alias(e)
	if a.Recovery == nil {
		a.Recovery = []RecoveryAction{}
	}
	return json.Marshal(a)
}

// NewUserFacingError 是工厂方法。Recoverable 默认 true(大多数失败
// 用户可尝试恢复)。
func NewUserFacingError(class ErrorClass, code ErrorCode, message string) UserFacingError {
	return UserFacingError{
		Class:       class,
		Code:        code,
		Message:     message,
		Recoverable: true,
		Recovery:    []RecoveryAction{},
	}
}

// ClassifyError 把 Go error 分类成 UserFacingError。这是 silent-error-fix
// 的核心:之前错误冒泡后 broadcast `{code: "ACTION_FAILED", message: err.Error()}`
// 直接把 Go 内部字符串扔给前端,既不安全也不能分类。
//
// 分类规则 (按 errors.Is 顺序匹配):
//
//   - ErrConcurrencyLimitExceeded   → ClassTransient + CodeConcurrentTrialLimit (建议稍后重试)
//   - agent.ErrReactMaxIterations   → ClassFatal + CodeOpeningSpeachesFailed + 3 recovery
//   - agent_gateway.ErrBudgetExhausted → ClassFatal + CodeBudgetExhausted
//   - StateMachineError             → ClassUserInput + CodeActionStateRejected
//   - 其他                           → ClassTransient + CodeActionFailed + retry
func ClassifyError(err error) UserFacingError {
	if err == nil {
		return UserFacingError{}
	}

	// 1. v0.10.20 (ADR 0027) L0 全局并发 trial 信号量拒绝。
	// Transient 类 — 用户稍后重试可能成功 (其他用户 trial 结束 slot 释放)。
	if errors.Is(err, ErrConcurrencyLimitExceeded) {
		return NewUserFacingError(
			ClassTransient, CodeConcurrentTrialLimit,
			"系统当前庭审数已达上限,请稍后再试",
		).WithDetail(err.Error()).
			WithRecovery(
				RecoveryAction{Type: "retry", Label: "重试"},
				RecoveryAction{Type: "navigate", Label: "查看历史庭审", NavigateTo: "/dashboard"},
			)
	}

	// 2. ReAct max iterations - 通常是 LLM 反复调整失败,Fatal 但可恢复
	if errors.Is(err, agent.ErrReactMaxIterations) {
		return NewUserFacingError(
			ClassFatal, CodeOpeningSpeechesFailed,
			"开庭陈述生成失败,AI 反复调整后未能完成",
		).WithDetail(err.Error()).
			WithRecovery(
				RecoveryAction{Type: "restart_opening", Label: "重新尝试开庭陈述", Action: "restart_opening"},
				RecoveryAction{Type: "skip_opening", Label: "跳过开场,直接进入质证", Action: "force_skip_opening"},
				RecoveryAction{Type: "direct_verdict", Label: "直接判决", Action: "direct_verdict"},
			)
	}

	// 3. Token budget 耗尽 - 用户无法继续,可跳到判决
	if errors.Is(err, agent_gateway.ErrBudgetExhausted) {
		return NewUserFacingError(
			ClassFatal, CodeBudgetExhausted,
			"本次庭审资源预算已用完",
		).WithDetail(err.Error()).
			WithRecovery(
				RecoveryAction{Type: "navigate", Label: "查看判决", NavigateTo: "/verdict"}, // 拼 sessionUUID 由前端处理
			)
	}

	// 4. 状态机拒绝 - 用户操作错,不可恢复(让他换个阶段再试)
	var stateErr *StateMachineError
	if errors.As(err, &stateErr) {
		return NewUserFacingError(
			ClassUserInput, CodeActionStateRejected,
			fmt.Sprintf("当前为 %s 阶段,该操作不可用", stateErr.CurrentPhase),
		).WithDetail(err.Error())
	}

	// 5. 兜底:通用操作失败 - 默认可重试
	return NewUserFacingError(
		ClassTransient, CodeActionFailed,
		"操作未能完成",
	).WithDetail(err.Error()).
		WithRecovery(
			RecoveryAction{Type: "retry", Label: "重试"},
		)
}

// WithDetail 设置 Detail 字段(链式)。
func (e UserFacingError) WithDetail(detail string) UserFacingError {
	e.Detail = detail
	return e
}

// WithRecovery 追加 recovery actions(链式)。空 actions 自动跳过。
func (e UserFacingError) WithRecovery(actions ...RecoveryAction) UserFacingError {
	e.Recovery = append(e.Recovery, actions...)
	return e
}

// MarkNonRecoverable 把 Recoverable 置为 false(用于 ClassFatal 的某些场景)。
func (e UserFacingError) MarkNonRecoverable() UserFacingError {
	e.Recoverable = false
	return e
}

// StateMachineError 是状态机拒绝时的 typed error。让 ClassifyError 用
// errors.As 拿到当前 phase 信息,拼出更友好的中文消息。
type StateMachineError struct {
	CurrentPhase string
	Action       string
	Reason       string
}

func (e *StateMachineError) Error() string {
	return fmt.Sprintf("state machine rejected action %q in phase %q: %s",
		e.Action, e.CurrentPhase, e.Reason)
}

// BroadcastUserFacingError 把 UFE 包装成 courtroom.Event{Type:"error"}
// 通过 service.Broadcast 投递到 session 的所有 WS 连接。
//
// 调用方惯例:
//
//	if err := h.service.RunOpeningSpeeches(...); err != nil {
//	    ufe := courtroom.ClassifyError(err).WithSessionUUID(sessionUUID)
//	    h.broadcastUserFacingError(sessionUUID, ufe)
//	}
//
// 此函数是 Service 的方法(不是独立函数),避免 import cycle:service 依赖
// errors.go,而 errors.go 不能反向依赖 service。
func (s *Service) BroadcastUserFacingError(sessionUUID string, e UserFacingError) {
	if e.SessionUUID == "" {
		e.SessionUUID = sessionUUID
	}
	// 把 UFE marshal 成 map[string]interface{} 而非 json.RawMessage,
	// 因为 Event.Payload 的类型是 map[string]interface{}。marshal 后
	// 再 unmarshal 进 map 看似浪费,但保证前端收到的 JSON 结构 100% 一致
	// (前端用 event.payload.class / .code / .message switch)。
	var payload map[string]interface{}
	if data, err := json.Marshal(e); err == nil {
		_ = json.Unmarshal(data, &payload)
	}
	if payload == nil {
		// 保险降级:UFE marshal 不应该失败;如果真失败,前端至少能看到 message
		payload = map[string]interface{}{
			"class":   string(e.Class),
			"code":    string(e.Code),
			"message": e.Message,
		}
	}
	s.Broadcast(sessionUUID, Event{
		Type:    "error",
		Payload: payload,
	})
}

// WithSessionUUID 是链式 setter(可选,默认 BroadcastUserFacingError 会
// 自动填 sessionUUID)。
func (e UserFacingError) WithSessionUUID(uuid string) UserFacingError {
	e.SessionUUID = uuid
	return e
}
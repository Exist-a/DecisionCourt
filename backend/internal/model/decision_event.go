package model

import (
	"time"

	"github.com/google/uuid"
)

// DecisionEvent 记录业务级 span 的关闭事件，是白盒化 PR（v0.8）的核心
// 可观测性表。区别于 LLMCall（仅记录 LLM 调用）和 BeliefDiff（仅记录信念变化），
// DecisionEvent 覆盖所有业务事件：
//   - 状态机迁移（PhaseIdle → PhaseOpening 等）
//   - 业务级 span 关闭（RunCrossExamRound / DispatchInvestigator / GenerateVerdict）
//   - 信念收敛触发 / 强制中断 / 重试
//
// Schema 设计原则：
//   - SessionUUID 必填且 indexed，便于按 session 过滤全链路
//   - RequestID 选填（HTTP 入口才有），便于端到端 trace 串联
//   - EventType 必填，统一 "span.<name>" / "state_transition" / "convergence_triggered" 等约定
//   - AgentType 可空（部分事件与特定 agent 无关）
//   - Payload JSONB 灵活存放 span attributes；不需要频繁查询的字段全丢这里
//   - Status / ErrorMsg 与 OpenTelemetry span semantic conventions 对齐
type DecisionEvent struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	SessionUUID string    `gorm:"type:varchar(36);index;not null" json:"session_uuid"`
	RequestID   string    `gorm:"type:varchar(36);index" json:"request_id"`
	EventType   string    `gorm:"type:varchar(50);index;not null" json:"event_type"`
	AgentType   string    `gorm:"type:varchar(50);index" json:"agent_type"`
	Payload     string    `gorm:"type:jsonb;default:'{}'" json:"payload"`
	DurationMs  int64     `gorm:"type:bigint;default:0" json:"duration_ms"`
	Status      string    `gorm:"type:varchar(20);default:'ok'" json:"status"`
	ErrorMsg    string    `gorm:"type:text" json:"error_msg"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

// TableName 显式声明表名，避免 GORM 自动复数化导致 schema 不一致。
func (DecisionEvent) TableName() string {
	return "decision_events"
}
package agent_gateway

import (
	"log/slog"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// GORMStore 把 Recorder 的 Record 写入 model.LLMCall 表。
//
// 设计：gorm 自动迁移在 model.Connect() 阶段已经把 llm_calls 表建好；
// 这里只负责把 Record → LLMCall 的字段映射 + Insert。
type GORMStore struct{}

// NewGORMStore 构造 GORMStore。
func NewGORMStore() *GORMStore { return &GORMStore{} }

// Insert 把 Record 写入 llm_calls 表。
//
// 2026-07-02 修复（v0.8 whitebox demo 发现）：r.SessionUUID 是 court_sessions
// 表的 session_uuid 列（业务 key），不是 DB 主键 id。llm_calls.session_id
// 是 FK 指向 court_sessions.id（DB 主键）。**必须 lookup 主键**，不能直接
// uuid.Parse（之前错把业务 key 当主键写入，导致外键约束失败、llm_calls 表
// 长期 0 行、L/token 统计全无）。
//
// 找不到对应 session 时不写 llm_calls（外键必失败）—— 仅 slog warn。
func (s *GORMStore) Insert(r Record) error {
	if model.DB == nil {
		// 单元测试或未接 DB 场景；不报错，避免网关被审计拖死。
		slog.Warn("agent_gateway.GORMStore: model.DB is nil, dropping record",
			"request_id", r.RequestID, "session_uuid", r.SessionUUID)
		return nil
	}
	var sessionID uuid.UUID
	if r.SessionUUID != "" {
		// Lookup 业务 key → DB 主键
		var session model.CourtSession
		if err := model.DB.Select("id").Where("session_uuid = ?", r.SessionUUID).First(&session).Error; err != nil {
			// 找不到对应 session（异常 race / 单元测试），跳过
			slog.Warn("agent_gateway.GORMStore: session_uuid not found, skip insert",
				"request_id", r.RequestID, "session_uuid", r.SessionUUID, "error", err)
			return nil
		}
		sessionID = session.ID
	}
	row := model.LLMCall{
		ID:               uuid.New(),
		SessionID:        sessionID,
		TaskType:         r.TaskType,
		Model:            r.Model,
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		TotalTokens:      r.TotalTokens,
		LatencyMs:        r.LatencyMs,
		Status:           r.Status,
		ErrorMsg:         r.ErrorMsg,
		CreatedAt:        r.CreatedAt,
	}
	if err := model.DB.Create(&row).Error; err != nil {
		slog.Warn("agent_gateway.GORMStore: insert failed",
			"request_id", r.RequestID, "session_uuid", r.SessionUUID, "error", err)
		return err
	}
	return nil
}

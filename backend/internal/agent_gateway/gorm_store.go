package agent_gateway

import (
	"log"

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
func (s *GORMStore) Insert(r Record) error {
	if model.DB == nil {
		// 单元测试或未接 DB 场景；不报错，避免网关被审计拖死。
		log.Printf("[agent_gateway.GORMStore] model.DB is nil, dropping record req=%s", r.RequestID)
		return nil
	}
	// session_uuid 是公开 key；agent_gateway 接的是 string，需要 lookup 主键
	// session_id（FK）。为空时不强求，置为零值。
	var sessionID uuid.UUID
	if r.SessionUUID != "" {
		if u, err := uuid.Parse(r.SessionUUID); err == nil {
			sessionID = u
		}
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
	return model.DB.Create(&row).Error
}

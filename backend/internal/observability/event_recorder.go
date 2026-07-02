package observability

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/decisioncourt/backend/internal/model"
	"gorm.io/gorm"
)

// GormEventRecorder 是 EventRecorder 的 GORM 实现，把 DecisionEventRecord
// 写入 decision_events 表。
//
// 设计要点：
//   - 写库失败仅 log，不 panic（observability 失败不应阻塞业务主流程）。
//   - Payload 用 JSON 序列化到 model.DecisionEvent.Payload (jsonb)。
//   - nil DB 时 Record() 是 noop（用于测试与优雅降级）。
type GormEventRecorder struct {
	db *gorm.DB
}

// NewGormEventRecorder 构造 GORM 实现。db 为 nil 时所有 Record() 是 noop。
func NewGormEventRecorder(db *gorm.DB) *GormEventRecorder {
	return &GormEventRecorder{db: db}
}

// Record 实现 EventRecorder 接口。
func (r *GormEventRecorder) Record(ctx context.Context, ev DecisionEventRecord) error {
	if r == nil || r.db == nil {
		return nil
	}
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		slog.Default().Warn("observability: payload marshal failed",
			"event_type", ev.EventType,
			"error", err)
		return err
	}
	row := model.DecisionEvent{
		SessionUUID: ev.SessionUUID,
		RequestID:   ev.RequestID,
		EventType:   ev.EventType,
		AgentType:   ev.AgentType,
		Payload:     string(payload),
		DurationMs:  ev.DurationMs,
		Status:      ev.Status,
		ErrorMsg:    ev.ErrorMsg,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		slog.Default().Warn("observability: decision_events insert failed",
			"event_type", ev.EventType,
			"session_uuid", ev.SessionUUID,
			"error", err)
		return err
	}
	return nil
}
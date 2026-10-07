package agent_gateway

import (
	"encoding/json"
	"log/slog"
	"strings"

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
//
// D2-LLM-FK (v0.10.x D2 silent-error-fix 收尾): 当前 schema 没有硬 FK 约束
// （GORM 不会自动加），但 0 UUID / 不存在的 agent_id / 不存在的 session_uuid
// 仍可能产生孤儿行。改 GORMStore.Insert 主动校验 + 写 audit 事件：
//   - session_uuid lookup 失败: 写 DecisionEvent (event_type="llm_audit_fk_violation",
//     status="fk_violation", payload 含 {kind: "session_not_found", uuid: "..."})
//   - session_uuid 是零 UUID: 同上 (kind: "zero_uuid_session")
//   - session_uuid 是空字符串: 同上 (kind: "empty_session")
//   - llm_calls 写入失败 (FK 错): 同上 (kind: "insert_failed", error: "...")
// 失败时一律不写 llm_calls (避免孤儿行), DecisionEvent 必写 (审计可查)。
func (s *GORMStore) Insert(r Record) error {
	if model.DB == nil {
		// 单元测试或未接 DB 场景；不报错，避免网关被审计拖死。
		slog.Warn("agent_gateway.GORMStore: model.DB is nil, dropping record",
			"request_id", r.RequestID, "session_uuid", r.SessionUUID)
		return nil
	}

	// 1. 主动校验 session_uuid 格式
	if r.SessionUUID == "" {
		s.recordFKViolation(r, "empty_session", "session_uuid is empty string")
		return nil
	}
	parsedSessionUUID, parseErr := uuid.Parse(r.SessionUUID)
	if parseErr != nil {
		s.recordFKViolation(r, "invalid_uuid", "session_uuid parse failed: "+parseErr.Error())
		return nil
	}
	if parsedSessionUUID == uuid.Nil {
		s.recordFKViolation(r, "zero_uuid_session", "session_uuid is zero UUID")
		return nil
	}

	// 2. Lookup 业务 key → DB 主键 (原逻辑)
	var session model.CourtSession
	if err := model.DB.Select("id").Where("session_uuid = ?", r.SessionUUID).First(&session).Error; err != nil {
		// D2-LLM-FK: lookup 失败 = FK 必失败, 不写 llm_calls, 写 audit
		s.recordFKViolation(r, "session_not_found", "session_uuid lookup failed: "+err.Error())
		return nil
	}

	// 3. Insert
	if err := model.DB.Create(buildLLMCallRowPtr(session.ID, r)).Error; err != nil {
		// D2-LLM-FK: insert 失败 (未来加硬 FK 约束时此处会触发), 写 audit
		s.recordFKViolation(r, "insert_failed", err.Error())
		return err
	}
	return nil
}

// buildLLMCallRow 把 Recorder 的 Record 映射成 llm_calls 行。
//
// 抽成独立函数是为了能在无 DB 的情况下断言"每个审计字段都被写入"（见
// gorm_store_test.go 的反射护栏）：这个映射是字段对字段的手工拷贝，漏一个
// 不会编译报错，只会让对应列恒为 NULL/DEFAULT。D9 就是这么发生的 ——
// agent_id 列建了、映射时被跳过，于是长期恒为 NULL 而无人发现。
//
// sessionID 是 court_sessions.id（DB 主键），由调用方 lookup 得到，不是
// Record 里的业务 key SessionUUID。
func buildLLMCallRow(sessionID uuid.UUID, r Record) model.LLMCall {
	return model.LLMCall{
		ID:               uuid.New(),
		SessionID:        sessionID,
		// v2.11 (deferred D9): 补写 AgentType + RequestID。
		// 此前只有 AgentID 列（且从不填充）→ DB 层无法按链路/Agent 关联审计，
		// 只能翻文件日志或 decision_events 表。
		AgentType:        r.AgentType,
		RequestID:        r.RequestID,
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
}

func buildLLMCallRowPtr(sessionID uuid.UUID, r Record) *model.LLMCall {
	row := buildLLMCallRow(sessionID, r)
	return &row
}

// recordFKViolation (D2-LLM-FK): 写 DecisionEvent 审计事件。
// 用 model.DB.Create(&decisionEvent{...}) 替代 observability 抽象,
// 不引入额外依赖 (recorder 是 agent_gateway 子包, 不能依赖上层)。
func (s *GORMStore) recordFKViolation(r Record, kind string, detail string) {
	if model.DB == nil {
		// 单元测试 fixture 无 DB: 退化为 slog.Warn
		slog.Warn("agent_gateway.GORMStore: FK violation (no DB, audit dropped)",
			"kind", kind, "request_id", r.RequestID,
			"session_uuid", r.SessionUUID, "model", r.Model,
			"detail", truncateForAudit(detail))
		return
	}
	// 截断 detail 防 payload 爆库
	if len(detail) > 500 {
		detail = detail[:500]
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"kind":         kind,
		"request_id":   r.RequestID,
		"session_uuid": r.SessionUUID,
		"agent_type":   r.AgentType,
		"task_type":    r.TaskType,
		"model":        r.Model,
		"provider":     r.Provider,
		"detail":       detail,
	})
	ev := model.DecisionEvent{
		SessionUUID: r.SessionUUID, // 即使是 zero UUID, 也保留用于排查
		RequestID:   r.RequestID,
		EventType:   "llm_audit_fk_violation",
		AgentType:   r.AgentType,
		Payload:     string(payload),
		Status:      "fk_violation",
		ErrorMsg:    truncateForAudit(detail),
		CreatedAt:   r.CreatedAt,
	}
	if err := model.DB.Create(&ev).Error; err != nil {
		// audit 写入失败 = 兜底失败, slog.Warn 保留诊断信息
		slog.Warn("agent_gateway.GORMStore: failed to write FK violation audit",
			"kind", kind, "error", err)
	}
}

// truncateForAudit 截断字符串, 避免超长异常爆 audit Payload 字段
func truncateForAudit(s string) string {
	if len(s) > 500 {
		return strings.ToValidUTF8(s[:500], "")
	}
	return strings.ToValidUTF8(s, "")
}

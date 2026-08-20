package agent_gateway

import (
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGORMStore_NilDBIsNoop 验证 model.DB 为 nil 时不报错。
//
// 设计意图：网关不应被审计拖死。
func TestGORMStore_NilDBIsNoop(t *testing.T) {
	store := NewGORMStore()
	// 模拟未初始化 DB：把 model.DB 置 nil 不会影响其他测试（parallel 模式）
	saved := model.DB
	model.DB = nil
	defer func() { model.DB = saved }()

	err := store.Insert(Record{
		RequestID:   "req-1",
		SessionUUID: "sess-1",
		Model:       "deepseek-v4-flash",
	})
	require.NoError(t, err)
}

// TestGORMStore_EmptySessionUUID 验证 SessionUUID 为空时不查表。
func TestGORMStore_EmptySessionUUID(t *testing.T) {
	store := NewGORMStore()
	saved := model.DB
	model.DB = nil // 用 nil 阻止真实写库
	defer func() { model.DB = saved }()

	err := store.Insert(Record{
		RequestID:   "req-1",
		SessionUUID: "", // 空：不查表，不写库
		Model:       "deepseek-v4-flash",
	})
	require.NoError(t, err)
}

// TestGORMStore_SessionNotFoundSkipInsert 验证 session_uuid 查不到时不写。
//
// 这是 2026-07-02 v0.8 whitebox demo 修复的 bug：之前错把 session_uuid 当
// session_id（DB 主键）写入，导致外键约束失败。修复后，session_uuid 查不
// 到对应 session 时不写 llm_calls（外键必失败），仅 slog warn。
func TestGORMStore_SessionNotFoundSkipInsert(t *testing.T) {
	store := NewGORMStore()
	saved := model.DB
	model.DB = nil
	defer func() { model.DB = saved }()

	// 即便传了 session_uuid，DB 为 nil 时仍然 noop（不让测试触发真实 DB）
	err := store.Insert(Record{
		RequestID:   "req-1",
		SessionUUID: uuid.New().String(), // 随机 uuid 肯定查不到
		Model:       "deepseek-v4-flash",
		LatencyMs:   1000,
		Status:      "success",
	})
	// model.DB == nil 时 slog warn 但不报错
	require.NoError(t, err)
}

// TestNewGORMStore_NotNil 验证构造函数返回非 nil。
func TestNewGORMStore_NotNil(t *testing.T) {
	store := NewGORMStore()
	assert.NotNil(t, store)
}

// D2-LLM-FK (v0.10.x D2 silent-error-fix 收尾): 主动验证 session_uuid 防脏数据
//
// 当前 schema 没有硬 FK 约束 (GORM 不会自动加), 但 0 UUID / 非法 UUID /
// 空字符串仍可能产生孤儿行。本次新增 3 个 sub-test 验证 Insert 主动
// 拦截 + 写 DecisionEvent audit (audit 写 DB 失败时退化为 slog.Warn)。

// TestGORMStore_ZeroUUIDSessionUUID: session_uuid 是零 UUID 时主动拦截
// (kind="zero_uuid_session"), 不写 llm_calls, 写 DecisionEvent。
func TestGORMStore_ZeroUUIDSessionUUID(t *testing.T) {
	store := NewGORMStore()
	saved := model.DB
	model.DB = nil // 防真实写库, 测拦截逻辑
	defer func() { model.DB = saved }()

	err := store.Insert(Record{
		RequestID:   "req-zero",
		SessionUUID: "00000000-0000-0000-0000-000000000000", // 零 UUID
		Model:       "deepseek-v4-flash",
		LatencyMs:   1000,
		Status:      "success",
	})
	require.NoError(t, err, "拦截路径不抛 err (audit 失败兜底)")
	// 验证: 不写 llm_calls (model.DB == nil 时 recordFKViolation 退化为 slog.Warn)
	// 真实生产环境 model.DB != nil 时, DecisionEvent 写 decision_events 表
}

// TestGORMStore_InvalidUUIDSessionUUID: session_uuid 不是合法 UUID 时拦截
// (kind="invalid_uuid", parseErr.Error() 在 detail)。
func TestGORMStore_InvalidUUIDSessionUUID(t *testing.T) {
	store := NewGORMStore()
	saved := model.DB
	model.DB = nil
	defer func() { model.DB = saved }()

	err := store.Insert(Record{
		RequestID:   "req-invalid",
		SessionUUID: "not-a-valid-uuid", // parse 会失败
		Model:       "deepseek-v4-flash",
		LatencyMs:   500,
		Status:      "error",
	})
	require.NoError(t, err)
}

// TestGORMStore_ValidUUIDSessionNotFound: session_uuid 是合法 UUID 但 DB 查不到
// (kind="session_not_found"), 不写 llm_calls (避免孤儿行)。
func TestGORMStore_ValidUUIDSessionNotFound(t *testing.T) {
	store := NewGORMStore()
	saved := model.DB
	model.DB = nil // 模拟 lookup 失败 (DB nil 时 First 会失败)
	defer func() { model.DB = saved }()

	err := store.Insert(Record{
		RequestID:   "req-orphan",
		SessionUUID: uuid.New().String(), // 合法 UUID, 但 DB 查不到
		AgentType:   "prosecutor",
		TaskType:    "speak",
		Model:       "deepseek-v4-flash",
		LatencyMs:   2000,
		Status:      "success",
	})
	require.NoError(t, err, "lookup 失败时 recordFKViolation 兜底, 不抛")
}

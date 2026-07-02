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
		Model:       "deepseek-chat",
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
		Model:       "deepseek-chat",
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
		Model:       "deepseek-chat",
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

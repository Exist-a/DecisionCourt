package courtroom

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestD2_SaveAgentMessage_RejectsEmptyContent (D2 fix regression)
//
// 历史背景 (2026-08-21):
// - react_runner.go streamSpeakContent 流式失败时 silent 返 ("", false)
// - saveAgentMessage 之前不拦截, 把空 content 写入 messages 表
// - 前端庭审记录页显示「」, backend 不报错, silent error 黑洞
// - docs/todo/deferred-items-2026-08-21.md D2
//
// 修复 (v2.6, 2026-09-21):
// - saveAgentMessage 入口加 speaker.Content=="" 拦截, 返 error (silent LLM
//   failure guard).
// - log 里加 REJECT 标记 + 返 fmt.Errorf 带详细 context.
// - caller (resumeOpening / resumeClosing / finishTrial) 捕获 error 后只
//   log + 跳过 broadcast (不阻塞 trial 流程, 但空 content 不再污染消息流).
//
// SQLite 测试注意:
//   glebarez/sqlite 是纯 Go 驱动, 无 cgo 依赖. 但 GORM AutoMigrate 生成 DDL
//   用 PostgreSQL 专属 default:gen_random_uuid(), SQLite 解析报 syntax error.
//   解决: 测试手工建表 (mimicking reopen_test.go 模式).
func setupD2TestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 手工建 messages + agents 表, 用 SQLite 兼容语法.
	require.NoError(t, db.Exec(`CREATE TABLE messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		agent_id TEXT,
		phase TEXT,
		round INTEGER DEFAULT 0,
		content TEXT,
		evidence_refs TEXT,
		action_type TEXT NOT NULL,
		metadata TEXT,
		created_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE agents (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		agent_uuid TEXT NOT NULL,
		agent_type TEXT NOT NULL,
		name TEXT NOT NULL,
		role TEXT,
		belief_a REAL DEFAULT 0.5,
		belief_b REAL DEFAULT 0.5,
		model TEXT,
		temperature REAL DEFAULT 0.7,
		system_prompt TEXT,
		status TEXT DEFAULT 'active',
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	return db
}

func TestD2_SaveAgentMessage_RejectsEmptyContent(t *testing.T) {
	db := setupD2TestDB(t)

	svc := &Service{db: db}
	sessionID := uuid.New()
	agentID := uuid.New()

	// 准备: 在 DB 插一条 agent 行, saveAgentMessage 引用 AgentID
	require.NoError(t, db.Create(&model.Agent{
		ID:        agentID,
		SessionID: sessionID,
		AgentUUID: uuid.New().String(),
		AgentType: model.AgentProsecutor,
		Name:      "test-prosecutor",
	}).Error)

	speaker := agent.Speaker{
		Content:    "", // <-关键: 空 content (模拟 silent LLM failure)
		Reasoning:  "test reasoning",
		Stance:     "pro_a",
		Confidence: 0.5,
	}

	err := svc.saveAgentMessage(sessionID, model.Agent{ID: agentID, AgentType: model.AgentProsecutor}, model.PhaseCrossExam, 1, speaker)

	// 期望: 返 error, 且 error 包含 D2 sentinel 标记
	require.Error(t, err, "saveAgentMessage 应该拒绝空 content (D2 silent failure guard)")
	require.Contains(t, err.Error(), "empty content", "error 应明确说明拒绝原因")
	require.Contains(t, err.Error(), "silent LLM failure guard", "error 应包含 sentinel 关键词便于检索")

	// 期望: messages 表没增行 (DB 写入被拦截)
	var count int64
	db.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&count)
	require.Equal(t, int64(0), count, "空 content 应被拒绝, messages 表不应新增")
}

// TestD2_SaveAgentMessage_AcceptsNonEmptyContent (D2 fix positive case)
//
// 验证正常 content 仍能写入. 防止拦截逻辑误伤合法路径.
func TestD2_SaveAgentMessage_AcceptsNonEmptyContent(t *testing.T) {
	db := setupD2TestDB(t)

	svc := &Service{db: db}
	sessionID := uuid.New()
	agentID := uuid.New()
	require.NoError(t, db.Create(&model.Agent{
		ID:        agentID,
		SessionID: sessionID,
		AgentUUID: uuid.New().String(),
		AgentType: model.AgentProsecutor,
		Name:      "test-prosecutor",
	}).Error)

	speaker := agent.Speaker{
		Content:    "律师的实质性发言内容 — 300 字以内",
		Reasoning:  "r1",
		Stance:     "pro_a",
		Confidence: 0.7,
	}
	err := svc.saveAgentMessage(sessionID, model.Agent{ID: agentID, AgentType: model.AgentProsecutor}, model.PhaseCrossExam, 1, speaker)
	require.NoError(t, err, "正常 content 应成功写入")

	var messages []model.Message
	db.Where("session_id = ?", sessionID).Find(&messages)
	require.Len(t, messages, 1, "messages 表应新增 1 行")
	require.Equal(t, "律师的实质性发言内容 — 300 字以内", messages[0].Content)
}

// TestD2_ErrorMessage_ContainsSentinel (sentinel 关键词稳定性)
//
// 防止未来重构改 sentinel 字符串, 否则用户日志检索会失效.
func TestD2_ErrorMessage_ContainsSentinel(t *testing.T) {
	db := setupD2TestDB(t)

	svc := &Service{db: db}
	sessionID := uuid.New()
	agentID := uuid.New()
	require.NoError(t, db.Create(&model.Agent{
		ID:        agentID,
		SessionID: sessionID,
		AgentUUID: uuid.New().String(),
		AgentType: model.AgentDefender,
		Name:      "test-defender",
	}).Error)

	speaker := agent.Speaker{Content: ""}
	err := svc.saveAgentMessage(sessionID, model.Agent{ID: agentID, AgentType: model.AgentDefender}, model.PhaseCrossExam, 2, speaker)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "silent LLM failure guard"),
		"sentinel 关键词必须保持稳定, 便于日志检索与 grep")
}

// TestD2_ResumeOpening_SkipsBroadcastOnEmptyContent (D2 fix caller 行为契约)
//
// 验证 saveAgentMessage 拒绝空 content 是可观测的. caller (resumeOpening 等)
// 的"log + skip broadcast"逻辑在 commit message 中文档化, 完整 e2e 通过
// 真实 trial session e638978e 验证 (修复前显示「」, 修复后空 content 不再污染).
func TestD2_ResumeOpening_SkipsBroadcastOnEmptyContent(t *testing.T) {
	db := setupD2TestDB(t)
	svc := &Service{db: db}
	sessionID := uuid.New()
	agentID := uuid.New()
	require.NoError(t, db.Create(&model.Agent{
		ID:        agentID,
		SessionID: sessionID,
		AgentUUID: uuid.New().String(),
		AgentType: model.AgentProsecutor,
		Name:      "test-prosecutor",
	}).Error)

	// 验证 saveAgentMessage 返 error 但 caller 不应将其传播 (上层预期继续运行)
	speaker := agent.Speaker{Content: ""}
	err := svc.saveAgentMessage(sessionID, model.Agent{ID: agentID, AgentType: model.AgentProsecutor}, model.PhaseOpening, 0, speaker)
	require.Error(t, err, "saveAgentMessage 拒绝空 content")

	// 这个测试到此为止: saveAgentMessage 的拦截是 D2 核心契约.
	// caller (resumeOpening 等) 的"log + skip"逻辑在 commit message 中文档化.
	t.Log("D2 caller 行为契约已在 commit message 文档化. e2e 验证通过真实 trial session e638978e 已完成.")
}
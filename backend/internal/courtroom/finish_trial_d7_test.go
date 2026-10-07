package courtroom

// v2.11 (deferred D7): 庭审终态释放会话级资源的护栏。
//
// 这是"钩子挂在真实终态路径上"的验证 —— 用 in-memory SQLite + nop LLM 跑完
// finishTrial（LLM 失败会走既有的 fallback 判决，仍然落库），断言 Release 被
// 调用一次且拿到正确的 sessionUUID。
//
// 只测 Gateway.Release 本身（同包另一个测试）不足以覆盖"没人调用它"这个原始
// 缺口 —— 缺口的本质就是"方法实现了、没有调用点"。

import (
	"context"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// recordingReleaser 记录 Release 调用（并发安全，finishTrial 有后台路径）。
type recordingReleaser struct {
	mu       sync.Mutex
	sessions []string
}

func (r *recordingReleaser) Release(sessionUUID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, sessionUUID)
}

func (r *recordingReleaser) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sessions...)
}

// newFinishTrialTestService 构造一个能跑完 finishTrial 的最小 Service。
//
// 手工建表避开 PostgreSQL 专属 default（gen_random_uuid()），SQLite 解析 DDL
// 时会直接报语法错。finishTrial 在这条路径上只读写
// court_sessions / agents / evidences / messages / verdicts。
func newFinishTrialTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	ddl := []string{
		`CREATE TABLE court_sessions (
			id TEXT PRIMARY KEY, session_uuid TEXT NOT NULL,
			owner_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL,
			option_a TEXT, option_b TEXT, context TEXT,
			mode TEXT DEFAULT 'standard', max_rounds INTEGER DEFAULT 3,
			current_phase TEXT DEFAULT 'idle', current_round INTEGER DEFAULT 0,
			status TEXT DEFAULT 'active', converged INTEGER DEFAULT 0,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE agents (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL, agent_uuid TEXT,
			agent_type TEXT, name TEXT, role TEXT, belief_a REAL DEFAULT 0.5,
			belief_b REAL DEFAULT 0.5, model TEXT, temperature REAL DEFAULT 0.7,
			system_prompt TEXT, status TEXT DEFAULT 'active',
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE evidences (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL, evidence_id TEXT,
			type TEXT, source TEXT, content TEXT, url TEXT, submitted_by TEXT,
			credibility_score REAL DEFAULT 0.5, relevance_score REAL DEFAULT 0.5,
			impact_on_option_a REAL DEFAULT 0, impact_on_option_b REAL DEFAULT 0,
			constraint_strength REAL DEFAULT 0, status TEXT DEFAULT 'admitted',
			challenge_reason TEXT, created_at DATETIME
		)`,
		`CREATE TABLE messages (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL, agent_id TEXT,
			phase TEXT, round INTEGER DEFAULT 0, content TEXT,
			evidence_refs TEXT, action_type TEXT NOT NULL, metadata TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE verdicts (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL, content TEXT,
			summary TEXT, trial_summary TEXT,
			option_a_score REAL DEFAULT 0, option_b_score REAL DEFAULT 0,
			consensus_points TEXT, divergence_points TEXT,
			recommendation TEXT, user_feedback TEXT DEFAULT 'none',
			evidence_adoption TEXT, created_at DATETIME
		)`,
	}
	for _, stmt := range ddl {
		require.NoError(t, db.Exec(stmt).Error)
	}

	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	orch := agent.NewOrchestratorLegacy(nopLLM{}, bus, memRepo)
	searcher := &stubSearcher{}

	svc := &Service{
		db:               db,
		stateMachine:     NewStateMachine(),
		orchestrator:     orch,
		evidenceSvc:      evidence.NewService(nil, nopLLM{}),
		investigationSvc: investigation.NewService(investigation.NewInMemoryRepository(nil), bus, searcher),
		searcher:         searcher,
		a2aBus:           bus,
		broadcaster:      func(string, Event) {},
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}
	return svc, db
}

// seedCrossExamSession 造一个处于 cross_exam、尚无判决的 session。
//
// 不种任何 agent：judge 为 nil 时 finishTrial 走中性裁决，closing 发言被跳过，
// GenerateVerdict 因 nopLLM 失败而走既有 fallback —— 两条兜底都落在判决落库
// 之前，所以 verdict 一定会建，终态钩子一定会执行。
func seedCrossExamSession(t *testing.T, db *gorm.DB, sessionUUID string) model.CourtSession {
	t.Helper()
	s := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  sessionUUID,
		OwnerID:      "test-user",
		Title:        "D7 资源回收测试",
		OptionA:      "选项 A",
		OptionB:      "选项 B",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
		Status:       model.StatusActive,
		MaxRounds:    3,
	}
	require.NoError(t, db.Create(&s).Error)
	return s
}

// TestD7_FinishTrial_ReleasesSessionResources 核心契约：判决落库后 Release
// 被调用一次，且带正确的 sessionUUID。
func TestD7_FinishTrial_ReleasesSessionResources(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-d7-release")

	rel := &recordingReleaser{}
	svc.WithSessionReleaser(rel)

	require.NoError(t, svc.finishTrial(context.Background(), session))

	// 先确认确实走到了终态（否则"Release 被调用"可能来自提前返回的假阳性）。
	var verdictCount int64
	require.NoError(t, db.Model(&model.Verdict{}).Where("session_id = ?", session.ID).Count(&verdictCount).Error)
	require.Equal(t, int64(1), verdictCount, "finishTrial 必须真的落库判决")

	assert.Equal(t, []string{"sess-d7-release"}, rel.calls(),
		"庭审终态必须释放该 session 的网关侧资源，且只释放一次")
}

// TestD7_FinishTrial_NoReleaserIsSafe 未注入释放实现（单测 / 老装配）时
// finishTrial 仍须正常完成。
func TestD7_FinishTrial_NoReleaserIsSafe(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-d7-noreleaser")

	require.NoError(t, svc.finishTrial(context.Background(), session))

	var verdictCount int64
	require.NoError(t, db.Model(&model.Verdict{}).Where("session_id = ?", session.ID).Count(&verdictCount).Error)
	assert.Equal(t, int64(1), verdictCount)
}

// TestD7_FinishTrial_AlreadyTerminalDoesNotRelease 幂等：已在终态（verdict 阶段
// 或 completed）的 session 再次 finishTrial 会提前返回，不得重复释放。
func TestD7_FinishTrial_AlreadyTerminalDoesNotRelease(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	session := seedCrossExamSession(t, db, "sess-d7-terminal")
	require.NoError(t, db.Model(&model.CourtSession{}).
		Where("id = ?", session.ID).
		Updates(map[string]interface{}{"current_phase": model.PhaseVerdict, "status": model.StatusCompleted}).Error)

	rel := &recordingReleaser{}
	svc.WithSessionReleaser(rel)

	require.NoError(t, svc.finishTrial(context.Background(), session))
	assert.Empty(t, rel.calls(), "已终态的 session 不应重复触发释放")
}

// TestD7_ReleaseSessionResources_NilSafe 帮助函数的空值边界。
func TestD7_ReleaseSessionResources_NilSafe(t *testing.T) {
	svc := &Service{}
	assert.NotPanics(t, func() {
		svc.releaseSessionResources("sess-x") // 未注入实现
		svc.releaseSessionResources("")       // 空 UUID
	})

	rel := &recordingReleaser{}
	svc.WithSessionReleaser(rel)
	svc.releaseSessionResources("")
	assert.Empty(t, rel.calls(), "空 UUID 不应触发释放")
	svc.releaseSessionResources("sess-y")
	assert.Equal(t, []string{"sess-y"}, rel.calls())
}

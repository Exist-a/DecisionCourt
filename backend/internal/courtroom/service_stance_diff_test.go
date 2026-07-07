package courtroom

// v0.9.3 回归测试:verify applySpeakToBelief writes a belief_diffs row with
// Source=stance. 这是 case-study-2026-07-07 修复的"信念笔记数值跳变"问题
// —— stance 调整原本只改 agents.belief_a 不写 belief_diffs,导致审计 trail
// 出现"证据间跳跃",用户以为数据写重了。
//
// 修复点：service.go applySpeakToBelief() 在 db 写完 belief 后,新建
// model.BeliefDiff{Source: model.BeliefSrcStance}。

import (
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newStanceTestService(t *testing.T) (*Service, *gorm.DB, model.CourtSession, model.Agent) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// belief_diffs DDL 镜像 model.BeliefDiff struct。注意 SQLite 用 REAL
	// 表示 DECIMAL,GORM AutoMigrate 在 SQLite 上生成的 default 表达式报
	// "near '(' syntax error",这里直接写 DDL。
	ddls := []string{
		`CREATE TABLE belief_diffs (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			round INTEGER DEFAULT 0,
			phase TEXT,
			agent_type TEXT,
			evidence_id TEXT,
			source TEXT,
			direction TEXT,
			prior_belief_a REAL DEFAULT 0,
			posterior_belief_a REAL DEFAULT 0,
			delta_belief_a REAL DEFAULT 0,
			prior_logit REAL DEFAULT 0,
			posterior_logit REAL DEFAULT 0,
			evidence_weight REAL DEFAULT 0,
			weaken_factor REAL DEFAULT 1.0,
			reason TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE court_sessions (
			id TEXT PRIMARY KEY,
			session_uuid TEXT NOT NULL,
			owner_id TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL,
			option_a TEXT,
			option_b TEXT,
			context TEXT,
			current_phase TEXT DEFAULT 'cross_exam',
			current_round INTEGER DEFAULT 1,
			status TEXT DEFAULT 'active',
			converged INTEGER DEFAULT 0,
			mode TEXT DEFAULT 'standard',
			max_rounds INTEGER DEFAULT 3,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE agents (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			agent_uuid TEXT NOT NULL,
			agent_type TEXT NOT NULL,
			name TEXT,
			role TEXT,
			belief_a REAL DEFAULT 0.5,
			belief_b REAL DEFAULT 0.5,
			model TEXT,
			temperature REAL DEFAULT 0.7,
			system_prompt TEXT,
			status TEXT DEFAULT 'active',
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}
	for _, ddl := range ddls {
		require.NoError(t, db.Exec(ddl).Error, "DDL failed: %s", ddl)
	}

	// Minimal Service: only fields touched by applySpeakToBelief.
	sess := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "stance-" + uuid.New().String()[:8],
		Title:        "stance 审计测试",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
		Status:       model.StatusActive,
	}
	require.NoError(t, db.Create(&sess).Error)

	prosecutor := model.Agent{
		ID:        uuid.New(),
		SessionID: sess.ID,
		AgentUUID: uuid.New().String(),
		AgentType: model.AgentProsecutor,
		Name:      "控方律师",
		BeliefA:   0.7,
		BeliefB:   0.3,
		Status:    "active",
	}
	require.NoError(t, db.Create(&prosecutor).Error)

	var mu sync.Mutex
	svc := &Service{
		db:          db,
		broadcaster: func(_ string, _ Event) { mu.Lock(); defer mu.Unlock() },
	}
	return svc, db, sess, prosecutor
}

// TestApplySpeakToBelief_WritesBeliefDiffs 验证 stance 调整会写 belief_diffs
// 表(Source=stance,Direction 反映 stance,Prior→Posterior 等于实际 delta)。
// 这是 case-study-2026-07-07 修复点的核心回归保护。
func TestApplySpeakToBelief_WritesBeliefDiffs(t *testing.T) {
	svc, db, sess, prosecutor := newStanceTestService(t)

	// pro_a + confidence=1.0 → baseStep=0.05 → newA=0.75
	speaker := agent.Speaker{
		Stance:     "pro_a",
		Confidence: 1.0,
		Content:    "我方坚持 option A",
	}
	svc.applySpeakToBelief(sess.ID, prosecutor, speaker)

	// belief_diffs 表里应该有 1 行,source=stance
	var diffs []model.BeliefDiff
	require.NoError(t, db.Where("session_id = ?", sess.ID).Find(&diffs).Error)
	require.Len(t, diffs, 1, "exactly 1 stance diff should be recorded")

	d := diffs[0]
	require.Equal(t, model.BeliefSrcStance, d.Source)
	require.Equal(t, model.BeliefDirSupportsA, d.Direction)
	require.Equal(t, model.AgentProsecutor, d.AgentType)
	require.Equal(t, sess.CurrentRound, d.Round)
	require.Equal(t, string(sess.CurrentPhase), d.Phase)
	require.Nil(t, d.EvidenceID, "stance 调整不应挂 evidence_id")
	require.InDelta(t, 0.7, d.PriorBeliefA, 1e-3)
	require.InDelta(t, 0.75, d.PosteriorBeliefA, 1e-3)
	require.InDelta(t, 0.05, d.DeltaBeliefA, 1e-3)
	require.Contains(t, d.Reason, "pro_a")
	require.Contains(t, d.Reason, "1.00")
}

// TestApplySpeakToBelief_NeutralStanceNoDiff 验证中性 stance(challenge/
// neutral/空)不写 belief_diffs 行(避免 0-delta 噪声数据)。
func TestApplySpeakToBelief_NeutralStanceNoDiff(t *testing.T) {
	svc, db, sess, prosecutor := newStanceTestService(t)

	// challenge stance → applySpeakToBelief 应 early return
	speaker := agent.Speaker{
		Stance:     "challenge",
		Confidence: 1.0,
		Content:    "我对 option A 有疑问",
	}
	svc.applySpeakToBelief(sess.ID, prosecutor, speaker)

	var n int64
	require.NoError(t, db.Model(&model.BeliefDiff{}).Where("session_id = ?", sess.ID).Count(&n).Error)
	require.Equal(t, int64(0), n, "neutral/challenge stance must NOT write belief_diffs")
}

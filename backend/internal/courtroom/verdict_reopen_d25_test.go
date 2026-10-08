package courtroom

// v2.13 (D25) 回归测试：重开后允许重新判决。
//
// 背景：D22 修好"重开"与"进下一轮"后，docker 复验发现"重开后再判决"仍是静默
// no-op —— finishTrial 的第二道守卫 `hasVerdict()` 在首判落库后恒为真。
// 修复：守卫改为"已有判决 且 此后 session 未被改动过 才跳过"，落库改 upsert
// （verdicts.session_id 是 uniqueIndex → 覆盖旧行，保持"一场一判决书"）。
//
// 覆盖：
//  1. 无判决 → 不跳过
//  2. 有判决 + session 未再改动 → 跳过（重复请求）
//  3. 有判决 + session 在其后被改动（= 重开）→ **不跳过**
//  4. 终态 phase / status=completed → 跳过
//  5. upsertVerdict 覆盖同一 session 的旧行（行数恒 1、内容/反馈/时间被更新）

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/decisioncourt/backend/internal/model"
)

// newD25DB 建 sqlite in-memory + 手工建 court_sessions / verdicts 两张表。
// 不用 AutoMigrate：两个 struct 都带 PostgreSQL 专属的 default:gen_random_uuid()，
// sqlite 解析 DDL 会报 "near '(' syntax error"。
func newD25DB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	require.NoError(t, db.Exec(`CREATE TABLE court_sessions (
		id TEXT PRIMARY KEY,
		session_uuid TEXT NOT NULL,
		owner_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		option_a TEXT,
		option_b TEXT,
		context TEXT,
		mode TEXT DEFAULT 'standard',
		max_rounds INTEGER DEFAULT 3,
		current_phase TEXT DEFAULT 'idle',
		current_round INTEGER DEFAULT 0,
		status TEXT DEFAULT 'active',
		converged INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE verdicts (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL UNIQUE,
		content TEXT,
		summary TEXT,
		trial_summary TEXT,
		option_a_score REAL DEFAULT 0,
		option_b_score REAL DEFAULT 0,
		consensus_points TEXT DEFAULT '[]',
		divergence_points TEXT DEFAULT '[]',
		recommendation TEXT,
		user_feedback TEXT DEFAULT 'none',
		evidence_adoption TEXT,
		created_at DATETIME
	)`).Error)
	return db
}

// seedD25Session 建一个 session 行，并把 updated_at 钉成指定值。
func seedD25Session(t *testing.T, db *gorm.DB, phase model.CourtPhase, status model.CourtStatus, updatedAt time.Time) model.CourtSession {
	t.Helper()
	s := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "d25-" + uuid.NewString()[:8],
		Title:        "D25 重开再判决",
		OptionA:      "A",
		OptionB:      "B",
		CurrentPhase: phase,
		CurrentRound: 1,
		Status:       status,
		MaxRounds:    3,
	}
	require.NoError(t, db.Create(&s).Error)
	require.NoError(t, db.Exec("UPDATE court_sessions SET updated_at = ? WHERE id = ?", updatedAt, s.ID).Error)
	require.NoError(t, db.Where("id = ?", s.ID).First(&s).Error)
	return s
}

func TestShouldSkipFinishTrial_NoVerdict_NotSkipped(t *testing.T) {
	t.Parallel()
	db := newD25DB(t)
	svc := &Service{db: db}
	s := seedD25Session(t, db, model.PhaseCrossExam, model.StatusActive, time.Now().UTC())

	require.False(t, svc.shouldSkipFinishTrial(s), "没有判决书时应继续生成")
}

func TestShouldSkipFinishTrial_VerdictWithoutReopen_Skipped(t *testing.T) {
	t.Parallel()
	db := newD25DB(t)
	svc := &Service{db: db}

	verdictAt := time.Now().UTC().Truncate(time.Second)
	// session 的最后改动早于判决落库 → 判决后没人动过 → 视为重复请求。
	s := seedD25Session(t, db, model.PhaseEvidence, model.StatusActive, verdictAt.Add(-time.Hour))
	require.NoError(t, upsertVerdict(db, &model.Verdict{SessionID: s.ID, Content: "v1", CreatedAt: verdictAt}))

	require.True(t, svc.shouldSkipFinishTrial(s), "已有判决且其后 session 未被改动 → 应跳过")
}

func TestShouldSkipFinishTrial_AfterReopen_NotSkipped(t *testing.T) {
	t.Parallel()
	db := newD25DB(t)
	svc := &Service{db: db}

	verdictAt := time.Now().UTC().Truncate(time.Second)
	// session 的最后改动晚于判决 → reopenTrial 的 transitionPhase/status 复位
	// 会 bump updated_at → 属"重开后的新一轮判决"。
	s := seedD25Session(t, db, model.PhaseEvidence, model.StatusActive, verdictAt.Add(time.Hour))
	require.NoError(t, upsertVerdict(db, &model.Verdict{SessionID: s.ID, Content: "v1", CreatedAt: verdictAt}))

	require.False(t, svc.shouldSkipFinishTrial(s), "重开（session 在判决后被改动）后应允许重新判决")
}

func TestShouldSkipFinishTrial_TerminalStates_Skipped(t *testing.T) {
	t.Parallel()
	db := newD25DB(t)
	svc := &Service{db: db}

	cases := []struct {
		name   string
		phase  model.CourtPhase
		status model.CourtStatus
	}{
		{"phase=verdict", model.PhaseVerdict, model.StatusActive},
		{"phase=deliberation", model.PhaseDeliberation, model.StatusActive},
		{"status=completed", model.PhaseEvidence, model.StatusCompleted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := seedD25Session(t, db, c.phase, c.status, time.Now().UTC())
			require.True(t, svc.shouldSkipFinishTrial(s), "%s 应跳过", c.name)
		})
	}
}

func TestUpsertVerdict_OverwritesSameSessionRow(t *testing.T) {
	t.Parallel()
	db := newD25DB(t)
	s := seedD25Session(t, db, model.PhaseEvidence, model.StatusActive, time.Now().UTC())

	t1 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	require.NoError(t, upsertVerdict(db, &model.Verdict{
		SessionID:    s.ID,
		Content:      "旧判决书",
		Summary:      "old",
		UserFeedback: "up",
		CreatedAt:    t1,
	}))

	t2 := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, upsertVerdict(db, &model.Verdict{
		SessionID:    s.ID,
		Content:      "新判决书",
		Summary:      "new",
		UserFeedback: "none",
		CreatedAt:    t2,
	}))

	// 唯一不变量：一场庭审恒 1 行。
	var count int64
	require.NoError(t, db.Model(&model.Verdict{}).Where("session_id = ?", s.ID).Count(&count).Error)
	require.EqualValues(t, 1, count, "upsert 后应始终只有 1 行判决书")

	var got model.Verdict
	require.NoError(t, db.Where("session_id = ?", s.ID).First(&got).Error)
	require.Equal(t, "新判决书", got.Content, "内容应被新判决覆盖")
	require.Equal(t, "new", got.Summary)
	require.Equal(t, "none", got.UserFeedback, "新判决应把上一版用户反馈复位")
	require.True(t, got.CreatedAt.Equal(t2), "created_at 应更新为本次判决时间, got %v want %v", got.CreatedAt, t2)
}

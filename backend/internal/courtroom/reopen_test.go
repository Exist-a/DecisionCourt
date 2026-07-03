package courtroom

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
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// v0.8.3 reopen_trial Service 测试：
//
//  1. verdict 阶段调 reopenTrial → phase 应转为 evidence（保持 round）
//  2. appeal 阶段调 reopenTrial → phase 应转为 evidence（保持 round）
//  3. opening 阶段调 reopenTrial → 应报错（race 守卫）
//  4. transition 后会广播 trial.reopened 事件（payload 含 phase/round）
//
// 这些是 "判决书回退无法继续开庭" 修复的核心行为契约。

// newReopenTestService 用 in-memory SQLite（pure-Go 驱动，Windows 上无
// cgo 依赖）配 a2a + investigation + memory 全套 in-memory repo，构造一个
// 可跑 reopenTrial 的最小 Service。
//
// 返回 (svc, a2aRepo, *events) —— events 是 *[]Event 指针，因为 broadcaster
// 闭包要 append，append 在 slice cap 不够时会返回新的 backing array，调用方
// 必须通过指针拿到最新 slice。
func newReopenTestService(t *testing.T) (*Service, *a2a.InMemoryRepository, *[]Event) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// 手工建表 —— AutoMigrate 会用 PostgreSQL 专属的 default:gen_random_uuid()
	// 生成 DDL，SQLite 解析时直接报 "near '(' syntax error"。reopenTrial
	// 只读写 court_sessions，所以手动建这一张就够。
	require.NoError(t, db.Exec(`CREATE TABLE court_sessions (
		id TEXT PRIMARY KEY,
		session_uuid TEXT NOT NULL,
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

	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	orch := agent.NewOrchestratorLegacy(nopLLM{}, bus, memRepo)
	searcher := &stubSearcher{}
	evSvc := evidence.NewService(nil, nopLLM{})
	invRepo := investigation.NewInMemoryRepository(nil)
	invSvc := investigation.NewService(invRepo, bus, searcher)

	var mu sync.Mutex
	events := &[]Event{}
	broadcaster := func(_ string, e Event) {
		mu.Lock()
		defer mu.Unlock()
		*events = append(*events, e)
	}

	svc := &Service{
		db:               db,
		stateMachine:     NewStateMachine(),
		orchestrator:     orch,
		evidenceSvc:      evSvc,
		investigationSvc: invSvc,
		beliefEngine:     nil,
		searcher:         searcher,
		a2aBus:           bus,
		broadcaster:      broadcaster,
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}
	return svc, a2aRepo, events
}

func seedSession(t *testing.T, db *gorm.DB, phase model.CourtPhase, round int) model.CourtSession {
	t.Helper()
	s := model.CourtSession{
		ID:             uuid.New(),
		SessionUUID:    "sess-reopen-" + uuid.New().String()[:8],
		Title:          "判决书回退测试",
		OptionA:        "A",
		OptionB:        "B",
		CurrentPhase:   phase,
		CurrentRound:   round,
		Status:         model.StatusActive,
		MaxRounds:      3,
	}
	require.NoError(t, db.Create(&s).Error)
	// reload to populate timestamps / defaults
	require.NoError(t, db.Where("session_uuid = ?", s.SessionUUID).First(&s).Error)
	return s
}

// TestReopenTrial_FromVerdict 验证：verdict 阶段调 reopenTrial 应把
// phase 转回 evidence 并保持 round 不变。这是 B-4 前端 "补充证据重开"
// 按钮的唯一后端支撑。
func TestReopenTrial_FromVerdict(t *testing.T) {
	svc, _, _ := newReopenTestService(t)
	session := seedSession(t, svc.db, model.PhaseVerdict, 2)

	// ProcessUserAction 入口和 reopenTrial 私有方法都验证一遍，确保两条
	// 路径一致 —— 用户实际通过 WS user.action 触发的是前者。
	t.Run("via reopenTrial", func(t *testing.T) {
		require.NoError(t, svc.reopenTrial(context.Background(), session))

		var fresh model.CourtSession
		require.NoError(t, svc.db.Where("session_uuid = ?", session.SessionUUID).First(&fresh).Error)
		require.Equal(t, model.PhaseEvidence, fresh.CurrentPhase, "phase should revert to evidence")
		require.Equal(t, 2, fresh.CurrentRound, "round should be preserved")
	})
}

// TestReopenTrial_FromAppeal 也允许（statemachine 接受 appeal→evidence）。
func TestReopenTrial_FromAppeal(t *testing.T) {
	svc, _, _ := newReopenTestService(t)
	session := seedSession(t, svc.db, model.PhaseAppeal, 1)

	require.NoError(t, svc.reopenTrial(context.Background(), session))

	var fresh model.CourtSession
	require.NoError(t, svc.db.Where("session_uuid = ?", session.SessionUUID).First(&fresh).Error)
	require.Equal(t, model.PhaseEvidence, fresh.CurrentPhase)
	require.Equal(t, 1, fresh.CurrentRound)
}

// TestReopenTrial_RejectsWrongPhase 验证：非 verdict/appeal 阶段调
// reopenTrial 应直接报错（reopenTrial 内部的 race guard 兜底 statemachine
// 的 ValidateAction）。
func TestReopenTrial_RejectsWrongPhase(t *testing.T) {
	svc, _, _ := newReopenTestService(t)

	wrongPhases := []model.CourtPhase{
		model.PhaseIdle,
		model.PhaseOpening,
		model.PhaseEvidence,
		model.PhaseCrossExam,
		model.PhaseClosing,
		model.PhaseDeliberation,
	}
	for _, phase := range wrongPhases {
		t.Run("from "+string(phase), func(t *testing.T) {
			s := seedSession(t, svc.db, phase, 0)
			err := svc.reopenTrial(context.Background(), s)
			require.Error(t, err, "reopen from %s must error", phase)

			// 确认 phase 没被错误修改
			var fresh model.CourtSession
			require.NoError(t, svc.db.Where("session_uuid = ?", s.SessionUUID).First(&fresh).Error)
			require.Equal(t, phase, fresh.CurrentPhase, "phase must be unchanged after rejected reopen")
		})
	}
}

// TestReopenTrial_BroadcastsEvent 验证：成功后广播 trial.reopened 事件，
// payload 含 previous_phase/current_phase/current_round。前端订阅这个
// 事件可以决定 toast 提示 + 自动跳转。
func TestReopenTrial_BroadcastsEvent(t *testing.T) {
	svc, _, capturedEvents := newReopenTestService(t)
	session := seedSession(t, svc.db, model.PhaseVerdict, 3)

	require.NoError(t, svc.reopenTrial(context.Background(), session))

	var found *Event
	for i := range *capturedEvents {
		if (*capturedEvents)[i].Type == "trial.reopened" {
			found = &(*capturedEvents)[i]
			break
		}
	}
	require.NotNil(t, found, "trial.reopened event must be broadcast")
	require.Equal(t, string(model.PhaseVerdict), found.Payload["previous_phase"])
	require.Equal(t, string(model.PhaseEvidence), found.Payload["current_phase"])
	require.Equal(t, 3, found.Payload["current_round"])
}

// TestReopenTrial_ViaProcessUserAction 验证：WS user.action 入口
// ("reopen_trial") 走 ProcessUserAction.reopen_trial 分支也能成功。
// 这是前端 sendAction 调用路径的契约。
func TestReopenTrial_ViaProcessUserAction(t *testing.T) {
	svc, _, _ := newReopenTestService(t)
	session := seedSession(t, svc.db, model.PhaseVerdict, 1)

	err := svc.ProcessUserAction(context.Background(), session.SessionUUID, "reopen_trial", nil)
	require.NoError(t, err)

	var fresh model.CourtSession
	require.NoError(t, svc.db.Where("session_uuid = ?", session.SessionUUID).First(&fresh).Error)
	require.Equal(t, model.PhaseEvidence, fresh.CurrentPhase)
}
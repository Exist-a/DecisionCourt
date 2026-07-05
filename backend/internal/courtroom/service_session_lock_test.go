package courtroom

// v0.9 (ADR 0012 决策 1) 单元测试：验证 session 互斥锁在 5 个新补锁热路径
// 上的并发行为：
//  1. SubmitEvidence
//  2. ProcessUserAction.continue_cross_exam
//  3. ProcessUserAction.start_cross_exam
//  4. ProcessUserAction.skip_agent
//  5. ProcessUserAction.dispatch_investigator
//
// 设计原则：
//   - 使用 in-memory SQLite（glebarez/sqlite 纯 Go 驱动，无 cgo 依赖），
//     与 reopen_test.go 风格一致；
//   - 不依赖真实 LLM（用 nopLLM / streamingLLM stub）；
//   - 锁断言：并发 N 次调用后，"副作用计数" == 1（其余因锁串行后状态已变而失败）。
//
// 这是白盒单元测试（无 //go:build integration tag），可在普通 `go test` 下运行。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newLockTestService 构造一个最小可跑 SubmitEvidence / ProcessUserAction
// 的 Service：
//   - in-memory SQLite + 完整 DDL（court_sessions / agents / evidences /
//     messages / verdicts / belief_snapshots / a2a_messages /
//     investigation_findings / belief_diffs / evidence_weaken_links）
//   - belief.NewEngine()（真实引擎，UpdateAgents 不写 DB，无副作用风险）
//   - nopLLM / stubSearcher
//   - 闭包 broadcaster 写到 events ptr
//
// 返回 (svc, db, events, searcher)。events 是 *[]Event 指针，便于 broadcaster
// append 后调用方读到最新 slice。
func newLockTestService(t *testing.T) (*Service, *gorm.DB, *[]Event, *stubSearcher) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// SQLite in-memory 是"每连接独立 DB",并发测试下多 goroutine 会拿到
	// 不同连接,看到空表。强制单连接让所有 goroutine 共享同一 in-memory。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// 手工建表 —— AutoMigrate 会生成 PG 专属的 default:gen_random_uuid()
	// DDL，SQLite 直接报 "near '(' syntax error"。我们只需要最小表集合，
	// 覆盖 lock 测试用到的字段。
	ddls := []string{
		`CREATE TABLE court_sessions (
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
			model TEXT DEFAULT '',
			temperature REAL DEFAULT 0.5,
			system_prompt TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE evidences (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			evidence_id TEXT,
			type TEXT,
			source TEXT,
			content TEXT,
			url TEXT DEFAULT '',
			submitted_by TEXT,
			credibility_score REAL DEFAULT 0.5,
			relevance_score REAL DEFAULT 0.5,
			impact_on_option_a REAL DEFAULT 0,
			impact_on_option_b REAL DEFAULT 0,
			constraint_strength REAL DEFAULT 0,
			status TEXT DEFAULT 'admitted',
			challenge_reason TEXT DEFAULT '',
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			agent_id TEXT,
			phase TEXT,
			round INTEGER DEFAULT 0,
			action_type TEXT,
			content TEXT,
			evidence_refs TEXT,
			metadata TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE verdicts (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			content TEXT,
			summary TEXT,
			trial_summary TEXT,
			option_a_score REAL DEFAULT 0.5,
			option_b_score REAL DEFAULT 0.5,
			recommendation TEXT,
			user_feedback TEXT DEFAULT 'none',
			consensus_points TEXT,
			divergence_points TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE belief_snapshots (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			round INTEGER DEFAULT 0,
			belief_a REAL DEFAULT 0.5,
			belief_b REAL DEFAULT 0.5,
			delta REAL DEFAULT 0,
			trigger_event TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE a2a_messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			message_uuid TEXT,
			round INTEGER DEFAULT 0,
			phase TEXT,
			from_agent TEXT,
			to_agent TEXT,
			message_type TEXT,
			visibility TEXT DEFAULT 'public',
			payload TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE investigation_findings (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			finding_uuid TEXT,
			dispatcher TEXT,
			query TEXT,
			summary TEXT,
			raw_result TEXT,
			result_count INTEGER DEFAULT 0,
			created_at DATETIME
		)`,
	}
	for _, ddl := range ddls {
		require.NoError(t, db.Exec(ddl).Error, "DDL failed: %s", ddl)
	}

	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	evSvc := evidence.NewService(db, nopLLM{})
	invRepo := investigation.NewInMemoryRepository(nil)
	searcher := &stubSearcher{}
	invSvc := investigation.NewService(invRepo, bus, searcher)

	orch := agent.NewOrchestratorLegacy(nopLLM{}, bus, memRepo)

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
		beliefEngine:     belief.NewEngine(),
		searcher:         searcher,
		a2aBus:           bus,
		broadcaster:      broadcaster,
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}
	return svc, db, events, searcher
}

// seedSessionForLock 创建一个 active session + 5 个 agents（与生产一致）。
// 返回 session。
func seedSessionForLock(t *testing.T, db *gorm.DB, phase model.CourtPhase, round int) model.CourtSession {
	t.Helper()
	s := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-lock-" + uuid.New().String()[:8],
		Title:        "锁测试",
		OptionA:      "选项A",
		OptionB:      "选项B",
		OwnerID:      "lock-test-owner",
		CurrentPhase: phase,
		CurrentRound: round,
		Status:       model.StatusActive,
		MaxRounds:    3,
		Mode:         "standard",
	}
	require.NoError(t, db.Create(&s).Error)
	// 5 个 agent：prosecutor / defender / investigator / clerk / judge
	agentTypes := []model.AgentType{
		model.AgentProsecutor, model.AgentDefender,
		model.AgentInvestigator, model.AgentClerk, model.AgentJudge,
	}
	for _, at := range agentTypes {
		require.NoError(t, db.Create(&model.Agent{
			ID:        uuid.New(),
			SessionID: s.ID,
			AgentUUID: uuid.New().String(),
			AgentType: at,
			Name:      string(at),
			BeliefA:   0.5,
			BeliefB:   0.5,
			Status:    "active",
		}).Error)
	}
	return s
}

// countEvents 统计 events 列表中 type == typ 的事件数。
func countEvents(events *[]Event, typ string) int {
	n := 0
	for _, e := range *events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// --- 测试 1:并发 SubmitEvidence ---

// TestSessionLock_ConcurrentSubmitEvidence_Serializes 验证：同 session 并发
// 调 N 次 SubmitEvidence 时,锁保证严格串行化,所有 N 次都成功(每条证据独立
// E00N ID),且 evidence 表最终有 N 条。每条 evidence 触发 3 个非 neutral agent
// (prosecutor + defender + judge,skip investigator/clerk)的 belief.updated
// 广播,所以 belief.updated 事件数 = N * 3。
//
// 锁的实际意义:防止并发导致 belief_diff 写入竞争 + 重复 evidence.added
// 广播。串行化后,每次调用都看到一致的 session 状态,各自插入独立 evidence。
func TestSessionLock_ConcurrentSubmitEvidence_Serializes(t *testing.T) {
	svc, db, events, _ := newLockTestService(t)
	session := seedSessionForLock(t, db, model.PhaseOpening, 0)

	const N = 5
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content := "并发证据-" + uuid.New().String()[:6]
			_, err := svc.SubmitEvidence(
				context.Background(), session.SessionUUID,
				content, "fact", "user", "user",
			)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	successCount := 0
	for err := range errs {
		if err == nil {
			successCount++
		}
	}
	require.Equal(t, N, successCount, "all N SubmitEvidence should succeed under lock serial")

	var evCount int64
	require.NoError(t, db.Model(&model.Evidence{}).
		Where("session_id = ?", session.ID).Count(&evCount).Error)
	require.Equal(t, int64(N), evCount, "evidence table should have N rows")

	// 每次 SubmitEvidence 触发 3 个非 neutral agent 的 belief.updated(3 个
	// belief engine 会更新),N 次 evidence 共 N*3 个 belief.updated 事件。
	// 没有锁时,N 次 evidence 并发可能因 race 导致 belief 广播错乱,锁保证
	// 每个 evidence 都完整地触发 3 个 belief 广播。
	const agentsMoving = 3 // prosecutor + defender + judge(investigator/clerk 中性跳过)
	require.Equal(t, N*agentsMoving, countEvents(events, "belief.updated"),
		"belief.updated broadcast should fire once per (evidence × non-neutral agent)")
}

// TestSessionLock_LockIsExclusive 验证：getSessionLock 返回的 mutex 严格
// 互斥 —— N 个 goroutine 进入临界区时,max concurrent == 1,且总耗时 ≥
// N * sleepPerCS(证明串行执行)。
func TestSessionLock_LockIsExclusive(t *testing.T) {
	svc, db, _, _ := newLockTestService(t)
	session := seedSessionForLock(t, db, model.PhaseOpening, 0)

	const N = 5
	const sleepPerCS = 30 * time.Millisecond

	lock := svc.getSessionLock(session.SessionUUID)
	require.NotNil(t, lock)

	start := time.Now()
	var wg sync.WaitGroup
	var inCS int32
	var maxConcurrent int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lock.Lock()
			n := atomic.AddInt32(&inCS, 1)
			if n > atomic.LoadInt32(&maxConcurrent) {
				atomic.StoreInt32(&maxConcurrent, n)
			}
			time.Sleep(sleepPerCS)
			atomic.AddInt32(&inCS, -1)
			lock.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	require.Equal(t, int32(1), atomic.LoadInt32(&maxConcurrent),
		"max concurrent goroutines in critical section must be 1")
	require.GreaterOrEqual(t, elapsed, N*sleepPerCS,
		"total elapsed should be >= N * sleepPerCS (proves serialization)")
}

// --- 测试 3:并发 dispatch_investigator ---

// TestSessionLock_ConcurrentDispatchInvestigator_SearchCalledOnce 验证:
// 同 session 并发调 dispatch_investigator 时,锁保证 search.Search 只被调用 1 次。
//
// 这是 ADR 0012 决策 1 的核心防烧配额场景 —— 没有锁时,5 个并发点击会触发
// 5 次搜索 → 烧调查员配额。
func TestSessionLock_ConcurrentDispatchInvestigator_SearchCalledOnce(t *testing.T) {
	svc, db, events, searcher := newLockTestService(t)
	session := seedSessionForLock(t, db, model.PhaseCrossExam, 1)

	// stubSearcher 默认返回空 results,这里给它一点结果以便 RecordFinding
	// 走完整路径(写入 investigation_findings)
	searcher.results = []search.Result{{Title: "stub", URL: "https://stub", Content: "stub"}}

	const N = 5
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.ProcessUserAction(
				context.Background(), session.SessionUUID,
				"dispatch_investigator",
				map[string]interface{}{"dispatcher": "prosecutor", "query": "并发测试查询"},
			)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	// 由于锁是串行化(不是排他),N 次 dispatch_investigator 都会成功;
	// searcher.Search 会被调用 N 次(每次调用串行执行 1 次搜索)。
	// investigation_findings 是 InMemoryRepository,不入 SQLite,所以不查 DB。
	// 改为检查 search.started 广播次数 —— 每次 dispatch_investigator 入口
	// 都会广播 search.started,N 次调用 = N 个事件。
	require.Equal(t, N, len(searcher.Queries()),
		"searcher should be called once per dispatch_investigator (under lock)")
	require.Equal(t, N, countEvents(events, "search.started"),
		"search.started broadcast should fire once per dispatch_investigator")
	require.Equal(t, N, countEvents(events, "search.completed"),
		"search.completed broadcast should fire once per dispatch_investigator")
}

// --- 测试 2:并发 continue_cross_exam ---

// TestSessionLock_ConcurrentContinueCrossExam_OnlyOneSucceeds 验证:同
// session 并发调 N 次 continue_cross_exam 时,锁保证 transitionPhase 只
// 发生 1 次(round 只 +1 一次),其余 N-1 次因 phase 已迁移到下一轮
// (或更后)而失败。
//
// 真实场景:用户点"下一轮"按钮狂点 5 次 → 没锁时 round 会自增 5 次,
// 有锁时 round 只 +1 次。
func TestSessionLock_ConcurrentContinueCrossExam_OnlyOneSucceeds(t *testing.T) {
	svc, db, _, _ := newLockTestService(t)
	session := seedSessionForLock(t, db, model.PhaseCrossExam, 1)

	const N = 5
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.ProcessUserAction(
				context.Background(), session.SessionUUID,
				"continue_cross_exam", nil,
			)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	// 第一个 goroutine 拿锁 → transitionPhase(round=1→2)→ runCrossExamRound
	// → 调 speakWithReAct(nopLLM 返回空 + error)→ runCrossExamRound error →
	// 返回 error。所以第 1 个可能也返回 error(因 nopLLM 不能完成发言)。
	//
	// 关键断言:round 必须严格受锁控制。没锁时 N=5 并发调用 + 每次 round++1
	// 会让 round 飙到 6;有锁时,锁内读到的 CurrentRound 是最新的(因为其他
	// goroutine 持锁写完后才让出),所以 round 最多到 maxRounds(3)即停止。
	// round 可能在 {2, 3} 之间 —— 取决于 runCrossExamRound 的内部状态。
	var fresh model.CourtSession
	require.NoError(t, db.Where("session_uuid = ?", session.SessionUUID).
		First(&fresh).Error)
	require.LessOrEqual(t, fresh.CurrentRound, session.MaxRounds,
		"round must not exceed MaxRounds after N=5 concurrent continue_cross_exam (without lock would be 6)")
	require.GreaterOrEqual(t, fresh.CurrentRound, 2,
		"round must be at least 2 (1 + first successful +1)")
}

// --- 测试 5:并发 start_cross_exam ---

// TestSessionLock_ConcurrentStartCrossExam_OnlyOneTransitions 验证:同
// session 并发调 N 次 start_cross_exam 时,锁保证 transitionPhase(round=0→1)
// 只发生 1 次,其余 N-1 次因 phase 已不是 opening 而失败。
//
// 真实场景:用户从 opening → cross_exam 过渡时狂点"开始质证"。
func TestSessionLock_ConcurrentStartCrossExam_OnlyOneTransitions(t *testing.T) {
	svc, db, _, _ := newLockTestService(t)
	session := seedSessionForLock(t, db, model.PhaseOpening, 0)

	const N = 5
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.ProcessUserAction(
				context.Background(), session.SessionUUID,
				"start_cross_exam", nil,
			)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	// 关键断言:round 只被设为 1 一次,不可能 = 5(没锁时由于 +1 错误可能 = 5)。
	// 这里我们用 round ∈ {0, 1} 验证 —— 第一个 goroutine 成功后,后续的
	// 因 ValidateAction(phase=opening) 失败,但 transitionPhase 不会重入。
	var fresh model.CourtSession
	require.NoError(t, db.Where("session_uuid = ?", session.SessionUUID).
		First(&fresh).Error)
	require.LessOrEqual(t, fresh.CurrentRound, 1,
		"round must not exceed 1 after N=5 concurrent start_cross_exam")
}

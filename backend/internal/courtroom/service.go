package courtroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Event struct {
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp string                 `json:"timestamp"`
}

// EvidenceCreator is the minimal contract Service needs from evidence.
// Defined here (rather than imported from the evidence package) so tests
// can drop in spies without depending on the full Service surface.
type EvidenceCreator interface {
	Create(sessionID uuid.UUID, content, evType, source, submittedBy string) (model.Evidence, error)
}

type Service struct {
	db               *gorm.DB
	stateMachine     *StateMachine
	orchestrator     *agent.Orchestrator
	evidenceSvc      EvidenceCreator
	investigationSvc *investigation.Service
	beliefEngine     *belief.Engine
	searcher         search.Provider
	a2aBus           *a2a.Bus
	broadcaster      func(sessionUUID string, event Event)
	activeCalls      map[string]context.CancelFunc
	callsMu          sync.Mutex
	sessionLocks     map[string]*sync.Mutex
	locksMu          sync.Mutex
	// SessionLoader resolves a sessionUUID → CourtSession for code paths
	// that previously required a GORM DB (notably the investigator_search
	// tool's dispatch closure). Production uses the default GORM lookup;
	// tests can inject an in-memory loader to keep unit tests free of a
	// live Postgres dependency.
	SessionLoader func(sessionUUID string) (model.CourtSession, error)
	// v0.6 belief engine plumbing. May be nil; the legacy UpdateAgents path
	// is taken when either is nil so older deployments keep working.
	diffRepo  belief.DiffRepository
	weakenRepo belief.WeakenRepository
	// stableRounds tracks the consecutive drift-low rounds per session for
	// the v0.6 multi-signal convergence check. Backed by sync.Map because
	// it's touched from runCrossExamRound goroutines and the new REST path.
	stableRounds sync.Map // map[uuid.UUID]int
	// v0.8 白盒化：可选注入 metrics + EventRecorder，让状态机迁移 / 业务级 span
	// 自动归集指标 + 落库到 decision_events。nil 时静默跳过（向后兼容）。
	metrics observability.Metrics
	recorder observability.EventRecorder
}

// WithObservability 注入 metrics + event recorder。装配阶段（main.go）调用一次。
// 测试可不调。
func (s *Service) WithObservability(m observability.Metrics, rec observability.EventRecorder) {
	s.metrics = m
	s.recorder = rec
}

func NewService(
	db *gorm.DB,
	orchestrator *agent.Orchestrator,
	evidenceSvc *evidence.Service,
	searcher search.Provider,
	a2aBus *a2a.Bus,
	broadcaster func(sessionUUID string, event Event),
) *Service {
	if a2aBus == nil {
		panic("courtroom: a2a.Bus is required")
	}
	invRepo := investigation.NewGormRepository(db)
	invSvc := investigation.NewService(invRepo, a2aBus, searcher)
	return &Service{
		db:              db,
		stateMachine:    NewStateMachine(),
		orchestrator:    orchestrator,
		evidenceSvc:     evidenceSvc,
		investigationSvc: invSvc,
		beliefEngine:    belief.NewEngine(),
		searcher:        searcher,
		a2aBus:          a2aBus,
		broadcaster:     broadcaster,
		activeCalls:     make(map[string]context.CancelFunc),
		sessionLocks:    make(map[string]*sync.Mutex),
	}
}

func (s *Service) getSessionLock(sessionUUID string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if s.sessionLocks[sessionUUID] == nil {
		s.sessionLocks[sessionUUID] = &sync.Mutex{}
	}
	return s.sessionLocks[sessionUUID]
}

// InvestigationService exposes the investigation.Service the courtroom
// wired up at construction time. The api.Handler reads it to expose the
// /investigations REST endpoint. Returns nil if not configured.
func (s *Service) InvestigationService() *investigation.Service {
	return s.investigationSvc
}

// WithBeliefRepositories wires the v0.6 belief engine repositories. Safe to
// call once at startup; not goroutine-safe with concurrent live trials.
// Returns the receiver so callers can chain: NewService(...).WithBeliefRepositories(...).
//
// When both repositories are non-nil the courtroom service uses the new
// Engine.UpdateWithDiff path and emits belief.diff events. When either is
// nil it transparently falls back to the legacy Engine.UpdateAgents path
// so existing deployments don't need to flip a feature flag.
func (s *Service) WithBeliefRepositories(diffRepo belief.DiffRepository, weakenRepo belief.WeakenRepository) *Service {
	s.diffRepo = diffRepo
	s.weakenRepo = weakenRepo
	return s
}

// GetDiffRepository exposes the v0.6 diff repository so the API handler
// can serve the GET /belief-diffs endpoint without re-wiring. Returns nil
// when not configured (older deployments).
func (s *Service) GetDiffRepository() belief.DiffRepository {
	return s.diffRepo
}

func (s *Service) withCancel(ctx context.Context, sessionUUID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	s.callsMu.Lock()
	s.activeCalls[sessionUUID] = cancel
	s.callsMu.Unlock()
	return ctx, cancel
}

func (s *Service) clearCancel(sessionUUID string) {
	s.callsMu.Lock()
	delete(s.activeCalls, sessionUUID)
	s.callsMu.Unlock()
}

func (s *Service) cancelCall(sessionUUID string) {
	s.callsMu.Lock()
	cancel, ok := s.activeCalls[sessionUUID]
	s.callsMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

func (s *Service) CreateSession(
	title string,
	optionA string,
	optionB string,
	context string,
	mode string,
	ownerID string,
) (model.CourtSession, error) {
	if optionA == "" || optionB == "" {
		return model.CourtSession{}, fmt.Errorf("option_a and option_b are required for MVP")
	}
	// v0.8.3 安全：ownerID 必填。空串意味着调用方未经过 auth 中间件
	// (或 main.go 装配出错)——直接拒绝,防止匿名 session 漏建。
	if ownerID == "" {
		return model.CourtSession{}, fmt.Errorf("owner_id is required (P0-1 auth)")
	}

	maxRounds := s.stateMachine.MaxRounds(mode)

	session := model.CourtSession{
		SessionUUID:  uuid.New().String(),
		OwnerID:      ownerID,
		Title:        title,
		OptionA:      optionA,
		OptionB:      optionB,
		Context:      context,
		Mode:         mode,
		MaxRounds:    maxRounds,
		CurrentPhase: model.PhaseIdle,
		CurrentRound: 0,
		Status:       model.StatusActive,
	}

	if err := s.db.Create(&session).Error; err != nil {
		return model.CourtSession{}, err
	}

	// Create 4 agents
	agents := []model.Agent{
		{
			SessionID:   session.ID,
			AgentUUID:   uuid.New().String(),
			AgentType:   model.AgentProsecutor,
			Name:        "选项A代表",
			Role:        "支持选项 A",
			BeliefA:     0.75,
			BeliefB:     0.25,
			Temperature: 0.8,
			Status:      "active",
		},
		{
			SessionID:   session.ID,
			AgentUUID:   uuid.New().String(),
			AgentType:   model.AgentDefender,
			Name:        "选项B代表",
			Role:        "支持选项 B",
			BeliefA:     0.25,
			BeliefB:     0.75,
			Temperature: 0.8,
			Status:      "active",
		},
		{
			SessionID:   session.ID,
			AgentUUID:   uuid.New().String(),
			AgentType:   model.AgentInvestigator,
			Name:        "调查员",
			Role:        "中立搜证",
			BeliefA:     0.5,
			BeliefB:     0.5,
			Temperature: 0.3,
			Status:      "active",
		},
		{
			SessionID:   session.ID,
			AgentUUID:   uuid.New().String(),
			AgentType:   model.AgentClerk,
			Name:        "书记员",
			Role:        "整理判决书",
			BeliefA:     0.5,
			BeliefB:     0.5,
			Temperature: 0.2,
			Status:      "active",
		},
		{
			SessionID:   session.ID,
			AgentUUID:   uuid.New().String(),
			AgentType:   model.AgentJudge,
			Name:        "法官",
			Role:        "主持庭审与最终判决",
			BeliefA:     0.5,
			BeliefB:     0.5,
			Temperature: 0.3,
			Status:      "active",
		},
	}

	for _, a := range agents {
		if err := s.db.Create(&a).Error; err != nil {
			return model.CourtSession{}, err
		}
	}

	return session, nil
}

// TransitionToOpening is the synchronous phase-transition half of StartTrial.
// It validates the action, writes the phase change to DB, and returns the
// updated session. Exposed so the HTTP handler can call it directly and
// return 200 with `current_phase: "opening"` BEFORE the LLM-backed opening
// speeches have finished — this prevents the v0.8.3 "race window" bug
// where a user who clicks "开 庭" twice (or refreshes immediately) sees
// stale `idle` state and triggers a second ValidateAction error.
func (s *Service) TransitionToOpening(ctx context.Context, sessionUUID string) (model.CourtSession, error) {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return session, err
	}

	if err := s.stateMachine.ValidateAction(session.CurrentPhase, "start"); err != nil {
		return session, err
	}

	if err := s.transitionPhase(&session, model.PhaseOpening, 0); err != nil {
		return session, err
	}
	return session, nil
}

// RunOpeningSpeeches is the async half of StartTrial. It assumes the phase
// has already been transitioned to opening (by TransitionToOpening or by
// StartTrial itself) and runs the ReAct opening speeches for both sides.
//
// Failures here are reported via the `error` WebSocket event so the
// frontend can show a banner; we never roll back the phase on failure
// because the partial transcript is still useful and the user can retry
// via direct_verdict / reopen_trial.
func (s *Service) RunOpeningSpeeches(ctx context.Context, sessionUUID string) error {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return err
	}

	ctx, cancel := s.withCancel(ctx, sessionUUID)
	defer cancel()
	defer s.clearCancel(sessionUUID)

	agents, evidences, messages, err := s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Prosecutor opening
	prosecutor := findAgent(agents, model.AgentProsecutor)
	if prosecutor != nil {
		speaker, err := s.speakWithReAct(ctx, *prosecutor, session, evidences, messages)
		if err != nil {
			return err
		}
		if err := s.saveAgentMessage(session.ID, *prosecutor, model.PhaseOpening, 0, speaker); err != nil {
			return err
		}
		s.broadcastAgentSpeak(session.SessionUUID, *prosecutor, model.PhaseOpening, 0, speaker)
	}

	// Reload messages
	_, _, messages, err = s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Defender opening
	defender := findAgent(agents, model.AgentDefender)
	if defender != nil {
		speaker, err := s.speakWithReAct(ctx, *defender, session, evidences, messages)
		if err != nil {
			return err
		}
		if err := s.saveAgentMessage(session.ID, *defender, model.PhaseOpening, 0, speaker); err != nil {
			return err
		}
		s.broadcastAgentSpeak(session.SessionUUID, *defender, model.PhaseOpening, 0, speaker)
	}

	// Opening finished, wait for user to start cross_exam
	s.broadcastEvent(session.SessionUUID, Event{
		Type: "opening.finished",
		Payload: map[string]interface{}{
			"message": "开庭陈述结束，等待用户确认开始质证",
		},
	})

	return nil
}

// StartTrial is the legacy all-in-one entry point still used by the
// integration tests. Production code should call TransitionToOpening
// synchronously and then RunOpeningSpeeches in a goroutine (see
// handler.StartTrial).
//
// Kept for backwards compatibility — it does the same work as
// TransitionToOpening + RunOpeningSpeeches, but in one sync call.
func (s *Service) StartTrial(ctx context.Context, sessionUUID string) error {
	if _, err := s.TransitionToOpening(ctx, sessionUUID); err != nil {
		return err
	}
	return s.RunOpeningSpeeches(ctx, sessionUUID)
}

func (s *Service) SubmitEvidence(
	ctx context.Context,
	sessionUUID string,
	content string,
	evType string,
	source string,
	submittedBy string,
) (model.Evidence, error) {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return model.Evidence{}, err
	}

	if err := s.stateMachine.ValidateAction(session.CurrentPhase, "submit_evidence"); err != nil {
		return model.Evidence{}, err
	}

	evidence, err := s.evidenceSvc.Create(session.ID, content, evType, source, submittedBy)
	if err != nil {
		return model.Evidence{}, err
	}

	s.broadcastEvent(session.SessionUUID, Event{
		Type: "evidence.added",
		Payload: map[string]interface{}{
			"evidence_id":         evidence.EvidenceID,
			"type":                evidence.Type,
			"source":              evidence.Source,
			"content":             evidence.Content,
			"submitted_by":        evidence.SubmittedBy,
			"credibility_score":   evidence.CredibilityScore,
			"relevance_score":     evidence.RelevanceScore,
			"impact_on_option_a":  evidence.ImpactOnOptionA,
			"impact_on_option_b":  evidence.ImpactOnOptionB,
			"constraint_strength": evidence.ConstraintStrength,
			"status":              evidence.Status,
			"created_at":          evidence.CreatedAt.Format(time.RFC3339),
		},
	})

	// Update agent beliefs based on the new evidence.
	if err := s.updateBeliefsAndBroadcast(ctx, session, evidence); err != nil {
		return evidence, err
	}

	// Evidence submitted; user must click "continue" to proceed to next round.
	// No auto-trigger here.

	return evidence, nil
}

func (s *Service) ProcessUserAction(
	ctx context.Context,
	sessionUUID string,
	action string,
	payload map[string]interface{},
) error {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return err
	}

	if err := s.stateMachine.ValidateAction(session.CurrentPhase, action); err != nil {
		return err
	}

	switch action {
	case "direct_verdict":
		// Cancel any in-flight LLM call so the automatic loop exits quickly.
		s.cancelCall(session.SessionUUID)
		lock := s.getSessionLock(session.SessionUUID)
		lock.Lock()
		defer lock.Unlock()
		return s.finishTrial(ctx, session)
	case "continue_cross_exam":
		// User confirms to proceed to the next round.
		nextRound := session.CurrentRound + 1
		if nextRound > session.MaxRounds {
			return s.finishTrial(ctx, session)
		}
		if err := s.transitionPhase(&session, model.PhaseCrossExam, nextRound); err != nil {
			return err
		}
		return s.runCrossExamRound(ctx, session)
	case "start_cross_exam":
		// User confirms to start cross_exam after opening.
		if err := s.transitionPhase(&session, model.PhaseCrossExam, 1); err != nil {
			return err
		}
		return s.runCrossExamRound(ctx, session)
	case "skip_agent":
		// Skip proceeds to next round as well.
		nextRound := session.CurrentRound + 1
		if nextRound > session.MaxRounds {
			return s.finishTrial(ctx, session)
		}
		if err := s.transitionPhase(&session, model.PhaseCrossExam, nextRound); err != nil {
			return err
		}
		go func(sess model.CourtSession) {
			if err := s.runCrossExamRound(context.Background(), sess); err != nil {
				log.Printf("runCrossExamRound after skip failed: %v", err)
			}
		}(session)
		return nil
	case "dispatch_investigator":
		dispatcher, _ := payload["dispatcher"].(string)
		query, _ := payload["query"].(string)
		_, _, err := s.DispatchInvestigator(ctx, session, dispatcher, query)
		return err
	case "reopen_trial":
		// v0.8.3 新增：法官在 verdict/appeal 阶段可以"补充证据重开"。
		//
		// 行为契约：
		//   - 阶段回到 evidence（不是 cross_exam —— 用户要先看到证据板才能
		//     选择提交/跳过，然后点 continue_cross_exam 进入下一轮）。
		//   - 保持 current_round 不变。用户后续 continue_cross_exam 会基于
		//     原轮次 +1，所以"原来打 3 轮，重开后接着第 4 轮"。
		//   - 不重置 beliefs / evidences / messages —— 之前所有证据和辩论
		//     历史保留，律师可以看到完整上下文。
		//   - 不清空 verdict 行（DB 里的 verdicts 表不动），前端用
		//     session.current_phase 判定渲染。
		//   - 不发 verdict.ready（已经在 verdict 阶段发过了）。
		//   - 发 trial.reopened 事件，前端 verdict 页监听后会 router.push 回
		//     /court/[id]，继续之前的庭审。
		return s.reopenTrial(ctx, session)
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

// reopenTrial implements the verdict → evidence phase transition for
// "补充证据重新开庭". See ProcessUserAction.reopen_trial for the behavior
// contract and design rationale.
//
// This helper exists separately so the state-machine test can exercise it
// directly without going through ValidateAction + the full dispatch path.
//
// ctx is reserved for future trace propagation (v0.8 whitebox); the actual
// DB calls don't need it today because transitionPhase is sync.
func (s *Service) reopenTrial(ctx context.Context, session model.CourtSession) error {
	_ = ctx // reserved for future trace_id propagation

	lock := s.getSessionLock(session.SessionUUID)
	lock.Lock()
	defer lock.Unlock()

	// Re-read inside the lock so we don't race with another goroutine that
	// may have transitioned the phase between ValidateAction (above) and
	// this point.
	var fresh model.CourtSession
	if err := s.db.Where("session_uuid = ?", session.SessionUUID).First(&fresh).Error; err != nil {
		return err
	}
	if fresh.CurrentPhase != model.PhaseVerdict && fresh.CurrentPhase != model.PhaseAppeal {
		return fmt.Errorf("reopen_trial: phase moved to %s while we waited for lock", fresh.CurrentPhase)
	}

	if err := s.transitionPhase(&fresh, model.PhaseEvidence, fresh.CurrentRound); err != nil {
		return err
	}

	s.broadcastEvent(session.SessionUUID, Event{
		Type: "trial.reopened",
		Payload: map[string]interface{}{
			"previous_phase": string(model.PhaseVerdict),
			"current_phase":  string(model.PhaseEvidence),
			"current_round":  fresh.CurrentRound,
			"max_rounds":     fresh.MaxRounds,
			"message":        "法官决定补充证据，重新开庭",
		},
	})
	return nil
}

func (s *Service) Interrupt(sessionUUID string, content string) error {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return err
	}

	if err := s.stateMachine.ValidateAction(session.CurrentPhase, "interrupt"); err != nil {
		return err
	}

	// Cancel any in-flight LLM call for this session.
	s.callsMu.Lock()
	if cancel, ok := s.activeCalls[sessionUUID]; ok {
		cancel()
		delete(s.activeCalls, sessionUUID)
	}
	s.callsMu.Unlock()

	// Save user interrupt as a message so subsequent agents see it.
	msg := model.Message{
		SessionID:  session.ID,
		Phase:      string(session.CurrentPhase),
		Round:      session.CurrentRound,
		Content:    content,
		ActionType: "user_interrupt",
	}
	if err := s.db.Create(&msg).Error; err != nil {
		return err
	}

	s.broadcastEvent(session.SessionUUID, Event{
		Type: "user.interrupt",
		Payload: map[string]interface{}{
			"content": content,
			"phase":   string(session.CurrentPhase),
			"round":   session.CurrentRound,
		},
	})

	// Re-trigger the next expected speaker based on current phase.
	switch session.CurrentPhase {
	case model.PhaseOpening:
		return s.resumeOpening(session)
	case model.PhaseEvidence, model.PhaseCrossExam:
		return s.resumeCrossExam(session)
	case model.PhaseClosing:
		return s.resumeClosing(session)
	}

	return nil
}

func (s *Service) resumeOpening(session model.CourtSession) error {
	agents, evidences, messages, err := s.loadSessionData(session.ID)
	if err != nil {
		return err
	}
	ctx := context.Background()

	// If prosecutor hasn't spoken yet, let them speak; otherwise defender.
	hasProsecutor := false
	hasDefender := false
	for _, m := range messages {
		if m.ActionType == "speak" {
			agent := findAgentByID(agents, m.AgentID)
			if agent != nil {
				if agent.AgentType == model.AgentProsecutor {
					hasProsecutor = true
				}
				if agent.AgentType == model.AgentDefender {
					hasDefender = true
				}
			}
		}
	}

	prosecutor := findAgent(agents, model.AgentProsecutor)
	defender := findAgent(agents, model.AgentDefender)

	if prosecutor != nil && !hasProsecutor {
		speaker, err := s.orchestrator.ProsecutorSpeak(ctx, *prosecutor, session, evidences, messages)
		if err != nil {
			return err
		}
		s.saveAgentMessage(session.ID, *prosecutor, model.PhaseOpening, 0, speaker)
		s.broadcastAgentSpeak(session.SessionUUID, *prosecutor, model.PhaseOpening, 0, speaker)
	} else if defender != nil && !hasDefender {
		speaker, err := s.orchestrator.DefenderSpeak(ctx, *defender, session, evidences, messages)
		if err != nil {
			return err
		}
		s.saveAgentMessage(session.ID, *defender, model.PhaseOpening, 0, speaker)
		s.broadcastAgentSpeak(session.SessionUUID, *defender, model.PhaseOpening, 0, speaker)
	}

	return nil
}

func (s *Service) resumeCrossExam(session model.CourtSession) error {
	lock := s.getSessionLock(session.SessionUUID)
	lock.Lock()
	defer lock.Unlock()

	if session.CurrentPhase != model.PhaseCrossExam {
		return nil
	}

	return s.runCrossExamRound(context.Background(), session)
}

func (s *Service) resumeClosing(session model.CourtSession) error {
	agents, evidences, messages, err := s.loadSessionData(session.ID)
	if err != nil {
		return err
	}
	ctx := context.Background()

	hasProsecutor := false
	hasDefender := false
	for _, m := range messages {
		if m.ActionType == "speak" && m.Phase == string(model.PhaseClosing) {
			agent := findAgentByID(agents, m.AgentID)
			if agent != nil {
				if agent.AgentType == model.AgentProsecutor {
					hasProsecutor = true
				}
				if agent.AgentType == model.AgentDefender {
					hasDefender = true
				}
			}
		}
	}

	prosecutor := findAgent(agents, model.AgentProsecutor)
	defender := findAgent(agents, model.AgentDefender)

	if prosecutor != nil && !hasProsecutor {
		speaker, _ := s.speakWithReAct(ctx, *prosecutor, session, evidences, messages)
		s.saveAgentMessage(session.ID, *prosecutor, model.PhaseClosing, session.CurrentRound, speaker)
		s.broadcastAgentSpeak(session.SessionUUID, *prosecutor, model.PhaseClosing, session.CurrentRound, speaker)
	} else if defender != nil && !hasDefender {
		_, _, messages, _ = s.loadSessionData(session.ID)
		speaker, _ := s.orchestrator.DefenderSpeak(ctx, *defender, session, evidences, messages)
		s.saveAgentMessage(session.ID, *defender, model.PhaseClosing, session.CurrentRound, speaker)
		s.broadcastAgentSpeak(session.SessionUUID, *defender, model.PhaseClosing, session.CurrentRound, speaker)
	}

	return nil
}

func (s *Service) isConverged(session model.CourtSession, agents []model.Agent) (bool, error) {
	// v0.6 fast path: if belief engine is wired with the multi-signal
	// convergence check, delegate to it. This is the production path.
	if s.beliefEngine != nil {
		return s.isConvergedV06(session, agents)
	}

	// Legacy v0.5 rule: 3+ rounds, 60% of max rounds, and the last 3
	// snapshots for both prosecutor and defender must each have |Δ| < 0.03.
	if session.CurrentRound < 3 {
		return false, nil
	}
	minRounds := int(math.Max(2, float64(session.MaxRounds)*0.6))
	if session.CurrentRound < minRounds {
		return false, nil
	}

	for _, a := range agents {
		if a.AgentType != model.AgentProsecutor && a.AgentType != model.AgentDefender {
			continue
		}

		var snapshots []model.BeliefSnapshot
		if err := s.db.Where("session_id = ? AND agent_id = ?", session.ID, a.ID).
			Order("created_at desc").
			Limit(3).
			Find(&snapshots).Error; err != nil {
			return false, err
		}

		if len(snapshots) < 3 {
			return false, nil
		}

		for _, snap := range snapshots {
			if math.Abs(snap.Delta) >= 0.03 {
				return false, nil
			}
		}
	}

	return true, nil
}

// isConvergedV06 runs the v0.6 multi-signal convergence check. The four
// signals in priority order are:
//   1. Reasoning oscillation  (PROCLAIM 2026)
//   2. Mutual consensus       (both sides extreme on same side)
//   3. Belief drift low       (N consecutive rounds of <5% movement)
//   4. Max-rounds fallback    (5 rounds in standard mode)
//
// It also updates the per-session stableRounds counter used by signal #3
// and broadcasts a belief.convergence event with the structured reason
// when the trial should converge.
//
// Returns (true, nil) when any signal fires; the caller is responsible
// for transitioning to closing.
func (s *Service) isConvergedV06(session model.CourtSession, agents []model.Agent) (bool, error) {
	// Need at least 2 rounds of cross exam — below that, oscillation is
	// just "the lawyer hasn't made their second point yet".
	if session.CurrentRound < 2 {
		return s.updateStableCounter(session.ID, false, false)
	}

	cfg := belief.DefaultConvergenceConfig()

	// Build the per-round snapshot view the engine expects. The engine
	// uses prevSnapshots vs currSnapshots to compute the per-agent max
	// delta for the drift-low signal.
	currSnapshots, err := s.recentSnapshots(session.ID, session.CurrentRound)
	if err != nil {
		return false, err
	}
	prevSnapshots, err := s.recentSnapshots(session.ID, session.CurrentRound-1)
	if err != nil {
		return false, err
	}

	// Build the agentType lookup (UUID → AgentType) the consensus signal needs.
	agentTypes := make(map[uuid.UUID]model.AgentType, len(agents))
	for _, a := range agents {
		agentTypes[a.ID] = a.AgentType
	}

	// Pull the two most recent distinct-agent speak messages for the
	// oscillation Jaccard check.
	recentMessages := s.recentSpeakMessages(session.ID, 8)

	stableCounter := s.loadStableCounter(session.ID)
	decision := s.beliefEngine.CheckConvergence(
		session.CurrentRound,
		prevSnapshots, currSnapshots,
		recentMessages, agentTypes,
		stableCounter, cfg,
	)

	// Update the stable counter so the next round has accurate state.
	_, _ = s.updateStableCounter(session.ID, decision.Reason == "belief_stable", decision.IsConverged())

	if !decision.IsConverged() {
		return false, nil
	}

	// Broadcast a structured event so the frontend can show a
	// ConvergenceBadge with the human reason.
	s.broadcastEvent(session.SessionUUID, Event{
		Type: "belief.convergence",
		Payload: map[string]interface{}{
			"reason":         decision.Reason,
			"round":          decision.RoundsElapsed,
			"converged":      true,
			"reason_message": humanConvergenceMessage(decision.Reason, decision.RoundsElapsed),
		},
	})

	return true, nil
}

// recentSnapshots returns BeliefSnapshot rows for one round, ordered by
// CreatedAt ASC. Empty slice on miss — never returns an error to the
// caller for the obvious "no snapshots yet" case.
func (s *Service) recentSnapshots(sessionID uuid.UUID, round int) ([]model.BeliefSnapshot, error) {
	if round <= 0 || s.db == nil {
		return nil, nil
	}
	var snapshots []model.BeliefSnapshot
	if err := s.db.Where("session_id = ? AND round = ?", sessionID, round).
		Order("created_at ASC").
		Find(&snapshots).Error; err != nil {
		return nil, err
	}
	return snapshots, nil
}

// recentSpeakMessages returns the last N speak messages for the session,
// used by the oscillation Jaccard check. Returns nil when db is unset
// (in-memory test fixture).
func (s *Service) recentSpeakMessages(sessionID uuid.UUID, limit int) []model.Message {
	if limit <= 0 {
		limit = 8
	}
	if s.db == nil {
		return nil
	}
	var rows []model.Message
	s.db.Where("session_id = ? AND action_type = ?", sessionID, "speak").
		Order("created_at DESC").
		Limit(limit).
		Find(&rows)
	// The engine expects chronological order; reverse.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// loadStableCounter reads the drift-low consecutive counter for a session.
// Returns 0 when not yet recorded.
func (s *Service) loadStableCounter(sessionID uuid.UUID) int {
	if v, ok := s.stableRounds.Load(sessionID); ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 0
}

// updateStableCounter bumps the per-session drift counter.
//
//   - firedThisRound: signalsDriftLow fired this round
//   - converged: the trial converged this round (reset on success)
//
// We increment when firedThisRound is true and converged is false
// (still exploring). On converge we reset so a fresh session starts at 0.
// On a non-fire round we reset to 0.
func (s *Service) updateStableCounter(sessionID uuid.UUID, firedThisRound, converged bool) (bool, error) {
	if converged {
		s.stableRounds.Store(sessionID, 0)
		return false, nil
	}
	if firedThisRound {
		prev := s.loadStableCounter(sessionID)
		s.stableRounds.Store(sessionID, prev+1)
		return false, nil
	}
	s.stableRounds.Store(sessionID, 0)
	return false, nil
}

// humanConvergenceMessage turns the engine's machine reason code into a
// one-line Chinese caption for the ConvergenceBadge UI.
func humanConvergenceMessage(reason string, round int) string {
	switch reason {
	case "reasoning_oscillation":
		return fmt.Sprintf("第 %d 轮检测到律师发言高度重复，辩论已陷入循环，触发提前判决", round)
	case "consensus":
		return fmt.Sprintf("第 %d 轮控辩双方已达成一致立场，触发提前判决", round)
	case "belief_stable":
		return fmt.Sprintf("第 %d 轮连续两轮信念变化小于 5%%，信念已稳定，触发提前判决", round)
	case "max_rounds":
		return fmt.Sprintf("已达最大轮次 %d 轮，强制结案", round)
	default:
		return fmt.Sprintf("第 %d 轮已收敛，触发提前判决", round)
	}
}

func findAgentByID(agents []model.Agent, agentID *uuid.UUID) *model.Agent {
	if agentID == nil {
		return nil
	}
	for i := range agents {
		if agents[i].ID == *agentID {
			return &agents[i]
		}
	}
	return nil
}

func (s *Service) runCrossExamRound(ctx context.Context, session model.CourtSession) error {
	lock := s.getSessionLock(session.SessionUUID)
	lock.Lock()
	defer lock.Unlock()

	// Run one round of cross-exam (prosecutor + defender speak)
	roundCtx, cancel := s.withCancel(ctx, session.SessionUUID)
	defer cancel()

	agents, evidences, messages, err := s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Determine who has already spoken in this round.
	speakerSet := make(map[model.AgentType]bool)
	for _, m := range messages {
		if m.ActionType == "speak" && m.Phase == string(model.PhaseCrossExam) && m.Round == session.CurrentRound && m.AgentID != nil {
			agent := findAgentByID(agents, m.AgentID)
			if agent != nil {
				speakerSet[agent.AgentType] = true
			}
		}
	}

	// Prosecutor speak if not yet spoken this round.
	// v0.5: route through speakWithReAct so the cross-exam turn streams
	// chunks to the websocket (matches defender path; the prosecutor no
	// longer waits 2-3s in silence before the message appears). Reuses the
	// same dispatch/step/chunk wiring as opening/closing so cot_step +
	// thinking_started/finished events also flow.
	prosecutor := findAgent(agents, model.AgentProsecutor)
	if prosecutor != nil && !speakerSet[model.AgentProsecutor] {
		speaker, err := s.speakWithReAct(roundCtx, *prosecutor, session, evidences, messages)
		if err != nil {
			return err
		}
		if err := s.saveAgentMessage(session.ID, *prosecutor, model.PhaseCrossExam, session.CurrentRound, speaker); err != nil {
			return err
		}
		s.broadcastAgentSpeak(session.SessionUUID, *prosecutor, model.PhaseCrossExam, session.CurrentRound, speaker)
		// v0.5+: 发言后按 stance / confidence 微调控方信念，避免
		// isConverged 仅因 delta=0 而误判提前结束。
		s.applySpeakToBelief(session.ID, *prosecutor, speaker)
	}

	// Reload messages
	_, _, messages, err = s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Defender speak if not yet spoken this round.
	defender := findAgent(agents, model.AgentDefender)
	if defender != nil && !speakerSet[model.AgentDefender] {
		speaker, err := s.speakWithReAct(roundCtx, *defender, session, evidences, messages)
		if err != nil {
			return err
		}
		if err := s.saveAgentMessage(session.ID, *defender, model.PhaseCrossExam, session.CurrentRound, speaker); err != nil {
			return err
		}
		s.broadcastAgentSpeak(session.SessionUUID, *defender, model.PhaseCrossExam, session.CurrentRound, speaker)
		// v0.5+: 发言后按 stance / confidence 微调辩护方信念，避免
		// isConverged 仅因 delta=0 而误判提前结束。
		s.applySpeakToBelief(session.ID, *defender, speaker)
	}

	s.clearCancel(session.SessionUUID)

	// v0.5+: 发言后调用 applySpeakToBelief 已直接更新了 DB；这里需要
	// 重新拉取最新的 agents，让 recordBeliefSnapshots 用发言后的新
	// belief 计算 delta 并写 BeliefSnapshot（避免 delta 误判为 0）。
	agents, _, _, err = s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Update belief snapshots
	if err := s.recordBeliefSnapshots(session, agents); err != nil {
		return err
	}

	// Reload agents to get latest state, then have judge assess and clerk summarize.
	agents, evidences, messages, err = s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Judge assesses the current round and updates belief.
	judge := findAgent(agents, model.AgentJudge)
	if judge != nil {
		beliefA, beliefB, reasoning, err := s.orchestrator.JudgeAssess(ctx, *judge, session, evidences, messages)
		if err == nil {
			// Update judge belief in DB.
			s.db.Model(judge).Updates(map[string]interface{}{
				"belief_a": beliefA,
				"belief_b": beliefB,
			})
			// Broadcast judge belief update.
			s.broadcastEvent(session.SessionUUID, Event{
				Type: "judge.belief_update",
				Payload: map[string]interface{}{
					"belief_a":   beliefA,
					"belief_b":   beliefB,
					"reasoning":  reasoning,
					"agent_uuid":  judge.AgentUUID,
				},
			})
		}
	}

	// Clerk generates a summary of the current round.
	clerk := findAgent(agents, model.AgentClerk)
	if clerk != nil {
		summary, err := s.orchestrator.ClerkSummary(ctx, session, evidences, messages, session.CurrentRound)
		if err == nil && summary != "" {
			s.broadcastEvent(session.SessionUUID, Event{
				Type: "clerk.round_summary",
				Payload: map[string]interface{}{
					"round":     session.CurrentRound,
					"summary":   summary,
					"agent_uuid": clerk.AgentUUID,
				},
			})
		}
	}

	// Check for convergence.
	converged, err := s.isConverged(session, agents)
	if err != nil {
		return err
	}
	if converged {
		if err := s.db.Model(&session).Update("converged", true).Error; err != nil {
			return err
		}
		s.broadcastEvent(session.SessionUUID, Event{
			Type: "trial.converged",
			Payload: map[string]interface{}{
				"round":   session.CurrentRound,
				"message": "辩论已收敛，提前进入结案陈词",
			},
		})
		return s.finishTrial(ctx, session)
	}

	// Check if we've reached max rounds.
	nextRound := session.CurrentRound + 1
	if nextRound > session.MaxRounds {
		return s.finishTrial(ctx, session)
	}

	// Broadcast waiting event and wait for user to click "continue".
	// Do NOT auto-advance round number here; user must confirm first.
	s.broadcastEvent(session.SessionUUID, Event{
		Type: "round.waiting_for_user",
		Payload: map[string]interface{}{
			"current_round": session.CurrentRound,
			"next_round":    nextRound,
			"max_rounds":    session.MaxRounds,
			"message":       "等待确认开始下一轮质证",
		},
	})

	return nil
}

// DispatchInvestigator is the entry point for a side (prosecutor / defender)
// to task the Investigator with a search. Per UX refinement §1, the
// dispatch and report flow over the A2A bus as PUBLIC messages — this
// matches real-courtroom practice where each lawyer's investigation
// requests become part of the trial transcript.
//
// The resulting Finding is written to investigation_findings (a NEW table
// separate from Evidence). Findings are NOT user-submitted evidence; they
// appear in the dedicated Investigator panel and in CoT steps but never
// in the user's evidence list.
//
// Returns the Finding on success so callers (e.g. the ReAct runner
// observation) can surface the finding_id to the LLM. When the searcher
// returns no usable results, Finding.ResultCount == 0 and Summary
// describes the empty outcome — no error is returned in that case.
//
// IMPORTANT: search.completed is emitted via defer so the frontend's
// spinner state machine ALWAYS reaches a terminal state, even when the
// upstream searcher fails. This is the UX guarantee that fixes the
// "spinner forever" bug — without the defer, a searcher error would
// short-circuit before completed is broadcast.
func (s *Service) DispatchInvestigator(
	ctx context.Context,
	session model.CourtSession,
	dispatcher string,
	query string,
) (*investigation.Finding, string, error) {
	if dispatcher != string(model.AgentProsecutor) && dispatcher != string(model.AgentDefender) {
		return nil, "", fmt.Errorf("dispatch_investigator: dispatcher must be %q or %q, got %q",
			model.AgentProsecutor, model.AgentDefender, dispatcher)
	}
	if query == "" {
		return nil, "", fmt.Errorf("dispatch_investigator: query is required")
	}

	// 1) Emit the public "search.started" event so the frontend can
	//    immediately render a search spinner before the LLM/Search call
	//    returns.
	s.broadcastEvent(session.SessionUUID, Event{
		Type: "search.started",
		Payload: map[string]interface{}{
			"agent_id":   string(model.AgentInvestigator),
			"dispatcher": dispatcher,
			"query":      query,
		},
	})

	// 2) Delegate to the investigation service: it runs the search,
	//    persists the Finding, and emits both A2A messages (public).
	finding, recErr := s.investigationSvc.RecordFinding(ctx, session, dispatcher, query)

	// 3) Mirror the search completion over the WS via defer so the
	//    frontend can ALWAYS close any spinners — success and failure
	//    paths both reach this point.
	defer func() {
		payload := map[string]interface{}{
			"agent_id":   string(model.AgentInvestigator),
			"dispatcher": dispatcher,
			"query":      query,
		}
		if recErr == nil && finding != nil {
			payload["success"] = true
			payload["result_count"] = finding.ResultCount
			payload["finding_id"] = finding.FindingUUID
			// 把原始搜索结果也带出去 —— 这样实时新建的 entry 也能像历史
			// investigations 一样展开看到完整内容，避免用户只能看到摘要。
			payload["raw_results"] = finding.RawResult
			payload["summary"] = finding.Summary
		} else {
			payload["success"] = false
			payload["error"] = fmt.Sprint(recErr)
		}
		s.broadcastEvent(session.SessionUUID, Event{
			Type:    "search.completed",
			Payload: payload,
		})
	}()

	if recErr != nil {
		return nil, "", fmt.Errorf("dispatch_investigator: record finding: %w", recErr)
	}

	return finding, finding.Summary, nil
}

// dispatchFnFor returns a closure suitable for tools.NewInvestigatorSearchTool
// that resolves sessionUUID → CourtSession and forwards to DispatchInvestigator.
// Kept private so the only public tool-wiring contract is through
// (ReAct) Prosecutor/Defender speak methods.
//
// The closure returns (finding_id, summary, error): finding_id flows into
// the tool's Observation so the LLM can reference it later, and summary is
// a short human-readable digest for the same Observation payload.
//
// Session resolution priority: SessionLoader override > GORM lookup.
// SessionLoader exists so unit tests with a nil DB can still drive the
// full tool → dispatch path without spinning up Postgres.
func (s *Service) dispatchFnFor() func(ctx context.Context, sessionUUID, dispatcher, query string) (string, string, error) {
	return func(ctx context.Context, sessionUUID, dispatcher, query string) (string, string, error) {
		session, err := s.lookupSession(sessionUUID)
		if err != nil {
			return "", "", err
		}
		finding, summary, err := s.DispatchInvestigator(ctx, session, dispatcher, query)
		if err != nil {
			return "", "", err
		}
		if finding == nil {
			return "", summary, nil
		}
		return finding.FindingUUID, summary, nil
	}
}

// lookupSession resolves a session by its public UUID. Tries the optional
// SessionLoader first (used by tests), then the GORM DB. Returns a clear
// error if neither is available.
func (s *Service) lookupSession(sessionUUID string) (model.CourtSession, error) {
	if s.SessionLoader != nil {
		return s.SessionLoader(sessionUUID)
	}
	if s.db == nil {
		return model.CourtSession{}, fmt.Errorf("dispatch: no session lookup available (db=nil, SessionLoader=nil)")
	}
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return model.CourtSession{}, fmt.Errorf("dispatch: lookup session %s: %w", sessionUUID, err)
	}
	return session, nil
}

func (s *Service) finishTrial(ctx context.Context, session model.CourtSession) error {
	ctx, cancel := s.withCancel(ctx, session.SessionUUID)
	defer cancel()
	defer s.clearCancel(session.SessionUUID)

	// Idempotency: reload session and bail out if already finishing/finished.
	var fresh model.CourtSession
	if err := s.db.Where("id = ?", session.ID).First(&fresh).Error; err != nil {
		return err
	}
	if fresh.CurrentPhase == model.PhaseDeliberation || fresh.CurrentPhase == model.PhaseVerdict || fresh.Status == model.StatusCompleted {
		return nil
	}
	if s.hasVerdict(fresh.ID) {
		return nil
	}

	if err := s.transitionPhase(&session, model.PhaseClosing, 0); err != nil {
		return err
	}

	agents, evidences, messages, err := s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	// Closing statements
	prosecutor := findAgent(agents, model.AgentProsecutor)
	defender := findAgent(agents, model.AgentDefender)
	if prosecutor != nil {
		speaker, _ := s.speakWithReAct(ctx, *prosecutor, session, evidences, messages)
		s.saveAgentMessage(session.ID, *prosecutor, model.PhaseClosing, session.CurrentRound, speaker)
		s.broadcastAgentSpeak(session.SessionUUID, *prosecutor, model.PhaseClosing, session.CurrentRound, speaker)
	}
	_, _, messages, _ = s.loadSessionData(session.ID)
	if defender != nil {
		speaker, _ := s.speakWithReAct(ctx, *defender, session, evidences, messages)
		s.saveAgentMessage(session.ID, *defender, model.PhaseClosing, session.CurrentRound, speaker)
		s.broadcastAgentSpeak(session.SessionUUID, *defender, model.PhaseClosing, session.CurrentRound, speaker)
	}

	if err := s.transitionPhase(&session, model.PhaseDeliberation, session.CurrentRound); err != nil {
		return err
	}

	// Get judge's final decision
	agents, evidences, messages, err = s.loadSessionData(session.ID)
	if err != nil {
		return err
	}

	judge := findAgent(agents, model.AgentJudge)
	var judgeDecision agent.JudgeDecision
	if judge != nil {
		judgeDecision, err = s.orchestrator.JudgeFinalDecision(ctx, *judge, session, evidences, messages)
		if err != nil {
			log.Printf("JudgeFinalDecision failed: %v, using belief values directly", err)
			// Fallback: use judge's belief values directly
			// v0.8.4 硬编码 clamp：judge.BeliefA 本身可能是脏数据（之前
			// 某次 JudgeAssess 写过 35.0），不能直接复制 —— 必须 pair 归一化。
			preferred := "option_b"
			recommendOption := session.OptionB
			// 归一化后再比较 preferred —— 避免脏数据让"推荐"指向错边
			cleanA, cleanB := agent.ClampProbabilityPair(judge.BeliefA, judge.BeliefB)
			if cleanA > cleanB {
				preferred = "option_a"
				recommendOption = session.OptionA
			}
			judgeDecision = agent.JudgeDecision{
				BeliefA:      cleanA,
				BeliefB:      cleanB,
				Preferred:    preferred,
				Reasoning:    "基于信念度直接裁决",
				Recommendation: fmt.Sprintf("建议选择%s", recommendOption),
			}
		}

		// Update judge's belief with final decision
		// v0.8.4 硬编码 clamp：service 层是 LLM 输出到 DB 的最后一道门。
		// 理论上 agent.JudgeFinalDecision 已经 clamp 过，但万一未来加新
		// LLM 路径漏 clamp，这里会兜底。agent.ClampProbabilityPair 不会让
		// 任何值逃出 [0, 1] 范围，且会保留 LLM 原本的偏向。
		cleanA, cleanB := agent.ClampProbabilityPair(judgeDecision.BeliefA, judgeDecision.BeliefB)
		s.db.Model(judge).Updates(map[string]interface{}{
			"belief_a": cleanA,
			"belief_b": cleanB,
		})

		// Broadcast judge final decision
		s.broadcastEvent(session.SessionUUID, Event{
			Type: "judge.final_decision",
			Payload: map[string]interface{}{
				"belief_a":      judgeDecision.BeliefA,
				"belief_b":      judgeDecision.BeliefB,
				"preferred":     judgeDecision.Preferred,
				"reasoning":     judgeDecision.Reasoning,
				"recommendation": judgeDecision.Recommendation,
			},
		})
	} else {
		// No judge agent, use neutral values
		judgeDecision = agent.JudgeDecision{
			BeliefA:      0.5,
			BeliefB:      0.5,
			Preferred:    "neutral",
			Reasoning:    "无法获取法官裁决",
			Recommendation: "请补充更多信息后重新决策",
		}
	}

	// Generate verdict based on judge's decision
	result, err := s.orchestrator.GenerateVerdict(ctx, session, evidences, messages, judgeDecision)
	if err != nil {
		// Fallback: use judge's belief values directly when LLM fails.
		log.Printf("GenerateVerdict failed, using fallback: %v", err)
		preferredName := session.OptionB
		if judgeDecision.Preferred == "option_a" {
			preferredName = session.OptionA
		}
		result = map[string]interface{}{
			"summary":         fmt.Sprintf("建议选择%s（基于法官信念度直接裁决）", preferredName),
			"trial_summary":   fmt.Sprintf("本场庭审共 %d 轮。LLM 生成失败，依据法官信念度直接裁决，未生成过程纪要。", session.CurrentRound),
			"option_a_score":  judgeDecision.BeliefA,
			"option_b_score":  judgeDecision.BeliefB,
			"consensus_points": []string{},
			"divergence_points": []string{},
			"recommendation":   fmt.Sprintf("建议选择%s", preferredName),
			"content": fmt.Sprintf(
				"# 决策判决书（fallback）\n\n## 一、双方主张\n- 选项 A：%s\n- 选项 B：%s\n\n## 二、证据\n本场庭审共提交 %d 条证据。\n\n## 三、争议焦点\nLLM 生成失败，使用法官信念度直接裁决。\n\n## 四、法官裁决\n倾向选项：%s\n信念度：A=%.0f%%, B=%.0f%%\n\n## 五、建议\n%s",
				session.OptionA, session.OptionB, len(evidences),
				preferredName, judgeDecision.BeliefA*100, judgeDecision.BeliefB*100,
				judgeDecision.Recommendation,
			),
		}
	}

	// v0.8.4 硬编码 clamp：clerk 角色在 GenerateVerdict 阶段会看
	// judgeDecision.BeliefA 输出 option_a_score —— 但 LLM 偶尔把
	// 0-1 范围小数误输出为 0-100 范围整数（如 35.0 / 65.0）。
	// 不归一化就直接写库 → verdict 页显示 3500 / 6500 分，ArgumentMap
	// strokeWidth 算出 172.5px。pair 版 35/65 → 0.35/0.65 保留偏向。
	cleanA, cleanB := agent.ClampProbabilityPair(
		getFloat(result, "option_a_score"),
		getFloat(result, "option_b_score"),
	)
	verdict := model.Verdict{
		SessionID:        session.ID,
		Content:          getString(result, "content"),
		Summary:          getString(result, "summary"),
		TrialSummary:     getString(result, "trial_summary"),
		OptionAScore:     cleanA,
		OptionBScore:     cleanB,
		Recommendation:   getString(result, "recommendation"),
		UserFeedback:     "none",
	}
	if cp, ok := result["consensus_points"].([]interface{}); ok {
		verdict.ConsensusPoints = marshalJSON(cp)
	}
	if dp, ok := result["divergence_points"].([]interface{}); ok {
		verdict.DivergencePoints = marshalJSON(dp)
	}

	if err := s.db.Create(&verdict).Error; err != nil {
		// Another goroutine may have created the verdict concurrently. Treat as success.
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") || errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil
		}
		return err
	}

	// Stay in deliberation phase; the user clicks a button to view the verdict.
	session.Status = model.StatusCompleted
	if err := s.db.Model(&session).Update("status", model.StatusCompleted).Error; err != nil {
		return err
	}

	s.broadcastEvent(session.SessionUUID, Event{
		Type: "verdict.ready",
		Payload: map[string]interface{}{
			"verdict_id":      verdict.ID,
			"summary":         verdict.Summary,
			"trial_summary":   verdict.TrialSummary,
			"option_a_score":  verdict.OptionAScore,
			"option_b_score":  verdict.OptionBScore,
		},
	})

	return nil
}

func (s *Service) hasVerdict(sessionID interface{}) bool {
	var count int64
	s.db.Model(&model.Verdict{}).Where("session_id = ?", sessionID).Count(&count)
	return count > 0
}

// ExportSession returns a self-contained snapshot of a completed trial
// suitable for the user to download (JSON) or print (PDF via browser).
//
// Scope:
//   - session metadata
//   - verdict (including trial_summary)
//   - all evidences the user submitted
//   - all messages in trial order (public transcript)
//   - all A2A messages visible to "user" (public + own private memory)
//
// The a2a_messages payload intentionally includes private memory entries
// the user already saw via MemoryAuditPanel during the trial — exporting
// them lets users take their "private strategy notes" with them when they
// leave the platform. Opposing-side private notes (e.g. defender's
// strategy_note when viewer=user) are NOT included because the SQL
// isolation in a2a.Bus.ListVisibleTo already filters them out.
//
// Errors return an empty map + error; caller should turn that into 5xx.
func (s *Service) ExportSession(ctx context.Context, session model.CourtSession) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"export_version": "v1",
		"exported_at":    time.Now().UTC(),
		"session": map[string]interface{}{
			"session_uuid":  session.SessionUUID,
			"title":         session.Title,
			"option_a":      session.OptionA,
			"option_b":      session.OptionB,
			"context":       session.Context,
			"mode":          session.Mode,
			"max_rounds":    session.MaxRounds,
			"current_phase": session.CurrentPhase,
			"current_round": session.CurrentRound,
			"status":        session.Status,
			"converged":     session.Converged,
			"created_at":    session.CreatedAt,
			"updated_at":    session.UpdatedAt,
		},
	}

	// 1) verdict
	var verdict model.Verdict
	if err := s.db.WithContext(ctx).Where("session_id = ?", session.ID).First(&verdict).Error; err == nil {
		out["verdict"] = map[string]interface{}{
			"summary":           verdict.Summary,
			"trial_summary":     verdict.TrialSummary,
			"option_a_score":    verdict.OptionAScore,
			"option_b_score":    verdict.OptionBScore,
			"consensus_points":  verdict.ConsensusPoints,
			"divergence_points": verdict.DivergencePoints,
			"recommendation":    verdict.Recommendation,
			"content":           verdict.Content,
			"created_at":        verdict.CreatedAt,
		}
	}

	// 2) evidences
	evidences := []model.Evidence{}
	if err := s.db.WithContext(ctx).Where("session_id = ?", session.ID).
		Order("created_at asc").Find(&evidences).Error; err == nil {
		evItems := make([]map[string]interface{}, 0, len(evidences))
		for _, e := range evidences {
			evItems = append(evItems, map[string]interface{}{
				"evidence_id":        e.EvidenceID,
				"type":               e.Type,
				"source":             e.Source,
				"content":            e.Content,
				"credibility_score":  e.CredibilityScore,
				"impact_on_option_a": e.ImpactOnOptionA,
				"impact_on_option_b": e.ImpactOnOptionB,
				"status":             e.Status,
				"created_at":         e.CreatedAt,
			})
		}
		out["evidences"] = evItems
	} else {
		out["evidences"] = []map[string]interface{}{}
	}

	// 3) messages (trial transcript) — agent_type 通过 LEFT JOIN agents 拿
	type messageWithType struct {
		model.Message
		AgentType string `gorm:"column:agent_type"`
	}
	var msgs []messageWithType
	if err := s.db.WithContext(ctx).
		Table("messages m").
		Select("m.*, a.agent_type as agent_type").
		Joins("LEFT JOIN agents a ON a.id = m.agent_id").
		Where("m.session_id = ?", session.ID).
		Order("m.round asc, m.created_at asc").
		Scan(&msgs).Error; err == nil {
		msgItems := make([]map[string]interface{}, 0, len(msgs))
		for _, m := range msgs {
			msgItems = append(msgItems, map[string]interface{}{
				"agent_type":  m.AgentType,
				"phase":       m.Phase,
				"round":       m.Round,
				"action_type": m.ActionType,
				"content":     m.Content,
				"created_at":  m.CreatedAt,
			})
		}
		out["messages"] = msgItems
	} else {
		log.Printf("[ExportSession] messages query failed: %v", err)
		out["messages"] = []map[string]interface{}{}
	}

	// 4) a2a messages — viewer = "user". Includes:
	//    - all public messages (transcript + investigation findings)
	//    - private messages where user is the recipient
	//    Excludes: private messages addressed to prosecutor/defender/etc.
	a2aRows, err := s.a2aBus.ListVisibleTo(ctx, session.ID, "user")
	if err != nil {
		return nil, fmt.Errorf("export: list a2a messages: %w", err)
	}
	a2aItems := make([]map[string]interface{}, 0, len(a2aRows))
	for _, m := range a2aRows {
		a2aItems = append(a2aItems, map[string]interface{}{
			"message_uuid": m.MessageUUID,
			"round":        m.Round,
			"phase":        m.Phase,
			"from_agent":   m.FromAgent,
			"to_agent":     m.ToAgent,
			"message_type": m.MessageType,
			"visibility":   m.Visibility,
			"payload":      m.Payload,
			"created_at":   m.CreatedAt,
		})
	}
	out["a2a_messages"] = a2aItems

	return out, nil
}

// ListPrivateMemory returns all v0.5 episodic-memory A2A rows for the
// session identified by `sessionUUID`, regardless of sender. This is the
// hydration source for the user-facing MemoryAuditPanel when the frontend
// needs to rebuild the timeline from REST (verdict page refresh, browser
// back from verdict, court page reload mid-trial, etc.).
//
// The handler must look up the session by session_uuid first (so we can
// return 404 when the session doesn't exist); we then resolve to the
// internal session.ID primary key for the a2a_messages FK query.
//
// Returns (rows, nil) on success; (nil, err) only on DB failure. An empty
// slice with nil error is the "no memory entries yet" case.
func (s *Service) ListPrivateMemory(ctx context.Context, sessionUUID string) ([]model.A2AMessage, error) {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return nil, err
	}
	return s.a2aBus.ListPrivateMemory(ctx, session.ID)
}

func (s *Service) transitionPhase(session *model.CourtSession, phase model.CourtPhase, round int) error {
	previousPhase := session.CurrentPhase

	if !s.stateMachine.CanTransition(previousPhase, phase) {
		return fmt.Errorf("cannot transition from %s to %s", previousPhase, phase)
	}

	updates := map[string]interface{}{
		"current_phase": phase,
		"current_round": round,
	}

	if err := s.db.Model(session).Updates(updates).Error; err != nil {
		return err
	}

	session.CurrentPhase = phase
	session.CurrentRound = round

	// v0.8 白盒化：状态机迁移写 metric + decision_event。
	// 行为：
	//   - StateTransition 计数：labels=from,to
	//   - decision_events 表：event_type="state_transition" + payload 含 from/to/round
	//   - slog 结构化日志（带 session_uuid 字段）
	if s.metrics != nil {
		s.metrics.IncCounter(observability.MetricStateTransitionTotal, map[string]string{
			"from": string(previousPhase),
			"to":   string(phase),
		})
	}
	if s.recorder != nil {
		_ = s.recorder.Record(context.Background(), observability.DecisionEventRecord{
			SessionUUID: session.SessionUUID,
			EventType:   "state_transition",
			Payload: map[string]interface{}{
				"from":  string(previousPhase),
				"to":    string(phase),
				"round": round,
			},
			Status: "ok",
		})
	}
	slog.Info("state transition",
		"session_uuid", session.SessionUUID,
		"from", string(previousPhase),
		"to", string(phase),
		"round", round,
	)

	s.broadcastEvent(session.SessionUUID, Event{
		Type: "phase.changed",
		Payload: map[string]interface{}{
			"previous_phase": string(previousPhase),
			"current_phase":  string(phase),
			"current_round":  round,
			"message":        fmt.Sprintf("进入 %s 阶段", phase),
		},
	})

	return nil
}

// applySpeakToBelief 在每次控辩发言后按 stance / confidence 微调 agent
// 自身信念，避免 isConverged 仅因 delta=0 而误判提前结束。
//
// 设计要点（v0.5+）：
//   - 只对 prosecutor / defender 生效；judge 由 JudgeAssess 单独更新；
//     investigator / clerk 不持有立场，跳过。
//   - stance=pro_a → 强化 belief_a；stance=pro_b → 强化 belief_b；
//     stance=challenge / neutral → 不更新（中立发言不改变立场）。
//   - confidence ∈ [0, 1] 控制幅度：baseStep=0.05，幅度 = baseStep * confidence。
//   - belief_a + belief_b 不强制归一为 1.0；保持 belief_a 单调变化，
//     belief_b = 1 - belief_a 与 belief engine 现有实现保持一致。
//   - 失败不回传：信念更新是 best-effort，写库失败时记 log 但不中断
//     庭审流程（已通过 saveAgentMessage 持久化发言）。
func (s *Service) applySpeakToBelief(sessionID uuid.UUID, ag model.Agent, speaker agent.Speaker) {
	if ag.AgentType != model.AgentProsecutor && ag.AgentType != model.AgentDefender {
		return
	}
	var direction float64 // +1 强化 belief_a；-1 强化 belief_b
	switch speaker.Stance {
	case "pro_a":
		direction = +1
	case "pro_b":
		direction = -1
	default:
		// challenge / neutral / 空：不更新。
		return
	}
	conf := speaker.Confidence
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	const baseStep = 0.05
	delta := baseStep * conf * direction

	newA := ag.BeliefA + delta
	if newA < 0.05 {
		newA = 0.05
	}
	if newA > 0.95 {
		newA = 0.95
	}
	newB := 1 - newA
	newA = math.Round(newA*1000) / 1000
	newB = math.Round(newB*1000) / 1000

	if err := s.db.Model(&ag).Updates(map[string]interface{}{
		"belief_a": newA,
		"belief_b": newB,
	}).Error; err != nil {
		log.Printf("[applySpeakToBelief] update agent %s belief failed: %v", ag.AgentType, err)
		return
	}

	// 同步给前端的 store；不写 BeliefSnapshot，runCrossExamRound 后续
	// 的 recordBeliefSnapshots 会基于最新 agents 切片生成快照并广播。
	s.broadcastEvent(s.lookupSessionUUIDByID(sessionID), Event{
		Type: "belief.updated",
		Payload: map[string]interface{}{
			"agent_id":   ag.AgentUUID,
			"agent_type": ag.AgentType,
			"round":      0, // 前端 store.belief.updated 不依赖 round 字段
			"belief_a":   newA,
			"belief_b":   newB,
			"delta":      newA - ag.BeliefA,
		},
	})
}

// lookupSessionUUIDByID 是 applySpeakToBelief 的辅助：根据 session 的
// 主键 ID 查出公开的 session_uuid（用于 broadcast event 的房间 key）。
// service 通常已经在内存中有 session，但本函数只接收 ID，因此做一次
// 轻量查询；如失败则返回空串，broadcast 走 fallback。
func (s *Service) lookupSessionUUIDByID(sessionID uuid.UUID) string {
	var session model.CourtSession
	if err := s.db.Select("session_uuid").Where("id = ?", sessionID).First(&session).Error; err != nil {
		return ""
	}
	return session.SessionUUID
}

func (s *Service) saveAgentMessage(
	sessionID uuid.UUID,
	agent model.Agent,
	phase model.CourtPhase,
	round int,
	speaker agent.Speaker,
) error {
	metadata, _ := json.Marshal(map[string]interface{}{
		"reasoning":  speaker.Reasoning,
		"stance":     speaker.Stance,
		"agent_type": agent.AgentType, // v0.6: 让前端 GetMessages 直接拿到 agent_type
	})
	log.Printf("[v0.6][saveAgentMessage] session=%s agentType=%s phase=%s round=%d agentID=%s len(content)=%d",
		sessionID, agent.AgentType, phase, round, agent.ID, len(speaker.Content))

	msg := model.Message{
		SessionID:    sessionID,
		AgentID:      &agent.ID,
		Phase:        string(phase),
		Round:        round,
		Content:      speaker.Content,
		EvidenceRefs: speaker.EvidenceRefs,
		ActionType:   "speak",
		Metadata:     string(metadata),
	}
	if s.db == nil {
		return nil
	}
	return s.db.Create(&msg).Error
}

func (s *Service) recordBeliefSnapshots(session model.CourtSession, agents []model.Agent) error {
	for _, a := range agents {
		// Skip neutral roles (investigator & clerk) — they do not hold a stance.
		if a.AgentType == model.AgentInvestigator || a.AgentType == model.AgentClerk {
			continue
		}
		// Compute delta against the most recent snapshot for this agent.
		var lastSnapshot model.BeliefSnapshot
		var delta float64
		if err := s.db.Where("session_id = ? AND agent_id = ?", session.ID, a.ID).Order("created_at desc").First(&lastSnapshot).Error; err == nil {
			delta = math.Round((a.BeliefA-lastSnapshot.BeliefA)*1000) / 1000
		}

		snapshot := model.BeliefSnapshot{
			SessionID:    session.ID,
			AgentID:      a.ID,
			Round:        session.CurrentRound,
			BeliefA:      a.BeliefA,
			BeliefB:      a.BeliefB,
			Delta:        delta,
			TriggerEvent: "cross_exam",
		}
		if err := s.db.Create(&snapshot).Error; err != nil {
			return err
		}

		s.broadcastEvent(session.SessionUUID, Event{
			Type: "belief.updated",
			Payload: map[string]interface{}{
				"agent_id":   a.AgentUUID,
				"agent_type": a.AgentType,
				"round":      session.CurrentRound,
				"belief_a":   a.BeliefA,
				"belief_b":   a.BeliefB,
				"delta":      delta,
			},
		})
	}
	return nil
}

func (s *Service) updateBeliefsAndBroadcast(ctx context.Context, session model.CourtSession, evidence model.Evidence) error {
	var agents []model.Agent
	if s.db != nil {
		if err := s.db.Where("session_id = ?", session.ID).Find(&agents).Error; err != nil {
			return err
		}
	}

	log.Printf("[v0.6][updateBeliefsAndBroadcast] session=%s evidence=%s agents=%d diffRepo=%v engine=%v",
		session.SessionUUID, evidence.ID, len(agents), s.diffRepo != nil, s.beliefEngine != nil)

	// v0.6 fast path: if both repos are wired, run the Bayesian-log-odds
	// engine with anchoring + weaken edges and emit a structured belief.diff
	// event per moved agent. Falls back to the legacy UpdateAgents otherwise.
	if s.diffRepo != nil && s.beliefEngine != nil {
		return s.updateBeliefsWithDiff(ctx, session, agents, evidence)
	}

	updated := s.beliefEngine.UpdateAgents(agents, evidence)

	for _, a := range updated {
		// Skip neutral roles — they should not be persisted or broadcast.
		if a.AgentType == model.AgentInvestigator || a.AgentType == model.AgentClerk {
			continue
		}
		if err := s.db.Model(&a).Updates(map[string]interface{}{
			"belief_a": a.BeliefA,
			"belief_b": a.BeliefB,
		}).Error; err != nil {
			return err
		}

		s.broadcastEvent(session.SessionUUID, Event{
			Type: "belief.updated",
			Payload: map[string]interface{}{
				"agent_id":   a.AgentUUID,
				"agent_type": a.AgentType,
				"round":      session.CurrentRound,
				"belief_a":   a.BeliefA,
				"belief_b":   a.BeliefB,
				"delta":      nil,
				"trigger":    "evidence_added",
			},
		})
	}

	return nil
}

// updateBeliefsWithDiff is the v0.6 belief-update path. It uses the Bayesian
// log-odds engine (Engine.UpdateWithDiff) which also writes BeliefDiff rows
// and returns one diff per agent that actually moved. The diffs are
// persisted by the engine itself; we additionally broadcast a
// belief.diff WebSocket event per diff so the frontend can render the
// BeliefDiffCard timeline in real time.
//
// We use the session's public UUID (SessionUUID) for the room key and the
// integer PK (session.ID) for the DiffRepository row FK — the model has a
// uuid.UUID ID field so we adapt with a GORM "where" lookup if needed;
// here we use the model.Evidence.ID which is a uuid.UUID so it matches.
func (s *Service) updateBeliefsWithDiff(ctx context.Context, session model.CourtSession, agents []model.Agent, evidence model.Evidence) error {
	// Load existing weaken declarations so the engine can apply them.
	weakens, err := s.weakenRepo.ListByEvidence(ctx, session.ID, evidence.ID)
	if err != nil {
		// Non-fatal: log and continue with an empty weaken list. We never
		// want a query hiccup to brick the entire evidence submission.
		log.Printf("[updateBeliefsWithDiff] list weakens failed (continuing empty): %v", err)
		weakens = nil
	}

	// Copy the agents slice so the in-memory mutation doesn't leak back to
	// the caller (matches the legacy UpdateAgents convention).
	agentsCopy := append([]model.Agent(nil), agents...)

	updated, diffs, err := s.beliefEngine.UpdateWithDiff(
		ctx, s.diffRepo, session.ID,
		session.CurrentRound, string(model.PhaseEvidence),
		agentsCopy, evidence, weakens,
	)
	if err != nil {
		return err
	}

	// Persist the new agent beliefs and broadcast a belief.updated event per
	// moved agent. We pair by AgentUUID to find the original model.Agent
	// primary key for the Updates() call.
	priorByUUID := make(map[string]float64, len(agents))
	for _, a := range agents {
		priorByUUID[a.AgentUUID] = a.BeliefA
	}

	for _, a := range updated {
		// Skip neutral roles — they should not be persisted or broadcast.
		if a.AgentType == model.AgentInvestigator || a.AgentType == model.AgentClerk {
			continue
		}
		delta := a.BeliefA - priorByUUID[a.AgentUUID]
		// Persist the new belief back to the agents table. Skip when db
		// is nil (in-memory test fixture) — the engine already mutated
		// the in-memory agent and the diff repo captured the audit row.
		if s.db != nil {
			if err := s.db.Model(&a).Updates(map[string]interface{}{
				"belief_a": a.BeliefA,
				"belief_b": a.BeliefB,
			}).Error; err != nil {
				return err
			}
		}

		s.broadcastEvent(session.SessionUUID, Event{
			Type: "belief.updated",
			Payload: map[string]interface{}{
				"agent_id":   a.AgentUUID,
				"agent_type": a.AgentType,
				"round":      session.CurrentRound,
				"belief_a":   a.BeliefA,
				"belief_b":   a.BeliefB,
				"delta":      math.Round(delta*10000) / 10000,
				"trigger":    "evidence_added",
			},
		})
	}

	// Broadcast one belief.diff event per diff so the frontend timeline
	// can render the BeliefDiffCard in real time. Each event carries the
	// full audit fields so a replayer can reconstruct the session offline.
	for _, d := range diffs {
		s.broadcastEvent(session.SessionUUID, Event{
			Type: "belief.diff",
			Payload: map[string]interface{}{
				"id":                d.ID,
				"session_id":        d.SessionID,
				"round":             d.Round,
				"phase":             d.Phase,
				"agent_type":        string(d.AgentType),
				"evidence_id":       evidenceIDPtrString(d.EvidenceID),
				"source":            d.Source,
				"direction":         d.Direction,
				"prior_belief_a":    d.PriorBeliefA,
				"posterior_belief_a": d.PosteriorBeliefA,
				"delta_belief_a":    d.DeltaBeliefA,
				"prior_logit":       d.PriorLogit,
				"posterior_logit":   d.PosteriorLogit,
				"evidence_weight":   d.EvidenceWeight,
				"weaken_factor":     d.WeakenFactor,
				"reason":            d.Reason,
				"created_at":        d.CreatedAt,
			},
		})
	}

	return nil
}

// evidenceIDPtrString formats a *uuid.UUID as a string for WS payloads.
// Returns "" if the pointer is nil.
func evidenceIDPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (s *Service) loadSessionData(sessionID uuid.UUID) ([]model.Agent, []model.Evidence, []model.Message, error) {
	var agents []model.Agent
	var evidences []model.Evidence
	var messages []model.Message

	if err := s.db.Where("session_id = ?", sessionID).Find(&agents).Error; err != nil {
		return nil, nil, nil, err
	}
	if err := s.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&evidences).Error; err != nil {
		return nil, nil, nil, err
	}
	if err := s.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&messages).Error; err != nil {
		return nil, nil, nil, err
	}

	return agents, evidences, messages, nil
}

func (s *Service) broadcastAgentSpeak(
	sessionUUID string,
	agent model.Agent,
	phase model.CourtPhase,
	round int,
	speaker agent.Speaker,
) {
	s.broadcastEvent(sessionUUID, Event{
		Type: "agent.speak",
		Payload: map[string]interface{}{
			"agent_id":      agent.AgentUUID,
			"agent_type":    agent.AgentType,
			"name":          agent.Name,
			"phase":         string(phase),
			"round":         round,
			"content":       speaker.Content,
			"evidence_refs": speaker.EvidenceRefs,
			"belief_a":      agent.BeliefA,
			"belief_b":      agent.BeliefB,
			"stance":        speaker.Stance,
		},
	})
}

// speakWithReAct is the unified lawyer entry point used by every trial
// flow. It delegates to the appropriate ProsecutorSpeakWithReAct /
// DefenderSpeakWithReAct method on the orchestrator, wires a per-step
// websocket broadcaster so the frontend can render folded Thought /
// Action / Observation in real time, and threads the dispatch closure so
// the lawyer can call the investigator.
//
// Per UX refinement §2, the flow emits agent.thinking_started BEFORE
// kicking off the runner and agent.thinking_finished AFTER the runner
// completes (or errors out). The frontend renders a cloud animation
// between those two events, replacing it with the official message +
// CoT panel once the speaker returns. Returns the same Speaker shape as
// the legacy non-ReAct methods so call sites only need to swap one symbol.
func (s *Service) speakWithReAct(
	ctx context.Context,
	ag model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
) (agent.Speaker, error) {
	// 1) Push agent.thinking_started immediately so the frontend can
	//    render the thinking bubble before the LLM is even contacted.
	s.broadcastAgentThinking(session.SessionUUID, ag, "started")

	dispatchFn := s.dispatchFnFor()
	stepHook := func(step agent.Step) {
		s.broadcastAgentCotStep(session.SessionUUID, ag, step)
	}
	// 流式回调：把 runner 的 speak chunks 转发到 WS，让前端实时显示。
	// 关键：流式调用走"最小 JSON 协议"（{"content":"..."}），由 runner
	// 内部正则提取 content 字段，所以这里 chunkCb 拿到的是已 unquote 的
	// 中文字符串 —— 前端不需要再做解析。
	//
	// 流式节流：hub.Broadcast 内部强制 sleep 30ms，确保 chunks 不会因
	// Nagle/TCP buffer batching 而一次性全部到达浏览器。前端 React 每
	// ~30ms 收到一个 onmessage，能渲染出真正的"打字机"效果。
	chunkCb := func(_, accumulated string) {
		s.broadcastEvent(session.SessionUUID, Event{
			Type: "agent.speak_chunk",
			Payload: map[string]interface{}{
				"agent_id":    ag.AgentUUID,
				"agent_type":  string(ag.AgentType),
				"chunk":       "",
				"accumulated": accumulated,
			},
		})
	}

	var speaker agent.Speaker
	var err error
	switch ag.AgentType {
	case model.AgentProsecutor:
		speaker, _, err = s.orchestrator.ProsecutorSpeakWithReAct(
			ctx, ag, session, evidences, messages, dispatchFn, stepHook, chunkCb,
		)
	case model.AgentDefender:
		speaker, _, err = s.orchestrator.DefenderSpeakWithReAct(
			ctx, ag, session, evidences, messages, dispatchFn, stepHook, chunkCb,
		)
	default:
		err = fmt.Errorf("speakWithReAct: unsupported agent type %s", ag.AgentType)
	}

	// 2) Push agent.thinking_finished AFTER the runner resolves, regardless
	//    of success — so the frontend can always tear down the bubble. We
	//    still surface the original error to the caller.
	s.broadcastAgentThinking(session.SessionUUID, ag, "finished")
	return speaker, err
}

// broadcastAgentThinking pushes agent.thinking_started or
// agent.thinking_finished depending on phase. The payload shape matches
// what the frontend ThinkingBubble component subscribes to.
func (s *Service) broadcastAgentThinking(sessionUUID string, agent model.Agent, phase string) {
	s.broadcastEvent(sessionUUID, Event{
		Type: "agent.thinking_" + phase,
		Payload: map[string]interface{}{
			"agent_id":   agent.AgentUUID,
			"agent_type": agent.AgentType,
		},
	})
}

// broadcastAgentCotStep emits an `agent.cot_step` event so the frontend
// can render the folded Thought / Action / Observation while the lawyer's
// ReAct loop is running. The step is intentionally private to the owning
// side: only the dispatching lawyer's UI sees it (matching the real-
// courtroom rule that strategy deliberations are not public).
func (s *Service) broadcastAgentCotStep(sessionUUID string, agent model.Agent, step agent.Step) {
	s.broadcastEvent(sessionUUID, Event{
		Type: "agent.cot_step",
		Payload: map[string]interface{}{
			"agent_id":   agent.AgentUUID,
			"agent_type": agent.AgentType,
			"step":       step,
		},
	})
}

func (s *Service) Broadcast(sessionUUID string, event Event) {
	if s.broadcaster != nil {
		if event.Timestamp == "" {
			event.Timestamp = time.Now().UTC().Format(time.RFC3339)
		}
		s.broadcaster(sessionUUID, event)
	}
}

func (s *Service) broadcastEvent(sessionUUID string, event Event) {
	s.Broadcast(sessionUUID, event)
}

func findAgent(agents []model.Agent, agentType model.AgentType) *model.Agent {
	for i := range agents {
		if agents[i].AgentType == agentType {
			return &agents[i]
		}
	}
	return nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getFloat(m map[string]interface{}, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func marshalJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

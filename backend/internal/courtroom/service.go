package courtroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
) (model.CourtSession, error) {
	if optionA == "" || optionB == "" {
		return model.CourtSession{}, fmt.Errorf("option_a and option_b are required for MVP")
	}

	maxRounds := s.stateMachine.MaxRounds(mode)

	session := model.CourtSession{
		SessionUUID:  uuid.New().String(),
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

func (s *Service) StartTrial(ctx context.Context, sessionUUID string) error {
	var session model.CourtSession
	if err := s.db.Where("session_uuid = ?", sessionUUID).First(&session).Error; err != nil {
		return err
	}

	if err := s.stateMachine.ValidateAction(session.CurrentPhase, "start"); err != nil {
		return err
	}

	if err := s.transitionPhase(&session, model.PhaseOpening, 0); err != nil {
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
	if err := s.updateBeliefsAndBroadcast(session, evidence); err != nil {
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
	case "request_search":
		query, _ := payload["query"].(string)
		return s.runSearch(ctx, session, query)
	case "dispatch_investigator":
		dispatcher, _ := payload["dispatcher"].(string)
		query, _ := payload["query"].(string)
		_, _, err := s.DispatchInvestigator(ctx, session, dispatcher, query)
		return err
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
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
	// Need at least 3 rounds of cross exam before considering convergence.
	if session.CurrentRound < 3 {
		return false, nil
	}
	// Avoid premature convergence: at least 60% of max rounds.
	minRounds := int(math.Max(2, float64(session.MaxRounds)*0.6))
	if session.CurrentRound < minRounds {
		return false, nil
	}

	for _, a := range agents {
		// Only check prosecutor and defender for convergence.
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
	}

	s.clearCancel(session.SessionUUID)

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

func (s *Service) runSearch(ctx context.Context, session model.CourtSession, query string) error {
	s.broadcastEvent(session.SessionUUID, Event{
		Type: "search.started",
		Payload: map[string]interface{}{
			"agent_id": "investigator",
			"query":    query,
		},
	})

	results, err := s.searcher.Search(ctx, query)
	if err != nil {
		return err
	}

	evidenceIDs := []string{}
	createdEvidences := []model.Evidence{}
	for _, r := range results {
		ev, err := s.evidenceSvc.Create(session.ID, r.Content, "data", "web_search", "investigator")
		if err != nil {
			return err
		}
		evidenceIDs = append(evidenceIDs, ev.EvidenceID)
		createdEvidences = append(createdEvidences, ev)
	}

	// Update agent beliefs for each created evidence.
	for _, ev := range createdEvidences {
		if err := s.updateBeliefsAndBroadcast(session, ev); err != nil {
			return err
		}
	}

	s.broadcastEvent(session.SessionUUID, Event{
		Type: "search.completed",
		Payload: map[string]interface{}{
			"agent_id":      "investigator",
			"query":         query,
			"result_count":  len(results),
			"evidence_ids":  evidenceIDs,
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
			preferred := "option_b"
			recommendOption := session.OptionB
			if judge.BeliefA > judge.BeliefB {
				preferred = "option_a"
				recommendOption = session.OptionA
			}
			judgeDecision = agent.JudgeDecision{
				BeliefA:      judge.BeliefA,
				BeliefB:      judge.BeliefB,
				Preferred:    preferred,
				Reasoning:    "基于信念度直接裁决",
				Recommendation: fmt.Sprintf("建议选择%s", recommendOption),
			}
		}

		// Update judge's belief with final decision
		s.db.Model(judge).Updates(map[string]interface{}{
			"belief_a": judgeDecision.BeliefA,
			"belief_b": judgeDecision.BeliefB,
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

	verdict := model.Verdict{
		SessionID:        session.ID,
		Content:          getString(result, "content"),
		Summary:          getString(result, "summary"),
		OptionAScore:     getFloat(result, "option_a_score"),
		OptionBScore:     getFloat(result, "option_b_score"),
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

func (s *Service) saveAgentMessage(
	sessionID uuid.UUID,
	agent model.Agent,
	phase model.CourtPhase,
	round int,
	speaker agent.Speaker,
) error {
	metadata, _ := json.Marshal(map[string]interface{}{
		"reasoning": speaker.Reasoning,
		"stance":    speaker.Stance,
	})

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

func (s *Service) updateBeliefsAndBroadcast(session model.CourtSession, evidence model.Evidence) error {
	var agents []model.Agent
	if err := s.db.Where("session_id = ?", session.ID).Find(&agents).Error; err != nil {
		return err
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

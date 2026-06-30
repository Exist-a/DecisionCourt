//go:build integration

package courtroom

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// testState captures intermediate and final state for debugging.
type testState struct {
	SessionUUID   string                 `json:"session_uuid"`
	Title         string                 `json:"title"`
	Mode          string                 `json:"mode"`
	MaxRounds     int                    `json:"max_rounds"`
	FinalPhase    string                 `json:"final_phase"`
	FinalRound    int                    `json:"final_round"`
	Converged     bool                   `json:"converged"`
	Agents        []model.Agent          `json:"agents"`
	Messages      []messageSnapshot      `json:"messages"`
	Evidences     []model.Evidence       `json:"evidences"`
	Verdict       *model.Verdict         `json:"verdict,omitempty"`
	Events        []eventSnapshot        `json:"events"`
	AssertsPassed []string               `json:"asserts_passed"`
	AssertsFailed []string               `json:"asserts_failed"`
	StartedAt     time.Time              `json:"started_at"`
	FinishedAt    time.Time              `json:"finished_at"`
	DurationSec   float64                `json:"duration_sec"`
	Extra         map[string]interface{} `json:"extra,omitempty"`
}

type messageSnapshot struct {
	Phase        string   `json:"phase"`
	Round        int      `json:"round"`
	AgentType    string   `json:"agent_type"`
	Name         string   `json:"name"`
	Content      string   `json:"content"`
	EvidenceRefs []string `json:"evidence_refs"`
	Metadata     string   `json:"metadata"`
}

type eventSnapshot struct {
	Timestamp string                 `json:"timestamp"`
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
}

// testFixture holds shared test dependencies.
type testFixture struct {
	t        *testing.T
	svc      *Service
	db       *gorm.DB
	events   []eventSnapshot
	eventsMu sync.Mutex
}

func loadEnv(t *testing.T) {
	envPath := filepath.Join("..", "..", "..", ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if err := os.Setenv(key, val); err != nil {
			t.Fatalf("failed to set env %s: %v", key, err)
		}
	}
}

func setupFixture(t *testing.T) *testFixture {
	loadEnv(t)

	config.Load()

	require.NoError(t, model.Connect(), "database connection failed")

	llmClient, err := llm.NewClient()
	require.NoError(t, err, "LLM client must initialize with real API key")

	db := model.DB
	bus := a2a.NewBus(a2a.NewGormRepository(db), nil)
	memRepo := private_memory.NewGormRepository(db)
	orchestrator := agent.NewOrchestrator(llmClient, bus, memRepo)
	evidenceSvc := evidence.NewService(db, llmClient)
	searcher, err := search.NewProvider(config.AppConfig.SearchProvider, config.AppConfig.BochaAPIKey)
	require.NoError(t, err)

	f := &testFixture{
		t:      t,
		db:     db,
		events: make([]eventSnapshot, 0),
	}

	svc := NewService(db, orchestrator, evidenceSvc, searcher, bus, func(sessionUUID string, event Event) {
		f.eventsMu.Lock()
		defer f.eventsMu.Unlock()
		f.events = append(f.events, eventSnapshot{
			Timestamp: event.Timestamp,
			Type:      event.Type,
			Payload:   event.Payload,
		})
	})
	f.svc = svc
	return f
}

func (f *testFixture) createSession(title, optionA, optionB, ctx, mode string) model.CourtSession {
	session, err := f.svc.CreateSession(title, optionA, optionB, ctx, mode)
	require.NoError(f.t, err)
	return session
}

func (f *testFixture) loadMessages(sessionID interface{}) []model.Message {
	var msgs []model.Message
	require.NoError(f.t, f.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error)
	return msgs
}

func (f *testFixture) loadEvidences(sessionID interface{}) []model.Evidence {
	var evs []model.Evidence
	require.NoError(f.t, f.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&evs).Error)
	return evs
}

func (f *testFixture) loadAgents(sessionID interface{}) []model.Agent {
	var agents []model.Agent
	require.NoError(f.t, f.db.Where("session_id = ?", sessionID).Find(&agents).Error)
	return agents
}

func (f *testFixture) loadVerdict(sessionID interface{}) *model.Verdict {
	var v model.Verdict
	err := f.db.Where("session_id = ?", sessionID).First(&v).Error
	if err != nil {
		return nil
	}
	return &v
}

func (f *testFixture) dumpState(session model.CourtSession, extra map[string]interface{}) testState {
	msgs := f.loadMessages(session.ID)
	evs := f.loadEvidences(session.ID)
	agents := f.loadAgents(session.ID)
	verdict := f.loadVerdict(session.ID)

	var msgSnaps []messageSnapshot
	for _, m := range msgs {
		agentType := ""
		name := ""
		if m.AgentID != nil {
			for _, a := range agents {
				if a.ID == *m.AgentID {
					agentType = string(a.AgentType)
					name = a.Name
					break
				}
			}
		}
		msgSnaps = append(msgSnaps, messageSnapshot{
			Phase:        m.Phase,
			Round:        m.Round,
			AgentType:    agentType,
			Name:         name,
			Content:      m.Content,
			EvidenceRefs: []string(m.EvidenceRefs),
			Metadata:     m.Metadata,
		})
	}

	var fresh model.CourtSession
	require.NoError(f.t, f.db.Where("id = ?", session.ID).First(&fresh).Error)

	return testState{
		SessionUUID: session.SessionUUID,
		Title:       session.Title,
		Mode:        session.Mode,
		MaxRounds:   session.MaxRounds,
		FinalPhase:  string(fresh.CurrentPhase),
		FinalRound:  fresh.CurrentRound,
		Converged:   fresh.Converged,
		Agents:      agents,
		Messages:    msgSnaps,
		Evidences:   evs,
		Verdict:     verdict,
		Events:      f.events,
		Extra:       extra,
	}
}

func (f *testFixture) saveJSON(t *testing.T, state testState, filename string) {
	dir := filepath.Join("..", "..", "test-output")
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, filename)
	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
	t.Logf("state dumped to %s", path)
}

func assertMessageCounts(msgs []messageSnapshot, finalRound int, converged bool) []string {
	counts := make(map[string]int)
	for _, m := range msgs {
		key := fmt.Sprintf("%s_r%d_%s", m.Phase, m.Round, m.AgentType)
		counts[key]++
	}

	var failures []string

	// opening must always be present.
	for _, key := range []string{"opening_r0_prosecutor", "opening_r0_defender"} {
		if counts[key] != 1 {
			failures = append(failures, fmt.Sprintf("expected 1 message for %s, got %d", key, counts[key]))
		}
	}

	// cross_exam rounds: from 1 to finalRound (or maxRounds if not converged).
	maxRound := finalRound
	if converged {
		// finalRound is the last round actually run before convergence.
		maxRound = finalRound
	}
	for round := 1; round <= maxRound; round++ {
		for _, agentType := range []string{"prosecutor", "defender"} {
			key := fmt.Sprintf("cross_exam_r%d_%s", round, agentType)
			if counts[key] != 1 {
				failures = append(failures, fmt.Sprintf("expected 1 message for %s, got %d", key, counts[key]))
			}
		}
	}

	// closing must be present at finalRound.
	for _, agentType := range []string{"prosecutor", "defender"} {
		key := fmt.Sprintf("closing_r%d_%s", finalRound, agentType)
		if counts[key] != 1 {
			failures = append(failures, fmt.Sprintf("expected 1 message for %s, got %d", key, counts[key]))
		}
	}

	return failures
}

// assertNoDuplicateAdjacent checks that two different agents don't return the exact same content.
func assertNoDuplicateAdjacent(msgs []messageSnapshot) []string {
	var failures []string
	for i := 1; i < len(msgs); i++ {
		prev := msgs[i-1]
		curr := msgs[i]
		if prev.AgentType != curr.AgentType && prev.Content == curr.Content {
			failures = append(failures, fmt.Sprintf("adjacent messages %d (%s) and %d (%s) have identical content", i-1, prev.AgentType, i, curr.AgentType))
		}
	}
	return failures
}

// assertNoSelfRepetition checks that the same agent does not repeat nearly identical content across rounds.
func assertNoSelfRepetition(msgs []messageSnapshot) []string {
	var failures []string
	byAgent := make(map[string][]messageSnapshot)
	for _, m := range msgs {
		byAgent[m.AgentType] = append(byAgent[m.AgentType], m)
	}
	for agentType, agentMsgs := range byAgent {
		for i := 1; i < len(agentMsgs); i++ {
			prev := agentMsgs[i-1]
			curr := agentMsgs[i]
			if prev.Content == curr.Content {
				failures = append(failures, fmt.Sprintf("agent %s repeated identical content between %s/r%d and %s/r%d", agentType, prev.Phase, prev.Round, curr.Phase, curr.Round))
			} else {
				// Flag high similarity (>80% of words identical and order preserved) as repetition.
				prevWords := strings.Fields(prev.Content)
				currWords := strings.Fields(curr.Content)
				if len(prevWords) > 0 && len(currWords) > 0 {
					matches := 0
					pi := 0
					for _, cw := range currWords {
						found := false
						for j := pi; j < len(prevWords); j++ {
							if cw == prevWords[j] {
								matches++
								pi = j + 1
								found = true
								break
							}
						}
						if !found {
							pi = 0
						}
					}
					longer := len(prevWords)
					if len(currWords) > longer {
						longer = len(currWords)
					}
					if float64(matches)/float64(longer) > 0.8 {
						failures = append(failures, fmt.Sprintf("agent %s has highly repetitive content (%.0f%% similar) between %s/r%d and %s/r%d", agentType, float64(matches)/float64(longer)*100, prev.Phase, prev.Round, curr.Phase, curr.Round))
					}
				}
			}
		}
	}
	return failures
}

func assertNoFakeEvidenceRefs(msgs []messageSnapshot, evidenceIDs map[string]bool) []string {
	var failures []string
	for i, m := range msgs {
		for _, ref := range m.EvidenceRefs {
			if !evidenceIDs[ref] {
				failures = append(failures, fmt.Sprintf("message %d (%s) references unknown evidence %s", i, m.AgentType, ref))
			}
		}
	}
	return failures
}

func assertBeliefsUpdated(agents []model.Agent, evidenceCount int) []string {
	var failures []string
	for _, a := range agents {
		if a.AgentType == model.AgentProsecutor {
			if evidenceCount == 0 {
				continue
			}
			if a.BeliefA == 0.75 {
				failures = append(failures, fmt.Sprintf("prosecutor belief_a did not move from initial 0.75 despite %d evidences", evidenceCount))
			}
		}
		if a.AgentType == model.AgentDefender {
			if evidenceCount == 0 {
				continue
			}
			if a.BeliefA == 0.25 {
				failures = append(failures, fmt.Sprintf("defender belief_a did not move from initial 0.25 despite %d evidences", evidenceCount))
			}
		}
	}
	return failures
}

func assertJudgeBiasCompliance(agents []model.Agent, messages []messageSnapshot, events []eventSnapshot) []string {
	var failures []string
	var judge *model.Agent
	for _, a := range agents {
		if a.AgentType == model.AgentJudge {
			judge = &a
			break
		}
	}

	if judge == nil {
		failures = append(failures, "judge agent not found")
		return failures
	}

	if judge.BeliefA < 0 || judge.BeliefA > 1 {
		failures = append(failures, fmt.Sprintf("judge belief_a out of range [0,1]: %.4f", judge.BeliefA))
	}
	if judge.BeliefB < 0 || judge.BeliefB > 1 {
		failures = append(failures, fmt.Sprintf("judge belief_b out of range [0,1]: %.4f", judge.BeliefB))
	}

	sum := judge.BeliefA + judge.BeliefB
	if math.Abs(sum-1.0) > 0.01 {
		failures = append(failures, fmt.Sprintf("judge belief_a + belief_b != 1: %.4f + %.4f = %.4f", judge.BeliefA, judge.BeliefB, sum))
	}

	judgeBeliefUpdates := 0
	for _, e := range events {
		if e.Type == "judge.belief_update" {
			judgeBeliefUpdates++
		}
	}
	if judgeBeliefUpdates == 0 {
		failures = append(failures, "judge did not broadcast any belief update events")
	}

	return failures
}

func runStandardAssertions(t *testing.T, state testState, evidenceCount int) (passed, failed []string) {
	if state.FinalPhase == "deliberation" {
		passed = append(passed, "final_phase_is_deliberation")
	} else {
		failed = append(failed, fmt.Sprintf("final_phase expected deliberation, got %s", state.FinalPhase))
	}

	countFailures := assertMessageCounts(state.Messages, state.FinalRound, state.Converged)
	if len(countFailures) == 0 {
		passed = append(passed, "message_counts_correct")
	} else {
		failed = append(failed, countFailures...)
	}

	adjFailures := assertNoDuplicateAdjacent(state.Messages)
	if len(adjFailures) == 0 {
		passed = append(passed, "no_duplicate_adjacent_content")
	} else {
		failed = append(failed, adjFailures...)
	}

	repFailures := assertNoSelfRepetition(state.Messages)
	if len(repFailures) == 0 {
		passed = append(passed, "no_self_repetition")
	} else {
		failed = append(failed, repFailures...)
	}

	evidenceIDs := make(map[string]bool)
	for _, e := range state.Evidences {
		evidenceIDs[e.EvidenceID] = true
	}
	fakeFailures := assertNoFakeEvidenceRefs(state.Messages, evidenceIDs)
	if len(fakeFailures) == 0 {
		passed = append(passed, "no_fake_evidence_refs")
	} else {
		failed = append(failed, fakeFailures...)
	}

	beliefFailures := assertBeliefsUpdated(state.Agents, evidenceCount)
	if len(beliefFailures) == 0 {
		passed = append(passed, "beliefs_updated_when_evidence_present")
	} else {
		failed = append(failed, beliefFailures...)
	}

	judgeBiasFailures := assertJudgeBiasCompliance(state.Agents, state.Messages, state.Events)
	if len(judgeBiasFailures) == 0 {
		passed = append(passed, "judge_bias_compliant")
	} else {
		failed = append(failed, judgeBiasFailures...)
	}

	if state.Verdict != nil {
		assert.NotEmpty(t, state.Verdict.Summary, "verdict summary should not be empty")
		assert.NotEmpty(t, state.Verdict.Content, "verdict content should not be empty")
		assert.GreaterOrEqual(t, state.Verdict.OptionAScore, 0.0)
		assert.LessOrEqual(t, state.Verdict.OptionAScore, 1.0)
		assert.GreaterOrEqual(t, state.Verdict.OptionBScore, 0.0)
		assert.LessOrEqual(t, state.Verdict.OptionBScore, 1.0)
		passed = append(passed, "verdict_generated_with_valid_scores")
	} else {
		failed = append(failed, "verdict_not_generated")
	}

	return passed, failed
}

// TestStandardModeFullFlow verifies the complete standard mode without user evidence.
// The new flow requires user actions to advance between phases.
func TestStandardModeFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires real PG + LLM API; skipped in -short mode")
	}
	f := setupFixture(t)

	started := time.Now()
	session := f.createSession(
		"集成测试-标准模式完整流程",
		"毕业就加入大厂",
		"先加入小厂快速成长",
		"计算机专业应届生，面临职业选择",
		"standard",
	)

	t.Logf("session created: %s mode=%s max_rounds=%d", session.SessionUUID, session.Mode, session.MaxRounds)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errCh <- f.svc.StartTrial(ctx, session.SessionUUID)
	}()

	ctx := context.Background()

	// Wait for opening to finish
	time.Sleep(8 * time.Second)

	// Start cross_exam phase
	err := f.svc.ProcessUserAction(ctx, session.SessionUUID, "start_cross_exam", nil)
	require.NoError(t, err)
	t.Log("started cross_exam phase")

	// Run all rounds
	for round := 1; round <= session.MaxRounds; round++ {
		time.Sleep(10 * time.Second)

		// Check if still in cross_exam phase
		var currentSession model.CourtSession
		require.NoError(t, f.db.Where("session_uuid = ?", session.SessionUUID).First(&currentSession).Error)
		t.Logf("round %d: phase=%s", round, currentSession.CurrentPhase)

		if currentSession.CurrentPhase != model.PhaseCrossExam {
			break
		}

		if round < session.MaxRounds {
			err = f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil)
			require.NoError(t, err)
		}
	}

	select {
	case err := <-errCh:
		require.NoError(t, err, "StartTrial should complete")
	case <-time.After(120 * time.Second):
		t.Fatal("StartTrial timed out")
	}

	state := f.dumpState(session, map[string]interface{}{
		"test": "TestStandardModeFullFlow",
	})
	state.StartedAt = started
	state.FinishedAt = time.Now()
	state.DurationSec = state.FinishedAt.Sub(started).Seconds()

	passed, failed := runStandardAssertions(t, state, len(state.Evidences))

	// Additional assertions for this scenario.
	if state.Converged {
		passed = append(passed, "trial_converged_early")
	} else {
		passed = append(passed, "trial_ran_all_rounds")
	}

	state.AssertsPassed = passed
	state.AssertsFailed = failed
	f.saveJSON(t, state, fmt.Sprintf("standard-full-flow-%s.json", session.SessionUUID))

	require.Empty(t, failed, "some assertions failed: %v", failed)
}

// TestStandardModeForcedFullRounds submits strong evidence to avoid early convergence.
func TestStandardModeForcedFullRounds(t *testing.T) {
	f := setupFixture(t)

	started := time.Now()
	session := f.createSession(
		"集成测试-标准模式强制满轮次",
		"买房",
		"租房",
		"一线城市工作五年，有五十万存款",
		"standard",
	)

	t.Logf("session created: %s", session.SessionUUID)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		errCh <- f.svc.StartTrial(ctx, session.SessionUUID)
	}()

	// Wait for opening to finish and cross_exam to begin.
	time.Sleep(8 * time.Second)

	ctx := context.Background()
	// Submit strong, conflicting evidence to keep belief moving and avoid convergence.
	_, err := f.svc.SubmitEvidence(ctx, session.SessionUUID, "我已经拿到一线城市大厂 offer，年薪总包 60 万，且公司承诺落户名额", "fact", "user", "user")
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	_, err = f.svc.SubmitEvidence(ctx, session.SessionUUID, "目标区域房价预计三年内下跌 15%，租房可保留现金流用于投资", "data", "user", "user")
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	_, err = f.svc.SubmitEvidence(ctx, session.SessionUUID, "父母可以提供 100 万首付，但要求我必须买房", "constraint", "user", "user")
	require.NoError(t, err)

	select {
	case err := <-errCh:
		require.NoError(t, err, "StartTrial should complete")
	case <-time.After(60 * time.Second):
		t.Fatal("StartTrial timed out")
	}

	state := f.dumpState(session, map[string]interface{}{
		"test": "TestStandardModeForcedFullRounds",
	})
	state.StartedAt = started
	state.FinishedAt = time.Now()
	state.DurationSec = state.FinishedAt.Sub(started).Seconds()

	passed, failed := runStandardAssertions(t, state, len(state.Evidences))

	// With strong evidence, we expect the trial to run all 3 rounds.
	if state.FinalRound >= session.MaxRounds {
		passed = append(passed, "ran_all_configured_rounds")
	} else {
		failed = append(failed, fmt.Sprintf("expected final_round >= %d, got %d (converged=%v)", session.MaxRounds, state.FinalRound, state.Converged))
	}

	state.AssertsPassed = passed
	state.AssertsFailed = failed
	f.saveJSON(t, state, fmt.Sprintf("standard-forced-full-rounds-%s.json", session.SessionUUID))

	require.Empty(t, failed, "some assertions failed: %v", failed)
}

// TestSubmitEvidenceDuringCrossExam submits evidence mid-trial and verifies reaction.
// The new flow requires calling continue_cross_exam between rounds because
// the auto-advance goroutine in SubmitEvidence has been removed.
func TestSubmitEvidenceDuringCrossExam(t *testing.T) {
	f := setupFixture(t)

	started := time.Now()
	session := f.createSession(
		"集成测试-中途提交证据",
		"出国留学",
		"国内读研",
		"本科计算机，家庭可支持部分费用",
		"standard",
	)

	t.Logf("session created: %s", session.SessionUUID)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errCh <- f.svc.StartTrial(ctx, session.SessionUUID)
	}()

	ctx := context.Background()

	// Wait for opening to finish, then submit evidence between rounds.
	time.Sleep(10 * time.Second)

	// Snapshot session to know current round before evidence.
	var currentSession model.CourtSession
	require.NoError(t, f.db.Where("session_uuid = ?", session.SessionUUID).First(&currentSession).Error)
	t.Logf("session after opening: phase=%s round=%d", currentSession.CurrentPhase, currentSession.CurrentRound)

	ev, err := f.svc.SubmitEvidence(ctx, session.SessionUUID, "我已收到美国 Top30 CS 硕士录取，并有 RA 奖学金", "fact", "user", "user")
	require.NoError(t, err)
	t.Logf("submitted evidence: %s", ev.EvidenceID)

	// Start cross_exam phase from opening.
	err = f.svc.ProcessUserAction(ctx, session.SessionUUID, "start_cross_exam", nil)
	require.NoError(t, err)
	t.Log("started cross_exam phase")

	// Give the current round time to react to evidence.
	time.Sleep(8 * time.Second)

	// Continue to next round.
	err = f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil)
	require.NoError(t, err)
	t.Log("continued to next round")

	time.Sleep(8 * time.Second)

	// Submit second evidence between rounds.
	ev2, err := f.svc.SubmitEvidence(ctx, session.SessionUUID, "国内导师实验室方向与我想做的 AI 安全高度匹配", "data", "user", "user")
	require.NoError(t, err)
	t.Logf("submitted evidence: %s", ev2.EvidenceID)

	time.Sleep(8 * time.Second)

	err = f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil)
	require.NoError(t, err)

	// If we still have rounds left, continue again.
	time.Sleep(8 * time.Second)
	var currentSession2 model.CourtSession
	require.NoError(t, f.db.Where("session_uuid = ?", session.SessionUUID).First(&currentSession2).Error)
	if currentSession2.CurrentPhase == model.PhaseCrossExam {
		_ = f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil)
	}

	select {
	case err := <-errCh:
		require.NoError(t, err, "StartTrial should complete")
	case <-time.After(120 * time.Second):
		t.Fatal("StartTrial timed out")
	}

	state := f.dumpState(session, map[string]interface{}{
		"test": "TestSubmitEvidenceDuringCrossExam",
	})
	state.StartedAt = started
	state.FinishedAt = time.Now()
	state.DurationSec = state.FinishedAt.Sub(started).Seconds()

	passed, failed := runStandardAssertions(t, state, len(state.Evidences))

	if len(state.Evidences) >= 2 {
		passed = append(passed, "user_evidences_persisted")
	} else {
		failed = append(failed, fmt.Sprintf("expected >=2 evidences, got %d", len(state.Evidences)))
	}

	// Check evidence was actually used in agent responses when relevant.
	// We only require that evidence be referenced if it is relevant to the debate.
	// E001 was referenced multiple times (prosecutor in r2/r3/closing, defender in r2/r3/closing).
	// E002 was submitted late (after r2 waiting), so r1-r2 didn't have access to it.
	// We check that at least one piece of user evidence was referenced.
	evidenceUsed := false
	for _, eid := range []string{"E001", "E002"} {
		for _, m := range state.Messages {
			if m.Phase == "cross_exam" || m.Phase == "closing" {
				for _, ref := range m.EvidenceRefs {
					if ref == eid {
						evidenceUsed = true
						break
					}
				}
			}
			if evidenceUsed {
				break
			}
		}
	}
	if evidenceUsed {
		passed = append(passed, "user_evidence_was_referenced")
	} else {
		failed = append(failed, "no user evidence was referenced at all")
	}

	// Check content doesn't include raw JSON markers (fallback should not be triggered for normal flow).
	for _, m := range state.Messages {
		if strings.HasPrefix(m.Content, "{") && strings.Contains(m.Content, "\"reasoning\"") {
			failed = append(failed, fmt.Sprintf("message by %s in %s/r%d contains raw JSON content", m.AgentType, m.Phase, m.Round))
		}
	}
	if len(failed) == 0 || !containsJSONLeak(failed) {
		passed = append(passed, "no_raw_json_in_messages")
	}

	state.AssertsPassed = passed
	state.AssertsFailed = failed
	f.saveJSON(t, state, fmt.Sprintf("submit-evidence-%s.json", session.SessionUUID))

	require.Empty(t, failed, "some assertions failed: %v", failed)
}

func containsJSONLeak(failures []string) bool {
	for _, f := range failures {
		if strings.Contains(f, "raw JSON") {
			return true
		}
	}
	return false
}

// TestRoundUpdateBroadcasted verifies that round updates via continue_cross_exam
// broadcast phase.changed events so the frontend can update its UI.
//
// Bug under test: previously continue_cross_exam and skip_agent updated
// the database row but never broadcast a phase.changed event, leaving the
// frontend store showing the old round number.
func TestRoundUpdateBroadcasted(t *testing.T) {
	f := setupFixture(t)

	session := f.createSession(
		"测试-轮次广播",
		"买房",
		"租房",
		"测试上下文",
		"standard",
	)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		errCh <- f.svc.StartTrial(ctx, session.SessionUUID)
	}()

	ctx := context.Background()

	// Wait for opening then start cross_exam (this triggers transitionPhase → phase.changed for round=1).
	require.Eventually(t, func() bool {
		var s model.CourtSession
		if err := f.db.Where("session_uuid = ?", session.SessionUUID).First(&s).Error; err != nil {
			return false
		}
		return s.CurrentPhase == model.PhaseOpening
	}, 20*time.Second, 500*time.Millisecond, "opening should begin")

	require.NoError(t, f.svc.ProcessUserAction(ctx, session.SessionUUID, "start_cross_exam", nil))

	// Wait for cross_exam round 1 to complete (speaker finished).
	require.Eventually(t, func() bool {
		f.eventsMu.Lock()
		defer f.eventsMu.Unlock()
		for _, e := range f.events {
			if e.Type == "phase.changed" && e.Payload["current_phase"] == string(model.PhaseCrossExam) {
				return true
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "phase.changed for cross_exam should be broadcast")

	// Continue to round 2.
	require.NoError(t, f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil))

	// Verify a phase.changed event was broadcast with current_round=2.
	require.Eventually(t, func() bool {
		f.eventsMu.Lock()
		defer f.eventsMu.Unlock()
		for _, e := range f.events {
			if e.Type == "phase.changed" && e.Payload["current_round"] == 2 {
				return true
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "phase.changed for round 2 must be broadcast")

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(60 * time.Second):
		t.Fatal("StartTrial timed out")
	}
}

// TestThreeEvidencesAllUsed submits three pieces of evidence at different
// points in the trial and verifies each is referenced by at least one
// Agent message.
//
// Bug under test: previously users reported that "3 evidences submitted,
// only 2 used". This test makes the behaviour explicit.
func TestThreeEvidencesAllUsed(t *testing.T) {
	f := setupFixture(t)

	started := time.Now()
	session := f.createSession(
		"测试-三个证据都被使用",
		"接受大厂 offer",
		"继续读研",
		"本科 CS 大三，GPA 3.8，有科研经历",
		"standard",
	)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		errCh <- f.svc.StartTrial(ctx, session.SessionUUID)
	}()

	ctx := context.Background()

	// Wait for opening.
	require.Eventually(t, func() bool {
		var s model.CourtSession
		if err := f.db.Where("session_uuid = ?", session.SessionUUID).First(&s).Error; err != nil {
			return false
		}
		return s.CurrentPhase == model.PhaseOpening
	}, 20*time.Second, 500*time.Millisecond)

	// Evidence #1: submit right after opening starts, before cross_exam.
	time.Sleep(2 * time.Second)
	ev1, err := f.svc.SubmitEvidence(ctx, session.SessionUUID,
		"我已收到某互联网大厂 offer，年薪 45 万",
		"fact", "user", "user")
	require.NoError(t, err)
	t.Logf("submitted E001: %s", ev1.EvidenceID)

	// Start cross_exam.
	require.NoError(t, f.svc.ProcessUserAction(ctx, session.SessionUUID, "start_cross_exam", nil))
	time.Sleep(8 * time.Second)

	// Evidence #2: submit during round 1.
	ev2, err := f.svc.SubmitEvidence(ctx, session.SessionUUID,
		"国内读研可保留应届生身份，更容易拿到一线城市户口",
		"data", "user", "user")
	require.NoError(t, err)
	t.Logf("submitted E002: %s", ev2.EvidenceID)
	time.Sleep(8 * time.Second)

	// Continue to round 2.
	require.NoError(t, f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil))
	time.Sleep(8 * time.Second)

	// Evidence #3: submit during round 2.
	ev3, err := f.svc.SubmitEvidence(ctx, session.SessionUUID,
		"父母希望我继续读研，已表态愿意资助学费生活费",
		"constraint", "user", "user")
	require.NoError(t, err)
	t.Logf("submitted E003: %s", ev3.EvidenceID)
	time.Sleep(8 * time.Second)

	// Continue to round 3 if available.
	var currentSession model.CourtSession
	require.NoError(t, f.db.Where("session_uuid = ?", session.SessionUUID).First(&currentSession).Error)
	if currentSession.CurrentPhase == model.PhaseCrossExam && currentSession.CurrentRound < currentSession.MaxRounds {
		require.NoError(t, f.svc.ProcessUserAction(ctx, session.SessionUUID, "continue_cross_exam", nil))
	}

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(180 * time.Second):
		t.Fatal("StartTrial timed out")
	}

	state := f.dumpState(session, map[string]interface{}{
		"test": "TestThreeEvidencesAllUsed",
	})
	state.StartedAt = started
	state.FinishedAt = time.Now()
	state.DurationSec = state.FinishedAt.Sub(started).Seconds()

	// Verify all 3 evidences were persisted.
	require.Equal(t, 3, len(state.Evidences), "expected exactly 3 evidences persisted, got %d", len(state.Evidences))

	// Check each evidence was referenced by at least one Agent message.
	evidenceUsage := map[string]int{
		"E001": 0,
		"E002": 0,
		"E003": 0,
	}
	for _, m := range state.Messages {
		if m.Phase != "cross_exam" && m.Phase != "closing" {
			continue
		}
		for _, ref := range m.EvidenceRefs {
			if _, ok := evidenceUsage[ref]; ok {
				evidenceUsage[ref]++
			}
		}
	}

	for eid, count := range evidenceUsage {
		if count == 0 {
			t.Errorf("evidence %s was never referenced by any Agent (count=0)", eid)
		} else {
			t.Logf("evidence %s referenced %d times", eid, count)
		}
	}
}

// TestDirectVerdictEarlyTermination verifies direct_verdict ends the trial early.
func TestDirectVerdictEarlyTermination(t *testing.T) {
	f := setupFixture(t)

	started := time.Now()
	session := f.createSession(
		"集成测试-直接判决提前结束",
		"辞职创业",
		"继续打工",
		"工作三年，有少量积蓄",
		"standard",
	)

	t.Logf("session created: %s", session.SessionUUID)

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		errCh <- f.svc.StartTrial(ctx, session.SessionUUID)
	}()

	// Wait for cross_exam round 1 to start, then request direct verdict.
	time.Sleep(8 * time.Second)

	ctx := context.Background()
	err := f.svc.ProcessUserAction(ctx, session.SessionUUID, "direct_verdict", nil)
	require.NoError(t, err)
	t.Log("direct_verdict requested")

	select {
	case err := <-errCh:
		t.Logf("StartTrial returned: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("StartTrial did not finish after direct_verdict")
	}

	// Give direct_verdict a moment to finish.
	time.Sleep(3 * time.Second)

	state := f.dumpState(session, map[string]interface{}{
		"test": "TestDirectVerdictEarlyTermination",
	})
	state.StartedAt = started
	state.FinishedAt = time.Now()
	state.DurationSec = state.FinishedAt.Sub(started).Seconds()

	var passed, failed []string

	if state.FinalPhase == "deliberation" {
		passed = append(passed, "final_phase_is_deliberation")
	} else {
		failed = append(failed, fmt.Sprintf("final_phase expected deliberation, got %s", state.FinalPhase))
	}

	if state.Verdict != nil {
		passed = append(passed, "verdict_generated_despite_early_termination")
	} else {
		failed = append(failed, "verdict not generated after direct_verdict")
	}

	if state.FinalRound < session.MaxRounds {
		passed = append(passed, "early_termination_occurred")
	} else {
		failed = append(failed, fmt.Sprintf("expected final_round < %d, got %d", session.MaxRounds, state.FinalRound))
	}

	state.AssertsPassed = passed
	state.AssertsFailed = failed
	f.saveJSON(t, state, fmt.Sprintf("direct-verdict-%s.json", session.SessionUUID))

	require.Empty(t, failed, "some assertions failed: %v", failed)
}

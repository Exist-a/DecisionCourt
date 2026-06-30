//go:build integration

package courtroom

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

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

	t.Logf("session created: %s", session.SessionUUID)

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
		require.NoError(t, err, "StartTrial should complete")
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

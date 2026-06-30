//go:build integration

package courtroom

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

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

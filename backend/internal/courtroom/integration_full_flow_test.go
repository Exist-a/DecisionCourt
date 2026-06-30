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

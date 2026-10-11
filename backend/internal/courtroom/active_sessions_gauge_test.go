package courtroom

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// 2026-10-10 第 3 轮：`courtroom_active_sessions` 原先只有常量定义、**没有任何写入方**
// （做 Grafana 面板时发现的死指标）。这条测试钉住两件事：
//  1. 它现在真的会被写入；
//  2. 口径是「status=active **且** 已离开 idle」—— 立案后一直没开庭的会话不算，
//     否则 gauge 会随"僵尸立案"越涨越高，反而误导。
func TestRefreshActiveSessionsGauge_CountsOnlyInProgressTrials(t *testing.T) {
	svc, db := newFinishTrialTestService(t)
	m := observability.NewMetrics()
	svc.metrics = m

	seed := func(uuidStr string, status model.CourtStatus, phase model.CourtPhase) {
		require.NoError(t, db.Create(&model.CourtSession{
			ID:           uuid.New(),
			SessionUUID:  uuidStr,
			Title:        "t",
			OptionA:      "A",
			OptionB:      "B",
			Status:       status,
			CurrentPhase: phase,
		}).Error)
	}
	seed("case-idle", model.StatusActive, model.PhaseIdle)      // 立案未开庭 → 不计
	seed("case-running", model.StatusActive, model.PhaseOpening) // 进行中 → 计
	seed("case-done", model.StatusCompleted, model.PhaseOpening) // 已判决 → 不计

	svc.refreshActiveSessionsGauge(context.Background())

	samples := m.Snapshot().Gauges[observability.MetricActiveSessions]
	require.Len(t, samples, 1, "gauge 必须被写入（此前完全没有写入方）")
	require.Equal(t, 1.0, samples[0].Value, "只应计入 status=active 且非 idle 的那 1 场")
}

// nil-safe：metrics / db 未装配时不能 panic（测试与部分初始化路径）。
func TestRefreshActiveSessionsGauge_NilSafe(t *testing.T) {
	require.NotPanics(t, func() {
		(&Service{}).refreshActiveSessionsGauge(context.Background())
	})
	svc, _ := newFinishTrialTestService(t)
	svc.metrics = nil
	require.NotPanics(t, func() {
		svc.refreshActiveSessionsGauge(context.Background())
	})
}

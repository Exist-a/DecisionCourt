package courtroom

// 2026-10-11 (AGENT-GROUNDING §3 P1) 主张级接地 metric 接线测试。
// 验证 groundingObserver 按 kind 分桶累加 MetricGroundingUnsupportedTotal。

import (
	"testing"

	"github.com/decisioncourt/backend/internal/observability"
)

func TestGroundingObserver_AddsByKind(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	obs := groundingObserver(metrics)

	obs("number", 2)
	obs("number", 1)
	obs("evidence_id", 1)

	snap := metrics.Snapshot()
	byKind := map[string]float64{}
	for _, s := range snap.Counters[observability.MetricGroundingUnsupportedTotal] {
		byKind[s.Labels["kind"]] = s.Value
	}

	if got := byKind["number"]; got != 3 {
		t.Errorf("kind=number: want 3, got %v", got)
	}
	if got := byKind["evidence_id"]; got != 1 {
		t.Errorf("kind=evidence_id: want 1, got %v", got)
	}
}

package courtroom

// v2.13 (deferred D24) 发言级幻觉 metric 接线测试。
// 验证 speakHallucinationObserver 按 mode 分桶累加 MetricSpeakHallucinationTotal。

import (
	"testing"

	"github.com/decisioncourt/backend/internal/observability"
)

func TestSpeakHallucinationObserver_IncrementsCounterByMode(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	obs := speakHallucinationObserver(metrics)

	// 同一 mode 两次 + 另一 mode 一次。
	obs("evidence_ref_empty_with_citation", "证据3")
	obs("evidence_ref_empty_with_citation", "证据4")
	obs("case_num_always_forbidden", "(2024)京01民初123号")

	snap := metrics.Snapshot()
	byMode := map[string]float64{}
	for _, s := range snap.Counters[observability.MetricSpeakHallucinationTotal] {
		byMode[s.Labels["mode"]] = s.Value
	}

	if got := byMode["evidence_ref_empty_with_citation"]; got != 2 {
		t.Errorf("mode=evidence_ref_empty_with_citation: want 2, got %v", got)
	}
	if got := byMode["case_num_always_forbidden"]; got != 1 {
		t.Errorf("mode=case_num_always_forbidden: want 1, got %v", got)
	}
}

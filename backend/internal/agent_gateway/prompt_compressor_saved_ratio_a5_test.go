package agent_gateway

import (
	"math"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/observability"
)

// TestCompress_RecordsSavedRatioMetric 是 M8-2 A5 的回归钉：压缩时必须记录
// "实际节省率" = 1 - after/before（0 = 一点没省）。
// 原 MetricPromptCompressionRatio 是 after/before，"省 0%" 时 = 1.0，看板不直观。
func TestCompress_RecordsSavedRatioMetric(t *testing.T) {
	m := observability.NewMetrics()
	pc := NewPromptCompressor(SmartCompressionConfig{Enabled: true, KeepRecentForcedN: 1}, m)

	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: strings.Repeat("a", 100)},
		{Role: "user", Content: "b"},
	}
	_, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusExhausted})
	if !info.Applied || info.BeforeLength == 0 {
		t.Fatalf("测试前置不成立：Applied=%v BeforeLength=%d", info.Applied, info.BeforeLength)
	}

	snap := m.Snapshot()
	hist := snap.Histograms[observability.MetricPromptCompressionSavedRatio]
	if len(hist) == 0 {
		t.Fatalf("prompt_compression_saved_ratio 指标未记录")
	}
	mean := hist[0].Sum / float64(hist[0].Count)
	want := 1 - float64(info.AfterLength)/float64(info.BeforeLength)
	if math.Abs(mean-want) > 1e-9 {
		t.Errorf("saved_ratio 均值 = %v，want %v", mean, want)
	}
	if mean <= 0 {
		t.Errorf("本次压缩确实省了，saved_ratio 应 > 0，got %v", mean)
	}
}

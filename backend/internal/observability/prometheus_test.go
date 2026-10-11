package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 2026-10-10 第 3 轮：Prometheus 文本导出面。目标不是"能输出"，而是
// **Prometheus 真能解析** —— 所以断言结构（TYPE 行 / le 累积 / +Inf / 转义）。
func TestRenderPrometheus_Shape(t *testing.T) {
	snap := MetricSnapshot{
		Timestamp: time.Now().UTC(),
		Counters: map[string][]MetricSample{
			"llm_call_total": {{Labels: map[string]string{"agent": "judge", "model": "m"}, Value: 3}},
		},
		Gauges: map[string][]MetricSample{
			"budget_ratio": {{Labels: nil, Value: 0.2076}},
		},
		Histograms: map[string][]MetricSample{
			"llm_call_duration_seconds": {{
				Labels:  map[string]string{"status": "ok"},
				Count:   4,
				Sum:     1.5,
				Buckets: []Bucket{{UpperBound: 0.5, Count: 3}, {UpperBound: 1, Count: 4}},
			}},
		},
	}
	out := RenderPrometheus(snap)

	require.Contains(t, out, "# TYPE llm_call_total counter")
	require.Contains(t, out, `llm_call_total{agent="judge",model="m"} 3`)
	require.Contains(t, out, "# TYPE budget_ratio gauge")
	require.Contains(t, out, "budget_ratio 0.2076")
	require.Contains(t, out, "# TYPE llm_call_duration_seconds histogram")
	// le 累积（0.5 → 3，1 → 4）与 +Inf = 总计数
	require.Contains(t, out, `llm_call_duration_seconds_bucket{le="0.5",status="ok"} 3`)
	require.Contains(t, out, `llm_call_duration_seconds_bucket{le="1",status="ok"} 4`)
	require.Contains(t, out, `llm_call_duration_seconds_bucket{le="+Inf",status="ok"} 4`)
	require.Contains(t, out, `llm_call_duration_seconds_sum{status="ok"} 1.5`)
	require.Contains(t, out, `llm_call_duration_seconds_count{status="ok"} 4`)
}

func TestRenderPrometheus_EmptySnapshot(t *testing.T) {
	out := RenderPrometheus(MetricSnapshot{})
	require.Equal(t, "", strings.TrimSpace(out), "空快照应输出空文本（无 TYPE 行噪声）")
}

func TestRenderPrometheus_EscapesLabelValues(t *testing.T) {
	snap := MetricSnapshot{
		Counters: map[string][]MetricSample{
			"m": {{Labels: map[string]string{"err": "a\"b\\c\nd"}, Value: 1}},
		},
	}
	out := RenderPrometheus(snap)
	require.Contains(t, out, `err="a\"b\\c\nd"`, "引号/反斜杠/换行必须按规范转义")
}

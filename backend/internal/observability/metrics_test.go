package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_Counter(t *testing.T) {
	m := NewMetrics()
	m.IncCounter("foo", map[string]string{"k": "v"})
	m.IncCounter("foo", map[string]string{"k": "v"})
	m.AddCounter("foo", map[string]string{"k": "v"}, 3)

	snap := m.Snapshot()
	require.Contains(t, snap.Counters, "foo")
	require.Len(t, snap.Counters["foo"], 1)
	assert.Equal(t, float64(5), snap.Counters["foo"][0].Value)
	assert.Equal(t, "v", snap.Counters["foo"][0].Labels["k"])
}

func TestMetrics_CounterWithDifferentLabels(t *testing.T) {
	m := NewMetrics()
	m.IncCounter("foo", map[string]string{"k": "v1"})
	m.IncCounter("foo", map[string]string{"k": "v2"})
	m.IncCounter("foo", map[string]string{"k": "v1"}) // 第二条 v1

	snap := m.Snapshot()
	require.Len(t, snap.Counters["foo"], 2)

	// labelsKey 是按 key 排序后拼出，所以 v1 + v1 vs v2 可能顺序不同
	// 转换为 map 再断言值
	values := map[string]float64{}
	for _, s := range snap.Counters["foo"] {
		values[s.Labels["k"]] = s.Value
	}
	assert.Equal(t, float64(2), values["v1"])
	assert.Equal(t, float64(1), values["v2"])
}

func TestMetrics_Gauge(t *testing.T) {
	m := NewMetrics()
	m.SetGauge("g", map[string]string{"x": "1"}, 10)
	m.SetGauge("g", map[string]string{"x": "1"}, 20)
	m.SetGauge("g", map[string]string{"x": "2"}, 100)

	snap := m.Snapshot()
	require.Contains(t, snap.Gauges, "g")
	require.Len(t, snap.Gauges["g"], 2)

	values := map[string]float64{}
	for _, s := range snap.Gauges["g"] {
		values[s.Labels["x"]] = s.Value
	}
	assert.Equal(t, float64(20), values["1"])
	assert.Equal(t, float64(100), values["2"])
}

func TestMetrics_Histogram(t *testing.T) {
	m := NewMetrics()
	m.ObserveHistogram("h", map[string]string{}, 0.05)
	m.ObserveHistogram("h", map[string]string{}, 0.5)
	m.ObserveHistogram("h", map[string]string{}, 5)
	m.ObserveHistogram("h", map[string]string{}, 50)

	snap := m.Snapshot()
	require.Contains(t, snap.Histograms, "h")
	require.Len(t, snap.Histograms["h"], 1)
	h := snap.Histograms["h"][0]
	assert.Equal(t, int64(4), h.Count)
	assert.InDelta(t, 55.55, h.Sum, 0.01)
	// 0.05 落入 <=0.05 桶
	// 0.5  落入 <=0.5 桶
	// 5    落入 <=5 桶
	// 50   落入 <=60 桶（最近一个）
	bucketCounts := map[float64]int64{}
	for _, b := range h.Buckets {
		bucketCounts[b.UpperBound] = b.Count
	}
	assert.Equal(t, int64(1), bucketCounts[0.05])
	assert.Equal(t, int64(2), bucketCounts[0.5])
	assert.Equal(t, int64(3), bucketCounts[5])
	assert.Equal(t, int64(4), bucketCounts[60])
}

func TestMetrics_ConcurrentSafety(t *testing.T) {
	m := NewMetrics()
	const goroutines = 50
	const iters = 100
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iters; j++ {
				m.IncCounter("c", nil)
				m.SetGauge("g", nil, float64(j))
				m.ObserveHistogram("h", nil, float64(j)/100)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	snap := m.Snapshot()
	assert.Equal(t, float64(goroutines*iters), snap.Counters["c"][0].Value)
	assert.Equal(t, int64(goroutines*iters), snap.Histograms["h"][0].Count)
}
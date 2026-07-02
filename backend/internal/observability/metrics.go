package observability

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 是白盒化的业务指标集合接口。Default 提供线程安全的内存实现，
// 未来可替换为 Prometheus client_golang adapter（接口签名保持兼容）。
type Metrics interface {
	IncCounter(name string, labels map[string]string)
	AddCounter(name string, labels map[string]string, delta float64)
	SetGauge(name string, labels map[string]string, value float64)
	ObserveHistogram(name string, labels map[string]string, value float64)
	// Snapshot 返回当前所有指标的快照（按 name + labels 序列化），供 /metrics 端点输出。
	Snapshot() MetricSnapshot
}

// MetricSnapshot 是 /metrics 端点的 JSON 结构。
type MetricSnapshot struct {
	Timestamp  time.Time                 `json:"timestamp"`
	Counters   map[string][]MetricSample `json:"counters,omitempty"`
	Gauges     map[string][]MetricSample `json:"gauges,omitempty"`
	Histograms map[string][]MetricSample `json:"histograms,omitempty"`
}

// MetricSample 是单条带 labels 的指标值。
type MetricSample struct {
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
	// Histogram 额外字段
	Count   int64    `json:"count,omitempty"`
	Sum     float64  `json:"sum,omitempty"`
	Buckets []Bucket `json:"buckets,omitempty"`
}

// Bucket 是 histogram 的一个分桶。
type Bucket struct {
	UpperBound float64 `json:"le"` // le = less than or equal
	Count      int64   `json:"count"`
}

// defaultBuckets 是 histogram 默认分桶边界。覆盖白盒化中最常用的分布：
// LLM 调用延迟 100ms ~ 30s，HTTP 请求 10ms ~ 5s，压缩耗时 1ms ~ 1s。
var defaultBuckets = []float64{
	0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300,
}

// 业务指标名称常量集中定义，方便跨包引用 + 拼写一致。
const (
	MetricActiveSessions         = "courtroom_active_sessions"
	MetricStateTransitionTotal   = "courtroom_state_transition_total"
	MetricLLMCallTotal           = "llm_call_total"
	MetricLLMCallDuration        = "llm_call_duration_seconds"
	MetricLLMCallTokens          = "llm_call_tokens_total"
	MetricBudgetRatio            = "budget_ratio"
	MetricBudgetRejectedTotal    = "budget_rejected_total"
	MetricA2AThroughputTotal     = "a2a_message_throughput_total"
	MetricBeliefDiffTotal        = "belief_diff_total"
	MetricCompressionApplied     = "compression_applied_total"
	MetricHTTPRequestDuration    = "http_request_duration_seconds"
	MetricDecisionEventTotal     = "decision_event_total"
	MetricSpanTotal              = "span_total"
	MetricSpanErrorTotal         = "span_error_total"
	MetricSpanDuration           = "span_duration_seconds"
)

// memMetrics 是线程安全的内存实现。
type memMetrics struct {
	mu         sync.RWMutex
	counters   map[string]map[string]*counterEntry // name → labelsKey → entry
	gauges     map[string]map[string]*gaugeEntry
	histograms map[string]map[string]*histogramEntry
}

type counterEntry struct {
	labels map[string]string
	value  uint64 // 使用 atomic.AddUint64，labels map 不可变
}

type gaugeEntry struct {
	labels map[string]string
	value  uint64 // 存 float64 bits，atomic.Load/Store
}

type histogramEntry struct {
	labels  map[string]string
	count   uint64
	sumBits uint64
	// buckets: 每个 bucket 一个原子计数
	bucketCounts []uint64
	bucketBounds []float64
}

// NewMetrics 构造内存版 Metrics 实例。零值时返回已初始化的实例。
func NewMetrics() Metrics {
	return &memMetrics{
		counters:   make(map[string]map[string]*counterEntry),
		gauges:     make(map[string]map[string]*gaugeEntry),
		histograms: make(map[string]map[string]*histogramEntry),
	}
}

func (m *memMetrics) IncCounter(name string, labels map[string]string) {
	m.AddCounter(name, labels, 1)
}

func (m *memMetrics) AddCounter(name string, labels map[string]string, delta float64) {
	key := labelsKey(labels)
	entry := m.getOrCreateCounter(name, key, labels)
	atomic.AddUint64(&entry.value, uint64(delta))
}

func (m *memMetrics) SetGauge(name string, labels map[string]string, value float64) {
	key := labelsKey(labels)
	entry := m.getOrCreateGauge(name, key, labels)
	atomic.StoreUint64(&entry.value, float64Bits(value))
}

func (m *memMetrics) ObserveHistogram(name string, labels map[string]string, value float64) {
	key := labelsKey(labels)
	entry := m.getOrCreateHistogram(name, key, labels)
	atomic.AddUint64(&entry.count, 1)
	// sum 累加：atomic CAS 重试直到成功
	for {
		old := atomic.LoadUint64(&entry.sumBits)
		newSum := float64Bits(float64FromBits(old) + value)
		if atomic.CompareAndSwapUint64(&entry.sumBits, old, newSum) {
			break
		}
	}
	// 更新每个 bucket 的计数
	for i, bound := range entry.bucketBounds {
		if value <= bound {
			atomic.AddUint64(&entry.bucketCounts[i], 1)
		}
	}
}

func (m *memMetrics) Snapshot() MetricSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := MetricSnapshot{
		Timestamp:  time.Now().UTC(),
		Counters:   make(map[string][]MetricSample),
		Gauges:     make(map[string][]MetricSample),
		Histograms: make(map[string][]MetricSample),
	}
	for name, entries := range m.counters {
		// 按 labelsKey 排序保证 Snapshot 输出一致
		keys := sortedKeys(entries)
		for _, k := range keys {
			e := entries[k]
			snap.Counters[name] = append(snap.Counters[name], MetricSample{
				Labels: e.labels,
				Value:  float64(atomic.LoadUint64(&e.value)),
			})
		}
	}
	for name, entries := range m.gauges {
		keys := sortedKeys(entries)
		for _, k := range keys {
			e := entries[k]
			snap.Gauges[name] = append(snap.Gauges[name], MetricSample{
				Labels: e.labels,
				Value:  float64FromBits(atomic.LoadUint64(&e.value)),
			})
		}
	}
	for name, entries := range m.histograms {
		keys := sortedKeys(entries)
		for _, k := range keys {
			e := entries[k]
			buckets := make([]Bucket, len(e.bucketBounds))
			for i, bound := range e.bucketBounds {
				buckets[i] = Bucket{
					UpperBound: bound,
					Count:      int64(atomic.LoadUint64(&e.bucketCounts[i])),
				}
			}
			snap.Histograms[name] = append(snap.Histograms[name], MetricSample{
				Labels:  e.labels,
				Count:   int64(atomic.LoadUint64(&e.count)),
				Sum:     float64FromBits(atomic.LoadUint64(&e.sumBits)),
				Buckets: buckets,
			})
		}
	}
	return snap
}

func (m *memMetrics) getOrCreateCounter(name, key string, labels map[string]string) *counterEntry {
	m.mu.RLock()
	if entries, ok := m.counters[name]; ok {
		if e, ok := entries[key]; ok {
			m.mu.RUnlock()
			return e
		}
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters[name] == nil {
		m.counters[name] = make(map[string]*counterEntry)
	}
	if e, ok := m.counters[name][key]; ok {
		return e
	}
	e := &counterEntry{labels: cloneLabels(labels)}
	m.counters[name][key] = e
	return e
}

func (m *memMetrics) getOrCreateGauge(name, key string, labels map[string]string) *gaugeEntry {
	m.mu.RLock()
	if entries, ok := m.gauges[name]; ok {
		if e, ok := entries[key]; ok {
			m.mu.RUnlock()
			return e
		}
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gauges[name] == nil {
		m.gauges[name] = make(map[string]*gaugeEntry)
	}
	if e, ok := m.gauges[name][key]; ok {
		return e
	}
	e := &gaugeEntry{labels: cloneLabels(labels)}
	m.gauges[name][key] = e
	return e
}

func (m *memMetrics) getOrCreateHistogram(name, key string, labels map[string]string) *histogramEntry {
	m.mu.RLock()
	if entries, ok := m.histograms[name]; ok {
		if e, ok := entries[key]; ok {
			m.mu.RUnlock()
			return e
		}
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.histograms[name] == nil {
		m.histograms[name] = make(map[string]*histogramEntry)
	}
	if e, ok := m.histograms[name][key]; ok {
		return e
	}
	e := &histogramEntry{
		labels:       cloneLabels(labels),
		bucketBounds: defaultBuckets,
		bucketCounts: make([]uint64, len(defaultBuckets)),
	}
	m.histograms[name][key] = e
	return e
}

func labelsKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := make([]byte, 0, 64)
	for _, k := range keys {
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, labels[k]...)
		b = append(b, ',')
	}
	return string(b)
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// float64Bits / float64FromBits 把 float64 按 IEEE 754 位模式转 uint64。
// 借助 math.Float64bits / Float64frombits，避免不安全的指针转换。
// atomic 操作直接走 uint64 路径更快。
func float64Bits(f float64) uint64 {
	return math.Float64bits(f)
}

func float64FromBits(b uint64) float64 {
	return math.Float64frombits(b)
}
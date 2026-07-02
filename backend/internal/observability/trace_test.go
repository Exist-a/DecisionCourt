package observability

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRecorder 记录 Span 写入的事件，便于断言。
type fakeRecorder struct {
	mu      sync.Mutex
	events  []DecisionEventRecord
	failNext bool
}

func (f *fakeRecorder) Record(_ context.Context, ev DecisionEventRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return errors.New("fake: insert failed")
	}
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeRecorder) all() []DecisionEventRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]DecisionEventRecord, len(f.events))
	copy(out, f.events)
	return out
}

func TestSpan_EndWritesMetricAndEvent(t *testing.T) {
	m := NewMetrics()
	rec := &fakeRecorder{}
	ctx := WithTrace(context.Background(), Trace{
		RequestID:   "req-1",
		SessionUUID: "sess-1",
		AgentType:   "prosecutor",
	})

	tracer := TracerFromContext(ctx, m, rec)
	span := tracer.StartSpan("TestSpan")
	span.SetAttr("key1", "v1")
	span.SetAttrs(map[string]interface{}{"k2": 2, "k3": true})

	// 给 sleep 一点点时间让 duration > 0
	time.Sleep(time.Millisecond)
	span.End()

	// 1. 验证 metric
	snap := m.Snapshot()
	require.Contains(t, snap.Counters, MetricSpanTotal)
	// 找到 name=TestSpan,status=ok 的样本
	found := false
	for _, s := range snap.Counters[MetricSpanTotal] {
		if s.Labels["name"] == "TestSpan" && s.Labels["status"] == "ok" {
			assert.Equal(t, float64(1), s.Value)
			found = true
		}
	}
	assert.True(t, found, "expected MetricSpanTotal[TestSpan:ok]=1")

	require.Contains(t, snap.Histograms, MetricSpanDuration)
	// histogram 应该有至少一个样本
	assert.GreaterOrEqual(t, snap.Histograms[MetricSpanDuration][0].Count, int64(1))

	// 2. 验证 decision_events
	events := rec.all()
	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, "span.TestSpan", ev.EventType)
	assert.Equal(t, "req-1", ev.RequestID)
	assert.Equal(t, "sess-1", ev.SessionUUID)
	assert.Equal(t, "prosecutor", ev.AgentType)
	assert.Equal(t, "ok", ev.Status)
	assert.Equal(t, "v1", ev.Payload["key1"])
	assert.Equal(t, 2, ev.Payload["k2"])
	assert.GreaterOrEqual(t, ev.DurationMs, int64(0))
}

func TestSpan_SetErrorMarksAsError(t *testing.T) {
	m := NewMetrics()
	rec := &fakeRecorder{}
	ctx := context.Background()

	tracer := TracerFromContext(ctx, m, rec)
	span := tracer.StartSpan("ErrSpan")
	span.SetError(errors.New("boom"))
	span.End()

	events := rec.all()
	require.Len(t, events, 1)
	assert.Equal(t, "error", events[0].Status)
	assert.Equal(t, "boom", events[0].ErrorMsg)
	assert.Equal(t, "boom", events[0].Payload["error.message"])

	snap := m.Snapshot()
	// MetricSpanErrorTotal[ErrSpan:error] = 1
	hasErr := false
	for _, s := range snap.Counters[MetricSpanErrorTotal] {
		if s.Labels["name"] == "ErrSpan" {
			assert.Equal(t, float64(1), s.Value)
			hasErr = true
		}
	}
	assert.True(t, hasErr)
}

func TestSpan_EndIsIdempotent(t *testing.T) {
	rec := &fakeRecorder{}
	tracer := TracerFromContext(context.Background(), nil, rec)
	span := tracer.StartSpan("Idem")
	span.End()
	span.End()
	span.End()
	assert.Len(t, rec.all(), 1)
}

func TestSpan_NilTracerReturnsNoop(t *testing.T) {
	var tracer *Tracer
	span := tracer.StartSpan("X")
	span.SetAttr("k", "v")
	span.SetError(errors.New("e"))
	span.End()
	// 不应 panic
}

func TestSpan_RecorderErrorDoesNotPanic(t *testing.T) {
	m := NewMetrics()
	rec := &fakeRecorder{failNext: true}
	tracer := TracerFromContext(context.Background(), m, rec)
	span := tracer.StartSpan("RecorderFails")
	span.End()
	// 即使 recorder 失败，metric 仍然成功 + 不 panic
	snap := m.Snapshot()
	assert.Contains(t, snap.Counters, MetricSpanTotal)
}

func TestSpan_NilRecorderStillWritesMetric(t *testing.T) {
	m := NewMetrics()
	tracer := TracerFromContext(context.Background(), m, nil)
	tracer.StartSpan("NoRec").End()
	snap := m.Snapshot()
	assert.Contains(t, snap.Counters, MetricSpanTotal)
}
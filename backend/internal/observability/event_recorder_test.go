package observability

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemoryEventRecorder 是不依赖 DB 的 EventRecorder 实现，用于测试。
type inMemoryEventRecorder struct {
	mu     sync.Mutex
	events []DecisionEventRecord
}

func (r *inMemoryEventRecorder) Record(_ context.Context, ev DecisionEventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *inMemoryEventRecorder) all() []DecisionEventRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]DecisionEventRecord, len(r.events))
	copy(out, r.events)
	return out
}

func TestGormEventRecorder_NilDBIsNoop(t *testing.T) {
	rec := NewGormEventRecorder(nil)
	err := rec.Record(context.Background(), DecisionEventRecord{
		SessionUUID: "sess-1",
		EventType:   "test.event",
	})
	require.NoError(t, err)
}

func TestGormEventRecorder_NilReceiverIsNoop(t *testing.T) {
	var rec *GormEventRecorder
	err := rec.Record(context.Background(), DecisionEventRecord{EventType: "x"})
	require.NoError(t, err)
}

func TestInMemoryRecorder_RecordsAllFields(t *testing.T) {
	rec := &inMemoryEventRecorder{}
	_ = rec.Record(context.Background(), DecisionEventRecord{
		SessionUUID: "sess-1",
		RequestID:   "req-1",
		EventType:   "span.RunCrossExamRound",
		AgentType:   "prosecutor",
		Payload:     map[string]interface{}{"round": 2},
		DurationMs:  1234,
		Status:      "ok",
	})
	events := rec.all()
	require.Len(t, events, 1)
	assert.Equal(t, "sess-1", events[0].SessionUUID)
	assert.Equal(t, "req-1", events[0].RequestID)
	assert.Equal(t, "span.RunCrossExamRound", events[0].EventType)
	assert.Equal(t, "prosecutor", events[0].AgentType)
	assert.Equal(t, int64(1234), events[0].DurationMs)
	assert.Equal(t, "ok", events[0].Status)
	assert.Equal(t, 2, events[0].Payload["round"])
}
package trace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// makeRun 构造测试用 Run,fields 填最小可识别集合。
func makeRun(traceID string, retryCount int, startedAt time.Time, latencyMs int) Run {
	return Run{
		RunID:      traceID + "-" + itoa(retryCount),
		TraceID:    traceID,
		SessionID:  "sess-test",
		AgentType:  "prosecutor",
		TaskType:   "react_speak",
		StartedAt:  startedAt,
		EndedAt:    startedAt.Add(time.Duration(latencyMs) * time.Millisecond),
		LatencyMs:  latencyMs,
		Status:     "ok",
		RetryCount: retryCount,
	}
}

// T1: Aggregate 全量聚合 (traceIDs=nil) — 返回所有 trace。
func TestAggregate_AllTraces(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	st := &SessionTrace{
		SessionID: "sess-test",
		Traces: map[string][]Run{
			"trace-A": {makeRun("trace-A", 0, base, 1000)},
			"trace-B": {makeRun("trace-B", 0, base.Add(5*time.Second), 2000)},
		},
	}

	traces := Aggregate(st, nil)
	require.Len(t, traces, 2)
	// 按 StartedAt 升序
	require.Equal(t, "trace-A", traces[0].TraceID)
	require.Equal(t, "trace-B", traces[1].TraceID)
}

// T2: Aggregate 按 traceIDs 过滤 — 只返回指定 trace。
func TestAggregate_FilterByTraceIDs(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	st := &SessionTrace{
		SessionID: "sess-test",
		Traces: map[string][]Run{
			"trace-A": {makeRun("trace-A", 0, base, 1000)},
			"trace-B": {makeRun("trace-B", 0, base.Add(time.Second), 1000)},
			"trace-C": {makeRun("trace-C", 0, base.Add(2*time.Second), 1000)},
		},
	}

	traces := Aggregate(st, []string{"trace-B"})
	require.Len(t, traces, 1)
	require.Equal(t, "trace-B", traces[0].TraceID)
}

// T3: BuildTree 单链 retry (RC=0 → RC=1 → RC=2)。
//
// 验证树状结构: root(RC=0) → child(RC=1) → grandchild(RC=2)。
func TestBuildTree_LinearRetry(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	runs := []Run{
		makeRun("trace-T", 0, base, 1000),
		makeRun("trace-T", 1, base.Add(2*time.Second), 1000),
		makeRun("trace-T", 2, base.Add(4*time.Second), 1000),
	}

	tree := BuildTree(runs)
	require.Equal(t, 0, tree.Run.RetryCount, "root 必是 RC=0")
	require.Len(t, tree.Children, 1, "root 1 个 child")
	require.Equal(t, 1, tree.Children[0].Run.RetryCount, "child 是 RC=1")
	require.Len(t, tree.Children[0].Children, 1, "grandchild 1 个")
	require.Equal(t, 2, tree.Children[0].Children[0].Run.RetryCount, "grandchild 是 RC=2")
	require.Empty(t, tree.Children[0].Children[0].Children, "叶子无 child")
}

// T4: BuildTree 空输入 + 单 run 边界。
func TestBuildTree_EdgeCases(t *testing.T) {
	// 空 → 空 RunNode
	tree := BuildTree(nil)
	require.Equal(t, Run{}, tree.Run)
	require.Empty(t, tree.Children)

	// 单 run (只有 RC=0)
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	runs := []Run{makeRun("trace-S", 0, base, 1000)}
	tree = BuildTree(runs)
	require.Equal(t, 0, tree.Run.RetryCount)
	require.Empty(t, tree.Children, "单 run 无 child")
}

// T5: SessionTrace 空数据 → Aggregate 返空数组 (非 nil)。
func TestAggregate_EmptySessionTrace(t *testing.T) {
	traces := Aggregate(&SessionTrace{}, nil)
	require.NotNil(t, traces, "空 SessionTrace 应返非 nil 切片")
	require.Empty(t, traces)

	traces = Aggregate(nil, nil)
	require.NotNil(t, traces)
	require.Empty(t, traces)
}

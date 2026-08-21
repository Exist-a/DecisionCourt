package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/stretchr/testify/require"
)

// writeFileLog 写一条测试用 agent_gateway 日志行到 temp 文件。
//
// 返回写入的文件路径 + date (YYYY-MM-DD 格式)。
func writeFileLog(t *testing.T, dir string, date string, entry agent_gateway.LogEntry) string {
	t.Helper()
	path := filepath.Join(dir, "agent_gateway_"+date+".log")
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	require.NoError(t, err)
	return path
}

// T1: InMemoryTraceStore.Put + ListBySession + GetTraceByID 基础流程。
func TestInMemoryTraceStore_BasicFlow(t *testing.T) {
	store := NewInMemoryTraceStore()

	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	st := &SessionTrace{
		SessionID: "sess-A",
		Traces: map[string][]Run{
			"trace-X": {
				makeRunForTest("trace-X", "sess-A", 0, base, 1000),
				makeRunForTest("trace-X", "sess-A", 1, base.Add(2*time.Second), 1000),
			},
		},
	}
	store.Put("sess-A", st)

	traces, err := store.ListBySession("sess-A", "")
	require.NoError(t, err)
	require.Len(t, traces, 1)
	require.Equal(t, "trace-X", traces[0].TraceID)
	require.Len(t, traces[0].Runs, 2)

	tr, err := store.GetTraceByID("sess-A", "trace-X")
	require.NoError(t, err)
	require.NotNil(t, tr)
	require.Equal(t, "trace-X", tr.TraceID)
	require.Equal(t, 0, tr.Tree.Run.RetryCount)
	require.Len(t, tr.Tree.Children, 1, "tree 应有 1 child")
}

// T2: InMemoryTraceStore 查不存在的 session / trace → 返空,非 error。
func TestInMemoryTraceStore_NotFound(t *testing.T) {
	store := NewInMemoryTraceStore()

	traces, err := store.ListBySession("missing", "")
	require.NoError(t, err)
	require.Empty(t, traces)

	t2, err := store.GetTraceByID("missing", "trace-X")
	require.NoError(t, err)
	require.Nil(t, t2, "找不到返 nil (前端按无数据渲染)")
}

// T3: FileTraceStore 读 1 天文件 + LRU 缓存命中。
func TestFileTraceStore_FileReadAndCache(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	writeFileLog(t, dir, "2026-08-22", agent_gateway.LogEntry{
		Timestamp: base, RequestID: "req-f1", SessionUUID: "sess-F",
		AgentType: "prosecutor", TaskType: "react_speak", LatencyMs: 1000,
		Status: "ok", RetryCount: 0,
	})
	writeFileLog(t, dir, "2026-08-22", agent_gateway.LogEntry{
		Timestamp: base.Add(2 * time.Second), RequestID: "req-f2", SessionUUID: "sess-F",
		AgentType: "defender", TaskType: "react_speak", LatencyMs: 1500,
		Status: "ok", RetryCount: 0,
	})

	store := NewFileTraceStore(dir)

	// 第 1 次查 → 读文件 + 写缓存
	traces, err := store.ListBySession("sess-F", "2026-08-22")
	require.NoError(t, err)
	require.Len(t, traces, 2)

	// 第 2 次查 → 命中缓存 (不会重新读文件,但结果应一致)
	traces2, err := store.ListBySession("sess-F", "2026-08-22")
	require.NoError(t, err)
	require.Len(t, traces2, 2)
	require.Equal(t, traces[0].TraceID, traces2[0].TraceID)
}

// T4: FileTraceStore 文件不存在 → 返空 SessionTrace (不返 error)。
func TestFileTraceStore_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTraceStore(dir)

	traces, err := store.ListBySession("sess-anything", "2026-08-22")
	require.NoError(t, err, "文件不存在不算 error,返空数据")
	require.Empty(t, traces)
}

// T5: LRU 缓存容量上限 — 超过 capacity 淘汰最旧。
func TestLRUCache_Eviction(t *testing.T) {
	c := newLRUCache(2)
	c.put("a", &SessionTrace{Traces: map[string][]Run{"a": nil}})
	c.put("b", &SessionTrace{Traces: map[string][]Run{"b": nil}})
	// 此时缓存 [b, a] (b 最新)

	c.put("c", &SessionTrace{Traces: map[string][]Run{"c": nil}})
	// 此时缓存 [c, b] (a 被淘汰)

	_, okA := c.get("a")
	require.False(t, okA, "a 应已被淘汰")
	_, okB := c.get("b")
	require.True(t, okB, "b 应仍在缓存")
	_, okC := c.get("c")
	require.True(t, okC, "c 应仍在缓存")
}

// makeRunForTest 是 store_test 专用的 Run 构造 (避免和 parser_test 名字冲突)。
func makeRunForTest(traceID, sessionID string, retryCount int, startedAt time.Time, latencyMs int) Run {
	return Run{
		RunID:      traceID + "-" + itoa(retryCount),
		TraceID:    traceID,
		SessionID:  sessionID,
		AgentType:  "prosecutor",
		TaskType:   "react_speak",
		StartedAt:  startedAt,
		EndedAt:    startedAt.Add(time.Duration(latencyMs) * time.Millisecond),
		LatencyMs:  latencyMs,
		Status:     "ok",
		RetryCount: retryCount,
	}
}

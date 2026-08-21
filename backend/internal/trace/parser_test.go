package trace

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/stretchr/testify/require"
)

// makeLogEntry 构造测试用 LogEntry,字段填全便于断言。
//
// 不导出于生产代码 (lowercase),仅测试本文件用。
func makeLogEntry(requestID, sessionUUID, agentType, taskType string, retryCount, latencyMs int, status string, ts time.Time) agent_gateway.LogEntry {
	return agent_gateway.LogEntry{
		Timestamp:        ts,
		RequestID:        requestID,
		SessionUUID:      sessionUUID,
		AgentType:        agentType,
		TaskType:         taskType,
		Model:            "deepseek-v4-flash",
		Provider:         "deepseek",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		LatencyMs:        latencyMs,
		Status:           status,
		RetryCount:       retryCount,
	}
}

// T1: ParseReader 解析多行 JSON Lines,按 trace_id 分组 + 字段映射正确。
func TestParseReader_MultiLines(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	entries := []agent_gateway.LogEntry{
		makeLogEntry("req-1", "sess-A", "prosecutor", "react_speak", 0, 3000, "ok", base),
		makeLogEntry("req-2", "sess-A", "defender", "react_speak", 0, 2500, "ok", base.Add(4*time.Second)),
		makeLogEntry("req-1", "sess-A", "prosecutor", "react_speak_retry", 1, 2000, "ok", base.Add(8*time.Second)),
	}
	var buf bytes.Buffer
	for _, e := range entries {
		data, _ := json.Marshal(e)
		buf.Write(data)
		buf.WriteByte('\n')
	}

	st, parseErrors, err := ParseReader(&buf)
	require.NoError(t, err)
	require.Equal(t, 0, parseErrors, "valid JSON Lines 应 0 解析错误")
	require.Equal(t, "sess-A", st.SessionID)
	require.Len(t, st.Traces, 2, "应有 2 个 trace_id (req-1 + req-2)")

	// req-1 有 2 runs (RC=0 + RC=1)
	require.Len(t, st.Traces["req-1"], 2)
	require.Equal(t, 0, st.Traces["req-1"][0].RetryCount, "parser 按 RC 升序排")
	require.Equal(t, 1, st.Traces["req-1"][1].RetryCount)

	// RunID 格式 = RequestID + "-" + RetryCount
	require.Equal(t, "req-1-0", st.Traces["req-1"][0].RunID)
	require.Equal(t, "req-1-1", st.Traces["req-1"][1].RunID)

	// TraceID = RequestID
	require.Equal(t, "req-1", st.Traces["req-1"][0].TraceID)

	// StartedAt = Timestamp - LatencyMs
	require.Equal(t, base.Add(-3*time.Second), st.Traces["req-1"][0].StartedAt)
	require.Equal(t, base, st.Traces["req-1"][0].EndedAt)
}

// T2: 空 reader 返 (空 SessionTrace, 0, nil) — 不返 error,也不返 nil。
func TestParseReader_Empty(t *testing.T) {
	st, parseErrors, err := ParseReader(bytes.NewReader(nil))
	require.NoError(t, err)
	require.Equal(t, 0, parseErrors)
	require.NotNil(t, st)
	require.Equal(t, "", st.SessionID)
	require.Empty(t, st.Traces)
}

// T3: 单行 JSON 解析失败 → 跳过该行 + parseErrors+1,继续解析后续行。
func TestParseReader_InvalidLineSkipped(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	good := makeLogEntry("req-ok", "sess-A", "prosecutor", "react_speak", 0, 1000, "ok", base)

	var buf bytes.Buffer
	buf.WriteString("this is not json\n")
	data, _ := json.Marshal(good)
	buf.Write(data)
	buf.WriteByte('\n')
	buf.WriteString("{incomplete json\n")
	data2, _ := json.Marshal(makeLogEntry("req-ok2", "sess-A", "defender", "react_speak", 0, 1000, "ok", base.Add(2*time.Second)))
	buf.Write(data2)
	buf.WriteByte('\n')

	st, parseErrors, err := ParseReader(&buf)
	require.NoError(t, err, "单行解析失败不应让整体失败")
	require.Equal(t, 2, parseErrors, "2 行 invalid 应累计 parseErrors=2")
	require.Len(t, st.Traces, 2, "2 行 valid 应被解析")
}

// T4: ParseFile 真实文件读写 + 文件不存在返 error。
func TestParseFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "agent_gateway_2026-08-22.log")

	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	entry := makeLogEntry("req-file", "sess-file", "judge", "judge_final", 0, 5000, "ok", base)
	data, _ := json.Marshal(entry)

	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0644))

	st, parseErrors, err := ParseFile(path)
	require.NoError(t, err)
	require.Equal(t, 0, parseErrors)
	require.Equal(t, "sess-file", st.SessionID)
	require.Len(t, st.Traces["req-file"], 1)
}

// T5: 文件不存在 → ParseFile 返 error,包装 path。
func TestParseFile_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	missing := filepath.Join(tmpDir, "missing.log")

	_, _, err := ParseFile(missing)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), missing), "error 应包含文件路径便于排查")
}

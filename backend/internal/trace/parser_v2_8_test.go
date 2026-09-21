package trace

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/stretchr/testify/require"
)

// v2.8 PR-3 (ADR 0042) — parser 后向兼容 + 新字段映射测试.
//
// LogEntry 加 4 个 optional 字段 (system_prompt / input_messages /
// output_content / output_truncated). parser/entryToRun 必须:
//   1. 后向兼容: 解析 v2.7 时期的 log 行 (无新字段) 时 zero-value 填充, 不报错
//   2. 正向映射: entryToRun 把 SystemPrompt/InputMessages/OutputContent
//      映射到 Run.Input / Run.Output

// TestParseReader_V28_OldLog_NoPromptFields 后向兼容:
// v2.7 时期的 log 行没有 system_prompt / input_messages / output_content /
// output_truncated 字段 — parser 必须用 stdlib json.Unmarshal 自动容错,
// 解析成功 + Run.Input / Run.Output 是 zero values.
func TestParseReader_V28_OldLog_NoPromptFields(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	oldEntry := makeLogEntry("req-old", "sess-1", "prosecutor", "react_speak", 0, 1000, "ok", base)

	var buf bytes.Buffer
	data, _ := json.Marshal(oldEntry)
	buf.Write(data)
	buf.WriteByte('\n')

	st, parseErrors, err := ParseReader(&buf)
	require.NoError(t, err)
	require.Equal(t, 0, parseErrors, "v2.7 log 行必须 zero-error 解析 (向后兼容)")
	require.Len(t, st.Traces["req-old"], 1)

	run := st.Traces["req-old"][0]
	// 后向兼容: 旧 log 行没有 4 个字段, Run.Input / Run.Output 是 zero values.
	require.Nil(t, run.Input, "v2.7 baseline: Run.Input must be nil when LogEntry had no prompt fields")
	require.Equal(t, "", run.Output, "v2.7 baseline: Run.Output must be empty string")
}

// TestParseReader_V28_NewLog_FullPrompts 正向映射:
// v2.8 full mode 写的 log (有 SystemPrompt + InputMessages + OutputContent)
// 必须正确解析, entryToRun 把内容映射到 Run.Input (map[system_prompt+messages]) +
// Run.Output (string).
func TestParseReader_V28_NewLog_FullPrompts(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.Local)
	entry := makeLogEntry("req-full", "sess-1", "judge", "judge_final", 0, 5000, "ok", base)
	entry.SystemPrompt = "你是资深庭审法官"
	entry.InputMessages = []llm.Message{
		{Role: "user", Content: "请给出最终判决"},
		{Role: "assistant", Content: "选项 A 长期收益显著"},
	}
	entry.OutputContent = "判决: 选择 A"
	entry.OutputTruncated = false

	var buf bytes.Buffer
	data, _ := json.Marshal(entry)
	buf.Write(data)
	buf.WriteByte('\n')

	st, parseErrors, err := ParseReader(&buf)
	require.NoError(t, err)
	require.Equal(t, 0, parseErrors)
	require.Len(t, st.Traces["req-full"], 1)

	run := st.Traces["req-full"][0]
	require.NotNil(t, run.Input, "v2.8 full mode must populate Run.Input from prompt fields")
	require.Contains(t, run.Input, "system_prompt")
	require.Equal(t, "你是资深庭审法官", run.Input["system_prompt"])
	require.Contains(t, run.Input, "messages")
	require.Equal(t, "判决: 选择 A", run.Output)
}

// TestEntryToRun_OldLog_ZeroValues 单元测 entryToRun 直接: 旧 LogEntry
// (无 prompt 字段) → Run.Input / Output zero values. 这是为后向兼容的
// 单元契约 (ParseReader 内部用 entryToRun).
func TestEntryToRun_OldLog_ZeroValues(t *testing.T) {
	entry := &agent_gateway.LogEntry{
		RequestID:   "req-direct",
		SessionUUID: "sess-direct",
		AgentType:   "judge",
		TaskType:    "judge_final",
		Status:      "ok",
		LatencyMs:   1000,
	}
	run := entryToRun(entry)
	require.Nil(t, run.Input)
	require.Equal(t, "", run.Output)
}

// TestEntryToRun_NewLog_PopulatedInputAndOutput 单测 entryToRun 新字段映射.
func TestEntryToRun_NewLog_PopulatedInputAndOutput(t *testing.T) {
	entry := &agent_gateway.LogEntry{
		RequestID:     "req-direct-full",
		SessionUUID:   "sess-direct",
		AgentType:     "judge",
		TaskType:      "judge_final",
		Status:        "ok",
		LatencyMs:     5000,
		SystemPrompt:  "你是资深庭审法官",
		InputMessages: []llm.Message{{Role: "user", Content: "请给出判决"}},
		OutputContent: "判决: 选项 A",
	}
	run := entryToRun(entry)
	require.NotNil(t, run.Input)
	require.Equal(t, "你是资深庭审法官", run.Input["system_prompt"])
	require.Equal(t, "判决: 选项 A", run.Output)
}

// 占位: 防 sync 引用未使用 (上方 import 防 lint 警).
var _ = sync.Mutex{}

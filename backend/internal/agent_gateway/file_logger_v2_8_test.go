package agent_gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// v2.8 PR-3 (ADR 0042) — FileLogger 4 字段 (system_prompt / input_messages /
// output_content / output_truncated) 写盘 + truncate 行为测试.
func TestFileLogger_V28_OmitsEmptyPromptFields(t *testing.T) {
	// v2.8 PR-3 (ADR 0042): metadata mode 时 gateway.writeFileLog 不填
	// SystemPrompt/InputMessages/OutputContent 字段 (empty string / nil slice
	// / false bool). file_logger.Write 必须尊重 omitempty, 让 JSON 不含这 4 个
	// key — 否则 parser / grep 会读到噪声字段.
	//
	// 这个测试只验证 file_logger omitempty 行为 (gateway mode 控制是另一层).
	dir := t.TempDir()
	fl := NewFileLogger(dir)
	defer fl.Close()

	// 空 LogEntry: 仅设 RequestID + Status,其余是 zero value.
	entry := LogEntry{
		RequestID: "req-meta",
		Status:    StatusSuccess,
	}
	if err := fl.Write(entry); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agent_gateway_"+todayLocal()+".log"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	line := string(data)

	// omitempty: 4 个字段 (空字符串 + nil slice + false bool) 不应序列化.
	for _, key := range []string{"system_prompt", "input_messages", "output_content", "output_truncated"} {
		if strings.Contains(line, `"`+key+`"`) {
			t.Errorf("omitempty failed: log line should NOT contain %q when value is zero; got %s", key, line)
		}
	}

	// 反序列化也应该 round-trip 到 zero value.
	var got LogEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SystemPrompt != "" {
		t.Errorf("SystemPrompt should be empty after omitempty round-trip, got %q", got.SystemPrompt)
	}
	if len(got.InputMessages) != 0 {
		t.Errorf("InputMessages should be empty after omitempty round-trip, got %+v", got.InputMessages)
	}
	if got.OutputContent != "" {
		t.Errorf("OutputContent should be empty, got %q", got.OutputContent)
	}
	if got.OutputTruncated {
		t.Errorf("OutputTruncated should be false")
	}
}

func TestFileLogger_V28_LogEntryCarriesPromptFields_RoundTrip(t *testing.T) {
	// 单元测: 4 字段在 struct -> JSON -> struct 路径下保真. 这个测试覆盖
	// file_logger 写盘逻辑之外的 pure struct 序列化层 (gateway.writeFileLog
	// 已 wire 满 4 字段; 这里只验证 marshal/unmarshal 对称的契约).
	dir := t.TempDir()
	fl := NewFileLogger(dir)
	defer fl.Close()

	entry := LogEntry{
		RequestID:      "req-full",
		Status:         StatusSuccess,
		SystemPrompt:   "你是庭审律师",
		InputMessages:  []llm.Message{{Role: "user", Content: "请给出结论"}},
		OutputContent:   "选项 A 长期收益显著优于 B",
		OutputTruncated: false,
	}
	if err := fl.Write(entry); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agent_gateway_"+todayLocal()+".log"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var got LogEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SystemPrompt != entry.SystemPrompt {
		t.Errorf("SystemPrompt round-trip: want %q, got %q", entry.SystemPrompt, got.SystemPrompt)
	}
	if len(got.InputMessages) != 1 || got.InputMessages[0].Role != "user" || got.InputMessages[0].Content != "请给出结论" {
		t.Errorf("InputMessages round-trip: got %+v", got.InputMessages)
	}
	if got.OutputContent != entry.OutputContent {
		t.Errorf("OutputContent round-trip: want %q, got %q", entry.OutputContent, got.OutputContent)
	}
	if got.OutputTruncated != false {
		t.Errorf("OutputTruncated should be false: got %v", got.OutputTruncated)
	}
}

func TestTruncateForLogBytes_BelowLimit_NoTruncate(t *testing.T) {
	s := "hello"
	got, trunc := truncateForLogBytes(s, 100)
	if got != s {
		t.Errorf("want unchanged, got %q", got)
	}
	if trunc {
		t.Errorf("should not truncate when len(s) <= maxBytes")
	}
}

func TestTruncateForLogBytes_AboveLimit_TruncatesAtMax(t *testing.T) {
	s := strings.Repeat("a", 100)
	got, trunc := truncateForLogBytes(s, 10)
	if got != strings.Repeat("a", 10) {
		t.Errorf("want 10 bytes, got %d", len(got))
	}
	if !trunc {
		t.Errorf("trunc flag should be true when truncated")
	}
}

func TestTruncateForLogBytes_ZeroMax_NoTruncate(t *testing.T) {
	got, trunc := truncateForLogBytes("hello", 0)
	if got != "" {
		t.Errorf("maxBytes <= 0 should return empty string")
	}
	if trunc {
		t.Errorf("zero maxBytes should not set trunc flag")
	}
}

func TestTruncateForLogBytes_ExactLength_NoTruncate(t *testing.T) {
	s := strings.Repeat("x", 50)
	got, trunc := truncateForLogBytes(s, 50)
	if got != s {
		t.Errorf("want exact match, got %d bytes", len(got))
	}
	if trunc {
		t.Errorf("exact length boundary should not truncate")
	}
}

// todayLocal 跟 production 代码一样用 Local() + 自定义 dateFormat.
func todayLocal() string {
	return nowFunc().Local().Format(dateFormat)
}

// nowFunc 让 date rotation 测试可注入时间点 (测试隔离).
// 用 var 注入而不是 time.Now 直接调用, 方便后续 timezone 边界测试.
// 当前默认 = time.Now, 简单实现.
var nowFunc = func() time.Time { return time.Now() }

// 占位: 防 time 引用未使用 (在 nowFunc 上方已用).
var _ = time.Now

// 占位: 防 encoding/json 引用未使用 (上方的 json.Unmarshal 已用).
var _ = json.Marshal

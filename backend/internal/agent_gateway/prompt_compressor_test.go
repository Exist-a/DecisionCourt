package agent_gateway

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/llm"
)

func TestPromptCompressor_NoOpWhenNormal(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{}, nil)
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
	}
	out, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusNormal})
	if len(out) != 1 || out[0].Content != "hello" {
		t.Errorf("should not compress normal status")
	}
	if info.Applied {
		t.Errorf("applied should be false")
	}
}

func TestPromptCompressor_TrimsLongMessages(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{}, nil)
	msgs := make([]llm.Message, 12)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Content: "msg"}
	}
	out, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusCompress})
	if len(out) != compressKeepHistory {
		t.Errorf("want %d messages, got %d", compressKeepHistory, len(out))
	}
	if !info.Applied {
		t.Errorf("applied should be true")
	}
	if info.BeforeCount != 12 || info.AfterCount != compressKeepHistory {
		t.Errorf("counts wrong: %d -> %d", info.BeforeCount, info.AfterCount)
	}
}

func TestPromptCompressor_TrimsLongContent(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{}, nil)
	long := strings.Repeat("a", compressMaxMsgLen+1)
	msgs := []llm.Message{{Role: "user", Content: long}}
	out, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusThrottle})
	if len(out) != 1 {
		t.Fatalf("want 1 msg, got %d", len(out))
	}
	if len(out[0].Content) != compressTargetLen {
		t.Errorf("content length: want %d got %d", compressTargetLen, len(out[0].Content))
	}
	if info.AfterLength == 0 {
		t.Errorf("AfterLength should be recorded")
	}
}

func TestPromptCompressor_KeepsSystemAtFront(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{}, nil)
	msgs := make([]llm.Message, 12)
	msgs[0] = llm.Message{Role: "system", Content: "sys-prompt"}
	for i := 1; i < len(msgs); i++ {
		msgs[i] = llm.Message{Role: "user", Content: "msg"}
	}
	out, _ := pc.Compress(msgs, BudgetSnapshot{Status: StatusExhausted})
	if out[0].Role != "system" || out[0].Content != "sys-prompt" {
		t.Errorf("system prompt should be kept at front: %+v", out[0])
	}
}

func TestPromptCompressor_EmptyMessages(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{}, nil)
	out, info := pc.Compress(nil, BudgetSnapshot{Status: StatusCompress})
	if len(out) != 0 {
		t.Errorf("want empty, got %d", len(out))
	}
	if info.Applied {
		t.Errorf("applied should be false for empty")
	}
}

// === v2.10 修复：system 消息不再被压缩器截断成失忆 ===
//
// 回归背景：ReAct 的 messages 数组往往只有一条 system（baseRules + 工具说明 +
// 庭审历史，实测 12~14 KB）。它此前和普通消息共用 compressMaxMsgLen(3000)，
// 被砍到 1500 字节（87~89% 指令销毁），模型因此编造「证据7/证据12」并被
// 反幻觉校验打回。

// buildSystemPrompt 造一条 n 字节的 system prompt（全中文，贴近真实场景，
// 同时保证截断必然落在多字节 rune 上）。
func buildSystemPrompt(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.WriteString("法庭规则：必须引用证据编号，不得编造。")
	}
	s := b.String()
	return s[:n]
}

// TestCompressScored_SystemNotTruncated: 12000 字节 system prompt 必须原样保留。
// 这是本次 bug 的直接回归钉（旧实现会变成 1500 字节）。
func TestCompressScored_SystemNotTruncated(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{Enabled: true}, nil)
	sys := buildSystemPrompt(12000)
	msgs := []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: "检察官发言"},
		{Role: "user", Content: "辩护律师发言"},
	}
	out, _ := pc.Compress(msgs, BudgetSnapshot{Status: StatusExhausted})

	var got string
	for _, m := range out {
		if m.Role == "system" {
			got = m.Content
		}
	}
	if got != sys {
		t.Errorf("system prompt 被改动了：want %d bytes, got %d bytes（差 %d）",
			len(sys), len(got), len(sys)-len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("system prompt 不是合法 UTF-8")
	}
}

// TestCompressLegacy_SystemNotTruncated: legacy 路径同样不得截断 system。
func TestCompressLegacy_SystemNotTruncated(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{Enabled: false}, nil)
	sys := buildSystemPrompt(12000)
	msgs := []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: "a"},
		{Role: "user", Content: "b"},
	}
	out, _ := pc.Compress(msgs, BudgetSnapshot{Status: StatusCompress})

	if out[0].Role != "system" {
		t.Fatalf("system should stay first, got %s", out[0].Role)
	}
	if out[0].Content != sys {
		t.Errorf("legacy 路径截断了 system：want %d bytes, got %d",
			len(sys), len(out[0].Content))
	}
}

// TestCompressScored_EarlyReturn_SystemNotTruncated: 只有 system（无非 system
// 消息）时的早返回分支同样不得截断。
func TestCompressScored_EarlyReturn_SystemNotTruncated(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{Enabled: true}, nil)
	sys := buildSystemPrompt(12000)
	out, info := pc.Compress(
		[]llm.Message{{Role: "system", Content: sys}},
		BudgetSnapshot{Status: StatusCompress},
	)
	if len(out) != 1 || out[0].Content != sys {
		t.Errorf("早返回分支截断了 system：want %d bytes, got %d",
			len(sys), len(out[0].Content))
	}
	if info.AfterLength != len(sys) {
		t.Errorf("AfterLength: want %d got %d", len(sys), info.AfterLength)
	}
}

// TestCompressScored_SystemTruncatedOnlyIfPathological: 病态超长（> 24000）
// 仍会被兜底截断，但必须保持合法 UTF-8（旧实现按字节切会切出半个汉字）。
func TestCompressScored_SystemTruncatedOnlyIfPathological(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{Enabled: true}, nil)
	sys := buildSystemPrompt(30000)
	out, _ := pc.Compress(
		[]llm.Message{{Role: "system", Content: sys}, {Role: "user", Content: "x"}},
		BudgetSnapshot{Status: StatusExhausted},
	)

	var got string
	for _, m := range out {
		if m.Role == "system" {
			got = m.Content
		}
	}
	if len(got) >= len(sys) {
		t.Fatalf("病态超长应被兜底截断，got %d bytes (orig %d)", len(got), len(sys))
	}
	if len(got) > compressSystemTargetLen {
		t.Errorf("截断后应不超过 %d 字节，got %d", compressSystemTargetLen, len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("截断结果不是合法 UTF-8（按字节切了半个汉字）")
	}
	if !strings.HasSuffix(got, compressTruncateMark) {
		t.Error("截断后应带 compressTruncateMark")
	}
}

// TestCompressScored_NonSystemStillTruncated: 普通消息的预算语义保持不变
// （确认本次修复没有顺手改坏别的行为）。
func TestCompressScored_NonSystemStillTruncated(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{Enabled: true}, nil)
	long := strings.Repeat("a", compressMaxMsgLen+500)
	out, _ := pc.Compress(
		[]llm.Message{{Role: "user", Content: long}},
		BudgetSnapshot{Status: StatusThrottle},
	)
	if len(out) != 1 {
		t.Fatalf("want 1 msg got %d", len(out))
	}
	if len(out[0].Content) != compressTargetLen {
		t.Errorf("普通消息仍应截到 %d 字节，got %d", compressTargetLen, len(out[0].Content))
	}
}

// TestMsgTruncateLimits: system 与普通消息走两套不同上限。
func TestMsgTruncateLimits(t *testing.T) {
	l, target := msgTruncateLimits(llm.Message{Role: "system"})
	if l != compressMaxSystemMsgLen || target != compressSystemTargetLen {
		t.Errorf("system 应用宽松上限，got (%d, %d)", l, target)
	}
	l, target = msgTruncateLimits(llm.Message{Role: "user"})
	if l != compressMaxMsgLen || target != compressTargetLen {
		t.Errorf("普通消息应用原上限，got (%d, %d)", l, target)
	}
}

// TestCutBytesRuneSafe: 表驱动覆盖 rune 边界回退。
func TestCutBytesRuneSafe(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"ascii 正常切", "abcdef", 3, "abc"},
		{"n=0", "abcdef", 0, ""},
		{"n<0", "abcdef", -1, ""},
		{"n>=len", "abcdef", 99, "abcdef"},
		{"中文正好落边界", "你好", 3, "你"},
		{"中文切在 rune 中间", "你好", 4, "你"},
		{"中文切在第二个字中间", "你好世界", 5, "你"},
		{"中英混合落边界", "a你好", 1, "a"},
		{"中英混合切中间", "a你好", 3, "a"},
		{"全中文全保留", "你好", 6, "你好"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cutBytesRuneSafe(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("want %q got %q", tc.want, got)
			}
			if !utf8.ValidString(got) {
				t.Errorf("结果不是合法 UTF-8: %q", got)
			}
		})
	}
}

// TestTruncateOversizedMessages_ReturnsTotalLength: helper 同时承担统计职责，
// 调用方靠返回值填 info.AfterLength。
func TestTruncateOversizedMessages_ReturnsTotalLength(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: buildSystemPrompt(12000)},
		{Role: "user", Content: "short"},
	}
	total := truncateOversizedMessages(msgs)
	want := len(msgs[0].Content) + len(msgs[1].Content)
	if total != want {
		t.Errorf("want %d got %d", want, total)
	}
}

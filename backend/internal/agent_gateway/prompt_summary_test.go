package agent_gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// stubSummaryGen 是 SummaryGenerator 的可编程假实现。
type stubSummaryGen struct {
	out      string
	err      error
	calls    int
	lastMsgs []llm.Message
}

func (s *stubSummaryGen) GenerateSummary(_ context.Context, msgs []llm.Message) (string, error) {
	s.calls++
	s.lastMsgs = msgs
	return s.out, s.err
}

// TestBuildEarlierSummary_NoKeep_NoInsert: keepMap 全 true → 不应有 anchors。
func TestBuildEarlierSummary_NoKeep_NoInsert(t *testing.T) {
	groups := []AtomicGroup{
		{Indices: []int{0}, GroupLength: 10},
	}
	keep := map[int]bool{0: true}
	out := BuildEarlierSummary(groups, keep, nil)
	if out != "" {
		t.Errorf("expected empty summary when nothing dropped, got %q", out)
	}
}

// TestBuildEarlierSummary_WithAnchor: 被丢的有 evidence_id 标记 → 摘要列出锚点。
func TestBuildEarlierSummary_WithAnchor(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Metadata: map[string]string{"agent_type": "prosecutor"}, Content: "evidence_id=E1 关键陈述"},
		{Role: "user", Content: "plain turn"},
	}
	groups := []AtomicGroup{
		{Indices: []int{0}, GroupLength: 30},
		{Indices: []int{1}, GroupLength: 5},
	}
	keep := map[int]bool{1: true} // 0 被丢
	out := BuildEarlierSummary(groups, keep, msgs)
	if out == "" {
		t.Fatalf("expected non-empty summary when dropped has anchor")
	}
	if !strings.Contains(out, "evidence_id=E1") {
		t.Errorf("summary should preserve evidence_id anchor: %s", out)
	}
	if !strings.Contains(out, "prosecutor") {
		t.Errorf("summary should preserve agent_type: %s", out)
	}
}

// TestBuildEarlierSummary_MaxAnchors: 超过 6 条 anchor 时被截断到 6。
func TestBuildEarlierSummary_MaxAnchors(t *testing.T) {
	msgs := make([]llm.Message, 10)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Metadata: map[string]string{"evidence_id": "E"}, Content: "x"}
	}
	groups := make([]AtomicGroup, len(msgs))
	for i := range groups {
		groups[i] = AtomicGroup{Indices: []int{i}, GroupLength: 1}
	}
	keep := map[int]bool{}
	out := BuildEarlierSummary(groups, keep, msgs)
	// 数 - 个数；maxAnchors = 6，所以锚点行最多 6 条
	count := strings.Count(out, "- [")
	if count > 6 {
		t.Errorf("expected ≤ 6 anchors, got %d (summary=%s)", count, out)
	}
}

// TestBuildEarlierSummary_EmptyMessages: 没有可参考的 messages → 返回 ""。
func TestBuildEarlierSummary_EmptyMessages(t *testing.T) {
	groups := []AtomicGroup{{Indices: []int{0}, GroupLength: 5}}
	keep := map[int]bool{1: true}
	out := BuildEarlierSummary(groups, keep, nil)
	if out != "" {
		t.Errorf("expected empty when msgs is nil and idx 0 out-of-range, got %q", out)
	}
}

// === v2.10 ADR 0044 #6: abstractive summary ===

// v2.10 ADR 0044 #6: generator 返回内容时采用 abstractive 结果。
func TestBuildAbstractiveSummary_UsesGenerator(t *testing.T) {
	gen := &stubSummaryGen{out: "检察官基于 E001 认定被告知情。"}
	dropped := []llm.Message{{Role: "assistant", Content: "略"}}

	got := BuildAbstractiveSummary(gen, dropped, "EXTRACTIVE_FALLBACK")
	if !strings.Contains(got, "检察官基于 E001 认定被告知情。") {
		t.Errorf("should use generator output, got %q", got)
	}
	if strings.Contains(got, "EXTRACTIVE_FALLBACK") {
		t.Errorf("fallback should not appear when generator succeeds, got %q", got)
	}
	if gen.calls != 1 {
		t.Errorf("generator should be called once, got %d", gen.calls)
	}
}

// v2.10 ADR 0044 #6: generator 报错 → 回退 extractive，绝不中断压缩。
func TestBuildAbstractiveSummary_FallbackOnError(t *testing.T) {
	gen := &stubSummaryGen{err: errors.New("llm down")}
	dropped := []llm.Message{{Role: "assistant", Content: "略"}}

	got := BuildAbstractiveSummary(gen, dropped, "EXTRACTIVE_FALLBACK")
	if got != "EXTRACTIVE_FALLBACK" {
		t.Errorf("should fall back on error, got %q", got)
	}
}

// v2.10 ADR 0044 #6: generator 返回空串 → 回退 extractive。
func TestBuildAbstractiveSummary_FallbackOnEmpty(t *testing.T) {
	gen := &stubSummaryGen{out: "   \n  "}
	dropped := []llm.Message{{Role: "assistant", Content: "略"}}

	got := BuildAbstractiveSummary(gen, dropped, "EXTRACTIVE_FALLBACK")
	if got != "EXTRACTIVE_FALLBACK" {
		t.Errorf("should fall back on empty output, got %q", got)
	}
}

// v2.10 ADR 0044 #6: generator 为 nil（未开启）→ 直接用 extractive。
func TestBuildAbstractiveSummary_NilGenerator(t *testing.T) {
	dropped := []llm.Message{{Role: "assistant", Content: "略"}}
	got := BuildAbstractiveSummary(nil, dropped, "EXTRACTIVE_FALLBACK")
	if got != "EXTRACTIVE_FALLBACK" {
		t.Errorf("nil generator should return fallback, got %q", got)
	}
}

// v2.10 ADR 0044 #6: 没有丢弃消息时不调用 generator。
func TestBuildAbstractiveSummary_NoDroppedMessages(t *testing.T) {
	gen := &stubSummaryGen{out: "should not be used"}
	got := BuildAbstractiveSummary(gen, nil, "EXTRACTIVE_FALLBACK")
	if got != "EXTRACTIVE_FALLBACK" {
		t.Errorf("no dropped messages should return fallback, got %q", got)
	}
	if gen.calls != 0 {
		t.Errorf("generator should not be called, got %d calls", gen.calls)
	}
}

// v2.10 ADR 0044 #6: CollectDroppedMessages 只收 keepMap=false 的消息，且保序。
func TestCollectDroppedMessages(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: "keep-0"},
		{Role: "assistant", Content: "drop-1"},
		{Role: "assistant", Content: "drop-2"},
		{Role: "assistant", Content: "keep-3"},
	}
	groups := BuildAtomicGroups(msgs, ScoreMessages(msgs, BudgetSnapshot{}))
	keepMap := map[int]bool{0: true, 3: true}

	dropped := CollectDroppedMessages(groups, keepMap, msgs)
	if len(dropped) != 2 {
		t.Fatalf("want 2 dropped, got %d", len(dropped))
	}
	if dropped[0].Content != "drop-1" || dropped[1].Content != "drop-2" {
		t.Errorf("dropped should preserve order, got %q / %q", dropped[0].Content, dropped[1].Content)
	}
}

// v2.10 ADR 0044 #6: NewSummaryGenerator 对 nil 客户端返回 nil（不 panic）。
func TestNewSummaryGenerator_NilClient(t *testing.T) {
	if gen := NewSummaryGenerator(nil, "m"); gen != nil {
		t.Error("nil client should yield nil generator")
	}
}

// v2.10 ADR 0044 #6: LLM 实现把丢弃消息按角色渲染后交给模型，并返回其输出。
func TestSummaryGenerator_GenerateSummary(t *testing.T) {
	fake := &fakeLLM{completeContent: "摘要正文"}
	gen := NewSummaryGenerator(fake, "deepseek-v4-flash")
	if gen == nil {
		t.Fatal("generator should not be nil for non-nil client")
	}

	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"agent_type": "prosecutor"}, Content: "检察官主张"},
		{Role: "assistant", Metadata: map[string]string{"agent_type": "judge"}, Content: "法官评估"},
	}
	got, err := gen.GenerateSummary(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "摘要正文" {
		t.Errorf("want 摘要正文 got %q", got)
	}
	if fake.completeCalls != 1 {
		t.Errorf("should make exactly one LLM call, got %d", fake.completeCalls)
	}
}

// v2.10 ADR 0044 #6: LLM 报错时向上传播，由 BuildAbstractiveSummary 兜底。
func TestSummaryGenerator_PropagatesError(t *testing.T) {
	fake := &fakeLLM{completeErr: errors.New("boom")}
	gen := NewSummaryGenerator(fake, "m")

	_, err := gen.GenerateSummary(context.Background(), []llm.Message{{Role: "assistant", Content: "x"}})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

// v2.10 ADR 0044 #6: 无消息时不调用 LLM。
func TestSummaryGenerator_EmptyMessagesNoCall(t *testing.T) {
	fake := &fakeLLM{completeContent: "unused"}
	gen := NewSummaryGenerator(fake, "m")

	got, err := gen.GenerateSummary(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("want empty got %q", got)
	}
	if fake.completeCalls != 0 {
		t.Errorf("should not call LLM for empty input, got %d", fake.completeCalls)
	}
}

// v2.10 ADR 0044 #6: 配置门 —— 必须显式开启且 SmartCompression 生效才启用。
func TestIsSmartCompressionAbstractiveSummaryEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  GatewayConfig
		want bool
	}{
		{
			name: "default off",
			cfg:  GatewayConfig{Enabled: true},
			want: false,
		},
		{
			name: "explicit on with smart compression default on",
			cfg:  GatewayConfig{Enabled: true, SmartCompressionAbstractiveSummary: true},
			want: true,
		},
		{
			name: "gateway disabled",
			cfg:  GatewayConfig{Enabled: false, SmartCompressionAbstractiveSummary: true},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.IsSmartCompressionAbstractiveSummaryEnabled(); got != tc.want {
				t.Errorf("want %v got %v", tc.want, got)
			}
		})
	}
}

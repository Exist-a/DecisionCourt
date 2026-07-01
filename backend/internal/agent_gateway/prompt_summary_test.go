package agent_gateway

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

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

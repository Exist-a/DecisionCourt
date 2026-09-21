package agent_gateway

import (
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestBuildAtomicGroups_ToolCallAtomic: tool_call ↔ tool_result 同组。
func TestBuildAtomicGroups_ToolCallAtomic(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "t1"}, Content: "call t1"},
		{Role: "tool", Metadata: map[string]string{"tool_call_id": "t1"}, Content: "result of t1"},
		{Role: "user", Content: "plain"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	groups := BuildAtomicGroups(msgs, scored)

	if len(groups) != 2 {
		t.Fatalf("want 2 groups (1 tool+1 plain), got %d", len(groups))
	}
	var toolGroup, plainGroup *AtomicGroup
	for i := range groups {
		if groups[i].ID != "" {
			toolGroup = &groups[i]
		} else {
			plainGroup = &groups[i]
		}
	}
	if toolGroup == nil || len(toolGroup.Indices) != 2 {
		t.Errorf("tool group should hold both assistant and tool result")
	}
	if plainGroup == nil || len(plainGroup.Indices) != 1 {
		t.Errorf("plain should be single-member group")
	}
}

// TestBuildAtomicGroups_NoMetadata: 没有 metadata 时每条都是单成员组。
func TestBuildAtomicGroups_NoMetadata(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "a"},
		{Role: "user", Content: "b"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	groups := BuildAtomicGroups(msgs, scored)
	if len(groups) != 2 {
		t.Errorf("want 2 single-member groups, got %d", len(groups))
	}
	for _, g := range groups {
		if g.ID != "" {
			t.Errorf("non-tool group should have empty ID, got %q", g.ID)
		}
		if len(g.Indices) != 1 {
			t.Errorf("each group should be 1 message, got %d", len(g.Indices))
		}
	}
}

// TestBuildAtomicGroups_MultipleToolCalls: 不同 tool_call_id 各自独立成组。
func TestBuildAtomicGroups_MultipleToolCalls(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "a"}, Content: "a call"},
		{Role: "tool", Metadata: map[string]string{"tool_call_id": "a"}, Content: "a result"},
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "b"}, Content: "b call"},
		{Role: "tool", Metadata: map[string]string{"tool_call_id": "b"}, Content: "b result"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	groups := BuildAtomicGroups(msgs, scored)
	if len(groups) != 2 {
		t.Errorf("want 2 separate tool groups, got %d", len(groups))
	}
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.ID] = true
	}
	if !seen["tool:a"] || !seen["tool:b"] {
		t.Errorf("expected tool:a and tool:b groups, got %+v", seen)
	}
}

// v2.10 ADR 0044 #3: evidence_id 链分组 —— 多条消息共享同一 evidence_id 时应绑定为原子组。
func TestBuildAtomicGroups_EvidenceChain(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"agent_type": "prosecutor"}, Content: "证据 E001 说明..."},
		{Role: "assistant", Metadata: map[string]string{"agent_type": "defender", "evidence_id": "E001"}, Content: "反驳 E001..."},
		{Role: "assistant", Metadata: map[string]string{"agent_type": "judge", "evidence_id": "E001"}, Content: "评估 E001..."},
		{Role: "assistant", Metadata: map[string]string{"agent_type": "prosecutor"}, Content: "无关发言"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	groups := BuildAtomicGroups(msgs, scored)

	// 应有 2 组：1 个 evidence:E001 组（2 条）+ 2 个 singleton（index 0 和 3）
	// 注意：index 0 没有 evidence_id metadata，所以只有 index 1 和 2 被分到 evidence 组
	evidenceGroupCount := 0
	singletonCount := 0
	for _, g := range groups {
		if g.ID == "evidence:E001" {
			evidenceGroupCount++
			if len(g.Indices) != 2 {
				t.Errorf("evidence group should have 2 members, got %d", len(g.Indices))
			}
		} else if g.ID == "" {
			singletonCount++
		}
	}
	if evidenceGroupCount != 1 {
		t.Errorf("want 1 evidence group, got %d", evidenceGroupCount)
	}
	if singletonCount != 2 {
		t.Errorf("want 2 singletons (index 0 and 3), got %d", singletonCount)
	}
}

// v2.10 ADR 0044 #3: tool_call 分组优先于 evidence_id 分组。
func TestBuildAtomicGroups_EvidenceChain_ToolCallPrecedence(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "t1", "evidence_id": "E001"}, Content: "tool call with evidence"},
		{Role: "tool", Metadata: map[string]string{"tool_call_id": "t1"}, Content: "tool result"},
		{Role: "assistant", Metadata: map[string]string{"evidence_id": "E001"}, Content: "evidence ref only"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	groups := BuildAtomicGroups(msgs, scored)

	// index 0 和 1 应在 tool:t1 组（tool_call 优先），index 2 应是 singleton
	for _, g := range groups {
		if g.ID == "tool:t1" {
			if len(g.Indices) != 2 {
				t.Errorf("tool group should have 2 members, got %d", len(g.Indices))
			}
		} else if g.ID == "evidence:E001" {
			t.Error("tool_call should take precedence over evidence_id grouping")
		}
	}
}

// v2.10 ADR 0044 #3: 单条 evidence_id 不应成组（留给 singleton 处理）。
func TestBuildAtomicGroups_EvidenceChain_SingleRef(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"evidence_id": "E001"}, Content: "only ref"},
		{Role: "assistant", Content: "plain"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	groups := BuildAtomicGroups(msgs, scored)

	for _, g := range groups {
		if g.ID != "" {
			t.Errorf("single evidence ref should not form a group, got %q", g.ID)
		}
	}
}

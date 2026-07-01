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

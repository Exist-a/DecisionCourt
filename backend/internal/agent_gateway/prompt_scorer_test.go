package agent_gateway

import (
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestScoreMessages_RoleBoost: judge 角色拿 1.5 / system 拿 2.0。
func TestScoreMessages_RoleBoost(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Metadata: map[string]string{"agent_type": "judge"}, Content: "analyze"},
		{Role: "user", Content: "usermsg"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	if scored[0].RoleWeight != 1.5 {
		t.Errorf("judge: want 1.5 got %f", scored[0].RoleWeight)
	}
	if scored[1].RoleWeight != 1.0 {
		t.Errorf("default: want 1.0 got %f", scored[1].RoleWeight)
	}
}

// TestScoreMessages_PositionBoost: 首末各 +0.3。
func TestScoreMessages_PositionBoost(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "open"},
		{Role: "user", Content: "mid"},
		{Role: "user", Content: "end"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	if scored[0].PositionBoost != 0.3 {
		t.Errorf("first: want 0.3 got %f", scored[0].PositionBoost)
	}
	if scored[1].PositionBoost != 0 {
		t.Errorf("mid: want 0 got %f", scored[1].PositionBoost)
	}
	if scored[2].PositionBoost != 0.3 {
		t.Errorf("last: want 0.3 got %f", scored[2].PositionBoost)
	}
}

// TestScoreMessages_ReferenceBoost: 含 evidence_id / @prosecutor 等 +0.3。
func TestScoreMessages_ReferenceBoost(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    float64
	}{
		{"plain", "regular content", 0},
		{"evidence", "见 evidence_id=E123 记载", 0.3},
		{"chinese ref", "刚才李律师提到的", 0.3},
		{"english ref", "as @defender argued earlier", 0.3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs := []llm.Message{{Role: "user", Content: tc.content}}
			scored := ScoreMessages(msgs, BudgetSnapshot{})
			if scored[0].ReferenceBoost != tc.want {
				t.Errorf("want %f got %f", tc.want, scored[0].ReferenceBoost)
			}
		})
	}
}

// TestScoreMessages_TypeBoost: assistant 含 tool_call_id 时 +0.5。
func TestScoreMessages_TypeBoost(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "t1"}, Content: "use tool"},
		{Role: "assistant", Content: "plain"},
		{Role: "tool", Metadata: map[string]string{"tool_call_id": "t1"}, Content: "result"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	if scored[0].TypeBoost != 0.5 {
		t.Errorf("tool_call_id owner: want 0.5 got %f", scored[0].TypeBoost)
	}
	if scored[1].TypeBoost != 0 {
		t.Errorf("plain assistant: want 0 got %f", scored[1].TypeBoost)
	}
	if scored[2].TypeBoost != 0 {
		t.Errorf("tool result: want 0 (only owner scored) got %f", scored[2].TypeBoost)
	}
}

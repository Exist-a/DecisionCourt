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

// v2.10 ADR 0044 #4: recency decay —— 同等内容质量下，越新的消息分数越高。
//
// 注意衰减是"混入"而非"覆盖"：首尾消息的 position_boost(+0.3) 仍会体现在
// final 上，所以整条序列不保证严格单调。这里只断言"中间段"（无 position
// boost 的同质消息）严格递增，这才是 recency 信号本身的语义。
func TestScoreMessages_RecencyDecay_Monotonic(t *testing.T) {
	// 5 条完全同质的消息：role/reference/type 全同，只有位置不同
	msgs := make([]llm.Message, 5)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "assistant", Content: "identical"}
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	// idx 1..3 均为 raw=1.2（无 position boost），应严格递增
	for i := 2; i <= 3; i++ {
		if scored[i].Score <= scored[i-1].Score {
			t.Errorf("recency decay: middle scores should increase, but scored[%d]=%.4f <= scored[%d]=%.4f",
				i, scored[i].Score, i-1, scored[i-1].Score)
		}
	}
	// 最后一条同时拿到 position_boost + 最高 recency，必须是最大值
	last := len(scored) - 1
	for i := 0; i < last; i++ {
		if scored[last].Score <= scored[i].Score {
			t.Errorf("last message should rank highest, got %.4f <= scored[%d]=%.4f",
				scored[last].Score, i, scored[i].Score)
		}
	}
}

// v2.10 ADR 0044 #4: 精确公式 final = 0.7×score + 0.3×(i/(n-1))。
func TestScoreMessages_RecencyDecay_Formula(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: "a"},
		{Role: "assistant", Content: "b"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})

	// idx 0: raw = 1.2(assistant) + 0.3(position first) = 1.5; weight = 0/1 = 0
	want0 := 0.7*1.5 + 0.3*0.0
	if diff := scored[0].Score - want0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("idx0: want %.4f got %.4f", want0, scored[0].Score)
	}
	// idx 1: raw = 1.2(assistant) + 0.3(position last) = 1.5; weight = 1/1 = 1
	want1 := 0.7*1.5 + 0.3*1.0
	if diff := scored[1].Score - want1; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("idx1: want %.4f got %.4f", want1, scored[1].Score)
	}
}

// v2.10 ADR 0044 #4: 单条消息不施加 decay（n>1 守卫），分数保持原公式。
func TestScoreMessages_RecencyDecay_SingleMessageUnchanged(t *testing.T) {
	msgs := []llm.Message{{Role: "assistant", Content: "only"}}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	// 无 decay：1.2 + 0.3(first 也是 last) = 1.5
	if diff := scored[0].Score - 1.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("single message should skip decay, want 1.5 got %.4f", scored[0].Score)
	}
}

// v2.10 ADR 0044 #4: decay 只改 Score，不改分项字段（供诊断/测试断言）。
func TestScoreMessages_RecencyDecay_ComponentsPreserved(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Metadata: map[string]string{"agent_type": "judge"}, Content: "open"},
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "t1"}, Content: "call"},
		{Role: "user", Content: "end"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	if scored[0].RoleWeight != 1.5 {
		t.Errorf("RoleWeight should be untouched, got %f", scored[0].RoleWeight)
	}
	if scored[1].TypeBoost != 0.5 {
		t.Errorf("TypeBoost should be untouched, got %f", scored[1].TypeBoost)
	}
	if scored[0].PositionBoost != 0.3 || scored[2].PositionBoost != 0.3 {
		t.Errorf("PositionBoost should be untouched, got %f / %f",
			scored[0].PositionBoost, scored[2].PositionBoost)
	}
}

// v2.10 ADR 0044 #4: 早期高分消息不应压过近期消息（本条问题要修的场景）。
// 开场陈词（role 权重更高）在极端 budget 下曾挤掉近期法官推理。
func TestScoreMessages_RecencyDecay_RecentBeatsEarlyOnTie(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Metadata: map[string]string{"agent_type": "prosecutor"}, Content: "opening statement"},
		{Role: "user", Metadata: map[string]string{"agent_type": "judge"}, Content: "clerk summary"},
	}
	scored := ScoreMessages(msgs, BudgetSnapshot{})
	// prosecutor(1.0) 早 vs judge(1.5) 晚 —— judge 同时赢在 role 和 recency
	if scored[1].Score <= scored[0].Score {
		t.Errorf("later judge message should outrank earlier prosecutor, got %.4f vs %.4f",
			scored[1].Score, scored[0].Score)
	}
}

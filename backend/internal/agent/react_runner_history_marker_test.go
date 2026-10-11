package agent

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/model"
)

// TestBuildInitialMessages_UsesSharedHistoryMarker 护栏：ReAct 的 system 拼接
// 必须使用 agent_gateway.TranscriptHistoryMarker 作为历史区段分隔，否则压缩器
// 的"保最近砍最旧"（M8-2 A1）会因切分点找不到而失效。
//
// 单一来源约束：两端各有一枚测试钉住同一字面量（gateway 侧 pin 常量值，
// 这里 pin 拼接线真的用了它）。
func TestBuildInitialMessages_UsesSharedHistoryMarker(t *testing.T) {
	r := NewReActRunner(nil, "role-system", nil, RunnerConfig{MaxIterations: 1})
	msgs := r.buildInitialMessages([]model.Message{
		{ActionType: "speech", Content: "第一轮发言"},
		{ActionType: "challenge", Content: "第二轮质证"},
	})
	if len(msgs) < 3 {
		t.Fatalf("M8-2 第 2 轮：应为 契约块 + 历史标记 + N 条历史行，got %d", len(msgs))
	}
	// 第一条必须是契约块（baseRules + 工具协议），不可压。
	if msgs[0].Role != "system" || !agent_gateway.IsContractSystemMessage(msgs[0]) {
		t.Fatalf("首条应为契约块 system 消息")
	}
	// 素材块必须包含共享的历史区段标记。
	var joined strings.Builder
	for _, m := range msgs[1:] {
		if !agent_gateway.IsMaterialMessage(m) {
			t.Fatalf("历史行必须是素材块（可压缩）")
		}
		joined.WriteString(m.Content)
	}
	if !strings.Contains(joined.String(), agent_gateway.TranscriptHistoryMarker) {
		t.Fatalf("素材块必须包含共享的历史区段标记，否则压缩器无法只丢最旧历史")
	}
	// 无历史时不应出现素材块。
	empty := r.buildInitialMessages(nil)
	if len(empty) != 1 || !agent_gateway.IsContractSystemMessage(empty[0]) {
		t.Fatalf("无转写时应只有契约块，got %d", len(empty))
	}
}

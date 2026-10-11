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
	if len(msgs) != 1 || msgs[0].Role != "system" {
		t.Fatalf("buildInitialMessages 应产出单条 system 消息，got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, agent_gateway.TranscriptHistoryMarker) {
		t.Fatalf("system 消息必须包含共享的历史区段标记，否则压缩器无法只丢最旧历史")
	}
	// 无历史时不应出现标记（避免空区段）。
	empty := r.buildInitialMessages(nil)
	if strings.Contains(empty[0].Content, agent_gateway.TranscriptHistoryMarker) {
		t.Errorf("无转写时不应出现历史区段标记")
	}
}

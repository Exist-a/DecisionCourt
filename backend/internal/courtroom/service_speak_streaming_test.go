package courtroom

import (
	"context"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// streamingLLM / buildStreamingSpeakService / nopLLM / stubSearcher
// 已迁移到 fakes_test.go 复用，本文件只保留业务断言。

// TestServiceSpeakWithReAct_BroadcastsSpeakChunkEvents 是这次流式 UX
// 改造的端到端测试：律师最终发言时，后端应当把 LLM 的逐个 chunk 转发到
// WS。前端 AgentAvatar 监听 agent.speak_chunk 实现打字机动画。
//
// 流式协议：LLM 以最小 JSON 形式输出 `{"content":"完整发言..."}`，
// 后端正则提取 content 字段并广播。
func TestServiceSpeakWithReAct_BroadcastsSpeakChunkEvents(t *testing.T) {
	// chunks 累积后是完整 JSON：{"content":"基于市场长期数据，选项 A在收益上更稳健。"}
	chunks := []string{
		`{"content":"基于市场`,
		`长期数据`,
		`，选项 A`,
		`在收益上`,
		`更稳健。"}`,
	}
	svc := buildStreamingSpeakService(t, chunks)

	var (
		mu      sync.Mutex
		records []agentSpeakChunkRecord
	)
	svc.broadcaster = func(_ string, ev Event) {
		mu.Lock()
		defer mu.Unlock()
		if ev.Type != "agent.speak_chunk" {
			return
		}
		chunk, _ := ev.Payload["chunk"].(string)
		accumulated, _ := ev.Payload["accumulated"].(string)
		agentID, _ := ev.Payload["agent_id"].(string)
		agentType, _ := ev.Payload["agent_type"].(string)
		records = append(records, agentSpeakChunkRecord{
			Chunk: chunk, Accumulated: accumulated,
			AgentID: agentID, AgentType: agentType,
		})
	}

	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-stream-bc"}
	ag := model.Agent{
		ID:        uuid.New(),
		AgentUUID: "ag-uuid-pro",
		AgentType: model.AgentProsecutor,
		SessionID: session.ID,
		BeliefA:   0.7,
	}
	// 不写 DB：speakWithReAct 只需要 ag 字段，nil DB 也能跑通。

	speaker, err := svc.speakWithReAct(context.Background(), ag, session, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "基于市场长期数据，选项 A在收益上更稳健。", speaker.Content)

	mu.Lock()
	defer mu.Unlock()

	// 每个流式 chunk 必须对应一次 broadcast
	require.GreaterOrEqual(t, len(records), 3, "应当至少广播 3 个 chunk；实际=%d", len(records))

	// 每个 broadcast 都必须带 agent_id / agent_type，前端用来路由到对应头像
	for _, r := range records {
		require.NotEmpty(t, r.AgentID, "agent_id 必须被填充")
		require.NotEmpty(t, r.AgentType, "agent_type 必须被填充")
		require.Contains(t, r.Accumulated, r.Chunk, "accumulated 必须包含本 chunk")
	}

	// accumulated 必须单调递增 —— 验证前端 bubble 拼接不会"倒退"
	var prev string
	for i, r := range records {
		require.True(t, len(r.Accumulated) >= len(prev),
			"accumulated 必须单调递增；records[%d]=%q 不长于 prev=%q", i, r.Accumulated, prev)
		prev = r.Accumulated
	}
}

package courtroom

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// streamingLLM 是 courtroom 测试用的"流式 LLM"fake：第一次 Complete
// 返回决策 JSON，第二次 StreamComplete 按 chunks 顺序发射字符串片段。
//
// 这是 courtroom 包内 fixture —— 之所以不在 agent 包复用，是因为 courtroom
// 测试需要的是「真实 courtroom.Service 接通 + ws broadcast 路径」的端到端
// 验证，跟 agent 单测的颗粒度不同。
type streamingLLM struct {
	decisionJSON string
	streamChunks []string

	mu              sync.Mutex
	completeCalls   int
	streamCalls     int
	streamedContent string
}

func (s *streamingLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeCalls++
	return s.decisionJSON, llm.Usage{}, nil
}

func (s *streamingLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	s.mu.Lock()
	chunks := append([]string{}, s.streamChunks...)
	s.streamCalls++
	s.mu.Unlock()

	out := make(chan llm.StreamChunk, len(chunks)+1)
	go func() {
		defer close(out)
		var collected strings.Builder
		for _, c := range chunks {
			collected.WriteString(c)
			out <- llm.StreamChunk{Content: c}
			time.Sleep(2 * time.Millisecond)
		}
		s.mu.Lock()
		s.streamedContent = collected.String()
		s.mu.Unlock()
		out <- llm.StreamChunk{Done: true}
	}()
	return out
}

// buildStreamingSpeakService 仿照 buildDispatchService，但用 streamingLLM
// 替换默认 LLM client，便于端到端验证 speakWithReAct 的 chunk 广播路径。
func buildStreamingSpeakService(t *testing.T, chunks []string) *Service {
	t.Helper()
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	evSvc := evidence.NewService(nil, nopLLM{})

	decision := `{"action":"speak","reasoning":"控方主张 A 长期收益更稳健","content":"","stance":"pro_a","confidence":0.85,"evidence_refs":[]}`
	llmClient := &streamingLLM{decisionJSON: decision, streamChunks: chunks}
	orch := agent.NewOrchestrator(llmClient, bus, memRepo)

	invRepo := investigation.NewInMemoryRepository(nil)
	invSvc := investigation.NewService(invRepo, bus, &stubSearcher{})

	svc := &Service{
		db:               nil,
		stateMachine:     NewStateMachine(),
		orchestrator:     orch,
		evidenceSvc:      evSvc,
		investigationSvc: invSvc,
		beliefEngine:     nil,
		searcher:         &stubSearcher{},
		a2aBus:           bus,
		broadcaster:      func(string, Event) {},
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}
	return svc
}

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

// agentSpeakChunkRecord 是测试用的 chunk 广播记录。
type agentSpeakChunkRecord struct {
	Chunk       string
	Accumulated string
	AgentID     string
	AgentType   string
}
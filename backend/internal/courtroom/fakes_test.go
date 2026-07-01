package courtroom

// 本文件汇总 courtroom 包所有单元测试复用的 fake / stub / 工厂 / 断言 helper。
// 之前这些 fakes 散落于 dispatch_investigator_test.go / service_speak_streaming_test.go
// / service_react_helpers_test.go / dispatch_investigator_events_test.go，每写一个新
// case 都要重新声明一次。本文件统一以避免重复，并让 *test.go 文件只剩业务断言。

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
)

// ---------- LLM fakes ----------

// reactScriptedLLM 是 deterministic 的脚本化 LLM，按 scripts 顺序返回字符串。
// 既被单元测试复用，也被集成测试复用。
type reactScriptedLLM struct {
	mu      sync.Mutex
	scripts []string
	calls   int
}

func (r *reactScriptedLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := r.calls
	r.calls++
	if idx >= len(r.scripts) {
		idx = len(r.scripts) - 1
	}
	return r.scripts[idx], llm.Usage{}, nil
}

// StreamComplete：默认假 LLM 不实现流式，立刻关闭 channel。
// 真正的流式测试用 streamingScriptedLLM（见 react_runner_streaming_test.go）。
func (r *reactScriptedLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true, Err: fmt.Errorf("reactScriptedLLM: streaming not supported in this fixture")}
	close(out)
	return out
}

// speakJSON / toolJSON 生成 LLM agent_output JSON。
func speakJSON(content, stance string, confidence float64) string {
	b, _ := json.Marshal(agent.AgentOutput{
		Action:       agent.ActionSpeak,
		Reasoning:    "final thought",
		Content:      content,
		EvidenceRefs: []string{},
		Confidence:   confidence,
		Stance:       stance,
	})
	return string(b)
}

func toolJSON(tool string, input map[string]interface{}) string {
	b, _ := json.Marshal(agent.AgentOutput{
		Action:    agent.ActionToolCall,
		Reasoning: "thinking",
		Tool:      tool,
		ToolInput: input,
	})
	return string(b)
}

// streamingLLM 第一次 Complete 返回 decisionJSON；StreamComplete 按 streamChunks 顺序发射片段。
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

// nopLLM returns empty responses; evidence evaluation will fall back to the
// keyword-based estimator which keeps the test deterministic.
type nopLLM struct{}

func (nopLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	return "", llm.Usage{}, fmt.Errorf("noop LLM: not used in dispatch tests")
}

func (nopLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

// ---------- Searcher fakes ----------

// stubSearcher 返回固定的 results，并记录所有收到的 query。
type stubSearcher struct {
	mu      sync.Mutex
	results []search.Result
	queries []string
}

func (s *stubSearcher) Name() string { return "stub" }
func (s *stubSearcher) Search(_ context.Context, q string) ([]search.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	return s.results, nil
}

func (s *stubSearcher) Queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.queries...)
}

// failingSearcher 总是返回 error —— 用于验证 search.completed 兜底事件。
type failingSearcher struct{ err error }

func (f *failingSearcher) Name() string { return "failing" }
func (f *failingSearcher) Search(_ context.Context, _ string) ([]search.Result, error) {
	return nil, f.err
}

type errFake string

func (e errFake) Error() string { return strings.ReplaceAll(string(e), " ", "_") }

// ---------- Evidence spy ----------

// spyEvidenceSvc records every Create call to assert DispatchInvestigator never
// creates Evidence rows (findings go to investigation_findings instead).
type spyEvidenceSvc struct {
	*evidence.Service
	mu      sync.Mutex
	creates []evidenceCreateCall
}

type evidenceCreateCall struct {
	sessionID uuid.UUID
	content   string
}

func (s *spyEvidenceSvc) Create(sessionID uuid.UUID, content, evType, source, submittedBy string) (model.Evidence, error) {
	s.mu.Lock()
	s.creates = append(s.creates, evidenceCreateCall{sessionID: sessionID, content: content})
	s.mu.Unlock()
	return s.Service.Create(sessionID, content, evType, source, submittedBy)
}

func (s *spyEvidenceSvc) Creates() []evidenceCreateCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]evidenceCreateCall{}, s.creates...)
}

// ---------- Service 工厂 ----------

// buildDispatchService spins up an isolated Service backed by in-memory repositories.
// nil *gorm.DB：测试必须避免任何写库路径（零结果 searcher 保证）。
func buildDispatchService(t *testing.T, results []search.Result) (*Service, *spyEvidenceSvc, *a2a.InMemoryRepository, *investigation.InMemoryRepository, *stubSearcher) {
	t.Helper()
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	innerEvSvc := evidence.NewService(nil, nopLLM{})
	evSpy := &spyEvidenceSvc{Service: innerEvSvc}
	searcher := &stubSearcher{results: results}
	invRepo := investigation.NewInMemoryRepository(nil)
	invSvc := investigation.NewService(invRepo, bus, searcher)

	orchestrator := agent.NewOrchestratorLegacy(nopLLM{}, bus, memRepo)

	svc := &Service{
		db:               nil,
		stateMachine:     NewStateMachine(),
		orchestrator:     orchestrator,
		evidenceSvc:      evSpy,
		investigationSvc: invSvc,
		beliefEngine:     nil,
		searcher:         searcher,
		a2aBus:           bus,
		broadcaster:      func(string, Event) {},
		activeCalls:      map[string]context.CancelFunc{},
		sessionLocks:     map[string]*sync.Mutex{},
	}
	return svc, evSpy, a2aRepo, invRepo, searcher
}

// buildStreamingSpeakService 仿 buildDispatchService，但用 streamingLLM 接通
// speakWithReAct 的 chunk 广播路径，便于端到端验证。
func buildStreamingSpeakService(t *testing.T, chunks []string) *Service {
	t.Helper()
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	evSvc := evidence.NewService(nil, nopLLM{})

	decision := `{"action":"speak","reasoning":"控方主张 A 长期收益更稳健","content":"","stance":"pro_a","confidence":0.85,"evidence_refs":[]}`
	llmClient := &streamingLLM{decisionJSON: decision, streamChunks: chunks}
	orch := agent.NewOrchestratorLegacy(llmClient, bus, memRepo)

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

// ---------- 事件 / 消息辅助 ----------

// findA2AByType 在 A2A 列表里找出指定 type 的那一条。
func findA2AByType(rows []model.A2AMessage, mt a2a.MessageType) *model.A2AMessage {
	for i := range rows {
		if rows[i].MessageType == string(mt) {
			return &rows[i]
		}
	}
	return nil
}

// indexOfEvent 返回指定类型事件在列表中的索引，-1 表示不存在。
func indexOfEvent(events []Event, typ string) int {
	for i, ev := range events {
		if ev.Type == typ {
			return i
		}
	}
	return -1
}

// containsEventType 报告 events 列表是否包含 type == typ 的事件。
func containsEventType(events []Event, typ string) bool {
	return indexOfEvent(events, typ) >= 0
}

// findEventByType 返回列表中第一条 type == typ 的事件指针，未找到返回 nil。
func findEventByType(events []Event, typ string) *Event {
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

// agentSpeakChunkRecord 是测试用的 chunk 广播记录。
type agentSpeakChunkRecord struct {
	Chunk       string
	Accumulated string
	AgentID     string
	AgentType   string
}

package investigation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// stubSearcher returns a fixed set of results and records the queries it
// received so tests can assert that the Investigator was actually invoked.
type stubSearcher struct {
	mu      sync.Mutex
	results []search.Result
	queries []string
	err     error
}

func (s *stubSearcher) Name() string { return "stub" }

func (s *stubSearcher) Search(_ context.Context, q string) ([]search.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

func (s *stubSearcher) Queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.queries...)
}

// buildService spins up an isolated investigation.Service backed by an
// in-memory repository and a fresh A2A bus. Returns (svc, repo, a2aRepo).
func buildService(t *testing.T, results []search.Result) (*Service, *InMemoryRepository, *a2a.InMemoryRepository, *stubSearcher) {
	t.Helper()
	repo := NewInMemoryRepository(nil)
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	searcher := &stubSearcher{results: results}
	svc := NewService(repo, bus, searcher)
	return svc, repo, a2aRepo, searcher
}

func TestRecordFinding_PersistsAndReturnsFinding(t *testing.T) {
	results := []search.Result{
		{Title: "报告 A", URL: "https://a", Content: "数据 1"},
		{Title: "报告 B", URL: "https://b", Content: "数据 2"},
		{Title: "报告 C", URL: "https://c", Content: "数据 3"},
	}
	svc, repo, _, searcher := buildService(t, results)
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "sess-1",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
	}

	finding, err := svc.RecordFinding(context.Background(), session, string(model.AgentProsecutor), "行业增长率")
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, finding.ID)
	require.NotEmpty(t, finding.FindingUUID)
	require.Equal(t, session.ID, finding.SessionID)
	require.Equal(t, string(model.AgentProsecutor), finding.Dispatcher)
	require.Equal(t, string(model.AgentInvestigator), finding.Investigator)
	require.Equal(t, "行业增长率", finding.Query)
	require.Equal(t, 3, finding.ResultCount)
	require.Equal(t, "stub", finding.SourceProvider)
	require.NotEmpty(t, finding.Summary)
	require.False(t, finding.CreatedAt.IsZero())

	// 搜索器被调用了 1 次，参数正确
	require.Equal(t, []string{"行业增长率"}, searcher.Queries())

	// 持久化：repo 中能找到
	stored, err := repo.GetByID(context.Background(), finding.ID)
	require.NoError(t, err)
	require.Equal(t, finding.FindingUUID, stored.FindingUUID)
}

func TestRecordFinding_BroadcastsPublicA2ADispatchAndReport(t *testing.T) {
	results := []search.Result{
		{Title: "A", URL: "u", Content: "a"},
	}
	svc, _, a2aRepo, _ := buildService(t, results)
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "sess-pub",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 2,
	}

	_, err := svc.RecordFinding(context.Background(), session, string(model.AgentDefender), "租房风险")
	require.NoError(t, err)

	// 应有 2 条 A2A 消息：dispatch（public）+ report（public）
	rows, err := a2aRepo.ListVisibleTo(context.Background(), session.ID, a2a.AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// dispatch: defender → investigator, public, query="租房风险"
	dispatchRow := findByType(rows, a2a.MessageTypeDispatch)
	require.NotNil(t, dispatchRow)
	require.Equal(t, string(model.AgentDefender), dispatchRow.FromAgent)
	require.Equal(t, string(model.AgentInvestigator), dispatchRow.ToAgent)
	require.Equal(t, string(a2a.VisibilityPublic), dispatchRow.Visibility)
	dispatchPayload, err := a2a.DecodePayload(*dispatchRow)
	require.NoError(t, err)
	require.Equal(t, "租房风险", dispatchPayload["query"])

	// report: investigator → defender, public, finding_id 应存在
	reportRow := findByType(rows, a2a.MessageTypeReport)
	require.NotNil(t, reportRow)
	require.Equal(t, string(model.AgentInvestigator), reportRow.FromAgent)
	require.Equal(t, string(model.AgentDefender), reportRow.ToAgent)
	require.Equal(t, string(a2a.VisibilityPublic), reportRow.Visibility)
	reportPayload, err := a2a.DecodePayload(*reportRow)
	require.NoError(t, err)
	require.Equal(t, "租房风险", reportPayload["query"])
	require.NotEmpty(t, reportPayload["finding_id"])
	require.Equal(t, float64(1), reportPayload["result_count"])

	// 对手（prosecutor）也应该能看到这两条（公开）
	defRows, err := a2aRepo.ListVisibleTo(context.Background(), session.ID, string(model.AgentProsecutor))
	require.NoError(t, err)
	require.Len(t, defRows, 2, "对方律师应能查看公开的调查记录")
}

func TestRecordFinding_NoResultsReturnsFindingWithCountZero(t *testing.T) {
	svc, repo, _, _ := buildService(t, nil)
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-empty"}

	finding, err := svc.RecordFinding(context.Background(), session, string(model.AgentProsecutor), "完全没结果")
	require.NoError(t, err)
	require.Equal(t, 0, finding.ResultCount)
	require.Contains(t, finding.Summary, "无搜索结果")

	// 仍然要持久化
	_, err = repo.GetByID(context.Background(), finding.ID)
	require.NoError(t, err)
}

func TestRecordFinding_RejectsInvalidDispatcher(t *testing.T) {
	svc, _, _, _ := buildService(t, nil)
	session := model.CourtSession{ID: uuid.New()}

	_, err := svc.RecordFinding(context.Background(), session, "judge", "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dispatcher must be")
}

func TestRecordFinding_RejectsEmptyQuery(t *testing.T) {
	svc, _, _, _ := buildService(t, nil)
	session := model.CourtSession{ID: uuid.New()}

	_, err := svc.RecordFinding(context.Background(), session, string(model.AgentProsecutor), "  ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "query is required")
}

func TestRecordFinding_PropagatesSearchError(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	searcher := &stubSearcher{err: fmt.Errorf("upstream timeout")}
	svc := NewService(repo, bus, searcher)
	session := model.CourtSession{ID: uuid.New()}

	_, err := svc.RecordFinding(context.Background(), session, string(model.AgentProsecutor), "q")
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream timeout")
}

func TestListBySession_OrdersByCreatedAt(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	repo := NewInMemoryRepository(clock.Now)
	bus := a2a.NewBus(a2a.NewInMemoryRepository(nil), nil)
	searcher := &stubSearcher{results: []search.Result{{Title: "t", URL: "u", Content: "c"}}}
	svc := NewService(repo, bus, searcher)
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-list"}

	// 插入 3 条，时钟按顺序递增
	for _, q := range []string{"query1", "query2", "query3"} {
		f, err := svc.RecordFinding(context.Background(), session, string(model.AgentProsecutor), q)
		require.NoError(t, err)
		require.NotNil(t, f)
		clock.t = clock.t.Add(time.Second)
	}

	list, err := svc.ListBySession(context.Background(), session.ID)
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, "query1", list[0].Query)
	require.Equal(t, "query2", list[1].Query)
	require.Equal(t, "query3", list[2].Query)

	// 顺序按时间升序
	require.True(t, list[0].CreatedAt.Before(list[1].CreatedAt))
	require.True(t, list[1].CreatedAt.Before(list[2].CreatedAt))
}

func TestListBySession_EmptyWhenNoFindings(t *testing.T) {
	svc, _, _, _ := buildService(t, nil)
	list, err := svc.ListBySession(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Len(t, list, 0)
}

func TestListBySession_FiltersBySession(t *testing.T) {
	svc, _, _, _ := buildService(t, []search.Result{{Title: "t", URL: "u", Content: "c"}})
	sessionA := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-a"}
	sessionB := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-b"}

	_, err := svc.RecordFinding(context.Background(), sessionA, string(model.AgentProsecutor), "a-q")
	require.NoError(t, err)
	_, err = svc.RecordFinding(context.Background(), sessionB, string(model.AgentDefender), "b-q")
	require.NoError(t, err)

	listA, err := svc.ListBySession(context.Background(), sessionA.ID)
	require.NoError(t, err)
	require.Len(t, listA, 1)
	require.Equal(t, "a-q", listA[0].Query)
}

func TestSummary_TruncatesLongResults(t *testing.T) {
	// 当搜索结果很多时，summary 应只取前 N 条拼接而不爆炸
	results := make([]search.Result, 20)
	for i := range results {
		results[i] = search.Result{
			Title:   fmt.Sprintf("title-%d", i),
			URL:     fmt.Sprintf("https://example.com/%d", i),
			Content: fmt.Sprintf("content-%d", i),
		}
	}
	svc, _, _, _ := buildService(t, results)
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-many"}

	f, err := svc.RecordFinding(context.Background(), session, string(model.AgentProsecutor), "many")
	require.NoError(t, err)
	require.Equal(t, 20, f.ResultCount)
	require.LessOrEqual(t, len(f.Summary), 1024, "summary 必须有上限")
	require.Contains(t, f.Summary, "title-0")
}

// fakeClock 让 ListBySession 顺序测试用固定时钟。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// findByType 辅助函数：从 A2A 消息列表里找出指定 type 的那一条。
func findByType(rows []model.A2AMessage, mt a2a.MessageType) *model.A2AMessage {
	for i := range rows {
		if rows[i].MessageType == string(mt) {
			return &rows[i]
		}
	}
	return nil
}
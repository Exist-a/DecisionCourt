package courtroom

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// stubSearcher / spyEvidenceSvc / nopLLM / buildDispatchService / findA2AByType
// 已迁移到 fakes_test.go 复用，本文件只保留业务断言。

func TestDispatchInvestigator_RejectsInvalidDispatcher(t *testing.T) {
	svc, _, _, _, _ := buildDispatchService(t, nil)
	session := model.CourtSession{ID: uuid.New()}

	_, _, err := svc.DispatchInvestigator(context.Background(), session, "judge", "行业增长率")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dispatcher must be")
}

func TestDispatchInvestigator_RejectsEmptyQuery(t *testing.T) {
	svc, _, _, _, _ := buildDispatchService(t, nil)
	session := model.CourtSession{ID: uuid.New()}

	_, _, err := svc.DispatchInvestigator(context.Background(), session, "prosecutor", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "query is required")
}

// TestDispatchInvestigator_DoesNotCreateEvidence 是这次重构的核心断言：
// 调用调查员搜索时，绝对不能在 evidences 表里写入新记录 —— 那是用户提交的概念。
func TestDispatchInvestigator_DoesNotCreateEvidence(t *testing.T) {
	results := []search.Result{
		{Title: "A", URL: "u", Content: "x"},
		{Title: "B", URL: "v", Content: "y"},
	}
	svc, evSpy, _, invRepo, _ := buildDispatchService(t, results)
	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-no-ev"}

	finding, _, err := svc.DispatchInvestigator(context.Background(), session, "prosecutor", "test")
	require.NoError(t, err)

	// 0. evidence 表不应有任何写入
	require.Equal(t, 0, len(evSpy.Creates()),
		"DispatchInvestigator 不应再写入 evidence；调查结果归到 investigation_findings")

	// 1. 应该有一条 finding
	findings, err := invRepo.ListBySession(context.Background(), session.ID)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, "test", findings[0].Query)
	require.Equal(t, 2, findings[0].ResultCount)
	require.NotEmpty(t, findings[0].FindingUUID)
	require.Equal(t, finding.FindingUUID, findings[0].FindingUUID)
}

// TestDispatchInvestigator_BroadcastsPublicDispatchAndReport 验证：dispatch
// + report 两条 A2A 消息都是 public visibility（类比正常庭审记录），双方
// 律师都能看到调查员的请求与回报。
func TestDispatchInvestigator_BroadcastsPublicDispatchAndReport(t *testing.T) {
	svc, _, a2aRepo, _, _ := buildDispatchService(t, []search.Result{{Title: "A", URL: "u", Content: "x"}})
	ctx := context.Background()
	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-public",
		CurrentPhase: model.PhaseCrossExam,
		CurrentRound: 1,
	}

	_, _, err := svc.DispatchInvestigator(ctx, session, "prosecutor", "行业增长率")
	require.NoError(t, err)

	// Orchestrator 应看到 2 条 A2A 消息：dispatch + report
	rows, err := a2aRepo.ListVisibleTo(ctx, session.ID, a2a.AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// dispatch: prosecutor → investigator, public
	dispatchRow := findA2AByType(rows, a2a.MessageTypeDispatch)
	require.NotNil(t, dispatchRow)
	require.Equal(t, "prosecutor", dispatchRow.FromAgent)
	require.Equal(t, string(model.AgentInvestigator), dispatchRow.ToAgent)
	require.Equal(t, string(a2a.VisibilityPublic), dispatchRow.Visibility,
		"dispatch 必须是 public — 庭审记录里律师问了什么是对手也能看到的")
	dispatchPayload, err := a2a.DecodePayload(*dispatchRow)
	require.NoError(t, err)
	require.Equal(t, "行业增长率", dispatchPayload["query"])

	// report: investigator → prosecutor, public, 含 finding_id + result_count
	reportRow := findA2AByType(rows, a2a.MessageTypeReport)
	require.NotNil(t, reportRow)
	require.Equal(t, string(model.AgentInvestigator), reportRow.FromAgent)
	require.Equal(t, "prosecutor", reportRow.ToAgent)
	require.Equal(t, string(a2a.VisibilityPublic), reportRow.Visibility,
		"report 必须是 public — 调查员的回报同样进入庭审记录")
	reportPayload, err := a2a.DecodePayload(*reportRow)
	require.NoError(t, err)
	require.Equal(t, "行业增长率", reportPayload["query"])
	require.NotEmpty(t, reportPayload["finding_id"], "report payload 必须带 finding_id")
	require.Equal(t, float64(1), reportPayload["result_count"])

	// 对手（defender）应能看到这两条 public 消息
	defRows, err := a2aRepo.ListVisibleTo(ctx, session.ID, string(model.AgentDefender))
	require.NoError(t, err)
	require.Len(t, defRows, 2, "defender 应该能看到 public 的 dispatch + report")

	// Clerk 也能看到（书记员需要根据庭审记录撰写判决书）
	clerkRows, err := a2aRepo.ListVisibleTo(ctx, session.ID, string(model.AgentClerk))
	require.NoError(t, err)
	require.Len(t, clerkRows, 2, "clerk 应该能看到 public 的 dispatch + report")
}

// TestDispatchInvestigator_ReturnsFindingAndSummary 验证：返回值包含 Finding
// 对象（finding_id 非空）和摘要字符串，能直接喂给 investigator_search tool。
func TestDispatchInvestigator_ReturnsFindingAndSummary(t *testing.T) {
	results := []search.Result{{Title: "A", URL: "u", Content: "x"}}
	svc, _, _, _, _ := buildDispatchService(t, results)
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "sess-return",
	}
	finding, summary, err := svc.DispatchInvestigator(context.Background(), session, "prosecutor", "test")
	require.NoError(t, err)
	require.NotNil(t, finding)
	require.NotEmpty(t, finding.FindingUUID)
	require.Equal(t, "test", finding.Query)
	require.Equal(t, 1, finding.ResultCount)
	require.NotEmpty(t, summary, "summary 必须非空以便 tool 渲染 Observation")
}

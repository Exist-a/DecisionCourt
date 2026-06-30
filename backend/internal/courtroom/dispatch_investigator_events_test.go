package courtroom

import (
	"context"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// indexOfEvent / containsEventType / findEventByType / failingSearcher / errFake
// 已迁移到 fakes_test.go 复用，本文件只保留业务断言。

// TestDispatchInvestigator_BroadcastsSearchStartedAndCompleted 是这次 UX
// 修复的核心测试：前端需要靠 search.started + search.completed 配对事件
// 来给 Investigator 头像加 spinner / 清除 spinner。这两个事件必须：
//
//  1. 都被发出（不能漏一个）
//  2. 顺序正确（started 在 completed 之前）
//  3. dispatcher / query 字段一致（前端用来匹配）
//  4. search.completed 带 finding_id（前端用来升级 dispatch 行）
func TestDispatchInvestigator_BroadcastsSearchStartedAndCompleted(t *testing.T) {
	results := []search.Result{
		{Title: "X", URL: "u", Content: "x"},
	}
	svc, _, _, _, _ := buildDispatchService(t, results)

	// 替换 broadcaster，记录所有事件
	var mu sync.Mutex
	var events []Event
	svc.broadcaster = func(_ string, ev Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}

	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "sess-search-events",
	}

	finding, _, err := svc.DispatchInvestigator(context.Background(), session, "prosecutor", "行业增长率")
	require.NoError(t, err)

	// 找到 started / completed 两个事件
	mu.Lock()
	defer mu.Unlock()
	var started, completed *Event
	for i := range events {
		switch events[i].Type {
		case "search.started":
			started = &events[i]
		case "search.completed":
			completed = &events[i]
		}
	}

	require.NotNil(t, started, "search.started 必须被发出")
	require.NotNil(t, completed, "search.completed 必须被发出")

	// 顺序：started 在 completed 之前
	startedIdx := indexOfEvent(events, "search.started")
	completedIdx := indexOfEvent(events, "search.completed")
	require.Less(t, startedIdx, completedIdx, "search.started 必须在 search.completed 之前")

	// started 的字段
	require.Equal(t, "investigator", started.Payload["agent_id"])
	require.Equal(t, "prosecutor", started.Payload["dispatcher"])
	require.Equal(t, "行业增长率", started.Payload["query"])

	// completed 的字段
	require.Equal(t, "investigator", completed.Payload["agent_id"])
	require.Equal(t, "prosecutor", completed.Payload["dispatcher"])
	require.Equal(t, "行业增长率", completed.Payload["query"])
	// 注意：payload 在 broadcast 时是 Go 原生 map（未经 JSON 序列化），
	// 所以 result_count 是 int 不是 float64；前端拿到的是 JSON 才是 float64。
	require.Equal(t, 1, completed.Payload["result_count"])
	require.Equal(t, finding.FindingUUID, completed.Payload["finding_id"],
		"search.completed 必须带 finding_id 以便前端升级 dispatch 行")
}

// TestDispatchInvestigator_SearchStartedBeforeSearchCompletedEvenWhenSearcherErrors
// 验证：即使 searcher 抛错，search.completed 仍然要发出（带 ok=false 或
// error_message），让前端能可靠地关闭 spinner。这是把 spinner 状态机
// 推进到终态的关键 —— 不然 dispatch 行的 spinner 会永远转。
func TestDispatchInvestigator_SearchStartedBeforeSearchCompletedEvenWhenSearcherErrors(t *testing.T) {
	// 重建 service，searcher 总是返回 error
	svc, _, _, _, _ := buildDispatchService(t, nil)

	// investigation service 持有自己的 searcher；要让它失败得换它
	// 自己的搜索器，而不是 svc.searcher（后者用于旧 dispatch 路径）。
	failer := &failingSearcher{err: errFake("upstream timeout")}
	svc.investigationSvc = investigation.NewService(
		investigation.NewInMemoryRepository(nil),
		svc.a2aBus,
		failer,
	)

	var mu sync.Mutex
	var events []Event
	svc.broadcaster = func(_ string, ev Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}

	session := model.CourtSession{ID: uuid.New(), SessionUUID: "sess-fail"}

	_, _, err := svc.DispatchInvestigator(context.Background(), session, "defender", "fail")
	require.Error(t, err, "dispatcher 应当把 searcher 错误传播出去")

	mu.Lock()
	defer mu.Unlock()
	// 即使失败，search.started 仍然要先发出（让前端立即显示 spinner）
	require.True(t, containsEventType(events, "search.started"),
		"即使 searcher 失败也要先发出 search.started，让前端立刻 spinner")
	// search.completed 也应该发出，让前端能关闭 spinner
	// （如果当前实现没有发出 completed，则这个测试会失败 ——
	//  它正好揭示了 spinner 永远转的 bug。）
	completed := findEventByType(events, "search.completed")
	if completed == nil {
		t.Fatalf("bug: search.completed 没有发出 —— 这就是为什么 dispatch 行的 spinner 永远转")
	}
}

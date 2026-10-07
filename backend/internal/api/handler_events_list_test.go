package api

// v2.11 (deferred D12): GET /api/v1/courtrooms/:session_uuid/events
//
// 覆盖 deferred D12 的三条验收：归属校验、分页边界、前缀过滤。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEventLister 是内存 fake，按 prefix + offset/limit 复刻 GORM 实现的语义，
// 让 handler 测试不需要 Postgres。
type fakeEventLister struct {
	rows     []model.DecisionEvent
	err      error
	lastQ    DecisionEventQuery
	called   int
	lastSess string
}

func (f *fakeEventLister) ListDecisionEvents(_ context.Context, sessionUUID string, q DecisionEventQuery) ([]model.DecisionEvent, error) {
	f.called++
	f.lastSess = sessionUUID
	f.lastQ = q
	if f.err != nil {
		return nil, f.err
	}
	matched := make([]model.DecisionEvent, 0, len(f.rows))
	for _, r := range f.rows {
		if q.EventTypePrefix != "" && !strings.HasPrefix(r.EventType, q.EventTypePrefix) {
			continue
		}
		matched = append(matched, r)
	}
	if q.Offset >= len(matched) {
		return nil, nil
	}
	matched = matched[q.Offset:]
	if q.Limit > 0 && len(matched) > q.Limit {
		matched = matched[:q.Limit]
	}
	return matched, nil
}

// eventsReadHandler 构造带 fake lister 的 Handler，session owner = test-user。
func eventsReadHandler(lister DecisionEventLister, owner string) *Handler {
	return &Handler{
		eventLister: lister,
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			return model.CourtSession{
				ID:          uuid.New(),
				SessionUUID: "sess-events",
				OwnerID:     owner,
			}, true
		},
	}
}

func makeEvent(eventType, agent string, t time.Time) model.DecisionEvent {
	return model.DecisionEvent{
		ID:          uuid.New(),
		SessionUUID: "sess-events",
		RequestID:   "req-" + eventType,
		EventType:   eventType,
		AgentType:   agent,
		Payload:     `{"phase":"opening"}`,
		DurationMs:  12,
		Status:      "ok",
		CreatedAt:   t,
	}
}

type eventsResp struct {
	Code int `json:"code"`
	Data struct {
		Events  []map[string]interface{} `json:"events"`
		Count   int                      `json:"count"`
		Limit   int                      `json:"limit"`
		Offset  int                      `json:"offset"`
		HasMore bool                     `json:"has_more"`
	} `json:"data"`
}

func getEvents(t *testing.T, h *Handler, sessionUUID, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := ginEngine(h)
	url := "/api/v1/courtrooms/" + sessionUUID + "/events"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestListDecisionEvents_Success 基本路径：返回事件 + payload 解析成对象。
func TestListDecisionEvents_Success(t *testing.T) {
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	lister := &fakeEventLister{rows: []model.DecisionEvent{
		makeEvent("fe.trial_started", "", base),
		makeEvent("span.RunCrossExamRound", "prosecutor", base.Add(time.Minute)),
	}}
	h := eventsReadHandler(lister, "test-user")

	resp := getEvents(t, h, "sess-events", "")
	require.Equal(t, http.StatusOK, resp.Code)

	var body eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Code)
	require.Equal(t, 2, body.Data.Count)
	assert.False(t, body.Data.HasMore)
	assert.Equal(t, defaultEventPageSize, body.Data.Limit)
	assert.Equal(t, 0, body.Data.Offset)

	first := body.Data.Events[0]
	assert.Equal(t, "fe.trial_started", first["event_type"])
	assert.Equal(t, "sess-events", first["session_uuid"])
	assert.Equal(t, "req-fe.trial_started", first["request_id"])
	// payload 必须是解析后的对象，不是 JSON 字符串。
	payload, ok := first["payload"].(map[string]interface{})
	require.True(t, ok, "payload 必须是对象, got %T", first["payload"])
	assert.Equal(t, "opening", payload["phase"])
}

// TestListDecisionEvents_RejectsNonOwner 归属校验：非 owner 必须 403 且不查库。
func TestListDecisionEvents_RejectsNonOwner(t *testing.T) {
	lister := &fakeEventLister{rows: []model.DecisionEvent{makeEvent("fe.x", "", time.Now())}}
	h := eventsReadHandler(lister, "other-user")

	resp := getEvents(t, h, "sess-events", "")
	require.Equal(t, http.StatusForbidden, resp.Code)
	assert.Equal(t, 0, lister.called, "越权请求不得触达数据源")
}

// TestListDecisionEvents_SessionNotFound 会话不存在 → 404。
func TestListDecisionEvents_SessionNotFound(t *testing.T) {
	lister := &fakeEventLister{}
	h := &Handler{
		eventLister:   lister,
		sessionLookup: func(_ string) (model.CourtSession, bool) { return model.CourtSession{}, false },
	}
	resp := getEvents(t, h, "missing", "")
	require.Equal(t, http.StatusNotFound, resp.Code)
	assert.Equal(t, 0, lister.called)
}

// TestListDecisionEvents_PrefixFilter 前缀过滤：只返回匹配前缀的事件，
// 且前缀原样传给数据源（handler 不自己做过滤）。
func TestListDecisionEvents_PrefixFilter(t *testing.T) {
	lister := &fakeEventLister{rows: []model.DecisionEvent{
		makeEvent("fe.trial_started", "", time.Now()),
		makeEvent("span.GenerateVerdict", "judge", time.Now()),
		makeEvent("state_transition", "", time.Now()),
	}}
	h := eventsReadHandler(lister, "test-user")

	resp := getEvents(t, h, "sess-events", "event_type_prefix=fe.")
	require.Equal(t, http.StatusOK, resp.Code)

	var body eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Equal(t, 1, body.Data.Count)
	assert.Equal(t, "fe.trial_started", body.Data.Events[0]["event_type"])
	assert.Equal(t, "fe.", lister.lastQ.EventTypePrefix, "前缀必须原样下推到数据源")
}

// TestListDecisionEvents_PrefixTooLong 前缀超过 varchar(50) → 400（前置拦截）。
func TestListDecisionEvents_PrefixTooLong(t *testing.T) {
	lister := &fakeEventLister{}
	h := eventsReadHandler(lister, "test-user")

	resp := getEvents(t, h, "sess-events", "event_type_prefix="+strings.Repeat("a", maxEventTypePrefixLen+1))
	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Equal(t, 0, lister.called)
}

// TestListDecisionEvents_PaginationHasMore 分页：多取 1 行判断 has_more，
// 且返回条数严格等于 limit。
func TestListDecisionEvents_PaginationHasMore(t *testing.T) {
	rows := make([]model.DecisionEvent, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, makeEvent("fe.e"+string(rune('a'+i)), "", time.Now()))
	}
	lister := &fakeEventLister{rows: rows}
	h := eventsReadHandler(lister, "test-user")

	// limit=2 → 第一页 2 条 + has_more=true；数据源应被要求取 3 行。
	resp := getEvents(t, h, "sess-events", "limit=2")
	require.Equal(t, http.StatusOK, resp.Code)
	var page1 eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))
	require.Equal(t, 2, page1.Data.Count)
	assert.True(t, page1.Data.HasMore)
	assert.Equal(t, 3, lister.lastQ.Limit, "为判断 has_more 必须多取 1 行")

	// 最后一页 offset=4 → 1 条 + has_more=false。
	resp = getEvents(t, h, "sess-events", "limit=2&offset=4")
	require.Equal(t, http.StatusOK, resp.Code)
	var page3 eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
	require.Equal(t, 1, page3.Data.Count)
	assert.False(t, page3.Data.HasMore)
	assert.Equal(t, 4, page3.Data.Offset)

	// offset 超出行数 → 空数组 + count=0，但不是错误。
	resp = getEvents(t, h, "sess-events", "limit=2&offset=99")
	require.Equal(t, http.StatusOK, resp.Code)
	var empty eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &empty))
	assert.Equal(t, 0, empty.Data.Count)
	assert.False(t, empty.Data.HasMore)
}

// TestListDecisionEvents_InvalidPaginationParams 分页边界：非法 limit/offset → 400。
func TestListDecisionEvents_InvalidPaginationParams(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"limit=0", "limit=0"},
		{"limit 超上限", "limit=501"},
		{"limit 非整数", "limit=abc"},
		{"limit 负数", "limit=-1"},
		{"offset 负数", "offset=-1"},
		{"offset 非整数", "offset=x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lister := &fakeEventLister{}
			h := eventsReadHandler(lister, "test-user")
			resp := getEvents(t, h, "sess-events", c.query)
			require.Equal(t, http.StatusBadRequest, resp.Code, "query=%q", c.query)
			assert.Equal(t, 0, lister.called, "非法参数不得触达数据源")
		})
	}
}

// TestListDecisionEvents_MaxLimitAccepted 边界内最大值必须被接受。
func TestListDecisionEvents_MaxLimitAccepted(t *testing.T) {
	lister := &fakeEventLister{}
	h := eventsReadHandler(lister, "test-user")
	resp := getEvents(t, h, "sess-events", "limit=500")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, 500, lister.lastQ.Limit-1, "数据源收到的 limit 应为 500(+1 探测行)")
}

// TestListDecisionEvents_NilListerDegrades 未注入数据源 → 200 空列表（不 500）。
func TestListDecisionEvents_NilListerDegrades(t *testing.T) {
	h := &Handler{
		eventLister: nil,
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			return model.CourtSession{SessionUUID: "sess-events", OwnerID: "test-user"}, true
		},
	}
	resp := getEvents(t, h, "sess-events", "")
	require.Equal(t, http.StatusOK, resp.Code)
	var body eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, 0, body.Data.Count)
}

// TestListDecisionEvents_ListerErrorReturns500 数据源错误 → 500（不吞）。
func TestListDecisionEvents_ListerErrorReturns500(t *testing.T) {
	lister := &fakeEventLister{err: assert.AnError}
	h := eventsReadHandler(lister, "test-user")
	resp := getEvents(t, h, "sess-events", "")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
}

// TestListDecisionEvents_BadPayloadDegrades 坏 payload（非 JSON）→ 空对象，不 500。
func TestListDecisionEvents_BadPayloadDegrades(t *testing.T) {
	bad := makeEvent("fe.x", "", time.Now())
	bad.Payload = "{not json"
	lister := &fakeEventLister{rows: []model.DecisionEvent{bad}}
	h := eventsReadHandler(lister, "test-user")

	resp := getEvents(t, h, "sess-events", "")
	require.Equal(t, http.StatusOK, resp.Code)
	var body eventsResp
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Equal(t, 1, body.Data.Count)
	// 降级为空对象而非 null / 字符串。
	payload, ok := body.Data.Events[0]["payload"].(map[string]interface{})
	require.True(t, ok, "坏 payload 应降级为空对象, got %T", body.Data.Events[0]["payload"])
	assert.Empty(t, payload)
}

// TestEscapeLikePrefix 钉住 LIKE 通配符转义：前缀里的 % / _ 必须字面匹配。
func TestEscapeLikePrefix(t *testing.T) {
	assert.Equal(t, `fe.`, escapeLikePrefix("fe."))
	assert.Equal(t, `100\%`, escapeLikePrefix("100%"))
	assert.Equal(t, `a\_b`, escapeLikePrefix("a_b"))
	assert.Equal(t, `c\\d`, escapeLikePrefix(`c\d`))
}

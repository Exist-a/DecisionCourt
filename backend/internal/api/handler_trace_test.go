package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/trace"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeTraceStore 是 trace.Store 接口的 fake,handler_trace_test.go 专用。
//
// 设计要点:
//   - sessions map[sessionID]→SessionTrace + traces map[traceID]→Trace 模拟存储
//   - 行为可预测,handler 测试只关心"调了 store 拿到结果"是否正确
//   - ListBySession / GetTraceByID 返回 fake 内部数据,不读文件
type fakeTraceStore struct {
	sessions map[string]*trace.SessionTrace
}

func newFakeTraceStore() *fakeTraceStore {
	return &fakeTraceStore{sessions: make(map[string]*trace.SessionTrace)}
}

func (f *fakeTraceStore) putSession(sessionID string, st *trace.SessionTrace) {
	f.sessions[sessionID] = st
}

func (f *fakeTraceStore) ListBySession(sessionID, _ string) ([]*trace.Trace, error) {
	st, ok := f.sessions[sessionID]
	if !ok {
		return []*trace.Trace{}, nil
	}
	return trace.Aggregate(st, nil), nil
}

func (f *fakeTraceStore) GetTraceByID(sessionID, traceID string) (*trace.Trace, error) {
	st, ok := f.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	traces := trace.Aggregate(st, []string{traceID})
	if len(traces) == 0 {
		return nil, nil
	}
	return traces[0], nil
}

// T1: GET /traces 返某 session 的所有 traces。
func TestListTraces_ReturnsSessionTraces(t *testing.T) {
	session := makeOwnedSession()
	fake := newFakeTraceStore()
	fake.putSession(session.SessionUUID, &trace.SessionTrace{
		SessionID: session.SessionUUID,
		Traces: map[string][]trace.Run{
			"trace-1": {makeTestRun("trace-1", session.SessionUUID, 0)},
			"trace-2": {makeTestRun("trace-2", session.SessionUUID, 0)},
		},
	})

	h := &Handler{
		traceStore: fake,
		sessionLookup: func(_ string) (model.CourtSession, bool) { return session, true },
	}
	r := ginEngine(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/traces", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Traces []*trace.Trace `json:"traces"`
			Count  int            `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 2, resp.Data.Count)
	require.Len(t, resp.Data.Traces, 2)
}

// T2: GET /traces/:trace_id 返单 trace (含 tree)。
func TestGetTrace_ReturnsSingleTraceWithTree(t *testing.T) {
	session := makeOwnedSession()
	fake := newFakeTraceStore()
	fake.putSession(session.SessionUUID, &trace.SessionTrace{
		SessionID: session.SessionUUID,
		Traces: map[string][]trace.Run{
			"trace-tree": {
				makeTestRun("trace-tree", session.SessionUUID, 0),
				makeTestRun("trace-tree", session.SessionUUID, 1),
			},
		},
	})

	h := &Handler{
		traceStore: fake,
		sessionLookup: func(_ string) (model.CourtSession, bool) { return session, true },
	}
	r := ginEngine(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/traces/trace-tree", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int         `json:"code"`
		Data *trace.Trace `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotNil(t, resp.Data)
	require.Equal(t, "trace-tree", resp.Data.TraceID)
	require.Len(t, resp.Data.Runs, 2)
	require.Equal(t, 0, resp.Data.Tree.Run.RetryCount)
	require.Len(t, resp.Data.Tree.Children, 1, "tree 应有 1 child (RC=1)")
}

// T3: trace 不存在 → 200 + data:null (前端按无数据渲染,不返 404)。
func TestGetTrace_NotFound(t *testing.T) {
	session := makeOwnedSession()
	fake := newFakeTraceStore()
	// 不 putSession,store 空

	h := &Handler{
		traceStore: fake,
		sessionLookup: func(_ string) (model.CourtSession, bool) { return session, true },
	}
	r := ginEngine(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/traces/nonexistent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "trace 不存在 200 + data:null")
	var resp struct {
		Code  int         `json:"code"`
		Data  *trace.Trace `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Nil(t, resp.Data)
}

// T4: date 参数格式错 → 400。
func TestListTraces_InvalidDate(t *testing.T) {
	session := makeOwnedSession()
	h := &Handler{
		traceStore: newFakeTraceStore(),
		sessionLookup: func(_ string) (model.CourtSession, bool) { return session, true },
	}
	r := ginEngine(h)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/courtrooms/"+session.SessionUUID+"/traces?date=not-a-date", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// T5: 不是 session owner → 403 (复用 checkSessionAccess)。
func TestListTraces_Forbidden(t *testing.T) {
	session := makeOwnedSession()
	session.OwnerID = "other-user" // owner 不是 test-user

	h := &Handler{
		traceStore: newFakeTraceStore(),
		sessionLookup: func(_ string) (model.CourtSession, bool) { return session, true },
	}
	r := ginEngine(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/traces", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

// makeOwnedSession 构造 test-user 拥有的 CourtSession。
//
// 复用了 handler_investigations_test.go 的 makeSession 模式,但 UUID 写死便于断言。
func makeOwnedSession() model.CourtSession {
	return model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "sess-trace-" + uuid.New().String()[:8],
		OwnerID:     "test-user",
		Title:       "Trace 测试庭审",
		OptionA:     "A",
		OptionB:     "B",
	}
}

// makeTestRun 构造 trace 测试用 Run (避免和 parser_test.go 名字冲突)。
func makeTestRun(traceID, sessionID string, retryCount int) trace.Run {
	return trace.Run{
		RunID:      traceID + "-" + itoa(retryCount),
		TraceID:    traceID,
		SessionID:  sessionID,
		AgentType:  "prosecutor",
		TaskType:   "react_speak",
		StartedAt:  timeNow,
		EndedAt:    timeNow,
		LatencyMs:  1000,
		Status:     "ok",
		RetryCount: retryCount,
	}
}

// intToStr 是 strconv.Itoa 的内联别名,handler_trace_test.go 用。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// timeNow 是固定时间,便于断言 StartedAt/EndedAt (不依赖 time.Now())。
var timeNow = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

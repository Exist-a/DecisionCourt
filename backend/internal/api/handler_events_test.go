package api

// v0.10 frontend analytics: TDD tests for POST /api/v1/courtrooms/:session_uuid/events
//
// 设计要点（见 ADR 0020）：
//   - 复用 observability.EventRecorder 接口，不另起一套埋点基础设施
//   - 鉴权复用 checkSessionAccess（必须 owner 才能上报）
//   - 埋点失败（recorder nil / Record 返错）→ 仍然返 200，不阻塞前端
//   - EventType 长度受 DB schema varchar(50) 约束
//
// ADR 0020 决策 #3：前端事件类型用 fe.<name> 前缀，和后端 span.X 区分。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeEventRecorder 是测试用的 EventRecorder，记录所有事件 + 可注入错误。
type fakeEventRecorder struct {
	mu     sync.Mutex
	events []observability.DecisionEventRecord
	failOn error // 非 nil 时 Record 返这个错
}

func (r *fakeEventRecorder) Record(_ context.Context, ev observability.DecisionEventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failOn != nil {
		return r.failOn
	}
	r.events = append(r.events, ev)
	return nil
}

func (r *fakeEventRecorder) all() []observability.DecisionEventRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]observability.DecisionEventRecord, len(r.events))
	copy(out, r.events)
	return out
}

// eventsHandler 构造带 fakeRecorder 的 Handler；sessionLookup 默认命中 + OwnerID=test-user。
func eventsHandler(rec observability.EventRecorder) *Handler {
	return &Handler{
		eventRecorder: rec,
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			return model.CourtSession{
				ID:          uuid.New(),
				SessionUUID: "sess-events",
				OwnerID:     "test-user",
			}, true
		},
	}
}

// postEvent 辅助函数：POST 到 /api/v1/courtrooms/:session_uuid/events
func postEvent(t *testing.T, h *Handler, sessionUUID string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	r := ginEngine(h)
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/courtrooms/"+sessionUUID+"/events", reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestPostEvent_Success 验证：合法事件 → 200 + recorder 收到完整字段
func TestPostEvent_Success(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := eventsHandler(rec)

	body := map[string]interface{}{
		"event_type": "fe.trial_started",
		"payload": map[string]interface{}{
			"phase": "opening",
			"foo":   "bar",
		},
		"duration_ms": 123,
		"status":      "ok",
	}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusOK, resp.Code, "successful event must return 200")

	var respBody struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &respBody))
	require.Equal(t, 0, respBody.Code)

	events := rec.all()
	require.Len(t, events, 1)
	require.Equal(t, "sess-events", events[0].SessionUUID)
	require.Equal(t, "fe.trial_started", events[0].EventType)
	require.Equal(t, int64(123), events[0].DurationMs)
	require.Equal(t, "ok", events[0].Status)
	require.Equal(t, "bar", events[0].Payload["foo"])
	require.Equal(t, "opening", events[0].Payload["phase"])
}

// TestPostEvent_DefaultStatus 验证：status 缺省时填 "ok"（DB 期望非空）
func TestPostEvent_DefaultStatus(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := eventsHandler(rec)

	body := map[string]interface{}{
		"event_type": "fe.tab_switched",
	}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusOK, resp.Code)
	events := rec.all()
	require.Len(t, events, 1)
	require.Equal(t, "ok", events[0].Status, "missing status must default to 'ok'")
}

// TestPostEvent_SessionNotFound 验证：session 不存在 → 404（不是 200，避免泄漏）
func TestPostEvent_SessionNotFound(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := &Handler{
		eventRecorder: rec,
		sessionLookup: func(_ string) (model.CourtSession, bool) { return model.CourtSession{}, false },
	}
	body := map[string]interface{}{"event_type": "fe.trial_started"}
	resp := postEvent(t, h, "missing", body)

	require.Equal(t, http.StatusNotFound, resp.Code)
	require.Empty(t, rec.all(), "missing session must not write any event")
}

// TestPostEvent_Forbidden 验证：不是 owner → 403（防他人往我 session 灌垃圾事件）
func TestPostEvent_Forbidden(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := &Handler{
		eventRecorder: rec,
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			// OwnerID 不是 test-user → ginEngine 注入的 viewer 不匹配
			return model.CourtSession{SessionUUID: "sess-events", OwnerID: "other-user"}, true
		},
	}
	body := map[string]interface{}{"event_type": "fe.trial_started"}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusForbidden, resp.Code)
	require.Empty(t, rec.all(), "non-owner must not write any event")
}

// TestPostEvent_MissingEventType 验证：event_type 缺失 → 400
func TestPostEvent_MissingEventType(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := eventsHandler(rec)
	body := map[string]interface{}{"payload": map[string]interface{}{"foo": "bar"}}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Empty(t, rec.all(), "missing event_type must not write any event")
}

// TestPostEvent_OversizedEventType 验证：event_type > 50 字符 → 400（DB varchar(50) 约束）
func TestPostEvent_OversizedEventType(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := eventsHandler(rec)
	body := map[string]interface{}{
		"event_type": "fe." + strings.Repeat("x", 60),
	}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusBadRequest, resp.Code,
		"event_type must be capped at DB schema length (50 chars)")
	require.Empty(t, rec.all())
}

// TestPostEvent_RecorderNil 验证：recorder 未注入 → 仍然 200（降级，不阻塞前端）
func TestPostEvent_RecorderNil(t *testing.T) {
	h := &Handler{
		eventRecorder: nil, // 关键：模拟生产环境 recorder 注入失败的场景
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			return model.CourtSession{SessionUUID: "sess-events", OwnerID: "test-user"}, true
		},
	}
	body := map[string]interface{}{"event_type": "fe.trial_started"}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusOK, resp.Code,
		"recorder nil must not break the API — analytics is best-effort")
}

// TestPostEvent_RecorderFails 验证：recorder.Record 返错 → 仍然 200（埋点失败不阻塞前端）
func TestPostEvent_RecorderFails(t *testing.T) {
	rec := &fakeEventRecorder{failOn: errors.New("simulated DB down")}
	h := eventsHandler(rec)

	body := map[string]interface{}{"event_type": "fe.trial_started"}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusOK, resp.Code,
		"recorder failure must not propagate to frontend — analytics is best-effort")
}

// TestPostEvent_BadJSON 验证：非法 JSON → 400（gin 自动返）
func TestPostEvent_BadJSON(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := eventsHandler(rec)

	r := ginEngine(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/courtrooms/sess-events/events",
		strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Empty(t, rec.all())
}

// TestPostEvent_ErrorEventFields 验证：status=error 时 error_msg 被正确传递
func TestPostEvent_ErrorEventFields(t *testing.T) {
	rec := &fakeEventRecorder{}
	h := eventsHandler(rec)

	body := map[string]interface{}{
		"event_type": "fe.ws_reconnect",
		"status":     "error",
		"error_msg":  "TCP half-open",
		"payload":    map[string]interface{}{"attempt": 3, "delay_ms": 8000},
	}
	resp := postEvent(t, h, "sess-events", body)

	require.Equal(t, http.StatusOK, resp.Code)
	events := rec.all()
	require.Len(t, events, 1)
	require.Equal(t, "error", events[0].Status)
	require.Equal(t, "TCP half-open", events[0].ErrorMsg)
	// payload 中的数值字段：Go encoding/json 解码 map[string]interface{} 时
	// int 默认变 float64。这是标准库行为,handler 不强制类型转换(保留
	// 前端原始数值语义,后续查询方按需 .(float64))。
	require.Equal(t, float64(3), events[0].Payload["attempt"])
}
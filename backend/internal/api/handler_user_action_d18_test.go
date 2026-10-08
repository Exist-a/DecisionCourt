package api

// v2.11 (deferred D18b) handler 层护栏：UserAction 必须让"同步可判定的失败"可见。
//
// 历史行为：handler 把 ProcessUserAction 丢进 detached goroutine 后**立刻**返回
// HTTP 200 + code 0，失败只进 slog。于是状态机拒绝（阶段不允许该 action）也报成功
// —— 前端按 code===0 判定成功、还会 router.push，用户完全看不到错误。
//
// 这组测试用真实 courtroom.Service（sqlite in-memory）打 handler，断言：
//   - 阶段不允许 → 400 + code 1003 + 带原因 message（且不启动异步执行）
//   - 阶段允许   → 200 + code 0（长耗时执行仍异步）

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newUserActionTestHandler 用 sqlite 建一个真实 courtroom.Service，并把
// sessionLookup 指向内存里的那条 session（避免 handler 去查全局 model.DB）。
func newUserActionTestHandler(t *testing.T, phase model.CourtPhase) (*Handler, *gorm.DB, model.CourtSession) {
	t.Helper()
	db := newListTestDB(t)

	session := model.CourtSession{
		ID:           uuid.New(),
		SessionUUID:  "sess-user-action-" + uuid.New().String()[:8],
		OwnerID:      "test-user",
		Title:        "D18b user action",
		OptionA:      "A",
		OptionB:      "B",
		CurrentPhase: phase,
		CurrentRound: 1,
		Status:       model.StatusActive,
		MaxRounds:    3,
	}
	require.NoError(t, db.Create(&session).Error)

	bus := a2a.NewBus(a2a.NewInMemoryRepository(nil), nil)
	// searcher 不能为 nil（investigation.NewService 会 panic）—— 复用本包既有的 stub。
	svc := courtroom.NewService(db, nil, nil, &stubSearcherForList{}, bus, func(string, courtroom.Event) {})

	h := &Handler{
		service:       svc,
		sessionLookup: func(string) (model.CourtSession, bool) { return session, true },
	}
	return h, db, session
}

// postAction 打 POST /api/v1/courtrooms/:uuid/actions。
func postAction(t *testing.T, h *Handler, sessionUUID, action string) *httptest.ResponseRecorder {
	t.Helper()
	r := ginEngine(h)
	body, err := json.Marshal(map[string]any{"action": action})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/courtrooms/"+sessionUUID+"/actions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestUserAction_RejectedPhaseReturnsVisibleError 核心修复：阶段不允许时
// 必须 400 + code 1003，而不是 200/code 0。
func TestUserAction_RejectedPhaseReturnsVisibleError(t *testing.T) {
	// cross_exam 阶段不允许 reopen_trial（只在 verdict/appeal 合法）
	h, _, session := newUserActionTestHandler(t, model.PhaseCrossExam)

	rec := postAction(t, h, session.SessionUUID, "reopen_trial")

	require.Equal(t, http.StatusBadRequest, rec.Code,
		"状态机拒绝必须让调用方看到（此前恒返 200/code 0，用户看不到失败）")

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1003, resp.Code, "1003 = 当前阶段不允许该操作")
	assert.NotEmpty(t, resp.Message, "必须带拒绝原因，前端才能提示")
	assert.Contains(t, resp.Message, "reopen trial")
}

// TestUserAction_AcceptedPhaseReturnsOK 阶段允许 → 200/code 0（长耗时执行仍异步）。
func TestUserAction_AcceptedPhaseReturnsOK(t *testing.T) {
	// verdict 阶段允许 reopen_trial；reopenTrial 只做 DB 迁移 + 广播，不需要 LLM
	h, db, session := newUserActionTestHandler(t, model.PhaseVerdict)

	rec := postAction(t, h, session.SessionUUID, "reopen_trial")

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)

	// 异步执行会真的把阶段改回 evidence —— 等一下它落库（最多 ~1s）
	var fresh model.CourtSession
	for i := 0; i < 20; i++ {
		if err := db.Where("session_uuid = ?", session.SessionUUID).First(&fresh).Error; err == nil &&
			fresh.CurrentPhase == model.PhaseEvidence {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.Equal(t, model.PhaseEvidence, fresh.CurrentPhase,
		"200 之后异步执行应真的生效（阶段回到 evidence）")
}

// TestUserAction_NilServiceReturns503 service 未注入 → 明确 503，而不是 nil panic。
func TestUserAction_NilServiceReturns503(t *testing.T) {
	h := &Handler{
		service: nil,
		sessionLookup: func(string) (model.CourtSession, bool) {
			return model.CourtSession{SessionUUID: "s", OwnerID: "test-user"}, true
		},
	}
	rec := postAction(t, h, "s", "direct_verdict")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

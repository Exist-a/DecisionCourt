package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// stubMemoryLister 是 handler.GetVisibleMemory 的最小化依赖：只实现
// MemoryLister 接口的 ListPrivateMemory 方法。production 走
// courtroom.Service，测试用 in-memory a2a repository 替代。
type stubMemoryLister struct {
	rows []model.A2AMessage
	err  error
}

func (s *stubMemoryLister) ListPrivateMemory(_ context.Context, _ string) ([]model.A2AMessage, error) {
	return s.rows, s.err
}

// TestGetVisibleMemory_ReturnsHydrationEnvelope 验证 v0.8.3 GET /memory
// 返回的 envelope 字段（含 from/to/message_type/visibility/payload/
// created_at 等）跟 WS a2a.message 广播完全一致 —— 这是前端能复用
// applyCourtEvent parse path 的契约。
func TestGetVisibleMemory_ReturnsHydrationEnvelope(t *testing.T) {
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "mem-hydrate-" + uuid.New().String()[:8],
		Title:       "策略笔记还原测试",
		OptionA:     "A",
		OptionB:     "B",
		CurrentPhase: model.PhaseCrossExam,
	}

	payloadJSON, err := json.Marshal(map[string]interface{}{
		"content":  "对方的逻辑漏洞在 X 点",
		"stance":   "support_a",
		"linked_evidence_ids": []string{"E001"},
	})
	require.NoError(t, err)

	rows := []model.A2AMessage{
		{
			ID:          uuid.New(),
			SessionID:   session.ID,
			MessageUUID: "mem-pro-001",
			Round:       1,
			Phase:       string(model.PhaseCrossExam),
			FromAgent:   "prosecutor",
			ToAgent:     "prosecutor",
			MessageType: string(a2a.MessageTypeStrategyNote),
			Visibility:  string(a2a.VisibilityPrivate),
			Payload:     string(payloadJSON),
		},
		{
			ID:          uuid.New(),
			SessionID:   session.ID,
			MessageUUID: "mem-def-001",
			Round:       1,
			Phase:       string(model.PhaseCrossExam),
			FromAgent:   "defender",
			ToAgent:     "defender",
			MessageType: string(a2a.MessageTypeOpponentWeakness),
			Visibility:  string(a2a.VisibilityPrivate),
			Payload:     `{"content":"X 点可以反将一军"}`,
		},
	}

	h := &Handler{
		sessionLookup: func(u string) (model.CourtSession, bool) {
			if u == session.SessionUUID {
				return session, true
			}
			return model.CourtSession{}, false
		},
		memoryLister: &stubMemoryLister{rows: rows},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/memory", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var body struct {
		Code int `json:"code"`
		Data struct {
			Memory []map[string]interface{} `json:"memory"`
			Count  int                      `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, 2, body.Data.Count)
	require.Len(t, body.Data.Memory, 2)

	first := body.Data.Memory[0]
	// Envelope 字段必须用 "from"/"to" 而不是 "from_agent"/"to_agent"，
	// 否则前端 applyCourtEvent.parse 全部 miss → strategy_note 还原失败。
	require.Equal(t, "mem-pro-001", first["message_uuid"])
	require.Equal(t, "prosecutor", first["from"])
	require.Equal(t, "prosecutor", first["to"])
	require.Equal(t, "strategy_note", first["message_type"])
	require.Equal(t, "private", first["visibility"])
	require.Equal(t, float64(1), first["round"])

	// payload 必须是 decoded map（前端 typeof payload.content === "string"）
	require.IsType(t, map[string]interface{}{}, first["payload"])
	payload := first["payload"].(map[string]interface{})
	require.Equal(t, "对方的逻辑漏洞在 X 点", payload["content"])
	require.Equal(t, "support_a", payload["stance"])
}

// TestGetVisibleMemory_EmptyArrayWhenNoMemory 保证：session 存在但
// memory 为空时返回 200 + 空数组（NOT 404，NOT null），这样前端的
// "暂无策略笔记"空状态能正常显示。
func TestGetVisibleMemory_EmptyArrayWhenNoMemory(t *testing.T) {
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "mem-empty-" + uuid.New().String()[:8],
		OwnerID:     "test-user",
		Title:       "无策略笔记测试",
		OptionA:     "A",
		OptionB:     "B",
		CurrentPhase: model.PhaseCrossExam,
	}

	h := &Handler{
		sessionLookup: func(u string) (model.CourtSession, bool) {
			if u == session.SessionUUID {
				return session, true
			}
			return model.CourtSession{}, false
		},
		memoryLister: &stubMemoryLister{rows: []model.A2AMessage{}},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/memory", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	body := w.Body.String()
	require.Contains(t, body, `"memory":[]`, "应该返回空数组而非 null")
	require.Contains(t, body, `"count":0`)
}

// TestGetVisibleMemory_SessionNotFound 验证：sessionUUID 不存在时返回
// 404 code=1002（与其它 GET 端点一致），前端可以跳过水合不报错。
func TestGetVisibleMemory_SessionNotFound(t *testing.T) {
	h := &Handler{
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			return model.CourtSession{}, false
		},
		memoryLister: &stubMemoryLister{}, // 即使有 lister，session 找不到也应短路
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/does-not-exist/memory", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), `"code":1002`)
}

// TestGetVisibleMemory_ListerError 验证：底层 service 抛错时返回 500
// code=1500，前端能在水合序列里跳过这一项继续走其它端点。
func TestGetVisibleMemory_ListerError(t *testing.T) {
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "mem-err-" + uuid.New().String()[:8],
		Title:       "error path",
		OptionA:     "A",
		OptionB:     "B",
	}

	h := &Handler{
		sessionLookup: func(u string) (model.CourtSession, bool) {
			if u == session.SessionUUID {
				return session, true
			}
			return model.CourtSession{}, false
		},
		memoryLister: &stubMemoryLister{err: errors.New("database on fire")},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/memory", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), `"code":1500`)
}

// TestGetVisibleMemory_MissingLister 验证：当 Handler 没注入 memoryLister
// 时（如只配 investigation 路由的单元测试）应返回 500 而不是 panic，
// 也不应该返回 200 假装一切正常。
func TestGetVisibleMemory_MissingLister(t *testing.T) {
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "mem-nolister-" + uuid.New().String()[:8],
		OwnerID:     "test-user",
		Title:       "no lister",
		OptionA:     "A",
		OptionB:     "B",
	}

	h := &Handler{
		sessionLookup: func(u string) (model.CourtSession, bool) {
			if u == session.SessionUUID {
				return session, true
			}
			return model.CourtSession{}, false
		},
		memoryLister: nil, // 故意不注入
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/memory", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "memory lister not configured")
}

// TestGetVisibleMemory_PayloadNestedContract 锁定 v1.0-patch (2026-08-23)
// fix U1/U2 发现的契约：memory 行的 content / stance / confidence /
// reasoning 必须在 payload 子对象里, 顶层不得平铺。
//
// 根因 (docs/todo/bugfix-log-2026-08-23.md §U1):
//   早期 frontend/lib/courtroomHydrate.ts 在 hydrate 完成后, 额外调一次
//   setMemoryEntries(memory.map(r => ({ ..., content: r.content ?? "" }))),
//   但 r.content 在顶层永远是 undefined, 正确字段在 r.payload.content。
//   而且 setMemoryEntries 在 applyCourtEvent 之后执行, 用空字符串整体覆盖
//   了已正确写入的数据, 导致判决书 / 历史庭审策略笔记全空。
//
// 此测试断言 envelope 形态不变, 防止未来重构破坏 schema, 强制前端
// 必须走 payload.content 嵌套读取 (对齐 store.applyCourtEvent L749-815)。
func TestGetVisibleMemory_PayloadNestedContract(t *testing.T) {
	session := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: "mem-contract-" + uuid.New().String()[:8],
		OwnerID:     "test-user",
		Title:       "payload 嵌套契约测试",
		OptionA:     "A",
		OptionB:     "B",
	}

	// 完整结构化字段: content + stance + confidence + reasoning + linked_evidence_ids
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"content":             "对方的核心论点是 X, 我方应从 Y 反驳",
		"stance":              "challenge",
		"confidence":           0.87,
		"reasoning":           "证据 E001 显示 X 站不住脚, 因为 Z",
		"linked_evidence_ids": []string{"E001", "E003"},
	})
	require.NoError(t, err)

	rows := []model.A2AMessage{
		{
			ID:          uuid.New(),
			SessionID:   session.ID,
			MessageUUID: "mem-contract-001",
			Round:       2,
			Phase:       string(model.PhaseCrossExam),
			FromAgent:   "prosecutor",
			ToAgent:     "prosecutor",
			MessageType: string(a2a.MessageTypeStrategyNote),
			Visibility:  string(a2a.VisibilityPrivate),
			Payload:     string(payloadJSON),
		},
	}

	h := &Handler{
		sessionLookup: func(u string) (model.CourtSession, bool) {
			if u == session.SessionUUID {
				return session, true
			}
			return model.CourtSession{}, false
		},
		memoryLister: &stubMemoryLister{rows: rows},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/memory", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var body struct {
		Code int `json:"code"`
		Data struct {
			Memory []map[string]interface{} `json:"memory"`
			Count  int                      `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, 1, body.Data.Count)

	row := body.Data.Memory[0]

	// === 契约 1: 顶层不得有 content 字段 ===
	// (前端错误路径曾读 r.content 永远得到 undefined → 全空)
	_, hasTopContent := row["content"]
	require.False(t, hasTopContent,
		"memory 行顶层不应有 content 字段; content 必须在 payload 子对象里")

	// === 契约 2: payload 必须是 decoded map ===
	require.IsType(t, map[string]interface{}{}, row["payload"])
	payload := row["payload"].(map[string]interface{})

	// === 契约 3: 结构化字段全部透传 ===
	require.Equal(t, "对方的核心论点是 X, 我方应从 Y 反驳", payload["content"])
	require.Equal(t, "challenge", payload["stance"])
	require.Equal(t, float64(0.87), payload["confidence"])
	require.Equal(t, "证据 E001 显示 X 站不住脚, 因为 Z", payload["reasoning"])

	// === 契约 4: linked_evidence_ids 是 string[] (前端渲染"关联证据"链路用) ===
	require.IsType(t, []interface{}{}, payload["linked_evidence_ids"])
	evids := payload["linked_evidence_ids"].([]interface{})
	require.Len(t, evids, 2)
	require.Equal(t, "E001", evids[0])
	require.Equal(t, "E003", evids[1])

	// === 契约 5: envelope 字段名必须与 WS a2a.message 一致 ===
	// (前端 applyCourtEvent a2a.message handler 读 p.from / p.to / p.message_type /
	//  p.visibility / p.payload, 任何字段改名都会让 hydrate 失败)
	require.Equal(t, "mem-contract-001", row["message_uuid"])
	require.Equal(t, "prosecutor", row["from"])
	require.Equal(t, "prosecutor", row["to"])
	require.Equal(t, "strategy_note", row["message_type"])
	require.Equal(t, "private", row["visibility"])
	require.Equal(t, float64(2), row["round"])
	require.NotEmpty(t, row["created_at"])
}
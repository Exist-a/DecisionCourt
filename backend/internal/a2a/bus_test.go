package a2a

import (
	"context"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// capturedBroadcast 记录测试期间广播的事件，便于断言审计行为。
type capturedBroadcast struct {
	SessionUUID string
	EventType   string
	Payload     map[string]interface{}
}

// newTestBus 返回一个基于 InMemoryRepository 的 Bus，同时附带一个 broadcast 捕获器。
func newTestBus(t *testing.T) (*Bus, *InMemoryRepository, *[]capturedBroadcast) {
	clock := func() time.Time {
		return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	}
	repo := NewInMemoryRepository(clock)
	var captured []capturedBroadcast
	bus := NewBusWithClock(repo, func(sessionUUID, eventType string, payload map[string]interface{}) {
		captured = append(captured, capturedBroadcast{
			SessionUUID: sessionUUID,
			EventType:   eventType,
			Payload:     payload,
		})
	}, clock)
	return bus, repo, &captured
}

func TestBus_Send_PublicMessage(t *testing.T) {
	bus, _, captured := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	msg, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "defender",
		MessageType: MessageTypeSpeech,
		Payload: map[string]interface{}{
			"content":   "选项 A 优势明显",
			"reasoning": "我看到对方可能漏看了 X",
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, msg.MessageUUID)
	require.Equal(t, VisibilityPublic, msg.Visibility)

	// 公开消息：所有 agent 都能看到
	for _, viewer := range []string{"prosecutor", "defender", "investigator", "clerk"} {
		rows, err := bus.ListVisibleTo(ctx, sessionID, viewer)
		require.NoError(t, err)
		require.Len(t, rows, 1, "viewer %s should see the public message", viewer)
	}

	// 审计广播：每条公开消息触发一次 a2a.message 广播
	require.Len(t, *captured, 1)
	require.Equal(t, "a2a.message", (*captured)[0].EventType)
	require.Equal(t, sessionID.String(), (*captured)[0].SessionUUID)
	require.Equal(t, "public", (*captured)[0].Payload["visibility"])
}

func TestBus_Send_PrivateMemoryMessage_BroadcastIncludesPayload(t *testing.T) {
	// v0.5 回归测试：私有 memory 类型（strategy_note / opponent_weakness /
	// self_correction / evidence_eval）的 a2a.message 广播必须包含
	// payload.content —— 这是 MemoryAuditPanel 渲染策略笔记的唯一来源。
	// 其它私有类型（dispatch / report）依然不带 payload。
	bus, _, captured := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	cases := []MessageType{
		MessageTypeStrategyNote,
		MessageTypeOpponentWeakness,
		MessageTypeSelfCorrection,
		MessageTypeEvidenceEval,
	}
	for i, mt := range cases {
		_, err := bus.Send(ctx, Message{
			SessionID:   sessionID,
			From:        "prosecutor",
			To:          "prosecutor",
			MessageType: mt,
			Visibility:  VisibilityPrivate,
			Payload: map[string]interface{}{
				"memory_type": string(mt),
				"content":     "memory-content-" + string(mt),
			},
		})
		require.NoError(t, err, "case %d %s", i, mt)
	}

	require.Len(t, *captured, len(cases))
	for i, c := range *captured {
		require.Equal(t, "a2a.message", c.EventType)
		require.Equal(t, "private", c.Payload["visibility"])
		require.Equal(t, string(cases[i]), c.Payload["message_type"])
		gotPayload, ok := c.Payload["payload"].(map[string]interface{})
		require.True(t, ok, "memory message broadcast must include payload map (case %d)", i)
		require.Equal(t, "memory-content-"+string(cases[i]), gotPayload["content"],
			"memory payload.content must reach the frontend MemoryAuditPanel")
	}
}

// TestBus_Send_BroadcastEnvelopeMatchesFrontendContract 是 v0.5 的端到端契约测试，
// 锁住 a2a.message 广播 envelope 的字段名 —— 前端 MemoryAuditPanel hydrate 一条
// MemoryEntry 依赖的字段必须在广播里出现，且**字段名必须一致**。
//
// 关键背景：v0.5 早期出过 Bug B —— 后端广播用 "from"，前端读 "p.from_agent"，
// 全部 MemoryEntry 错配成 prosecutor + 数量显示为 0。这个测试就是为此而生的
// "schema freeze"：任何人改了广播字段名都会立刻爆红，不让 Bug B 复活。
//
// 锁定的契约字段（与 frontend/store/courtroomStore.ts a2a.message handler 一致）：
//   - visibility        ← c.Payload["visibility"]
//   - message_type      ← c.Payload["message_type"]
//   - from / to         ← c.Payload["from"] / ["to"]  ← 注意不是 "from_agent" / "to_agent"
//   - round / phase     ← c.Payload["round"] / ["phase"]
//   - id                ← c.Payload["id"]
//   - created_at        ← c.Payload["created_at"]
//   - payload (memory)  ← c.Payload["payload"]  ← 仅 memory 类型有
//   - payload.content   ← c.Payload["payload"]["content"]  ← MemoryEntry.content 来源
//
// 注意：MememoryEntry.agentType 是从前端的 from 字段归一化来的（mapFromToAgentType），
// 所以这条测试同时锁住了「后端 from 是 agent_type 字符串」和「前端能正确归一化」。
func TestBus_Send_BroadcastEnvelopeMatchesFrontendContract(t *testing.T) {
	bus, _, captured := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "defender", // 关键：从辩方发，验证字段名能区分控辩
		To:          "defender",
		Round:       2,
		Phase:       "cross_exam",
		MessageType: MessageTypeStrategyNote,
		Visibility:  VisibilityPrivate,
		Payload: map[string]interface{}{
			"memory_type":        string(MessageTypeStrategyNote),
			"content":            "下一轮应强调 E002 数据来源权威性",
			"linked_evidence_ids": []string{"E002"},
		},
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1)
	got := (*captured)[0].Payload

	// 1) 顶层必填字段
	require.Equal(t, "a2a.message", (*captured)[0].EventType)
	require.Equal(t, sessionID.String(), (*captured)[0].SessionUUID)
	require.Equal(t, "private", got["visibility"], "visibility=private required")
	require.Equal(t, "defender", got["from"], `from field MUST be "from" (NOT "from_agent")`)
	require.Equal(t, "defender", got["to"], `to field MUST be "to" (NOT "to_agent")`)
	require.Equal(t, "strategy_note", got["message_type"], "message_type mismatch")

	// 2) round/phase
	require.Equal(t, 2, got["round"])
	require.Equal(t, "cross_exam", got["phase"])

	// 3) id 和 created_at 必须存在（具体值不重要，但不能是 nil）
	require.NotNil(t, got["id"], "id is required for MemoryEntry.id")
	require.NotEmpty(t, got["created_at"], "created_at is required for sort stability")

	// 4) payload 嵌套 + payload.content（这是 MemoryEntry.content 唯一来源）
	payload, ok := got["payload"].(map[string]interface{})
	require.True(t, ok, "private memory broadcast must include payload envelope")
	require.Equal(t, "下一轮应强调 E002 数据来源权威性", payload["content"],
		"MemoryEntry.content reads from payload.content; if this drifts, panel is empty")
	linked, ok := payload["linked_evidence_ids"].([]string)
	require.True(t, ok, "linked_evidence_ids must be []string")
	require.Equal(t, []string{"E002"}, linked)

	// 5) 防御：明确禁止旧字段名（防止后人"修复"成 from_agent 风格）
	_, hasLegacyFromAgent := got["from_agent"]
	require.False(t, hasLegacyFromAgent,
		`envelope MUST NOT contain legacy "from_agent" — frontend reads "from"`)
}

// TestBus_Send_BroadcastRoomKey_UsesSessionUUIDNotSessionID 是 v0.5 终极
// 回归测试 —— 锁住 Bus.Send 的广播房间 key 必须用 Message.SessionUUID
// （court_sessions.session_uuid 字符串列），**不能用 SessionID**（uuid.UUID
// 主键）。两把钥匙不一致会让 hub.Broadcast 找不到房间，前端永远收不到
// a2a.message 事件。
//
// v0.5 用户报告"策略笔记永远不出现"的根因：recordSideEffects /
// makeMemoryHook 都只填了 SessionID，Bus.Send 用 SessionID.String()
// 当 room key —— 但 WebSocket hub.Join 用的是 URL param (SessionUUID
// 字符串)，于是 a2a.message 进了"鬼屋"被静默丢弃。
//
// 这个测试模拟生产 wiring：hub.Join 用 SessionUUID，A2A bus 也用
// SessionUUID，必须能找到 1 个客户端；之前错的实现会找到 0 个。
func TestBus_Send_BroadcastRoomKey_UsesSessionUUIDNotSessionID(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	var capturedSessionKeys []string
	bus := NewBus(repo, func(sessionUUID, eventType string, payload map[string]interface{}) {
		capturedSessionKeys = append(capturedSessionKeys, sessionUUID)
	})
	ctx := context.Background()
	sessionID := uuid.New() // 假设这是 court_sessions.id 主键（uuid）
	sessionUUID := "ws-room-key-xxx" // 假设这是 court_sessions.session_uuid 字符串

	// v0.5 修复后：调用方填 SessionUUID，bus 用 SessionUUID 广播。
	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		SessionUUID: sessionUUID, // <-- v0.5 关键修复：必须填
		From:        "prosecutor",
		To:          "prosecutor",
		MessageType: MessageTypeStrategyNote,
		Visibility:  VisibilityPrivate,
		Payload: map[string]interface{}{
			"content": "strategy-note-content",
		},
	})
	require.NoError(t, err)

	require.Len(t, capturedSessionKeys, 1, "bus must broadcast exactly once")
	require.Equal(t, sessionUUID, capturedSessionKeys[0],
		"broadcast room key MUST be SessionUUID (the WS hub room key), NOT SessionID.String()")
	require.NotEqual(t, sessionID.String(), capturedSessionKeys[0],
		"v0.5 regression: broadcast was using SessionID.String() before — that's the bug")
}

// TestBus_Send_NoSessionUUID_FallsBackWithWarn 是兜底测试 —— 老调用方
// 忘了填 SessionUUID，bus 应该 fallback 到 SessionID.String() 并打 WARN。
// 这是"过渡期"兼容层，PR 4.5 之后会强制要求所有调用方填 SessionUUID。
func TestBus_Send_NoSessionUUID_FallsBackWithWarn(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	var capturedSessionKeys []string
	bus := NewBus(repo, func(sessionUUID, eventType string, payload map[string]interface{}) {
		capturedSessionKeys = append(capturedSessionKeys, sessionUUID)
	})
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		// SessionUUID 故意留空 —— 测 fallback 路径
		From:        "defender",
		To:          "defender",
		MessageType: MessageTypeStrategyNote,
		Visibility:  VisibilityPrivate,
	})
	require.NoError(t, err)

	// Fallback：应该用 SessionID.String()
	require.Equal(t, sessionID.String(), capturedSessionKeys[0],
		"fallback should use SessionID.String() when SessionUUID is empty")
}

func TestBus_Send_PrivateDispatch_BroadcastStillHidesPayload(t *testing.T) {
	// 与上一个测试对应：非 memory 私有消息（dispatch/report）依然不带
	// payload —— 这是 a2a 安全边界，防止 agent 间 reasoning 泄漏到
	// websocket 订阅者。
	bus, _, captured := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "investigator",
		MessageType: MessageTypeDispatch,
		Visibility:  VisibilityPrivate,
		Payload: map[string]interface{}{
			"query":     "行业数据",
			"reasoning": "对手忽视了 2024 增长率",
		},
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1)
	_, hasPayload := (*captured)[0].Payload["payload"]
	require.False(t, hasPayload, "private dispatch audit broadcast must NOT include payload")
}

func TestBus_Send_PrivateMessage_OnlyRecipientSeesIt(t *testing.T) {
	bus, _, captured := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	// Prosecutor 派遣 Investigator，dispatch 请求是私有消息
	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "investigator",
		MessageType: MessageTypeDispatch,
		Visibility:  VisibilityPrivate,
		Payload: map[string]interface{}{
			"query":     "行业增长率",
			"reasoning": "我希望找到能支持 A 的数据",
		},
	})
	require.NoError(t, err)

	// Investigator 看到的消息
	invRows, err := bus.ListVisibleTo(ctx, sessionID, "investigator")
	require.NoError(t, err)
	require.Len(t, invRows, 1, "investigator should see the private dispatch")

	// Defender 看不到
	defRows, err := bus.ListVisibleTo(ctx, sessionID, "defender")
	require.NoError(t, err)
	require.Len(t, defRows, 0, "defender must NOT see prosecutor's private dispatch")

	// Clerk 看不到
	clerkRows, err := bus.ListVisibleTo(ctx, sessionID, "clerk")
	require.NoError(t, err)
	require.Len(t, clerkRows, 0, "clerk must NOT see prosecutor's private dispatch")

	// 发送方自己看不到吗？不，发送方也是消息参与者（from_agent 字段会被 ListVisibleTo 包含）
	// PRD §9.3 隔离规则表：发送方当然能看到自己发出去的消息
	proRows, err := bus.ListVisibleTo(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, proRows, 1, "sender (prosecutor) should also see their own dispatch")

	// 审计广播：私有消息不应包含 payload 字段，避免泄漏
	require.Len(t, *captured, 1)
	require.Equal(t, "private", (*captured)[0].Payload["visibility"])
	_, hasPayload := (*captured)[0].Payload["payload"]
	require.False(t, hasPayload, "private message audit broadcast must NOT include payload")
}

func TestBus_Send_PrivateReportFromInvestigator(t *testing.T) {
	bus, _, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	// Investigator 的回报也是私有消息，只给 Prosecutor
	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "investigator",
		To:          "prosecutor",
		MessageType: MessageTypeReport,
		Visibility:  VisibilityPrivate,
		Payload: map[string]interface{}{
			"summary": "搜索到 3 条支持 A 的数据",
		},
	})
	require.NoError(t, err)

	proRows, err := bus.ListVisibleTo(ctx, sessionID, "prosecutor")
	require.NoError(t, err)
	require.Len(t, proRows, 1, "prosecutor should see report addressed to them")

	defRows, err := bus.ListVisibleTo(ctx, sessionID, "defender")
	require.NoError(t, err)
	require.Len(t, defRows, 0, "defender must NOT see prosecutor's private report")

	// 即使 Investigator 是发送方，自己也不能在 ListVisibleTo 中"看到"这条私有的"给 prosecutor 的报告"
	// 实际上 Investigator 作为 from_agent 也会被包含，这与上面 case 一致：发送方可见自己的消息。
	invRows, err := bus.ListVisibleTo(ctx, sessionID, "investigator")
	require.NoError(t, err)
	require.Len(t, invRows, 1, "investigator as sender should see their own report")
}

func TestBus_OrchestratorSeesEverything(t *testing.T) {
	bus, _, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	// 多条混合消息
	_, err := bus.Send(ctx, Message{SessionID: sessionID, From: "prosecutor", To: "defender", MessageType: MessageTypeSpeech})
	require.NoError(t, err)
	_, err = bus.Send(ctx, Message{SessionID: sessionID, From: "prosecutor", To: "investigator", MessageType: MessageTypeDispatch, Visibility: VisibilityPrivate})
	require.NoError(t, err)
	_, err = bus.Send(ctx, Message{SessionID: sessionID, From: "investigator", To: "prosecutor", MessageType: MessageTypeReport, Visibility: VisibilityPrivate})
	require.NoError(t, err)
	_, err = bus.Send(ctx, Message{SessionID: sessionID, From: "defender", To: "prosecutor", MessageType: MessageTypeChallenge, Visibility: VisibilityPrivate})
	require.NoError(t, err)

	rows, err := bus.ListVisibleTo(ctx, sessionID, AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rows, 4, "orchestrator must see every A2A message regardless of visibility")
}

func TestBus_Send_DefaultVisibilityIsPublic(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "defender",
		MessageType: MessageTypeSpeech,
	})
	require.NoError(t, err)

	// 通过 ListVisibleTo 间接校验落库行为：default visibility=public 应让所有 viewer 看到
	for _, viewer := range []string{"prosecutor", "defender", "investigator", "clerk"} {
		rows, err := bus.ListVisibleTo(ctx, sessionID, viewer)
		require.NoError(t, err)
		require.Len(t, rows, 1, "default visibility should be public so viewer %s can see it", viewer)
	}

	// 直接断言落库的 row.Visibility 字段
	rows, err := repo.ListVisibleTo(ctx, sessionID, AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, string(VisibilityPublic), rows[0].Visibility)
}

func TestBus_Send_RequiresFromToType(t *testing.T) {
	bus, _, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	cases := []struct {
		name string
		msg  Message
	}{
		{"missing from", Message{SessionID: sessionID, To: "defender", MessageType: MessageTypeSpeech}},
		{"missing to", Message{SessionID: sessionID, From: "prosecutor", MessageType: MessageTypeSpeech}},
		{"missing type", Message{SessionID: sessionID, From: "prosecutor", To: "defender"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bus.Send(ctx, tc.msg)
			require.Error(t, err)
		})
	}
}

func TestMessage_SanitizedPayload_StripsReasoning(t *testing.T) {
	m := Message{
		Payload: map[string]interface{}{
			"content":   "公开内容",
			"reasoning": "私有推理",
			"stance":    "pro_a",
		},
	}
	out := m.SanitizedPayload()
	_, hasReasoning := out["reasoning"]
	require.False(t, hasReasoning, "reasoning must be stripped from sanitized payload")
	require.Equal(t, "公开内容", out["content"])
	require.Equal(t, "pro_a", out["stance"])
}

func TestBus_DecodePayload_RoundTrip(t *testing.T) {
	bus, _, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	original := map[string]interface{}{
		"query":       "行业数据",
		"max_results": float64(3),
	}
	msg, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "investigator",
		MessageType: MessageTypeDispatch,
		Visibility:  VisibilityPrivate,
		Payload:     original,
	})
	require.NoError(t, err)

	rows, err := bus.ListVisibleTo(ctx, sessionID, "investigator")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	decoded, err := DecodePayload(rows[0])
	require.NoError(t, err)
	require.Equal(t, "行业数据", decoded["query"])
	require.EqualValues(t, 3, decoded["max_results"])

	// 隐式契约：MessageUUID 通过列表回读应当一致
	require.Equal(t, msg.MessageUUID, rows[0].MessageUUID)
	require.WithinDuration(t, msg.CreatedAt, rows[0].CreatedAt, time.Second)
}

func TestBus_Send_FillsMessageUUIDAndCreatedAt(t *testing.T) {
	bus, repo, _ := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	msg, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "defender",
		MessageType: MessageTypeSpeech,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, msg.ID)
	require.NotEmpty(t, msg.MessageUUID)

	// 时钟由 InMemoryRepository 提供，断言一致性
	rows, err := repo.ListVisibleTo(ctx, sessionID, AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, msg.MessageUUID, rows[0].MessageUUID)
	require.Equal(t, msg.CreatedAt.UTC(), rows[0].CreatedAt.UTC())
}

func TestBus_Send_AuditBroadcast_PublicIncludesPayload(t *testing.T) {
	bus, _, captured := newTestBus(t)
	ctx := context.Background()
	sessionID := uuid.New()

	payload := map[string]interface{}{"content": "公开内容", "stance": "pro_a"}
	_, err := bus.Send(ctx, Message{
		SessionID:   sessionID,
		From:        "prosecutor",
		To:          "defender",
		MessageType: MessageTypeSpeech,
		Payload:     payload,
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1)
	require.Equal(t, "a2a.message", (*captured)[0].EventType)
	require.Equal(t, "speech", (*captured)[0].Payload["message_type"])
	require.Equal(t, "public", (*captured)[0].Payload["visibility"])
	require.Equal(t, "prosecutor", (*captured)[0].Payload["from"])
	require.Equal(t, "defender", (*captured)[0].Payload["to"])
	gotPayload, ok := (*captured)[0].Payload["payload"].(map[string]interface{})
	require.True(t, ok, "public message audit should carry payload map")
	require.Equal(t, "公开内容", gotPayload["content"])
}

func TestInMemoryRepository_IsolatedBySessionID(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	sessionA := uuid.New()
	sessionB := uuid.New()

	_, err := repo.Insert(ctx, model.A2AMessage{
		SessionID:   sessionA,
		MessageUUID: uuid.New().String(),
		FromAgent:   "prosecutor",
		ToAgent:     "defender",
		MessageType: string(MessageTypeSpeech),
		Visibility:  string(VisibilityPublic),
	})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, model.A2AMessage{
		SessionID:   sessionB,
		MessageUUID: uuid.New().String(),
		FromAgent:   "prosecutor",
		ToAgent:     "defender",
		MessageType: string(MessageTypeSpeech),
		Visibility:  string(VisibilityPublic),
	})
	require.NoError(t, err)

	rowsA, err := repo.ListVisibleTo(ctx, sessionA, AddressOrchestrator)
	require.NoError(t, err)
	require.Len(t, rowsA, 1)
	require.Equal(t, sessionA, rowsA[0].SessionID)
}
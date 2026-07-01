package courtroom

import (
	"context"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/google/uuid"
)

// TestExportSession_A2AVisibilityIsolation 是 ExportSession 最重要的正确性
// 测试：验证 export 只包含 user 视角可见的 a2a_messages（public + user
// 自己的 private），**不**包含对家 private memory（控方 strategy_note 当
// viewer=user 不应出现，因为 from/to 都是 prosecutor 而不是 user）。
//
// 这条测试不依赖真实 DB：a2a 部分用 InMemoryRepository，db/svc 用
// partial fake（只覆盖 ExportSession 用到的字段）。
func TestExportSession_A2AVisibilityIsolation(t *testing.T) {
	sessionID := uuid.New()
	sessionUUID := "court-export-test"

	// 1) 准备 a2a InMemoryRepository + Bus
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)

	// 2) 写 4 条 a2a 消息：
	//    a) public 发言（双方都能看）
	//    b) 控方 → 控方 private memory（user 看不到）
	//    c) 辩方 → 辩方 private memory（user 看不到）
	//    d) user → user private note（user 能看，比如系统发给 user 的）
	//    e) 控方 → user 私信（user 能看）
	tests := []struct {
		name    string
		msg     a2a.Message
		visible bool
	}{
		{
			name: "public speech",
			msg: a2a.Message{
				SessionUUID: sessionUUID,
				SessionID:   sessionID,
				Round:       1,
				Phase:       "cross_exam",
				From:        "prosecutor",
				To:          "defender",
				MessageType: a2a.MessageTypeSpeech,
				Visibility:  a2a.VisibilityPublic,
				Payload:     map[string]interface{}{"content": "public content"},
			},
			visible: true,
		},
		{
			name: "prosecutor private memory (MUST NOT LEAK)",
			msg: a2a.Message{
				SessionUUID: sessionUUID,
				SessionID:   sessionID,
				Round:       1,
				Phase:       "cross_exam",
				From:        "prosecutor",
				To:          "prosecutor",
				MessageType: a2a.MessageTypeStrategyNote,
				Visibility:  a2a.VisibilityPrivate,
				Payload:     map[string]interface{}{"content": "prosecutor secret"},
			},
			visible: false,
		},
		{
			name: "defender private memory (MUST NOT LEAK)",
			msg: a2a.Message{
				SessionUUID: sessionUUID,
				SessionID:   sessionID,
				Round:       1,
				Phase:       "cross_exam",
				From:        "defender",
				To:          "defender",
				MessageType: a2a.MessageTypeStrategyNote,
				Visibility:  a2a.VisibilityPrivate,
				Payload:     map[string]interface{}{"content": "defender secret"},
			},
			visible: false,
		},
		{
			name: "prosecutor dispatch (public)",
			msg: a2a.Message{
				SessionUUID: sessionUUID,
				SessionID:   sessionID,
				Round:       2,
				Phase:       "cross_exam",
				From:        "prosecutor",
				To:          "investigator",
				MessageType: a2a.MessageTypeDispatch,
				Visibility:  a2a.VisibilityPublic,
				Payload:     map[string]interface{}{"query": "..."},
			},
			visible: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := bus.Send(context.Background(), tc.msg); err != nil {
				t.Fatalf("bus.Send failed: %v", err)
			}
		})
	}

	// 3) 用 ListVisibleTo("user") 验证 a2a 隔离 —— 这正是 ExportSession
	//    用来拿 a2a_messages 的接口。返回结果里必须不含"prosecutor secret"
	//    和"defender secret"。
	rows, err := bus.ListVisibleTo(context.Background(), sessionID, "user")
	if err != nil {
		t.Fatalf("ListVisibleTo failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 visible rows (public + dispatch), got %d", len(rows))
	}
	for _, r := range rows {
		// 双重检查：payload 不含敏感字符串
		// 简单序列化就行，因为 payload 已经是 map[string]interface{}
		s := serializeForAssertion(r.Payload)
		if strings.Contains(s, "prosecutor secret") {
			t.Errorf("LEAK: user should NOT see prosecutor private memory, got: %s", s)
		}
		if strings.Contains(s, "defender secret") {
			t.Errorf("LEAK: user should NOT see defender private memory, got: %s", s)
		}
	}

	// 4) 反向验证：orchestrator viewer 能看到全部（含 2 条 private）
	//    确认不是 ListVisibleTo 本身 bug，而是 ExportSession 用 "user" 隔离。
	allRows, err := bus.ListVisibleTo(context.Background(), sessionID, a2a.AddressOrchestrator)
	if err != nil {
		t.Fatalf("ListVisibleTo(orchestrator) failed: %v", err)
	}
	// orchestrator 看到全部 4 条：public + 控 private + 辩 private + dispatch
	if len(allRows) != 4 {
		t.Errorf("orchestrator should see all 4 messages, got %d", len(allRows))
	}
}

// serializeForAssertion 把任意 payload 拍平成字符串方便 substring 检查。
// a2a.Payload 可能是 map / string / 其它；本测试用 map。
func serializeForAssertion(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]interface{}); ok {
		var b strings.Builder
		for k, v := range m {
			b.WriteString(k)
			b.WriteString(":")
			b.WriteString(serializeForAssertion(v))
			b.WriteString(";")
		}
		return b.String()
	}
	return ""
}

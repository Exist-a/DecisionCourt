package a2a

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// TestInMemoryRepository_ListPrivateMemory 验证 v0.5 私有情节记忆类型
// (strategy_note / opponent_weakness / self_correction / evidence_eval)
// 全部被 ListPrivateMemory 返回，而其他 message_type（包括所有 public
// 的 speech / dispatch / report）都被过滤掉。
//
// 这是 v0.8.3 "刷新后策略笔记 Tab 全空" 修复的核心后端契约 —— GET
// /api/v1/courtrooms/:uuid/memory 必须返回这些行才能让 MemoryAuditPanel
// 在 reload 后恢复完整时间线。
func TestInMemoryRepository_ListPrivateMemory(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	sessionID := uuid.New()

	// 灌入全部四种 private memory 类型 + 一些 public 行（应被过滤）
	rows := []model.A2AMessage{
		{SessionID: sessionID, MessageUUID: "mem-1", MessageType: string(MessageTypeStrategyNote), Visibility: string(VisibilityPrivate), FromAgent: "prosecutor", ToAgent: "prosecutor", Payload: `{"content":"note 1"}`},
		{SessionID: sessionID, MessageUUID: "mem-2", MessageType: string(MessageTypeOpponentWeakness), Visibility: string(VisibilityPrivate), FromAgent: "defender", ToAgent: "defender", Payload: `{"content":"weakness 1"}`},
		{SessionID: sessionID, MessageUUID: "mem-3", MessageType: string(MessageTypeSelfCorrection), Visibility: string(VisibilityPrivate), FromAgent: "prosecutor", ToAgent: "prosecutor", Payload: `{"content":"correction 1"}`},
		{SessionID: sessionID, MessageUUID: "mem-4", MessageType: string(MessageTypeEvidenceEval), Visibility: string(VisibilityPrivate), FromAgent: "defender", ToAgent: "defender", Payload: `{"content":"eval 1"}`},
		// 不应被返回的：public speech / dispatch / report
		{SessionID: sessionID, MessageUUID: "speech-1", MessageType: string(MessageTypeSpeech), Visibility: string(VisibilityPublic), FromAgent: "prosecutor", ToAgent: "defender", Payload: `{"content":"public speech"}`},
		{SessionID: sessionID, MessageUUID: "dispatch-1", MessageType: string(MessageTypeDispatch), Visibility: string(VisibilityPublic), FromAgent: "prosecutor", ToAgent: "investigator", Payload: `{"query":"x"}`},
		{SessionID: sessionID, MessageUUID: "report-1", MessageType: string(MessageTypeReport), Visibility: string(VisibilityPrivate), FromAgent: "investigator", ToAgent: "prosecutor", Payload: `{"content":"private report"}`},
		// 别的 session 的私有 memory —— 不应被这个 session 看到
		{SessionID: uuid.New(), MessageUUID: "mem-other", MessageType: string(MessageTypeStrategyNote), Visibility: string(VisibilityPrivate), FromAgent: "prosecutor", ToAgent: "prosecutor", Payload: `{}`},
	}
	for _, r := range rows {
		if _, err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
	}

	got, err := repo.ListPrivateMemory(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListPrivateMemory failed: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 private memory rows, got %d", len(got))
	}
	// 校验 UUID 集合：保证 speech / dispatch / report / 其他 session 的都被过滤
	seen := make(map[string]bool)
	for _, r := range got {
		seen[r.MessageUUID] = true
	}
	for _, want := range []string{"mem-1", "mem-2", "mem-3", "mem-4"} {
		if !seen[want] {
			t.Errorf("expected %s in result", want)
		}
	}
	for _, dontWant := range []string{"speech-1", "dispatch-1", "report-1", "mem-other"} {
		if seen[dontWant] {
			t.Errorf("did NOT expect %s in result", dontWant)
		}
	}
}

// TestInMemoryRepository_ListPrivateMemory_EmptySession 验证：session
// 没有 memory 时返回空切片而不是 nil —— handler 期望非 nil 切片以
// 避免前端的 ".map is not a function" 之类的 error。
func TestInMemoryRepository_ListPrivateMemory_EmptySession(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	got, err := repo.ListPrivateMemory(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ListPrivateMemory failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 rows, got %d", len(got))
	}
}
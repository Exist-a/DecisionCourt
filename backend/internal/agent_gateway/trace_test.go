package agent_gateway

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// 验证 WithTrace / FromContext 在 nil / 设置过 / 缺失三种情况下行为一致：
// 缺失字段返回空串而不是 panic，便于在调用方不强制注入 trace 时网关仍能
// 跑通（用于未接入主流程的测试 LLM client）。

func TestFromContext_EmptyByDefault(t *testing.T) {
	t.Parallel()
	tr := FromContext(context.Background())
	if tr.SessionUUID != "" {
		t.Errorf("expected empty SessionUUID, got %q", tr.SessionUUID)
	}
	if tr.AgentType != "" {
		t.Errorf("expected empty AgentType, got %q", tr.AgentType)
	}
	if tr.TaskType != "" {
		t.Errorf("expected empty TaskType, got %q", tr.TaskType)
	}
	if tr.RequestID != "" {
		t.Errorf("expected empty RequestID, got %q", tr.RequestID)
	}
}

func TestWithTrace_RoundTrip(t *testing.T) {
	t.Parallel()
	sid := uuid.NewString()
	ctx := WithTrace(context.Background(), Trace{
		SessionUUID: sid,
		AgentType:   "prosecutor",
		TaskType:    "speak",
	})

	got := FromContext(ctx)
	if got.SessionUUID != sid {
		t.Errorf("SessionUUID: want %q got %q", sid, got.SessionUUID)
	}
	if got.AgentType != "prosecutor" {
		t.Errorf("AgentType: want prosecutor got %q", got.AgentType)
	}
	if got.TaskType != "speak" {
		t.Errorf("TaskType: want speak got %q", got.TaskType)
	}
}

func TestWithTrace_AutoRequestID(t *testing.T) {
	t.Parallel()
	ctx := WithTrace(context.Background(), Trace{
		SessionUUID: uuid.NewString(),
	})
	tr := FromContext(ctx)
	if tr.RequestID == "" {
		t.Errorf("expected RequestID to be auto-assigned, got empty")
	}
	// uuid string 长度为 36
	if len(tr.RequestID) != 36 {
		t.Errorf("RequestID should be uuid (36 chars), got len=%d val=%q", len(tr.RequestID), tr.RequestID)
	}
}

func TestWithTrace_PreservesExplicitRequestID(t *testing.T) {
	t.Parallel()
	rid := "req-fixed-123"
	ctx := WithTrace(context.Background(), Trace{
		SessionUUID: uuid.NewString(),
		RequestID:   rid,
	})
	if got := FromContext(ctx).RequestID; got != rid {
		t.Errorf("RequestID: want %q got %q", rid, got)
	}
}

func TestWithTrace_OverwriteWithNewer(t *testing.T) {
	t.Parallel()
	ctx := WithTrace(context.Background(), Trace{
		SessionUUID: "outer",
		AgentType:   "judge",
	})
	ctx = WithTrace(ctx, Trace{
		SessionUUID: "inner",
		AgentType:   "prosecutor",
		TaskType:    "speak",
	})
	tr := FromContext(ctx)
	if tr.SessionUUID != "inner" {
		t.Errorf("SessionUUID: want inner got %q", tr.SessionUUID)
	}
	if tr.AgentType != "prosecutor" {
		t.Errorf("AgentType: want prosecutor got %q", tr.AgentType)
	}
	if tr.TaskType != "speak" {
		t.Errorf("TaskType: want speak got %q", tr.TaskType)
	}
}

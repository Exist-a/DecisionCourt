package external

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBridge_ConvertExternalTask 验证外部 A2A task → 内部 A2A Message 转换。
func TestBridge_ConvertExternalTask(t *testing.T) {
	bridge := NewBridge(nil)
	validUUID := uuid.New().String()
	task := ExternalTask{
		ID:     "task-123",
		Method: "tasks/send",
		Message: ExternalMessage{
			Role: "user",
			Parts: []ExternalPart{
				{Type: "text", Text: "hello world"},
			},
		},
		SessionUUID: validUUID,
	}
	ctx := context.Background()

	msg, err := bridge.ConvertToInternalMessage(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, validUUID, msg.SessionUUID)
	assert.Equal(t, "external", msg.From)
	assert.Equal(t, "judge", msg.To) // default 接收方
	assert.Equal(t, a2a.VisibilityPrivate, msg.Visibility)
	parsed, _ := uuid.Parse(validUUID)
	assert.Equal(t, parsed, msg.SessionID)
}

// TestBridge_DefaultTo 验证默认接收方是 judge。
func TestBridge_DefaultTo(t *testing.T) {
	bridge := NewBridge(nil)
	task := ExternalTask{
		ID: "task-1",
		Message: ExternalMessage{
			Parts: []ExternalPart{{Type: "text", Text: "hi"}},
		},
	}
	msg, err := bridge.ConvertToInternalMessage(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, "judge", msg.To)
}

// TestBridge_EmptyText 验证空文本也能转换（不报错）。
func TestBridge_EmptyText(t *testing.T) {
	bridge := NewBridge(nil)
	task := ExternalTask{
		ID: "task-1",
		Message: ExternalMessage{
			Parts: []ExternalPart{},
		},
	}
	msg, err := bridge.ConvertToInternalMessage(context.Background(), task)
	require.NoError(t, err)
	assert.NotEmpty(t, msg.Payload)
}

// TestBridge_NoBus 验证 Bus 为 nil 时不报错（用于测试 + 最小实装）。
func TestBridge_NoBus(t *testing.T) {
	bridge := NewBridge(nil) // nil bus
	task := ExternalTask{
		ID: "task-1",
		Message: ExternalMessage{
			Parts: []ExternalPart{{Type: "text", Text: "hi"}},
		},
	}
	// ConvertToInternalMessage 不需要 bus
	_, err := bridge.ConvertToInternalMessage(context.Background(), task)
	require.NoError(t, err)
}

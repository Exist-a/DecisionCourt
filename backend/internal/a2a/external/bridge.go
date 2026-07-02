package external

import (
	"context"
	"fmt"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/google/uuid"
)

// Bridge 把外部 A2A 任务桥接到内部 A2A 总线。
//
// 设计：
//   - Bus 为 nil 时仅做转换不发送（用于测试与最小实装）
//   - 外部 task → 内部 a2a.Message 转换（保持 trace_id 串联）
//   - 默认接收方是 judge（外部用户向法官提问）
//   - MessageType = "inquiry"（最贴近的现有语义：外部提问）
type Bridge struct {
	Bus *a2a.Bus
}

// ExternalTask 是 A2A 协议标准的 task 格式（最小子集）。
type ExternalTask struct {
	ID          string          `json:"id"`
	Method      string          `json:"method"`
	Message     ExternalMessage `json:"message"`
	SessionUUID string          `json:"sessionId,omitempty"`
}

// ExternalMessage 是 A2A 协议 message 格式（最小子集）。
type ExternalMessage struct {
	Role  string         `json:"role"`
	Parts []ExternalPart `json:"parts"`
}

// ExternalPart 是 message 的内容部分。
type ExternalPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewBridge 构造 Bridge。bus 可为 nil（用于测试）。
func NewBridge(bus *a2a.Bus) *Bridge {
	return &Bridge{Bus: bus}
}

// ConvertToInternalMessage 把外部 task 转换为内部 a2a.Message。
//
// 转换规则：
//   - From = "external"（标记来源）
//   - To = "judge"（默认接收方）
//   - Visibility = private（外部来源默认私有）
//   - SessionID = 解析 sessionUUID；解析失败保留原值（业务 key 仍可用）
//   - MessageType = a2a.MessageTypeInquiry（"提问"语义最接近）
func (b *Bridge) ConvertToInternalMessage(_ context.Context, task ExternalTask) (a2a.Message, error) {
	sessionID := uuid.Nil
	if task.SessionUUID != "" {
		if u, err := uuid.Parse(task.SessionUUID); err == nil {
			sessionID = u
		}
	}
	// 拼接 text parts → payload
	text := ""
	for _, p := range task.Message.Parts {
		if p.Type == "text" {
			text += p.Text
		}
	}
	return a2a.Message{
		MessageUUID: uuid.New().String(),
		SessionID:   sessionID,
		SessionUUID: task.SessionUUID,
		From:        "external",
		To:          "judge",
		MessageType: a2a.MessageTypeInquiry,
		Visibility:  a2a.VisibilityPrivate,
		Payload: map[string]interface{}{
			"text":   text,
			"method": task.Method,
		},
	}, nil
}

// PublishToInternalBus 桥接外部 task 到内部 A2A bus。
//
// 行为：
//   - ConvertToInternalMessage 转格式
//   - Bus 为 nil 时仅 return nil（noop）
//   - Bus 非 nil 时 Bus.Send（会落库 + 按可见性广播）
func (b *Bridge) PublishToInternalBus(ctx context.Context, task ExternalTask) error {
	if b == nil {
		return nil
	}
	msg, err := b.ConvertToInternalMessage(ctx, task)
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	if b.Bus == nil {
		// 桥接器没接 bus：noop（用于测试）
		return nil
	}
	// Bus.Send(ctx, msg) - 实际签名见 internal/a2a/bus.go:98
	_, err = b.Bus.Send(ctx, msg)
	return err
}

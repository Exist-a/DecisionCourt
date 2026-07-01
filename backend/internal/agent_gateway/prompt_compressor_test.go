package agent_gateway

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

func TestPromptCompressor_NoOpWhenNormal(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{})
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
	}
	out, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusNormal})
	if len(out) != 1 || out[0].Content != "hello" {
		t.Errorf("should not compress normal status")
	}
	if info.Applied {
		t.Errorf("applied should be false")
	}
}

func TestPromptCompressor_TrimsLongMessages(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{})
	msgs := make([]llm.Message, 12)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Content: "msg"}
	}
	out, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusCompress})
	if len(out) != compressKeepHistory {
		t.Errorf("want %d messages, got %d", compressKeepHistory, len(out))
	}
	if !info.Applied {
		t.Errorf("applied should be true")
	}
	if info.BeforeCount != 12 || info.AfterCount != compressKeepHistory {
		t.Errorf("counts wrong: %d -> %d", info.BeforeCount, info.AfterCount)
	}
}

func TestPromptCompressor_TrimsLongContent(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{})
	long := strings.Repeat("a", compressMaxMsgLen+1)
	msgs := []llm.Message{{Role: "user", Content: long}}
	out, info := pc.Compress(msgs, BudgetSnapshot{Status: StatusThrottle})
	if len(out) != 1 {
		t.Fatalf("want 1 msg, got %d", len(out))
	}
	if len(out[0].Content) != compressTargetLen {
		t.Errorf("content length: want %d got %d", compressTargetLen, len(out[0].Content))
	}
	if info.AfterLength == 0 {
		t.Errorf("AfterLength should be recorded")
	}
}

func TestPromptCompressor_KeepsSystemAtFront(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{})
	msgs := make([]llm.Message, 12)
	msgs[0] = llm.Message{Role: "system", Content: "sys-prompt"}
	for i := 1; i < len(msgs); i++ {
		msgs[i] = llm.Message{Role: "user", Content: "msg"}
	}
	out, _ := pc.Compress(msgs, BudgetSnapshot{Status: StatusExhausted})
	if out[0].Role != "system" || out[0].Content != "sys-prompt" {
		t.Errorf("system prompt should be kept at front: %+v", out[0])
	}
}

func TestPromptCompressor_EmptyMessages(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{})
	out, info := pc.Compress(nil, BudgetSnapshot{Status: StatusCompress})
	if len(out) != 0 {
		t.Errorf("want empty, got %d", len(out))
	}
	if info.Applied {
		t.Errorf("applied should be false for empty")
	}
}

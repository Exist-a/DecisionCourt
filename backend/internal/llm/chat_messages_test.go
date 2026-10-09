package llm

// 回归护栏（2026-10-09）：空 systemPrompt 不得变成一条"没有 content 的 system 消息"。
//
// 背景：go-openai 的 ChatCompletionMessage.Content 带 `json:"content,omitempty"`，
// 空字符串在序列化时整个字段被丢掉 → 上游收到 {"role":"system"} → DeepSeek 返回
// 422 `messages[0]: missing field 'content'`。
//
// 真实影响：promptlab 的 LLM-as-judge（evalViaLLM 传 systemPrompt=""）因此恒失败，
// Prompt Lab 的 LLM 评分 / A/B 一直不可用，而接口仍返 200、失败信息只藏在 reasoning 里。
// 所以这里断言的**不是**内部结构，而是真正发给上游的 JSON —— 那才是上游校验的东西。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildChatMessages_SkipsEmptySystem 单元层面：空 system 不产生 system 消息。
func TestBuildChatMessages_SkipsEmptySystem(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "喂进去的内容"}}

	got := buildChatMessages("", msgs)
	require.Len(t, got, 1, "空 systemPrompt 不应产生 system 消息")
	assert.Equal(t, "user", got[0].Role)
	assert.Equal(t, "喂进去的内容", got[0].Content)

	got = buildChatMessages("你是法官", msgs)
	require.Len(t, got, 2)
	assert.Equal(t, openai.ChatMessageRoleSystem, got[0].Role)
	assert.Equal(t, "你是法官", got[0].Content)
	assert.Equal(t, "user", got[1].Role)
}

// TestBuildChatMessages_EmptySystemAndNoMessages 极端输入不 panic。
func TestBuildChatMessages_EmptySystemAndNoMessages(t *testing.T) {
	assert.Empty(t, buildChatMessages("", nil))
	assert.Len(t, buildChatMessages("sys", nil), 1)
}

// TestComplete_PayloadHasNoContentlessMessage 端到端拦住这个坑：
// 起一个假上游，断言**每条消息的 JSON 里都有非空 content 字段**。
//
// 这同时覆盖 promptlab 的真实调用形态（systemPrompt="" + 单条 user 消息）。
func TestComplete_PayloadHasNoContentlessMessage(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","model":"m",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = srv.URL
	c := &openAIClient{client: openai.NewClientWithConfig(cfg)}

	_, _, err := c.Complete(context.Background(), "", // ← promptlab 的真实形态
		[]Message{{Role: "user", Content: "评定这段发言"}},
		CompletionOptions{Model: "test-model"},
	)
	require.NoError(t, err)
	require.NotEmpty(t, captured, "假上游应当收到请求体")

	var payload struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(captured, &payload))
	require.Len(t, payload.Messages, 1, "空 systemPrompt 时只应发 1 条消息")

	for i, m := range payload.Messages {
		raw, ok := m["content"]
		require.True(t, ok,
			"messages[%d] 没有 content 字段 —— 上游会 422 missing field `content`（原始体: %s）",
			i, string(captured))
		var s string
		require.NoError(t, json.Unmarshal(raw, &s))
		assert.NotEmpty(t, s, "messages[%d] 的 content 不能为空字符串", i)
	}
}

// TestComplete_PayloadKeepsSystemMessageWhenPresent 非空 systemPrompt 行为不变
// （避免修复顺手改坏了正常路径）。
func TestComplete_PayloadKeepsSystemMessageWhenPresent(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","model":"m",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = srv.URL
	c := &openAIClient{client: openai.NewClientWithConfig(cfg)}

	_, _, err := c.Complete(context.Background(), "你是法官",
		[]Message{{Role: "user", Content: "内容"}},
		CompletionOptions{Model: "test-model"},
	)
	require.NoError(t, err)

	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(captured, &payload))
	require.Len(t, payload.Messages, 2)
	assert.Equal(t, "system", payload.Messages[0].Role)
	assert.Equal(t, "你是法官", payload.Messages[0].Content)
}

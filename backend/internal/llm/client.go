package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/decisioncourt/backend/internal/config"
	"github.com/sashabaranov/go-openai"
)

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type CompletionOptions struct {
	Model       string
	Temperature float32
	MaxTokens   int
	JSONMode    bool
}

// StreamChunk 是流式输出的一次增量。Content 是相对上一次的新片段；
// Done=true 时本批为最后一块；Err 仅在 Done=true 时可能携带错误。
type StreamChunk struct {
	Content string
	Done    bool
	Err     error
}

type Client interface {
	Complete(ctx context.Context, systemPrompt string, messages []Message, opts CompletionOptions) (string, Usage, error)

	// StreamComplete 流式返回 content 片段，每次 channel 收到一个 chunk。
	// 流结束时 channel 关闭；调用方需要持续 drain 直到收到 Done=true 的
	// chunk（Err 可能非 nil）。流式仅用于最终 speak action —— tool_call /
	// reflect 决策仍走 Complete 因为需要完整 JSON。
	//
	// 实现要求：
	//   - 至少在 Done=true 时关闭 channel
	//   - 任何中间错误后立即 Done=true 并带 Err
	//   - 不阻塞调用方
	StreamComplete(ctx context.Context, systemPrompt string, messages []Message, opts CompletionOptions) <-chan StreamChunk
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Metadata 是 Agent Gateway 评分 / 原子组识别用的可选标签，不会发送到上游 LLM。
	//   已知 keys：
	//     agent_type    "judge" / "prosecutor" / "defender" / ...
	//     tool_call_id  标记 assistant(tool_calls) 与 tool 结果配对
	//     evidence_id   标记证据引用
	//   不序列化到 JSON（避免影响上游解析）。
	Metadata map[string]string `json:"-"`
}

type openAIClient struct {
	client *openai.Client
}

func NewClient() (Client, error) {
	cfg := config.AppConfig
	if cfg.LLMAPIKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY is not set")
	}

	clientConfig := openai.DefaultConfig(cfg.LLMAPIKey)
	if cfg.LLMBaseURL != "" {
		clientConfig.BaseURL = cfg.LLMBaseURL
	}

	return &openAIClient{
		client: openai.NewClientWithConfig(clientConfig),
	}, nil
}

func (c *openAIClient) Complete(
	ctx context.Context,
	systemPrompt string,
	messages []Message,
	opts CompletionOptions,
) (string, Usage, error) {
	if opts.Model == "" {
		opts.Model = config.AppConfig.LLMModelV3
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.7
	}

	chatMessages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}

	for _, m := range messages {
		chatMessages = append(chatMessages, openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	req := openai.ChatCompletionRequest{
		Model:       opts.Model,
		Messages:    chatMessages,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
	}

	if opts.JSONMode {
		req.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}

	start := time.Now()
	resp, err := c.client.CreateChatCompletion(ctx, req)
	latency := time.Since(start)

	if err != nil {
		return "", Usage{}, fmt.Errorf("llm completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", Usage{}, fmt.Errorf("no completion choices returned")
	}

	content := resp.Choices[0].Message.Content
	usage := Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	fmt.Printf("[LLM] provider=%s model=%s latency=%dms tokens=%d\n",
		config.AppConfig.LLMProvider,
		opts.Model,
		latency.Milliseconds(),
		usage.TotalTokens,
	)

	if opts.JSONMode {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			// Try to extract JSON from markdown code block
			content = extractJSON(content)
		}
	}

	return content, usage, nil
}

// StreamComplete 流式输出 content。返回的 channel 在流结束（Done=true）
// 时关闭；调用方在拿到 Done 后应当停止 drain。
//
// 注意：本实现暂未启用 JSONMode（流式 JSON 解析对 partial JSON 边界敏感，
// deepseek 当前 API 也未官方保证）。调用方应当在决定 speak action 后
// 用 JSONMode=false 调用本方法，专门生成纯文本 content。
func (c *openAIClient) StreamComplete(
	ctx context.Context,
	systemPrompt string,
	messages []Message,
	opts CompletionOptions,
) <-chan StreamChunk {
	out := make(chan StreamChunk, 16) // small buffer so caller never blocks

	if opts.Model == "" {
		opts.Model = config.AppConfig.LLMModelV3
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.7
	}

	chatMessages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
	}
	for _, m := range messages {
		chatMessages = append(chatMessages, openai.ChatCompletionMessage{
			Role: m.Role, Content: m.Content,
		})
	}

	go func() {
		defer close(out)
		stream, err := c.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:       opts.Model,
			Messages:    chatMessages,
			Temperature: opts.Temperature,
			MaxTokens:   opts.MaxTokens,
		})
		if err != nil {
			out <- StreamChunk{Done: true, Err: fmt.Errorf("open stream: %w", err)}
			return
		}

		// v0.5 防御式 ctx 取消转发：DeepSeek 的 SSE stream 偶尔会卡住
		// 不发也不关（bufio.Reader 不响应 ctx），导致本 goroutine 永不退出、
		// channel 永不 close，消费侧 for-range 死循环 —— 整个 trial 挂起。
		// 这里开一个 watcher：ctx 一旦取消，立刻 stream.Close() 强制 Recv
		// 返回错误，本 goroutine 收到错误后写 Done 块 + close channel，
		// 消费侧就能 break 出 for-range 走 retry 兜底。
		streamDone := make(chan struct{})
		defer close(streamDone)
		go func() {
			select {
			case <-ctx.Done():
				_ = stream.Close()
			case <-streamDone:
			}
		}()
		defer stream.Close()

		for {
			resp, err := stream.Recv()
			if err != nil {
				// io.EOF 是正常结束
				if err.Error() == "EOF" {
					out <- StreamChunk{Done: true}
					return
				}
				out <- StreamChunk{Done: true, Err: fmt.Errorf("stream recv: %w", err)}
				return
			}
			// 跳过空 choice（DeepSeek / OpenAI 流式偶尔返回 placeholder）
			if len(resp.Choices) == 0 {
				continue
			}
			chunk := resp.Choices[0].Delta.Content
			if chunk == "" {
				continue
			}
			out <- StreamChunk{Content: chunk}
		}
	}()

	return out
}

// extractJSON 从 LLM 输出中提取 JSON 字符串。
//
// v0.8.3 安全(P2-5):用 strings.Index 替代手写 substring 扫描;用
// strings.LastIndex 找最右 ``` 防止 LLM 嵌套 ```;trim 前后空白;
// 最后用 json.Valid 兜底,确保返回合法 JSON。
//
// 优先级链:
//   1. ```json ... ``` 块(标准 markdown 包裹)
//   2. ``` ... ``` 块(无语言标记的代码块)
//   3. 裸 JSON(首 { + 末 } 之间),用 json.Valid 验证合法性
//
// 如果所有尝试都失败,返回原始 content(让调用方决定如何处理)。
func extractJSON(content string) string {
	const maxLen = 64 * 1024 // 64KB cap,防 LLM 异常输出大字符串把内存吃满
	if len(content) > maxLen {
		content = content[:maxLen]
	}

	// 1) ```json ... ```
	if i := strings.Index(content, "```json"); i >= 0 {
		start := i + len("```json")
		// skip 紧随其后的换行
		start = skipWhitespaceNewline(content, start)
		// 找最右一个 ``` 结束(LastIndex 抗嵌套)
		if j := strings.LastIndex(content, "```"); j > start {
			return strings.TrimSpace(content[start:j])
		}
		// 没找到结束 ```,截到末尾
		return strings.TrimSpace(content[start:])
	}

	// 2) ``` ... ``` (无 json 标记)
	if i := strings.Index(content, "```"); i >= 0 {
		start := i + 3
		start = skipWhitespaceNewline(content, start)
		if j := strings.LastIndex(content, "```"); j > start {
			candidate := strings.TrimSpace(content[start:j])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// 3) 裸 JSON:首 { + 末 } 区间
	if first := strings.IndexByte(content, '{'); first >= 0 {
		if last := strings.LastIndexByte(content, '}'); last > first {
			candidate := content[first : last+1]
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// 都失败,返回原内容(调用方做最终 fallback)
	return content
}

// skipWhitespaceNewline 跳过空白 + 换行。如果 start 已越界返回 len。
func skipWhitespaceNewline(s string, start int) int {
	if start >= len(s) {
		return start
	}
	for start < len(s) && (s[start] == ' ' || s[start] == '\n' || s[start] == '\r' || s[start] == '\t') {
		start++
	}
	return start
}

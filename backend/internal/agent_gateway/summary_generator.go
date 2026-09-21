package agent_gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/decisioncourt/backend/internal/llm"
)

// v2.10 ADR 0044 #6: abstractive summary 的 LLM 实现。
//
// 背景：extractive 摘要（BuildEarlierSummary）只列 evidence_id + 角色 + 120 字
// preview，LLM 看得到"被告已知情"这个结论，却看不到依据，于是可能编造其他依据。
// 这里在 opt-in 的前提下额外调一次轻量 LLM，要求它保留推理链（谁基于什么论点
// 得出什么结论）。
//
// 设计约束：
//   - 用的是"裸客户端"（Gateway 构造时拿到的 inner），不是 Gateway 自己 ——
//     避免 Gateway → compressor → Gateway 的递归调用。
//   - 低温 + 短 max_tokens + 短超时：摘要不是主链路，不能拖慢庭审。
//   - 任何失败都由 BuildAbstractiveSummary 兜底回退 extractive。

// summaryGenerator 是 SummaryGenerator 的默认实现。
type summaryGenerator struct {
	client llm.Client
	model  string
}

// NewSummaryGenerator 用给定的 LLM 客户端构造 abstractive 摘要生成器。
// client 为 nil 时返回 nil（调用方据此跳过 abstractive 路径）。
func NewSummaryGenerator(client llm.Client, model string) SummaryGenerator {
	if client == nil {
		return nil
	}
	return &summaryGenerator{client: client, model: model}
}

// abstractiveSummaryMaxInputMsgs 限制喂给摘要模型的丢弃消息条数。
// 压缩场景下被丢的消息可能很多，全喂进去等于把省下的 token 又花回去。
const abstractiveSummaryMaxInputMsgs = 20

// abstractiveSummaryMaxMsgChars 单条消息在摘要输入里的截断长度。
const abstractiveSummaryMaxMsgChars = 800

const abstractiveSummarySystemPrompt = `你是庭审记录的摘要器。你会收到若干条因 token 预算被裁剪掉的庭审发言。

请把它们压缩成一段简明中文摘要，要求：
1. 保留推理链——谁（检察官/辩护律师/法官/书记员）基于什么论点或证据得出什么结论；
2. 不要只写结论，必须带上支撑结论的依据；
3. 只依据给定内容，绝对不要补充、推断或编造任何未出现的事实、数字或证据编号；
4. 输出纯文本段落，不要 markdown 标题，不要列表符号，不超过 300 字。`

// GenerateSummary 调用轻量 LLM 生成 abstractive 摘要。
func (g *summaryGenerator) GenerateSummary(ctx context.Context, messages []llm.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// 从最近的消息往回取（越近的信息越相关），再翻回时间正序。
	start := 0
	if len(messages) > abstractiveSummaryMaxInputMsgs {
		start = len(messages) - abstractiveSummaryMaxInputMsgs
	}
	window := messages[start:]

	var b strings.Builder
	for _, m := range window {
		role := m.Role
		if t, ok := m.Metadata["agent_type"]; ok && t != "" {
			role = t
		}
		content := strings.TrimSpace(m.Content)
		if len(content) > abstractiveSummaryMaxMsgChars {
			content = content[:abstractiveSummaryMaxMsgChars] + "..."
		}
		fmt.Fprintf(&b, "[%s] %s\n", role, content)
	}

	content, _, err := g.client.Complete(
		ctx,
		abstractiveSummarySystemPrompt,
		[]llm.Message{{Role: "user", Content: b.String()}},
		llm.CompletionOptions{
			Model:       g.model,
			Temperature: 0.3, // 低温：摘要是收敛任务，不需要发挥
			MaxTokens:   400,
			JSONMode:    false, // 要的是自然语言段落，不是 JSON
		},
	)
	if err != nil {
		return "", fmt.Errorf("abstractive summary: %w", err)
	}
	return content, nil
}

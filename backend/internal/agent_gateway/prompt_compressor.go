package agent_gateway

import (
	"github.com/decisioncourt/backend/internal/llm"
)

// PromptCompressor 在 token 预算紧张时压缩历史上下文。MVP 策略简单：
//   - system 消息永远保留在最前；
//   - 其余消息只保留最近的 5 条；
//   - 单条消息内容超过 3000 字符时截断到 1500 并加标记。
// 这样可以避免递归引入更多 LLM 调用来做摘要。
const (
	compressKeepHistory = 5
	compressMaxMsgLen   = 3000
	compressTargetLen   = 1500
	compressTruncateMark = "...（已压缩）"
)

// CompressionInfo 记录压缩前后统计，供文件日志分析。
type CompressionInfo struct {
	Applied      bool
	BeforeCount  int
	AfterCount   int
	BeforeLength int
	AfterLength  int
	DroppedCount int
}

// PromptCompressor 是无状态压缩器。
type PromptCompressor struct{}

// NewPromptCompressor 构造压缩器。
func NewPromptCompressor() *PromptCompressor { return &PromptCompressor{} }

// Compress 根据预算状态返回压缩后的消息列表。若状态不是 compress /
// throttle / exhausted，则原样返回。
func (pc *PromptCompressor) Compress(messages []llm.Message, bs BudgetSnapshot) ([]llm.Message, CompressionInfo) {
	info := CompressionInfo{BeforeCount: len(messages)}
	if len(messages) == 0 {
		return nil, info
	}
	if bs.Status != StatusCompress && bs.Status != StatusThrottle && bs.Status != StatusExhausted {
		return messages, info
	}

	info.Applied = true

	var system *llm.Message
	nonSystem := make([]llm.Message, 0, len(messages))
	for _, m := range messages {
		info.BeforeLength += len(m.Content)
		if m.Role == "system" && system == nil {
			cp := m
			system = &cp
		} else {
			nonSystem = append(nonSystem, m)
		}
	}

	if len(nonSystem) > compressKeepHistory {
		info.DroppedCount = len(nonSystem) - compressKeepHistory
		nonSystem = nonSystem[len(nonSystem)-compressKeepHistory:]
	}

	out := make([]llm.Message, 0, len(nonSystem)+1)
	if system != nil {
		out = append(out, *system)
	}
	out = append(out, nonSystem...)
	info.AfterCount = len(out)

	for i := range out {
		if len(out[i].Content) > compressMaxMsgLen {
			keep := compressTargetLen - len(compressTruncateMark)
			if keep < 0 {
				keep = 0
			}
			out[i].Content = out[i].Content[:keep] + compressTruncateMark
		}
		info.AfterLength += len(out[i].Content)
	}

	return out, info
}

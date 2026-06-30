package agent_gateway

import (
	"math"

	"github.com/decisioncourt/backend/internal/llm"
)

// Throttler 在 token 预算接近上限时降低 max_tokens 与 temperature，
// 既省 token 又减少 LLM 发散。
//
// 策略：
//   - 触发状态：compress / throttle / exhausted
//   - temperature 固定降到 0.2
//   - max_tokens 按比例压缩：remaining = 1 - ratio；目标 = max(100, int(max_tokens * remaining * 2))
//   - 完全耗尽 (ratio >= 1) 时直接降到 100
//   - 豁免任务类型（verdict / final_decision / summary / assess）保留原 max_tokens：
//     这些是关键输出，截断会导致判决书 / 结案陈词 / 评估报告丢失，破坏业务
const (
	throttleMinMaxTokens = 100
)

// ThrottleExemptTaskTypes 是不会被 throttler 降低 max_tokens 的 task_type。
// 这些任务都是"必须给到完整输出"的关键产物。
var ThrottleExemptTaskTypes = map[string]bool{
	"verdict":           true, // 书记员结案陈词
	"final":             true, // 法官最终裁决
	"assess":            true, // 法官证据评估
	"summary":           true, // 书记员总结
	"evidence_eval":     true, // 证据评估
	"judge.final":       true,
	"judge.assess":      true,
	"clerk.verdict":     true,
	"clerk.summary":     true,
	"evidence.evaluate": true,
}

// ThrottleInfo 记录限流前后配置。
type ThrottleInfo struct {
	Applied         bool
	MaxTokensBefore int
	MaxTokensAfter  int
	TemperatureBefore float32
	Exempted        bool
	ExemptReason    string
}

// Throttler 是无状态限流器。
type Throttler struct{}

// NewThrottler 构造限流器。
func NewThrottler() *Throttler { return &Throttler{} }

// Apply 根据预算状态返回限流后的 CompletionOptions。
// taskType 命中豁免列表时保留原 max_tokens 不变，仅记录 Exempted=true。
func (th *Throttler) Apply(opts llm.CompletionOptions, bs BudgetSnapshot, taskType string) (llm.CompletionOptions, ThrottleInfo) {
	info := ThrottleInfo{
		MaxTokensBefore:   opts.MaxTokens,
		TemperatureBefore: opts.Temperature,
	}
	if bs.Status != StatusCompress && bs.Status != StatusThrottle && bs.Status != StatusExhausted {
		return opts, info
	}
	// 关键任务豁免：保留 max_tokens
	if ThrottleExemptTaskTypes[taskType] {
		info.Applied = false
		info.Exempted = true
		info.ExemptReason = "task_type=" + taskType + " is critical output"
		info.MaxTokensAfter = opts.MaxTokens
		// temperature 仍可降，鼓励确定性格式
		out := opts
		out.Temperature = 0.2
		return out, info
	}
	info.Applied = true

	out := opts
	out.Temperature = 0.2

	base := opts.MaxTokens
	if base <= 0 {
		base = 500 // 合理默认值
	}
	remaining := 1.0 - bs.Ratio
	if remaining < 0 {
		remaining = 0
	}
	scaled := int(math.Ceil(float64(base) * remaining * 2))
	if scaled < throttleMinMaxTokens {
		scaled = throttleMinMaxTokens
	}
	out.MaxTokens = scaled
	info.MaxTokensAfter = scaled
	return out, info
}

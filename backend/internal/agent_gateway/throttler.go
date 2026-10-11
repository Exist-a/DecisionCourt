package agent_gateway

import (
	"math"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/observability"
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
type Throttler struct {
	// v2.3 (ADR 0037) 注入 metrics；nil 时所有埋点 no-op
	metrics observability.Metrics
}

// NewThrottler 构造限流器。
// metrics 传 nil 时所有埋点 no-op（向后兼容）。
func NewThrottler(metrics observability.Metrics) *Throttler { return &Throttler{metrics: metrics} }

// Apply 根据预算状态返回限流后的 CompletionOptions。
// taskType 命中豁免列表时保留原 max_tokens 不变，仅记录 Exempted=true。
//
// v2.12 (M8-2 A2)：**JSON 输出同样豁免 max_tokens 压缩**（理由见下方注释）——
// 决策步输出必须能被 json.Unmarshal 解析，截断的 JSON 是不可用产物。
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
		return th.exempt(opts, info, taskType, "task_type="+taskType+" is critical output")
	}
	// v2.12 (M8-2 A2)：JSON 输出豁免。
	//
	// react_think / react_reflect 等决策步输出是一整份 JSON（action + reasoning +
	// content + evidence_refs …），而限流公式 `max(100, base*remaining*2)` 会把 500
	// 砍到 297/247/196/147/100 —— 100 token（中文 ≈60 字）几乎必然把 JSON 截断 →
	// json.Unmarshal 失败 → 触发 react_think_retry（同样被砍到 100）→ 再次失败则
	// return error 打断整轮。实测该场限流省下的输出额度 ~1.4k，疑似引发的重试
	// ~29k（占全场 14%），净负（docs/COMPRESSION-REDESIGN-PROPOSAL.md §3.1）。
	//
	// 截断的 JSON 是**不可用**产物，与"关键输出"同类：保留 max_tokens，仅降 temperature。
	if opts.JSONMode {
		return th.exempt(opts, info, taskType, "json_output: truncated JSON is unusable")
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

	if th.metrics != nil {
		th.metrics.IncCounter(observability.MetricLLMThrottleAppliedTotal, map[string]string{"task_type": taskType})
	}
	return out, info
}

// exempt 实现"保留 max_tokens、仅降 temperature"的豁免路径（关键任务 / JSON 输出共用）。
func (th *Throttler) exempt(opts llm.CompletionOptions, info ThrottleInfo, taskType, reason string) (llm.CompletionOptions, ThrottleInfo) {
	info.Applied = false
	info.Exempted = true
	info.ExemptReason = reason
	info.MaxTokensAfter = opts.MaxTokens
	out := opts
	out.Temperature = 0.2
	if th.metrics != nil {
		th.metrics.IncCounter(observability.MetricLLMThrottleExemptedTotal, map[string]string{"task_type": taskType})
	}
	return out, info
}

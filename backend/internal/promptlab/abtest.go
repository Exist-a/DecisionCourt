package promptlab

import (
	"context"
	"fmt"
	"math"

	"github.com/decisioncourt/backend/internal/llm"
)

// ABTestMaxTrialOutputs 是单次 A/B Test 允许的最大 trial_outputs 长度。
//
// 设计理由:
//   - A/B Test 成本翻倍 (2 个 LLM 调用 per output),trial_outputs 越长越贵
//   - 实战场景:用户想比两个版本的 output,通常 3-5 条样本足够
//   - 上限 20 防止"误传 100 条把 LLM 配额烧光"
//   - REST 端点收到超长 trial_outputs 时返回 400 (不允许静默截断,
///    否则用户看到结果少以为是 bug)
const ABTestMaxTrialOutputs = 20

// ABTestMinConfidenceDiff 是 A/B Test 决胜的最小 mean score 差值。
// 差值 ≤ 此值时判 tie,避免 LLM 评分抖动导致赢家频繁切换。
// 0.1 是经验值,与 v0.10.25 novelty judge 同等级阈值。
const ABTestMinConfidenceDiff = 0.1

// ABTestWinner A/B Test 结果中的赢家标识。
// 严格三态 ("A" / "B" / "tie"),与 plan §2.5 ABTestResult.Winner 字段类型一致。
type ABTestWinner string

const (
	ABTestWinnerA   ABTestWinner = "A"
	ABTestWinnerB   ABTestWinner = "B"
	ABTestWinnerTie ABTestWinner = "tie"
)

// ABTestResult 是单次 A/B Test 的输出。
//
// 字段语义:
//   - VersionA / VersionB: 被比较的两个 prompt 版本标识 (semver-pr 字符串)
//   - Rule: 比较所用的 eval rule
//   - ScoresA / ScoresB: 每条 trial output 对应的 EvalResult (数组下标对齐)
//   - MeanA / MeanB: ScoresA / ScoresB 的平均 score,便于前端快速展示
//   - Winner: "A" / "B" / "tie" (差距 < ABTestMinConfidenceDiff → tie)
//   - Confidence: |MeanA - MeanB|,0-1,前端可显示 "A 领先 0.23" 这类文案
//   - Reasoning: LLM judge 生成的简要总结 (≤ 80 字),确定性规则留空
type ABTestResult struct {
	VersionA   string         `json:"version_a"`
	VersionB   string         `json:"version_b"`
	Rule       EvalRule       `json:"rule"`
	ScoresA    []EvalResult   `json:"scores_a"`
	ScoresB    []EvalResult   `json:"scores_b"`
	MeanA      float64        `json:"mean_a"`
	MeanB      float64        `json:"mean_b"`
	Winner     ABTestWinner   `json:"winner"`
	Confidence float64        `json:"confidence"`
	Reasoning  string         `json:"reasoning"`
}

// RunABTest 对同一组 trial outputs,分别用 versionA / versionB 各跑一次 eval rule,
// 比较两个版本的平均 score,决出 winner (差距不足 → tie)。
//
// 行为要点:
//   - len(trialOutputs) == 0: 返回 error (无样本无法决出 winner)
//   - len(trialOutputs) > ABTestMaxTrialOutputs: 返回 error (REST 端点应已校验,这里是防御性兜底)
//   - rule 是确定性规则 (EvalRuleLength): 每条 output 算 2 次 Eval,versionA / versionB
//     走相同的确定性逻辑,理论上 MeanA == MeanB → tie。这是个 degenerate 场景,
//     不报错,返回 tie 让用户明白 "A/B 在确定性规则上无差异"
//   - 任何一条 Eval 失败: 整次 A/B Test 失败,error 含底层信息
//     (不静默跳过,防止"5 条 output 其中 1 条 judge LLM 挂了但用户不知道")
//
// versionA / versionB 是 semver-pr 字符串 (例如 "1.0.3-pr1" / "1.0.4-pr1"),
// 仅作为标识参与结果输出,不参与实际 LLM 调用 (Eval 只关心 rule + output)。
// 当前 v1.0.3 PR-B2 不实现"按版本切换 prompt 跑 trial"的逻辑
// (需要先有"prompt 版本 → LLM 调用"的全链路接线,是 v1.0.x 后续讨论项)。
// 本 PR 限定: 输入 trial_outputs 是已经按两个版本各跑一次 trial 后的输出文本,
// AB Test 只做"评分比较" 不做"重新跑 LLM"。
func RunABTest(
	ctx context.Context,
	llmClient llm.Client,
	versionA, versionB string,
	rule EvalRule,
	trialOutputs []string,
) (*ABTestResult, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("promptlab.RunABTest: llm client is nil")
	}
	if !IsBuiltinRule(string(rule)) {
		return nil, fmt.Errorf("promptlab.RunABTest: unknown rule %q", rule)
	}
	if len(trialOutputs) == 0 {
		return nil, fmt.Errorf("promptlab.RunABTest: trial_outputs is empty")
	}
	if len(trialOutputs) > ABTestMaxTrialOutputs {
		return nil, fmt.Errorf("promptlab.RunABTest: trial_outputs length %d exceeds max %d",
			len(trialOutputs), ABTestMaxTrialOutputs)
	}

	result := &ABTestResult{
		VersionA: versionA,
		VersionB: versionB,
		Rule:     rule,
		ScoresA:  make([]EvalResult, 0, len(trialOutputs)),
		ScoresB:  make([]EvalResult, 0, len(trialOutputs)),
	}

	// 对每条 trial output,A/B 两个版本各 eval 一次。
	// 当前 v1.0.3 PR-B2 不区分 version 的 prompt 内容,两边用同一份 output 跑同一条 rule,
	// 评分结果理论上一致 → tie。这是设计妥协:v1.0.3 PR-B2 只交付"评分比较" 框架,
	// "重新跑 LLM 拿 A/B 输出"的接线留到后续 PR (需要 prompt 版本 → LLM 调用路由)。
	for _, output := range trialOutputs {
		evalA, err := Eval(ctx, llmClient, rule, output)
		if err != nil {
			return nil, fmt.Errorf("promptlab.RunABTest: eval version_a output: %w", err)
		}
		result.ScoresA = append(result.ScoresA, evalA)

		evalB, err := Eval(ctx, llmClient, rule, output)
		if err != nil {
			return nil, fmt.Errorf("promptlab.RunABTest: eval version_b output: %w", err)
		}
		result.ScoresB = append(result.ScoresB, evalB)
	}

	result.MeanA = meanScore(result.ScoresA)
	result.MeanB = meanScore(result.ScoresB)

	diff := result.MeanA - result.MeanB
	switch {
	case math.Abs(diff) < ABTestMinConfidenceDiff:
		// 差距不足 → tie (避免 LLM 评分抖动导致赢家频繁切换)
		result.Winner = ABTestWinnerTie
	case diff > 0:
		result.Winner = ABTestWinnerA
	default:
		result.Winner = ABTestWinnerB
	}
	result.Confidence = math.Abs(diff)
	result.Reasoning = buildABReasoning(result)

	return result, nil
}

// meanScore 计算一组 EvalResult 的平均 score。
// 空数组返回 0,避免 panic (调用方已保证非空,但保留防御)。
func meanScore(results []EvalResult) float64 {
	if len(results) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range results {
		sum += r.Score
	}
	return sum / float64(len(results))
}

// buildABReasoning 生成 A/B Test 结果的可读总结。
// 确定性规则 (EvalRuleLength) 时不调 LLM,直接用 mean score 描述;
// LLM 规则时 (TODO) 未来可让 LLM judge 生成一段总结。当前 v1.0.3 PR-B2
// 不做 LLM 总结 (成本高 + 当前 trial_outputs 通常 ≤ 5, 数字足以说明问题),
// 仅输出结构化数字摘要。
func buildABReasoning(r *ABTestResult) string {
	return fmt.Sprintf(
		"rule=%s mean_a=%.2f mean_b=%.2f winner=%s confidence=%.2f",
		r.Rule, r.MeanA, r.MeanB, r.Winner, r.Confidence,
	)
}

package agent

import (
	"math"

	"github.com/decisioncourt/backend/internal/belief"
)

// ClampProbability 硬编码 clamp 任何 LLM 返回的"概率值"到 [0, 1] 范围。
//
// 背景：v0.8.3 实装发现 DeepSeek 在 JudgeAssess / JudgeFinalDecision /
// GenerateVerdict（clerk 角色）三个 prompt 都会偶尔把 0-1 范围的小数
// 误输出为 0-100 范围的整数（如 35.0 / 65.0）—— 推测是 prompt 里
// "对选项 A 的支持度：40%" 字符串被 LLM 误解为 0-100 范围。
// 这直接污染：
//   - agents.belief_a（被 JudgeAssess 写库）
//   - verdicts.option_a_score / option_b_score（被 GenerateVerdict 写库）
// 进而让 verdict 页显示 3500 分、ArgumentMap strokeWidth 算出 172.5px。
//
// 硬编码语义：
//   - 写库前**无条件** clamp，不做"看起来合理就放过"的判断
//   - 不 fallback 到 judge.BeliefA 兜底 —— 那会让 DB 已有脏数据循环污染
//   - 所有 LLM 输出值（包括 0、1、负数、NaN、Inf）都必经此函数
//
// Clamp 范围是 [0, 1] 而不是 [0.05, 0.95]（v0.6 engine 内部用的范围），
// 是因为 verdict.option_a_score 也走这里；verdict 分数允许 0 和 1 边界
// 表达"完全倾向"。
//
// NaN / Inf 守卫：belief.Clamp01 用 `v < lo` / `v > hi` 比较，NaN 的
// 比较结果恒为 false（IEEE 754 规范），所以 NaN 会直接穿透到调用方。
// Postgres 写 NaN 列会报 "invalid input syntax for type double
// precision"，所以必须在 Clamp01 之前先剥掉 NaN/Inf。
//
// 单值版本：只对单个值做 [0,1] 边界 clamp，不做归一化。用于不构成
// "概率分布对"的单值场景（实际项目里少用，绝大部分 LLM 输出是
// (a, b) pair 形式，应优先用 ClampProbabilityPair）。
func ClampProbability(v float64) float64 {
	return clampSingleToUnit(v)
}

// ClampProbabilityPair 硬编码 clamp + 归一化 (a, b) 一对概率值。
//
// 这是 v0.8.4 修复 v0.8.3 LLM 抽风问题的核心入口 —— DeepSeek 偶尔
// 把 0-1 范围小数输出为 0-100 范围整数（35.0 / 65.0），单值 clamp
// 会把它们都收窄到 1.0 / 1.0（"双方都完全确定"），**丢失 LLM 原本
// 表达的 35% / 65% 偏向**。
//
// 处理流程（顺序关键！）：
//  1. NaN/Inf 守卫（→ 0.0 / 1.0）—— **不** clamp 到 [0,1] 边界
//  2. 检测 0-100 范围：如果 a + b > 1.5 视为 0-100，整体除以 100
//     阈值 1.5 选得安全：合法的 0-1 范围内 a + b 最大 2.0（0.99+0.99
//     边缘），远小于 1.5 + 误差；35/65 / 100/100 / 70/80 都会触发。
//  3. Clamp 到 [0, 1]（含 0-100 检测后才做 —— 否则 35 会先变 1.0
//     然后再被错误地 /100 = 0.01，丢失"35% 偏向"信息）
//  4. 归一化到 sum=1（v0.6 belief_a + belief_b = 1 的业务约束）
//
// 兜底：a = b = 0 时给 0.5/0.5（中立），避免 0/0 写 DB。
func ClampProbabilityPair(a, b float64) (float64, float64) {
	// Step 1: NaN/Inf 守卫（用 sanitize 不要直接 clamp —— 否则 35 会变 1.0）
	a = sanitizeNaNInf(a)
	b = sanitizeNaNInf(b)

	// Step 2: 0-100 范围检测（在 clamp 之前！35/65 → 0.35/0.65 才对）
	if a+b > 1.5 {
		a = a / 100
		b = b / 100
	}

	// Step 3: Clamp 到 [0, 1]（兜底 0-100 检测漏过或极端 case）
	a = belief.Clamp01(a, 0, 1)
	b = belief.Clamp01(b, 0, 1)

	// Step 4: 归一化到 sum=1（v0.6 业务约束：belief_a + belief_b = 1）
	sum := a + b
	if sum > 0 {
		return a / sum, b / sum
	}
	// 全 0 兜底：给中立 0.5/0.5（不要让 0/0 写 DB）
	return 0.5, 0.5
}

// sanitizeNaNInf 把 NaN/Inf 收窄到合法值，但**不**做 [0,1] 边界 clamp。
// 与 clampSingleToUnit 区分：clampSingleToUnit 用于单值入口直接 clamp，
// sanitizeNaNInf 用于 pair 入口先剥 NaN/Inf 再走 0-100 检测。
func sanitizeNaNInf(v float64) float64 {
	if math.IsNaN(v) {
		return 0.0
	}
	if math.IsInf(v, 1) {
		return 1.0
	}
	if math.IsInf(v, -1) {
		return 0.0
	}
	return v
}

func clampSingleToUnit(v float64) float64 {
	if math.IsNaN(v) {
		return 0.0
	}
	if math.IsInf(v, 1) {
		return 1.0
	}
	if math.IsInf(v, -1) {
		return 0.0
	}
	return belief.Clamp01(v, 0, 1)
}

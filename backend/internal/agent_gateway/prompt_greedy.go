package agent_gateway

import "sort"

// EstimateTokens 粗估一段文本的 token 数（v2.10 ADR 0044 #5）。
//
// 关键：必须按字符类别区分，否则退化成"字符数"——纯 chars/N 与旧的字符数预算
// 完全同构（线性缩放不改变 GreedyPack 的相对比较），无法修复本条问题。
//
//   - CJK 字符（含中文/日文/韩文）：约 1.5 token/char
//   - ASCII 及其它：约 0.25 token/char
//
// 这样代码块（ASCII 密集）不再"看起来占空间"而挤掉同等字符数的中文推理文本。
// 精度足够用于组间相对排序，不是上游真实 token 数。
func EstimateTokens(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if r >= 0x2E80 { // CJK 部首扩展及更宽的字符区
			cjk++
		} else {
			other++
		}
	}
	return cjk*3/2 + other/4
}

// GreedyPack 给定原子组清单 + 预算快照，按 GroupScore 降序贪心填入。
//
// v2.10 ADR 0044 #5: "填入"的目标改为 token 数 = totalTokens × keepRatio，
// 其中 keepRatio 由 bs.Status 决定（与压缩档位匹配）。
//
// v2.10 ADR 0044 #2: scoreThreshold 过滤——低于阈值的组在贪心循环前被排除。
//
// 注：原子组不可拆。GreedyPack 是逐组决策的——整组放进 / 整组丢；不会发生
// "组内部分进部分出"。
//
// 返回：
//
//	keepIdx        所有"被保留"的消息索引（按 groups 排序后的顺序，非原序）
//	keptGroupCount 最终被保留的组数（独立消息组按 1 记）
//	recentForced   占位 0（recent 强制保留在 CompressScored 单独实现）
func GreedyPack(groups []AtomicGroup, bs BudgetSnapshot, scoreThreshold float64) (keepIdx []int, keptGroupCount int, recentForced int) {
	sorted := make([]AtomicGroup, len(groups))
	copy(sorted, groups)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GroupScore > sorted[j].GroupScore
	})

	// v2.10 ADR 0044 #2: 预过滤——低于阈值的组直接排除
	if scoreThreshold > 0 {
		var filtered []AtomicGroup
		for _, g := range sorted {
			if g.GroupScore >= scoreThreshold {
				filtered = append(filtered, g)
			}
		}
		sorted = filtered
	}

	// v2.10 ADR 0044 #5: 用内容感知的 token 估算替代字符数做预算单位
	totalTokens := 0
	for _, g := range sorted {
		totalTokens += g.tokens()
	}
	if totalTokens == 0 {
		// 内容全空，按"全保留"处理 — 不浪费一次保留配额
		for _, g := range sorted {
			keepIdx = append(keepIdx, g.Indices...)
			keptGroupCount++
		}
		return keepIdx, keptGroupCount, 0
	}

	keepRatio := keepRatioForStatus(bs.Status)
	target := int(float64(totalTokens) * keepRatio)

	accum := 0
	for _, g := range sorted {
		gTokens := g.tokens()
		// 已保留至少 1 个组，再加就会越界 → 停止。
		// 第一个组总是被强制保留（不让 scored 列表为空）。
		if keptGroupCount > 0 && accum+gTokens > target {
			break
		}
		accum += gTokens
		keepIdx = append(keepIdx, g.Indices...)
		keptGroupCount++
	}
	return keepIdx, keptGroupCount, 0
}

// keepRatioForStatus 把预算状态映射到保留比例。
// Compress 温和；Exhausted 最激。
func keepRatioForStatus(status string) float64 {
	switch status {
	case StatusCompress:
		return 0.7
	case StatusThrottle:
		return 0.4
	case StatusExhausted:
		return 0.2
	}
	return 1.0
}

package agent_gateway

import "sort"

// GreedyPack 给定原子组清单 + 预算快照，按 GroupScore 降序贪心填入。
//
// "填入"的目标字节数 = totalLength × keepRatio，其中 keepRatio 由 bs.Status
// 决定（与压缩档位匹配）：
//
//	StatusCompress   → 保留 70% 字节
//	StatusThrottle   → 保留 40% 字节
//	StatusExhausted  → 保留 20% 字节
//
// 与"按 token 数截断"相比，按 ratio 截断在压缩档切换时行为更稳定，不依赖
// 字符数估算（估算错误会让输出飘）。
//
// 注：原子组不可拆。GreedyPack 是逐组决策的——整组放进 / 整组丢；不会发生
// "组内部分进部分出"。
//
// 返回：
//   keepIdx        所有"被保留"的消息索引（按 groups 排序后的顺序，非原序）
//   keptGroupCount 最终被保留的组数（独立消息组按 1 记）
//   recentForced   占位 0（recent 强制保留在 CompressScored 单独实现）
func GreedyPack(groups []AtomicGroup, bs BudgetSnapshot) (keepIdx []int, keptGroupCount int, recentForced int) {
	sorted := make([]AtomicGroup, len(groups))
	copy(sorted, groups)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GroupScore > sorted[j].GroupScore
	})

	totalBytes := 0
	for _, g := range sorted {
		totalBytes += g.GroupLength
	}
	if totalBytes == 0 {
		// 内容全空，按"全保留"处理 — 不浪费一次保留配额
		for _, g := range sorted {
			keepIdx = append(keepIdx, g.Indices...)
			keptGroupCount++
		}
		return keepIdx, keptGroupCount, 0
	}

	keepRatio := keepRatioForStatus(bs.Status)
	target := int(float64(totalBytes) * keepRatio)

	accum := 0
	for _, g := range sorted {
		// 已保留至少 1 个组，再加就会越界 → 停止。
		// 第一个组总是被强制保留（不让 scored 列表为空）。
		if keptGroupCount > 0 && accum+g.GroupLength > target {
			break
		}
		accum += g.GroupLength
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

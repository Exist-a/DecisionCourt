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
// v2 扩展了 Strategy / AtomicGroups / AtomicGroupsKept / RecentForcedKept /
// SummarizedBlocks 五个字段；老策略（legacy / keep-5）一律 0 或空。
type CompressionInfo struct {
	Applied              bool
	BeforeCount          int
	AfterCount           int
	BeforeLength         int
	AfterLength          int
	DroppedCount         int
	Strategy             string // "legacy" | "scored"
	AtomicGroups         int
	AtomicGroupsKept     int
	RecentForcedKept     int
	SummarizedBlocks     int
	ScoreThreshold       float64
	ScoreAvgBefore       float64
	ScoreAvgAfter        float64
}

// PromptCompressor 是无状态压缩器，根据 cfg 选择 legacy / scored 策略。
type PromptCompressor struct {
	cfg SmartCompressionConfig
}

// SmartCompressionConfig 由 Gateway 注入；Compress 据此判断走哪条路径。
type SmartCompressionConfig struct {
	Enabled                bool
	KeepRecentForcedN      int
	SummaryInsertThreshold int
	ScoreThreshold         float64
}

// NewPromptCompressor 构造压缩器。
func NewPromptCompressor(cfg SmartCompressionConfig) *PromptCompressor {
	if cfg.KeepRecentForcedN <= 0 {
		cfg.KeepRecentForcedN = 3
	}
	if cfg.SummaryInsertThreshold <= 0 {
		cfg.SummaryInsertThreshold = 5
	}
	if cfg.ScoreThreshold <= 0 {
		cfg.ScoreThreshold = 0.3
	}
	return &PromptCompressor{cfg: cfg}
}

// Compress 根据预算状态与 cfg 选择策略：
//   - 状态 normal → 不压缩，原样返回
//   - cfg.Enabled == false → legacy（保留 system + 最近 5 条）
//   - cfg.Enabled == true  → scored（三阶段管道：评分 / 原子组 / 贪心打包）
func (pc *PromptCompressor) Compress(messages []llm.Message, bs BudgetSnapshot) ([]llm.Message, CompressionInfo) {
	info := CompressionInfo{BeforeCount: len(messages)}
	if len(messages) == 0 {
		return nil, info
	}
	if bs.Status != StatusCompress && bs.Status != StatusThrottle && bs.Status != StatusExhausted {
		return messages, info
	}
	info.Applied = true

	if pc.cfg.Enabled {
		return CompressScored(messages, bs, pc.cfg, info)
	}
	return CompressLegacy(messages, info)
}

// CompressLegacy 保留 v0.5+ 的 "system + 最近 5 条 + 超长截断" 行为；
// 行为不变以保证向后兼容。
func CompressLegacy(messages []llm.Message, info CompressionInfo) ([]llm.Message, CompressionInfo) {
	info.Strategy = "legacy"
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

// CompressScored 走"评分 + 原子组 + 贪心打包 + 兜底摘要"四阶段。
// 详细规则参见 .trae/documents/prompt-compression-courtscenario.md。
//
// 对 system 消息的处理：始终强制保留（与 legacy 一致）；非 system 才进评分。
func CompressScored(messages []llm.Message, bs BudgetSnapshot, cfg SmartCompressionConfig, info CompressionInfo) ([]llm.Message, CompressionInfo) {
	info.Strategy = "scored"
	for _, m := range messages {
		info.BeforeLength += len(m.Content)
	}

	// 分离 system / 非 system
	var system []llm.Message
	nonSystem := make([]llm.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			system = append(system, m)
		} else {
			nonSystem = append(nonSystem, m)
		}
	}

	if len(nonSystem) == 0 {
		out := append([]llm.Message{}, system...)
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
		info.AfterCount = len(out)
		return out, info
	}

	// Stage 1: 评分
	scored := ScoreMessages(nonSystem, bs)

	// Stage 2: 原子组识别
	groups := BuildAtomicGroups(nonSystem, scored)

	// Stage 3: 贪心打包（recentForced 由 CompressScored 自己计算，避免双重计数）
	keepSet, keptGroupCount, _ := GreedyPack(groups, bs)

	// 强制保留最近 N 条（不论分数）
	keepMap := map[int]bool{}
	for _, idx := range keepSet {
		keepMap[idx] = true
	}
	var recentForced int
	for i := len(nonSystem) - cfg.KeepRecentForcedN; i < len(nonSystem); i++ {
		if i < 0 {
			continue
		}
		if !keepMap[i] {
			keepMap[i] = true
			recentForced++
		}
	}

	// 按原顺序输出
	var kept []llm.Message
	var droppedCount int
	for i, m := range nonSystem {
		if keepMap[i] {
			kept = append(kept, m)
		} else {
			droppedCount++
		}
	}
	info.DroppedCount = droppedCount
	info.RecentForcedKept = recentForced
	info.AtomicGroups = len(groups)
	info.AtomicGroupsKept = keptGroupCount

	// 兜底摘要
	if droppedCount > cfg.SummaryInsertThreshold {
		summary := BuildEarlierSummary(groups, keepMap, nonSystem)
		if summary != "" {
			// 插到非 system 区段的最前面（保留 system 在最前）
			summaryMsg := llm.Message{
				Role:    "system",
				Content: summary,
			}
			kept = append([]llm.Message{summaryMsg}, kept...)
			info.SummarizedBlocks = 1
		}
	}

	// 单条超长截断（保留 legacy 的最后一步）
	out := make([]llm.Message, 0, len(kept)+len(system))
	out = append(out, system...)
	out = append(out, kept...)
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
	info.AfterCount = len(out)

	// 评分均值
	var sumB, sumA float64
	for _, s := range scored {
		sumB += s.Score
	}
	if len(scored) > 0 {
		info.ScoreAvgBefore = sumB / float64(len(scored))
	}
	// After avg 拿 keepMap 中的
	keptCount := 0
	for _, s := range scored {
		if keepMap[s.Index] {
			sumA += s.Score
			keptCount++
		}
	}
	if keptCount > 0 {
		info.ScoreAvgAfter = sumA / float64(keptCount)
	}
	info.ScoreThreshold = cfg.ScoreThreshold

	return out, info
}

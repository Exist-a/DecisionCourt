package agent_gateway

import (
	"time"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/observability"
)

// cutBytesRuneSafe 取 s 的前 n 字节，保证结果是合法 UTF-8。
//
// 若 n 恰好落在某个多字节 rune 中间，则回退到该 rune 的起始边界。字节预算
// 语义保持不变（与 compressMaxMsgLen / compressTargetLen 一致），只是不再
// 切出半个汉字 —— 旧实现用裸 `s[:n]`，而 system prompt 几乎全中文，切口
// 大概率落在字符中间，产出的非法 UTF-8 片段会被塞进 LLM 请求。
func cutBytesRuneSafe(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	// 回退到最近的 rune 起始字节。
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// msgTruncateLimits 返回该消息适用的（触发上限, 截断目标）字节数。
//
// system 消息单独一套宽松上限：它承载 agent 指令 + 工具说明 + 庭审历史，
// 用普通对话内容的 3000/1500 会把模型变成失忆状态（见文件顶部常量注释）。
func msgTruncateLimits(m llm.Message) (limit, target int) {
	if m.Role == "system" {
		return compressMaxSystemMsgLen, compressSystemTargetLen
	}
	return compressMaxMsgLen, compressTargetLen
}

// truncateOversizedMessages 就地截断超长消息，返回截断后的总字节数。
//
// 这是压缩管道的最后一步（原实现在 legacy / scored 早返回 / scored 主路径
// 各抄了一份）。抽出来一是消重，二是保证三条路径对 system 的处理一致 ——
// v2.10 之前的 bug 正是三处都无条件截断 system。
func truncateOversizedMessages(msgs []llm.Message) int {
	total := 0
	for i := range msgs {
		limit, target := msgTruncateLimits(msgs[i])
		if len(msgs[i].Content) > limit {
			keep := target - len(compressTruncateMark)
			msgs[i].Content = cutBytesRuneSafe(msgs[i].Content, keep) + compressTruncateMark
		}
		total += len(msgs[i].Content)
	}
	return total
}

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

	// v2.10 修复：system 消息装的是 agent 指令（baseRules）+ 工具说明 + 庭审历史，
	// 不是普通对话内容 —— 见 react_runner.go buildInitialMessages，整个 messages
	// 数组往往就只有这一条 system。此前它和普通消息共用 compressMaxMsgLen(3000)，
	// 被砍到 1500 字节：实测 12304→1500 / 13883→1500（87~89% 指令被销毁），
	// 前 1482 字节只剩 baseRules 开头，工具说明与全部庭审历史丢失 —— 模型因此
	// 不知道庭上有哪些证据，转而编造「证据7/证据12」并被反幻觉校验打回
	// （ADR 0015/0021）。故 system 单独用一条宽松上限：
	// 24000 字节 ≈ 12k token，为实测最大值(13883)的 1.7 倍，正常庭审不受影响，
	// 仅对长期庭审的历史膨胀兜底。
	compressMaxSystemMsgLen = 24000
	compressSystemTargetLen = 20000
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
	cfg     SmartCompressionConfig
	// v2.3 (ADR 0037) 注入 metrics；nil 时所有埋点 no-op
	metrics observability.Metrics
}

// SmartCompressionConfig 由 Gateway 注入；Compress 据此判断走哪条路径。
type SmartCompressionConfig struct {
	Enabled                bool
	KeepRecentForcedN      int
	SummaryInsertThreshold int
	ScoreThreshold         float64
	// v2.10 ADR 0044 #6: abstractive 摘要（opt-in）。
	// AbstractiveSummary 为 false 或 SummaryGen 为 nil 时保持 extractive 行为。
	// 这里把 generator 放在 config 上（而非 CompressScored 的额外入参），是为了
	// 不改动压缩管道的函数签名 —— 既有 5 个单测与 eval 基线都直接调
	// CompressScored，签名一变全要跟着改，而这不是本条问题要解决的问题。
	AbstractiveSummary bool
	SummaryGen         SummaryGenerator
}

// NewPromptCompressor 构造压缩器。
// metrics 传 nil 时所有埋点 no-op（向后兼容）。
func NewPromptCompressor(cfg SmartCompressionConfig, metrics observability.Metrics) *PromptCompressor {
	if cfg.KeepRecentForcedN <= 0 {
		cfg.KeepRecentForcedN = 3
	}
	if cfg.SummaryInsertThreshold <= 0 {
		cfg.SummaryInsertThreshold = 5
	}
	if cfg.ScoreThreshold <= 0 {
		cfg.ScoreThreshold = 0.3
	}
	return &PromptCompressor{cfg: cfg, metrics: metrics}
}

// Compress 根据预算状态与 cfg 选择策略：
//   - 状态 normal → 不压缩，原样返回
//   - cfg.Enabled == false → legacy（保留 system + 最近 5 条）
//   - cfg.Enabled == true  → scored（三阶段管道：评分 / 原子组 / 贪心打包）
func (pc *PromptCompressor) Compress(messages []llm.Message, bs BudgetSnapshot) ([]llm.Message, CompressionInfo) {
	start := time.Now()
	info := CompressionInfo{BeforeCount: len(messages)}
	if len(messages) == 0 {
		return nil, info
	}
	if bs.Status != StatusCompress && bs.Status != StatusThrottle && bs.Status != StatusExhausted {
		// v2.3 (ADR 0037) 跳过路径也埋点，便于计算触发率。
		if pc.metrics != nil {
			pc.metrics.IncCounter(observability.MetricCompressionApplied, map[string]string{"status": "skipped_normal"})
		}
		return messages, info
	}
	info.Applied = true

	var out []llm.Message
	if pc.cfg.Enabled {
		out, info = CompressScored(messages, bs, pc.cfg, info)
	} else {
		out, info = CompressLegacy(messages, info)
	}

	// v2.3 (ADR 0037) 压缩完成后埋点：strategy / ratio / dropped / duration / summary_inserted。
	if pc.metrics != nil {
		dur := time.Since(start).Seconds()
		labels := map[string]string{
			"strategy": info.Strategy,
			"trigger":  string(bs.Status),
		}
		pc.metrics.IncCounter(observability.MetricCompressionApplied, labels)
		pc.metrics.ObserveHistogram(observability.MetricPromptCompressionDuration, labels, dur)
		if info.BeforeLength > 0 {
			ratio := float64(info.AfterLength) / float64(info.BeforeLength)
			pc.metrics.ObserveHistogram(observability.MetricPromptCompressionRatio, labels, ratio)
		}
		if info.SummarizedBlocks > 0 {
			pc.metrics.IncCounter(observability.MetricPromptCompressionSummaryTotal, labels)
		}
	}
	return out, info
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
	info.AfterLength = truncateOversizedMessages(out)
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
		info.AfterLength = truncateOversizedMessages(out)
		info.AfterCount = len(out)
		return out, info
	}

	// Stage 1: 评分
	scored := ScoreMessages(nonSystem, bs)

	// Stage 2: 原子组识别
	groups := BuildAtomicGroups(nonSystem, scored)

	// Stage 3: 贪心打包（recentForced 由 CompressScored 自己计算，避免双重计数）
	// v2.10 ADR 0044 #2: 传入 ScoreThreshold 激活死配置
	keepSet, keptGroupCount, _ := GreedyPack(groups, bs, cfg.ScoreThreshold)

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
		// v2.10 ADR 0044 #6: opt-in abstractive 摘要 —— 在 extractive 锚点基础上
		// 再补一段保留推理链的自然语言摘要；失败自动回退到 extractive 结果。
		if cfg.AbstractiveSummary && cfg.SummaryGen != nil {
			dropped := CollectDroppedMessages(groups, keepMap, nonSystem)
			summary = BuildAbstractiveSummary(cfg.SummaryGen, dropped, summary)
		}
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

	// 单条超长截断（保留 legacy 的最后一步；system 走单独上限）
	out := make([]llm.Message, 0, len(kept)+len(system))
	out = append(out, system...)
	out = append(out, kept...)
	info.AfterLength = truncateOversizedMessages(out)
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

package agent_gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// BuildEarlierSummary 在被丢消息数量超过 SummaryInsertThreshold 时调用，
// 用一句简洁的提示让模型知道"前面跳过了若干轮的简介"。
//
// 业内替代方案是用 LLM 摘要（递归 + 成本），与 Berkeley arXiv 2407.08892 结论
// 不符（extractive 优于 abstractive）。当前实现取折中：
//   - 不调用 LLM
//   - 把被丢弃的消息里"可信锚点"（提到 evidence_id / @prosecutor / @defender / agent_type）
//     列出来
//
// 如果没有任何锚点，返回 ""；调用方决定要不要插入。
//
// 参数 allMessages 是 nonSystem 段的完整切片（按原顺序）；buildEarlierSummary
// 只读这个切片，不持有引用。
func BuildEarlierSummary(groups []AtomicGroup, keepMap map[int]bool, allMessages []llm.Message) string {
	type anchor struct {
		idx     int
		preview string
	}
	var keptAnchors []anchor

	for _, g := range groups {
		for _, idx := range g.Indices {
			if keepMap[idx] {
				continue
			}
			if idx < 0 || idx >= len(allMessages) {
				continue
			}
			m := allMessages[idx]
			preview := summaryPreview(m)
			if preview != "" {
				keptAnchors = append(keptAnchors, anchor{idx: idx, preview: preview})
			}
		}
	}
	if len(keptAnchors) == 0 {
		return ""
	}
	// 限制条数，避免 summary 自身过长
	const maxAnchors = 6
	if len(keptAnchors) > maxAnchors {
		keptAnchors = keptAnchors[:maxAnchors]
	}
	var b strings.Builder
	b.WriteString("[earlier context omitted — key anchors preserved below]\n")
	for _, a := range keptAnchors {
		fmt.Fprintf(&b, "- %s\n", a.preview)
	}
	b.WriteString("(The above are anchor references; detailed turns were elided to fit budget.)")
	return b.String()
}

// summaryPreview 返回一句话预览；如果消息里同时有角色 metadata，使用它。
func summaryPreview(m llm.Message) string {
	role := m.Role
	if t, ok := m.Metadata["agent_type"]; ok && t != "" {
		role = t
	}
	content := strings.TrimSpace(m.Content)
	if len(content) > 120 {
		content = content[:120] + "..."
	}
	// 替换换行让 preview 尽量保持单行
	content = strings.ReplaceAll(content, "\n", " ")
	return fmt.Sprintf("[%s] %s", role, content)
}

// ============================================================================
// v2.10 ADR 0044 #6: abstractive summary（opt-in）
// ============================================================================

// SummaryGenerator 是 abstractive 摘要的抽象。Gateway 注入一个持有轻量 LLM
// 客户端的实现；压缩器本身不依赖 llm.Client，保持可单测。
//
// ctx 由调用方给出 —— 压缩管道当前是 ctx-free 的（Compress 不接 ctx），
// BuildAbstractiveSummary 因此传 context.Background() 并自带超时；
// 接口保留 ctx 参数是为了将来把 ctx 一路穿下来时不必改签名。
type SummaryGenerator interface {
	GenerateSummary(ctx context.Context, messages []llm.Message) (string, error)
}

// summaryGenerationTimeout 单次 abstractive 摘要调用的硬超时。
// 摘要只是"锦上添花"，绝不能拖慢主 LLM 调用链路。
const summaryGenerationTimeout = 15 * time.Second

// BuildAbstractiveSummary 生成 abstractive 摘要；任何一步失败都回退到
// extractiveSummary（通常就是 BuildEarlierSummary 的结果），保证压缩路径
// 不会因为摘要失败而中断或丢上下文。
//
// 参数 dropped 是被判定丢弃的消息（按原顺序）。gen 为 nil 或返回空串时
// 直接返回 fallback。
func BuildAbstractiveSummary(gen SummaryGenerator, dropped []llm.Message, fallback string) string {
	if gen == nil || len(dropped) == 0 {
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), summaryGenerationTimeout)
	defer cancel()

	out, err := gen.GenerateSummary(ctx, dropped)
	if err != nil {
		slog.Warn("agent_gateway: abstractive summary failed, falling back to extractive",
			"err", err, "dropped", len(dropped))
		return fallback
	}
	out = strings.TrimSpace(out)
	if out == "" {
		// 空输出也算失败路径：回退 extractive，否则会插入一条只有 header 的空摘要。
		slog.Warn("agent_gateway: abstractive summary returned empty, falling back",
			"dropped", len(dropped))
		return fallback
	}
	// 成功路径也留痕：该功能每次会真花一次 LLM 调用，静默成功会让运维
	// 无法区分"没触发"和"触发了但没生效"。
	slog.Info("agent_gateway: abstractive summary applied",
		"dropped_msgs", len(dropped), "summary_chars", len(out))
	return "[earlier context summarized — reasoning chain preserved]\n" + out
}

// CollectDroppedMessages 按原顺序收集被丢弃的消息，供摘要生成使用。
// 与 BuildEarlierSummary 共用同一套 keepMap 判定，保证"摘要覆盖的范围"
// 与"实际丢弃的范围"一致。
func CollectDroppedMessages(groups []AtomicGroup, keepMap map[int]bool, allMessages []llm.Message) []llm.Message {
	var dropped []llm.Message
	for _, g := range groups {
		for _, idx := range g.Indices {
			if keepMap[idx] {
				continue
			}
			if idx < 0 || idx >= len(allMessages) {
				continue
			}
			dropped = append(dropped, allMessages[idx])
		}
	}
	return dropped
}

package agent_gateway

import (
	"fmt"
	"strings"

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

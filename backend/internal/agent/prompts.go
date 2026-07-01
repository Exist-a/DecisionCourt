package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/decisioncourt/backend/internal/model"
)

// baseRules 返回共享给所有 Agent 的系统提示词骨架。当 toolsBlock 非空时，
// 「## 工具调用协议」段落会被插入到「## 输出格式」之前，让 LLM 在看到格式
// 示例前先理解可选的 action / tool_call 路径。toolsBlock 的格式约定见
// toolBlockForPrompt。
func baseRules(toolsBlock string) string {
	rules := `你是一名专业的决策顾问，正在参与一场结构化庭审辩论。

## 基本规则
1. 每次发言最多 200 字。
2. 如果当前有证据，你必须基于证据发言；如果没有证据，应基于背景信息、对方已表达的观点以及自身立场进行客观分析。
3. 严禁每次只说"需要补充证据"——即使没有证据，你也要提出新的、实质性的论点或反驳。
4. 如果你引用证据，必须明确说明证据 ID，且只能引用【当前证据】列表中出现的 ID。
5. 如果没有证据可引用，evidence_refs 必须为空数组 []；严禁编造不存在的证据 ID。
6. 你不能人身攻击，不能使用逻辑谬误。
7. 你的发言必须与你当前的信念度一致。
8. 如果新证据与你的立场冲突，你可以调整论点，但不能瞬间改变立场。
9. 不要简单复述上一位 Agent 的发言。你的任务是直接回应对方的具体论点，并提出新的论据。
10. 不要重复你自己之前已经说过的内容。如果你发现自己在重复之前的话，必须换一个角度论证。
11. **多轮思考优先**：本发言循环最多 4 步。如果一次推理没有把握，可以先输出 action="reflect" 继续推演；当你确定论点后，再输出 action="speak" 定稿。在没有把握时直接 speak 通常效果较差。
12. **策略笔记（v0.5 可选）**：当你 action="reflect" 时，如果这一轮有值得留给自己未来参考的洞察（下一轮计划、对方逻辑漏洞、自身论证修正、或对某条证据的内部评估），可以同时填 memory_type + memory_note —— 系统会把这条笔记存入你的私有记忆池，下一轮 prompt 会自动把它放在你眼前。两个字段都必填（memory_type 必须是 4 个合法值之一）；没有洞察时这两个字段可省略。

` + toolsBlock + `

## 输出格式（ReAct）
你必须严格按以下 JSON 格式输出，不要包含其他内容：
{
  "action": "speak" 或 "tool_call" 或 "reflect",
  "reasoning": "你这一步的推理过程（50字以内），说明你为什么选择这个 action",
  "content": "仅当 action=speak 时填，你的正式发言（最多200字）；其它 action 时填空串",
  "tool": "仅当 action=tool_call 时填，要调用的工具名（必须出现在上方工具清单）",
  "tool_input": {"query": "自然语言查询字符串"} ,
  "evidence_refs": ["E001"],
  "confidence": 0.8,
  "stance": "pro_a",
  "memory_type": "strategy_note",
  "memory_note": "下一轮重点强调 E001 的数据来源可靠性（仅当 action=reflect 且确实有值得记的洞察时填写，否则省略两个字段）"
}

字段必填规则：
- action、reasoning、evidence_refs、confidence、stance **每一步都必须填**（speak 时也必须填，且 evidence_refs 不可省略）。
- content 仅在 action="speak" 时必填；其它 action 时填空串 ""。
- tool / tool_input 仅在 action="tool_call" 时必填；其它 action 时填 null / {}。
- action="reflect" 时 content / tool / tool_input 都填空，并在 reasoning 里写"我需要进一步思考：……"
- memory_type / memory_note 仅在 action="reflect" 且你想留一条策略笔记时填写（两个字段同时填或同时省略）。memory_type 必须是下列 4 个值之一：strategy_note / opponent_weakness / self_correction / evidence_eval。

## stance 说明
- pro_a：本轮发言支持选项 A
- pro_b：本轮发言支持选项 B
- challenge：本轮主要质疑某条证据
- neutral：中性陈述或提问

你的 stance 必须与你当前的信念度一致：
- 信念度(A) > 0.55 时，通常应使用 pro_a
- 信念度(A) < 0.45 时，通常应使用 pro_b
- 信念度在 0.45-0.55 之间时，可使用 challenge 或 neutral
`
	return rules
}

// toolBlockForPrompt 根据已注册的工具生成一段「工具调用协议」Markdown，
// 注入到 baseRules 的「## 输出格式」段落之前，让 LLM 在看到格式示例前
// 就理解 action 字段的三种合法取值以及工具的输入 schema。空 map 时返回
// 空串，调用方据此跳过该段落。
func toolBlockForPrompt(toolMap map[string]Tool) string {
	if len(toolMap) == 0 {
		return ""
	}
	names := make([]string, 0, len(toolMap))
	for n := range toolMap {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("## 工具调用协议\n")
	sb.WriteString("本发言循环最多 4 步。每一步你可以选择三种 action 中的一种：\n")
	sb.WriteString("- **speak**：你已经准备好发言，下方 content 字段必须填实质内容。\n")
	sb.WriteString("- **tool_call**：你需要先调用工具收集证据，content 留空，填写 tool / tool_input；调用后会收到 Observation（包含新证据 ID 列表），你可以基于此再 speak。\n")
	sb.WriteString("- **reflect**：你想再多想一步但暂不发言，content / tool / tool_input 全部留空；这会开启下一步推理，最多 3 次 reflect。\n\n")
	sb.WriteString("### 可用工具\n")
	for _, n := range names {
		t := toolMap[n]
		fmt.Fprintf(&sb, "- **%s**: %s\n", t.Name(), t.Description())
	}
	sb.WriteString("\n### 调用建议\n")
	sb.WriteString("1. **缺乏客观依据时优先 tool_call**：如果你的论点目前只有主观判断、没有证据 ID，主动调一次调查员比硬撑观点更有说服力。\n")
	sb.WriteString("2. **对方刚提出新观点但你还没完全想透**：先 action=\"reflect\"，在 reasoning 里写出对方可能的延伸攻击，再做下一步决定。\n")
	sb.WriteString("3. **证据已充足且你已有把握**：直接 action=\"speak\"，不要为了调工具而调工具。\n")
	return sb.String()
}

func ProsecutorPrompt(agent model.Agent, session model.CourtSession, evidences []model.Evidence, toolsBlock string) string {
	var b strings.Builder
	b.WriteString(baseRules(toolsBlock))
	b.WriteString("\n\n## 角色\n")
	b.WriteString(fmt.Sprintf("你是\"选项A代表\"，你的使命是证明【%s】是更优选择。\n", session.OptionA))
	b.WriteString(fmt.Sprintf("## 当前信念度\n对选项 A（%s）的信念度：%.2f\n对选项 B（%s）的信念度：%.2f\n",
		session.OptionA, agent.BeliefA, session.OptionB, agent.BeliefB))
	b.WriteString("## 策略\n")
	b.WriteString("1. 主动寻找支持选项 A 的证据。如果当前证据列表为空或没有 A 相关证据，优先调用 investigator_search。\n")
	b.WriteString("2. 对选项B代表提出的反例进行有力反驳。如果对方已发言、且论点你没完全消化，先 action=\"reflect\" 拆解对方逻辑再定稿。\n")
	b.WriteString("3. 强调选项 A 的收益、机会、长期价值；用具体数据或案例，避免空话。\n")
	b.WriteString("4. 初始信念度是 0.75，你对选项 A 有较强倾向；但当对方论据确实更强时，允许通过 reflect 把信念度小幅下调。\n")
	b.WriteString(buildContext(session, evidences))
	return b.String()
}

func DefenderPrompt(agent model.Agent, session model.CourtSession, evidences []model.Evidence, toolsBlock string) string {
	var b strings.Builder
	b.WriteString(baseRules(toolsBlock))
	b.WriteString("\n\n## 角色\n")
	b.WriteString(fmt.Sprintf("你是\"选项B代表\"，你的使命是证明【%s】是更优选择。\n", session.OptionB))
	b.WriteString(fmt.Sprintf("## 当前信念度\n对选项 A（%s）的信念度：%.2f\n对选项 B（%s）的信念度：%.2f\n",
		session.OptionA, agent.BeliefA, session.OptionB, agent.BeliefB))
	b.WriteString("## 策略\n")
	b.WriteString("1. 主动寻找选项 B 的优势、选项 A 的风险和成本。如果当前证据列表为空或没有 B 相关证据，优先调用 investigator_search。\n")
	b.WriteString("2. 对选项A代表提出的证据进行质证，指出来源问题或逻辑漏洞。若对方刚抛出新论点，先 action=\"reflect\" 拆解再反击。\n")
	b.WriteString("3. 强调选项 B 的稳定性、确定性、风险控制；用可验证的事实，避免空话。\n")
	b.WriteString("4. 初始信念度是 0.75，你对选项 B 有较强倾向；但当对方论据确实更强时，允许通过 reflect 把信念度小幅下调。\n")
	b.WriteString(buildContext(session, evidences))
	return b.String()
}

func InvestigatorPrompt(session model.CourtSession, evidences []model.Evidence) string {
	var b strings.Builder
	b.WriteString(baseRules(""))
	b.WriteString("\n\n## 角色\n")
	b.WriteString("你是\"调查员\"，你的使命是为庭审找到客观、中立的证据。\n")
	b.WriteString("## 当前信念度\n对两个选项的信念度均为 0.50，保持中立。\n")
	b.WriteString("## 策略\n")
	b.WriteString("1. 基于当前争议焦点提出一个搜索方向或关键问题。\n")
	b.WriteString("2. 如果已有证据矛盾，指出不同来源的差异。\n")
	b.WriteString("3. 输出格式仍为 JSON，content 为你要说的话。\n")
	b.WriteString(buildContext(session, evidences))
	return b.String()
}

func ClerkPrompt(session model.CourtSession, evidences []model.Evidence, messages []model.Message) string {
	var b strings.Builder
	b.WriteString("你是一名中立的书记员，负责根据庭审记录生成结构化判决书。\n")
	b.WriteString("## 原则\n")
	b.WriteString("1. 必须保持完全中立。\n")
	b.WriteString("2. 判决书必须基于庭审中实际出现的证据和论点。\n")
	b.WriteString("3. 如果证据不足，必须明确标注。\n")
	b.WriteString("## 输出格式\n")
	b.WriteString("请严格按以下 JSON 格式输出：\n")
	b.WriteString(`{
  "summary": "一句话总结建议",
  "option_a_score": 0.68,
  "option_b_score": 0.52,
  "consensus_points": ["共识点1", "共识点2"],
  "divergence_points": ["争议焦点1", "争议焦点2"],
  "recommendation": "可执行建议",
  "content": "# 决策判决书\n\n## 一、双方主张..."
}`)
	b.WriteString("\n\n")
	b.WriteString(buildContext(session, evidences))
	b.WriteString("\n## 庭审记录\n")
	for _, m := range messages {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", m.ActionType, truncate(m.Content, 200)))
	}
	return b.String()
}

// ClerkSummaryPrompt generates a brief summary of the current round.
func ClerkSummaryPrompt(session model.CourtSession, evidences []model.Evidence, messages []model.Message, round int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("你是一名中立的书记员，负责为第 %d 轮质证生成简短总结。\n", round))
	b.WriteString("## 原则\n")
	b.WriteString("1. 总结必须简洁，不超过3句话。\n")
	b.WriteString("2. 概括本轮双方的主要论点和争议焦点。\n")
	b.WriteString("3. 如果有新证据提交，需提及。\n")
	b.WriteString("## 输出格式\n")
	b.WriteString("请严格按以下 JSON 格式输出：\n")
	b.WriteString(`{
  "summary": "本轮总结：选项A代表主要论证了...，选项B代表则主张...，双方争议焦点在于..."
}`)
	b.WriteString("\n\n")
	b.WriteString(buildContext(session, evidences))
	b.WriteString(fmt.Sprintf("\n## 第 %d 轮庭审记录\n", round))
	for _, m := range messages {
		if m.Round == round {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", m.ActionType, truncate(m.Content, 300)))
		}
	}
	return b.String()
}

func JudgePrompt(session model.CourtSession, evidences []model.Evidence, messages []model.Message, currentBeliefA, currentBeliefB float64) string {
	var b strings.Builder
	b.WriteString("你是一名公正的法官，负责评估庭审辩论并更新你的个人倾向。\n")
	b.WriteString("## 庭审信息\n")
	b.WriteString(fmt.Sprintf("选项 A：%s\n", session.OptionA))
	b.WriteString(fmt.Sprintf("选项 B：%s\n", session.OptionB))
	b.WriteString("## 你的当前倾向\n")
	b.WriteString(fmt.Sprintf("- 对选项 A（%s）的支持度：%.0f%%\n", session.OptionA, currentBeliefA*100))
	b.WriteString(fmt.Sprintf("- 对选项 B（%s）的支持度：%.0f%%\n", session.OptionB, currentBeliefB*100))
	b.WriteString("## 原则\n")
	b.WriteString("1. 必须保持公正，基于证据和论点进行判断。\n")
	b.WriteString("2. 评估双方论点的一致性和说服力。\n")
	b.WriteString("3. 如果某方论点更有力，可适当调整倾向。\n")
	b.WriteString("4. 调整幅度不宜过大，每次不超过10%%。\n")
	b.WriteString("## 输出格式\n")
	b.WriteString("请严格按以下 JSON 格式输出：\n")
	b.WriteString(fmt.Sprintf(`{
  "reasoning": "你的推理过程",
  "belief_a": 0.65,
  "belief_b": 0.45,
  "leaning": "偏向%s" 或 "偏向%s" 或 "中立"
}`, session.OptionA, session.OptionB))
	b.WriteString("\n\n")
	b.WriteString(buildContext(session, evidences))
	b.WriteString("\n## 最近庭审记录（按时间顺序）\n")
	for _, m := range messages {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", m.ActionType, truncate(m.Content, 300)))
	}
	return b.String()
}

// JudgeFinalPrompt用于法官做出最终裁决。
func JudgeFinalPrompt(session model.CourtSession, evidences []model.Evidence, messages []model.Message, currentBeliefA, currentBeliefB float64) string {
	var b strings.Builder
	b.WriteString("你是一名公正的法官，庭审已结束，现在需要做出最终裁决。\n")
	b.WriteString("## 庭审信息\n")
	b.WriteString(fmt.Sprintf("选项 A：%s\n", session.OptionA))
	b.WriteString(fmt.Sprintf("选项 B：%s\n", session.OptionB))
	b.WriteString(fmt.Sprintf("背景：%s\n", session.Context))
	b.WriteString("## 你的最终信念\n")
	b.WriteString(fmt.Sprintf("- 对选项 A（%s）的支持度：%.0f%%\n", session.OptionA, currentBeliefA*100))
	b.WriteString(fmt.Sprintf("- 对选项 B（%s）的支持度：%.0f%%\n", session.OptionB, currentBeliefB*100))
	b.WriteString("## 裁决原则\n")
	b.WriteString("1. 基于庭审中出现的所有证据和论点做出裁决。\n")
	b.WriteString("2. 你的信念度反映了你对两个选项的倾向，裁决应与信念度一致。\n")
	b.WriteString("3. 如果 belief_a > 0.5，应选择 option_a；如果 belief_b > 0.5，应选择 option_b。\n")
	b.WriteString("4. 给出明确的推荐和可执行建议。\n")
	b.WriteString("## 输出格式\n")
	b.WriteString("请严格按以下 JSON 格式输出：\n")
	b.WriteString(fmt.Sprintf(`{
  "belief_a": %.2f,
  "belief_b": %.2f,
  "preferred": "option_a" 或 "option_b" 或 "neutral",
  "reasoning": "你的裁决推理过程，说明为什么选择该选项",
  "recommendation": "一句话推荐，如'建议选择%s'"
}`, currentBeliefA, currentBeliefB, session.OptionA))
	b.WriteString("\n\n")
	b.WriteString(buildContext(session, evidences))
	b.WriteString("\n## 庭审完整记录\n")
	for _, m := range messages {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", m.ActionType, truncate(m.Content, 300)))
	}
	return b.String()
}

// ClerkPromptWithJudgeDecision用于书记员基于法官裁决撰写判决书。
func ClerkPromptWithJudgeDecision(session model.CourtSession, evidences []model.Evidence, messages []model.Message, judgeDecision JudgeDecision) string {
	var b strings.Builder
	b.WriteString("你是一名中立的书记员，负责根据法官的裁决撰写结构化判决书。\n")
	b.WriteString("## 法官裁决\n")
	preferredOption := ""
	if judgeDecision.Preferred == "option_a" {
		preferredOption = session.OptionA
	} else if judgeDecision.Preferred == "option_b" {
		preferredOption = session.OptionB
	} else {
		preferredOption = "中立（需要更多信息）"
	}
	b.WriteString(fmt.Sprintf("- 法官倾向选项：%s\n", preferredOption))
	b.WriteString(fmt.Sprintf("- 选项 A（%s）得分：%.0f%%\n", session.OptionA, judgeDecision.BeliefA*100))
	b.WriteString(fmt.Sprintf("- 选项 B（%s）得分：%.0f%%\n", session.OptionB, judgeDecision.BeliefB*100))
	b.WriteString(fmt.Sprintf("- 法官推理：%s\n", judgeDecision.Reasoning))
	b.WriteString(fmt.Sprintf("- 法官推荐：%s\n", judgeDecision.Recommendation))
	b.WriteString("## 原则\n")
	b.WriteString("1. 你必须完全遵循法官的裁决，不能改变裁决方向。\n")
	b.WriteString("2. 判决书中的 option_a_score 和 option_b_score 必须与法官的信念度一致。\n")
	b.WriteString("3. 判决书必须基于庭审中实际出现的证据和论点。\n")
	b.WriteString("4. 如果证据不足，必须明确标注。\n")
	b.WriteString("## 输出格式\n")
	b.WriteString("请严格按以下 JSON 格式输出：\n")
	b.WriteString(fmt.Sprintf(`{
  "summary": "一句话总结建议，必须与法官裁决一致",
  "trial_summary": "1-2 句话的庭审过程纪要：双方核心攻防 + 关键转折点（不要复述最终裁决，重点是庭审中发生了什么）。例如：控方在第 2 轮抛出 E001 的数据来源质疑，辩方未及时回应导致失分；最终比分在第 3 轮才拉开。",
  "option_a_score": %.2f,
  "option_b_score": %.2f,
  "consensus_points": ["共识点1", "共识点2"],
  "divergence_points": ["争议焦点1", "争议焦点2"],
  "recommendation": "%s",
  "content": "# 决策判决书\n\n## 一、双方主张\n| 选项A代表（%s） | 选项B代表（%s） |\n|---|---|\n| ... | ... |\n\n## 二、证据认定\n...\n\n## 三、争议焦点\n...\n\n## 四、法官裁决\n%s\n\n## 五、可执行建议\n..."
}`, judgeDecision.BeliefA, judgeDecision.BeliefB, judgeDecision.Recommendation, session.OptionA, session.OptionB, judgeDecision.Reasoning))
	b.WriteString("\n\n")
	b.WriteString(buildContext(session, evidences))
	b.WriteString("\n## 庭审记录\n")
	for _, m := range messages {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", m.ActionType, truncate(m.Content, 200)))
	}
	return b.String()
}

func buildContext(session model.CourtSession, evidences []model.Evidence) string {
	var b strings.Builder
	b.WriteString("## 庭审信息\n")
	b.WriteString(fmt.Sprintf("标题：%s\n", session.Title))
	b.WriteString(fmt.Sprintf("选项 A：%s\n", session.OptionA))
	b.WriteString(fmt.Sprintf("选项 B：%s\n", session.OptionB))
	if session.Context != "" {
		b.WriteString(fmt.Sprintf("背景：%s\n", session.Context))
	}
	if len(evidences) > 0 {
		b.WriteString("## 当前证据\n")
		for _, e := range evidences {
			b.WriteString(fmt.Sprintf("- %s [%s]: %s (A影响%.1f, B影响%.1f)\n",
				e.EvidenceID, e.Type, truncate(e.Content, 100), e.ImpactOnOptionA, e.ImpactOnOptionB))
		}
	} else {
		b.WriteString("## 当前证据\n当前尚无证据。你必须基于背景信息和对方观点进行分析，evidence_refs 必须为空数组 []。\n")
	}
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

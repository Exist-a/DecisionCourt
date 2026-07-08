package agent

// v0.9.4 (HA-001) 调查发现 vs 用户证据混淆测试。
//
// 用户反馈的幻觉场景:
//   "agent 会将调查员搜索的信息当成用户本身提交的证据"
//
// 复现路径(代码反推):
//
//  1. 用户提交 user evidence E001="但是我还有工作" / E002="有点累,医生让我休息"。
//  2. Prosecutor 轮次:
//       iter 1: action=tool_call tool=investigator_search query="2024 劳动法 工作时长"
//       searcher 跑搜 → 写 investigation_finding 行 + 发 A2A report (public)
//       Observation: "搜索完成: finding_id=<uuid>。摘要=[1] 劳动法第三条规定 8 小时..."
//       iter 2: action=speak content="依据劳动法第三条...(E001 显示...)"
//               evidence_refs=["E001"]
//
//     ↑ content 套用了 search 内容,但 evidence_refs 套用了真实用户证据 ID。
//     在用户眼里 agent 像是说"用户告诉过我 8 小时工作时长"(假)。
//
// 根因分析 (本测试写于 v0.9.4):
//   - buildContext 只读 Evidence 表;不读 InvestigationFinding。
//   - InvestigationFinding 摘要只在 ReAct Runner 的 Observation 字段出现;
//     system prompt 里没有"调查发现"section 分离,所以 LLM 视觉上看不到
//     "这条调查发现 ≠ 用户证据"。
//   - baseRules 第 13/14 条(ADR 0015)只禁止"凭空编造"和"虚构对方论点",
//     没禁止"借尸还魂"——套用真实 E00X ID + 引用真实 finding 内容。
//
// 本测试作为修复 spec,修复前应该全 FAIL,修复后转 GREEN。

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
)

// T1:baseRules 必须显式禁止「把 ReAct Observation 中的调查发现内容
// 套到 evidence_refs 中的 E00X ID 上」。当前 baseRules 没有。
func TestBaseRules_ForbidsSourcingFindingAsEvidence(t *testing.T) {
	t.Parallel()

	rules := baseRules("")

	// 关键短语:baseRules 必须同时出现 "调查" + "不能/严禁/不得" + "evidence/证据"
	hasInvestigation := strings.Contains(rules, "调查")
	hasNegation := strings.Contains(rules, "不能") ||
		strings.Contains(rules, "严禁") ||
		strings.Contains(rules, "不得")
	hasEvidenceTerm := strings.Contains(rules, "evidence") ||
		strings.Contains(rules, "证据")

	if !(hasInvestigation && hasNegation && hasEvidenceTerm) {
		t.Errorf(
			"baseRules 缺少禁止「把调查发现内容当作 evidence ID 来源」的规则。"+
				"三个条件: hasInvestigation=%v hasNegation=%v hasEvidenceTerm=%v\n"+
				"应当追加一条 v0.9.4 (HA-001) 规则,例如:\n"+
				"「严禁把 ReAct Observation (调查发现) 的内容套到 evidence_refs 中的 E00X 上。"+
				"引用调查发现必须用『调查员发现...』『调研显示...』,绝不能写成" +
				"『E00X 显示...』『依据 E00X ...[search 内容]』」\n"+
				"当前 rules 前 600 字符: %q",
			hasInvestigation, hasNegation, hasEvidenceTerm,
			truncate(rules, 600),
		)
	}
}

// T2:baseRules 必须给出反例引导,让 LLM 见到 "E00X 显示 [search 内容]" 这种
// 模板立刻识别为禁止 pattern。当前 ADR 0015 给的是日期/案号反例,没有 evidence
// 借用反例。
func TestBaseRules_ExplicitRefusalPattern(t *testing.T) {
	t.Parallel()

	rules := baseRules("")

	// 反例模板:必须出现 "E001 显示" 或类似「把 evidence ID 当 search 内容出处」
	// 的反面示例 + "调查" 同时出现,LLM 才能识别 forbidden pattern。
	probeForbidden := strings.Contains(rules, "E00") && strings.Contains(rules, "显示")
	hasInvestigation := strings.Contains(rules, "调查")

	if !(probeForbidden && hasInvestigation) {
		t.Errorf("baseRules 缺少数值反例引导。「E00X 显示」+「调查」须同时出现, "+
			"让 LLM 见到类似模式即识别为 forbidden。当前规则前 800 字符: %q",
			truncate(rules, 800))
	}
}

// T3:buildContext(## 当前证据) 不得出现 investigation_finding 内容。当前实现
// 行为正确,但作为 regression 保护。
func TestBuildContext_NotInflatedWithFindings(t *testing.T) {
	t.Parallel()

	session := model.CourtSession{Title: "t", OptionA: "A", OptionB: "B"}
	evidences := []model.Evidence{
		{EvidenceID: "E001", Source: "user", CredibilityScore: 0.85, Content: "但是我还有工作"},
	}
	probe := "劳动法第三条规定 8 小时工作时长"

	got := buildContext(session, evidences)
	if strings.Contains(got, probe) {
		t.Errorf("buildContext 混入调查发现内容,违反 user/evidence vs search/finding 分离。" +
			"应走独立「调查活动」section,不要塞到「当前证据」")
	}
	// 反向断言:必须有 "当前证据" 标题,保证现有渲染位置正确
	if !strings.Contains(got, "当前证据") {
		t.Errorf("buildContext 缺「当前证据」section header — 现行版本有这标题," +
			"如果重构丢了,需要补回来")
	}
}

// T4:完整 prosecutorSpeakReAct() 系统 prompt(把 InvestigationFinding 视图
// 也注入后)必须包含独立的 "调查活动" / "调查发现" section;且该 section 不得
// 出现 E00X 字面,以避免 LLM 把 finding_id 和 evidence_id 视觉混淆。
//
// 当前实现完全没有该 section,所以这个测试会 FAIL(regression 保护)。
func TestProsecutorPrompt_ShouldHaveFindingSection_WhenFindingsExist(t *testing.T) {
	t.Parallel()

	session := model.CourtSession{
		Title: "t", OptionA: "选项 A", OptionB: "选项 B",
	}
	evidences := []model.Evidence{
		{EvidenceID: "E001", Source: "user", CredibilityScore: 0.85, Content: "但是我还有工作"},
	}

	// 模拟 evidence + 一个 finding 的可视化结构(具体的"工具 Observation"层 Prompt
	// 在当前实现路径里只通过 ReAct 临时 user message 出现,system prompt 部分需要
	// 后续 PR 注入一个 "## 调查发现" section 把 finding 内容显式渲染)。
	// 我们这里简化:直接断言"有 findings section header(占位也可以),且 E00X 不能
	// 在 findings 区域出现"。
	prompt := ProsecutorPrompt(model.Agent{BeliefA: 0.75}, session, evidences, "")

	// 当前位置:prompt 没有独立 finding section;只读得到 ## 当前证据 区块
	hasFindingsHeader := strings.Contains(prompt, "调查发现") ||
		strings.Contains(prompt, "调查活动") ||
		strings.Contains(prompt, "InvestigationFinding")

	if !hasFindingsHeader {
		t.Errorf("ProsecutorPrompt 缺少独立的「调查发现」/「调查活动」section。" +
			"LLM 在 system prompt 视觉上看不到调查发现和用户证据的视觉分隔。" +
			"需要新加 buildInvestigationContext(session, findings) 帮助函数并在 " +
			"ProsecutorPrompt / DefenderPrompt 里调用(与 buildContext 并列)。\n" +
			"当前 prompt 的 ## 标题清单: %s", extractHeaders(prompt))
	}

	// 即便修复加了 section,该 section 内部也不能出现 E00X 字面(防止 LLM
	// 视觉混淆 finding_id 和 evidence_id)。
	if hasFindingsHeader {
		idx := indexAny(prompt, []string{"调查发现", "调查活动", "InvestigationFinding"})
		if idx >= 0 {
			rest := prompt[idx:]
			end := strings.Index(rest, "\n## ")
			if end < 0 {
				end = len(rest)
			}
			findingSection := rest[:end]
			if strings.Contains(findingSection, "E00") {
				t.Errorf("「调查发现」section 不能出现 E00X 字面,会诱导 LLM 混用 finding_id 和 evidence_id。"+
					"\n  section 内容 preview: %q", truncate(findingSection, 200))
			}
		}
	}
}

// extractHeaders 抽 ## / ### 标题,便于诊断。
func extractHeaders(s string) string {
	var hs []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "## ") {
			hs = append(hs, strings.TrimPrefix(line, "## "))
		}
	}
	return strings.Join(hs, " | ")
}

// indexAny 找任一子串首次出现位置,无则 -1。
func indexAny(s string, subs []string) int {
	for _, sub := range subs {
		if i := strings.Index(s, sub); i >= 0 {
			return i
		}
	}
	return -1
}

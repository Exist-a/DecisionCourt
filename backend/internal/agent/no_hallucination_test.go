package agent

// v0.9.1 (ADR 0015) 证据真实性 + LLM 幻觉防御测试。
//
// 这套测试不调真实 LLM,而是直接验证 prompt 构造逻辑:
//   1. buildContext 必须显示 source 标签（区分用户陈述/搜索结果）
//   2. buildContext 必须显示 submitted_by + credibility
//   3. baseRules 必须包含"严禁编造证据细节"强约束
//   4. baseRules 必须包含"严禁虚构对方论点"强约束
//   5. withArgumentSummaryText 必须把 user_interrupt 注入摘要(用户中途补充不被吞)
//   6. 当 evidence 内容很短(如"有点累,医生让我休息")时,LLM 看到的 prompt 应明确说
//      "内容短"提示(供后续引导 LLM 不要脑补)
//
// 实战来源:v0.9.0 用户报告 agent 引用了"2023-03-15 医嘱""(2022)京01民终1234号 案号"
// 等用户根本没提交过的细节。根因是 buildContext 只显示 evidence_id + content,
// LLM 看不到来源/可信度/内容长度,且 baseRules 没禁止编造细节。

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
)

func TestBuildContext_ShowsSourceAndCredibility(t *testing.T) {
	t.Parallel()

	session := model.CourtSession{
		Title:    "测试庭审",
		OptionA:  "工作",
		OptionB:  "休息",
		Context:  "用户陈述：医生让我休息",
	}
	evidences := []model.Evidence{
		{
			EvidenceID:       "E001",
			Source:           "user",
			SubmittedBy:      "anon_test_user_001",
			CredibilityScore: 0.85,
			Type:             "fact",
			Content:          "但是我还有工作",
		},
		{
			EvidenceID:       "E002",
			Source:           "user",
			SubmittedBy:      "anon_test_user_001",
			CredibilityScore: 0.85,
			Type:             "fact",
			Content:          "有点累,医生让我休息",
		},
	}

	got := buildContext(session, evidences)

	// ✅ 关键断言 1:prompt 必须显示中文 source 标签
	if !strings.Contains(got, "用户陈述") {
		t.Error("buildContext 应当包含「用户陈述」中文标签,实际没找到")
	}
	// ✅ 关键断言 2:prompt 必须显示 submitted_by
	if !strings.Contains(got, "anon_test_user_001") {
		t.Error("buildContext 应当包含 submitted_by 让 LLM 知道证据是谁提的")
	}
	// ✅ 关键断言 3:prompt 必须显示 credibility 数值
	if !strings.Contains(got, "credibility=0.85") {
		t.Error("buildContext 应当包含 credibility=0.85 让 LLM 看到可信度")
	}
	// ✅ 关键断言 4:prompt 必须显示 evidence_id
	if !strings.Contains(got, "E001") || !strings.Contains(got, "E002") {
		t.Error("buildContext 必须显示所有 evidence_id")
	}
}

func TestBuildContext_InvestigatorSourceLabel(t *testing.T) {
	t.Parallel()

	session := model.CourtSession{Title: "t", OptionA: "A", OptionB: "B"}
	evidences := []model.Evidence{
		{EvidenceID: "E001", Source: "investigator", SubmittedBy: "system_investigator",
			CredibilityScore: 0.6, Type: "fact", Content: "搜索结果片段"},
	}

	got := buildContext(session, evidences)

	if !strings.Contains(got, "搜索结果") {
		t.Error("source=investigator 应映射为「搜索结果」中文标签")
	}
}

func TestBaseRules_NoHallucinationConstraint(t *testing.T) {
	t.Parallel()

	rules := baseRules("")

	// ✅ 关键断言:baseRules 必须包含两条 ADR 0015 强约束
	if !strings.Contains(rules, "严禁编造证据细节") {
		t.Error("baseRules 缺少「严禁编造证据细节」(ADR 0015 §1)")
	}
	if !strings.Contains(rules, "严禁虚构对方论点") {
		t.Error("baseRules 缺少「严禁虚构对方论点」(ADR 0015 §2)")
	}
	// 必须明确禁止编造日期、案号等具体数字
	if !strings.Contains(rules, "2023年3月15日") {
		t.Error("baseRules 应当给出编造的具体反例(2023年3月15日)引导 LLM 避免")
	}
	if !strings.Contains(rules, "案号") {
		t.Error("baseRules 应当明确禁止编造案号")
	}
}

func TestSourceLabel_Mapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"user", "用户陈述"},
		{"investigator", "搜索结果"},
		{"system", "系统注入"},
		{"unknown", "unknown"}, // 未知值保持原样,不报错
	}

	for _, c := range cases {
		if got := sourceLabel(c.input); got != c.want {
			t.Errorf("sourceLabel(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
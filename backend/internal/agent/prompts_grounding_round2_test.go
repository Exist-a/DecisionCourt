package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

// 2026-10-10 Round 2「接地素材管道」护栏测试。
//
// 背景（docs/AGENT-GROUNDING-ANALYSIS.md）：此前 (a) 截断按**字节**语义、
// 中文可见量少 3 倍且会切出半个汉字；(b) 证据正文只给 200 字节≈66 字
// （提交上限却是 4096 字符）→ 模型看不到细节；(c) 调查发现是占位符，从不
// 进 prompt。这些测试把修好的契约钉住，防止回归。

// truncate 必须是 rune 语义：中文按字符计数，且绝不产出非法 UTF-8。
func TestTruncate_RuneBased_NoMojibake(t *testing.T) {
	s := strings.Repeat("中", 100) // 300 字节 / 100 rune

	got := truncate(s, 50)
	require.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8（字节语义会切出半个汉字）")
	require.Equal(t, 53, utf8.RuneCountInString(got), "50 rune + \"...\"；字节语义下这里只有约 16 个汉字")

	require.Equal(t, s, truncate(s, 100), "≤ max 不截断")
	require.Equal(t, s, truncate(s, -1), "max<0 表示不截断")
}

// 证据正文必须给足全文（在提交上限 4096 字符内不截断）。
func TestProsecutorPrompt_EvidenceContentGivenInFull(t *testing.T) {
	content := strings.Repeat("证据内容", 80) + "｜尾部标记XYZ" // 320 rune + 标记（远超旧的 200 字节≈66 字）
	evidences := []model.Evidence{{
		EvidenceID:       "E001",
		Type:             "document",
		Source:           "user",
		SubmittedBy:      "u1",
		Content:          content,
		CredibilityScore: 0.85,
	}}
	session := model.CourtSession{Title: "标题", OptionA: "A", OptionB: "B", SessionUUID: "sess-1"}

	p, err := ProsecutorPrompt(model.Agent{BeliefA: 0.75}, session, evidences, "", nil)
	require.NoError(t, err)
	require.True(t, utf8.ValidString(p))
	require.Contains(t, p, "尾部标记XYZ",
		"证据正文应给足全文；旧的 200 字节截断会让尾部标记不可见")
	require.Contains(t, p, content, "320 rune 的证据正文应完整进入 prompt")
}

// 调查发现必须真正渲染（不再是 "[由 ... 注入]" 占位符）。
func TestBuildInvestigationContext_RendersFindings(t *testing.T) {
	findings := []investigation.Finding{
		{Investigator: "investigator", Query: "行业平均薪资", Summary: "调查发现一：行业平均月薪 12000", ResultCount: 5},
		{Investigator: "investigator", Query: "裁员概率", Summary: "调查发现二：裁员概率 15%", ResultCount: 3},
	}
	got := buildInvestigationContext(model.CourtSession{SessionUUID: "sess-9"}, findings)

	require.Contains(t, got, "## 调查活动")
	require.Contains(t, got, "共 2 条")
	require.Contains(t, got, "行业平均月薪 12000")
	require.Contains(t, got, "裁员概率 15%")
	require.NotContains(t, got, "由 ProsecutorSpeak / DefenderSpeak 在调用时通过",
		"占位符必须消失，改为真实 finding 列表")
}

// 无 findings 时仍输出 section header（视觉一致性），并明确说明"尚无"。
func TestBuildInvestigationContext_EmptyWithoutFindings(t *testing.T) {
	got := buildInvestigationContext(model.CourtSession{SessionUUID: "s"}, nil)
	require.Contains(t, got, "## 调查活动")
	require.Contains(t, got, "尚无调查发现")
}

// ProsecutorPrompt / DefenderPrompt 必须把 findings 带进最终提示词。
func TestLawyerPrompts_InjectInvestigationFindings(t *testing.T) {
	findings := []investigation.Finding{
		{Investigator: "investigator", Query: "q", Summary: "发现内容ABC", ResultCount: 1},
	}
	session := model.CourtSession{Title: "t", OptionA: "A", OptionB: "B", SessionUUID: "sess-2"}
	agent := model.Agent{BeliefA: 0.75, BeliefB: 0.25}

	pro, err := ProsecutorPrompt(agent, session, nil, "", findings)
	require.NoError(t, err)
	require.Contains(t, pro, "发现内容ABC")

	def, err := DefenderPrompt(agent, session, nil, "", findings)
	require.NoError(t, err)
	require.Contains(t, def, "发现内容ABC")
}

// 对手最新发言应完整可见（旧实现按 150 字节≈50 字截断；发言本身上限 300 rune）。
func TestWithArgumentSummaryText_OpponentLatestFullyVisible(t *testing.T) {
	long := strings.Repeat("证", 300) // 900 字节 / 300 rune
	messages := []model.Message{
		{ActionType: "speak", Content: "本方上一轮发言"},
		{ActionType: "speak", Content: long},
	}

	got := withArgumentSummaryText(messages, model.AgentProsecutor, model.AgentDefender)
	require.Contains(t, got, long, "对手最新发言应完整可见，不再被 150 字节截断")
}

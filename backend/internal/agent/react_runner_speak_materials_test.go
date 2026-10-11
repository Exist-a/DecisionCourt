package agent

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

// TestReActRunner_SpeakStreamCarriesCaseMaterials 钉住「发言步全量带」这一行为：
// 发言步（streamSpeakContent）的系统提示必须包含由 RunnerConfig.SpeakMaterials
// 注入的案件素材，同时保留「只输出一行 JSON」的强约束。
//
// 注入式回归（docs/AGENT-GROUNDING-ANALYSIS.md §8 决策第 3 条）：往素材里塞一个
// 明确标记，断言它确实出现在发言步的 prompt 里 —— 否则下次有人"顺手改回去"
// （重新让发言步 fresh、不喂素材）不会被发现。
func TestReActRunner_SpeakStreamCarriesCaseMaterials(t *testing.T) {
	const marker = "MATERIAL-MARKER-E001-月薪8000元的唯一细节"
	decision := `{"action":"speak","reasoning":"控方主张 A 更优","content":"","stance":"pro_a","confidence":0.85,"evidence_refs":[]}`
	llmClient := &streamingScriptedLLM{
		completeScript: []string{decision},
		streamChunks:   []string{`{"content":"基于素材，选项 A 更稳健。"}`},
	}

	r := NewReActRunner(llmClient, "role-system", nil, RunnerConfig{
		MaxIterations:  2,
		SpeakMaterials: "## 案件素材\n" + marker + "\n",
	})
	// 流式路径只在 OnSpeakChunk 非 nil 时启用（见 Run 的 ActionSpeak 分支）。
	r.cfg.OnSpeakChunk = func(_, _ string) {}

	_, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)

	require.Len(t, llmClient.streamSysPrompts, 1, "发言步应恰好流式生成一次")
	sys := llmClient.streamSysPrompts[0]
	require.Contains(t, sys, marker,
		"发言步 prompt 必须包含 SpeakMaterials 注入的案件素材")
	require.Contains(t, sys, `{"content":"<完整发言文本>"}`,
		"必须保留『只输出一行 JSON』的强约束（防止 ReAct action JSON 污染输出格式）")
	require.NotContains(t, sys, "忽略之前的对话历史",
		"不应再声明『忽略对话历史』——发言步现在要吃素材")
}

// TestReActRunner_SpeakStreamWithoutMaterials_BackwardCompatible 确认
// SpeakMaterials 为空时退化为旧的最小 prompt（旧调用方 / 单测不受影响）。
func TestReActRunner_SpeakStreamWithoutMaterials_BackwardCompatible(t *testing.T) {
	decision := `{"action":"speak","reasoning":"R","content":"","stance":"pro_a","confidence":0.5,"evidence_refs":[]}`
	llmClient := &streamingScriptedLLM{
		completeScript: []string{decision},
		streamChunks:   []string{`{"content":"发言"}`},
	}
	r := NewReActRunner(llmClient, "sys", nil, RunnerConfig{MaxIterations: 2})
	r.cfg.OnSpeakChunk = func(_, _ string) {}

	_, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)

	require.Len(t, llmClient.streamSysPrompts, 1)
	sys := llmClient.streamSysPrompts[0]
	require.Contains(t, sys, `{"content":"<完整发言文本>"}`)
	require.NotContains(t, sys, "## 案件素材", "未提供素材时不应凭空出现素材区段")
}

// TestBuildSpeakMaterials_IncludesMaterials_NoMetadataLeak 覆盖素材装配本身：
// 证据正文 / 调查发现 / 庭审历史都要进素材；且庭审历史只走 ActionType+Content
// 白名单（Metadata 里的 reasoning 不得泄漏进发言步素材）。
func TestBuildSpeakMaterials_IncludesMaterials_NoMetadataLeak(t *testing.T) {
	session := model.CourtSession{
		Title:   "是否转向方案 A",
		OptionA: "方案 A",
		OptionB: "方案 B",
		Context: "背景说明文本",
	}
	evidences := []model.Evidence{{
		EvidenceID:       "E001",
		Source:           "user",
		SubmittedBy:      "user",
		CredibilityScore: 0.85,
		Type:             "document",
		Content:          "EVIDENCE-BODY-独家P75分位96万",
	}}
	findings := []investigation.Finding{{
		Investigator: "investigator",
		Query:        "行业融资数据",
		Summary:      "FINDING-SUMMARY-18个月内进入B轮比例41%",
		ResultCount:  3,
	}}
	messages := []model.Message{{
		ActionType: "speak",
		Content:    "TRANSCRIPT-BODY-对方主张B更稳",
		Metadata:   `{"reasoning":"SECRET-REASONING-不得泄漏","agent_type":"defender"}`,
	}}

	got, err := buildSpeakMaterials(session, evidences, findings, messages, model.AgentProsecutor, model.AgentDefender)
	require.NoError(t, err)

	require.Contains(t, got, "## 案件素材")
	require.Contains(t, got, "EVIDENCE-BODY-独家P75分位96万", "证据正文必须进素材")
	require.Contains(t, got, "FINDING-SUMMARY-18个月内进入B轮比例41%", "调查发现必须进素材")
	require.Contains(t, got, "TRANSCRIPT-BODY-对方主张B更稳", "庭审历史必须进素材")
	require.Contains(t, got, "## 庭审历史")
	require.NotContains(t, got, "SECRET-REASONING-不得泄漏",
		"庭审历史只能走 ActionType+Content 白名单，Metadata.reasoning 不得进入素材")
}

package agent

// v2.13 (deferred D24) 发言级幻觉度量测试。
//
// 背景: 反幻觉守卫(ValidateAgainstHallucination)对 speak 输出有效, 但质量
// 度量只覆盖判决书(MetricVerdictEvidenceAccuracy) → 发言幻觉率只能翻日志。
// 本次加 OnHallucination 观察者, 让上层 wire 成 metric。
//
// 覆盖:
//  1. 幻觉内容 → validateSpeak 触发 OnHallucination(mode 非空)
//  2. 干净内容 → 不触发
//  3. 未注入观察者(nil) → 不 panic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newSpeakRunner(observer func(mode, pattern string)) *ReActRunner {
	return NewReActRunner(nil, "sys", nil, RunnerConfig{
		OnHallucination: observer,
	})
}

func TestReActRunner_ValidateSpeak_ReportsHallucination(t *testing.T) {
	t.Parallel()

	var gotMode, gotPattern string
	reportCalls := 0
	r := newSpeakRunner(func(mode, pattern string) {
		reportCalls++
		gotMode = mode
		gotPattern = pattern
	})

	// evidence_refs 空 + 文本含"证据3"与百分比 → 必触发幻觉硬拒。
	out := &AgentOutput{
		Reasoning:    "因为证据3显示复合月增速达41%",
		Content:      "依据证据3，复合月增速达41%，我方主张成立。",
		Confidence:   0.6,
		Stance:       "pro_a",
		EvidenceRefs: nil,
	}
	err := r.validateSpeak(out)
	require.Error(t, err, "幻觉内容应被 validateSpeak 硬拒")
	require.Equal(t, 1, reportCalls, "OnHallucination 应恰好触发一次")
	require.NotEmpty(t, gotMode, "mode 应被上报(供 metric label)")
	require.NotEmpty(t, gotPattern, "pattern 应被上报(便于 debug)")
}

func TestReActRunner_ValidateSpeak_CleanContentNoReport(t *testing.T) {
	t.Parallel()

	reportCalls := 0
	r := newSpeakRunner(func(_, _ string) { reportCalls++ })

	// 无证据/案号/百分比/数字引用 → 干净, 不触发。
	// (注意: 不能带 evidence_refs —— allowedIDs 未知(测试传 nil)时非空 refs
	//  会被保守判为 evidence_ref_unverified 而拒绝。)
	out := &AgentOutput{
		Reasoning:  "基于双方陈述与常识判断",
		Content:    "我方主张该方案长期更稳健，恳请合议庭综合考量。",
		Confidence: 0.5,
		Stance:     "pro_a",
	}
	require.NoError(t, r.validateSpeak(out))
	require.Equal(t, 0, reportCalls, "干净内容不应触发 OnHallucination")
}

func TestReActRunner_ValidateSpeak_NilObserverSafe(t *testing.T) {
	t.Parallel()

	r := newSpeakRunner(nil) // 未注入观察者
	out := &AgentOutput{
		Reasoning:    "因为证据9显示增速88%",
		Content:      "依据证据9，增速88%。",
		Confidence:   0.5,
		Stance:       "pro_a",
		EvidenceRefs: nil,
	}
	// 不应 panic; 仍应被拒。
	require.Error(t, r.validateSpeak(out))
}

// TestReActRunner_ValidateSpeak_HonorsAllowedEvidenceIDs (v2.13) 是
// RunnerConfig.AllowedEvidenceIDs 接线的回归护栏。
//
// 该字段此前恒为 nil,ValidateAgainstHallucination 的 Layer B 因此对**任何**
// 带 evidence_refs 的发言一律"保守拒绝"(见 output_validator.go)→ 每次发言
// 白重试一次(重试期间无流式输出 = 前端打字机卡 2s)后仍原样放行,守卫只贡献
// 延迟、没拦住任何东西。
//
// 接线后: 引用本场真实 evidence_id 放行,编造的仍拒绝。
func TestReActRunner_ValidateSpeak_HonorsAllowedEvidenceIDs(t *testing.T) {
	t.Parallel()

	newOut := func(refs ...string) *AgentOutput {
		return &AgentOutput{
			Reasoning:    "基于证据展开论证",
			Content:      "我方援引上述证据说明该方案更为稳健。",
			Confidence:   0.6,
			Stance:       "pro_a",
			EvidenceRefs: refs,
		}
	}

	wired := NewReActRunner(nil, "sys", nil, RunnerConfig{
		AllowedEvidenceIDs: []string{"E001", "E002"},
	})

	// 1) 引用本场真实存在的 E001 → 放行
	require.NoError(t, wired.validateSpeak(newOut("E001")),
		"引用真实 evidence_id 不应被判为幻觉")

	// 2) 引用不存在的 E999 → 仍拒绝(编造 ID 本来就是幻觉)
	err := wired.validateSpeak(newOut("E999"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "E999", "编造 evidence_id 必须被拒且报出该 ID")

	// 3) 未接线(nil) → 维持旧的保守拒绝,不静默放宽(向后兼容)
	legacy := NewReActRunner(nil, "sys", nil, RunnerConfig{})
	require.Error(t, legacy.validateSpeak(newOut("E001")),
		"未接线时应维持保守拒绝")
}

package agent

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// v0.10.24 候选 1: isStanceConsistent 老函数边界 + applySpeakerStanceJudge 新函数单测

// --- mockAgent 构造测试用 agent ---
func mockStanceAgent(agentType model.AgentType, beliefA float64) model.Agent {
	return model.Agent{
		ID:        uuid.New(),
		AgentType: agentType,
		BeliefA:   beliefA,
	}
}

// --- 老 isStanceConsistent 边界测试 (3 个) ---
// 老实现实际阈值:
//   - pro_a: 拒绝当 BeliefA < 0.45 (通用检查) OR BeliefA < 0.55 (Defender 特化)
//   - pro_b: 拒绝当 BeliefA > 0.45 (Prosecutor 特化) OR BeliefA > 0.55 (通用检查)
// 实际是 [0.45, 0.55] 双边界, 中间地带 (0.45 ≤ belief ≤ 0.55) 任何 stance 都一致 (逃逸口)。

func TestIsStanceConsistent_Boundary0_45(t *testing.T) {
	// 0.45 + pro_b: 0.45 > 0.45 false → 不被 Prosecutor 分支拒绝 → return true
	ag := mockStanceAgent(model.AgentProsecutor, 0.45)
	require.True(t, isStanceConsistent(ag, "pro_b"), "0.45 + pro_b 是一致 (老用 > 0.45 严格)")
	// 0.45 + pro_a: 0.45 < 0.45 false → 不被通用分枝拒绝 → return true
	require.True(t, isStanceConsistent(ag, "pro_a"), "0.45 + pro_a 是一致 (老用 < 0.45 严格, 0.45 算 '高')")
	// 0.46 + pro_b: 0.46 > 0.45 true → Prosecutor 拒绝 → return false
	ag3 := mockStanceAgent(model.AgentProsecutor, 0.46)
	require.False(t, isStanceConsistent(ag3, "pro_b"), "0.46 + pro_b 不一致 (0.46 > 0.45)")
	// 0.44 + pro_a: 0.44 < 0.45 true → 通用拒绝 → return false
	ag2 := mockStanceAgent(model.AgentProsecutor, 0.44)
	require.False(t, isStanceConsistent(ag2, "pro_a"), "0.44 + pro_a 不一致 (0.44 < 0.45)")
}

func TestIsStanceConsistent_Boundary0_55(t *testing.T) {
	// 0.55 + pro_a: 0.55 < 0.55 false → Defender 不拒绝 → 通用 0.55 < 0.45 false → return true
	ag := mockStanceAgent(model.AgentDefender, 0.55)
	require.True(t, isStanceConsistent(ag, "pro_a"), "0.55 + pro_a 是一致 (老用 < 0.55 严格)")
	// 0.55 + pro_b: 0.55 > 0.45 false (走通用) AND 0.55 > 0.55 false → return true
	require.True(t, isStanceConsistent(ag, "pro_b"), "0.55 + pro_b 也是一致 (0.55 > 0.55 不成立)")
	// 0.56 + pro_b: 0.56 > 0.55 true → 通用拒绝 → return false
	ag2 := mockStanceAgent(model.AgentDefender, 0.56)
	require.False(t, isStanceConsistent(ag2, "pro_b"), "0.56 + pro_b 不一致 (0.56 > 0.55)")
	// 0.54 + pro_a: 0.54 < 0.55 true → Defender 拒绝 → return false
	ag3 := mockStanceAgent(model.AgentDefender, 0.54)
	require.False(t, isStanceConsistent(ag3, "pro_a"), "0.54 + pro_a 不一致 (0.54 < 0.55)")
}

func TestIsStanceConsistent_ChallengeAlwaysTrue(t *testing.T) {
	ag := mockStanceAgent(model.AgentProsecutor, 0.5) // 中性
	// challenge stance 永远一致 (逃逸口)
	require.True(t, isStanceConsistent(ag, "challenge"))
	require.True(t, isStanceConsistent(ag, "neutral"))
}

// --- applySpeakerStanceJudge 单元测试 (7 个) ---
// 仿 novelty_check_test.go, 但 applySpeakerStanceJudge 调 LLM client,
// 用 fakeLLM client 模拟不同 judge 响应

// fakeLLMStance 是 minimal LLM client mock, 按调用顺序返回预置内容
type fakeLLMStance struct {
	responses []string // 每次 Complete 返回下一个, 最后一个反复
	idx       int
}

func (f *fakeLLMStance) Complete(ctx context.Context, systemPrompt string, messages []llm.Message, opts llm.CompletionOptions) (string, llm.Usage, error) {
	if f.idx >= len(f.responses) {
		f.idx = len(f.responses) - 1
	}
	r := f.responses[f.idx]
	f.idx++
	return r, llm.Usage{}, nil
}

func (f *fakeLLMStance) StreamComplete(ctx context.Context, systemPrompt string, messages []llm.Message, opts llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

func newStanceTestRunner(t *testing.T, fake *fakeLLMStance, agentType model.AgentType, beliefA float64) *ReActRunner {
	r := NewReActRunner(fake, "test-system", nil, RunnerConfig{
		MaxIterations:  1,
		SpeakerBeliefA: beliefA,
		SpeakerAgent:   mockStanceAgent(agentType, beliefA),
	})
	return r
}

// beliefA=0.5 (中性) + pro_a → 老 isStanceConsistent 通过 → 跳过 judge → 无标记
func TestApplySpeakerStanceJudge_NoJudgeAt0_5Belief(t *testing.T) {
	fake := &fakeLLMStance{responses: []string{}}
	r := newStanceTestRunner(t, fake, model.AgentProsecutor, 0.5)
	out := Speaker{Content: "anything", Stance: "pro_a"}
	got, _ := applySpeakerStanceJudge(out, r, context.Background(), nil)
	require.False(t, got.StanceRejected)
	require.Empty(t, got.StanceJudgeReason)
}

// beliefA=0.3 + stance=pro_b → 一致 → 跳过 judge
func TestApplySpeakerStanceJudge_BeliefConsistent_SkipJudge(t *testing.T) {
	fake := &fakeLLMStance{responses: []string{}}
	r := newStanceTestRunner(t, fake, model.AgentProsecutor, 0.3)
	out := Speaker{Content: "anything", Stance: "pro_b"}
	got, _ := applySpeakerStanceJudge(out, r, context.Background(), nil)
	require.False(t, got.StanceRejected)
	// fake 没被调用 (idx=0)
	require.Equal(t, 0, fake.idx)
}

// beliefA=0.3 + stance=pro_a → 不一致 → 调 judge → judge 返回 true → pass
func TestApplySpeakerStanceJudge_JudgePass(t *testing.T) {
	fake := &fakeLLMStance{responses: []string{
		`{"is_consistent": true, "reason": "支持 B 没毛病"}`,
	}}
	r := newStanceTestRunner(t, fake, model.AgentProsecutor, 0.3)
	out := Speaker{Content: "anything", Stance: "pro_a"}
	got, _ := applySpeakerStanceJudge(out, r, context.Background(), nil)
	require.False(t, got.StanceRejected)
	// judge 调了 1 次
	require.Equal(t, 1, fake.idx)
}

// beliefA=0.3 + stance=pro_a → judge false → retry → 第二次 judge true → pass
func TestApplySpeakerStanceJudge_Retry1Success(t *testing.T) {
	// LLM call 顺序: judge1 (idx 0), retry1 重生成 (idx 1), judge2 (idx 2)
	// judge2 返回 true → pass, 不进 judgeStanceOnce
	fake := &fakeLLMStance{responses: []string{
		`{"is_consistent": false, "reason": "judge1 fail"}`,
		`{"action":"speak","content":"改内容","reasoning":"...","stance":"pro_b","confidence":0.5,"evidence_refs":[]}`, // retry1 重生成
		`{"is_consistent": true, "reason": "judge2 pass"}`, // judge2 通过
	}}
	r := newStanceTestRunner(t, fake, model.AgentProsecutor, 0.3)
	out := Speaker{Content: "支持 A 的内容", Stance: "pro_a"}
	got, _ := applySpeakerStanceJudge(out, r, context.Background(), nil)
	require.False(t, got.StanceRejected, "第 2 次 judge true 应 pass")
	require.Equal(t, 3, fake.idx, "调 3 次: judge1 + retry1 + judge2")
}

// beliefA=0.3 + stance=pro_a → judge false → 2 次 retry 都失败 → fallback rejected=true
func TestApplySpeakerStanceJudge_Retry2FailureFallback(t *testing.T) {
	// LLM call 顺序 (5 次): judge1, retry1, judge2, retry2, judgeStanceOnce
	fake := &fakeLLMStance{responses: []string{
		`{"is_consistent": false, "reason": "judge1 fail"}`,
		`{"action":"speak","content":"改1","reasoning":"x","stance":"pro_a","confidence":0.5,"evidence_refs":[]}`, // retry1
		`{"is_consistent": false, "reason": "judge2 fail"}`,
		`{"action":"speak","content":"改2","reasoning":"x","stance":"pro_a","confidence":0.5,"evidence_refs":[]}`, // retry2
		`{"is_consistent": false, "reason": "final reason"}`, // judgeStanceOnce
	}}
	r := newStanceTestRunner(t, fake, model.AgentProsecutor, 0.3)
	out := Speaker{Content: "支持 A 的内容", Stance: "pro_a"}
	got, _ := applySpeakerStanceJudge(out, r, context.Background(), nil)
	require.True(t, got.StanceRejected)
	require.Contains(t, got.StanceJudgeReason, "final reason", "fallback 应拿最后一次 judge reason")
	require.Equal(t, 5, fake.idx)
}

// judge LLM 返回非 JSON → fallback rejected=true (保守放行)
func TestApplySpeakerStanceJudge_JudgeParseFailure(t *testing.T) {
	fake := &fakeLLMStance{responses: []string{
		"this is not JSON at all",
	}}
	r := newStanceTestRunner(t, fake, model.AgentProsecutor, 0.3)
	out := Speaker{Content: "支持 A 的内容", Stance: "pro_a"}
	got, _ := applySpeakerStanceJudge(out, r, context.Background(), nil)
	require.True(t, got.StanceRejected)
	require.Contains(t, got.StanceJudgeReason, "judge 输出非 JSON")
}

// --- StanceJudgePrompt 模板测试 (1 个) ---

func TestStanceJudgePrompt_ContainsBeliefAndContent(t *testing.T) {
	prompt := StanceJudgePrompt(model.AgentProsecutor, 0.3, "支持选项 A 的具体内容")
	// model.AgentProsecutor 实际值是 "prosecutor" (小写), 见 model/db.go:72
	require.Contains(t, prompt, "prosecutor")
	require.Contains(t, prompt, "0.30")
	require.Contains(t, prompt, "支持选项 A 的具体内容")
	require.Contains(t, prompt, "is_consistent")
	require.Contains(t, prompt, "0.45")
	require.Contains(t, prompt, "0.55")
}
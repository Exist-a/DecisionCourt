package promptlab

import (
	"context"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/stretchr/testify/require"
)

// fakeABLLM 复 fakeEvalLLM 的语义,但 AbTest 路径会反复调 Complete
// (每条 trial output 调 2 次: Eval A + Eval B),所以 responses 必须 ≥ 2*len(trialOutputs)。
type fakeABLLM struct {
	responses []string
	idx       int
}

func (f *fakeABLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	if f.idx >= len(f.responses) {
		f.idx = len(f.responses) - 1
	}
	if f.idx < 0 {
		f.idx = 0
	}
	r := f.responses[f.idx]
	f.idx++
	return r, llm.Usage{}, nil
}

func (f *fakeABLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

// T1: 长度规则确定性 + 同 output → MeanA == MeanB → tie (因为 v1.0.3 PR-B2
// 不区分 version 的 prompt,A/B 走相同 Eval,理论上结果一致)。
//
// 设计理由: 当前 PR 不实现 "按 version 切换 prompt 跑 trial",
// AbTest 只做评分比较框架,确定性规则上 A/B 结果必然 tie。
// 这是 v1.0.3 PR-B2 的明确设计妥协 (见 abtest.go 注释)。
func TestABTest_DeterministicRuleTie(t *testing.T) {
	fake := &fakeABLLM{}
	outputs := []string{
		strings.Repeat("中", 100), // 100 字 → pass
		strings.Repeat("a", 200), // 200 字 → pass
	}

	result, err := RunABTest(context.Background(), fake, "1.0.3-pr1", "1.0.4-pr1", EvalRuleLength, outputs)
	require.NoError(t, err)
	require.Equal(t, "1.0.3-pr1", result.VersionA)
	require.Equal(t, "1.0.4-pr1", result.VersionB)
	require.Equal(t, EvalRuleLength, result.Rule)
	require.Len(t, result.ScoresA, 2, "A 评分应有 2 条")
	require.Len(t, result.ScoresB, 2, "B 评分应有 2 条")
	require.InDelta(t, 1.0, result.MeanA, 0.001, "两条都 ≤ 300 → MeanA=1.0")
	require.InDelta(t, 1.0, result.MeanB, 0.001, "两条都 ≤ 300 → MeanB=1.0")
	require.Equal(t, ABTestWinnerTie, result.Winner, "确定性规则上 A/B 结果相同 → tie")
	require.InDelta(t, 0.0, result.Confidence, 0.001)
}

// T2: trial_outputs 长度超限 → error,不调 LLM。
func TestABTest_TooManyOutputsError(t *testing.T) {
	fake := &fakeABLLM{}
	outputs := make([]string, ABTestMaxTrialOutputs+1)
	for i := range outputs {
		outputs[i] = "any"
	}
	_, err := RunABTest(context.Background(), fake, "v_a", "v_b", EvalRuleLength, outputs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max")
}

// T3: trial_outputs 为空 → error。
func TestABTest_EmptyOutputsError(t *testing.T) {
	fake := &fakeABLLM{}
	_, err := RunABTest(context.Background(), fake, "v_a", "v_b", EvalRuleLength, []string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

// T4: LLM 规则 + 2 条 trial outputs — 验证 ScoresA/ScoresB 数组下标对齐 + LLM 调用次数正确。
//
// mock LLM 4 次调用按 idx 顺序返回不同 JSON。abtest.go 顺序: A[0], B[0], A[1], B[1]。
// 4 次响应设计为 [0.9, 0.4, 0.9, 0.4],使 A 都得 0.9,B 都得 0.4 → MeanA=0.9,MeanB=0.4,
// 差 0.5 > ABTestMinConfidenceDiff → Winner=B。
func TestABTest_LLMRuleScoresAlignment(t *testing.T) {
	fake := &fakeABLLM{
		responses: []string{
			`{"score": 0.9, "pass": true, "reason": "OK"}`,
			`{"score": 0.4, "pass": false, "reason": "BAD"}`,
			`{"score": 0.9, "pass": true, "reason": "OK"}`,
			`{"score": 0.4, "pass": false, "reason": "BAD"}`,
		},
	}
	outputs := []string{"good speech", "bad speech"}

	result, err := RunABTest(context.Background(), fake, "v_a", "v_b", EvalRuleStanceMention, outputs)
	require.NoError(t, err)
	require.Len(t, result.ScoresA, 2)
	require.Len(t, result.ScoresB, 2)

	require.Equal(t, 0.9, result.ScoresA[0].Score, "A[0] 来自 fake response #1 = 0.9")
	require.Equal(t, 0.9, result.ScoresA[1].Score, "A[1] 来自 fake response #3 = 0.9")
	require.Equal(t, 0.4, result.ScoresB[0].Score, "B[0] 来自 fake response #2 = 0.4")
	require.Equal(t, 0.4, result.ScoresB[1].Score, "B[1] 来自 fake response #4 = 0.4")

	require.InDelta(t, 0.9, result.MeanA, 0.001)
	require.InDelta(t, 0.4, result.MeanB, 0.001)
	require.Equal(t, ABTestWinnerA, result.Winner, "MeanA=0.9 > MeanB=0.4 → Winner=A")
	require.InDelta(t, 0.5, result.Confidence, 0.001)
	require.Contains(t, result.Reasoning, "rule=stance_mention", "reasoning 应包含 rule 名")
}

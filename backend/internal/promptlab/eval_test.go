package promptlab

import (
	"context"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/stretchr/testify/require"
)

// fakeEvalLLM 是 minimal LLM client mock,按调用顺序返回预置 JSON / 异常。
//
// 设计要点:
//   - Complete 返回 (content, Usage{}, err) — 与 llm.Client 接口对齐
//   - responses 数组按调用顺序消费,最后一个反复 (与 stance_judge_test.go 同模式)
//   - jsonModeCalls 记录每次调用 opts.JSONMode=true,测试 JSONMode 强制开启
//   - temperatureCalls 记录每次 opts.Temperature,验证 Eval 用 0.2
type fakeEvalLLM struct {
	responses       []string
	idx             int
	jsonModeCalls   []bool
	temperatureCalls []float32
}

func (f *fakeEvalLLM) Complete(_ context.Context, _ string, _ []llm.Message, opts llm.CompletionOptions) (string, llm.Usage, error) {
	f.jsonModeCalls = append(f.jsonModeCalls, opts.JSONMode)
	f.temperatureCalls = append(f.temperatureCalls, opts.Temperature)
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

func (f *fakeEvalLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

// T1: length_compliance 确定性 — 短发言 (50 字) 应 pass。
func TestEval_LengthPass(t *testing.T) {
	fake := &fakeEvalLLM{}
	output := strings.Repeat("中", 50) // 50 runes

	result, err := Eval(context.Background(), fake, EvalRuleLength, output)
	require.NoError(t, err)
	require.Equal(t, EvalRuleLength, result.Rule)
	require.Equal(t, 1.0, result.Score, "50/300 chars 应该 score=1.0")
	require.True(t, result.Pass, "50 字应该 pass")
	require.Contains(t, result.Reasoning, "length=50/300")
	require.Equal(t, 0, result.LatencyMs, "确定性规则不应调 LLM,LatencyMs=0")
	require.Empty(t, fake.jsonModeCalls, "确定性规则不应调 LLM,无 JSONMode 调用")
}

// T2: length_compliance 确定性 — 超长发言 (500 字) 应 fail,score=0。
func TestEval_LengthFail(t *testing.T) {
	fake := &fakeEvalLLM{}
	output := strings.Repeat("a", 500)

	result, err := Eval(context.Background(), fake, EvalRuleLength, output)
	require.NoError(t, err)
	require.Equal(t, EvalRuleLength, result.Rule)
	require.Equal(t, 0.0, result.Score, "500/300 chars 应该 score=0")
	require.False(t, result.Pass, "500 字应 fail")
	require.Contains(t, result.Reasoning, "length=500/300")
}

// T3: UTF-8 多字节 — 中文 rune 数与 byte 数差异。
// strings.Repeat("中", 10) 是 10 runes / 30 bytes,验证 Eval 按 rune 数不算 byte 数。
func TestEval_LengthUTF8RuneCount(t *testing.T) {
	fake := &fakeEvalLLM{}
	output := strings.Repeat("中", 10) // 10 runes, 30 bytes

	result, err := Eval(context.Background(), fake, EvalRuleLength, output)
	require.NoError(t, err)
	require.True(t, result.Pass, "10 中文字符 = 10 runes ≤ 300")
	require.Contains(t, result.Reasoning, "length=10/300", "必须按 rune 数,不按 byte 数")
}

// T4: evidence_id_format 走 LLM 路径 — mock 返回合法 JSON,score=0.85 → pass。
func TestEval_EvidenceIDFormatPass(t *testing.T) {
	fake := &fakeEvalLLM{
		responses: []string{`{"score": 0.85, "pass": true, "reason": "引用的 E001 合法"}`},
	}
	output := "我方主张 X,依据证据 E001。"

	result, err := Eval(context.Background(), fake, EvalRuleEvidenceID, output)
	require.NoError(t, err)
	require.Equal(t, EvalRuleEvidenceID, result.Rule)
	require.Equal(t, 0.85, result.Score)
	require.True(t, result.Pass, "score=0.85 > 0.7 应 pass")
	require.Equal(t, "引用的 E001 合法", result.Reasoning)
	require.GreaterOrEqual(t, result.LatencyMs, 0, "LLM 路径 LatencyMs 应 ≥ 0（mock 耗时为 0 也合法）")
	require.Len(t, fake.jsonModeCalls, 1, "LLM 路径应调 1 次")
	require.True(t, fake.jsonModeCalls[0], "JSONMode 必须开")
	require.Equal(t, float32(0.2), fake.temperatureCalls[0], "temperature 必须 0.2")
}

// T5: stance_mention 走 LLM 路径 — mock 输出非 JSON,兜底 score=0 + pass=false。
func TestEval_LLMInvalidJSONFallback(t *testing.T) {
	fake := &fakeEvalLLM{
		responses: []string{`not json, just plain text`},
	}
	output := "some speech content"

	result, err := Eval(context.Background(), fake, EvalRuleStanceMention, output)
	require.NoError(t, err, "LLM 输出非 JSON 不应让 Eval 整体失败")
	require.Equal(t, EvalRuleStanceMention, result.Rule)
	require.Equal(t, 0.0, result.Score, "非 JSON 兜底 score=0")
	require.False(t, result.Pass, "非 JSON 兜底 pass=false")
	require.Contains(t, result.Reasoning, "judge 输出非 JSON", "reason 应说明非 JSON")
}

// T6: 未知 rule — 返回 error,LLM 不应被调。
func TestEval_UnknownRuleError(t *testing.T) {
	fake := &fakeEvalLLM{}
	_, err := Eval(context.Background(), fake, EvalRule("nonexistent_rule"), "any")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown rule")
	require.Empty(t, fake.jsonModeCalls, "未知 rule 不应调 LLM")
}

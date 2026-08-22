package agent

// v1.0-patch (2026-08-22): 回归测试 — hallucination 硬拒 + retry 失败时
// streamedFallback 软降级, 不让整轮 cross_exam 因空 content 中断。
//
// 根因链:
//   1. streamSpeakContent 成功 → out.Content = streamed
//   2. ValidateAgainstHallucination 失败 (如 percentRegex 匹配 "15%")
//   3. out.Content = "" 清空 → 走 retry 路径
//   4. retry 后 validateSpeak 仍失败 → fall-through 空 content
//   5. saveAgentMessage guard 返 error → 整轮 cross_exam 中断
//      (用户 2026-08-22 反馈 "操作未能完成")
//
// 修复: retry 失败 fall-through 时, 若 out.Content 为空且 streamedFallback
// 非空, 恢复 streamedFallback 内容 (软降级, slog.Warn 记录)。

import (
	"context"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

// fakeLLMFallback 模拟: 第 1 次流式成功 (含百分比触发 hallucination fail),
// 第 2 次 (retry) 返回不含 action speak 的内容让 validateSpeak 失败。
type fakeLLMFallback struct {
	responses []string
	idx       int
}

func (f *fakeLLMFallback) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	if f.idx >= len(f.responses) {
		f.idx = len(f.responses) - 1
	}
	r := f.responses[f.idx]
	f.idx++
	return r, llm.Usage{}, nil
}

func (f *fakeLLMFallback) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 2)
	if f.idx < len(f.responses) {
		ch <- llm.StreamChunk{Content: f.responses[f.idx]}
	}
	f.idx++
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

// T1: hallucination 硬拒 + retry 失败 → 恢复 streamedFallback (content 非空)
func TestStreamFallback_HallucinationRetryFail_StillHasContent(t *testing.T) {
	// 第 1 次流式返回: {"content":"...成功率提升 15%..."} (percentRegex 命中)
	// 第 2 次 retry 返回: 无效 JSON 让 validateSpeak 失败
	fake := &fakeLLMFallback{
		responses: []string{
			`{"content":"我方认为成功率提升 15%，损失 3 万元。"}`,
			`invalid json`,
		},
	}

	r := NewReActRunner(fake, "test-system", nil, RunnerConfig{
		MaxIterations:  1,
		SpeakerBeliefA: 0.7,
		SpeakerAgent:   model.Agent{AgentType: model.AgentProsecutor, BeliefA: 0.7},
		OnSpeakChunk:   func(chunk, accumulated string) {}, // 启用流式路径
	})

	speaker, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)
	// 关键断言: content 不能为空 (软降级保住了 stream 内容)
	require.NotEmpty(t, strings.TrimSpace(speaker.Content),
		"hallucination 硬拒 + retry 失败后, 应恢复 streamedFallback 而不是返回空 content")
}

// T2: hallucination 通过 → 正常路径 content 不变
func TestStreamFallback_HallucinationPass_NormalContent(t *testing.T) {
	fake := &fakeLLMFallback{
		responses: []string{
			`{"content":"我方基于现有材料主张，本案关键事实尚待核实。"}`,
		},
	}

	r := NewReActRunner(fake, "test-system", nil, RunnerConfig{
		MaxIterations:  1,
		SpeakerBeliefA: 0.7,
		SpeakerAgent:   model.Agent{AgentType: model.AgentProsecutor, BeliefA: 0.7},
		OnSpeakChunk:   func(chunk, accumulated string) {},
	})

	speaker, _, err := r.Run(context.Background(), []model.Message{})
	require.NoError(t, err)
	require.NotEmpty(t, speaker.Content)
}

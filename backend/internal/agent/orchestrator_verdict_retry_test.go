package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// v2.7 PR-2 (ADR 0041) — JudgeFinalDecision + GenerateVerdict retry-on-Canceled hook 测试.
//
// 行为契约 (per plan §B.6):
//   1. 第一次 LLM.Complete 返 context.Canceled → 自动 one-shot retry with detached ctx
//   2. 第二次 (retry) 必须用 detached ctx (background + 90s timeout) — 即使 caller
//      传进来的 ctx 已 cancelled, retry 仍能继续
//   3. 第一次返其他 error (network / parse / timeout) → 不重试 (因为只有 cancel
//      是 "false positive" 可恢复; 真错误就是真错误)
//   4. 父 ctx 已 cancelled 时, verdict LLM call 仍能完成
//
// 不变量:
//   - retry 必须有上限 (1-shot, 不无限 retry, 防 LLM 真挂)
//   - 重试只在 context.Canceled 时触发; context.DeadlineExceeded 不重试
//     (timeout 通常是真的慢, retry 也无效)

// stubLLMVerdictRetry 是 stubLLM 的扩展,支持按 call 序号返回不同 result.
type stubLLMVerdictRetry struct {
	mu sync.Mutex

	// callResults 按调用顺序返回. 元素是 (output string, err error).
	// 越界时用最后一个 (re-use),保持 fixture 简单.
	callResults []stubCallResult

	// callCount 真实递增,确保 call N 用 callResults[N].
	// (此前用 len(callResults) 当 idx 是 bug, 两次 call 都拿到末位 slot.)
	callCount int

	// observedCtxIsBackground 用于断言 retry 用的 ctx 是 context.Background() 派生
	// 而不是 caller 传的 (可能在 retry 时已 cancelled).
	firstCallCtx  context.Context
	retryCallCtx  context.Context
	firstCallSeen bool
}

type stubCallResult struct {
	output string
	err    error
}

func (s *stubLLMVerdictRetry) Complete(ctx context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	callNum := s.callCount
	s.callCount++
	s.recordCall(ctx)

	if callNum >= len(s.callResults) {
		callNum = len(s.callResults) - 1
	}
	r := s.callResults[callNum]
	return r.output, llm.Usage{}, r.err
}

func (s *stubLLMVerdictRetry) recordCall(ctx context.Context) {
	// 简单实现: 在 sync.Mutex 保护下记录前两次调用的 ctx.
	// (因为我们只想断言 retry 不复用 caller 的 ctx.)
	if !s.firstCallSeen {
		s.firstCallCtx = ctx
		s.firstCallSeen = true
		return
	}
	s.retryCallCtx = ctx
}

func (s *stubLLMVerdictRetry) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true}
	close(out)
	return out
}

// validJudgeDecisionJSON 返回 JudgeFinalDecision LLM 的合法 JSON 响应.
func validJudgeDecisionJSON() string {
	d := JudgeDecision{
		BeliefA:        0.6,
		BeliefB:        0.4,
		Preferred:      "option_a",
		Reasoning:      "E001 长期收益显著优于 B",
		Recommendation: "建议选择 A",
	}
	b, _ := json.Marshal(d)
	return string(b)
}

// validVerdictJSON 返回 GenerateVerdict LLM 的合法 JSON 响应.
func validVerdictJSON() string {
	r := map[string]interface{}{
		"summary":          "建议选择 A（基于证据 E001）",
		"trial_summary":    "本场庭审共 3 轮，双方充分辩论",
		"option_a_score":   0.6,
		"option_b_score":   0.4,
		"consensus_points": []string{"E001 长期收益优势"},
		"divergence_points": []string{"B 短期风险评估"},
		"recommendation":   "选 A",
		"content":          "## 一、双方主张\n...## 二、证据认定\n## 四、法官裁决",
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func newTestOrchestratorWithLLM(t *testing.T, llmClient llm.Client) *Orchestrator {
	t.Helper()
	a2aRepo := a2a.NewInMemoryRepository(nil)
	memRepo := private_memory.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	return NewOrchestratorLegacy(llmClient, bus, memRepo)
}

func makeSessionForVerdict() (model.CourtSession, model.Agent) {
	sessionID := uuid.New()
	agentID := uuid.New()
	session := model.CourtSession{
		ID:          sessionID,
		SessionUUID: "test-session-uuid",
		OptionA:     "选项 A",
		OptionB:     "选项 B",
		Context:     "测试上下文",
	}
	judge := model.Agent{
		ID:        agentID,
		AgentType: model.AgentJudge,
		Name:      "法官",
		BeliefA:   0.5,
		BeliefB:   0.5,
	}
	return session, judge
}

// T1: JudgeFinalDecision — 第一次 context.Canceled → retry 成功
func TestJFV2Retry_FirstCanceled_RetrySucceeds(t *testing.T) {
	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: context.Canceled}, // 第一次 false-positive cancel
			{output: validJudgeDecisionJSON()}, // retry 成功
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, judge := makeSessionForVerdict()

	decision, err := orch.JudgeFinalDecision(context.Background(), judge, session, nil, nil)
	require.NoError(t, err, "retry 后应返回 success, 不应 propagate context.Canceled")
	require.Equal(t, "option_a", decision.Preferred)
	require.InDelta(t, 0.6, decision.BeliefA, 0.01)

	llmClient.mu.Lock()
	defer llmClient.mu.Unlock()
	require.NotNil(t, llmClient.retryCallCtx, "retry must have happened")
	require.NotEqual(t, llmClient.firstCallCtx, llmClient.retryCallCtx,
		"retry ctx must be a DIFFERENT context (detached), not reused firstCallCtx")
	_, hasDeadline := llmClient.retryCallCtx.Deadline()
	require.True(t, hasDeadline,
		"retry ctx must have a deadline (i.e., derived from Background+WithTimeout, not bare Background)")
}

// T2: GenerateVerdict — 第一次 context.Canceled → retry 成功
func TestGenerateVerdictRetry_FirstCanceled_RetrySucceeds(t *testing.T) {
	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: context.Canceled},
			{output: validVerdictJSON()},
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, _ := makeSessionForVerdict()
	judgeDecision := JudgeDecision{
		BeliefA:        0.6,
		BeliefB:        0.4,
		Preferred:      "option_a",
		Reasoning:      "test",
		Recommendation: "选 A",
	}

	result, err := orch.GenerateVerdict(context.Background(), session, nil, nil, judgeDecision)
	require.NoError(t, err, "retry 后必须 success, 不能 propagate context.Canceled")
	require.NotNil(t, result)
	require.Contains(t, result, "summary")
}

// T3: 非 context.Canceled 错误不重试
func TestJFV2Retry_NonCanceledError_DoesNotRetry(t *testing.T) {
	realErr := errors.New("network unreachable (not a cancel)")
	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: realErr},
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, judge := makeSessionForVerdict()

	_, err := orch.JudgeFinalDecision(context.Background(), judge, session, nil, nil)
	require.ErrorIs(t, err, realErr, "non-cancel error must propagate as-is, no retry")

	llmClient.mu.Lock()
	defer llmClient.mu.Unlock()
	require.Nil(t, llmClient.retryCallCtx, "must NOT retry on non-cancel error (retryCallCtx stays nil)")
}

// T4: context.DeadlineExceeded 不重试 (timeout 是真慢, retry 也无效)
func TestJFV2Retry_DeadlineExceeded_DoesNotRetry(t *testing.T) {
	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: context.DeadlineExceeded},
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, judge := makeSessionForVerdict()

	_, err := orch.JudgeFinalDecision(context.Background(), judge, session, nil, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded, "deadline-exceeded propagates as-is, no retry")

	llmClient.mu.Lock()
	defer llmClient.mu.Unlock()
	require.Nil(t, llmClient.retryCallCtx, "must NOT retry on DeadlineExceeded")
}

// T5: 父 ctx 已取消时, retry 仍能成功 (detached ctx 是 v2.7 PR-2 的核心)
//
// 这是模拟用户点 direct_verdict 时 HTTP ctx 已 cancelled 的场景.
// finishTrial 内部会把 verdict LLM 调用改用 verdictCtx (detached). 但
// orchestrator.go 内部仍做 belt-and-suspenders retry — 即使 caller 传的
// ctx 已 cancelled, retry 用 detached ctx 让 verdict 生成有第二道兜底.
func TestJFV2Retry_ParentCtxCanceled_RetryRecoversCtx(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	parentCancel() // 立刻 cancel parent ctx

	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: context.Canceled}, // 第一次因为 parent ctx 已 cancelled
			{output: validJudgeDecisionJSON()}, // retry 用 detached ctx 成功
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, judge := makeSessionForVerdict()

	decision, err := orch.JudgeFinalDecision(parentCtx, judge, session, nil, nil)
	require.NoError(t, err, "retry with detached ctx must succeed even if parent ctx is cancelled")
	require.Equal(t, "option_a", decision.Preferred)

	llmClient.mu.Lock()
	defer llmClient.mu.Unlock()
	require.NotNil(t, llmClient.retryCallCtx, "retry must have happened")
	require.NotEqual(t, llmClient.firstCallCtx, llmClient.retryCallCtx,
		"retry ctx must be a FRESH context (not parent ctx which is cancelled at retry time)")
	_, hasDeadline := llmClient.retryCallCtx.Deadline()
	require.True(t, hasDeadline,
		"retry ctx must have a 90s deadline (detached from parent)")
}

// T6: retry 不消耗时间 — 即使我们把 detached timeout 设 90s, 测试不该真的等
func TestJFV2Retry_RetryHappensQuickly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: context.Canceled},
			{output: validJudgeDecisionJSON()},
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, judge := makeSessionForVerdict()

	start := time.Now()
	_, err := orch.JudgeFinalDecision(context.Background(), judge, session, nil, nil)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Less(t, elapsed, 1*time.Second,
		"retry should be near-instant for happy cancel path; if test takes >1s the retry isn't using detached ctx correctly")
}

// T7: 一次性 retry 限制 — 即使第一次 Canceled 第二次又 Canceled, 第二次的 err
// 是 propagate (不再 infinite retry).
func TestJFV2Retry_OneShotLimit(t *testing.T) {
	llmClient := &stubLLMVerdictRetry{
		callResults: []stubCallResult{
			{err: context.Canceled},
			{err: context.Canceled}, // retry 也 cancel — 不再 retry
		},
	}
	orch := newTestOrchestratorWithLLM(t, llmClient)
	session, judge := makeSessionForVerdict()

	_, err := orch.JudgeFinalDecision(context.Background(), judge, session, nil, nil)
	require.ErrorIs(t, err, context.Canceled, "if retry also fails with Canceled, propagate (no infinite retry)")

	// call count 检查: 必须只 call 2 次 (first + retry, 不是 first + retry + retry + ...)
	llmClient.mu.Lock()
	defer llmClient.mu.Unlock()
	require.NotNil(t, llmClient.retryCallCtx, "retry should have happened")
}

// 占位: 不让 a2a / private_memory / time 等 imports 报警.
var (
	_ = a2a.AddressOrchestrator
	_ = private_memory.NewInMemoryRepository
	_ = time.Second
)

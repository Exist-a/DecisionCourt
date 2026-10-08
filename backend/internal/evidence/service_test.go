package evidence

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/llm"
)

// capturingClient 是 llm.Client 的测试替身：记下每次调用从 ctx 取到的 Trace，
// 并按 evidence/ 期望的 JSON 结构返回。
//
// 存在的理由（v2.13 回归钉）：evaluateEvidence 曾经构造 Trace 时漏了
// SessionUUID，导致 agent_gateway 的审计落库按空 session_uuid 反查主键失败，
// 每次证据评估都丢掉 llm_calls 行。这个 bug 在 LLM 调用结果、证据分数、
// 用户可见行为上全都看不出来 —— 只有 llm_calls 表少行 + decision_events
// 里多出 llm_audit_fk_violation 才暴露。所以断言必须钉在 Trace 上。
type capturingClient struct {
	calls []agent_gateway.Trace
}

const validEvidenceJSON = `{
  "impact_on_option_a": 0.4,
  "impact_on_option_b": -0.2,
  "credibility_score": 0.9,
  "relevance_score": 0.8,
  "constraint_strength": 0
}`

func (c *capturingClient) Complete(
	ctx context.Context,
	_ string,
	_ []llm.Message,
	_ llm.CompletionOptions,
) (string, llm.Usage, error) {
	c.calls = append(c.calls, agent_gateway.FromContext(ctx))
	return validEvidenceJSON, llm.Usage{TotalTokens: 1}, nil
}

func (c *capturingClient) StreamComplete(
	_ context.Context,
	_ string,
	_ []llm.Message,
	_ llm.CompletionOptions,
) <-chan llm.StreamChunk {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Done: true}
	close(ch)
	return ch
}

// TestEvaluateEvidence_TraceCarriesSessionUUID 是本文件的核心断言：
// 证据评估调用 LLM 时，ctx 上的 Trace 必须带 session_uuid。
// 不传 → 审计行丢失（见 capturingClient 注释）。
func TestEvaluateEvidence_TraceCarriesSessionUUID(t *testing.T) {
	fake := &capturingClient{}
	svc := NewService(nil, fake)

	const wantUUID = "3f1c9b7e-0000-4a2b-8c1d-9e8f7a6b5c4d"
	svc.evaluateEvidence("选项A", "选项B", "案情", "一份证据", "fact", wantUUID)

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 LLM call, got %d", len(fake.calls))
	}
	if got := fake.calls[0].SessionUUID; got != wantUUID {
		t.Errorf("trace SessionUUID = %q, want %q (missing it silently drops the llm_calls audit row)", got, wantUUID)
	}
	if got := fake.calls[0].AgentType; got != "clerk" {
		t.Errorf("trace AgentType = %q, want %q", got, "clerk")
	}
	if got := fake.calls[0].TaskType; got != "evidence_eval" {
		t.Errorf("trace TaskType = %q, want %q", got, "evidence_eval")
	}
}

// TestEvaluateEvidence_ParsesLLMResult 保证补 trace 时没改坏解析路径：
// LLM 返回合法 JSON 时，分数必须来自 LLM 而不是 keyword fallback。
func TestEvaluateEvidence_ParsesLLMResult(t *testing.T) {
	svc := NewService(nil, &capturingClient{})

	impactA, impactB, credibility, relevance, constraint :=
		svc.evaluateEvidence("选项A", "选项B", "案情", "一份证据", "fact", "sess-1")

	if impactA != 0.4 {
		t.Errorf("impactA = %v, want 0.4", impactA)
	}
	if impactB != -0.2 {
		t.Errorf("impactB = %v, want -0.2", impactB)
	}
	if credibility != 0.9 {
		t.Errorf("credibility = %v, want 0.9", credibility)
	}
	if relevance != 0.8 {
		t.Errorf("relevance = %v, want 0.8", relevance)
	}
	if constraint != 0 {
		t.Errorf("constraint = %v, want 0", constraint)
	}
}

// TestEvaluateEvidence_NilClientFallsBack 钉住"无 LLM 客户端时走 keyword 估算"，
// 确认把 sessionUUID 加进签名后这条降级分支还在。
func TestEvaluateEvidence_NilClientFallsBack(t *testing.T) {
	svc := NewService(nil, nil)

	_, _, credibility, relevance, _ :=
		svc.evaluateEvidence("选项A", "选项B", "案情", "一份证据", "fact", "sess-1")

	if credibility != 0.85 {
		t.Errorf("credibility = %v, want 0.85 (keyword fallback)", credibility)
	}
	if relevance != 0.8 {
		t.Errorf("relevance = %v, want 0.8 (keyword fallback)", relevance)
	}
}

package courtroom

// v0.10.21 PR-D (P2-C1): 埋 RunCrossExamRound span 测试。
//
// 验证:
//
//	1. SpanRunCrossExamRound 名称常量正确
//	2. Span API 在 nil metrics / nil recorder 场景下不 panic
//	   (这是 v0.10.0 Span 设计的关键契约: nil-safe)
//	3. Span.SetAttrs / SetError 都能正常工作
//	4. End() 多次调用 idempotent
//
// 注: runCrossExamRound 完整端到端测试需要构造 LLM + DB + session lock,
//      属于 integration test 范畴(本 PR 不写,避免工作量爆炸)。

import (
	"context"
	"errors"
	"testing"

	"github.com/decisioncourt/backend/internal/observability"
)

// TestSpanRunCrossExamRound_NameConstant 验证名称常量拼写正确,
// 防止 typo 影响 decision_events event_type 一致性。
func TestSpanRunCrossExamRound_NameConstant(t *testing.T) {
	t.Parallel()
	expected := "RunCrossExamRound"
	if observability.SpanRunCrossExamRound != expected {
		t.Errorf("SpanRunCrossExamRound = %q, want %q", observability.SpanRunCrossExamRound, expected)
	}
}

// TestTracerFromContext_NilSafe 验证 TracerFromContext(nil, nil, nil) 路径不 panic。
// 这是 runCrossExamRound 在 s.metrics=nil / s.recorder=nil 时不能炸的关键。
func TestTracerFromContext_NilSafe(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("TracerFromContext(nil, nil, nil) panicked: %v", r)
		}
	}()
	tracer := observability.TracerFromContext(context.Background(), nil, nil)
	if tracer == nil {
		t.Fatal("TracerFromContext returned nil")
	}
	span := tracer.StartSpan(observability.SpanRunCrossExamRound)
	if span == nil {
		t.Fatal("StartSpan returned nil")
	}
	// 这些调用在 nil metrics 路径都必须安全
	span.SetAttrs(map[string]interface{}{
		"session_uuid": "test-uuid",
		"round":        1,
		"phase":        "cross_exam",
	})
	span.SetError(errors.New("test error"))
	span.SetStatus("error")
	span.End()
	// 二次 End 应 idempotent
	span.End()
}

// TestSpanRunCrossExamRound_NilMetricsRecorder 验证:
// 在 courtroom.Service.metrics = nil 时, runCrossExamRound 里的 span 注入
// 不会因 nil metrics/recorder 而 panic。
//
// 实际 runCrossExamRound 需要 LLM + DB 才能完整跑(不在本测试范围);
// 这里只验证 span 注入部分的 nil-safe 契约。
func TestSpanRunCrossExamRound_NilMetricsRecorder(t *testing.T) {
	t.Parallel()
	// 模拟 Service 在装配阶段未注入 metrics 的场景
	svc := &Service{}
	if svc.metrics != nil {
		t.Fatalf("expected svc.metrics nil, got %v", svc.metrics)
	}

	// 直接调 span API(等价于 runCrossExamRound 顶部的 4 行)
	tracer := observability.TracerFromContext(context.Background(), svc.metrics, svc.recorder)
	span := tracer.StartSpan(observability.SpanRunCrossExamRound)
	span.SetAttrs(map[string]interface{}{
		"session_uuid": "abc-123",
		"round":        2,
	})
	span.End() // End 必须 idempotent 且 nil-safe
}
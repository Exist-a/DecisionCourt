package observability

import (
	"context"
)

// BusinessSpanNames 是业务级 span 名称的常量集合。
// 集中定义方便跨包引用 + 拼写一致 + 减少 typo 风险。
const (
	SpanStateTransition       = "CourtroomStateTransition"
	SpanRunCrossExamRound     = "RunCrossExamRound"
	SpanOpenCase              = "OpenCase"
	SpanSubmitEvidence        = "SubmitEvidence"
	SpanDispatchInvestigator  = "DispatchInvestigator"
	SpanBeliefUpdate          = "BeliefUpdate"
	SpanConvergenceCheck      = "ConvergenceCheck"
	SpanGenerateVerdict       = "GenerateVerdict"
	SpanA2APublish            = "A2A.Publish"
	SpanA2ABroadcast          = "A2A.Broadcast"
	SpanReActSpeak            = "ReAct.Speak"
	SpanReActToolCall         = "ReAct.ToolCall"
)

// TracerFromContext 是便捷构造：先从 ctx 取 trace，再构造 Tracer。
//
// 用法：
//   span := observability.TracerFromContext(ctx, metrics, recorder).StartSpan(SpanRunCrossExamRound)
//   defer span.End()
//
// 这样调用方无需手动管理 Tracer 实例。
func TracerFromContext(ctx context.Context, meter Metrics, rec EventRecorder) *Tracer {
	if ctx == nil {
		ctx = context.Background()
	}
	return NewTracer(ctx, meter, rec)
}
package agent_gateway

import (
	"context"

	"github.com/google/uuid"
)

// Trace 是 Agent Gateway 装饰器在每次 LLM 调用时从 ctx 读取的关联元数据。
// 由调用方在调用 llm.Client.Complete/StreamComplete 前用 WithTrace 注入；
// Gateway 装饰器在埋点时通过 FromContext 取出。
//
// 设计要点：
//   - 字段全部可选；缺失时空字符串，decorator 仍然能写库（session_id 为
//     uuid.Nil，agent_type=""，避免日志缺失直接 fail）。
//   - RequestID 留空时由 WithTrace 自动生成一个 uuid，方便在
//     llm_calls 表里唯一定位单次调用。
//   - ctx 嵌套时后者覆盖前者（最内层调用方权威）。
type Trace struct {
	SessionUUID string
	AgentType   string
	TaskType    string
	RequestID   string
	// Sessionless（R14）显式声明"这次调用本来就没有庭审会话"，例如 Prompt Lab 的
	// eval / abtest —— 它们评的是 prompt 质量，不属于任何一场庭审。
	//
	// 为什么需要这个显式标记，而不是简单地把空 session_uuid 也写库：审计表上
	// `session_id` 是外键，空值既可能是**有意**（Prompt Lab），也可能是**漏填**的 bug。
	// 以前一律当 bug 拦下（只写 llm_audit_fk_violation 审计事件），代价是无 session
	// 的调用永远进不了 llm_calls —— 看不到成本、也无法按 prompt 版本归因。
	// 现在把两者区分开：声明了 Sessionless 的照写（session_id 为 NULL），
	// 没声明却为空的**仍然被拦**，那道"漏填 session"的护栏继续有效。
	Sessionless bool
}

type traceKey struct{}

// WithTrace 把 trace 元数据挂到 ctx 上返回。返回的 ctx 适合直接传给
// llm.Client.Complete/StreamComplete。
func WithTrace(parent context.Context, tr Trace) context.Context {
	if tr.RequestID == "" {
		tr.RequestID = uuid.NewString()
	}
	return context.WithValue(parent, traceKey{}, tr)
}

// FromContext 取出 ctx 上的 trace；缺失时返回零值而不是 panic，方便测试
// 路径不传 trace 时不挂掉。
func FromContext(ctx context.Context) Trace {
	if ctx == nil {
		return Trace{}
	}
	if v, ok := ctx.Value(traceKey{}).(Trace); ok {
		return v
	}
	return Trace{}
}

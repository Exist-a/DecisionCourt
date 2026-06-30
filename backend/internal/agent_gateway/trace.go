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

// Package observability 提供白盒化基础设施：slog 结构化日志、Prometheus
// 兼容的业务指标、trace_id 上下文传播、Gin 中间件、业务事件审计落库。
//
// 设计原则：
//   - 仅依赖 Go 标准库（log/slog 已于 Go 1.21+ 内置），避免引入额外依赖。
//   - 与 agent_gateway.Trace 复用 trace_id 字段，不重复发明轮子。
//   - 接口优先 + 默认内存实现，方便测试时注入替身。
//   - OTLP / Prometheus 适配器留作 future PR 扩展（参见 ADR 0010）。
//
// 三大支柱：
//   1. Logging   - slog JSON handler，自动从 ctx 注入 trace_id / session_uuid / agent_type
//   2. Metrics   - 业务指标（counter / gauge / histogram），可从 /metrics JSON 端点查询
//   3. Tracing   - trace_id / request_id ctx 传播；business_spans.go 提供业务级 span
//
// 业务事件落库：
//   decision_events 表记录状态机迁移 / 调查员 dispatch 完成 / 信念收敛触发等
//   业务级 span 关闭事件，供前端 / 离线回放使用。
package observability

import (
	"context"
	"log/slog"
	"os"
	"sync"
)

// ctxKey 类型避免与其他包的 context.Value key 冲突。
type ctxKey int

const (
	loggerCtxKey ctxKey = iota
	traceCtxKey
)

// Trace 是 trace 元数据，与 agent_gateway.Trace 字段保持一致（不重复定义，
// 但因为 logger 在 ctx 上取的是字符串，所以这里只读 RequestID/SessionUUID）。
type Trace struct {
	RequestID   string
	SessionUUID string
	AgentType   string
	TaskType    string
}

// Default 返回全局默认 logger，初始化为 JSON 格式打到 stdout。
//
// 装配阶段可调用 Init 替换为带文件落盘的 handler；
// 测试阶段可调用 SetDefault 注入自定义 logger。
var (
	defaultLogger *slog.Logger
	defaultOnce   sync.Once
)

// Default 返回进程级默认 logger。第一次访问时延迟初始化为 JSON handler 打到 stdout。
func Default() *slog.Logger {
	defaultOnce.Do(func() {
		defaultLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	})
	return defaultLogger
}

// SetDefault 替换全局默认 logger。仅用于装配阶段（如 main.go）和测试。
func SetDefault(l *slog.Logger) {
	defaultLogger = l
	defaultOnce.Do(func() {}) // 让 future 的 Default() 调用直接返回已设置的 logger
}

// WithLogger 把 logger 挂到 ctx。配合 FromContext 使用可在调用链任意层获取 logger。
func WithLogger(parent context.Context, l *slog.Logger) context.Context {
	return context.WithValue(parent, loggerCtxKey, l)
}

// FromContext 从 ctx 取出 logger。缺失时返回 Default()。
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return Default()
	}
	if l, ok := ctx.Value(loggerCtxKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return Default()
}

// WithTrace 把 trace 元数据挂到 ctx。trace_id 字段名为 "trace_id" 与
// OTel 标准一致，方便未来切换 OTel 时不改日志字段。
func WithTrace(parent context.Context, tr Trace) context.Context {
	return context.WithValue(parent, traceCtxKey, tr)
}

// TraceFromContext 从 ctx 取出 trace。缺失时返回零值（所有字段空字符串）。
func TraceFromContext(ctx context.Context) Trace {
	if ctx == nil {
		return Trace{}
	}
	if tr, ok := ctx.Value(traceCtxKey).(Trace); ok {
		return tr
	}
	return Trace{}
}

// Logger 返回带 trace 元数据的 logger（自动加 trace_id / session_uuid / agent_type 字段）。
// 用法：observability.Logger(ctx).Info("...")，ctx 必须先 WithTrace 或 WithLogger。
func Logger(ctx context.Context) *slog.Logger {
	l := FromContext(ctx)
	tr := TraceFromContext(ctx)
	if tr.RequestID == "" && tr.SessionUUID == "" && tr.AgentType == "" {
		return l
	}
	return l.With(
		slog.String("trace_id", tr.RequestID),
		slog.String("session_uuid", tr.SessionUUID),
		slog.String("agent_type", tr.AgentType),
	)
}

// LogValue 把 Trace 编码为 slog.Value，方便嵌套到其他结构。
func (t Trace) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("trace_id", t.RequestID),
		slog.String("session_uuid", t.SessionUUID),
	}
	if t.AgentType != "" {
		attrs = append(attrs, slog.String("agent_type", t.AgentType))
	}
	if t.TaskType != "" {
		attrs = append(attrs, slog.String("task_type", t.TaskType))
	}
	return slog.GroupValue(attrs...)
}
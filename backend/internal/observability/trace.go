package observability

import (
	"context"
	"sync"
	"time"
)

// Span 是业务级 span 的抽象。轻量、自包含；不依赖 OTel SDK。
//
// 设计要点：
//   - StartSpan 返回的 span 必须 defer End()；End 时自动写入决策事件 / 指标。
//   - Attributes 通过 SetAttr / WithAttrs 增量设置，最终写入 decision_events.payload。
//   - 错误通过 SetError(err) 标记；End 时自动归类为 status=error 并记 metric。
//
// 为什么不直接用 OTel SDK？
//   1. 引入 OTel 需 5+ 依赖，对纯 Go MVP 过重。
//   2. 当前 trace 需求是"业务事件可审计"，OTel 的分布式 trace 语义未来再加。
//   3. 接口稳定后，可写一个 OTelTracerSpan adapter 平滑替换（参见 ADR 0010 §未来工作）。
type Span interface {
	// SetAttr 设置单个属性（覆盖语义）。
	SetAttr(key string, value interface{})
	// SetAttrs 批量设置属性。
	SetAttrs(attrs map[string]interface{})
	// SetError 标记 span 失败。多次调用以最后一次为准。
	SetError(err error)
	// SetStatus 设置 status（"ok" / "error" / 自定义字符串）。
	SetStatus(status string)
	// End 关闭 span：写入 decision_events（如已注入 Recorder）+ 增加 metric。
	// 多次 End 只生效第一次（idempotent）。
	End()
}

// spanImpl 是 Span 的默认实现。
type spanImpl struct {
	mu          sync.Mutex
	name        string
	trace       Trace
	attrs       map[string]interface{}
	status      string
	err         error
	startTime   int64 // unix nano
	endTime     int64
	ended       bool
	recorder    EventRecorder
	metrics     Metrics
	metricName  string
	metricError string
	srcCtx      context.Context
}

// EventRecorder 是 decision_events 写入器接口，由 courtroom 包实现并注入。
// 这里抽象为接口避免 observability 直接依赖 model 包（防止循环引用）。
type EventRecorder interface {
	Record(ctx context.Context, ev DecisionEventRecord) error
}

// DecisionEventRecord 是写入 decision_events 表的最小单元。
// 字段与 model.DecisionEvent 对齐，但用 interface{} 而非 pg 特定类型以便复用。
type DecisionEventRecord struct {
	SessionUUID string
	RequestID   string
	EventType   string
	AgentType   string
	Payload     map[string]interface{}
	DurationMs  int64
	Status      string
	ErrorMsg    string
}

// Tracer 是创建 span 的工厂。
type Tracer struct {
	trace   Trace
	meter   Metrics
	rec     EventRecorder
	srcCtx  context.Context
}

// NewTracer 构造一个 Tracer。EventRecorder 为 nil 时 End 只记 metric，不写库（用于测试）。
func NewTracer(ctx context.Context, meter Metrics, rec EventRecorder) *Tracer {
	return &Tracer{
		trace:  TraceFromContext(ctx),
		meter:  meter,
		rec:    rec,
		srcCtx: ctx,
	}
}

// StartSpan 开启一个业务级 span。name 是事件名（如 "RunCrossExamRound" / "A2A.Publish"）。
func (t *Tracer) StartSpan(name string) Span {
	if t == nil {
		return noopSpan{}
	}
	return &spanImpl{
		name:       name,
		trace:      t.trace,
		attrs:      make(map[string]interface{}),
		startTime:  nowUnixNano(),
		recorder:   t.rec,
		metrics:    t.meter,
		metricName: MetricSpanTotal,
		metricError: MetricSpanErrorTotal,
	}
}

// SetAttr 实现 Span。
func (s *spanImpl) SetAttr(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.attrs[key] = value
}

func (s *spanImpl) SetAttrs(attrs map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	for k, v := range attrs {
		s.attrs[k] = v
	}
}

func (s *spanImpl) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.err = err
	s.status = "error"
	if err != nil {
		s.attrs["error.message"] = err.Error()
	}
}

func (s *spanImpl) SetStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.status = status
}

func (s *spanImpl) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended = true
	s.endTime = nowUnixNano()
	dur := time.Duration(s.endTime - s.startTime)
	if s.status == "" {
		s.status = "ok"
	}

	// 1. 写 metric
	if s.metrics != nil {
		labels := map[string]string{
			"name":   s.name,
			"status": s.status,
		}
		if s.trace.AgentType != "" {
			labels["agent_type"] = s.trace.AgentType
		}
		s.metrics.IncCounter(s.metricName, labels)
		s.metrics.ObserveHistogram(MetricSpanDuration, labels, dur.Seconds())
		if s.status == "error" {
			s.metrics.IncCounter(s.metricError, labels)
		}
	}

	// 2. 写 decision_events
	if s.recorder != nil {
		errMsg := ""
		if s.err != nil {
			errMsg = s.err.Error()
		}
		_ = s.recorder.Record(s.srcCtx, DecisionEventRecord{
			SessionUUID: s.trace.SessionUUID,
			RequestID:   s.trace.RequestID,
			EventType:   "span." + s.name,
			AgentType:   s.trace.AgentType,
			Payload:     s.attrs,
			DurationMs:  dur.Milliseconds(),
			Status:      s.status,
			ErrorMsg:    errMsg,
		})
	}
}

// noopSpan 是 Tracer 为 nil 时的占位实现，避免调用方 nil-check。
type noopSpan struct{}

func (noopSpan) SetAttr(string, interface{})         {}
func (noopSpan) SetAttrs(map[string]interface{})     {}
func (noopSpan) SetError(error)                      {}
func (noopSpan) SetStatus(string)                    {}
func (noopSpan) End()                                {}
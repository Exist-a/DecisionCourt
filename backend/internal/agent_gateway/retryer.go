package agent_gateway

import (
	"context"
	"time"
)

// Retryer 对 LLM 调用做退避重试。MVP 仅对 Complete 生效；
// StreamComplete 失败不重试，因为流式重试会破坏 chunk 连续性。
//
// 默认退避：500ms, 1s, 2s，最多 3 次重试。可通过 NewRetryerWithBackoff
// 自定义，方便单测加速。
const (
	defaultBackoffBase = 500 * time.Millisecond
)

// Retryer 执行重试。
type Retryer struct {
	backoffDurations []time.Duration
	lastCount        int
}

// RetryResult 把每次 operation 的结果暴露给调用方。调用方在闭包里更新
// 自己的 result 变量；Retryer 只负责重试与计数。
func NewRetryer() *Retryer {
	return NewRetryerWithBackoff([]time.Duration{
		defaultBackoffBase,
		2 * defaultBackoffBase,
		4 * defaultBackoffBase,
	})
}

// NewRetryerWithBackoff 用自定义退避构造；空切片表示不重试。
func NewRetryerWithBackoff(durations []time.Duration) *Retryer {
	return &Retryer{backoffDurations: durations}
}

// Do 执行 operation，失败时按退避重试。返回最终 error；调用方闭包可
// 获取 operation 成功时的返回值。返回 nil 表示 operation 在某次成功。
func (r *Retryer) Do(operation func() error) error {
	r.lastCount = 0
	err := operation()
	if err == nil {
		return nil
	}
	for _, d := range r.backoffDurations {
		select {
		case <-time.After(d):
		}
		r.lastCount++
		if err = operation(); err == nil {
			return nil
		}
	}
	return err
}

// DoContext 与 Do 相同，但支持 ctx 取消。本项目中 Complete 使用。
func (r *Retryer) DoContext(ctx context.Context, operation func() error) error {
	r.lastCount = 0
	err := operation()
	if err == nil {
		return nil
	}
	for _, d := range r.backoffDurations {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		r.lastCount++
		if err = operation(); err == nil {
			return nil
		}
	}
	return err
}

// LastCount 返回上一次 Do 的重试次数（不含首次尝试）。
func (r *Retryer) LastCount() int { return r.lastCount }
